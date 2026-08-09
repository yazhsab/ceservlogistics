package database

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

// Slow-query observability.
//
// # Why this exists
//
// A 4 vCPU host with a bounded pool has very little room between "a query got
// slower" and "the pool is exhausted and the API is down". The gap between
// those two states is measured in one bad query, and by the time it shows up as
// a latency alert on an HTTP route the cause is already several layers away.
//
// pgx exposes a QueryTracer hook, so every statement can be timed for the price
// of one time.Since per query. What is *recorded* is deliberately cheap:
//
//   - A histogram labelled by operation only — SELECT, INSERT, UPDATE, DELETE.
//     Labelling by statement text would give a Prometheus series per query in
//     the codebase, which on this host is a memory problem rather than an
//     insight.
//   - A log line only past a threshold, so the steady state writes nothing.
//
// # What is deliberately not logged
//
// Query *arguments* never appear. They are addresses, phone numbers, names and
// COD amounts; a slow-query log that carries them turns a debugging aid into a
// personal-data spill that outlives the incident. The statement text alone
// identifies the query, and the code has one place per query to look it up.

// QueryTracer times every statement, records a histogram, and logs the slow
// ones. A nil tracer is safe and does nothing.
type QueryTracer struct {
	log       *slog.Logger
	threshold time.Duration
	duration  *prometheus.HistogramVec
	slowTotal *prometheus.CounterVec
}

// SlowQueryMetrics is the subset of the metric set this tracer writes to,
// declared as an interface-free struct so the database package does not depend
// on telemetry (telemetry already depends on nothing, and a cycle here would
// force one of them to move).
type SlowQueryMetrics struct {
	Duration  *prometheus.HistogramVec
	SlowTotal *prometheus.CounterVec
}

// NewQueryTracer builds a tracer. A threshold of zero disables slow logging but
// keeps the histogram, which is the right default for a busy production host:
// the percentile is always useful, the log line is only useful when something
// is wrong.
func NewQueryTracer(log *slog.Logger, threshold time.Duration, m SlowQueryMetrics) *QueryTracer {
	return &QueryTracer{log: log, threshold: threshold, duration: m.Duration, slowTotal: m.SlowTotal}
}

type traceKey struct{}

type traceData struct {
	start time.Time
	sql   string
}

// TraceQueryStart records the start time on the context pgx threads through.
func (t *QueryTracer) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if t == nil {
		return ctx
	}
	return context.WithValue(ctx, traceKey{}, &traceData{start: time.Now(), sql: data.SQL})
}

// TraceQueryEnd observes the duration and logs if it crossed the threshold.
func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if t == nil {
		return
	}
	td, ok := ctx.Value(traceKey{}).(*traceData)
	if !ok || td == nil {
		return
	}
	elapsed := time.Since(td.start)
	op := operationOf(td.sql)

	if t.duration != nil {
		t.duration.WithLabelValues(op).Observe(elapsed.Seconds())
	}
	if t.threshold <= 0 || elapsed < t.threshold {
		return
	}
	if t.slowTotal != nil {
		t.slowTotal.WithLabelValues(op).Inc()
	}
	if t.log == nil {
		return
	}
	attrs := []any{
		slog.String("operation", op),
		slog.Int64("duration_ms", elapsed.Milliseconds()),
		slog.String("sql", summarise(td.sql)),
	}
	if data.Err != nil {
		attrs = append(attrs, slog.String("error", data.Err.Error()))
	}
	// Warn rather than Error: a slow query is a signal, not yet a failure, and
	// an alert that fires on every slow read trains people to ignore it.
	t.log.LogAttrs(ctx, slog.LevelWarn, "slow query", toAttrs(attrs)...)
}

func toAttrs(vals []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(vals))
	for _, v := range vals {
		if a, ok := v.(slog.Attr); ok {
			out = append(out, a)
		}
	}
	return out
}

// operationOf extracts the leading verb. Low cardinality is the whole point:
// this is a metric label.
func operationOf(sql string) string {
	s := strings.TrimSpace(sql)
	// Skip leading sqlc/-- comments, which every generated query carries.
	for strings.HasPrefix(s, "--") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
			continue
		}
		break
	}
	if i := strings.IndexAny(s, " \t\n("); i > 0 {
		s = s[:i]
	}
	switch verb := strings.ToUpper(s); verb {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "COPY", "BEGIN", "COMMIT", "ROLLBACK":
		return verb
	default:
		return "OTHER"
	}
}

// summarise collapses a statement to one bounded line. Arguments are never
// included; see the package comment.
func summarise(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	const max = 300
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
