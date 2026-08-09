package commission

import (
	"strings"
	"testing"
)

func i64(v int64) *int64 { return &v }
func i32(v int32) *int32 { return &v }

// A representative shipment: ₹500 freight, ₹75 surcharge, ₹103.50 tax,
// ₹678.50 total, ₹2,000 COD, 1.5 kg chargeable.
func sampleBasis() Basis {
	return Basis{
		FreightMinor:      50000,
		SurchargeMinor:    7500,
		TaxableMinor:      57500,
		TaxMinor:          10350,
		TotalMinor:        67850,
		CODAmountMinor:    200000,
		ChargeableWeightG: 1500,
		ShipmentCount:     1,
	}
}

func TestFixedCommission(t *testing.T) {
	out, err := Calculate(Rule{
		Method: MethodFixed, Currency: "INR", FixedAmountMinor: i64(2500),
	}, sampleBasis())
	if err != nil {
		t.Fatal(err)
	}
	if out.AmountMinor != 2500 {
		t.Fatalf("amount = %d, want 2500", out.AmountMinor)
	}
	if len(out.Steps) == 0 {
		t.Fatal("a calculation must produce a trace")
	}
}

func TestPercentageCommissionAgainstEachBasis(t *testing.T) {
	b := sampleBasis()
	// 12% expressed as 1200 basis points.
	cases := []struct {
		basis string
		want  int64
	}{
		{BasisFreight, 6000},              // 12% of 50000
		{BasisSurcharge, 900},             // 12% of 7500
		{BasisFreightPlusSurcharge, 6900}, // 12% of 57500
		{BasisTotalBeforeTax, 6900},       // 12% of 57500
		{BasisTotal, 8142},                // 12% of 67850
		{BasisCODAmount, 24000},           // 12% of 200000
	}
	for _, tc := range cases {
		t.Run(tc.basis, func(t *testing.T) {
			out, err := Calculate(Rule{
				Method: MethodPercentage, Currency: "INR",
				RateBp: i32(1200), BasisName: tc.basis,
			}, b)
			if err != nil {
				t.Fatal(err)
			}
			if out.AmountMinor != tc.want {
				t.Fatalf("%s: amount = %d, want %d", tc.basis, out.AmountMinor, tc.want)
			}
		})
	}
}

func TestPercentageRoundsHalfUp(t *testing.T) {
	// 2.5% of 101 = 2.525 minor units. Half-up gives 3, not 2.
	// This is the same convention the pricing engine uses; a mismatch here
	// would make commission and revenue disagree by a paisa per shipment,
	// which compounds into a real reconciliation problem.
	out, err := Calculate(Rule{
		Method: MethodPercentage, Currency: "INR",
		RateBp: i32(250), BasisName: BasisFreight,
	}, Basis{FreightMinor: 101})
	if err != nil {
		t.Fatal(err)
	}
	if out.AmountMinor != 3 {
		t.Fatalf("amount = %d, want 3 (half-up rounding)", out.AmountMinor)
	}
}

func TestMinimumAndMaximumClamps(t *testing.T) {
	b := Basis{FreightMinor: 10000}

	// 1% of 10000 = 100, below a floor of 500.
	out, err := Calculate(Rule{
		Method: MethodPercentage, Currency: "INR", RateBp: i32(100),
		BasisName: BasisFreight, MinAmountMinor: i64(500),
	}, b)
	if err != nil {
		t.Fatal(err)
	}
	if out.AmountMinor != 500 {
		t.Fatalf("amount = %d, want the 500 floor", out.AmountMinor)
	}
	if !out.Clamped {
		t.Fatal("a clamped result must say so, or the number looks like an error")
	}
	if out.GrossMinor != 100 {
		t.Fatalf("gross = %d, want the pre-clamp 100 preserved", out.GrossMinor)
	}

	// 50% of 10000 = 5000, above a ceiling of 1000.
	out, err = Calculate(Rule{
		Method: MethodPercentage, Currency: "INR", RateBp: i32(5000),
		BasisName: BasisFreight, MaxAmountMinor: i64(1000),
	}, b)
	if err != nil {
		t.Fatal(err)
	}
	if out.AmountMinor != 1000 {
		t.Fatalf("amount = %d, want the 1000 ceiling", out.AmountMinor)
	}
	if !out.Clamped {
		t.Fatal("a clamped result must say so")
	}
}

