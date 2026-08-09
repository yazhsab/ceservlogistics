package ops

import (
	"net/http"
	"time"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// The Release 2 handlers all parse the same handful of things off a request.
// Collecting them here keeps twelve modules from each inventing their own
// slightly different notion of "the facility I am working at" or "how deep is
// this page".

// Context bundles what every operational handler needs from a request.
type Context struct {
	Principal *tenant.Principal
	Facility  *Facility
	Device    Device
}

// Request resolves the principal, working facility and device envelope.
//
// requireFacility is false for planning endpoints that do not touch a parcel;
// passing true makes the operating unit mandatory, which is what stops a scan
// being recorded with no idea where it happened.
func (r *Resolver) Request(req *http.Request, requireFacility bool) (*Context, error) {
	p, err := tenant.Require(req)
	if err != nil {
		return nil, err
	}
	out := &Context{Principal: p, Device: DeviceFrom(req)}

	unitID := httpx.Query(req, "operatingUnitId")
	if unitID == "" {
		unitID = req.Header.Get("X-Operating-Unit")
	}
	if requireFacility || unitID != "" {
		f, fErr := r.RequireFacility(req.Context(), p, unitID)
		if fErr != nil {
			return nil, fErr
		}
		out.Facility = f
	}
	return out, nil
}

// FacilityFor resolves a named operating unit from a decoded body field.
func (r *Resolver) FacilityFor(req *http.Request, p *tenant.Principal, publicID string) (*Facility, error) {
	return r.RequireFacility(req.Context(), p, publicID)
}

// Page reads the standard limit and cursor query parameters.
func Page(r *http.Request, defaultLimit int) (int, *pagination.Cursor, error) {
	limit, err := httpx.QueryInt(r, "limit", defaultLimit, 1, 100)
	if err != nil {
		return 0, nil, err
	}
	cursor, err := pagination.DecodeCursor(httpx.Query(r, "cursor"), "desc")
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

// ParseTime reads an optional RFC3339 timestamp.
func ParseTime(v *validate.Validator, field, raw string) *time.Time {
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		v.Add(field, "must be an RFC 3339 timestamp")
		return nil
	}
	return &t
}

// ParseDate reads a required YYYY-MM-DD date.
func ParseDate(v *validate.Validator, field, raw string, required bool) time.Time {
	if raw == "" {
		if required {
			v.Add(field, "is required")
		}
		return time.Time{}
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		v.Add(field, "must be a date in YYYY-MM-DD form")
		return time.Time{}
	}
	return d
}

// ValidBarcode validates a scannable code.
//
// The bound is generous enough for an AWB, a piece barcode and a bag tag, and
// tight enough that a client cannot use the field to push a large string into
// an indexed column.
func ValidBarcode(v *validate.Validator, field, raw string, required bool) string {
	value := trim(raw)
	if value == "" {
		if required {
			v.Add(field, "is required")
		}
		return ""
	}
	if len(value) < 4 || len(value) > 64 {
		v.Add(field, "must be between 4 and 64 characters")
		return ""
	}
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			v.Add(field, "contains characters that are not valid in a barcode")
			return ""
		}
	}
	return value
}

// Barcodes validates a batch of scannable codes.
func Barcodes(v *validate.Validator, field string, raw []string, max int) []string {
	if len(raw) == 0 {
		v.Add(field, "must contain at least one barcode")
		return nil
	}
	if len(raw) > max {
		v.Addf(field, "must contain at most %d barcodes", max)
		return nil
	}
	out := make([]string, 0, len(raw))
	for i, b := range raw {
		value := ValidBarcode(v, fieldIndex(field, i), b, true)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// RequireIdempotencyKey demands the header on an operation that must not run
// twice, when the client has not supplied a device event id instead.
func RequireIdempotencyKey(r *http.Request, d Device) (string, error) {
	key, err := httpx.IdempotencyKey(r, false)
	if err != nil {
		return "", err
	}
	if key != "" {
		return key, nil
	}
	if d.HasEventKey() {
		return "", nil
	}
	return "", apierr.New(http.StatusBadRequest, apierr.CodeIdempotencyKeyRequired,
		"Supply an Idempotency-Key header, or an X-Device-Id and X-Device-Event-Id pair, so a retry cannot repeat this operation.")
}

func fieldIndex(field string, i int) string {
	return field + "[" + itoa(i) + "]"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
