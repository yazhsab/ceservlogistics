// Package database owns the PostgreSQL connection pool and transaction
// primitives shared by every module.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ceserve/courier-os/internal/platform/config"
)

// DB wraps a bounded pgx pool.
type DB struct {
	Pool *pgxpool.Pool
	log  *slog.Logger
}

// Open builds and verifies the pool.
//
// A server-side statement_timeout is applied to every connection so that a
// pathological query cannot pin a connection (and therefore a slot of the small
// pool) indefinitely on the 4 vCPU target.
func Open(ctx context.Context, cfg config.DatabaseConfig, log *slog.Logger, opts ...Option) (*DB, error) {
	var o options
	for _, apply := range opts {
		apply(&o)
	}
	// Defensive defaults: a zero MaxConns or HealthCheckPeriod makes pgx panic
	// at runtime rather than fail at startup, so callers that build a
	// DatabaseConfig by hand (tools, tests, one-off jobs) get sane values
	// instead of a crash in a background goroutine.
	cfg = withPoolDefaults(cfg)

	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.MaxConnLifetimeJitter = cfg.MaxConnLifetime / 10
	pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	pc.HealthCheckPeriod = cfg.HealthCheckPeriod
	pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	if pc.ConnConfig.RuntimeParams == nil {
		pc.ConnConfig.RuntimeParams = map[string]string{}
	}
	if cfg.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	}
	// Bound how long a transaction may sit idle holding locks.
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "30000"
	pc.ConnConfig.RuntimeParams["application_name"] = "courier-os"

	// Every statement is timed. The tracer is nil-safe, so a caller that does
	// not want query observability (migrations, one-off tools) simply omits it.
	if o.tracer != nil {
		pc.ConnConfig.Tracer = o.tracer
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout+2*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	log.Info("database pool ready",
		slog.Int("max_conns", int(cfg.MaxConns)),
		slog.Int("min_conns", int(cfg.MinConns)),
		slog.Duration("statement_timeout", cfg.StatementTimeout),
	)
	return &DB{Pool: pool, log: log}, nil
}

// Option configures the pool beyond its DatabaseConfig.
type Option func(*options)

type options struct{ tracer *QueryTracer }

// WithQueryTracer times every statement and logs the slow ones.
func WithQueryTracer(t *QueryTracer) Option {
	return func(o *options) { o.tracer = t }
}

// withPoolDefaults fills in any unset pool setting.
func withPoolDefaults(cfg config.DatabaseConfig) config.DatabaseConfig {
	if cfg.MaxConns < 1 {
		cfg.MaxConns = 10
	}
	if cfg.MinConns < 0 || cfg.MinConns > cfg.MaxConns {
		cfg.MinConns = 0
	}
	if cfg.MaxConnLifetime <= 0 {
		cfg.MaxConnLifetime = 55 * time.Minute
	}
	if cfg.MaxConnIdleTime <= 0 {
		cfg.MaxConnIdleTime = 5 * time.Minute
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.HealthCheckPeriod <= 0 {
		cfg.HealthCheckPeriod = 30 * time.Second
	}
	return cfg
}

// Close releases all connections.
func (d *DB) Close() {
	if d != nil && d.Pool != nil {
		d.Pool.Close()
	}
}

// Ping verifies connectivity, used by the readiness probe.
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

// Stats exposes pool utilisation for metrics and the readiness payload.
func (d *DB) Stats() PoolStats {
	s := d.Pool.Stat()
	return PoolStats{
		AcquiredConns:   s.AcquiredConns(),
		IdleConns:       s.IdleConns(),
		TotalConns:      s.TotalConns(),
		MaxConns:        s.MaxConns(),
		NewConnsCount:   s.NewConnsCount(),
		AcquireCount:    s.AcquireCount(),
		EmptyAcquires:   s.EmptyAcquireCount(),
		CanceledAcquire: s.CanceledAcquireCount(),
	}
}

// PoolStats is a serialisable snapshot of pool utilisation.
type PoolStats struct {
	AcquiredConns   int32 `json:"acquiredConns"`
	IdleConns       int32 `json:"idleConns"`
	TotalConns      int32 `json:"totalConns"`
	MaxConns        int32 `json:"maxConns"`
	NewConnsCount   int64 `json:"newConnsCount"`
	AcquireCount    int64 `json:"acquireCount"`
	EmptyAcquires   int64 `json:"emptyAcquireCount"`
	CanceledAcquire int64 `json:"canceledAcquireCount"`
}

// InTx runs fn inside a transaction, committing on success and rolling back on
// error or panic.
//
// Serialization failures and deadlocks are retried with bounded backoff because
// concurrent branch operations legitimately collide (Constitution §22). fn must
// therefore be free of external side effects.
func (d *DB) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return d.InTxOpts(ctx, pgx.TxOptions{}, fn)
}

// InTxOpts is InTx with explicit transaction options (isolation level, access
// mode).
func (d *DB) InTxOpts(ctx context.Context, opts pgx.TxOptions, fn func(pgx.Tx) error) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := d.runTx(ctx, opts, fn)
		if err == nil {
			return nil
		}
		lastErr = err
		if !IsRetryable(err) || attempt == maxAttempts || ctx.Err() != nil {
			return err
		}
		backoff := time.Duration(attempt*attempt) * 10 * time.Millisecond
		d.log.Warn("retrying transaction after serialization conflict",
			slog.Int("attempt", attempt), slog.String("error", err.Error()))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return lastErr
}

func (d *DB) runTx(ctx context.Context, opts pgx.TxOptions, fn func(pgx.Tx) error) (err error) {
	tx, err := d.Pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			// Use a detached context so rollback still runs when the request
			// context has already been cancelled.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// PostgreSQL SQLSTATE codes used across the platform.
const (
	SQLStateUniqueViolation     = "23505"
	SQLStateForeignKeyViolation = "23503"
	SQLStateCheckViolation      = "23514"
	SQLStateNotNullViolation    = "23502"
	SQLStateExclusionViolation  = "23P01"
	SQLStateSerializationFail   = "40001"
	SQLStateDeadlockDetected    = "40P01"
	SQLStateLockNotAvailable    = "55P03"
	SQLStateQueryCanceled       = "57014"
	SQLStateRaiseException      = "P0001"
)

// IsRetryable reports whether err is a transient concurrency failure that is
// safe to retry.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == SQLStateSerializationFail || pgErr.Code == SQLStateDeadlockDetected
	}
	return false
}

// IsUniqueViolation reports whether err is a unique-constraint violation,
// optionally restricted to specific constraint names.
func IsUniqueViolation(err error, constraints ...string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != SQLStateUniqueViolation {
		return false
	}
	if len(constraints) == 0 {
		return true
	}
	for _, c := range constraints {
		if pgErr.ConstraintName == c {
			return true
		}
	}
	return false
}

// IsForeignKeyViolation reports whether err is an FK violation.
func IsForeignKeyViolation(err error, constraints ...string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != SQLStateForeignKeyViolation {
		return false
	}
	if len(constraints) == 0 {
		return true
	}
	for _, c := range constraints {
		if pgErr.ConstraintName == c {
			return true
		}
	}
	return false
}

// ConstraintName returns the violated constraint name, or "".
func ConstraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// IsNoRows reports whether err signals an empty result set.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// RaisedMessage returns the message from a PL/pgSQL RAISE EXCEPTION, or "".
// Database triggers guard append-only tables; the message identifies which.
func RaisedMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == SQLStateRaiseException {
		return pgErr.Message
	}
	return ""
}
