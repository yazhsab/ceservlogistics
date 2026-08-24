// Package pricing implements the versioned, auditable pricing engine (M06).
//
// Constitution §12 and §19 govern this package:
//
//   - money is bigint minor units end to end; no float ever touches an
//     authoritative amount;
//   - percentages are integer basis points, so 18.5% is exactly 1850;
//   - rounding is HALF_UP away from zero, applied once per derived line item
//     and never re-applied to a running total;
//   - the result is a line-by-line breakdown plus the exact inputs, which
//     booking persists so a price can be reproduced years later even if the
//     rate card is edited or deleted.
package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/money"
)

// EngineVersion is stamped on every snapshot. Bump it whenever the calculation
// changes so historical snapshots remain interpretable.
const EngineVersion = "1.0.0"

// Line-item kinds, in the order they appear in a breakdown.
const (
	KindFreight   = "FREIGHT"
	KindSurcharge = "SURCHARGE"
	KindDiscount  = "DISCOUNT"
	KindTax       = "TAX"
	KindRounding  = "ROUNDING"
)

// Package is one physical piece being priced.
type Package struct {
	ActualWeightGrams int32 `json:"actualWeightGrams"`
	LengthMM          int32 `json:"lengthMm,omitempty"`
	WidthMM           int32 `json:"widthMm,omitempty"`
	HeightMM          int32 `json:"heightMm,omitempty"`
}

// QuoteInput is everything the engine needs.
type QuoteInput struct {
	OrganizationID int64
	CustomerID     *int64
	FranchiseID    *int64

	Service *dbgen.CourierService

	OriginZoneID   int64
	OriginZoneCode string
	DestZoneID     int64
	DestZoneCode   string
	OriginStateID  int64
	DestStateID    int64

	Packages []Package

	PaymentMode        string
	DeclaredValueMinor int64
	CODAmountMinor     int64
	InsuranceRequired  bool

	OriginIsRemote bool
	DestIsRemote   bool
	// IsIntraState selects CGST+SGST versus IGST.
	IsIntraState bool

	Currency money.Currency
	At       time.Time
}

// LineItem is one row of the price breakdown.
type LineItem struct {
	Kind        string `json:"kind"`
	Code        string `json:"code"`
	Label       string `json:"label"`
	AmountMinor int64  `json:"amountMinor"`
	// Basis records what the amount was computed from, so a reviewer can redo
	// the arithmetic without the engine.
	BasisMinor  int64  `json:"basisMinor,omitempty"`
	Rate        string `json:"rate,omitempty"`
	RuleID      string `json:"ruleId,omitempty"`
	Explanation string `json:"explanation"`
}

// WeightBreakdown records how chargeable weight was derived.
type WeightBreakdown struct {
	ActualGrams        int32  `json:"actualWeightGrams"`
	VolumetricGrams    int32  `json:"volumetricWeightGrams"`
	VolumetricDivisor  int32  `json:"volumetricDivisor"`
	RoundingGrams      int32  `json:"weightRoundingGrams"`
	MinChargeableGrams int32  `json:"minChargeableWeightGrams"`
	ChargeableGrams    int32  `json:"chargeableWeightGrams"`
	Explanation        string `json:"explanation"`
}

// Quote is the engine's output.
type Quote struct {
	Currency string `json:"currency"`

	RateCardID        string `json:"rateCardId"`
	RateCardCode      string `json:"rateCardCode"`
	RateCardScope     string `json:"rateCardScope"`
	RateCardVersionID string `json:"rateCardVersionId"`
	VersionNumber     int32  `json:"rateCardVersion"`
	EngineVersion     string `json:"pricingEngineVersion"`

	OriginZoneCode      string `json:"originZoneCode"`
	DestinationZoneCode string `json:"destinationZoneCode"`
	ServiceCode         string `json:"serviceCode"`

	Weight WeightBreakdown `json:"weight"`

	FreightMinor        int64 `json:"freightMinor"`
	SurchargeTotalMinor int64 `json:"surchargeTotalMinor"`
	DiscountTotalMinor  int64 `json:"discountTotalMinor"`
	TaxableMinor        int64 `json:"taxableMinor"`
	TaxTotalMinor       int64 `json:"taxTotalMinor"`
	RoundingMinor       int64 `json:"roundingMinor"`
	TotalMinor          int64 `json:"totalMinor"`

	LineItems []LineItem `json:"lineItems"`

	// internal identifiers for the booking snapshot
	rateCardDBID        int64
	rateCardVersionDBID int64
}

