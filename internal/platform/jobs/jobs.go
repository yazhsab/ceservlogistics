// Package jobs implements the PostgreSQL-backed background job queue.
//
// Constitution §5 forbids Kafka or RabbitMQ without an ADR, and §30 requires
// retry, bounded backoff, deduplication, error recording and dead-lettering.
// A `jobs` table claimed with FOR UPDATE SKIP LOCKED provides all of that with
// one durable store, which is the right trade for a single 4 vCPU box.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
)

// Queue names.
const (
	QueueDefault = "default"
	QueueImport  = "import"
)

// Job types handled in Release 1.
const (
	TypePincodeImport     = "geography.pincode_import"
	TypeZoneMappingImport = "geography.zone_mapping_import"
	TypeMaintenance       = "platform.maintenance"
)

// Job is a claimed unit of work handed to a Handler.
type Job struct {
	ID             int64
	PublicID       string
	OrganizationID *int64
	Queue          string
	Type           string
	Payload        []byte
	Attempts       int32
	MaxAttempts    int32
}

// Decode unmarshals the payload into v.
func (j Job) Decode(v any) error {
	if len(j.Payload) == 0 {
		return errors.New("jobs: empty payload")
	}
	return json.Unmarshal(j.Payload, v)
}

// Handler processes one job. Returning an error triggers retry with backoff
// until max_attempts, after which the job is dead-lettered.
type Handler func(ctx context.Context, job Job) error

// ErrPermanent marks a failure that must not be retried — bad input rather than
// a transient fault. The job is dead-lettered immediately.
var ErrPermanent = errors.New("jobs: permanent failure")

// retryAfterError lets a handler dictate when its own retry should happen,
// overriding the queue's generic exponential backoff.
//
// It exists because some work has a schedule the queue cannot know. A webhook
// to a partner whose server is down should back off further than a PDF render
// that hit a blip, and — more importantly — the delivery row records its own
// next_attempt_at for an operator to read. Without this, the row would say one
// thing and the queue would do another, which is worse than either alone.
type retryAfterError struct {
	after time.Duration
	cause error
}

func (e *retryAfterError) Error() string { return e.cause.Error() }
func (e *retryAfterError) Unwrap() error { return e.cause }

// RetryAfter wraps an error with the interval before the job should run again.
//
// A zero or negative interval falls back to the queue's own backoff.
func RetryAfter(after time.Duration, cause error) error {
	if cause == nil {
		return nil
	}
	if after <= 0 {
		return cause
	}
	return &retryAfterError{after: after, cause: cause}
}

// retryIntervalOf returns the handler-specified interval, if any.
func retryIntervalOf(err error) (time.Duration, bool) {
	var target *retryAfterError
	if errors.As(err, &target) {
		return target.after, true
	}
	return 0, false
}

// Enqueuer submits jobs.
type Enqueuer struct {
	q *dbgen.Queries
}

// NewEnqueuer builds an Enqueuer.
func NewEnqueuer(q *dbgen.Queries) *Enqueuer { return &Enqueuer{q: q} }

// EnqueueOptions tunes one submission.
type EnqueueOptions struct {
	Queue          string
	OrganizationID *int64
	Priority       int16
	RunAt          time.Time
	MaxAttempts    int32
	// DedupeKey suppresses a duplicate submission while an identical job is
	// still pending or running.
	DedupeKey string
	CreatedBy *int64
}

// Enqueue submits a job. It returns the job's public id, or "" when a
// deduplicated job already existed.
func (e *Enqueuer) Enqueue(ctx context.Context, jobType string, payload any, opts EnqueueOptions) (string, error) {
	return e.enqueue(ctx, e.q, jobType, payload, opts)
}

// EnqueueTx submits a job inside an existing transaction, so the job becomes
// visible only if the surrounding business change commits.
func (e *Enqueuer) EnqueueTx(ctx context.Context, tx pgx.Tx, jobType string, payload any, opts EnqueueOptions) (string, error) {
	return e.enqueue(ctx, e.q.WithTx(tx), jobType, payload, opts)
}

