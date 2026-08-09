package validate

import (
	"regexp"
	"strings"
)

// Country-aware validation for the two identifiers a business counterparty is
// invoiced under.
//
// # Why this replaces GSTIN and PAN
//
// Those are Indian names for a general idea: the tax registration a
// counterparty is billed against, and the registration that proves the company
// exists. Every market has both under different names and different shapes.
// A Nigerian operator asked for a "GSTIN" will either leave it blank or put
// their TIN in a field validated against an Indian format, and the request will
// be rejected for a reason that makes no sense to them.
//
// The old GSTIN and PAN validators remain for callers that genuinely mean the
// Indian identifiers. New code should use TaxRegistration and
// BusinessRegistration with the organization's country.
//
// # Unknown countries are accepted, not guessed
//
// A country with no rule here gets a length and character check only. Refusing
// an identifier because the platform has not learned its format yet would
// block a market for no safety benefit; accepting it means the field is stored
// as typed and can be validated properly when somebody adds the rule.

var (
	// India: 2-digit state code, 10-character PAN, entity digit, 'Z', checksum.
	gstinRe = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)
	panOnly = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)

	// Nigeria: the FIRS Taxpayer Identification Number. Commonly shown as
	// 8 digits, a hyphen and 4 more (12345678-0001); the hyphen is stripped
	// before checking, so either form is accepted.
	ngTINRe = regexp.MustCompile(`^[0-9]{8,14}$`)
	// Nigeria: the CAC registration, historically "RC" followed by digits for a
	// company and "BN" for a business name. The prefix is optional because it
	// is written inconsistently.
	ngRCRe = regexp.MustCompile(`^(RC|BN)?[0-9]{4,10}$`)

	// The fallback for a market with no rule yet.
	genericTaxIDRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9\-/]{3,29}$`)
)

// TaxRegistrationLabel names the tax identifier in the caller's market, for
// error messages and for a UI that should not say "GSTIN" to a Nigerian.
func TaxRegistrationLabel(country string) string {
	switch strings.ToUpper(country) {
	case "IN":
		return "GSTIN"
	case "NG":
		return "TIN"
	default:
		return "tax registration number"
	}
}

// BusinessRegistrationLabel does the same for the company registration.
func BusinessRegistrationLabel(country string) string {
	switch strings.ToUpper(country) {
	case "IN":
		return "PAN"
	case "NG":
		return "RC number"
	default:
		return "business registration number"
	}
}

// TaxRegistration validates the identifier a counterparty is invoiced under.
func (v *Validator) TaxRegistration(field, value, country string, required bool) string {
	value = normaliseID(value)
	if value == "" {
		if required {
			v.Addf(field, "A %s is required.", TaxRegistrationLabel(country))
		}
		return ""
	}

	ok := false
	switch strings.ToUpper(country) {
	case "IN":
		ok = gstinRe.MatchString(value)
	case "NG":
		// The hyphen in 12345678-0001 is presentation, not data.
		ok = ngTINRe.MatchString(strings.ReplaceAll(value, "-", ""))
	default:
		ok = genericTaxIDRe.MatchString(value)
	}
	if !ok {
		v.Addf(field, "Must be a valid %s.", TaxRegistrationLabel(country))
	}
	return value
}

// BusinessRegistration validates the company registration number.
func (v *Validator) BusinessRegistration(field, value, country string, required bool) string {
	value = normaliseID(value)
	if value == "" {
		if required {
			v.Addf(field, "A %s is required.", BusinessRegistrationLabel(country))
		}
		return ""
	}

	ok := false
	switch strings.ToUpper(country) {
	case "IN":
		ok = panOnly.MatchString(value)
	case "NG":
		ok = ngRCRe.MatchString(value)
	default:
		ok = genericTaxIDRe.MatchString(value)
	}
	if !ok {
		v.Addf(field, "Must be a valid %s.", BusinessRegistrationLabel(country))
	}
	return value
}

// normaliseID upper-cases and strips the spaces people put in registration
// numbers when copying them off a certificate.
func normaliseID(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(value), ""))
}
