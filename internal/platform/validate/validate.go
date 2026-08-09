// Package validate provides request-payload validation primitives.
//
// Validation is explicit and accumulating: a request is checked in full and the
// client receives every field problem at once, in the standard envelope's
// details map, rather than one error per round trip.
package validate

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/publicid"
)

// Validator accumulates field errors.
type Validator struct {
	fields map[string]string
	order  []string
}

// New builds an empty Validator.
func New() *Validator { return &Validator{fields: map[string]string{}} }

// Add records a field error. The first error per field wins so the message is
// the most specific one the caller checked first.
func (v *Validator) Add(field, message string) {
	if _, exists := v.fields[field]; exists {
		return
	}
	v.fields[field] = message
	v.order = append(v.order, field)
}

// Addf records a formatted field error.
func (v *Validator) Addf(field, format string, args ...any) {
	v.Add(field, fmt.Sprintf(format, args...))
}

// Check records an error when cond is false.
func (v *Validator) Check(cond bool, field, message string) {
	if !cond {
		v.Add(field, message)
	}
}

// HasErrors reports whether any field failed.
func (v *Validator) HasErrors() bool { return len(v.fields) > 0 }

// Err returns a 422 apierr with the accumulated field errors, or nil.
func (v *Validator) Err() error {
	if !v.HasErrors() {
		return nil
	}
	details := make(map[string]any, len(v.fields)+1)
	fieldErrs := make([]map[string]string, 0, len(v.order))
	for _, f := range v.order {
		fieldErrs = append(fieldErrs, map[string]string{"field": f, "message": v.fields[f]})
	}
	details["fields"] = fieldErrs
	return apierr.Validation("One or more fields failed validation.", details)
}

// ---- field rules -----------------------------------------------------------

// Required checks that a string is non-empty after trimming.
func (v *Validator) Required(field, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		v.Add(field, "This field is required.")
	}
	return value
}

// Length bounds a string by rune count.
func (v *Validator) Length(field, value string, min, max int) {
	n := utf8.RuneCountInString(value)
	if n < min {
		v.Addf(field, "Must be at least %d characters.", min)
	}
	if n > max {
		v.Addf(field, "Must be at most %d characters.", max)
	}
}

// Email normalises and validates an email address, returning the lowercase form.
func (v *Validator) Email(field, value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		v.Add(field, "This field is required.")
		return value
	}
	if len(value) > 254 {
		v.Add(field, "Email address is too long.")
		return value
	}
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value || !strings.Contains(value, ".") {
		v.Add(field, "Must be a valid email address.")
	}
	return value
}

var codeRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,31}$`)

// Code validates an operator-assigned business code (branch code, service code,
// rate-card code). Codes are uppercased and constrained so they are safe in URLs,
// CSV exports and label barcodes.
func (v *Validator) Code(field, value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		v.Add(field, "This field is required.")
		return value
	}
	if !codeRe.MatchString(value) {
		v.Add(field, "Must be 2-32 characters using A-Z, 0-9, '-' and '_', starting with a letter or digit.")
	}
	return value
}

var phoneRe = regexp.MustCompile(`^\+?[0-9][0-9 \-]{5,19}$`)

// Phone validates a phone number, returning the digits-only form with an
// optional leading '+'.
func (v *Validator) Phone(field, value string, required bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			v.Add(field, "This field is required.")
		}
		return ""
	}
	if !phoneRe.MatchString(value) {
		v.Add(field, "Must be a valid phone number (6-20 digits, optional leading '+').")
		return value
	}
	var b strings.Builder
	for i, r := range value {
		if r == '+' && i == 0 {
			b.WriteRune(r)
		} else if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var pincodeRe = regexp.MustCompile(`^[1-9][0-9]{5}$`)

// Pincode validates a 6-digit postal code that does not start with zero.
//
// The same shape serves both markets the platform has run in: an Indian PIN
// code and a Nigerian postal code are each six digits with a non-zero lead
// (Lagos 100001, Abuja 900001). That is a coincidence rather than a design, so
// the message says "postal code" and not "PIN code" — a Nigerian sender told
// their address is not a valid PIN code learns nothing.
//
// Whether Nigerian postal codes are *usable* as the serviceability key is a
// separate question, open in docs/localisation-nigeria.md §4.
func (v *Validator) Pincode(field, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		v.Add(field, "This field is required.")
		return value
	}
	if !pincodeRe.MatchString(value) {
		v.Add(field, "Must be a 6-digit postal code not starting with 0.")
	}
	return value
}

// PublicID validates an opaque public identifier and its prefix.
func (v *Validator) PublicID(field, value, prefix string, required bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			v.Add(field, "This field is required.")
		}
		return ""
	}
	if !publicid.Valid(prefix, value) {
		v.Addf(field, "Must be a valid identifier beginning with %q.", prefix+"_")
	}
	return value
}

// Enum checks a value against an allowlist, returning the uppercased form.
func (v *Validator) Enum(field, value string, allowed []string, required bool) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		if required {
			v.Add(field, "This field is required.")
		}
		return ""
	}
	for _, a := range allowed {
		if value == a {
			return value
		}
	}
	v.Addf(field, "Must be one of: %s.", strings.Join(allowed, ", "))
	return value
}

// IntRange bounds an integer.
func (v *Validator) IntRange(field string, value, min, max int) {
	if value < min || value > max {
		v.Addf(field, "Must be between %d and %d.", min, max)
	}
}

// Int64Range bounds a 64-bit integer.
func (v *Validator) Int64Range(field string, value, min, max int64) {
	if value < min || value > max {
		v.Addf(field, "Must be between %d and %d.", min, max)
	}
}

// NonNegativeMinor validates a monetary amount in minor units.
func (v *Validator) NonNegativeMinor(field string, value int64) {
	if value < 0 {
		v.Add(field, "Must not be negative.")
	}
	if value > 1_000_000_000_000 { // ₹10 billion
		v.Add(field, "Exceeds the maximum permitted amount.")
	}
}

// BasisPoints validates a percentage expressed in basis points.
func (v *Validator) BasisPoints(field string, value int32) {
	if value < 0 || value > 1_000_000 { // 0% .. 10000%
		v.Add(field, "Must be between 0 and 1000000 basis points.")
	}
}

// Latitude validates a WGS84 latitude.
func (v *Validator) Latitude(field string, value *float64) {
	if value == nil {
		return
	}
	if *value < -90 || *value > 90 {
		v.Add(field, "Must be between -90 and 90.")
	}
}

// Longitude validates a WGS84 longitude.
func (v *Validator) Longitude(field string, value *float64) {
	if value == nil {
		return
	}
	if *value < -180 || *value > 180 {
		v.Add(field, "Must be between -180 and 180.")
	}
}

// EffectiveRange validates that an effective-from/to pair is ordered.
func (v *Validator) EffectiveRange(fromField, toField string, from, to *time.Time) {
	if from != nil && to != nil && !to.After(*from) {
		v.Add(toField, "Must be after "+fromField+".")
	}
}

var gstRe = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][0-9A-Z]Z[0-9A-Z]$`)
var panRe = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)

// GSTIN validates an Indian GST identification number.
func (v *Validator) GSTIN(field, value string, required bool) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		if required {
			v.Add(field, "This field is required.")
		}
		return ""
	}
	if !gstRe.MatchString(value) {
		v.Add(field, "Must be a valid 15-character GSTIN.")
	}
	return value
}

// PAN validates an Indian permanent account number.
func (v *Validator) PAN(field, value string, required bool) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		if required {
			v.Add(field, "This field is required.")
		}
		return ""
	}
	if !panRe.MatchString(value) {
		v.Add(field, "Must be a valid 10-character PAN.")
	}
	return value
}

// Text trims and bounds a free-text field, rejecting control characters that
// would corrupt logs, CSV exports or label rendering.
func (v *Validator) Text(field, value string, min, max int, required bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			v.Add(field, "This field is required.")
		}
		return ""
	}
	for _, r := range value {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			v.Add(field, "Must not contain control characters.")
			return value
		}
	}
	v.Length(field, value, min, max)
	return value
}

// Password enforces the credential policy: length plus at least three of the
// four character classes. Composition rules alone are weak, so the real
// protection remains Argon2id hashing plus login throttling; this rejects only
// the most obviously guessable inputs.
func (v *Validator) Password(field, value string, minLength int) {
	if value == "" {
		v.Add(field, "This field is required.")
		return
	}
	if utf8.RuneCountInString(value) < minLength {
		v.Addf(field, "Must be at least %d characters.", minLength)
		return
	}
	if len(value) > 256 {
		v.Add(field, "Must be at most 256 characters.")
		return
	}
	var lower, upper, digit, symbol bool
	for _, r := range value {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	classes := 0
	for _, ok := range []bool{lower, upper, digit, symbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		v.Add(field, "Must contain at least three of: lowercase, uppercase, digit, symbol.")
	}
}
