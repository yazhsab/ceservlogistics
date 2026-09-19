package shipment

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/pricing"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/tenant"
)

const maxCommercialMinor int64 = 1_000_000_000_000

type InsuranceAcceptance struct {
	Accepted         *bool  `json:"accepted"`
	QuoteFingerprint string `json:"quoteFingerprint,omitempty"`
}

type InsuranceDecision struct {
	Status     string                  `json:"status"`
	Quote      *pricing.InsuranceQuote `json:"quote,omitempty"`
	RecordedAt *time.Time              `json:"recordedAt,omitempty"`
	RecordedBy string                  `json:"recordedBy,omitempty"`
	ActorType  string                  `json:"actorType,omitempty"`
}

type BillingParty struct {
	Party      string `json:"party"`
	CustomerID string `json:"customerId,omitempty"`
}

type BillingInstructions struct {
	Transportation BillingParty `json:"transportation"`
	DutyTax        BillingParty `json:"dutyTax"`
}

type CustomsItem struct {
	Description     string `json:"description"`
	Quantity        int64  `json:"quantity"`
	UnitOfMeasure   string `json:"unitOfMeasure"`
	UnitValueMinor  int64  `json:"unitValueMinor"`
	CountryOfOrigin string `json:"countryOfOrigin"`
	HSCode          string `json:"hsCode,omitempty"`
}

type CustomsDeclaration struct {
	InvoiceNumber        string        `json:"invoiceNumber,omitempty"`
	DeclarationStatement string        `json:"declarationStatement,omitempty"`
	ReasonForExport      string        `json:"reasonForExport"`
	TermsOfSale          string        `json:"termsOfSale,omitempty"`
	Currency             string        `json:"currency"`
	Items                []CustomsItem `json:"items"`
	DiscountMinor        int64         `json:"discountMinor"`
	FreightMinor         int64         `json:"freightMinor"`
	OtherChargesMinor    int64         `json:"otherChargesMinor"`
}

type CustomsSummary struct {
	Declaration        CustomsDeclaration `json:"declaration"`
	LineTotalsMinor    []int64            `json:"lineTotalsMinor"`
	GoodsSubtotalMinor int64              `json:"goodsSubtotalMinor"`
	DeclaredValueMinor int64              `json:"declaredValueMinor"`
	InsuranceMinor     int64              `json:"insuranceMinor"`
	InvoiceTotalMinor  int64              `json:"invoiceTotalMinor"`
}

type CommercialSnapshot struct {
	Insurance InsuranceDecision   `json:"insurance"`
	Customs   *CustomsSummary     `json:"customs,omitempty"`
	Billing   BillingInstructions `json:"billing"`
}

type BookingPreview struct {
	Quote              *pricing.Quote         `json:"quote"`
	Serviceability     *serviceability.Result `json:"serviceability"`
	Commercial         *CommercialSnapshot    `json:"commercial"`
	DeclaredValueMinor int64                  `json:"declaredValueMinor"`
}

func validateCommercial(v *validate.Validator, req *BookingRequest) {
	if req.Billing == nil {
		req.Billing = &BillingInstructions{Transportation: BillingParty{Party: "SHIPPER"}, DutyTax: BillingParty{Party: "RECEIVER"}}
	}
	for field, party := range map[string]*BillingParty{"billing.transportation": &req.Billing.Transportation, "billing.dutyTax": &req.Billing.DutyTax} {
		party.Party = v.Enum(field+".party", party.Party, []string{"SHIPPER", "RECEIVER", "THIRD_PARTY"}, true)
		party.CustomerID = v.PublicID(field+".customerId", party.CustomerID, publicid.PrefixCustomer, party.Party == "THIRD_PARTY")
		if party.Party == "SHIPPER" && party.CustomerID != "" {
			v.Add(field+".customerId", "The shipper uses the booking customer account.")
		}
	}
	if req.InsuranceAcceptance != nil {
		if req.InsuranceAcceptance.Accepted == nil {
			v.Add("insuranceAcceptance.accepted", "Explicitly supply the customer's acceptance or decline.")
		} else if *req.InsuranceAcceptance.Accepted != req.InsuranceRequired {
			v.Add("insuranceAcceptance.accepted", "The insurance decision must match the insurance request.")
		}
	}
	if req.Customs == nil {
		return
	}
	c := req.Customs
	c.InvoiceNumber = v.Text("customs.invoiceNumber", c.InvoiceNumber, 0, 80, false)
	c.DeclarationStatement = v.Text("customs.declarationStatement", c.DeclarationStatement, 0, 2000, false)
	c.ReasonForExport = v.Text("customs.reasonForExport", c.ReasonForExport, 2, 100, true)
	c.TermsOfSale = v.Text("customs.termsOfSale", c.TermsOfSale, 0, 100, false)
	c.Currency = v.Enum("customs.currency", c.Currency, []string{"NGN", "INR", "USD"}, true)
	v.IntRange("customs.items", len(c.Items), 1, 100)
	var sum int64
	for i := range c.Items {
		item := &c.Items[i]
		field := fmt.Sprintf("customs.items[%d]", i)
		item.Description = v.Text(field+".description", item.Description, 2, 500, true)
		item.UnitOfMeasure = v.Text(field+".unitOfMeasure", item.UnitOfMeasure, 1, 20, true)
		item.CountryOfOrigin = v.CountryCode(field+".countryOfOrigin", item.CountryOfOrigin, true)
		item.HSCode = v.Text(field+".hsCode", item.HSCode, 0, 20, false)
		v.Int64Range(field+".quantity", item.Quantity, 1, 1_000_000)
		v.NonNegativeMinor(field+".unitValueMinor", item.UnitValueMinor)
		if item.Quantity <= 0 || item.Quantity > 1_000_000 || item.UnitValueMinor < 0 {
			continue
		}
		if item.UnitValueMinor > (maxCommercialMinor-sum)/item.Quantity {
			v.Add(field+".unitValueMinor", "Goods total exceeds the permitted amount.")
			continue
		}
		sum += item.Quantity * item.UnitValueMinor
	}
	v.NonNegativeMinor("customs.discountMinor", c.DiscountMinor)
	v.NonNegativeMinor("customs.freightMinor", c.FreightMinor)
	v.NonNegativeMinor("customs.otherChargesMinor", c.OtherChargesMinor)
	if c.DiscountMinor > sum {
		v.Add("customs.discountMinor", "Discount cannot exceed the goods subtotal.")
		return
	}
	value := sum - c.DiscountMinor
	if req.DeclaredValueMinor != 0 && req.DeclaredValueMinor != value {
		v.Add("declaredValueMinor", "Must equal the customs goods subtotal after discount.")
	}
	req.DeclaredValueMinor = value
}

