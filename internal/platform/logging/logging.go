// Package logging provides structured JSON logging with automatic redaction of
// sensitive values (Constitution §36).
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey int

const loggerKey ctxKey = iota

// Sensitive keys are replaced with a fixed marker wherever they appear in a log
// record, regardless of which code path produced them. This is a defence in
// depth measure: the primary control is simply never passing secrets to the
// logger.
var sensitiveKeys = map[string]struct{}{
	"password":               {},
	"password_hash":          {},
	"passwordhash":           {},
	"current_password":       {},
	"new_password":           {},
	"token":                  {},
	"access_token":           {},
	"accesstoken":            {},
	"refresh_token":          {},
	"refreshtoken":           {},
	"reset_token":            {},
	"authorization":          {},
	"cookie":                 {},
	"set-cookie":             {},
	"api_key":                {},
	"apikey":                 {},
	"api_secret":             {},
	"secret":                 {},
	"otp":                    {},
	"card_number":            {},
	"cvv":                    {},
	"jwt_secret":             {},
	"x-api-key":              {},
	"idempotency-key":        {},
	"proxy-authorization":    {},
	"webhook_signing_secret": {},
}

const redacted = "[REDACTED]"

// Config controls logger construction.
type Config struct {
	Level  string // debug|info|warn|error
	Format string // json|text
	Output io.Writer
}

// New builds the application logger.
func New(cfg Config) *slog.Logger {
	out := cfg.Output
	if out == nil {
		out = os.Stdout
	}
	var lvl slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl, ReplaceAttr: redact}
	var h slog.Handler
	if strings.EqualFold(cfg.Format, "text") {
		h = slog.NewTextHandler(out, opts)
	} else {
		h = slog.NewJSONHandler(out, opts)
	}
	return slog.New(h)
}

func redact(groups []string, a slog.Attr) slog.Attr {
	if _, bad := sensitiveKeys[strings.ToLower(a.Key)]; bad {
		return slog.String(a.Key, redacted)
	}
	return a
}

// WithContext stores a logger (typically request-scoped, with request_id and
// tenant attributes already bound) on the context.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the request-scoped logger, falling back to the default.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