// conditions is the JSON policy attached to a surcharge or discount rule.
type conditions struct {
	RemoteOrigin          *bool    `json:"remoteOrigin,omitempty"`
	RemoteDestination     *bool    `json:"remoteDestination,omitempty"`
	RemoteEither          *bool    `json:"remoteEither,omitempty"`
	PaymentModes          []string `json:"paymentModes,omitempty"`
	MinWeightGrams        *int32   `json:"minWeightGrams,omitempty"`
	MaxWeightGrams        *int32   `json:"maxWeightGrams,omitempty"`
	MinDeclaredValueMinor *int64   `json:"minDeclaredValueMinor,omitempty"`
	RequiresInsurance     *bool    `json:"requiresInsurance,omitempty"`
	IntraStateOnly        *bool    `json:"intraStateOnly,omitempty"`
}

// Engine computes quotes.
type Engine struct {
	db  *database.DB
	q   *dbgen.Queries
	log *slog.Logger
}

// NewEngine builds the pricing engine.
func NewEngine(db *database.DB, q *dbgen.Queries, log *slog.Logger) *Engine {
	return &Engine{db: db, q: q, log: log}
}

// DB exposes the pool for handlers that need a transaction (rate card
// activation supersedes and activates in one atomic step).
func (e *Engine) DB() *database.DB { return e.db }

// Quote prices a shipment.
func (e *Engine) Quote(ctx context.Context, in QuoteInput) (*Quote, error) {
	if in.At.IsZero() {
		in.At = time.Now()
	}
	if in.Currency == "" {
		in.Currency = money.INR
	}
	if len(in.Packages) == 0 {
		return nil, apierr.Validation("At least one package is required to price a shipment.", nil)
	}

	// 1. Rate card.
	card, err := e.resolveRateCard(ctx, in)
	if err != nil {
		return nil, err
	}

	quote := &Quote{
		Currency:            string(in.Currency),
		RateCardID:          card.PublicID,
		RateCardCode:        card.Code,
		RateCardScope:       card.Scope,
		RateCardVersionID:   card.VersionPublicID,
		VersionNumber:       card.VersionNumber,
		EngineVersion:       EngineVersion,
		OriginZoneCode:      in.OriginZoneCode,
		DestinationZoneCode: in.DestZoneCode,
		ServiceCode:         in.Service.Code,
		LineItems:           []LineItem{},
		rateCardDBID:        card.ID,
		rateCardVersionDBID: card.VersionID,
	}

	// 2. Chargeable weight.
	zoneRate, zoneErr := e.q.FindZoneRate(ctx, dbgen.FindZoneRateParams{
		RateCardVersionID: card.VersionID, CourierServiceID: in.Service.ID,
		OriginZoneID: in.OriginZoneID, DestinationZoneID: in.DestZoneID,
	})
	haveZoneRate := zoneErr == nil
	if zoneErr != nil && !database.IsNoRows(zoneErr) {
		return nil, apierr.Internal(fmt.Errorf("find zone rate: %w", zoneErr))
	}
	minChargeable := int32(0)
	if haveZoneRate {
		minChargeable = zoneRate.MinChargeableWeightGrams
	}
	quote.Weight = ComputeChargeableWeight(in.Packages, in.Service, minChargeable)

	// 3. Freight.
	freight, freightItem, err := e.computeFreight(ctx, in, card, zoneRate, haveZoneRate, quote.Weight.ChargeableGrams)
	if err != nil {
		return nil, err
	}
	quote.FreightMinor = freight
	quote.LineItems = append(quote.LineItems, freightItem)

	// 4. Surcharges.
	surchargeTaxable, surchargeExempt, surchargeItems, err := e.computeSurcharges(ctx, in, card.VersionID, freight, quote.Weight.ChargeableGrams)
	if err != nil {
		return nil, err
	}
	quote.LineItems = append(quote.LineItems, surchargeItems...)
	quote.SurchargeTotalMinor = surchargeTaxable + surchargeExempt

	// 5. Discounts.
	discount, discountItems, err := e.computeDiscounts(ctx, in, card.VersionID, freight, surchargeTaxable+surchargeExempt)
	if err != nil {
		return nil, err
	}
	quote.LineItems = append(quote.LineItems, discountItems...)
	quote.DiscountTotalMinor = discount

	// 6. Taxable base: freight plus taxable surcharges, less discount, floored
	// at zero so an over-generous discount cannot produce negative tax.
	taxable := freight + surchargeTaxable - discount
	if taxable < 0 {
		taxable = 0
	}
	quote.TaxableMinor = taxable

	// 7. Tax.
	taxTotal, taxItems, err := e.computeTax(ctx, in, taxable)
	if err != nil {
		return nil, err
	}
	quote.LineItems = append(quote.LineItems, taxItems...)
	quote.TaxTotalMinor = taxTotal

	// 8. Total. Every component is already an exact integer in minor units, so
	// no further rounding is applied and roundingMinor is always zero. It is
	// kept in the response and the snapshot so a future invoice-level rounding
	// policy has a place to live without changing the contract.
	quote.RoundingMinor = 0
	quote.TotalMinor = freight + surchargeTaxable + surchargeExempt - discount + taxTotal
	if quote.TotalMinor < 0 {
		quote.TotalMinor = 0
	}
	return quote, nil
}