func standardSlabs() []Slab {
	return []Slab{
		{FromMinor: 0, ToMinor: i64(50000), AmountMinor: 1000, Label: "up to ₹500"},
		{FromMinor: 50000, ToMinor: i64(200000), AmountMinor: 2500, Label: "₹500–₹2000"},
		{FromMinor: 200000, ToMinor: nil, AmountMinor: 5000, Label: "above ₹2000"},
	}
}

func TestSlabSelection(t *testing.T) {
	cases := []struct {
		name  string
		value int64
		want  int64
	}{
		{"bottom of first band", 0, 1000},
		{"inside first band", 25000, 1000},
		{"boundary belongs to the upper band", 50000, 2500},
		{"inside second band", 100000, 2500},
		{"boundary belongs to the upper band again", 200000, 5000},
		{"open-ended top band", 999999999, 5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Calculate(Rule{
				Method: MethodSlab, Currency: "INR",
				BasisName: BasisFreight, Slabs: standardSlabs(),
			}, Basis{FreightMinor: tc.value})
			if err != nil {
				t.Fatal(err)
			}
			if out.AmountMinor != tc.want {
				t.Fatalf("value %d: amount = %d, want %d", tc.value, out.AmountMinor, tc.want)
			}
		})
	}
}

func TestSlabSelectionIsIndependentOfConfigurationOrder(t *testing.T) {
	// Configuration order must not decide money. The same bands supplied in
	// reverse must pay the same.
	forward := standardSlabs()
	reversed := []Slab{forward[2], forward[0], forward[1]}

	for _, value := range []int64{0, 49999, 50000, 199999, 200000, 5000000} {
		a, err := Calculate(Rule{Method: MethodSlab, Currency: "INR",
			BasisName: BasisFreight, Slabs: forward}, Basis{FreightMinor: value})
		if err != nil {
			t.Fatal(err)
		}
		b, err := Calculate(Rule{Method: MethodSlab, Currency: "INR",
			BasisName: BasisFreight, Slabs: reversed}, Basis{FreightMinor: value})
		if err != nil {
			t.Fatal(err)
		}
		if a.AmountMinor != b.AmountMinor {
			t.Fatalf("value %d: forward order paid %d, reversed order paid %d",
				value, a.AmountMinor, b.AmountMinor)
		}
	}
}

func TestSlabWithFlatPlusRate(t *testing.T) {
	// ₹10 flat plus 1% of the basis.
	out, err := Calculate(Rule{
		Method: MethodSlab, Currency: "INR", BasisName: BasisFreight,
		Slabs: []Slab{{FromMinor: 0, ToMinor: nil, AmountMinor: 1000, RateBp: i32(100)}},
	}, Basis{FreightMinor: 50000})
	if err != nil {
		t.Fatal(err)
	}
	// 1000 + 1% of 50000 = 1000 + 500 = 1500
	if out.AmountMinor != 1500 {
		t.Fatalf("amount = %d, want 1500", out.AmountMinor)
	}
}

func TestSlabGapIsRefusedRatherThanPayingZero(t *testing.T) {
	// A gap in the table is a configuration bug. Paying nothing silently would
	// hide it until a franchise noticed a missing payment.
	gapped := []Slab{
		{FromMinor: 0, ToMinor: i64(10000), AmountMinor: 100},
		{FromMinor: 50000, ToMinor: nil, AmountMinor: 500},
	}
	_, err := Calculate(Rule{
		Method: MethodSlab, Currency: "INR", BasisName: BasisFreight, Slabs: gapped,
	}, Basis{FreightMinor: 25000}) // falls in the gap
	if err == nil {
		t.Fatal("a basis value in a slab gap must be refused, not paid as zero")
	}
	if !strings.Contains(err.Error(), "gap") && !strings.Contains(err.Error(), "No slab band") {
		t.Fatalf("error should name the gap, got: %v", err)
	}
}

