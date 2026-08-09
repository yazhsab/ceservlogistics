package validate

import "testing"

// A market's identifiers must be accepted in that market and refused in the
// wrong one. The failure this prevents is quiet: a Nigerian TIN entered into a
// field validated as an Indian GSTIN is rejected with advice that makes no
// sense to the person reading it.

func TestTaxRegistrationPerCountry(t *testing.T) {
	for _, tc := range []struct {
		name, country, value string
		valid                bool
	}{
		{"indian gstin", "IN", "29ABCDE1234F1Z5", true},
		{"indian gstin, lowercase and spaced", "IN", "29abcde1234f1z5", true},
		{"indian gstin too short", "IN", "29ABCDE1234F1Z", false},
		{"nigerian tin", "NG", "12345678", true},
		{"nigerian tin, hyphenated", "NG", "12345678-0001", true},
		{"nigerian tin with letters", "NG", "ABC12345", false},
		// A GSTIN is not a TIN and must not pass as one.
		{"gstin offered as a nigerian tin", "NG", "29ABCDE1234F1Z5", false},
		// An unknown market is accepted on shape alone rather than blocked.
		{"unknown market", "GH", "TIN-99887766", true},
		{"unknown market, junk", "GH", "!!", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			v.TaxRegistration("taxId", tc.value, tc.country, true)
			if got := v.Err() == nil; got != tc.valid {
				t.Fatalf("valid = %v, want %v (err: %v)", got, tc.valid, v.Err())
			}
		})
	}
}

func TestBusinessRegistrationPerCountry(t *testing.T) {
	for _, tc := range []struct {
		name, country, value string
		valid                bool
	}{
		{"indian pan", "IN", "ABCDE1234F", true},
		{"indian pan malformed", "IN", "ABCD1234F", false},
		{"nigerian rc with prefix", "NG", "RC1234567", true},
		{"nigerian rc without prefix", "NG", "1234567", true},
		{"nigerian business name", "NG", "BN123456", true},
		{"nigerian rc too short", "NG", "12", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			v.BusinessRegistration("regNo", tc.value, tc.country, true)
			if got := v.Err() == nil; got != tc.valid {
				t.Fatalf("valid = %v, want %v (err: %v)", got, tc.valid, v.Err())
			}
		})
	}
}

func TestTaxIDsAreNormalised(t *testing.T) {
	v := New()
	// People copy these off certificates with spaces in.
	got := v.TaxRegistration("taxId", " 29 abcde 1234 f1z5 ", "IN", true)
	if err := v.Err(); err != nil {
		t.Fatalf("a spaced GSTIN was refused: %v", err)
	}
	if got != "29ABCDE1234F1Z5" {
		t.Fatalf("normalised to %q", got)
	}
}

func TestLabelsMatchTheMarket(t *testing.T) {
	// The label is what a UI shows and what the error says. Getting it wrong is
	// how a Nigerian operator is asked for a GSTIN.
	for _, tc := range []struct{ country, tax, business string }{
		{"IN", "GSTIN", "PAN"},
		{"NG", "TIN", "RC number"},
		{"GH", "tax registration number", "business registration number"},
	} {
		if got := TaxRegistrationLabel(tc.country); got != tc.tax {
			t.Errorf("%s tax label = %q, want %q", tc.country, got, tc.tax)
		}
		if got := BusinessRegistrationLabel(tc.country); got != tc.business {
			t.Errorf("%s business label = %q, want %q", tc.country, got, tc.business)
		}
	}
}

func TestOptionalTaxIDsAreAllowedToBeEmpty(t *testing.T) {
	v := New()
	v.TaxRegistration("taxId", "", "NG", false)
	v.BusinessRegistration("regNo", "", "NG", false)
	if err := v.Err(); err != nil {
		t.Fatalf("empty optional identifiers were refused: %v", err)
	}
}