// ComputeChargeableWeight derives billable weight from the packages.
//
//	volumetric(g) = L(cm) x W(cm) x H(cm) / divisor x 1000
//	50 cm x 40 cm x 25 cm / 5000 = 10 kg = 10000 g
//
// with the division rounded HALF_UP, summed across packages. Chargeable weight
// is the greater of actual and volumetric, raised to the lane's minimum, then
// rounded UP to the product's slab granularity — rounding up, because a courier
// bills the slab a parcel falls into, never the one below it.
func ComputeChargeableWeight(packages []Package, svc *dbgen.CourierService, minChargeableGrams int32) WeightBreakdown {
	var actual, volumetric int64
	for _, p := range packages {
		actual += int64(p.ActualWeightGrams)
		if p.LengthMM > 0 && p.WidthMM > 0 && p.HeightMM > 0 {
			// Millimetres in, centimetres for the formula: /10 per dimension is
			// /1000 overall, and the result is converted from kilograms to grams
			// by x1000 — the two cancel, so the numerator is used directly.
			numerator := int64(p.LengthMM) * int64(p.WidthMM) * int64(p.HeightMM)
			volumetric += money.DivRoundHalfUp(numerator, int64(svc.VolumetricDivisor))
		}
	}

	chargeable := actual
	basis := "actual"
	if volumetric > chargeable {
		chargeable = volumetric
		basis = "volumetric"
	}
	if int64(minChargeableGrams) > chargeable {
		chargeable = int64(minChargeableGrams)
		basis = "lane minimum"
	}
	if int64(svc.MinWeightGrams) > chargeable {
		chargeable = int64(svc.MinWeightGrams)
		basis = "service minimum"
	}
	rounding := int64(svc.WeightRoundingGrams)
	if rounding > 1 {
		chargeable = ((chargeable + rounding - 1) / rounding) * rounding
	}

	return WeightBreakdown{
		ActualGrams:        int32(actual),
		VolumetricGrams:    int32(volumetric),
		VolumetricDivisor:  svc.VolumetricDivisor,
		RoundingGrams:      svc.WeightRoundingGrams,
		MinChargeableGrams: minChargeableGrams,
		ChargeableGrams:    int32(chargeable),
		Explanation: fmt.Sprintf(
			"actual=%dg volumetric=%dg (divisor %d); %s basis selected, rounded up to the nearest %dg -> %dg",
			actual, volumetric, svc.VolumetricDivisor, basis, rounding, chargeable),
	}
}

