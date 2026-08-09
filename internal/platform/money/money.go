// Package money implements authoritative monetary arithmetic in integer minor
// units.
//
// Constitution §12: floats are never used for authoritative money. ₦125.50 is
// stored as 12550 (kobo); ₹125.50 likewise as paise. Every amount carries or
// inherits an explicit currency.
//
// Rounding policy for the whole platform is HALF_UP on the absolute value
// (i.e. away from zero at the .5 boundary), applied once per derived line item.
// It is the convention used by Nigerian VAT and by Indian courier billing and
// GST alike; it is exercised by TestRounding in money_test.go.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Currency is an ISO-4217 alphabetic code.
type Currency string

const (
	INR Currency = "INR"
	NGN Currency = "NGN"
	USD Currency = "USD"
)

// Exponent returns the number of minor units per major unit as a power of ten.
// All supported currencies happen to use two minor digits — paise, kobo,
// cents — but the switch stays explicit rather than returning 2 unconditionally.
// A zero-decimal currency (JPY) or a three-decimal one (KWD) would otherwise be
// silently mis-scaled by a factor of a hundred, and the default below exists
// only so an unsupported code has *some* answer for display. Anything that
// posts to the ledger goes through Supported first.
func (c Currency) Exponent() int {
	switch c {
	case INR, NGN, USD:
		return 2
	default:
		return 2
	}
}

// supported is the single source for what this platform handles. Both
// Currency.Supported and SupportedCodes read it, so a currency added here
// cannot be accepted by one and rejected by the other.
var supported = []Currency{INR, NGN, USD}

// Valid reports whether c is well-formed: three uppercase letters.
//
// This is a shape check, not a membership check. It deliberately accepts codes
// the platform does not yet support, so that adding a currency is a
// configuration change rather than a code change. Where an unknown currency
// would be a correctness problem rather than a presentation one — anything that
// posts to the ledger — use Supported instead.
func (c Currency) Valid() bool {
	if len(c) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if c[i] < 'A' || c[i] > 'Z' {
			return false
		}
	}
	return true
}

// Supported reports whether c is a currency this platform actually knows how to
// handle, meaning Exponent returns a real answer for it rather than a default.
//
// The ledger uses this rather than Valid: an unrecognised code would be stored,
// summed and reported as though it were money, and the resulting statement
// would be quietly wrong rather than loudly rejected.
func (c Currency) Supported() bool {
	for _, known := range supported {
		if c == known {
			return true
		}
	}
	return false
}

// SupportedCodes returns the handled currencies as strings, for error messages
// that tell a caller what to use instead.
//
// It copies rather than exposing the slice: a caller appending to it would
// quietly widen what the ledger accepts.
func SupportedCodes() []string {
	out := make([]string, 0, len(supported))
	for _, c := range supported {
		out = append(out, string(c))
	}
	return out
}

// ErrCurrencyMismatch is returned when two amounts of different currencies are
// combined.
var ErrCurrencyMismatch = errors.New("money: currency mismatch")

// Amount is a monetary value in minor units with an explicit currency.
type Amount struct {
	Minor    int64
	Currency Currency
}

// New builds an Amount.
func New(minor int64, c Currency) Amount { return Amount{Minor: minor, Currency: c} }

// Zero returns a zero amount in currency c.
func Zero(c Currency) Amount { return Amount{Minor: 0, Currency: c} }

// Add returns a+b. It returns an error if currencies differ.
func (a Amount) Add(b Amount) (Amount, error) {
	if a.Currency != b.Currency {
		return Amount{}, fmt.Errorf("%w: %s + %s", ErrCurrencyMismatch, a.Currency, b.Currency)
	}
	return Amount{Minor: a.Minor + b.Minor, Currency: a.Currency}, nil
}

// Sub returns a-b. It returns an error if currencies differ.
func (a Amount) Sub(b Amount) (Amount, error) {
	if a.Currency != b.Currency {
		return Amount{}, fmt.Errorf("%w: %s - %s", ErrCurrencyMismatch, a.Currency, b.Currency)
	}
	return Amount{Minor: a.Minor - b.Minor, Currency: a.Currency}, nil
}

// String renders the amount as a decimal string with the currency code, e.g.
// "INR 125.50". Intended for logs and human-readable documents, never for
// arithmetic.
func (a Amount) String() string {
	return string(a.Currency) + " " + FormatMinor(a.Minor, a.Currency.Exponent())
}

