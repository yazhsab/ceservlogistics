package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/logging"
	"github.com/ceserve/courier-os/internal/platform/publicid"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxClientIP
	ctxRoutePattern
)

// HeaderRequestID is the correlation header echoed on every response.
const (
	HeaderRequestID      = "X-Request-Id"
	HeaderIdempotencyKey = "Idempotency-Key"
	HeaderOrgContext     = "X-Organization-Context"
)

// RequestID returns the correlation ID bound to the request context.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxRequestID).(string)
	return v
}

// ClientIP returns the resolved client IP.
func ClientIP(ctx context.Context) string {
	v, _ := ctx.Value(ctxClientIP).(string)
	return v
}

// RoutePattern returns the chi route pattern, used for low-cardinality metrics.
func RoutePattern(ctx context.Context) string {
	v, _ := ctx.Value(ctxRoutePattern).(string)
	return v
}

// WithRoutePattern records the matched route pattern.
func WithRoutePattern(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, ctxRoutePattern, pattern)
}

// RequestContext assigns a correlation ID, resolves the client IP against the
// trusted-proxy allowlist, and binds a request-scoped logger.
//
// An inbound X-Request-Id is honoured only if it looks like an ID we would have
// generated; otherwise a fresh one is minted. That prevents a caller from
// injecting newlines or unbounded strings into every log line downstream.
func RequestContext(trustedProxies []string) func(http.Handler) http.Handler {
	nets := parseCIDRs(trustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := sanitiseRequestID(r.Header.Get(HeaderRequestID))
			if reqID == "" {
				reqID = publicid.New("req")
			}
			ip := clientIP(r, nets)

			ctx := context.WithValue(r.Context(), ctxRequestID, reqID)
			ctx = context.WithValue(ctx, ctxClientIP, ip)
			ctx = logging.WithContext(ctx, logging.FromContext(ctx).With(
				slog.String("request_id", reqID),
			))
			w.Header().Set(HeaderRequestID, reqID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func sanitiseRequestID(v string) string {
	if v == "" || len(v) > 64 {
		return ""
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return ""
		}
	}
	return v
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(strings.TrimSpace(c)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// clientIP trusts X-Forwarded-For only when the immediate peer is a configured
// proxy. Rate limiting and login throttling key on this value, so an untrusted
// header would be a trivial bypass.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	peerHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peerHost = r.RemoteAddr
	}
	peer := net.ParseIP(peerHost)
	if peer == nil {
		return peerHost
	}
	isTrusted := false
	for _, n := range trusted {
		if n.Contains(peer) {
			isTrusted = true
			break
		}
	}
	if !isTrusted {
		return peer.String()
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Right-most entry that is not itself a trusted proxy is the client.
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue
			}
			trustedHop := false
			for _, n := range trusted {
				if n.Contains(ip) {
					trustedHop = true
					break
				}
			}
			if !trustedHop {
				return ip.String()
			}
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xr != "" {
		if ip := net.ParseIP(xr); ip != nil {
			return ip.String()
		}
	}
	return peer.String()
}

// statusWriter records the response status and size for access logging.
type statusWriter struct {
	http.ResponseWriter
	status  int
	written int64
	wrote   bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.status = code
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.written += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// AccessLog emits one structured line per request.
type AccessLogHook func(r *http.Request, status int, duration time.Duration)

// AccessLog logs method, route, status, latency and size. Health and metrics
// endpoints are logged at debug level so probe traffic does not dominate logs.
func AccessLog(hook AccessLogHook) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			dur := time.Since(start)

			log := logging.FromContext(r.Context())
			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("route", RoutePattern(r.Context())),
				slog.Int("status", sw.status),
				slog.Int64("bytes", sw.written),
				slog.Int64("duration_ms", dur.Milliseconds()),
				slog.String("client_ip", ClientIP(r.Context())),
			}
			switch {
			case isProbePath(r.URL.Path):
				log.Debug("request", attrs...)
			case sw.status >= 500:
				log.Error("request", attrs...)
			case sw.status >= 400:
				log.Warn("request", attrs...)
			default:
				log.Info("request", attrs...)
			}
			if hook != nil {
				hook(r, sw.status, dur)
			}
		})
	}
}

func isProbePath(p string) bool {
	return p == "/healthz" || p == "/readyz" || p == "/livez" || p == "/metrics"
}

// Recoverer converts a panic into a redacted 500 and keeps the process alive.
// The stack trace is logged, never returned.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				logging.FromContext(r.Context()).Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				WriteError(w, r, apierr.Internal(fmt.Errorf("panic: %v", rec)))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets conservative response headers. The API serves JSON only,
// so the CSP simply forbids everything.
func SecurityHeaders(cfg config.SecurityConfig) func(http.Handler) http.Handler {
	hsts := "max-age=" + strconv.FormatInt(int64(cfg.HSTSMaxAge.Seconds()), 10) + "; includeSubDomains"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=(), payment=()")
			h.Set("Cache-Control", "no-store")
			if cfg.HSTSEnabled {
				h.Set("Strict-Transport-Security", hsts)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS applies a strict origin allowlist. There is no reflection of arbitrary
// origins: an origin is either configured or the CORS headers are omitted, in
// which case the browser blocks the response.
func CORS(cfg config.HTTPConfig) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.CORSOrigins))
	wildcard := false
	for _, o := range cfg.CORSOrigins {
		if o == "*" {
			wildcard = true
			continue
		}
		allowed[strings.ToLower(strings.TrimRight(o, "/"))] = struct{}{}
	}
	allowHeaders := strings.Join([]string{
		"Accept", "Authorization", "Content-Type", HeaderRequestID, HeaderIdempotencyKey, HeaderOrgContext,
	}, ", ")
	exposeHeaders := strings.Join([]string{
		HeaderRequestID, "Location", "Retry-After", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
	}, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				_, ok := allowed[strings.ToLower(strings.TrimRight(origin, "/"))]
				switch {
				case ok:
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
					if cfg.CORSAllowCreds {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				case wildcard:
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", allowHeaders)
				h.Set("Access-Control-Max-Age", "600")
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if origin != "" {
				w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit caps request body size. Uploads use a separate, larger limit
// applied by the upload handler itself.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds handler execution. The handler's context is cancelled, which
// propagates to pgx and releases the pooled connection.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			done := make(chan struct{})
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			var once sync.Once
			go func() {
				defer func() {
					if rec := recover(); rec != nil {
						logging.FromContext(ctx).Error("panic in timed handler", slog.Any("panic", rec),
							slog.String("stack", string(debug.Stack())))
						once.Do(func() { WriteError(sw, r, apierr.Internal(fmt.Errorf("panic: %v", rec))) })
					}
					close(done)
				}()
				next.ServeHTTP(sw, r.WithContext(ctx))
			}()

			select {
			case <-done:
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					once.Do(func() {
						WriteError(sw, r, apierr.New(http.StatusGatewayTimeout, apierr.CodeUnavailable,
							"The request took too long to process."))
					})
				}
				// Wait for the handler goroutine so it cannot write after the
				// response is finished.
				<-done
			}
		})
	}
}

// NotFoundHandler renders unmatched routes through the standard envelope.
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, apierr.New(http.StatusNotFound, apierr.CodeNotFound,
			"The requested endpoint does not exist."))
	}
}

// MethodNotAllowedHandler renders 405 through the standard envelope.
func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, apierr.New(http.StatusMethodNotAllowed, apierr.CodeMethodNotAllowed,
			"The HTTP method is not supported for this endpoint."))
	}
}
