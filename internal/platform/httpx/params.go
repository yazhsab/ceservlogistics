package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
)

// PathPublicID extracts a URL path parameter and validates its public-ID shape
// and prefix before it reaches any repository. A malformed identifier is a 404,
// not a 400, so probing for the existence of an ID space yields nothing.
func PathPublicID(r *http.Request, name, prefix, resource string) (string, error) {
	raw := chi.URLParam(r, name)
	if !publicid.Valid(prefix, raw) {
		return "", apierr.NotFound(resource)
	}
	return raw, nil
}

// PathParam returns a raw path parameter bounded to maxLen.
func PathParam(r *http.Request, name string, maxLen int) (string, error) {
	v := chi.URLParam(r, name)
	if v == "" {
		return "", apierr.BadRequest("Path parameter " + name + " is required.")
	}
	if len(v) > maxLen {
		return "", apierr.BadRequest("Path parameter " + name + " is too long.")
	}
	return v, nil
}

// Query reads a trimmed query parameter.
func Query(r *http.Request, name string) string {
	return strings.TrimSpace(r.URL.Query().Get(name))
}

// QueryDefault reads a query parameter with a fallback.
func QueryDefault(r *http.Request, name, def string) string {
	if v := Query(r, name); v != "" {
		return v
	}
	return def
}

// QueryInt parses an integer query parameter, clamped to [min,max].
func QueryInt(r *http.Request, name string, def, min, max int) (int, error) {
	raw := Query(r, name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apierr.Validation("Query parameter must be an integer.", map[string]any{"parameter": name})
	}
	if n < min || n > max {
		return 0, apierr.Validation("Query parameter is out of range.", map[string]any{
			"parameter": name, "min": min, "max": max,
		})
	}
	return n, nil
}

// QueryBool parses a boolean query parameter.
func QueryBool(r *http.Request, name string, def bool) (bool, error) {
	raw := Query(r, name)
	if raw == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, apierr.Validation("Query parameter must be true or false.", map[string]any{"parameter": name})
	}
	return b, nil
}

// QueryTime parses an RFC3339 timestamp query parameter.
func QueryTime(r *http.Request, name string) (*time.Time, error) {
	raw := Query(r, name)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// Accept a bare date for convenience on filter parameters.
		if d, derr := time.Parse("2006-01-02", raw); derr == nil {
			return &d, nil
		}
		return nil, apierr.Validation("Query parameter must be an RFC3339 timestamp or YYYY-MM-DD date.",
			map[string]any{"parameter": name})
	}
	return &t, nil
}

// QueryEnum validates a query parameter against an allowlist.
func QueryEnum(r *http.Request, name string, allowed []string) (string, error) {
	raw := strings.ToUpper(Query(r, name))
	if raw == "" {
		return "", nil
	}
	for _, a := range allowed {
		if raw == a {
			return raw, nil
		}
	}
	return "", apierr.Validation("Query parameter has an unsupported value.", map[string]any{
		"parameter": name, "allowed": allowed,
	})
}

// QueryEnumList parses a comma-separated enum list against an allowlist,
// bounded in length so a caller cannot force an unbounded IN () list.
func QueryEnumList(r *http.Request, name string, allowed []string, maxItems int) ([]string, error) {
	raw := Query(r, name)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxItems {
		return nil, apierr.Validation("Too many values supplied for query parameter.", map[string]any{
			"parameter": name, "maxItems": maxItems,
		})
	}
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		v := strings.ToUpper(strings.TrimSpace(p))
		if v == "" {
			continue
		}
		ok := false
		for _, a := range allowed {
			if v == a {
				ok = true
				break
			}
		}
		if !ok {
			return nil, apierr.Validation("Query parameter has an unsupported value.", map[string]any{
				"parameter": name, "value": v, "allowed": allowed,
			})
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}

// QueryPublicID validates an optional public-ID filter parameter.
func QueryPublicID(r *http.Request, name, prefix string) (string, error) {
	raw := Query(r, name)
	if raw == "" {
		return "", nil
	}
	if !publicid.Valid(prefix, raw) {
		return "", apierr.Validation("Query parameter is not a valid identifier.", map[string]any{
			"parameter": name, "expectedPrefix": prefix,
		})
	}
	return raw, nil
}

// IdempotencyKey extracts and validates the Idempotency-Key header.
func IdempotencyKey(r *http.Request, required bool) (string, error) {
	key := strings.TrimSpace(r.Header.Get(HeaderIdempotencyKey))
	if key == "" {
		if required {
			return "", apierr.New(http.StatusBadRequest, apierr.CodeIdempotencyKeyRequired,
				"The Idempotency-Key header is required for this operation.")
		}
		return "", nil
	}
	if len(key) < 8 || len(key) > 128 {
		return "", apierr.Validation("Idempotency-Key must be between 8 and 128 characters.", nil)
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == ':' || c == '.'
		if !ok {
			return "", apierr.Validation("Idempotency-Key may only contain letters, digits, '-', '_', ':' and '.'.", nil)
		}
	}
	return key, nil
}