func (e *Enqueuer) enqueue(ctx context.Context, q *dbgen.Queries, jobType string, payload any, opts EnqueueOptions) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("jobs: marshal payload: %w", err)
	}
	if opts.Queue == "" {
		opts.Queue = QueueDefault
	}
	if opts.Priority == 0 {
		opts.Priority = 100
	}
	if opts.RunAt.IsZero() {
		opts.RunAt = time.Now()
	}
	if opts.MaxAttempts == 0 {
		opts.MaxAttempts = 5
	}
	params := dbgen.EnqueueJobParams{
		PublicID:       publicid.New(publicid.PrefixJob),
		OrganizationID: opts.OrganizationID,
		Queue:          opts.Queue,
		JobType:        jobType,
		Payload:        raw,
		Priority:       opts.Priority,
		RunAt:          opts.RunAt,
		MaxAttempts:    opts.MaxAttempts,
		CreatedBy:      opts.CreatedBy,
	}
	if opts.DedupeKey != "" {
		params.DedupeKey = &opts.DedupeKey
	}
	row, err := q.EnqueueJob(ctx, params)
	if err != nil {
		if database.IsNoRows(err) {
			return "", nil // deduplicated
		}
		return "", fmt.Errorf("jobs: enqueue: %w", err)
	}
	return row.PublicID, nil
}

// Worker claims and executes jobs.
type Worker struct {
	db          *database.DB
	q           *dbgen.Queries
	log         *slog.Logger
	metrics     *telemetry.Metrics
	handlers    map[string]Handler
	queues      []string
	concurrency int
	batchSize   int
	poll        time.Duration
	stuckAfter  time.Duration
	workerID    string
}

// WorkerConfig configures the runner.
type WorkerConfig struct {
	Queues       []string
	Concurrency  int
	BatchSize    int
	PollInterval time.Duration
	StuckAfter   time.Duration
	WorkerID     string
}

// NewWorker builds a Worker.
func NewWorker(db *database.DB, q *dbgen.Queries, cfg WorkerConfig, log *slog.Logger, m *telemetry.Metrics) *Worker {
	if cfg.WorkerID == "" {
		cfg.WorkerID = publicid.New("wrk")
	}
	if len(cfg.Queues) == 0 {
		cfg.Queues = []string{QueueDefault}
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = cfg.Concurrency
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.StuckAfter <= 0 {
		cfg.StuckAfter = 15 * time.Minute
	}
	return &Worker{
		db: db, q: q, log: log, metrics: m,
		handlers:    map[string]Handler{},
		queues:      cfg.Queues,
		concurrency: cfg.Concurrency,
		batchSize:   cfg.BatchSize,
		poll:        cfg.PollInterval,
		stuckAfter:  cfg.StuckAfter,
		workerID:    cfg.WorkerID,
	}
}

// Register binds a handler to a job type. Registering twice is a programming
// error and panics at startup rather than silently losing jobs.
func (w *Worker) Register(jobType string, h Handler) {
	if _, dup := w.handlers[jobType]; dup {
		panic("jobs: duplicate handler for " + jobType)
	}
	w.handlers[jobType] = h
}

// Run polls until ctx is cancelled, then waits for in-flight jobs to finish.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("worker started",
		slog.String("worker_id", w.workerID),
		slog.Any("queues", w.queues),
		slog.Int("concurrency", w.concurrency))

	sem := make(chan struct{}, w.concurrency)
	var wg sync.WaitGroup
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	maintenance := time.NewTicker(time.Minute)
	defer maintenance.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker draining in-flight jobs")
			wg.Wait()
			w.log.Info("worker stopped")
			return nil
		case <-maintenance.C:
			w.reclaimStuck(ctx)
			w.publishDepth(ctx)
		case <-ticker.C:
			claimed, err := w.claim(ctx)
			if err != nil {
				if ctx.Err() != nil {
					continue
				}
				w.log.Error("failed to claim jobs", slog.String("error", err.Error()))
				continue
			}
			for _, job := range claimed {
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					// Release the claim so another worker picks it up promptly.
					w.requeue(context.WithoutCancel(ctx), job, "worker shutting down")
					continue
				}
				wg.Add(1)
				go func(j Job) {
					defer wg.Done()
					defer func() { <-sem }()
					w.execute(ctx, j)
				}(job)
			}
		}
	}
}

func (w *Worker) claim(ctx context.Context) ([]Job, error) {
	rows, err := w.q.ClaimJobs(ctx, dbgen.ClaimJobsParams{
		Queues:   w.queues,
		MaxJobs:  int32(w.batchSize),
		WorkerID: &w.workerID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, Job{
			ID: r.ID, PublicID: r.PublicID, OrganizationID: r.OrganizationID,
			Queue: r.Queue, Type: r.JobType, Payload: r.Payload,
			Attempts: r.Attempts, MaxAttempts: r.MaxAttempts,
		})
	}
	return out, nil
}

