// Command worker runs background jobs: bulk imports and periodic maintenance.
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

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"github.com/ceserve/courier-os/internal/api"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/manifest"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/logging"
	"github.com/ceserve/courier-os/internal/platform/storage"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker: fatal: %v\n", err)
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
		slog.String("component", "worker"),
	)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.Setup(ctx, cfg, log)
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer tel.Shutdown(context.WithoutCancel(ctx))

	// The worker runs a smaller pool than the API: it is bounded by
	// WORKER_CONCURRENCY, and on the 4 vCPU target the API must keep the
	// larger share of the connection budget.
	dbCfg := cfg.Database
	if dbCfg.MaxConns > int32(cfg.Worker.Concurrency)+4 {
		dbCfg.MaxConns = int32(cfg.Worker.Concurrency) + 4
	}
	db, err := database.Open(ctx, dbCfg, log, database.WithQueryTracer(
		database.NewQueryTracer(log, dbCfg.SlowQueryThreshold, database.SlowQueryMetrics{
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

	// The worker builds the same application graph as the API so job handlers
	// use exactly the same services, with the same validation and the same
	// cache invalidation, as the request path.
	objects, err := storage.Open(storage.Config{
		Driver: cfg.Storage.Driver, Bucket: cfg.Storage.Bucket,
		Endpoint: cfg.Storage.Endpoint, Region: cfg.Storage.Region,
		AccessKey: cfg.Storage.AccessKey, SecretKey: cfg.Storage.SecretKey,
		UsePathStyle: cfg.Storage.UsePathStyle, Dir: cfg.Storage.Dir,
	})
	if err != nil {
		return fmt.Errorf("open object storage: %w", err)
	}

	app := api.New(api.Dependencies{
		Config: cfg, DB: db, Cache: redis, Logger: log, Telemetry: tel, Storage: objects,
	})

	worker := jobs.NewWorker(db, app.Queries, jobs.WorkerConfig{
		Queues:       cfg.Worker.Queues,
		Concurrency:  cfg.Worker.Concurrency,
		BatchSize:    cfg.Worker.BatchSize,
		PollInterval: cfg.Worker.PollInterval,
		StuckAfter:   cfg.Worker.StuckJobTimeout,
	}, log, tel.Metrics)

	app.GeographyImport.RegisterHandlers(worker)
	// Manifest documents are rendered off the request path (§38).
	manifest.NewDocumentBuilder(app.Manifest, objects).RegisterHandlers(worker)
	// Release 4: everything that talks to the outside world does it from here,
	// never from a request. A dead SMS gateway or a partner's dead endpoint
	// costs a retry, not a shipment.
	app.Notification.RegisterHandlers(worker)
	app.Webhooks.RegisterHandlers(worker)
	app.Analytics.RegisterHandlers(worker)
	// Reports are generated here and nowhere else: §38 forbids building one in
	// an HTTP request, and the engine streams straight to object storage.
	app.Reporting.RegisterHandlers(worker)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return worker.Run(gctx) })
	g.Go(func() error { return runMaintenance(gctx, app, log) })

	// A small listener, for two reasons that both turned out to be defects.
	//
	// The container healthcheck is inherited from the image, which serves the
	// API — so it probed :8080/livez against a process with no HTTP listener
	// and reported a perfectly healthy worker as unhealthy forever. A permanent
	// red that everyone learns to ignore is worse than no healthcheck.
	//
	// And every courier_jobs_* metric is recorded by whichever process runs the
	// job, which is this one. Without a scrape endpoint here, queue depth and
	// worker failure counts existed in memory and were visible to nobody.
	g.Go(func() error {
		mux := http.NewServeMux()
		mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		// Readiness is the dependencies, same as the API: a worker that cannot
		// reach PostgreSQL is not going to claim a job.
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			probeCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := app.DB.Pool.Ping(probeCtx); err != nil {
				http.Error(w, "database unavailable", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
		})
		mux.Handle("/metrics", tel.MetricsHandler())
		return httpx.RunAux(gctx, cfg.Worker.HealthAddr, mux, log)
	})

	log.Info("worker ready",
		slog.Any("queues", cfg.Worker.Queues),
		slog.Int("concurrency", cfg.Worker.Concurrency),
		slog.String("health_addr", cfg.Worker.HealthAddr))

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// runMaintenance performs the periodic housekeeping that keeps unbounded tables
// bounded: expired idempotency keys, expired sessions and reset tokens, and
// lockouts whose window has passed.
func runMaintenance(ctx context.Context, app *api.Application, log *slog.Logger) error {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			sweep(ctx, app, log)
		}
	}
}

