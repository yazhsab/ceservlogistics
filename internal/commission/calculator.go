// Package commission implements M21: the versioned commission engine.
//
// The engine answers one question — "how much does this party earn from this
// event?" — and it must answer it the same way twice, forever. Two design
// choices carry that:
//
//   - Rule *selection* is a total order (specificity, then priority, then id),
//     so the winner never depends on row order or query plan.
//   - Rule *arithmetic* lives in an effective-dated version, and a calculation
//     stores the version id it used along with its inputs and a step-by-step
//     trace. Editing a rate today cannot change what was earned last month,
//     because last month's calculation does not consult today's configuration.
//
// Everything in this file is pure: given inputs it returns a number and a
// trace, with no I/O. That is what makes the simulation endpoint and the real
// calculation provably the same computation rather than two implementations
// that are supposed to agree.
package commission

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/money"
)

// Commission types.
const (
	TypeBooking             = "BOOKING"
	TypePickup              = "PICKUP"
	TypeOriginHandling      = "ORIGIN_HANDLING"
	TypeDestinationHandling = "DESTINATION_HANDLING"
	TypeDelivery            = "DELIVERY"
	TypeCOD                 = "COD"
	TypeVolumeIncentive     = "VOLUME_INCENTIVE"
	TypeCustom              = "CUSTOM"
)

// Calculation methods.
const (
	MethodFixed      = "FIXED"
	MethodPercentage = "PERCENTAGE"
	MethodSlab       = "SLAB"
)

// Bases a percentage or slab can be applied to.
const (
	BasisFreight              = "FREIGHT"
	BasisSurcharge            = "SURCHARGE"
	BasisFreightPlusSurcharge = "FREIGHT_PLUS_SURCHARGE"
	BasisTotalBeforeTax       = "TOTAL_BEFORE_TAX"
	BasisTotal                = "TOTAL"
	BasisCODAmount            = "COD_AMOUNT"
	BasisChargeableWeight     = "CHARGEABLE_WEIGHT"
	BasisShipmentCount        = "SHIPMENT_COUNT"
)

// Recipient roles.
const (
	RoleOriginFranchise      = "ORIGIN_FRANCHISE"
	RoleDestinationFranchise = "DESTINATION_FRANCHISE"
	RolePickupAgent          = "PICKUP_AGENT"
	RoleDeliveryAgent        = "DELIVERY_AGENT"
	RoleOriginUnit           = "ORIGIN_UNIT"
	RoleDestinationUnit      = "DESTINATION_UNIT"
	RoleCustom               = "CUSTOM"
)

// Error codes.
const (
	CodeNoRule          = "COMMISSION_NO_RULE"
	CodeNoVersion       = "COMMISSION_NO_VERSION"
	CodeAlreadyExists   = "COMMISSION_ALREADY_CALCULATED"
	CodeInvalidSlabs    = "COMMISSION_INVALID_SLABS"
	CodeNotCalculated   = "COMMISSION_NOT_CALCULATED"
	CodeAlreadyPosted   = "COMMISSION_ALREADY_POSTED"
	CodeAlreadyReversed = "COMMISSION_ALREADY_REVERSED"
	CodeNoRecipient     = "COMMISSION_NO_RECIPIENT"
)

// Basis carries the numbers a rule can be applied to. All monetary fields are
// minor units; weight is grams; count is a plain count.
//
// It is filled from the shipment's charge snapshot — the immutable record of
// what was actually priced — rather than from live rate cards, which is what
// makes a recalculation reproduce the original number.
type Basis struct {
	FreightMinor      int64 `json:"freightMinor"`
	SurchargeMinor    int64 `json:"surchargeMinor"`
	DiscountMinor     int64 `json:"discountMinor"`
	TaxableMinor      int64 `json:"taxableMinor"`
	TaxMinor          int64 `json:"taxMinor"`
	TotalMinor        int64 `json:"totalMinor"`
	CODAmountMinor    int64 `json:"codAmountMinor"`
	ChargeableWeightG int64 `json:"chargeableWeightGrams"`
	ShipmentCount     int64 `json:"shipmentCount"`
}

// valueFor returns the number a named basis refers to.
func (b Basis) valueFor(basis string) (int64, error) {
	switch basis {
	case BasisFreight:
		return b.FreightMinor, nil
	case BasisSurcharge:
		return b.SurchargeMinor, nil
	case BasisFreightPlusSurcharge:
		return b.FreightMinor + b.SurchargeMinor, nil
	case BasisTotalBeforeTax:
		return b.TaxableMinor, nil
	case BasisTotal:
		return b.TotalMinor, nil
	case BasisCODAmount:
		return b.CODAmountMinor, nil
	case BasisChargeableWeight:
		return b.ChargeableWeightG, nil
	case BasisShipmentCount:
		return b.ShipmentCount, nil
	}
	return 0, apierr.Validation("Unknown commission basis.",
		map[string]any{"basis": basis})
}

