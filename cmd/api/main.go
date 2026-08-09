// Command api runs the Courier OS HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ceserve/courier-os/internal/api"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/logging"
	"github.com/ceserve/courier-os/internal/platform/storage"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api: fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(logging.Config{Level: cfg.Log.Level, Format: cfg.Log.Format}).With(
		slog.String("service", cfg.Service.Name),
		slog.String("version", cfg.Service.Version),
		slog.String("component", "api"),
	)
	slog.SetDefault(log)

	// SIGINT/SIGTERM cancels the root context, which drains the HTTP server and
	// then releases every dependency in reverse order.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, cfg, log)
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer tel.Shutdown(context.WithoutCancel(ctx))

	// Published before anything can fail, so a process that dies during startup
	// still tells a dashboard which build died.
	tel.Metrics.SetBuildInfo(cfg.Service.Version, cfg.Service.Commit, cfg.Service.BuiltAt)

	db, err := database.Open(ctx, cfg.Database, log, database.WithQueryTracer(
		database.NewQueryTracer(log, cfg.Database.SlowQueryThreshold, database.SlowQueryMetrics{
			Duration: tel.Metrics.QueryDuration, SlowTotal: tel.Metrics.SlowQueries,
		})))
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	redis, err := cache.Open(ctx, cfg.Redis, log)
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	defer func() { _ = redis.Close() }()

	objects, err := storage.Open(storage.Config{
		Driver: cfg.Storage.Driver, Bucket: cfg.Storage.Bucket,
		Endpoint: cfg.Storage.Endpoint, Region: cfg.Storage.Region,
		AccessKey: cfg.Storage.AccessKey, SecretKey: cfg.Storage.SecretKey,
		UsePathStyle: cfg.Storage.UsePathStyle, Dir: cfg.Storage.Dir,
	})
	if err != nil {
		return fmt.Errorf("open object storage: %w", err)
	}
	log.Info("object storage ready",
		slog.String("driver", cfg.Storage.Driver), slog.String("bucket", cfg.Storage.Bucket))

	app := api.New(api.Dependencies{
		Config: cfg, DB: db, Cache: redis, Logger: log, Telemetry: tel, Storage: objects,
	})

	// The schema is verified but never migrated here: migrations are an
	// explicit deploy step (cmd/migrate) so a rolling restart can never apply
	// DDL implicitly.
	if err := verifySchema(ctx, db, log); err != nil {
		return err
	}

	srv := httpx.NewServer(cfg.HTTP, app.Handler(), log)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.Run(gctx) })

	if cfg.HTTP.EnableMetrics {
		// Metrics listen on a separate, loopback-bound port so the scrape
		// endpoint is never exposed through the public ingress.
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", tel.MetricsHandler())
		g.Go(func() error { return httpx.RunAux(gctx, cfg.HTTP.MetricsAddr, metricsMux, log) })
	}
	g.Go(func() error { return publishRuntimeStats(gctx, app, tel) })

	log.Info("api ready",
		slog.String("env", cfg.Env),
		slog.String("addr", cfg.HTTP.Addr),
		slog.Bool("metrics", cfg.HTTP.EnableMetrics),
		slog.Bool("rate_limit", cfg.Security.RateLimitEnabled))

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// verifySchema refuses to start against a database whose migrations have not
// been applied, which turns a confusing runtime failure into a clear startup
// error naming the missing version.
func verifySchema(ctx context.Context, db *database.DB, log *slog.Logger) error {
	var applied int
	err := db.Pool.QueryRow(ctx, `
		SELECT count(*)::int FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'schema_migrations'`).Scan(&applied)
	if err != nil {
		return fmt.Errorf("verify schema: %w", err)
	}
	if applied == 0 {
		return errors.New("database has no schema_migrations table; run `migrate up` before starting the API")
	}
	var version int64
	if err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(max(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version == 0 {
		return errors.New("no migrations have been applied; run `migrate up` before starting the API")
	}
	log.Info("schema verified", slog.Int64("migration_version", version))
	return nil
}

// publishPoolStats keeps the database pool gauges current for the dashboard.
// publishRuntimeStats samples the things that are cheap to read and expensive
// to be blind to: pool saturation, and whether the dependencies are answering.
//
// Sampling rather than instrumenting: pool stats are a snapshot by nature, and
// a health check is a probe. Fifteen seconds is frequent enough to catch a pool
// filling up before requests start queuing, and infrequent enough that the
// probes themselves are not load.
func publishRuntimeStats(ctx context.Context, app *api.Application, tel *telemetry.Provider) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s := app.DB.Stats()
			tel.Metrics.SetPoolStats(telemetry.PoolStatsSnapshot{
				Acquired: s.AcquiredConns, Idle: s.IdleConns,
				Total: s.TotalConns, Max: s.MaxConns,
			})

			// Bounded probes. A hung dependency must not stall the sampler and
			// leave every gauge frozen at its last good value, which reads as
			// health.
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			tel.Metrics.SetDependencyUp("postgres", app.DB.Pool.Ping(probeCtx) == nil)
			if app.Cache != nil {
				tel.Metrics.SetDependencyUp("redis", app.Cache.Ping(probeCtx) == nil)
			}
			cancel()
		}
	}
}
