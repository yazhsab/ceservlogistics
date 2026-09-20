package pricing

import (
	"testing"

	"github.com/ceserve/courier-os/internal/dbgen"
)

func testService(divisor, rounding, minWeight int32) *dbgen.CourierService {
	return &dbgen.CourierService{
		Code: "TEST", VolumetricDivisor: divisor,
		WeightRoundingGrams: rounding, MinWeightGrams: minWeight,
		MaxWeightGrams: 50_000, SlaTransitHours: 48,
	}
}

// TestComputeChargeableWeight pins the volumetric formula and the slab
// rounding, which together decide what a customer is billed for.
//
// The business formula is (L x W x H in cm) / divisor, producing kilograms.
// The API stores exact millimetres; converting mm to cm and kg to grams cancels
// the three powers of ten, so the stored numerator can be used directly.
func TestComputeChargeableWeight(t *testing.T) {
	cases := []struct {
		name           string
		packages       []Package
		divisor        int32
		rounding       int32
		minWeight      int32
		minChargeable  int32
		wantVolumetric int32
		wantChargeable int32
	}{
		{
			name:     "actual weight dominates and is rounded up to the slab",
			packages: []Package{{ActualWeightGrams: 600}},
			divisor:  5000, rounding: 500, minWeight: 1,
			wantVolumetric: 0, wantChargeable: 1000,
		},
		{
			name:     "50 by 40 by 25 centimetres is 10 kilograms",
			packages: []Package{{ActualWeightGrams: 500, LengthMM: 500, WidthMM: 400, HeightMM: 250}},
			divisor:  5000, rounding: 500, minWeight: 1,
			wantVolumetric: 10000, wantChargeable: 10000,
		},
		{
			name:     "volumetric weight dominates a light bulky parcel",
			packages: []Package{{ActualWeightGrams: 500, LengthMM: 300, WidthMM: 200, HeightMM: 150}},
			divisor:  5000, rounding: 500, minWeight: 1,
			wantVolumetric: 1800, wantChargeable: 2000,
		},
		{
			name:     "exact slab boundary is not rounded up",
			packages: []Package{{ActualWeightGrams: 1000}},
			divisor:  5000, rounding: 500, minWeight: 1,
			wantVolumetric: 0, wantChargeable: 1000,
		},
		{
			name:     "one gram over a boundary moves to the next slab",
			packages: []Package{{ActualWeightGrams: 1001}},
			divisor:  5000, rounding: 500, minWeight: 1,
			wantVolumetric: 0, wantChargeable: 1500,
		},
		{
			name: "multi-piece weights and volumes both accumulate",
			packages: []Package{
				{ActualWeightGrams: 400, LengthMM: 200, WidthMM: 200, HeightMM: 100},
				{ActualWeightGrams: 600, LengthMM: 200, WidthMM: 200, HeightMM: 100},
			},
			divisor: 5000, rounding: 500, minWeight: 1,
			// each piece is 4,000,000/5000 = 800 g volumetric
			wantVolumetric: 1600, wantChargeable: 2000,
		},
		{
			name:     "the lane minimum lifts a very light parcel",
			packages: []Package{{ActualWeightGrams: 50}},
			divisor:  5000, rounding: 500, minWeight: 1, minChargeable: 2000,
			wantVolumetric: 0, wantChargeable: 2000,
		},
		{
			name:     "the product minimum lifts a very light parcel",
			packages: []Package{{ActualWeightGrams: 10}},
			divisor:  5000, rounding: 500, minWeight: 500,
			wantVolumetric: 0, wantChargeable: 500,
		},
		{
			name:     "a surface divisor produces a heavier volumetric weight",
			packages: []Package{{ActualWeightGrams: 500, LengthMM: 300, WidthMM: 200, HeightMM: 150}},
			divisor:  4000, rounding: 500, minWeight: 1,
			wantVolumetric: 2250, wantChargeable: 2500,
		},
		{
			name:     "missing dimensions contribute no volumetric weight",
			packages: []Package{{ActualWeightGrams: 700, LengthMM: 300, WidthMM: 0, HeightMM: 150}},
			divisor:  5000, rounding: 500, minWeight: 1,
			wantVolumetric: 0, wantChargeable: 1000,
		},
		{
			name:     "a one-gram rounding step disables slab rounding",
			packages: []Package{{ActualWeightGrams: 733}},
			divisor:  5000, rounding: 1, minWeight: 1,
			wantVolumetric: 0, wantChargeable: 733,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := testService(tc.divisor, tc.rounding, tc.minWeight)
			got := ComputeChargeableWeight(tc.packages, svc, tc.minChargeable)
			if got.VolumetricGrams != tc.wantVolumetric {
				t.Errorf("volumetric = %d g, want %d g", got.VolumetricGrams, tc.wantVolumetric)
			}
			if got.ChargeableGrams != tc.wantChargeable {
				t.Errorf("chargeable = %d g, want %d g", got.ChargeableGrams, tc.wantChargeable)
			}
			if got.ChargeableGrams < got.ActualGrams {
				t.Error("chargeable weight must never be below actual weight")
			}
			if got.ChargeableGrams < got.VolumetricGrams {
				t.Error("chargeable weight must never be below volumetric weight")
			}
			if got.Explanation == "" {
				t.Error("the weight breakdown must explain how it was derived")
			}
		})
	}
}