func sweep(ctx context.Context, app *api.Application, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if n, err := app.Idempotency.PurgeExpired(ctx); err != nil {
		log.Warn("failed to purge idempotency keys", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("purged expired idempotency keys", slog.Int64("count", n))
	}
	if n, err := app.Queries.PurgeExpiredSessions(ctx); err != nil {
		log.Warn("failed to purge sessions", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("purged expired sessions", slog.Int64("count", n))
	}
	if n, err := app.Queries.PurgeExpiredResetTokens(ctx); err != nil {
		log.Warn("failed to purge reset tokens", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("purged expired reset tokens", slog.Int64("count", n))
	}
	if n, err := app.Queries.UnlockExpiredUserLocks(ctx); err != nil {
		log.Warn("failed to clear expired lockouts", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("cleared expired account lockouts", slog.Int64("count", n))
	}

	// An API key with an expiry stops working whether or not anyone remembered
	// to revoke it.
	if err := app.Queries.ExpireAPIKeys(ctx); err != nil {
		log.Warn("failed to expire API keys", slog.String("error", err.Error()))
	}

	// Notifications and webhook deliveries are claimed PENDING -> SENDING and
	// nothing else, so a worker that dies mid-send leaves its row stranded.
	// Recover on a staleness window well past any send timeout.
	stale := pgtype.Interval{Microseconds: (15 * time.Minute).Microseconds(), Valid: true}
	if n, err := app.Queries.ReclaimStuckNotifications(ctx,
		dbgen.ReclaimStuckNotificationsParams{StaleAfter: stale}); err != nil {
		log.Warn("failed to reclaim stuck notifications", slog.String("error", err.Error()))
	} else if len(n) > 0 {
		log.Info("reclaimed stuck notifications", slog.Int("count", len(n)))
	}
	if n, err := app.Queries.ReclaimStuckDeliveries(ctx,
		dbgen.ReclaimStuckDeliveriesParams{StaleAfter: stale}); err != nil {
		log.Warn("failed to reclaim stuck webhook deliveries", slog.String("error", err.Error()))
	} else if len(n) > 0 {
		log.Info("reclaimed stuck webhook deliveries", slog.Int("count", len(n)))
	}

	// Both Raise() and Emit() log and continue when their send job cannot be
	// enqueued, so that a queue problem never rolls back the business write
	// that caused the message. These two sweeps are what collects the rows that
	// choice leaves behind; re-enqueueing is deduplicated, so a row that
	// already has a live job costs nothing.
	if n, err := app.Notification.SweepDue(ctx, 500); err != nil {
		log.Warn("failed to sweep due notifications", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("re-enqueued notifications with no job", slog.Int("count", n))
	}
	if n, err := app.Webhooks.SweepDue(ctx, 500); err != nil {
		log.Warn("failed to sweep due webhook deliveries", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("re-enqueued webhook deliveries with no job", slog.Int("count", n))
	}

	// One backlog reading per tenant per sweep, for the command centre's trend
	// charts. The live gauges are counted at request time and do not depend on
	// this; only the history does.
	if n, err := app.Analytics.CaptureAll(ctx); err != nil {
		log.Warn("failed to capture operational snapshots", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("captured operational snapshots", slog.Int("organizations", n))
	}
	// An export is customer data sitting in a bucket. Expiring the record and
	// deleting the object happen together; doing only the first would leave the
	// file behind for ever.
	if n, err := app.Reporting.ExpireRuns(ctx); err != nil {
		log.Warn("failed to expire report runs", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("expired report downloads", slog.Int("count", n))
	}

	// Trend charts look back weeks, not years, and this table is written on a
	// timer forever.
	if n, err := app.Analytics.PurgeSnapshots(ctx, 90*24*time.Hour); err != nil {
		log.Warn("failed to purge old snapshots", slog.String("error", err.Error()))
	} else if n > 0 {
		log.Info("purged old operational snapshots", slog.Int64("count", n))
	}
}