func (b *Booker) prepareCommercial(ctx context.Context, prep *prepared) error {
	req, p := prep.request, prep.principal
	snapshot := &CommercialSnapshot{Billing: *req.Billing, Insurance: InsuranceDecision{Status: "NOT_REQUESTED"}}
	if req.InsuranceRequired {
		snapshot.Insurance.Status = "AWAITING_ACCEPTANCE"
		snapshot.Insurance.Quote = prep.quote.Insurance
	}
	if req.Billing.Transportation.Party == "SHIPPER" {
		prep.billingCustomer = prep.customer
	}
	for index, party := range []BillingParty{req.Billing.Transportation, req.Billing.DutyTax} {
		if party.CustomerID == "" {
			continue
		}
		account, err := b.customer.ResolveForBooking(ctx, p.OrganizationID, party.CustomerID)
		if err != nil {
			return err
		}
		if !p.IsCustomerInScope(account.Customer.ID) {
			return apierr.NotFound("Billing customer")
		}
		if index == 0 {
			prep.billingCustomer = account
		}
		if account.Customer.Status != "ACTIVE" {
			return apierr.Conflict("BILLING_ACCOUNT_INACTIVE", "The third-party billing account is not active.")
		}
	}
	if req.PaymentMode == "CREDIT" && prep.billingCustomer == nil {
		return apierr.Validation("Select the receiver billing account before booking transportation on credit.", nil)
	}
	if req.Customs != nil {
		c := req.Customs
		if c.Currency != string(prep.quote.Currency) {
			return apierr.Validation("Customs values must use the shipment currency; currency conversion is not supported.", nil)
		}
		var premium int64
		if prep.quote.Insurance != nil {
			premium = prep.quote.Insurance.PremiumMinor
		}
		result, err := b.customsSummary(ctx, &req, premium)
		if err != nil {
			return err
		}

		snapshot.Customs = result
	}
	prep.commercial = snapshot
	return nil
}

// CustomsValuationPreview deliberately excludes insurance: only a full shipment
// quote can establish that premium, eligibility and currency compatibility.
type CustomsValuationPreview struct {
	Currency                  string  `json:"currency"`
	LineTotalsMinor           []int64 `json:"lineTotalsMinor"`
	GoodsSubtotalMinor        int64   `json:"goodsSubtotalMinor"`
	DeclaredValueMinor        int64   `json:"declaredValueMinor"`
	TotalBeforeInsuranceMinor int64   `json:"totalBeforeInsuranceMinor"`
}

// calculateCustomsSummary accepts commercial inputs validated by validateCommercial.
// Both standalone valuation and final booking use this same calculation.
func calculateCustomsSummary(req *BookingRequest, premium int64) (*CustomsSummary, error) {
	c := req.Customs
	result := &CustomsSummary{Declaration: *c, DeclaredValueMinor: req.DeclaredValueMinor, InsuranceMinor: premium, LineTotalsMinor: []int64{}}
	for _, item := range c.Items {
		amount := item.Quantity * item.UnitValueMinor
		result.LineTotalsMinor = append(result.LineTotalsMinor, amount)
		result.GoodsSubtotalMinor += amount
	}
	// Check before addition to reject overflow even for a very large charge.
	total := result.DeclaredValueMinor
	for _, charge := range []int64{c.FreightMinor, c.OtherChargesMinor, premium} {
		if charge < 0 || charge > maxCommercialMinor-total {
			return nil, apierr.Validation("Customs invoice total exceeds the permitted amount.", nil)
		}
		total += charge
	}
	result.InvoiceTotalMinor = total
	return result, nil
}