// Slab is one band of a SLAB rule.
//
// A band pays AmountMinor, or RateBp against the basis value, or both. Bands
// are matched on the basis value falling in [FromMinor, ToMinor); the last band
// may leave ToMinor nil to mean "and above".
type Slab struct {
	FromMinor   int64  `json:"fromMinor"`
	ToMinor     *int64 `json:"toMinor,omitempty"`
	AmountMinor int64  `json:"amountMinor"`
	RateBp      *int32 `json:"rateBp,omitempty"`
	Label       string `json:"label,omitempty"`
}

// Rule is the arithmetic half of a commission rule version, decoupled from the
// database row so the calculator can be tested and simulated without one.
type Rule struct {
	Method   string
	Currency string

	FixedAmountMinor *int64
	RateBp           *int32
	BasisName        string
	Slabs            []Slab

	MinAmountMinor *int64
	MaxAmountMinor *int64
}

// Step is one line of the arithmetic, kept so a franchise can be shown exactly
// how a number was reached.
type Step struct {
	Description string `json:"description"`
	Value       int64  `json:"value,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// Outcome is a computed commission.
type Outcome struct {
	Method      string `json:"method"`
	BasisName   string `json:"basis,omitempty"`
	BasisValue  int64  `json:"basisValue"`
	RateBp      *int32 `json:"rateBp,omitempty"`
	GrossMinor  int64  `json:"grossAmountMinor"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	// Clamped records that a min or max changed the answer, so a surprising
	// number is self-explaining rather than looking like a calculation error.
	Clamped bool   `json:"clamped"`
	Steps   []Step `json:"trace"`
}

// Calculate applies a rule to a basis.
//
// Rounding is half-up throughout, delegated to money.ApplyBP, so it matches the
// pricing engine's convention rather than introducing a second one (§12).
func Calculate(r Rule, b Basis) (*Outcome, error) {
	out := &Outcome{Method: r.Method, Currency: r.Currency, Steps: make([]Step, 0, 6)}

	switch r.Method {
	case MethodFixed:
		if r.FixedAmountMinor == nil {
			return nil, apierr.Validation("A fixed rule needs an amount.", nil)
		}
		out.GrossMinor = *r.FixedAmountMinor
		out.Steps = append(out.Steps, Step{
			Description: "Fixed amount per qualifying event",
			Value:       out.GrossMinor,
		})

	case MethodPercentage:
		if r.RateBp == nil {
			return nil, apierr.Validation("A percentage rule needs a rate.", nil)
		}
		value, err := b.valueFor(r.BasisName)
		if err != nil {
			return nil, err
		}
		out.BasisName, out.BasisValue, out.RateBp = r.BasisName, value, r.RateBp
		out.GrossMinor = money.ApplyBP(value, money.BasisPoints(*r.RateBp))
		out.Steps = append(out.Steps,
			Step{Description: "Basis " + r.BasisName, Value: value},
			Step{
				Description: fmt.Sprintf("Apply %s%% of basis", formatBP(*r.RateBp)),
				Value:       out.GrossMinor,
				Detail:      fmt.Sprintf("%d × %d bp, rounded half-up", value, *r.RateBp),
			})

	case MethodSlab:
		value, err := b.valueFor(r.BasisName)
		if err != nil {
			return nil, err
		}
		slab, idx, err := selectSlab(r.Slabs, value)
		if err != nil {
			return nil, err
		}
		out.BasisName, out.BasisValue = r.BasisName, value

		amount := slab.AmountMinor
		detail := fmt.Sprintf("flat %d", slab.AmountMinor)
		if slab.RateBp != nil {
			rated := money.ApplyBP(value, money.BasisPoints(*slab.RateBp))
			amount += rated
			out.RateBp = slab.RateBp
			detail = fmt.Sprintf("flat %d plus %d bp of %d = %d",
				slab.AmountMinor, *slab.RateBp, value, rated)
		}
		out.GrossMinor = amount
		out.Steps = append(out.Steps,
			Step{Description: "Basis " + r.BasisName, Value: value},
			Step{
				Description: fmt.Sprintf("Matched slab %d (%s)", idx+1, describeSlab(slab)),
				Value:       amount,
				Detail:      detail,
			})

	default:
		return nil, apierr.Validation("Unknown calculation method.",
			map[string]any{"method": r.Method,
				"allowed": []string{MethodFixed, MethodPercentage, MethodSlab}})
	}

	// Clamp last, and say so. A silent clamp is the kind of thing that turns
	// into a long argument with a franchise six weeks later.
	out.AmountMinor = money.Clamp(out.GrossMinor, r.MinAmountMinor, r.MaxAmountMinor)
	if out.AmountMinor != out.GrossMinor {
		out.Clamped = true
		bound := "minimum"
		if out.AmountMinor < out.GrossMinor {
			bound = "maximum"
		}
		out.Steps = append(out.Steps, Step{
			Description: fmt.Sprintf("Applied %s bound", bound),
			Value:       out.AmountMinor,
			Detail:      fmt.Sprintf("gross %d adjusted to %d", out.GrossMinor, out.AmountMinor),
		})
	}

	if out.AmountMinor < 0 {
		return nil, apierr.Validation("A commission cannot be negative.",
			map[string]any{"amountMinor": out.AmountMinor})
	}
	return out, nil
}

