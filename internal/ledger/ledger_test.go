package ledger

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// The tests in this file exercise the pure logic — leg validation and the
// balance arithmetic — without a database. The database half of the invariant
// (the deferred constraint trigger) is proven in tests/integration, where a
// real PostgreSQL connection can be attacked.

func TestTotalOfRejectsUnbalancedLegs(t *testing.T) {
	cases := []struct {
		name string
		legs []Leg
		want string
	}{
		{
			name: "debits exceed credits",
			legs: []Leg{
				{AccountCode: AcctCash, Debit: 10000},
				{AccountCode: AcctFreightRevenue, Credit: 6000},
			},
			want: CodeUnbalanced,
		},
		{
			name: "credits exceed debits",
			legs: []Leg{
				{AccountCode: AcctCash, Debit: 5000},
				{AccountCode: AcctFreightRevenue, Credit: 5001},
			},
			want: CodeUnbalanced,
		},
		{
			name: "off by one paisa",
			legs: []Leg{
				{AccountCode: AcctCash, Debit: 1},
				{AccountCode: AcctFreightRevenue, Credit: 2},
			},
			want: CodeUnbalanced,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := totalOf(tc.legs)
			if err == nil {
				t.Fatal("expected an unbalanced transaction to be refused")
			}
			if !strings.Contains(err.Error(), "does not balance") {
				t.Fatalf("error should explain the imbalance, got: %v", err)
			}
		})
	}
}

func TestTotalOfAcceptsBalancedLegs(t *testing.T) {
	legs := []Leg{
		{AccountCode: AcctCash, Debit: 12550},
		{AccountCode: AcctFreightRevenue, Credit: 10000},
		{AccountCode: AcctTaxPayable, Credit: 2550},
	}
	total, err := totalOf(legs)
	if err != nil {
		t.Fatalf("balanced legs were refused: %v", err)
	}
	if total != 12550 {
		t.Fatalf("total = %d, want 12550", total)
	}
}

func TestTotalOfRejectsZeroTransaction(t *testing.T) {
	// Both sides zero balances trivially, but a zero-value journal is noise
	// that would clutter every statement without recording anything.
	_, err := totalOf([]Leg{
		{AccountCode: AcctCash, Debit: 0, Credit: 0},
		{AccountCode: AcctFreightRevenue, Debit: 0, Credit: 0},
	})
	if err == nil {
		t.Fatal("a zero-value transaction should be refused")
	}
}

// TestBalanceInvariantHoldsForRandomPostings is the property test the release
// specification asks for: for any randomly generated set of legs, totalOf
// accepts it if and only if debits equal credits.
func TestBalanceInvariantHoldsForRandomPostings(t *testing.T) {
	rng := rand.New(rand.NewSource(20260808))

	const iterations = 20000
	accepted, refused := 0, 0

	for i := 0; i < iterations; i++ {
		n := 2 + rng.Intn(6) // 2..7 legs
		legs := make([]Leg, 0, n)
		var debits, credits int64

		for j := 0; j < n; j++ {
			// Amounts spanning a paisa to about ten lakh, which is the real
			// operating range for a courier network.
			amount := int64(1 + rng.Intn(100_000_00))
			if rng.Intn(2) == 0 {
				legs = append(legs, Leg{AccountCode: AcctCash, Debit: amount})
				debits += amount
			} else {
				legs = append(legs, Leg{AccountCode: AcctFreightRevenue, Credit: amount})
				credits += amount
			}
		}

		// Random amounts essentially never balance by chance, so half the
		// iterations are deliberately closed out with a balancing leg. Without
		// this the test would only ever exercise the rejection path.
		if rng.Intn(2) == 0 && debits != credits {
			if debits > credits {
				diff := debits - credits
				legs = append(legs, Leg{AccountCode: AcctFreightRevenue, Credit: diff})
				credits += diff
			} else {
				diff := credits - debits
				legs = append(legs, Leg{AccountCode: AcctCash, Debit: diff})
				debits += diff
			}
		}

		total, err := totalOf(legs)
		balanced := debits == credits && debits > 0

		switch {
		case balanced && err != nil:
			t.Fatalf("iteration %d: balanced legs (D=%d C=%d) were refused: %v",
				i, debits, credits, err)
		case !balanced && err == nil:
			t.Fatalf("iteration %d: unbalanced legs (D=%d C=%d) were accepted",
				i, debits, credits)
		case balanced:
			if total != debits {
				t.Fatalf("iteration %d: total = %d, want %d", i, total, debits)
			}
			accepted++
		default:
			refused++
		}
	}

	// Sanity: the generator must actually produce both outcomes, or the test
	// proves nothing. Balanced sets are rare by chance, so this is a low bar
	// that still catches a generator that stopped varying.
	if accepted == 0 {
		t.Fatal("no balanced case was generated; the property test proved nothing")
	}
	if refused == 0 {
		t.Fatal("no unbalanced case was generated; the property test proved nothing")
	}
	t.Logf("checked %d random postings: %d balanced, %d unbalanced", iterations, accepted, refused)
}

