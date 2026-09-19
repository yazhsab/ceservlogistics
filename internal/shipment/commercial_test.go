package shipment

import (
	"testing"

	"github.com/ceserve/courier-os/internal/platform/validate"
)

func TestCustomsValidationDerivesGoodsValueAndRejectsOverflow(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		quantity, value, discount, declared int64
		valid                               bool
		want                                int64
	}{
		{"derived", 2, 250000, 50000, 0, true, 450000},
		{"mismatch", 2, 250000, 50000, 500000, false, 450000},
		{"excess discount", 1, 100, 101, 0, false, 0},
		{"overflow", 1000000, 9223372036854775807, 0, 0, false, 0},
		{"zero quantity", 0, 100, 0, 0, false, 0},
		{"negative amount", 1, -1, 0, 0, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := BookingRequest{DeclaredValueMinor: tc.declared, Customs: &CustomsDeclaration{Currency: "NGN", ReasonForExport: "Sale", DiscountMinor: tc.discount, Items: []CustomsItem{{Description: "Clothing", Quantity: tc.quantity, UnitOfMeasure: "PCS", UnitValueMinor: tc.value, CountryOfOrigin: "NG"}}}}
			v := validate.New()
			validateCommercial(v, &req)
			if (v.Err() == nil) != tc.valid {
				t.Fatalf("validation: %v", v.Err())
			}
			if tc.valid && req.DeclaredValueMinor != tc.want {
				t.Fatalf("declared %d", req.DeclaredValueMinor)
			}
		})
	}
}

func TestBillingPartiesAreIndependentAndThirdPartyRequiresAccount(t *testing.T) {
	req := BookingRequest{Billing: &BillingInstructions{Transportation: BillingParty{Party: "RECEIVER"}, DutyTax: BillingParty{Party: "SHIPPER"}}}
	v := validate.New()
	validateCommercial(v, &req)
	if v.Err() != nil {
		t.Fatal(v.Err())
	}
	req.Billing.Transportation.Party = "THIRD_PARTY"
	v = validate.New()
	validateCommercial(v, &req)
	if v.Err() == nil {
		t.Fatal("third-party account must be identified")
	}
}

func TestCustomsSummaryUsesSharedTotalsAndBounds(t *testing.T) {
	req := BookingRequest{Customs: &CustomsDeclaration{Currency: "NGN", ReasonForExport: "Sale", DiscountMinor: 50000, FreightMinor: 10000, OtherChargesMinor: 2000, Items: []CustomsItem{
		{Description: "Shirts", Quantity: 2, UnitOfMeasure: "PCS", UnitValueMinor: 250000, CountryOfOrigin: "NG"},
		{Description: "Books", Quantity: 3, UnitOfMeasure: "PCS", UnitValueMinor: 10000, CountryOfOrigin: "NG"},
	}}}
	v := validate.New()
	validateCommercial(v, &req)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}
	before, err := calculateCustomsSummary(&req, 0)
	if err != nil {
		t.Fatal(err)
	}
	final, err := calculateCustomsSummary(&req, 4800)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.LineTotalsMinor) != 2 || before.LineTotalsMinor[0] != 500000 || before.LineTotalsMinor[1] != 30000 || before.GoodsSubtotalMinor != 530000 || before.DeclaredValueMinor != 480000 || before.InvoiceTotalMinor != 492000 || final.InvoiceTotalMinor != 496800 {
		t.Fatalf("incorrect customs totals: before=%+v final=%+v", before, final)
	}
	if _, err := calculateCustomsSummary(&req, 9223372036854775807); err == nil {
		t.Fatal("overflowing premium accepted")
	}
	req.Customs.FreightMinor = maxCommercialMinor
	if _, err := calculateCustomsSummary(&req, 0); err == nil {
		t.Fatal("excess customs total accepted")
	}
}
