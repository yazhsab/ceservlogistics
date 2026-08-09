package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLockID is the advisory-lock key that serialises migrations across
// every process. Two API containers starting simultaneously therefore cannot
// apply the same migration twice, and a rolling deploy is safe without external
// coordination.
const migrationLockID int64 = 7_246_190_842_115_003

// noTransactionDirective marks a migration that must run outside a transaction,
// for example CREATE INDEX CONCURRENTLY.
const noTransactionDirective = "-- migrate:no-transaction"

// Migration is a single versioned change.
type Migration struct {
	Version  int64
	Name     string
	UpSQL    string
	DownSQL  string
	Checksum string
	NoTx     bool
}

// AppliedMigration is a row of schema_migrations.
type AppliedMigration struct {
	Version     int64
	Name        string
	Checksum    string
	AppliedAt   time.Time
	ExecutionMS int64
}

// Migrator applies migrations from an fs.FS.
type Migrator struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	list []Migration
}

// NewMigrator loads and validates the migration corpus.
//
// Files are named `<version>_<name>.up.sql` with an optional matching
// `.down.sql`. Versions must be unique and are applied in ascending order.
func NewMigrator(pool *pgxpool.Pool, fsys fs.FS, dir string, log *slog.Logger) (*Migrator, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}
	ups := map[int64]Migration{}
	downs := map[int64]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		version, name, direction, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}
		switch direction {
		case "up":
			if _, dup := ups[version]; dup {
				return nil, fmt.Errorf("duplicate migration version %d", version)
			}
			sum := sha256.Sum256(raw)
			ups[version] = Migration{
				Version:  version,
				Name:     name,
				UpSQL:    string(raw),
				Checksum: hex.EncodeToString(sum[:]),
				NoTx:     strings.Contains(string(raw), noTransactionDirective),
			}
		case "down":
			downs[version] = string(raw)
		}
	}
	list := make([]Migration, 0, len(ups))
	for v, m := range ups {
		m.DownSQL = downs[v]
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Version < list[j].Version })
	if len(list) == 0 {
		return nil, errors.New("no migrations found")
	}
	return &Migrator{pool: pool, log: log, list: list}, nil
}

func parseMigrationName(fileName string) (int64, string, string, error) {
	base := strings.TrimSuffix(fileName, ".sql")
	idx := strings.LastIndex(base, ".")
	if idx < 0 {
		return 0, "", "", fmt.Errorf("migration %q must end with .up.sql or .down.sql", fileName)
	}
	direction := base[idx+1:]
	if direction != "up" && direction != "down" {
		return 0, "", "", fmt.Errorf("migration %q must end with .up.sql or .down.sql", fileName)
	}
	rest := base[:idx]
	verStr, name, ok := strings.Cut(rest, "_")
	if !ok {
		return 0, "", "", fmt.Errorf("migration %q must be named <version>_<name>.<direction>.sql", fileName)
	}
	version, err := strconv.ParseInt(verStr, 10, 64)
	if err != nil || version <= 0 {
		return 0, "", "", fmt.Errorf("migration %q has an invalid version prefix", fileName)
	}
	return version, name, direction, nil
}

const createSchemaMigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version      bigint PRIMARY KEY,
    name         text        NOT NULL,
    checksum     text        NOT NULL,
    applied_at   timestamptz NOT NULL DEFAULT now(),
    execution_ms bigint      NOT NULL DEFAULT 0
);`

// Up applies every pending migration.
func (m *Migrator) Up(ctx context.Context) (applied int, err error) {
	return m.upTo(ctx, m.list[len(m.list)-1].Version)
}

// UpTo applies pending migrations up to and including target.
func (m *Migrator) UpTo(ctx context.Context, target int64) (int, error) { return m.upTo(ctx, target) }

func (m *Migrator) upTo(ctx context.Context, target int64) (int, error) {
	conn, release, err := m.lock(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	if _, err := conn.Exec(ctx, createSchemaMigrations); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}
	appliedMap, err := m.appliedOn(ctx, conn)
	if err != nil {
		return 0, err
	}
	if err := m.verifyChecksums(appliedMap); err != nil {
		return 0, err
	}

	count := 0
	for _, mig := range m.list {
		if mig.Version > target {
			break
		}
		if _, done := appliedMap[mig.Version]; done {
			continue
		}
		start := time.Now()
		if err := m.applyOne(ctx, conn, mig); err != nil {
			return count, fmt.Errorf("migration %d_%s failed: %w", mig.Version, mig.Name, err)
		}
		dur := time.Since(start)
		m.log.Info("migration applied",
			slog.Int64("version", mig.Version),
			slog.String("name", mig.Name),
			slog.Int64("duration_ms", dur.Milliseconds()),
		)
		count++
	}
	return count, nil
}

func (m *Migrator) applyOne(ctx context.Context, conn *pgxpool.Conn, mig Migration) error {
	start := time.Now()
	const record = `INSERT INTO schema_migrations (version, name, checksum, execution_ms) VALUES ($1,$2,$3,$4)`

	if mig.NoTx {
		// Statements such as CREATE INDEX CONCURRENTLY cannot run in a
		// transaction. The bookkeeping insert is therefore not atomic with the
		// DDL; the migration body must be idempotent (IF NOT EXISTS).
		if _, err := conn.Exec(ctx, mig.UpSQL); err != nil {
			return err
		}
		_, err := conn.Exec(ctx, record, mig.Version, mig.Name, mig.Checksum, time.Since(start).Milliseconds())
		return err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, mig.UpSQL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, record, mig.Version, mig.Name, mig.Checksum, time.Since(start).Milliseconds()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Down rolls back the last n applied migrations.
func (m *Migrator) Down(ctx context.Context, n int) (int, error) {
	if n < 1 {
		return 0, errors.New("down requires a positive step count")
	}
	conn, release, err := m.lock(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	if _, err := conn.Exec(ctx, createSchemaMigrations); err != nil {
		return 0, err
	}
	appliedMap, err := m.appliedOn(ctx, conn)
	if err != nil {
		return 0, err
	}
	byVersion := map[int64]Migration{}
	for _, mig := range m.list {
		byVersion[mig.Version] = mig
	}
	versions := make([]int64, 0, len(appliedMap))
	for v := range appliedMap {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] > versions[j] })

	rolled := 0
	for i := 0; i < n && i < len(versions); i++ {
		v := versions[i]
		mig, ok := byVersion[v]
		if !ok {
			return rolled, fmt.Errorf("migration %d is applied but its files are missing", v)
		}
		if strings.TrimSpace(mig.DownSQL) == "" {
			return rolled, fmt.Errorf("migration %d_%s has no down file and cannot be rolled back", v, mig.Name)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return rolled, err
		}
		if _, err := tx.Exec(ctx, mig.DownSQL); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return rolled, fmt.Errorf("rollback of %d_%s failed: %w", v, mig.Name, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, v); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return rolled, err
		}
		if err := tx.Commit(ctx); err != nil {
			return rolled, err
		}
		m.log.Info("migration rolled back", slog.Int64("version", v), slog.String("name", mig.Name))
		rolled++
	}
	return rolled, nil
}

// Status returns the pending and applied sets.
func (m *Migrator) Status(ctx context.Context) (applied []AppliedMigration, pending []Migration, err error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, createSchemaMigrations); err != nil {
		return nil, nil, err
	}
	appliedMap, err := m.appliedOn(ctx, conn)
	if err != nil {
		return nil, nil, err
	}
	for _, mig := range m.list {
		if a, ok := appliedMap[mig.Version]; ok {
			applied = append(applied, a)
		} else {
			pending = append(pending, mig)
		}
	}
	sort.Slice(applied, func(i, j int) bool { return applied[i].Version < applied[j].Version })
	return applied, pending, nil
}

// Validate confirms that every applied migration still matches its file. A
// mismatch means someone edited a migration that production has already run,
// which silently diverges environments; it is treated as a hard failure.
func (m *Migrator) Validate(ctx context.Context) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, createSchemaMigrations); err != nil {
		return err
	}
	appliedMap, err := m.appliedOn(ctx, conn)
	if err != nil {
		return err
	}
	return m.verifyChecksums(appliedMap)
}

func (m *Migrator) verifyChecksums(applied map[int64]AppliedMigration) error {
	byVersion := map[int64]Migration{}
	for _, mig := range m.list {
		byVersion[mig.Version] = mig
	}
	var problems []string
	for v, a := range applied {
		mig, ok := byVersion[v]
		if !ok {
			problems = append(problems, fmt.Sprintf("version %d is applied in the database but has no migration file", v))
			continue
		}
		if mig.Checksum != a.Checksum {
			problems = append(problems, fmt.Sprintf("version %d (%s) was modified after being applied", v, mig.Name))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("migration validation failed:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func (m *Migrator) appliedOn(ctx context.Context, conn *pgxpool.Conn) (map[int64]AppliedMigration, error) {
	rows, err := conn.Query(ctx,
		`SELECT version, name, checksum, applied_at, execution_ms FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[int64]AppliedMigration{}
	for rows.Next() {
		var a AppliedMigration
		if err := rows.Scan(&a.Version, &a.Name, &a.Checksum, &a.AppliedAt, &a.ExecutionMS); err != nil {
			return nil, err
		}
		out[a.Version] = a
	}
	return out, rows.Err()
}

// lock acquires the migration advisory lock on a dedicated connection.
func (m *Migrator) lock(ctx context.Context) (*pgxpool.Conn, func(), error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire connection: %w", err)
	}
	lockCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := conn.Exec(lockCtx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf("acquire migration lock (another migration may be running): %w", err)
	}
	return conn, func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer unlockCancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
			m.log.Warn("failed to release migration lock", slog.String("error", err.Error()))
		}
		conn.Release()
	}, nil
}

// ExecScript runs an arbitrary SQL script (used for seeds) in one transaction.
func ExecScript(ctx context.Context, pool *pgxpool.Pool, sql string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