// BasisPoints is a percentage expressed in hundredths of a percent.
// 18% == 1800 bp, 18.5% == 1850 bp. Integer basis points keep every percentage
// rule exactly representable, which floats cannot guarantee.
type BasisPoints int32

// Percent builds BasisPoints from a whole percentage.
func Percent(p int) BasisPoints { return BasisPoints(p * 100) }

// Float returns the percentage as a float. Presentation only — never used in
// authoritative arithmetic.
func (bp BasisPoints) Float() float64 { return float64(bp) / 100.0 }

// ApplyBP multiplies base (minor units) by bp basis points and rounds HALF_UP
// away from zero.
//
//	ApplyBP(10000, 1850) == 1850   (18.5% of ₹100.00 is ₹18.50)
//	ApplyBP(333, 1000)   == 33     (10% of 3.33 is 0.333 -> 33 paise? no: 0.333 -> 33)
//
// The multiplication is performed in int64; callers must keep base within
// sane courier-billing magnitudes (well under 9.2e18/10000 ≈ 9.2e14 minor units,
// i.e. ₹9.2 billion), which RoundedMulBP enforces.
func ApplyBP(base int64, bp BasisPoints) int64 {
	return divRoundHalfUp(mulSaturating(base, int64(bp)), 10000)
}

// ApplyPerUnit multiplies a per-unit rate by a unit count.
func ApplyPerUnit(ratePerUnit int64, units int64) int64 {
	return mulSaturating(ratePerUnit, units)
}

// DivRoundHalfUp exposes the platform rounding rule for callers that compute
// their own numerator/denominator (e.g. proportional allocation).
func DivRoundHalfUp(numerator, denominator int64) int64 {
	return divRoundHalfUp(numerator, denominator)
}

// divRoundHalfUp performs integer division rounding half away from zero.
func divRoundHalfUp(n, d int64) int64 {
	if d == 0 {
		panic("money: division by zero")
	}
	neg := (n < 0) != (d < 0)
	an, ad := abs64(n), abs64(d)
	q := an / ad
	r := an % ad
	if r*2 >= ad {
		q++
	}
	if neg {
		return -q
	}
	return q
}

// mulSaturating multiplies and panics on overflow rather than silently wrapping.
// A wrapped monetary value is a correctness catastrophe; failing loudly during a
// transaction that will be rolled back is strictly safer.
func mulSaturating(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	p := a * b
	if p/b != a {
		panic(fmt.Sprintf("money: multiplication overflow (%d * %d)", a, b))
	}
	return p
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Clamp bounds v to [min,max]. A nil bound (represented by ok=false) is ignored.
func Clamp(v int64, min *int64, max *int64) int64 {
	if min != nil && v < *min {
		v = *min
	}
	if max != nil && v > *max {
		v = *max
	}
	return v
}

// FormatMinor renders minor units as a fixed-point decimal string with the given
// exponent. FormatMinor(12550, 2) == "125.50".
func FormatMinor(minor int64, exponent int) string {
	if exponent <= 0 {
		return strconv.FormatInt(minor, 10)
	}
	neg := minor < 0
	v := abs64(minor)
	div := int64(1)
	for i := 0; i < exponent; i++ {
		div *= 10
	}
	major := v / div
	frac := v % div
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(strconv.FormatInt(major, 10))
	b.WriteByte('.')
	fs := strconv.FormatInt(frac, 10)
	for i := len(fs); i < exponent; i++ {
		b.WriteByte('0')
	}
	b.WriteString(fs)
	return b.String()
}

// ParseMinor parses a decimal string such as "125.50" into minor units using the
// given exponent. It rejects anything that is not an exact representation, so
// that API inputs can never silently lose precision.
func ParseMinor(s string, exponent int) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("money: empty amount")
	}
	neg := false
	switch s[0] {
	case '-':
		neg, s = true, s[1:]
	case '+':
		s = s[1:]
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" && !hasFrac {
		return 0, errors.New("money: invalid amount")
	}
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > exponent {
		return 0, fmt.Errorf("money: amount %q has more than %d decimal places", s, exponent)
	}
	for len(fracPart) < exponent {
		fracPart += "0"
	}
	digits := intPart + fracPart
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, fmt.Errorf("money: invalid amount %q", s)
		}
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: amount out of range: %w", err)
	}
	if neg {
		v = -v
	}
	return v, nil
}
