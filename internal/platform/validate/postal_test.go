package validate

import "testing"

func TestCountryAwarePostalCodes(t *testing.T) {
	for _, tc := range []struct {
		country, input, want string
		valid                bool
	}{
		{"NG", "100001", "100001", true}, {"NG", "SS0 7JJ", "SS0 7JJ", false},
		{"IN", "012345", "012345", false}, {"GB", "ss07jj", "SS0 7JJ", true},
		{"GB", " SW1A  1AA ", "SW1A 1AA", true}, {"GB", "100001", "100 001", false},
		{"GB", "GIR0AA", "GIR 0AA", true}, {"US", "00501", "00501", true},
		{"US", "", "", false}, {"US", "<script>", "<SCRIPT>", false},
	} {
		t.Run(tc.country+tc.input, func(t *testing.T) {
			v := New()
			got := v.PostalCode("postalCode", tc.input, tc.country)
			if got != tc.want || (v.Err() == nil) != tc.valid {
				t.Fatalf("got %q, error %v", got, v.Err())
			}
		})
	}
}