// resolvedCard bundles a rate card with its active version.
type resolvedCard struct {
	ID              int64
	PublicID        string
	Code            string
	Scope           string
	VersionID       int64
	VersionPublicID string
	VersionNumber   int32
}

// resolveRateCard picks the card that applies, with a clear error when none
// does — a missing rate card is a configuration gap operators must see, never
// a silently free shipment.
func (e *Engine) resolveRateCard(ctx context.Context, in QuoteInput) (*resolvedCard, error) {
	row, err := e.q.ResolveRateCardForBooking(ctx, dbgen.ResolveRateCardForBookingParams{
		OrganizationID: in.OrganizationID,
		CustomerID:     in.CustomerID,
		FranchiseID:    in.FranchiseID,
		AsOf:           in.At,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.Validation(
				"No active rate card applies to this booking. Configure a default retail rate card or assign one to the customer.",
				map[string]any{"code": "RATE_CARD_NOT_FOUND"})
		}
		return nil, apierr.Internal(fmt.Errorf("resolve rate card: %w", err))
	}
	if row.Currency != string(in.Currency) {
		return nil, apierr.Validation("The rate card currency does not match the requested currency.",
			map[string]any{"rateCardCurrency": row.Currency, "requestedCurrency": string(in.Currency)})
	}
	return &resolvedCard{
		ID: row.ID, PublicID: row.PublicID, Code: row.Code, Scope: row.Scope,
		VersionID: row.VersionID, VersionPublicID: row.VersionPublicID, VersionNumber: row.VersionNumber,
	}, nil
}

