package money

import "testing"

// TestApplyBPRounding pins the platform rounding rule: HALF_UP away from zero,
// applied once per derived amount. These are the exact cases that appear on a
// courier invoice, so a change here is a change to what customers are charged.
func TestApplyBPRounding(t *testing.T) {
	cases := []struct {
		name string
		base int64
		bp   BasisPoints
		want int64
	}{
		{"18.5% of 75.00 rounds .5 up", 7500, 1850, 1388}, // 1387.5 -> 1388
		{"18% of 88.88 rounds .84 up", 8888, 1800, 1600},  // 1599.84 -> 1600
		{"9% of 88.88 rounds .992 up", 8888, 900, 800},    // 799.92 -> 800
		{"exact multiple is unchanged", 10000, 1800, 1800},
		{"zero base", 0, 1800, 0},
		{"zero rate", 12345, 0, 0},
		{"1% of 1 rounds to zero", 1, 100, 0},        // 0.01 -> 0
		{"50% of 1 rounds half up to 1", 1, 5000, 1}, // 0.5 -> 1
		{"49% of 1 rounds down to zero", 1, 4900, 0}, // 0.49 -> 0
		{"negative base rounds away from zero", -7500, 1850, -1388},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyBP(tc.base, tc.bp); got != tc.want {
				t.Errorf("ApplyBP(%d, %d) = %d, want %d", tc.base, tc.bp, got, tc.want)
			}
		})
	}
}

func TestDivRoundHalfUp(t *testing.T) {
	cases := []struct{ n, d, want int64 }{
		{10, 4, 3},   // 2.5 -> 3
		{-10, 4, -3}, // -2.5 -> -3
		{10, -4, -3},
		{9, 4, 2},  // 2.25 -> 2
		{11, 4, 3}, // 2.75 -> 3
		{0, 7, 0},
		{7, 7, 1},
	}
	for _, tc := range cases {
		if got := DivRoundHalfUp(tc.n, tc.d); got != tc.want {
			t.Errorf("DivRoundHalfUp(%d, %d) = %d, want %d", tc.n, tc.d, got, tc.want)
		}
	}
}

func TestDivRoundHalfUpPanicsOnZeroDivisor(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on division by zero")
		}
	}()
	DivRoundHalfUp(1, 0)
}

// TestMultiplicationOverflowPanics: a wrapped monetary value would be a silent
// catastrophe, so overflow fails loudly inside a transaction that will roll
// back rather than persisting a wrong number.
func TestMultiplicationOverflowPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on multiplication overflow")
		}
	}()
	ApplyPerUnit(1<<62, 8)
}

func TestFormatMinor(t *testing.T) {
	cases := []struct {
		minor    int64
		exponent int
		want     string
	}{
		{12550, 2, "125.50"},
		{5, 2, "0.05"},
		{0, 2, "0.00"},
		{-12550, 2, "-125.50"},
		{100000, 2, "1000.00"},
		{1500, 3, "1.500"},
		{999, 0, "999"},
	}
	for _, tc := range cases {
		if got := FormatMinor(tc.minor, tc.exponent); got != tc.want {
			t.Errorf("FormatMinor(%d, %d) = %q, want %q", tc.minor, tc.exponent, got, tc.want)
		}
	}
}

// TestParseMinorRejectsPrecisionLoss: an API input with more decimals than the
// currency supports must be rejected, never silently truncated.
func TestParseMinorRejectsPrecisionLoss(t *testing.T) {
	valid := map[string]int64{
		"125.50": 12550, "125.5": 12550, "125": 12500,
		"0.01": 1, "-125.50": -12550, "+7.25": 725,
	}
	for in, want := range valid {
		got, err := ParseMinor(in, 2)
		if err != nil {
			t.Errorf("ParseMinor(%q) returned an error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMinor(%q) = %d, want %d", in, got, want)
		}
	}
	for _, in := range []string{"125.505", "", "abc", "12.3.4", "1,000.00"} {
		if _, err := ParseMinor(in, 2); err == nil {
			t.Errorf("ParseMinor(%q) should have failed", in)
		}
	}
}

func TestAmountArithmeticRejectsCurrencyMismatch(t *testing.T) {
	inr := New(1000, INR)
	usd := New(1000, USD)
	if _, err := inr.Add(usd); err == nil {
		t.Error("adding different currencies must fail")
	}
	if _, err := inr.Sub(usd); err == nil {
		t.Error("subtracting different currencies must fail")
	}
	sum, err := inr.Add(New(500, INR))
	if err != nil || sum.Minor != 1500 {
		t.Errorf("Add = %v, %v; want 1500 INR", sum, err)
	}
}

func TestClamp(t *testing.T) {
	min, max := int64(100), int64(500)
	cases := []struct {
		v        int64
		min, max *int64
		want     int64
	}{
		{50, &min, &max, 100},
		{600, &min, &max, 500},
		{300, &min, &max, 300},
		{50, nil, &max, 50},
		{600, &min, nil, 600},
		{600, nil, nil, 600},
	}
	for _, tc := range cases {
		if got := Clamp(tc.v, tc.min, tc.max); got != tc.want {
			t.Errorf("Clamp(%d) = %d, want %d", tc.v, got, tc.want)
		}
	}
}

// TestSupportedCurrenciesAgree pins the two views of the same list together.
//
// Currency.Supported gates the ledger; SupportedCodes populates the error that
// tells a caller what to use instead. A currency in one and not the other means
// either a rejected payment with unhelpful advice, or advice to use a code that
// will be refused.
func TestSupportedCurrenciesAgree(t *testing.T) {
	codes := SupportedCodes()
	if len(codes) == 0 {
		t.Fatal("no currencies are supported")
	}
	for _, code := range codes {
		if !Currency(code).Supported() {
			t.Errorf("%s is advertised but not accepted", code)
		}
		if Currency(code).Exponent() != 2 {
			t.Errorf("%s has exponent %d; check the money formatting assumptions",
				code, Currency(code).Exponent())
		}
	}
	// The three the platform ships with today.
	for _, want := range []Currency{INR, NGN, USD} {
		if !want.Supported() {
			t.Errorf("%s is not supported", want)
		}
	}
	// And something plausible but unknown is still refused, so an unrecognised
	// code cannot be summed into a statement as though it were money.
	if Currency("ZAR").Supported() {
		t.Error("an unsupported currency was accepted")
	}
	// SupportedCodes must hand out a copy.
	codes[0] = "XXX"
	if SupportedCodes()[0] == "XXX" {
		t.Error("SupportedCodes exposed its backing slice")
	}
}