func (w *Worker) execute(ctx context.Context, job Job) {
	log := w.log.With(
		slog.String("job_id", job.PublicID),
		slog.String("job_type", job.Type),
		slog.Int("attempt", int(job.Attempts)),
	)
	handler, ok := w.handlers[job.Type]
	if !ok {
		log.Error("no handler registered; dead-lettering job")
		w.fail(ctx, job, fmt.Errorf("no handler registered for %s", job.Type), true)
		return
	}

	if w.metrics != nil {
		w.metrics.JobsInFlight.Inc()
		defer w.metrics.JobsInFlight.Dec()
	}

	start := time.Now()
	// Jobs get their own context so a shutdown signal does not abort work
	// mid-transaction; the drain in Run() waits for them.
	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()

	err := func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("panic: %v", rec)
			}
		}()
		return handler(jobCtx, job)
	}()

	if err != nil {
		log.Error("job failed", slog.String("error", err.Error()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()))
		w.fail(jobCtx, job, err, errors.Is(err, ErrPermanent))
		if w.metrics != nil {
			w.metrics.JobsProcessed.WithLabelValues(job.Type, "failed").Inc()
		}
		return
	}
	if err := w.q.CompleteJob(jobCtx, job.ID); err != nil {
		log.Error("failed to mark job complete", slog.String("error", err.Error()))
	}
	log.Info("job completed", slog.Int64("duration_ms", time.Since(start).Milliseconds()))
	if w.metrics != nil {
		w.metrics.JobsProcessed.WithLabelValues(job.Type, "succeeded").Inc()
	}
}

// fail records the error and schedules a retry with exponential backoff, or
// dead-letters the job when attempts are exhausted or the error is permanent.
func (w *Worker) fail(ctx context.Context, job Job, cause error, permanent bool) {
	// A handler that knows its own schedule wins. Otherwise the queue's generic
	// exponential backoff applies.
	backoff := retryBackoff(job.Attempts)
	if d, ok := retryIntervalOf(cause); ok {
		backoff = d
	}
	attemptsLeft := job.MaxAttempts - job.Attempts
	if permanent {
		// Exhaust the attempt budget so RetryJob transitions straight to DEAD.
		attemptsLeft = 0
	}
	msg := truncate(cause.Error(), 2000)
	if attemptsLeft <= 0 {
		msg = "PERMANENT: " + msg
	}
	status, err := w.q.RetryJob(ctx, dbgen.RetryJobParams{
		JobID:          job.ID,
		BackoffSeconds: int32(backoff.Seconds()),
		LastError:      &msg,
	})
	if err != nil {
		w.log.Error("failed to record job failure", slog.String("error", err.Error()))
		return
	}
	if permanent && status != "DEAD" {
		// The row still had attempts left; force it dead so a permanent input
		// error is not retried four more times.
		deadMsg := msg
		for i := int32(0); i < attemptsMax && status != "DEAD"; i++ {
			status, err = w.q.RetryJob(ctx, dbgen.RetryJobParams{
				JobID: job.ID, BackoffSeconds: 0, LastError: &deadMsg,
			})
			if err != nil {
				return
			}
		}
	}
	if status == "DEAD" {
		w.log.Error("job dead-lettered",
			slog.String("job_id", job.PublicID),
			slog.String("job_type", job.Type),
			slog.String("error", msg))
	}
}

// attemptsMax bounds the forced-dead loop above; jobs.max_attempts is capped at
// 50 by a CHECK constraint.
const attemptsMax = 50

func (w *Worker) requeue(ctx context.Context, job Job, reason string) {
	msg := reason
	_, _ = w.q.RetryJob(ctx, dbgen.RetryJobParams{
		JobID: job.ID, BackoffSeconds: 0, LastError: &msg,
	})
}

func (w *Worker) reclaimStuck(ctx context.Context) {
	n, err := w.q.ReleaseStuckJobs(ctx, int32(w.stuckAfter.Seconds()))
	if err != nil {
		w.log.Warn("failed to reclaim stuck jobs", slog.String("error", err.Error()))
		return
	}
	if n > 0 {
		w.log.Warn("reclaimed stuck jobs", slog.Int64("count", n))
	}
}

func (w *Worker) publishDepth(ctx context.Context) {
	if w.metrics == nil {
		return
	}
	rows, err := w.q.CountJobsByQueue(ctx)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, r := range rows {
		w.metrics.JobQueueDepth.WithLabelValues(r.Queue).Set(float64(r.Pending))
		seen[r.Queue] = true
	}
	for _, q := range w.queues {
		if !seen[q] {
			w.metrics.JobQueueDepth.WithLabelValues(q).Set(0)
		}
	}
}

// retryBackoff grows exponentially and is capped, so a persistently failing
// integration backs off to a five-minute cadence instead of hammering.
func retryBackoff(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	secs := math.Pow(2, float64(attempt)) * 5
	if secs > 300 {
		secs = 300
	}
	return time.Duration(secs) * time.Second
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
