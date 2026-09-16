package validate

import (
	"regexp"
	"strings"
)

var internationalPostalCode = regexp.MustCompile(`^[0-9A-Z][0-9A-Z -]{2,11}$`)
var ukPostalCode = regexp.MustCompile(`^(GIR 0AA|[A-Z]{1,2}[0-9][0-9A-Z]? [0-9][A-Z]{2})$`)

// PostalCode keeps postal identifiers as strings. Country-aware validation is
// shared by quote, geography and booking; existence/serviceability is checked
// separately against configured reference data.
func (v *Validator) PostalCode(field, value, country string) string {
	value = strings.ToUpper(strings.Join(strings.Fields(value), " "))
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "NG" || country == "IN" {
		return v.Pincode(field, value)
	}
	if country == "GB" {
		compact := strings.ReplaceAll(value, " ", "")
		if len(compact) >= 5 && len(compact) <= 7 {
			value = compact[:len(compact)-3] + " " + compact[len(compact)-3:]
		}
		if !ukPostalCode.MatchString(value) {
			v.Add(field, "Enter a valid UK postcode, for example SS0 7JJ.")
		}
	} else if !internationalPostalCode.MatchString(value) {
		v.Add(field, "Enter a postal code of 3–12 letters, digits, spaces or hyphens.")
	}
	return value
}

// AddressCountry resolves an omitted country consistently at every boundary.
func AddressCountry(value, organizationCountry string) string {
	if strings.TrimSpace(value) == "" {
		value = organizationCountry
	}
	if strings.TrimSpace(value) == "" {
		value = "NG"
	}
	return strings.ToUpper(strings.TrimSpace(value))
}