// selectSlab finds the band a value falls in.
//
// Bands are sorted by lower bound first, so configuration order does not decide
// money. An unmatched value is an error rather than a zero: a gap in the slab
// table is a configuration bug, and paying nothing silently would hide it.
func selectSlab(slabs []Slab, value int64) (Slab, int, error) {
	if len(slabs) == 0 {
		return Slab{}, 0, apierr.Conflict(CodeInvalidSlabs, "The slab rule has no bands.")
	}
	sorted := make([]Slab, len(slabs))
	copy(sorted, slabs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].FromMinor < sorted[j].FromMinor })

	for i, s := range sorted {
		if value < s.FromMinor {
			continue
		}
		if s.ToMinor == nil || value < *s.ToMinor {
			return s, i, nil
		}
	}
	return Slab{}, 0, apierr.Conflict(CodeInvalidSlabs,
		fmt.Sprintf("No slab band covers a basis value of %d. The bands leave a gap or stop short.", value)).
		WithDetail("basisValue", value).
		WithDetail("bandCount", len(sorted))
}

// ValidateSlabs checks a slab table at configuration time, so a broken table is
// rejected when it is written rather than when it first has to pay someone.
func ValidateSlabs(slabs []Slab) error {
	if len(slabs) == 0 {
		return apierr.Conflict(CodeInvalidSlabs, "A slab rule needs at least one band.")
	}
	sorted := make([]Slab, len(slabs))
	copy(sorted, slabs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].FromMinor < sorted[j].FromMinor })

	if sorted[0].FromMinor != 0 {
		return apierr.Conflict(CodeInvalidSlabs,
			fmt.Sprintf("The first slab band must start at 0, not %d, or small values match nothing.",
				sorted[0].FromMinor))
	}

	for i, s := range sorted {
		if s.AmountMinor < 0 {
			return apierr.Conflict(CodeInvalidSlabs,
				fmt.Sprintf("Band %d has a negative amount.", i+1))
		}
		if s.RateBp != nil && (*s.RateBp < 0 || *s.RateBp > 1_000_000) {
			return apierr.Conflict(CodeInvalidSlabs,
				fmt.Sprintf("Band %d has a rate outside 0–1000000 basis points.", i+1))
		}
		if s.ToMinor != nil && *s.ToMinor <= s.FromMinor {
			return apierr.Conflict(CodeInvalidSlabs,
				fmt.Sprintf("Band %d ends at or before it starts.", i+1))
		}
		if i == len(sorted)-1 {
			if s.ToMinor != nil {
				return apierr.Conflict(CodeInvalidSlabs,
					"The last slab band must be open-ended, or large values match nothing.")
			}
			continue
		}
		// Bands must be contiguous. A gap silently pays nothing; an overlap
		// makes the answer depend on ordering.
		next := sorted[i+1]
		if s.ToMinor == nil {
			return apierr.Conflict(CodeInvalidSlabs,
				fmt.Sprintf("Band %d is open-ended but is not the last band.", i+1))
		}
		if *s.ToMinor != next.FromMinor {
			return apierr.Conflict(CodeInvalidSlabs,
				fmt.Sprintf("Bands %d and %d are not contiguous: %d then %d.",
					i+1, i+2, *s.ToMinor, next.FromMinor))
		}
	}
	return nil
}

// DecodeSlabs reads the stored JSON form.
func DecodeSlabs(raw []byte) ([]Slab, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var slabs []Slab
	if err := json.Unmarshal(raw, &slabs); err != nil {
		return nil, apierr.Conflict(CodeInvalidSlabs,
			"The stored slab table could not be read.")
	}
	return slabs, nil
}

func describeSlab(s Slab) string {
	if s.Label != "" {
		return s.Label
	}
	if s.ToMinor == nil {
		return fmt.Sprintf("%d and above", s.FromMinor)
	}
	return fmt.Sprintf("%d to %d", s.FromMinor, *s.ToMinor)
}

// formatBP renders basis points as a percentage for human-readable traces.
func formatBP(bp int32) string {
	whole := bp / 100
	frac := bp % 100
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	if frac%10 == 0 {
		return fmt.Sprintf("%d.%d", whole, frac/10)
	}
	return fmt.Sprintf("%d.%02d", whole, frac)
}