// computeFreight prices the chargeable weight for the lane.
//
// An explicit weight slab wins when one covers the weight; otherwise the linear
// base-plus-additional model from the zone rate applies. Additional weight is
// charged per started step, matching how couriers actually bill.
func (e *Engine) computeFreight(
	ctx context.Context, in QuoteInput, card *resolvedCard,
	zoneRate dbgen.ZoneRate, haveZoneRate bool, chargeable int32,
) (int64, LineItem, error) {
	slab, err := e.q.FindWeightSlab(ctx, dbgen.FindWeightSlabParams{
		RateCardVersionID: card.VersionID, CourierServiceID: in.Service.ID,
		OriginZoneID: in.OriginZoneID, DestinationZoneID: in.DestZoneID,
		ChargeableWeightGrams: chargeable,
	})
	if err == nil {
		amount := slab.PriceMinor
		explanation := fmt.Sprintf("slab %dg-%s priced at %s",
			slab.FromWeightGrams, upperBound(slab.ToWeightGrams), formatMinor(slab.PriceMinor, in.Currency))
		// A slab with an open upper bound may still charge for weight beyond it.
		if slab.ToWeightGrams != nil && chargeable > *slab.ToWeightGrams &&
			slab.AdditionalStepGrams != nil && slab.AdditionalPriceMinor != nil {
			excess := int64(chargeable - *slab.ToWeightGrams)
			steps := (excess + int64(*slab.AdditionalStepGrams) - 1) / int64(*slab.AdditionalStepGrams)
			extra := money.ApplyPerUnit(*slab.AdditionalPriceMinor, steps)
			amount += extra
			explanation += fmt.Sprintf(" plus %d additional step(s) of %dg at %s",
				steps, *slab.AdditionalStepGrams, formatMinor(*slab.AdditionalPriceMinor, in.Currency))
		}
		return amount, LineItem{
			Kind: KindFreight, Code: "FREIGHT", Label: "Freight charge",
			AmountMinor: amount, BasisMinor: 0, RuleID: slab.PublicID,
			Explanation: explanation,
		}, nil
	}
	if !database.IsNoRows(err) {
		return 0, LineItem{}, apierr.Internal(fmt.Errorf("find weight slab: %w", err))
	}

	if !haveZoneRate {
		var stateRate struct {
			PublicID, StateName    string
			BaseWeight, StepWeight int32
			BaseCost, StepCost     int64
		}
		err := e.db.Pool.QueryRow(ctx, `
			SELECT r.public_id, s.name, r.base_weight_grams, r.base_cost_minor,
			       r.additional_step_grams, r.additional_cost_minor
			  FROM state_base_rates r JOIN states s ON s.id=r.state_id
			 WHERE r.organization_id=$1 AND r.state_id=$2 AND r.is_active
			   AND (r.courier_service_id=$3 OR r.courier_service_id IS NULL)
			 ORDER BY (r.courier_service_id IS NOT NULL) DESC LIMIT 1`,
			in.OrganizationID, in.DestStateID, in.Service.ID).Scan(
			&stateRate.PublicID, &stateRate.StateName, &stateRate.BaseWeight,
			&stateRate.BaseCost, &stateRate.StepWeight, &stateRate.StepCost)
		if err == nil {
			amount := stateRate.BaseCost
			explanation := fmt.Sprintf("%s state base cost for %dg at %s", stateRate.StateName,
				stateRate.BaseWeight, formatMinor(stateRate.BaseCost, in.Currency))
			if chargeable > stateRate.BaseWeight {
				excess := int64(chargeable - stateRate.BaseWeight)
				steps := (excess + int64(stateRate.StepWeight) - 1) / int64(stateRate.StepWeight)
				amount += money.ApplyPerUnit(stateRate.StepCost, steps)
				explanation += fmt.Sprintf(" plus %d additional weight step(s)", steps)
			}
			return amount, LineItem{Kind: KindFreight, Code: "STATE_BASE_FREIGHT",
				Label: "State base freight", AmountMinor: amount, RuleID: stateRate.PublicID,
				Explanation: explanation}, nil
		}
		if !database.IsNoRows(err) {
			return 0, LineItem{}, apierr.Internal(fmt.Errorf("find state base rate: %w", err))
		}
		return 0, LineItem{}, apierr.Validation("No rate is configured for this lane or destination state.",
			map[string]any{"code": "RATE_NOT_CONFIGURED", "serviceCode": in.Service.Code,
				"originZone": in.OriginZoneCode, "destZone": in.DestZoneCode})
	}

	amount := zoneRate.BasePriceMinor
	explanation := fmt.Sprintf("base %dg at %s",
		zoneRate.BaseWeightGrams, formatMinor(zoneRate.BasePriceMinor, in.Currency))
	if chargeable > zoneRate.BaseWeightGrams {
		excess := int64(chargeable - zoneRate.BaseWeightGrams)
		steps := (excess + int64(zoneRate.AdditionalStepGrams) - 1) / int64(zoneRate.AdditionalStepGrams)
		extra := money.ApplyPerUnit(zoneRate.AdditionalPriceMinor, steps)
		amount += extra
		explanation += fmt.Sprintf(" plus %d x %dg at %s = %s",
			steps, zoneRate.AdditionalStepGrams,
			formatMinor(zoneRate.AdditionalPriceMinor, in.Currency), formatMinor(extra, in.Currency))
	}
	return amount, LineItem{
		Kind: KindFreight, Code: "FREIGHT", Label: "Freight charge",
		AmountMinor: amount, RuleID: zoneRate.PublicID,
		Explanation: explanation,
	}, nil
}

// computeSurcharges applies the rate card's surcharge rules in priority order.
//
// Rules are applied in ascending priority so that a percentage rule based on
// FREIGHT_PLUS_SURCHARGES sees a running total that is itself deterministic.
func (e *Engine) computeSurcharges(
	ctx context.Context, in QuoteInput, versionID, freight int64, chargeable int32,
) (taxable, exempt int64, items []LineItem, err error) {
	rules, err := e.q.ListSurchargeRulesForVersion(ctx, dbgen.ListSurchargeRulesForVersionParams{
		RateCardVersionID: versionID, CourierServiceID: &in.Service.ID,
	})
	if err != nil {
		return 0, 0, nil, apierr.Internal(fmt.Errorf("list surcharge rules: %w", err))
	}
	running := freight
	for _, rule := range rules {
		if !matchesConditions(rule.Conditions, in, chargeable) {
			continue
		}
		basis := surchargeBasis(rule.AppliesTo, freight, running, in)
		amount, rate := applyCalc(rule.CalcType, rule.ValueMinor, rule.PercentageBp, basis, chargeable)
		amount = money.Clamp(amount, rule.MinAmountMinor, rule.MaxAmountMinor)
		if amount == 0 {
			continue
		}
		items = append(items, LineItem{
			Kind: KindSurcharge, Code: rule.Code, Label: rule.Name,
			AmountMinor: amount, BasisMinor: basis, Rate: rate, RuleID: rule.PublicID,
			Explanation: fmt.Sprintf("%s on %s of %s = %s",
				rate, humaniseBasis(rule.AppliesTo), formatMinor(basis, in.Currency),
				formatMinor(amount, in.Currency)),
		})
		running += amount
		if rule.IsTaxable {
			taxable += amount
		} else {
			exempt += amount
		}
	}
	return taxable, exempt, items, nil
}