// TestConstructedBalancedPostingsAreAlwaysAccepted complements the test above:
// it builds sets that are balanced by construction across many shapes and
// magnitudes, so a systematic arithmetic error would surface.
func TestConstructedBalancedPostingsAreAlwaysAccepted(t *testing.T) {
	rng := rand.New(rand.NewSource(99))

	for i := 0; i < 5000; i++ {
		n := 1 + rng.Intn(5)
		legs := make([]Leg, 0, n*2)
		var expected int64

		for j := 0; j < n; j++ {
			amount := int64(1 + rng.Intn(50_000_00))
			legs = append(legs,
				Leg{AccountCode: AcctCash, Debit: amount},
				Leg{AccountCode: AcctFreightRevenue, Credit: amount},
			)
			expected += amount
		}

		total, err := totalOf(legs)
		if err != nil {
			t.Fatalf("iteration %d: a balanced-by-construction posting was refused: %v", i, err)
		}
		if total != expected {
			t.Fatalf("iteration %d: total = %d, want %d", i, total, expected)
		}
	}
}

func TestPostingValidationRejectsMalformedLegs(t *testing.T) {
	base := func(legs []Leg) Posting {
		return Posting{
			SourceType: SourceCommission, Purpose: "TEST",
			Description: "test posting", Currency: "INR",
			PostingDate: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
			Legs:        legs,
		}
	}

	cases := []struct {
		name string
		in   Posting
		want string
	}{
		{
			name: "leg with both sides set",
			in: base([]Leg{
				{AccountCode: AcctCash, Debit: 100, Credit: 100},
				{AccountCode: AcctFreightRevenue, Credit: 100},
			}),
			want: "either a debit or a credit",
		},
		{
			name: "leg with neither side set",
			in: base([]Leg{
				{AccountCode: AcctCash},
				{AccountCode: AcctFreightRevenue, Credit: 100},
			}),
			want: "either a debit or a credit",
		},
		{
			name: "negative amount",
			in: base([]Leg{
				{AccountCode: AcctCash, Debit: -100},
				{AccountCode: AcctFreightRevenue, Credit: 100},
			}),
			want: "cannot be negative",
		},
		{
			name: "single leg",
			in: base([]Leg{
				{AccountCode: AcctCash, Debit: 100},
			}),
			want: "at least two legs",
		},
		{
			name: "missing account code",
			in: base([]Leg{
				{Debit: 100},
				{AccountCode: AcctFreightRevenue, Credit: 100},
			}),
			want: "needs an account",
		},
		{
			name: "party leg without a party id",
			in: base([]Leg{
				{AccountCode: AcctFranchisePayable, Credit: 100, PartyType: PartyFranchise},
				{AccountCode: AcctCommissionExpense, Debit: 100},
			}),
			want: "needs a party id",
		},
		{
			name: "unsupported currency",
			in: Posting{
				SourceType: SourceCommission, Purpose: "TEST", Description: "d",
				Currency: "XYZ",
				Legs: []Leg{
					{AccountCode: AcctCash, Debit: 100},
					{AccountCode: AcctFreightRevenue, Credit: 100},
				},
			},
			want: "Unsupported currency",
		},
		{
			name: "manual posting without a reason",
			in: Posting{
				SourceType: SourceManual, Purpose: "TEST", Description: "d", Currency: "INR",
				Legs: []Leg{
					{AccountCode: AcctCash, Debit: 100},
					{AccountCode: AcctFreightRevenue, Credit: 100},
				},
			},
			want: "requires a reason",
		},
		{
			name: "adjustment without a reason",
			in: Posting{
				SourceType: SourceAdjustment, Purpose: "TEST", Description: "d", Currency: "INR",
				Legs: []Leg{
					{AccountCode: AcctCash, Debit: 100},
					{AccountCode: AcctFreightRevenue, Credit: 100},
				},
			},
			want: "requires a reason",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			err := in.validate()
			if err == nil {
				t.Fatalf("expected %q to be refused", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestPostingValidationAcceptsAWellFormedPosting(t *testing.T) {
	in := Posting{
		SourceType: SourceCommission, SourceID: ptr(int64(1)), Purpose: "DELIVERY",
		Description: "Delivery commission for AWB DMO260808000001",
		Currency:    "INR",
		Legs: []Leg{
			{AccountCode: AcctCommissionExpense, Debit: 4250},
			{AccountCode: AcctFranchisePayable, Credit: 4250,
				PartyType: PartyFranchise, PartyID: 7},
		},
	}
	if err := in.validate(); err != nil {
		t.Fatalf("a well-formed posting was refused: %v", err)
	}
	// A zero posting date is filled in rather than rejected, so callers that do
	// not care about backdating need not set it.
	if in.PostingDate.IsZero() {
		t.Fatal("validate should default the posting date")
	}
}

func TestNormalBalanceFollowsAccountType(t *testing.T) {
	// Getting this wrong inverts every report built on the account, so it is
	// worth asserting rather than assuming.
	cases := map[string]string{
		TypeAsset:     NormalDebit,
		TypeExpense:   NormalDebit,
		TypeLiability: NormalCredit,
		TypeEquity:    NormalCredit,
		TypeRevenue:   NormalCredit,
	}
	for accountType, want := range cases {
		got, err := normalBalanceFor(accountType)
		if err != nil {
			t.Fatalf("%s: %v", accountType, err)
		}
		if got != want {
			t.Fatalf("%s has normal balance %s, want %s", accountType, got, want)
		}
	}
	if _, err := normalBalanceFor("NONSENSE"); err == nil {
		t.Fatal("an unknown account type should be refused")
	}
}

func TestSubsidiaryAccountCodesAreStable(t *testing.T) {
	// The generated code is what a subsidiary account is found by on the next
	// posting, so its shape must not drift.
	for _, tc := range []struct {
		party string
		want  string
	}{
		{PartyFranchise, "FRN"},
		{PartyCustomer, "CUS"},
		{PartyOperatingUnit, "OPU"},
		{PartyAgent, "AGT"},
		{PartyCarrier, "CAR"},
	} {
		if got := shortParty(tc.party); got != tc.want {
			t.Fatalf("shortParty(%s) = %s, want %s", tc.party, got, tc.want)
		}
	}
	code := fmt.Sprintf("%s.%s.%d", AcctFranchisePayable, shortParty(PartyFranchise), 42)
	if code != "2200.FRN.42" {
		t.Fatalf("subsidiary code = %s, want 2200.FRN.42", code)
	}
}

func ptr[T any](v T) *T { return &v }
