package pricing

import (
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/money"
)

func TestInsuranceEligibilityAndValue(t *testing.T) {
	in := QuoteInput{Service: &dbgen.CourierService{InsuranceAllowed: true}, InsuranceRequired: true, Currency: money.NGN, DeclaredValueMinor: 5000000}
	if err := validateInsuranceRequest(in); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int64{0, -1, 1_000_000_000_001} {
		in.DeclaredValueMinor = value
		if err := validateInsuranceRequest(in); err == nil {
			t.Fatalf("invalid value %d accepted", value)
		}
	}
	in.DeclaredValueMinor = 5000000
	in.Service.InsuranceAllowed = false
	if err := validateInsuranceRequest(in); err == nil {
		t.Fatal("ineligible service accepted insurance")
	}
	in.InsuranceRequired = false
	if err := validateInsuranceRequest(in); err != nil {
		t.Fatal("insurance policy affected an uninsured shipment")
	}
}

func TestInsuranceFingerprintTracksConfiguredRuleAndVersion(t *testing.T) {
	in := QuoteInput{OrganizationID: 1, Service: &dbgen.CourierService{InsuranceAllowed: true}, InsuranceRequired: true, DeclaredValueMinor: 5000000, Currency: money.NGN, At: time.Now()}
	makeQuote := func(total int64, version string, bp int32) *Quote {
		offer := &InsuranceQuote{PolicyVersion: version, DeclaredValueMinor: in.DeclaredValueMinor, Currency: "NGN"}
		offer.addRule(dbgen.SurchargeRule{PublicID: "sur_configured", CalcType: "PERCENTAGE", AppliesTo: "DECLARED_VALUE", PercentageBp: &bp}, in.DeclaredValueMinor, 125000, "configured insurance")
		q := &Quote{Insurance: offer, TotalMinor: total}
		fingerprintInsurance(q, in)
		return q
	}
	first := makeQuote(150000, "rcv_one", 250).Insurance.QuoteFingerprint
	in.At = in.At.Add(time.Minute)
	if makeQuote(150000, "rcv_one", 250).Insurance.QuoteFingerprint != first {
		t.Fatal("clock invalidated identical quote")
	}
	for _, q := range []*Quote{makeQuote(150001, "rcv_one", 250), makeQuote(150000, "rcv_two", 250), makeQuote(150000, "rcv_one", 350)} {
		if q.Insurance.QuoteFingerprint == first {
			t.Fatal("changed price, rule or version retained acceptance fingerprint")
		}
	}
	in.OrganizationID = 2
	if makeQuote(150000, "rcv_one", 250).Insurance.QuoteFingerprint == first {
		t.Fatal("fingerprint not tenant-bound")
	}
}

func TestInsuranceHeadlineDoesNotMisrepresentComplexRules(t *testing.T) {
	bp := int32(250)
	minimum := int64(100000)
	for _, rule := range []dbgen.SurchargeRule{
		{CalcType: "FIXED", AppliesTo: "DECLARED_VALUE", ValueMinor: &minimum},
		{CalcType: "PERCENTAGE", AppliesTo: "FREIGHT", PercentageBp: &bp},
		{CalcType: "PERCENTAGE", AppliesTo: "DECLARED_VALUE", PercentageBp: &bp, MinAmountMinor: &minimum},
	} {
		offer := &InsuranceQuote{}
		offer.addRule(rule, 1000000, 100000, "configured rule")
		if offer.RateBP != nil {
			t.Fatal("complex premium misrepresented as a declared-value percentage")
		}
	}
	offer := &InsuranceQuote{}
	rule := dbgen.SurchargeRule{CalcType: "PERCENTAGE", AppliesTo: "DECLARED_VALUE", PercentageBp: &bp}
	offer.addRule(rule, 1000000, 25000, "2.5%")
	if offer.RateBP == nil || *offer.RateBP != 250 {
		t.Fatal("configured percentage missing")
	}
	offer.addRule(rule, 1000000, 25000, "second rule")
	if offer.RateBP != nil || offer.PremiumMinor != 50000 {
		t.Fatal("composite quote lost its breakdown")
	}
}