// computeDiscounts applies discount rules.
//
// A non-stackable rule ends evaluation: the highest-priority matching discount
// is the only one applied. This is checked per rule rather than globally, so a
// card can mix a stackable loyalty discount with an exclusive promotion.
func (e *Engine) computeDiscounts(
	ctx context.Context, in QuoteInput, versionID, freight, surchargeTotal int64,
) (int64, []LineItem, error) {
	rules, err := e.q.ListDiscountRulesForVersion(ctx, dbgen.ListDiscountRulesForVersionParams{
		RateCardVersionID: versionID, CourierServiceID: &in.Service.ID,
	})
	if err != nil {
		return 0, nil, apierr.Internal(fmt.Errorf("list discount rules: %w", err))
	}
	var total int64
	var items []LineItem
	for _, rule := range rules {
		basis := freight
		if rule.AppliesTo == "FREIGHT_PLUS_SURCHARGES" {
			basis = freight + surchargeTotal
		}
		if basis < rule.MinSubtotalMinor {
			continue
		}
		var amount int64
		var rate string
		if rule.DiscountType == "PERCENTAGE" {
			bp := money.BasisPoints(*rule.PercentageBp)
			amount = money.ApplyBP(basis, bp)
			rate = fmt.Sprintf("%.2f%%", bp.Float())
		} else {
			amount = *rule.ValueMinor
			rate = formatMinor(amount, in.Currency)
		}
		if rule.MaxDiscountMinor != nil && amount > *rule.MaxDiscountMinor {
			amount = *rule.MaxDiscountMinor
		}
		// A discount can never exceed what is left to discount.
		if remaining := basis - total; amount > remaining {
			amount = remaining
		}
		if amount <= 0 {
			continue
		}
		items = append(items, LineItem{
			Kind: KindDiscount, Code: rule.Code, Label: rule.Name,
			AmountMinor: -amount, BasisMinor: basis, Rate: rate, RuleID: rule.PublicID,
			Explanation: fmt.Sprintf("%s off %s of %s = -%s",
				rate, humaniseBasis(rule.AppliesTo), formatMinor(basis, in.Currency),
				formatMinor(amount, in.Currency)),
		})
		total += amount
		if !rule.IsStackable {
			break
		}
	}
	return total, items, nil
}

// computeTax applies statutory tax rules to the taxable base.
func (e *Engine) computeTax(ctx context.Context, in QuoteInput, taxable int64) (int64, []LineItem, error) {
	if taxable == 0 {
		return 0, nil, nil
	}
	rules, err := e.q.ListApplicableTaxRules(ctx, dbgen.ListApplicableTaxRulesParams{
		// intra_state_only is nullable ("applies either way"), so the comparison
		// parameter is typed as a pointer.
		OrganizationID: in.OrganizationID, AsOf: in.At, IsIntraState: &in.IsIntraState,
	})
	if err != nil {
		return 0, nil, apierr.Internal(fmt.Errorf("list tax rules: %w", err))
	}
	var total int64
	var items []LineItem
	for _, rule := range rules {
		bp := money.BasisPoints(rule.PercentageBp)
		amount := money.ApplyBP(taxable, bp)
		if amount == 0 {
			continue
		}
		item := LineItem{
			Kind: KindTax, Code: rule.Code, Label: rule.Name,
			AmountMinor: amount, BasisMinor: taxable,
			Rate: fmt.Sprintf("%.2f%%", bp.Float()), RuleID: rule.PublicID,
			Explanation: fmt.Sprintf("%s %s on taxable value %s = %s",
				rule.TaxType, fmt.Sprintf("%.2f%%", bp.Float()),
				formatMinor(taxable, in.Currency), formatMinor(amount, in.Currency)),
		}
		items = append(items, item)
		total += amount
	}
	return total, items, nil
}

