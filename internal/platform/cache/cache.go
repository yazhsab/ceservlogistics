// Package cache wraps Redis/Valkey.
//
// Constitution §29: Redis is never the system of record. Every method degrades
// gracefully — a cache miss and a cache outage are indistinguishable to callers,
// who always have a PostgreSQL fallback path.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceserve/courier-os/internal/platform/config"
)

// Cache is the platform cache client.
type Cache struct {
	rdb     *redis.Client
	prefix  string
	log     *slog.Logger
	healthy atomic.Bool
}

// ErrMiss signals a cache miss.
var ErrMiss = errors.New("cache: miss")

// Open connects to Redis. When cfg.Required is false a connection failure is
// logged and the cache degrades to always-miss instead of blocking startup.
func Open(ctx context.Context, cfg config.RedisConfig, log *slog.Logger) (*Cache, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opts.PoolSize = cfg.PoolSize
	opts.MinIdleConns = cfg.MinIdleConns
	opts.DialTimeout = cfg.DialTimeout
	opts.ReadTimeout = cfg.ReadTimeout
	opts.WriteTimeout = cfg.WriteTimeout

	c := &Cache{rdb: redis.NewClient(opts), prefix: cfg.KeyPrefix, log: log}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout+time.Second)
	defer cancel()
	if err := c.rdb.Ping(pingCtx).Err(); err != nil {
		if cfg.Required {
			_ = c.rdb.Close()
			return nil, fmt.Errorf("ping redis: %w", err)
		}
		log.Warn("redis unavailable at startup; continuing in degraded (always-miss) mode",
			slog.String("error", err.Error()))
		return c, nil
	}
	c.healthy.Store(true)
	log.Info("redis ready", slog.Int("pool_size", cfg.PoolSize))
	return c, nil
}

// Close releases the client.
func (c *Cache) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

// Client exposes the underlying client for components that need Lua scripting
// (the rate limiter). Callers must apply Key() themselves.
func (c *Cache) Client() *redis.Client { return c.rdb }

// Key namespaces a cache key with the configured prefix.
func (c *Cache) Key(parts ...string) string {
	k := c.prefix
	for i, p := range parts {
		if i > 0 {
			k += ":"
		}
		k += p
	}
	return k
}

// Ping reports cache health for the readiness probe.
func (c *Cache) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return errors.New("cache: not configured")
	}
	err := c.rdb.Ping(ctx).Err()
	c.healthy.Store(err == nil)
	return err
}

// Healthy reports the last observed health state without issuing a round trip.
func (c *Cache) Healthy() bool { return c != nil && c.healthy.Load() }

func (c *Cache) note(op, key string, err error) {
	if err == nil {
		c.healthy.Store(true)
		return
	}
	if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
		return
	}
	c.healthy.Store(false)
	c.log.Warn("cache operation failed; continuing without cache",
		slog.String("op", op), slog.String("key", key), slog.String("error", err.Error()))
}

// GetJSON decodes a cached JSON value into dst. It returns ErrMiss on miss,
// on decode failure, and on cache outage.
func (c *Cache) GetJSON(ctx context.Context, key string, dst any) error {
	if c == nil || c.rdb == nil {
		return ErrMiss
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		c.note("get", key, err)
		return ErrMiss
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		// A poisoned entry (e.g. after a struct change) must not break requests.
		c.log.Warn("discarding undecodable cache entry", slog.String("key", key))
		_ = c.rdb.Del(ctx, key).Err()
		return ErrMiss
	}
	return nil
}

// SetJSON stores a JSON value with a TTL. Failures are logged, never returned:
// a cache write failure must not fail a business operation.
func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		c.log.Error("cache marshal failed", slog.String("key", key), slog.String("error", err.Error()))
		return
	}
	c.note("set", key, c.rdb.Set(ctx, key, raw, ttl).Err())
}

// Delete removes keys. Used by explicit cache invalidation on configuration
// changes (Constitution §20).
func (c *Cache) Delete(ctx context.Context, keys ...string) {
	if c == nil || c.rdb == nil || len(keys) == 0 {
		return
	}
	c.note("del", keys[0], c.rdb.Del(ctx, keys...).Err())
}

// DeletePrefix removes every key under a prefix using SCAN in bounded batches.
// SCAN (never KEYS) keeps invalidation non-blocking on a shared Redis.
func (c *Cache) DeletePrefix(ctx context.Context, prefix string) {
	if c == nil || c.rdb == nil {
		return
	}
	var cursor uint64
	const batch = 500
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, prefix+"*", batch).Result()
		if err != nil {
			c.note("scan", prefix, err)
			return
		}
		if len(keys) > 0 {
			c.note("del", prefix, c.rdb.Del(ctx, keys...).Err())
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

// Incr atomically increments a counter and applies a TTL on first use. Used for
// login throttling. It returns (0,false) when the cache is unavailable so the
// caller can decide its fail-open/fail-closed policy explicitly.
func (c *Cache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, bool) {
	if c == nil || c.rdb == nil {
		return 0, false
	}
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		c.note("incr", key, err)
		return 0, false
	}
	return incr.Val(), true
}

// TTL returns the remaining lifetime of a key.
func (c *Cache) TTL(ctx context.Context, key string) (time.Duration, bool) {
	if c == nil || c.rdb == nil {
		return 0, false
	}
	d, err := c.rdb.TTL(ctx, key).Result()
	if err != nil {
		c.note("ttl", key, err)
		return 0, false
	}
	return d, true
}
