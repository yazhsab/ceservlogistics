package security

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/httpx"
)

// slidingWindowScript implements an approximate sliding window counter.
//
// The current and previous fixed windows are combined by weighting the previous
// window by the fraction of it still inside the sliding period. This costs two
// counters per key instead of a sorted set per key, which matters when Redis is
// sharing 16 GB with PostgreSQL, and it removes the burst-at-window-boundary
// flaw of a plain fixed window.
//
// KEYS[1] current window counter, KEYS[2] previous window counter
// ARGV[1] limit, ARGV[2] window seconds, ARGV[3] elapsed fraction (0..1) x 1000
// Returns {allowed, weightedCount}
const slidingWindowScript = `
local cur = tonumber(redis.call('GET', KEYS[1]) or '0')
local prev = tonumber(redis.call('GET', KEYS[2]) or '0')
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local elapsed = tonumber(ARGV[3]) / 1000.0
local weighted = prev * (1.0 - elapsed) + cur
if weighted >= limit then
  return {0, math.floor(weighted)}
end
cur = redis.call('INCR', KEYS[1])
if cur == 1 then
  redis.call('EXPIRE', KEYS[1], window * 2)
end
return {1, math.floor(weighted + 1)}
`

// Limiter enforces request rate limits.
type Limiter struct {
	cache  *cache.Cache
	script *redis.Script
	limit  int
	window time.Duration
	log    *slog.Logger

	// local is the fallback limiter used when Redis is unavailable. It is
	// per-process and therefore weaker than the shared limiter, but failing
	// open entirely would remove brute-force protection exactly when the
	// platform is already degraded.
	local *localLimiter
}

// NewLimiter builds a rate limiter.
func NewLimiter(c *cache.Cache, perMinute int, log *slog.Logger) *Limiter {
	if perMinute < 1 {
		perMinute = 1
	}
	return &Limiter{
		cache:  c,
		script: redis.NewScript(slidingWindowScript),
		limit:  perMinute,
		window: time.Minute,
		log:    log,
		local:  newLocalLimiter(perMinute, time.Minute),
	}
}

// Result describes a limiter decision.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// Allow consumes one unit for key, using an explicit limit override when limit
// is greater than zero.
func (l *Limiter) Allow(ctx context.Context, key string, limit int) Result {
	if limit <= 0 {
		limit = l.limit
	}
	if l.cache == nil || l.cache.Client() == nil {
		return l.local.allow(key, limit)
	}
	now := time.Now()
	windowSecs := int64(l.window.Seconds())
	curWindow := now.Unix() / windowSecs
	elapsed := int64(float64(now.Unix()%windowSecs) / float64(windowSecs) * 1000)

	curKey := l.cache.Key("rl", key, strconv.FormatInt(curWindow, 10))
	prevKey := l.cache.Key("rl", key, strconv.FormatInt(curWindow-1, 10))

	res, err := l.script.Run(ctx, l.cache.Client(), []string{curKey, prevKey},
		limit, windowSecs, elapsed).Slice()
	if err != nil {
		l.log.Warn("rate limiter degraded to in-process counters", slog.String("error", err.Error()))
		return l.local.allow(key, limit)
	}
	allowed, _ := res[0].(int64)
	count, _ := res[1].(int64)
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	return Result{
		Allowed:    allowed == 1,
		Limit:      limit,
		Remaining:  remaining,
		RetryAfter: time.Duration(windowSecs-now.Unix()%windowSecs) * time.Second,
	}
}

// Middleware applies a per-client rate limit. Authenticated requests are keyed
// by session where available (set by the auth middleware via KeyFunc); anonymous
// requests are keyed by resolved client IP.
func (l *Limiter) Middleware(enabled bool, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := ""
			if keyFunc != nil {
				key = keyFunc(r)
			}
			if key == "" {
				key = "ip:" + httpx.ClientIP(r.Context())
			}
			res := l.Allow(r.Context(), key, 0)
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(int64(res.RetryAfter.Seconds()), 10))
			if !res.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(res.RetryAfter.Seconds()), 10))
				httpx.WriteError(w, r, apierr.RateLimited("Too many requests. Please retry shortly."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// localLimiter is a bounded in-process fixed-window fallback.
type localLimiter struct {
	mu      sync.Mutex
	counts  map[string]*localEntry
	limit   int
	window  time.Duration
	maxKeys int
}

type localEntry struct {
	count   int
	resetAt time.Time
}

func newLocalLimiter(limit int, window time.Duration) *localLimiter {
	return &localLimiter{counts: map[string]*localEntry{}, limit: limit, window: window, maxKeys: 50_000}
}

func (l *localLimiter) allow(key string, limit int) Result {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Opportunistic sweep keeps the map bounded without a background goroutine.
	if len(l.counts) > l.maxKeys {
		for k, e := range l.counts {
			if now.After(e.resetAt) {
				delete(l.counts, k)
			}
		}
		if len(l.counts) > l.maxKeys {
			l.counts = map[string]*localEntry{}
		}
	}

	e, ok := l.counts[key]
	if !ok || now.After(e.resetAt) {
		e = &localEntry{resetAt: now.Add(l.window)}
		l.counts[key] = e
	}
	if e.count >= limit {
		return Result{Allowed: false, Limit: limit, Remaining: 0, RetryAfter: time.Until(e.resetAt)}
	}
	e.count++
	return Result{Allowed: true, Limit: limit, Remaining: limit - e.count, RetryAfter: time.Until(e.resetAt)}
}
