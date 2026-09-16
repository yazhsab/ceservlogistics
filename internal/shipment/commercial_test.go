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
