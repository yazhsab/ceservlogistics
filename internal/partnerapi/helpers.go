package partnerapi

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ceserve/courier-os/internal/platform/validate"
)

// parseDate reads a required YYYY-MM-DD field.
func parseDate(v *validate.Validator, field, raw string) time.Time {
	if raw == "" {
		v.Add(field, "is required")
		return time.Time{}
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		v.Add(field, "must be a date in YYYY-MM-DD form")
		return time.Time{}
	}
	return d
}

// parseTime reads a required RFC 3339 timestamp.
//
// Returns a usable zero value on failure so the caller can keep collecting
// validation errors instead of stopping at the first one — a partner debugging
// an integration should see every problem with a payload at once.
func parseTime(v *validate.Validator, field, raw string) *time.Time {
	if raw == "" {
		v.Add(field, "is required")
		return &time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		v.Add(field, "must be an RFC 3339 timestamp")
		return &time.Time{}
	}
	return &t
}

func fieldIdx(field string, i int) string { return fmt.Sprintf("%s[%d]", field, i) }

// rawJSON passes stored JSON through to a response without re-encoding it.
func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}