func TestValidateSlabsCatchesBadTables(t *testing.T) {
	cases := []struct {
		name  string
		slabs []Slab
		want  string
	}{
		{"empty", nil, "at least one band"},
		{
			"does not start at zero",
			[]Slab{{FromMinor: 1000, ToMinor: nil, AmountMinor: 100}},
			"must start at 0",
		},
		{
			"gap between bands",
			[]Slab{
				{FromMinor: 0, ToMinor: i64(1000), AmountMinor: 10},
				{FromMinor: 5000, ToMinor: nil, AmountMinor: 50},
			},
			"not contiguous",
		},
		{
			"overlapping bands",
			[]Slab{
				{FromMinor: 0, ToMinor: i64(5000), AmountMinor: 10},
				{FromMinor: 1000, ToMinor: nil, AmountMinor: 50},
			},
			"not contiguous",
		},
		{
			"last band is closed",
			[]Slab{
				{FromMinor: 0, ToMinor: i64(1000), AmountMinor: 10},
				{FromMinor: 1000, ToMinor: i64(2000), AmountMinor: 50},
			},
			"must be open-ended",
		},
		{
			"open-ended band in the middle",
			[]Slab{
				{FromMinor: 0, ToMinor: nil, AmountMinor: 10},
				{FromMinor: 1000, ToMinor: nil, AmountMinor: 50},
			},
			"open-ended but is not the last",
		},
		{
			"band ends before it starts",
			[]Slab{
				{FromMinor: 0, ToMinor: i64(1000), AmountMinor: 10},
				{FromMinor: 1000, ToMinor: i64(500), AmountMinor: 50},
			},
			"ends at or before it starts",
		},
		{
			"negative amount",
			[]Slab{{FromMinor: 0, ToMinor: nil, AmountMinor: -1}},
			"negative amount",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlabs(tc.slabs)
			if err == nil {
				t.Fatalf("expected %q to be refused at configuration time", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateSlabsAcceptsAGoodTable(t *testing.T) {
	if err := ValidateSlabs(standardSlabs()); err != nil {
		t.Fatalf("a contiguous, open-ended table was refused: %v", err)
	}
}

func TestCalculateRejectsUnknownBasisAndMethod(t *testing.T) {
	if _, err := Calculate(Rule{
		Method: MethodPercentage, Currency: "INR", RateBp: i32(100), BasisName: "MOONBEAMS",
	}, sampleBasis()); err == nil {
		t.Fatal("an unknown basis should be refused")
	}
	if _, err := Calculate(Rule{Method: "TELEPATHY", Currency: "INR"}, sampleBasis()); err == nil {
		t.Fatal("an unknown method should be refused")
	}
}

func TestCalculateRejectsIncompleteRules(t *testing.T) {
	if _, err := Calculate(Rule{Method: MethodFixed, Currency: "INR"}, sampleBasis()); err == nil {
		t.Fatal("a fixed rule without an amount should be refused")
	}
	if _, err := Calculate(Rule{
		Method: MethodPercentage, Currency: "INR", BasisName: BasisFreight,
	}, sampleBasis()); err == nil {
		t.Fatal("a percentage rule without a rate should be refused")
	}
}

func TestZeroBasisPaysZeroNotAnError(t *testing.T) {
	// A free shipment earns no percentage commission. That is a legitimate
	// answer, not a failure.
	out, err := Calculate(Rule{
		Method: MethodPercentage, Currency: "INR", RateBp: i32(1200), BasisName: BasisFreight,
	}, Basis{FreightMinor: 0})
	if err != nil {
		t.Fatalf("a zero basis should calculate to zero, not error: %v", err)
	}
	if out.AmountMinor != 0 {
		t.Fatalf("amount = %d, want 0", out.AmountMinor)
	}
}

func TestCalculationIsDeterministic(t *testing.T) {
	// The same rule and basis must produce the same number every time, which is
	// what makes a settlement reproducible.
	rule := Rule{
		Method: MethodSlab, Currency: "INR", BasisName: BasisTotal,
		Slabs: standardSlabs(), MinAmountMinor: i64(500), MaxAmountMinor: i64(4000),
	}
	b := sampleBasis()

	first, err := Calculate(rule, b)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		again, err := Calculate(rule, b)
		if err != nil {
			t.Fatal(err)
		}
		if again.AmountMinor != first.AmountMinor || again.GrossMinor != first.GrossMinor {
			t.Fatalf("run %d produced %d/%d, first run produced %d/%d",
				i, again.GrossMinor, again.AmountMinor, first.GrossMinor, first.AmountMinor)
		}
	}
}

func TestFormatBPRendersReadablePercentages(t *testing.T) {
	// The trace is shown to franchise owners, so 1250 bp must read as 12.5%
	// rather than as a raw number.
	for _, tc := range []struct {
		bp   int32
		want string
	}{
		{1200, "12"}, {1250, "12.5"}, {1234, "12.34"}, {100, "1"}, {5, "0.05"},
	} {
		if got := formatBP(tc.bp); got != tc.want {
			t.Fatalf("formatBP(%d) = %s, want %s", tc.bp, got, tc.want)
		}
	}
}
