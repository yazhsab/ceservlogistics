// Package telemetry wires OpenTelemetry tracing and Prometheus metrics.
//
// Observability stays deliberately light on the initial VPS (Constitution §36):
// tracing is off by default and sampled at 5% when enabled; metrics are a
// handful of low-cardinality series scraped from a loopback-bound listener.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/ceserve/courier-os/internal/platform/config"
)

// Provider owns telemetry lifecycle.
type Provider struct {
	tracer   *sdktrace.TracerProvider
	registry *prometheus.Registry
	Metrics  *Metrics
	log      *slog.Logger
}

// Setup initialises tracing and metrics.
func Setup(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Provider, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	m := newMetrics(reg)
	p := &Provider{registry: reg, Metrics: m, log: log}

	if !cfg.Telemetry.Enabled {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		return p, nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.Telemetry.OTLPEndpoint),
		func() otlptracehttp.Option {
			if cfg.Telemetry.OTLPInsecure {
				return otlptracehttp.WithInsecure()
			}
			return otlptracehttp.WithCompression(otlptracehttp.GzipCompression)
		}(),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}
	res, err := sdkresource.Merge(sdkresource.Default(), sdkresource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.Service.Name),
		semconv.ServiceVersion(cfg.Service.Version),
		attribute.String("deployment.environment", cfg.Env),
	))
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithMaxQueueSize(2048), sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Telemetry.SampleRatio))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	p.tracer = tp
	log.Info("tracing enabled", slog.String("endpoint", cfg.Telemetry.OTLPEndpoint),
		slog.Float64("sample_ratio", cfg.Telemetry.SampleRatio))
	return p, nil
}

// Shutdown flushes pending spans.
func (p *Provider) Shutdown(ctx context.Context) {
	if p == nil || p.tracer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.tracer.Shutdown(ctx); err != nil {
		p.log.Warn("tracer shutdown failed", slog.String("error", err.Error()))
	}
}

// MetricsHandler serves the Prometheus scrape endpoint.
func (p *Provider) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		ErrorHandling:       promhttp.ContinueOnError,
		MaxRequestsInFlight: 3,
	})
}

// Tracer returns a named tracer.
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }

// Metrics holds the platform metric set.
type Metrics struct {
	HTTPRequests   *prometheus.CounterVec
	HTTPDuration   *prometheus.HistogramVec
	DBPoolConns    *prometheus.GaugeVec
	JobsProcessed  *prometheus.CounterVec
	JobsInFlight   prometheus.Gauge
	JobQueueDepth  *prometheus.GaugeVec
	BusinessEvents *prometheus.CounterVec
	CacheOps       *prometheus.CounterVec

	// BuildInfo is the always-1 gauge that carries the running version as
	// labels. Without it a dashboard cannot answer "which build produced this
	// spike", which is the first question asked in every incident.
	BuildInfo *prometheus.GaugeVec
	// DependencyUp is 1 or 0 per external dependency. A counter of errors tells
	// you something broke; this tells you whether it is broken *now*, which is
	// what an alert needs.
	DependencyUp *prometheus.GaugeVec
	// QueryDuration is labelled by SQL verb only — see database.QueryTracer for
	// why the statement text is not a label.
	QueryDuration *prometheus.HistogramVec
	SlowQueries   *prometheus.CounterVec
}