// ---- rule evaluation -------------------------------------------------------

// matchesConditions evaluates a rule's JSON policy against the shipment.
// An unparsable policy is treated as "does not match": a malformed rule must
// never silently apply a charge.
func matchesConditions(raw []byte, in QuoteInput, chargeable int32) bool {
	if len(raw) == 0 || string(raw) == "{}" {
		return true
	}
	var c conditions
	if err := json.Unmarshal(raw, &c); err != nil {
		return false
	}
	if c.RemoteOrigin != nil && *c.RemoteOrigin != in.OriginIsRemote {
		return false
	}
	if c.RemoteDestination != nil && *c.RemoteDestination != in.DestIsRemote {
		return false
	}
	if c.RemoteEither != nil && *c.RemoteEither != (in.OriginIsRemote || in.DestIsRemote) {
		return false
	}
	if len(c.PaymentModes) > 0 {
		found := false
		for _, m := range c.PaymentModes {
			if m == in.PaymentMode {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if c.MinWeightGrams != nil && chargeable < *c.MinWeightGrams {
		return false
	}
	if c.MaxWeightGrams != nil && chargeable > *c.MaxWeightGrams {
		return false
	}
	if c.MinDeclaredValueMinor != nil && in.DeclaredValueMinor < *c.MinDeclaredValueMinor {
		return false
	}
	if c.RequiresInsurance != nil && *c.RequiresInsurance != in.InsuranceRequired {
		return false
	}
	if c.IntraStateOnly != nil && *c.IntraStateOnly != in.IsIntraState {
		return false
	}
	return true
}

func surchargeBasis(appliesTo string, freight, running int64, in QuoteInput) int64 {
	switch appliesTo {
	case "FREIGHT_PLUS_SURCHARGES":
		return running
	case "DECLARED_VALUE":
		return in.DeclaredValueMinor
	case "COD_AMOUNT":
		return in.CODAmountMinor
	default:
		return freight
	}
}

func humaniseBasis(appliesTo string) string {
	switch appliesTo {
	case "FREIGHT_PLUS_SURCHARGES":
		return "freight plus surcharges"
	case "DECLARED_VALUE":
		return "declared value"
	case "COD_AMOUNT":
		return "COD amount"
	default:
		return "freight"
	}
}

// applyCalc computes an amount for one rule and returns a human-readable rate.
func applyCalc(calcType string, valueMinor *int64, percentageBP *int32, basis int64, chargeableGrams int32) (int64, string) {
	switch calcType {
	case "PERCENTAGE":
		if percentageBP == nil {
			return 0, ""
		}
		bp := money.BasisPoints(*percentageBP)
		return money.ApplyBP(basis, bp), fmt.Sprintf("%.2f%%", bp.Float())
	case "PER_KG":
		if valueMinor == nil {
			return 0, ""
		}
		// Charged per started kilogram of chargeable weight.
		kg := (int64(chargeableGrams) + 999) / 1000
		return money.ApplyPerUnit(*valueMinor, kg), fmt.Sprintf("per kg x %d", kg)
	default: // FIXED
		if valueMinor == nil {
			return 0, ""
		}
		return *valueMinor, "fixed"
	}
}

func upperBound(v *int32) string {
	if v == nil {
		return "and above"
	}
	return fmt.Sprintf("%dg", *v)
}

func formatMinor(v int64, c money.Currency) string {
	return string(c) + " " + money.FormatMinor(v, c.Exponent())
}

// RateCardDBID exposes the internal rate card id for the booking snapshot.
func (q *Quote) RateCardDBID() int64 { return q.rateCardDBID }

// RateCardVersionDBID exposes the internal version id for the booking snapshot.
func (q *Quote) RateCardVersionDBID() int64 { return q.rateCardVersionDBID }
