// Package pagination implements the platform's two pagination modes.
//
// Constitution §27: every list endpoint paginates and high-volume tables use
// keyset (cursor) pagination. Offset pagination is retained only for small,
// bounded configuration tables where operators legitimately want page numbers.
//
// Cursors are opaque base64 payloads. They are not encrypted — they carry no
// secret — but they are validated strictly and always re-scoped to the caller's
// tenant on the server, so a forged cursor cannot cross a tenant boundary.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/platform/apierr"
)

// Limits.
const (
	DefaultLimit = 25
	MaxLimit     = 100
)

// Cursor is the decoded keyset position. Sorting is always on a
// (sort value, id) pair so the key is unique and the scan is stable even when
// many rows share a timestamp.
type Cursor struct {
	// Time is the primary sort value for time-ordered listings.
	Time *time.Time `json:"t,omitempty"`
	// Text is the primary sort value for lexicographically ordered listings.
	Text string `json:"s,omitempty"`
	// ID is the tiebreaker: the internal row id.
	ID int64 `json:"i"`
	// Dir records the direction the cursor was minted for, so a cursor from an
	// ascending listing cannot be replayed against a descending one.
	Dir string `json:"d"`
}

// Encode renders a cursor as an opaque string.
func (c Cursor) Encode() string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses an opaque cursor. An empty string yields a nil cursor.
func DecodeCursor(s, expectedDir string) (*Cursor, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) > 512 {
		return nil, apierr.Validation("Cursor is not valid.", map[string]any{"parameter": "cursor"})
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, apierr.Validation("Cursor is not valid.", map[string]any{"parameter": "cursor"})
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, apierr.Validation("Cursor is not valid.", map[string]any{"parameter": "cursor"})
	}
	if c.ID < 0 {
		return nil, apierr.Validation("Cursor is not valid.", map[string]any{"parameter": "cursor"})
	}
	if expectedDir != "" && c.Dir != expectedDir {
		return nil, apierr.Validation("Cursor does not match the requested sort order.",
			map[string]any{"parameter": "cursor"})
	}
	return &c, nil
}

// CursorPage is the envelope for keyset-paginated responses.
type CursorPage[T any] struct {
	Data       []T            `json:"data"`
	Pagination CursorMetadata `json:"pagination"`
}

// CursorMetadata describes the position within a keyset listing.
type CursorMetadata struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
	Sort       string `json:"sort"`
	Order      string `json:"order"`
}

// NewCursorPage builds a keyset page. Callers fetch limit+1 rows; this trims the
// probe row and derives hasMore.
func NewCursorPage[T any](items []T, limit int, sort, order string, next func(T) Cursor) CursorPage[T] {
	page := CursorPage[T]{Data: items, Pagination: CursorMetadata{Limit: limit, Sort: sort, Order: order}}
	if len(items) > limit {
		page.Data = items[:limit]
		page.Pagination.HasMore = true
		page.Pagination.NextCursor = next(page.Data[limit-1]).Encode()
	}
	if page.Data == nil {
		page.Data = []T{}
	}
	return page
}

// OffsetPage is the envelope for offset-paginated responses.
type OffsetPage[T any] struct {
	Data       []T            `json:"data"`
	Pagination OffsetMetadata `json:"pagination"`
}

// OffsetMetadata describes the position within an offset listing.
type OffsetMetadata struct {
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
	TotalItems int64  `json:"totalItems"`
	TotalPages int    `json:"totalPages"`
	HasMore    bool   `json:"hasMore"`
	Sort       string `json:"sort"`
	Order      string `json:"order"`
}

// NewOffsetPage builds an offset page.
func NewOffsetPage[T any](items []T, page, pageSize int, total int64, sort, order string) OffsetPage[T] {
	if items == nil {
		items = []T{}
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return OffsetPage[T]{
		Data: items,
		Pagination: OffsetMetadata{
			Page:       page,
			PageSize:   pageSize,
			TotalItems: total,
			TotalPages: totalPages,
			HasMore:    int64(page*pageSize) < total,
			Sort:       sort,
			Order:      order,
		},
	}
}

// SortSpec is a validated sort instruction.
type SortSpec struct {
	Field string
	Desc  bool
}

// Order returns "ASC" or "DESC".
func (s SortSpec) Order() string {
	if s.Desc {
		return "DESC"
	}
	return "ASC"
}

// ParseSort validates a `sort` query value such as "createdAt:desc" against an
// allowlist mapping API field names to a canonical internal key.
//
// The returned Field is always one of the allowlist values, never caller text,
// so it can be interpolated into ORDER BY without risking injection.
func ParseSort(raw string, allowed map[string]string, defaultField string, defaultDesc bool) (SortSpec, error) {
	if raw == "" {
		return SortSpec{Field: defaultField, Desc: defaultDesc}, nil
	}
	name, dir, _ := strings.Cut(raw, ":")
	canonical, ok := allowed[strings.TrimSpace(name)]
	if !ok {
		keys := make([]string, 0, len(allowed))
		for k := range allowed {
			keys = append(keys, k)
		}
		return SortSpec{}, apierr.Validation("Unsupported sort field.", map[string]any{
			"parameter": "sort", "allowed": keys,
		})
	}
	desc := defaultDesc
	switch strings.ToLower(strings.TrimSpace(dir)) {
	case "asc":
		desc = false
	case "desc":
		desc = true
	case "":
	default:
		return SortSpec{}, apierr.Validation("Sort direction must be asc or desc.", map[string]any{"parameter": "sort"})
	}
	return SortSpec{Field: canonical, Desc: desc}, nil
}

// ClampLimit bounds a requested page size.
func ClampLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apierr.Validation("limit must be an integer.", map[string]any{"parameter": "limit"})
	}
	if n < 1 || n > MaxLimit {
		return 0, apierr.Validation("limit must be between 1 and 100.", map[string]any{
			"parameter": "limit", "min": 1, "max": MaxLimit,
		})
	}
	return n, nil
}