func newMetrics(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "courier", Subsystem: "http", Name: "requests_total",
			Help: "Total HTTP requests by route, method and status class.",
		}, []string{"route", "method", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "courier", Subsystem: "http", Name: "request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"route", "method"}),
		DBPoolConns: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "courier", Subsystem: "db", Name: "pool_connections",
			Help: "PostgreSQL pool connections by state.",
		}, []string{"state"}),
		JobsProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "courier", Subsystem: "jobs", Name: "processed_total",
			Help: "Background jobs processed by type and outcome.",
		}, []string{"job_type", "outcome"}),
		JobsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "courier", Subsystem: "jobs", Name: "in_flight",
			Help: "Background jobs currently executing.",
		}),
		JobQueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "courier", Subsystem: "jobs", Name: "queue_depth",
			Help: "Pending background jobs by queue.",
		}, []string{"queue"}),
		BusinessEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "courier", Subsystem: "business", Name: "events_total",
			Help: "Business events by type and outcome (bookings, quotes, logins).",
		}, []string{"event", "outcome"}),
		CacheOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "courier", Subsystem: "cache", Name: "operations_total",
			Help: "Cache operations by name and outcome.",
		}, []string{"name", "outcome"}),
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "courier", Subsystem: "app", Name: "build_info",
			Help: "Always 1. Labels carry the running version, commit and build time.",
		}, []string{"version", "commit", "built_at", "go_version"}),
		DependencyUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "courier", Subsystem: "app", Name: "dependency_up",
			Help: "1 when an external dependency answered its last health check, 0 otherwise.",
		}, []string{"dependency"}),
		QueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "courier", Subsystem: "db", Name: "query_duration_seconds",
			Help: "Database statement latency by SQL verb.",
			// Tighter at the bottom than the HTTP histogram: a query that takes
			// 250 ms on this host is already a problem, so the resolution needs
			// to be where the decisions are.
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"operation"}),
		SlowQueries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "courier", Subsystem: "db", Name: "slow_queries_total",
			Help: "Statements that exceeded DB_SLOW_QUERY_THRESHOLD, by SQL verb.",
		}, []string{"operation"}),
	}
	reg.MustRegister(m.HTTPRequests, m.HTTPDuration, m.DBPoolConns, m.JobsProcessed,
		m.JobsInFlight, m.JobQueueDepth, m.BusinessEvents, m.CacheOps,
		m.BuildInfo, m.DependencyUp, m.QueryDuration, m.SlowQueries)
	return m
}

// SetBuildInfo publishes the running build. Called once at startup.
func (m *Metrics) SetBuildInfo(version, commit, builtAt string) {
	if m == nil {
		return
	}
	m.BuildInfo.WithLabelValues(
		orUnknown(version), orUnknown(commit), orUnknown(builtAt), runtime.Version(),
	).Set(1)
}

// SetDependencyUp records the result of a health check.
func (m *Metrics) SetDependencyUp(dependency string, up bool) {
	if m == nil {
		return
	}
	v := 0.0
	if up {
		v = 1
	}
	m.DependencyUp.WithLabelValues(dependency).Set(v)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// ObserveHTTP records one request. The status label is a class ("2xx") rather
// than an exact code to keep series count low.
func (m *Metrics) ObserveHTTP(route, method string, status int, d time.Duration) {
	if m == nil {
		return
	}
	if route == "" {
		route = "unmatched"
	}
	m.HTTPRequests.WithLabelValues(route, method, statusClass(status)).Inc()
	m.HTTPDuration.WithLabelValues(route, method).Observe(d.Seconds())
}

func statusClass(status int) string {
	switch {
	case status < 200:
		return "1xx"
	case status < 300:
		return "2xx"
	case status < 400:
		return "3xx"
	case status < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// RecordBusiness increments a business counter.
func (m *Metrics) RecordBusiness(event, outcome string) {
	if m == nil {
		return
	}
	m.BusinessEvents.WithLabelValues(event, outcome).Inc()
}

// RecordCache increments a cache counter.
func (m *Metrics) RecordCache(name, outcome string) {
	if m == nil {
		return
	}
	m.CacheOps.WithLabelValues(name, outcome).Inc()
}

// PoolStatsSource is satisfied by database.DB.
type PoolStatsSource interface {
	Stats() PoolStatsSnapshot
}

// PoolStatsSnapshot mirrors database.PoolStats without importing it, keeping
// telemetry free of a dependency on the database package.
type PoolStatsSnapshot struct {
	Acquired int32
	Idle     int32
	Total    int32
	Max      int32
}

// SetPoolStats publishes pool utilisation.
func (m *Metrics) SetPoolStats(s PoolStatsSnapshot) {
	if m == nil {
		return
	}
	m.DBPoolConns.WithLabelValues("acquired").Set(float64(s.Acquired))
	m.DBPoolConns.WithLabelValues("idle").Set(float64(s.Idle))
	m.DBPoolConns.WithLabelValues("total").Set(float64(s.Total))
	m.DBPoolConns.WithLabelValues("max").Set(float64(s.Max))
}