// TestApplyCalc covers the three surcharge calculation types.
func TestApplyCalc(t *testing.T) {
	value := int64(7500)
	bp := int32(1850)

	if got, rate := applyCalc("FIXED", &value, nil, 100000, 1000); got != 7500 || rate != "fixed" {
		t.Errorf("FIXED = %d (%s), want 7500 (fixed)", got, rate)
	}
	if got, _ := applyCalc("PERCENTAGE", nil, &bp, 10000, 1000); got != 1850 {
		t.Errorf("PERCENTAGE of 10000 at 18.5%% = %d, want 1850", got)
	}
	// PER_KG charges per started kilogram: 1200 g is two kilograms.
	if got, _ := applyCalc("PER_KG", &value, nil, 0, 1200); got != 15000 {
		t.Errorf("PER_KG for 1200 g = %d, want 15000 (two started kg)", got)
	}
	if got, _ := applyCalc("PER_KG", &value, nil, 0, 1000); got != 7500 {
		t.Errorf("PER_KG for exactly 1000 g = %d, want 7500 (one kg)", got)
	}
	// A malformed rule contributes nothing rather than a wrong amount.
	if got, _ := applyCalc("PERCENTAGE", &value, nil, 10000, 1000); got != 0 {
		t.Errorf("a PERCENTAGE rule with no rate must contribute nothing, got %d", got)
	}
}

// TestMatchesConditions covers surcharge applicability.
func TestMatchesConditions(t *testing.T) {
	base := QuoteInput{
		PaymentMode: "COD", OriginIsRemote: false, DestIsRemote: true,
		DeclaredValueMinor: 500000, InsuranceRequired: true, IsIntraState: false,
	}
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"empty conditions always match", `{}`, true},
		{"remoteEither matches when the destination is remote", `{"remoteEither":true}`, true},
		{"remoteOrigin false matches a non-remote origin", `{"remoteOrigin":false}`, true},
		{"remoteOrigin true does not match", `{"remoteOrigin":true}`, false},
		{"payment mode list matches", `{"paymentModes":["COD","TO_PAY"]}`, true},
		{"payment mode list excludes", `{"paymentModes":["PREPAID"]}`, false},
		{"weight floor excludes a lighter parcel", `{"minWeightGrams":5000}`, false},
		{"weight ceiling includes", `{"maxWeightGrams":5000}`, true},
		{"declared value floor includes", `{"minDeclaredValueMinor":100000}`, true},
		{"declared value floor excludes", `{"minDeclaredValueMinor":900000}`, false},
		{"insurance requirement matches", `{"requiresInsurance":true}`, true},
		{"intra-state only excludes an inter-state lane", `{"intraStateOnly":true}`, false},
		{"malformed conditions never apply a charge", `{"remoteEither":`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesConditions([]byte(tc.raw), base, 1000); got != tc.want {
				t.Errorf("matchesConditions(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeDomesticCity(t *testing.T) {
	for input, want := range map[string]string{
		"Port Harcourt": "PORTHARCOURT",
		"Jama'are":      "JAMAARE",
		"  K-Dere  ":    "KDERE",
		"Okomu/Iddo":    "OKOMUIDDO",
	} {
		if got := normalizeDomesticCity(input); got != want {
			t.Errorf("normalizeDomesticCity(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSurchargeBasisSelection(t *testing.T) {
	in := QuoteInput{DeclaredValueMinor: 250000, CODAmountMinor: 99000}
	cases := map[string]int64{
		"FREIGHT":                 7500,
		"FREIGHT_PLUS_SURCHARGES": 8888,
		"DECLARED_VALUE":          250000,
		"COD_AMOUNT":              99000,
		"UNKNOWN":                 7500, // unknown bases fall back to freight
	}
	for appliesTo, want := range cases {
		if got := surchargeBasis(appliesTo, 7500, 8888, in); got != want {
			t.Errorf("surchargeBasis(%s) = %d, want %d", appliesTo, got, want)
		}
	}
}