func (b *Booker) customsSummary(ctx context.Context, req *BookingRequest, premium int64) (*CustomsSummary, error) {
	for _, item := range req.Customs.Items {
		country, err := b.q.GetCountryByISO2(ctx, item.CountryOfOrigin)
		if err != nil || country.Status != "ACTIVE" {
			return nil, apierr.Validation("A goods country of origin is not configured or active.", nil)
		}
	}
	return calculateCustomsSummary(req, premium)
}

func (h *Handler) previewCustoms(w http.ResponseWriter, r *http.Request) error {
	if _, err := tenant.Require(r); err != nil {
		return err
	}
	var customs CustomsDeclaration
	if err := httpx.DecodeJSON(w, r, &customs); err != nil {
		return err
	}
	req := &BookingRequest{Customs: &customs}
	v := validate.New()
	validateCommercial(v, req)
	if err := v.Err(); err != nil {
		return err
	}
	result, err := h.booker.customsSummary(r.Context(), req, 0)
	if err != nil {
		return err
	}
	return httpx.OK(w, CustomsValuationPreview{
		Currency:                  customs.Currency,
		LineTotalsMinor:           result.LineTotalsMinor,
		GoodsSubtotalMinor:        result.GoodsSubtotalMinor,
		DeclaredValueMinor:        result.DeclaredValueMinor,
		TotalBeforeInsuranceMinor: result.InvoiceTotalMinor,
	})
}

func acceptInsurance(prep *prepared) error {
	decision := &prep.commercial.Insurance
	acceptance := prep.request.InsuranceAcceptance
	if prep.request.InsuranceRequired {
		if acceptance == nil || acceptance.Accepted == nil || !*acceptance.Accepted {
			return apierr.Conflict("INSURANCE_ACCEPTANCE_REQUIRED", "Confirm the customer's acceptance of the quoted insurance premium before booking.")
		}
		if prep.quote.Insurance == nil || subtle.ConstantTimeCompare([]byte(acceptance.QuoteFingerprint), []byte(prep.quote.Insurance.QuoteFingerprint)) != 1 {
			return apierr.Conflict("INSURANCE_QUOTE_CHANGED", "The insurance quote changed. Preview again and confirm the customer's acceptance of the updated premium.")
		}
		decision.Status = "ACCEPTED"
	} else if acceptance != nil {
		decision.Status = "DECLINED"
	}
	if acceptance != nil {
		decision.RecordedAt = &prep.at
		decision.ActorType = actorTypeFor(prep.principal, false)
		decision.RecordedBy = prep.principal.UserPublicID
		if prep.principal.IsPartner {
			decision.RecordedBy = prep.principal.PartnerKeyName
		}
	}
	return nil
}

func writeCommercial(ctx context.Context, q *dbgen.Queries, prep *prepared, s dbgen.Shipment) error {
	insurance, err := json.Marshal(prep.commercial.Insurance)
	if err != nil {
		return apierr.Internal(err)
	}
	billing, err := json.Marshal(prep.commercial.Billing)
	if err != nil {
		return apierr.Internal(err)
	}
	var customs []byte
	if prep.commercial.Customs != nil {
		customs, err = json.Marshal(prep.commercial.Customs)
		if err != nil {
			return apierr.Internal(err)
		}
	}
	var payer *int64
	if prep.billingCustomer != nil {
		payer = &prep.billingCustomer.Customer.ID
	}
	if err := q.CreateShipmentCommercialSnapshot(ctx, dbgen.CreateShipmentCommercialSnapshotParams{OrganizationID: s.OrganizationID, ShipmentID: s.ID, Insurance: insurance, Billing: billing, Customs: customs, TransportCustomerID: payer}); err != nil {
		return apierr.Internal(err)
	}
	return nil
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req BookingRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	preview, err := h.booker.Preview(r.Context(), p, req)
	if err != nil {
		return err
	}
	return httpx.OK(w, preview)
}

// Preview runs the same validation and calculation as booking without allocating an AWB.
func (b *Booker) Preview(ctx context.Context, p *tenant.Principal, req BookingRequest) (*BookingPreview, error) {
	prep, err := b.prepare(ctx, p, req)
	if err != nil {
		return nil, err
	}
	if p.IsPartner {
		prep.routing.Explanation = nil
		prep.routing.ExplanationID = ""
	}
	return &BookingPreview{Quote: prep.quote, Serviceability: prep.routing, Commercial: prep.commercial, DeclaredValueMinor: prep.request.DeclaredValueMinor}, nil
}
