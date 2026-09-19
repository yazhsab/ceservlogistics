package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/tests/harness"
)

// Test fixtures configure rates explicitly; there is no production fallback.
func configureCommercialInsurance(t *testing.T, env *harness.Env, tn *harness.Tenant, bp int32) string {
	t.Helper()
	return configureCommercialInsuranceRules(t, env, tn, map[string]any{"code": "COVER", "name": "Shipment insurance", "surchargeType": "INSURANCE", "calcType": "PERCENTAGE", "percentageBp": bp, "appliesTo": "DECLARED_VALUE", "priority": 40})
}

func configureCommercialInsuranceRules(t *testing.T, env *harness.Env, tn *harness.Tenant, rules ...map[string]any) string {
	t.Helper()
	ctx := context.Background()
	var versionID string
	if err := env.DB.Pool.QueryRow(ctx, `UPDATE rate_card_versions SET status='DRAFT' WHERE id=$1 RETURNING public_id`, tn.RateCardVersionID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		response := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+versionID+"/surcharges", tn.AdminAccessTok, rule)
		if response.Status != 201 {
			t.Fatalf("configure insurance: %d %s", response.Status, response.Raw)
		}
	}
	active := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+versionID+"/activate", tn.AdminAccessTok, nil)
	if active.Status != 200 {
		t.Fatalf("activate insurance: %d %s", active.Status, active.Raw)
	}
	return versionID
}

func commercialBooking(tn *harness.Tenant) map[string]any {
	return tn.BookingBody(map[string]any{
		"insuranceRequired": true,
		"customs": map[string]any{"currency": "NGN", "reasonForExport": "Sale", "discountMinor": 50000, "freightMinor": 10000, "otherChargesMinor": 2000,
			"items": []any{map[string]any{"description": "Cotton shirts", "quantity": 2, "unitOfMeasure": "PCS", "unitValueMinor": 250000, "countryOfOrigin": "NG", "hsCode": "610910"}}},
		"billing": map[string]any{"transportation": map[string]any{"party": "SHIPPER"}, "dutyTax": map[string]any{"party": "RECEIVER"}},
	})
}

func acceptPreview(t *testing.T, env *harness.Env, tn *harness.Tenant, body map[string]any) map[string]any {
	t.Helper()
	preview := env.Do(t, http.MethodPost, "/api/v1/shipments/preview", tn.AdminAccessTok, body)
	if preview.Status != http.StatusOK {
		t.Fatalf("preview %d: %s", preview.Status, preview.Raw)
	}
	quote := preview.Body["quote"].(map[string]any)
	insurance := quote["insurance"].(map[string]any)
	body["insuranceAcceptance"] = map[string]any{"accepted": true, "quoteFingerprint": insurance["quoteFingerprint"]}
	return preview.Body
}

func TestCommercialBookingPersistsExactConsentAndCustoms(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "COMM"})
	configureCommercialInsurance(t, env, tn, 100)
	body := commercialBooking(tn)
	preview := acceptPreview(t, env, tn, body)
	commercial := preview["commercial"].(map[string]any)
	customs := commercial["customs"].(map[string]any)
	for field, want := range map[string]float64{"goodsSubtotalMinor": 500000, "declaredValueMinor": 450000, "insuranceMinor": 4500, "invoiceTotalMinor": 466500} {
		if customs[field] != want {
			t.Fatalf("%s = %v want %v", field, customs[field], want)
		}
	}
	key := [2]string{"Idempotency-Key", harness.RandomKey()}
	created := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body, key)
	if created.Status != 201 {
		t.Fatalf("book %d: %s", created.Status, created.Raw)
	}
	id := created.Body["id"].(string)
	stored := created.Body["commercial"].(map[string]any)
	decision := stored["insurance"].(map[string]any)
	if decision["status"] != "ACCEPTED" || decision["recordedBy"] == nil || decision["recordedAt"] == nil {
		t.Fatalf("missing consent evidence: %+v", decision)
	}
	if decision["quote"].(map[string]any)["premiumMinor"] != float64(4500) {
		t.Fatal("wrong premium")
	}
	replayed := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body, key)
	if replayed.Status != 201 || replayed.Body["id"] != id || replayed.Headers.Get("Idempotent-Replay") != "true" {
		t.Fatal("commercial booking replay duplicated")
	}
	read := env.Do(t, "GET", "/api/v1/shipments/"+id, tn.AdminAccessTok, nil)
	if read.Status != 200 || read.Body["commercial"] == nil {
		t.Fatal("snapshot missing on read")
	}
	if _, err := env.DB.Pool.Exec(context.Background(), `UPDATE shipment_commercial_snapshots SET insurance = '{}'`); err == nil {
		t.Fatal("consent can be mutated")
	}
	if _, err := env.DB.Pool.Exec(context.Background(), `DELETE FROM shipment_commercial_snapshots`); err == nil {
		t.Fatal("consent can be deleted")
	}
	other := env.NewTenant(t, geo, harness.TenantOptions{Code: "OTHER"})
	if got := env.Do(t, "GET", "/api/v1/shipments/"+id, other.AdminAccessTok, nil); got.Status != 404 {
		t.Fatal("cross-tenant commercial read")
	}
	if got := env.Do(t, "POST", "/api/v1/shipments/preview", "", body); got.Status != 401 {
		t.Fatal("unauthenticated commercial preview")
	}
}

func TestCommercialInsuranceRequiresFreshAcceptanceAndEligibleService(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "CONSENT"})
	configureCommercialInsurance(t, env, tn, 100)
	body := commercialBooking(tn)
	if got := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body); got.ErrorCode() != "INSURANCE_ACCEPTANCE_REQUIRED" {
		t.Fatalf("missing acceptance: %d %s", got.Status, got.Raw)
	}
	acceptPreview(t, env, tn, body)
	body["customs"].(map[string]any)["discountMinor"] = float64(40000)
	if got := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body); got.ErrorCode() != "INSURANCE_QUOTE_CHANGED" {
		t.Fatalf("stale consent: %d %s", got.Status, got.Raw)
	}
	if _, err := env.DB.Pool.Exec(context.Background(), `UPDATE courier_services SET insurance_allowed=false WHERE id=$1`, tn.ServiceID); err != nil {
		t.Fatal(err)
	}
	if got := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body); got.ErrorCode() != "INSURANCE_NOT_AVAILABLE" {
		t.Fatalf("ineligible: %d %s", got.Status, got.Raw)
	}
	var count int
	if err := env.DB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM shipments WHERE organization_id=$1`, tn.OrgID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed validation created shipments: %d %v", count, err)
	}
}

func TestCommercialBillingChoicesAndThirdPartyIsolation(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PAYERS"})
	other := env.NewTenant(t, geo, harness.TenantOptions{Code: "FOREIGN"})
	for _, transport := range []string{"SHIPPER", "RECEIVER", "THIRD_PARTY"} {
		for _, duty := range []string{"SHIPPER", "RECEIVER", "THIRD_PARTY"} {
			party := func(kind string) map[string]any {
				p := map[string]any{"party": kind}
				if kind == "THIRD_PARTY" {
					p["customerId"] = tn.CustomerPublicID
				}
				return p
			}
			body := tn.BookingBody(map[string]any{"billing": map[string]any{"transportation": party(transport), "dutyTax": party(duty)}, "insuranceAcceptance": map[string]any{"accepted": false}})
			got := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
			if got.Status != 201 {
				t.Fatalf("%s/%s: %d %s", transport, duty, got.Status, got.Raw)
			}
			commercial := got.Body["commercial"].(map[string]any)
			if commercial["insurance"].(map[string]any)["status"] != "DECLINED" {
				t.Fatal("decline not recorded")
			}
			billing := commercial["billing"].(map[string]any)
			if billing["transportation"].(map[string]any)["party"] != transport || billing["dutyTax"].(map[string]any)["party"] != duty {
				t.Fatal("payer selections lost")
			}
		}
	}
	for _, customer := range []string{"", other.CustomerPublicID} {
		body := tn.BookingBody(map[string]any{"billing": map[string]any{"transportation": map[string]any{"party": "THIRD_PARTY", "customerId": customer}, "dutyTax": map[string]any{"party": "SHIPPER"}}})
		got := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body)
		if got.Status != 404 && got.Status != 422 {
			t.Fatalf("foreign/missing account: %d %s", got.Status, got.Raw)
		}
	}
}

func TestCommercialInternationalPreviewPreservesCountryAndPostcode(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "INTL"})
	configureCommercialInsurance(t, env, tn, 100)
	ctx := context.Background()
	country, err := env.Queries.UpsertCountry(ctx, dbgen.UpsertCountryParams{PublicID: publicid.New("cnt"), Iso2: "GB", Iso3: "GBR", Name: "United Kingdom", PhoneCode: "+44", Currency: "GBP"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := env.Queries.UpsertState(ctx, dbgen.UpsertStateParams{PublicID: publicid.New("stt"), CountryID: country.ID, Code: "ESS", Name: "Essex"})
	if err != nil {
		t.Fatal(err)
	}
	code, err := env.Queries.UpsertPincode(ctx, dbgen.UpsertPincodeParams{PublicID: publicid.New("pin"), CountryID: country.ID, StateID: state.ID, Code: "SS0 7JJ"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.Queries.UpsertPincodeZoneMapping(ctx, dbgen.UpsertPincodeZoneMappingParams{PublicID: publicid.New(publicid.PrefixZoneMapping), OrganizationID: tn.OrgID, PincodeID: code.ID, ZoneID: tn.ZoneNationalID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.Queries.CreateServiceArea(ctx, dbgen.CreateServiceAreaParams{PublicID: publicid.New(publicid.PrefixServiceArea), OrganizationID: tn.OrgID, OperatingUnitID: tn.DestBranchID, PincodeID: code.ID, AreaType: "DELIVERY", Priority: 100})
	if err != nil {
		t.Fatal(err)
	}
	saved := env.Do(t, "POST", "/api/v1/customers/"+tn.CustomerPublicID+"/addresses", tn.AdminAccessTok, map[string]any{"label": "UK recipient", "addressType": "DELIVERY", "contactName": "UK Recipient", "contactPhone": "+441234567890", "line1": "10 Example Road", "countryCode": "GB", "pincode": "ss07jj"})
	if saved.Status != 201 || saved.Body["countryCode"] != "GB" || saved.Body["pincode"] != "SS0 7JJ" {
		t.Fatalf("saved international address: %d %s", saved.Status, saved.Raw)
	}
	savedID := saved.Body["id"].(string)
	changed := env.Do(t, "PATCH", "/api/v1/customers/"+tn.CustomerPublicID+"/addresses/"+savedID, tn.AdminAccessTok, map[string]any{"countryCode": "NG", "pincode": tn.OriginPincode})
	if changed.Status != 200 || changed.Body["countryCode"] != "NG" {
		t.Fatalf("address country update: %d %s", changed.Status, changed.Raw)
	}
	body := commercialBooking(tn)
	recipient := body["recipient"].(map[string]any)
	recipient["countryCode"] = "gb"
	recipient["pincode"] = "ss07jj"
	recipient["city"] = "Southend-on-Sea"
	recipient["state"] = "Essex"
	preview := acceptPreview(t, env, tn, body)
	destination := preview["serviceability"].(map[string]any)["destination"].(map[string]any)
	if destination["countryCode"] != "GB" || destination["pincode"] != "SS0 7JJ" {
		t.Fatalf("country lost: %+v", destination)
	}
	got := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
	if got.Status != 201 {
		t.Fatalf("international booking: %d %s", got.Status, got.Raw)
	}
	address := got.Body["addresses"].(map[string]any)["recipient"].(map[string]any)
	if address["countryCode"] != "GB" || address["pincode"] != "SS0 7JJ" {
		t.Fatal("address snapshot lost country/postcode")
	}
	recipient["countryCode"] = "NG"
	if got := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body); got.Status != 422 {
		t.Fatalf("wrong country accepted postcode: %d %s", got.Status, got.Raw)
	}
}

func TestCommercialTransportationAccountOwnsCreditAndInvoice(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "BILLTO"})
	ctx := context.Background()
	payer, err := env.Queries.CreateCustomer(ctx, dbgen.CreateCustomerParams{PublicID: publicid.New(publicid.PrefixCustomer), OrganizationID: tn.OrgID, Code: "PAYER", CustomerType: "BUSINESS", Name: "Transport Payer", Phone: "+2348012345678", Metadata: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Queries.UpsertCreditProfile(ctx, dbgen.UpsertCreditProfileParams{PublicID: publicid.New(publicid.PrefixCreditProfile), OrganizationID: tn.OrgID, CustomerID: payer.ID, Currency: "NGN", CreditLimitMinor: 10000000, PaymentTermsDays: 30, CreditStatus: "GOOD"}); err != nil {
		t.Fatal(err)
	}
	body := tn.BookingBody(map[string]any{"paymentMode": "CREDIT", "billing": map[string]any{"transportation": map[string]any{"party": "THIRD_PARTY", "customerId": payer.PublicID}, "dutyTax": map[string]any{"party": "SHIPPER"}}})
	got := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
	if got.Status != 201 {
		t.Fatalf("third-party credit booking: %d %s", got.Status, got.Raw)
	}
	id := got.Body["id"].(string)
	profile, err := env.Queries.GetCreditProfileByCustomer(ctx, payer.ID)
	if err != nil || float64(profile.CreditUsedMinor) != got.Body["totalAmountMinor"] {
		t.Fatalf("wrong account reservation: %v %s", err, got.Raw)
	}
	for _, account := range []int64{payer.ID, tn.CustomerID} {
		rows, err := env.Queries.ListBillableShipments(ctx, dbgen.ListBillableShipmentsParams{OrganizationID: tn.OrgID, CustomerID: account, PeriodStart: time.Now().Add(-time.Hour), PeriodEnd: time.Now().Add(time.Hour), Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if account == payer.ID {
			want = 1
		}
		if len(rows) != want {
			t.Fatalf("invoice account %d has %d shipments want %d", account, len(rows), want)
		}
	}
	cancelled := env.Do(t, "POST", "/api/v1/shipments/"+id+"/cancel", tn.AdminAccessTok, map[string]any{"reason": "Customer cancelled before pickup"})
	if cancelled.Status != 200 {
		t.Fatalf("cancel %d: %s", cancelled.Status, cancelled.Raw)
	}
	profile, err = env.Queries.GetCreditProfileByCustomer(ctx, payer.ID)
	if err != nil || profile.CreditUsedMinor != 0 {
		t.Fatalf("credit was not released: %+v %v", profile, err)
	}
}

func TestCommercialPartnerPreviewRequiresScopeAndRecordsKeyAcceptance(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "PARTC"})
	configureCommercialInsurance(t, env, tn, 100)
	token, _ := issueKey(t, env, tn, "commercial partner", []string{partner.ScopeShipmentCreate})
	denied, _ := issueKey(t, env, tn, "read only partner", []string{partner.ScopeShipmentRead})
	body := commercialBooking(tn)
	if got := env.Do(t, "POST", "/api/v1/partner/shipments/preview", denied, body); got.Status != 403 {
		t.Fatalf("unscoped preview %d", got.Status)
	}
	preview := env.Do(t, "POST", "/api/v1/partner/shipments/preview", token, body)
	if preview.Status != 200 {
		t.Fatalf("partner preview %d: %s", preview.Status, preview.Raw)
	}
	route := preview.Body["serviceability"].(map[string]any)
	if route["explanation"] != nil {
		t.Fatal("partner received internal routing explanation")
	}
	body["insuranceAcceptance"] = map[string]any{"accepted": true, "quoteFingerprint": preview.Body["quote"].(map[string]any)["insurance"].(map[string]any)["quoteFingerprint"]}
	got := env.Do(t, "POST", "/api/v1/partner/shipments", token, body, [2]string{"Idempotency-Key", harness.RandomKey()})
	if got.Status != 201 {
		t.Fatalf("partner book %d: %s", got.Status, got.Raw)
	}
	decision := got.Body["commercial"].(map[string]any)["insurance"].(map[string]any)
	if decision["status"] != "ACCEPTED" || decision["recordedBy"] != "commercial partner" || decision["actorType"] != "PARTNER" {
		t.Fatalf("wrong partner evidence %+v", decision)
	}
}

func TestCommercialInsuranceUsesExistingRulesWithoutApplyingShippingDiscount(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "LEGACYI"})
	ctx := context.Background()
	// Temporarily draft the fixture version to seed a historical insurance rule.
	if _, err := env.DB.Pool.Exec(ctx, `UPDATE rate_card_versions SET status='DRAFT' WHERE id=$1`, tn.RateCardVersionID); err != nil {
		t.Fatal(err)
	}
	bp := int32(900)
	if _, err := env.Queries.CreateSurchargeRule(ctx, dbgen.CreateSurchargeRuleParams{PublicID: publicid.New(publicid.PrefixSurchargeRule), OrganizationID: tn.OrgID, RateCardVersionID: tn.RateCardVersionID, Code: "OLD_COVER", Name: "Legacy insurance", SurchargeType: "INSURANCE", CalcType: "PERCENTAGE", PercentageBp: &bp, AppliesTo: "DECLARED_VALUE", Conditions: []byte("{}"), IsTaxable: true}); err != nil {
		t.Fatal(err)
	}
	bp = 10000
	if _, err := env.Queries.CreateDiscountRule(ctx, dbgen.CreateDiscountRuleParams{PublicID: publicid.New(publicid.PrefixDiscountRule), OrganizationID: tn.OrgID, RateCardVersionID: tn.RateCardVersionID, Code: "FREE_SHIP", Name: "Full shipping discount", DiscountType: "PERCENTAGE", PercentageBp: &bp, AppliesTo: "FREIGHT_PLUS_SURCHARGES", Conditions: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.DB.Pool.Exec(ctx, `UPDATE rate_card_versions SET status='ACTIVE' WHERE id=$1`, tn.RateCardVersionID); err != nil {
		t.Fatal(err)
	}
	preview := acceptPreview(t, env, tn, commercialBooking(tn))
	quote := preview["quote"].(map[string]any)
	if quote["taxableMinor"] != float64(40500) {
		t.Fatalf("shipping discount reduced insurance: %+v", quote)
	}
	count := 0
	for _, line := range quote["lineItems"].([]any) {
		item := line.(map[string]any)
		if item["code"] == "SHIPMENT_INSURANCE" {
			t.Fatal("a hardcoded insurance charge was appended")
		}
		if item["code"] == "OLD_COVER" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("insurance lines %d", count)
	}
	declined := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, tn.BookingBody(map[string]any{"declaredValueMinor": 450000}))
	if declined.Status != 200 || declined.Body["quote"].(map[string]any)["totalMinor"] != float64(0) {
		t.Fatalf("declined insurance charged: %s", declined.Raw)
	}
	empty := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(map[string]any{"insuranceAcceptance": map[string]any{}}))
	if empty.Status != 422 {
		t.Fatalf("empty object invented decline: %d %s", empty.Status, empty.Raw)
	}
	discounted := commercialBooking(tn)
	discounted["insuranceRequired"] = false
	discounted["customs"].(map[string]any)["discountMinor"] = 500000
	discounted["packages"] = []any{map[string]any{"actualWeightGrams": 500, "declaredValueMinor": 500000}}
	mismatched := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, discounted)
	if mismatched.Status != 422 {
		t.Fatalf("package value overrode customs net value: %d %s", mismatched.Status, mismatched.Raw)
	}

}

func TestCommercialInsuranceConfigurationAndVersionChange(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CONFIG"})
	body := commercialBooking(tn)
	missing := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body)
	if missing.Status != 409 || missing.ErrorCode() != "INSURANCE_RATE_NOT_CONFIGURED" {
		t.Fatalf("missing rate must not imply 1%%: %d %s", missing.Status, missing.Raw)
	}
	versionID := configureCommercialInsurance(t, env, tn, 250)
	preview := acceptPreview(t, env, tn, body)
	offer := preview["quote"].(map[string]any)["insurance"].(map[string]any)
	if offer["premiumMinor"] != float64(11250) || offer["rateBp"] != float64(250) || offer["policyVersion"] != versionID {
		t.Fatalf("configured rate not used: %+v", offer)
	}
	rules := offer["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["code"] != "COVER" || rules[0].(map[string]any)["ruleId"] == "" {
		t.Fatalf("configured rule evidence missing: %+v", rules)
	}
	if preview["commercial"].(map[string]any)["customs"].(map[string]any)["invoiceTotalMinor"] != float64(473250) {
		t.Fatal("customs did not use configured premium")
	}
	booked := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
	if booked.Status != 201 {
		t.Fatalf("book: %d %s", booked.Status, booked.Raw)
	}
	if got := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+versionID+"/surcharges", tn.AdminAccessTok, map[string]any{}); got.Status != 409 {
		t.Fatalf("published rate version editable: %d %s", got.Status, got.Raw)
	}

	other := env.NewTenant(t, geo, harness.TenantOptions{Code: "RATEOTHER"})
	configureCommercialInsurance(t, env, other, 375)
	otherQuote := acceptPreview(t, env, other, commercialBooking(other))["quote"].(map[string]any)["insurance"].(map[string]any)
	if otherQuote["premiumMinor"] != float64(16875) || otherQuote["rateBp"] != float64(375) {
		t.Fatalf("tenant rate leaked: %+v", otherQuote)
	}

	// Publish a real new version through the administration API. Previously
	// accepted terms must expire without rewriting an already booked shipment.
	var cardID string
	if err := env.DB.Pool.QueryRow(context.Background(), `SELECT public_id FROM rate_cards WHERE id=$1`, tn.RateCardID).Scan(&cardID); err != nil {
		t.Fatal(err)
	}
	draft := env.Do(t, "POST", "/api/v1/rate-cards/"+cardID+"/versions", tn.AdminAccessTok, map[string]any{"effectiveFrom": time.Now().Add(-time.Minute)})
	if draft.Status != 201 {
		t.Fatalf("draft: %d %s", draft.Status, draft.Raw)
	}
	nextID := draft.Body["id"].(string)
	rate := env.Do(t, "PUT", "/api/v1/rate-cards/versions/"+nextID+"/zone-rates", tn.AdminAccessTok, map[string]any{"serviceCode": tn.ServiceCode, "originZoneCode": "LOCAL", "destinationZoneCode": "NATIONAL", "baseWeightGrams": 500, "basePriceMinor": 10000, "additionalStepGrams": 500})
	if rate.Status != 200 {
		t.Fatalf("lane: %d %s", rate.Status, rate.Raw)
	}
	rule := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+nextID+"/surcharges", tn.AdminAccessTok, map[string]any{"code": "COVER", "name": "Revised cover", "surchargeType": "INSURANCE", "calcType": "PERCENTAGE", "percentageBp": 375, "appliesTo": "DECLARED_VALUE"})
	if rule.Status != 201 {
		t.Fatalf("rule: %d %s", rule.Status, rule.Raw)
	}
	active := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+nextID+"/activate", tn.AdminAccessTok, nil)
	if active.Status != 200 {
		t.Fatalf("activate: %d %s", active.Status, active.Raw)
	}
	stale := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
	if stale.ErrorCode() != "INSURANCE_QUOTE_CHANGED" {
		t.Fatalf("old rate accepted: %d %s", stale.Status, stale.Raw)
	}
	revised := acceptPreview(t, env, tn, body)["quote"].(map[string]any)["insurance"].(map[string]any)
	if revised["premiumMinor"] != float64(16875) || revised["policyVersion"] != nextID || revised["quoteFingerprint"] == offer["quoteFingerprint"] {
		t.Fatalf("new version ignored: %+v", revised)
	}
	if got := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body); got.Status != 201 {
		t.Fatalf("reaccepted quote: %d %s", got.Status, got.Raw)
	}
	saved := env.Do(t, "GET", "/api/v1/shipments/"+booked.Body["id"].(string), tn.AdminAccessTok, nil)
	savedOffer := saved.Body["commercial"].(map[string]any)["insurance"].(map[string]any)["quote"].(map[string]any)
	if savedOffer["premiumMinor"] != float64(11250) || savedOffer["policyVersion"] != versionID {
		t.Fatalf("historical consent repriced: %+v", savedOffer)
	}
}

func TestCommercialInsuranceConfiguredAmountsAndTax(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	cases := []struct {
		name              string
		declared, premium int64
		config            map[string]any
		headline, taxable bool
	}{
		{"percentage", 450000, 11250, map[string]any{"percentageBp": 250}, true, true},
		{"round_down", 19, 0, map[string]any{"percentageBp": 250}, true, true},
		{"round_half_up", 20, 1, map[string]any{"percentageBp": 250}, true, true},
		{"minimum", 10000, 1000, map[string]any{"percentageBp": 250, "minAmountMinor": 1000}, false, true},
		{"maximum", 450000, 5000, map[string]any{"percentageBp": 250, "maxAmountMinor": 5000}, false, true},
		{"fixed_exempt", 450000, 3750, map[string]any{"calcType": "FIXED", "valueMinor": 3750, "isTaxable": false}, false, false},
		{"per_kg", 450000, 275, map[string]any{"calcType": "PER_KG", "valueMinor": 275}, false, true},
		{"explicit_zero", 450000, 0, map[string]any{"percentageBp": 0}, true, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tn := env.NewTenant(t, geo, harness.TenantOptions{Code: fmt.Sprintf("RULE%d", i)})
			config := map[string]any{"code": "COVER", "name": "Configured insurance", "surchargeType": "INSURANCE", "calcType": "PERCENTAGE", "appliesTo": "DECLARED_VALUE", "serviceCode": tn.ServiceCode}
			for key, value := range tc.config {
				config[key] = value
			}
			configureCommercialInsuranceRules(t, env, tn, config)
			body := tn.BookingBody(map[string]any{"insuranceRequired": true, "declaredValueMinor": tc.declared})
			quote := acceptPreview(t, env, tn, body)["quote"].(map[string]any)
			offer := quote["insurance"].(map[string]any)
			if offer["premiumMinor"] != float64(tc.premium) {
				t.Fatalf("amount: %+v", offer)
			}
			_, hasHeadline := offer["rateBp"]
			if hasHeadline != tc.headline {
				t.Fatalf("misleading headline: %+v", offer)
			}
			rule := offer["rules"].([]any)[0].(map[string]any)
			if rule["isTaxable"] != tc.taxable {
				t.Fatalf("tax setting lost: %+v", rule)
			}
			delete(body, "insuranceAcceptance")
			body["insuranceRequired"] = false
			uninsured := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body)
			if uninsured.Status != 200 {
				t.Fatalf("uninsured: %d %s", uninsured.Status, uninsured.Raw)
			}
			baseline := uninsured.Body["quote"].(map[string]any)
			if baseline["insurance"] != nil {
				t.Fatal("unrequested insurance charged")
			}
			taxableDelta := quote["taxableMinor"].(float64) - baseline["taxableMinor"].(float64)
			expectedTaxable := float64(0)
			if tc.taxable {
				expectedTaxable = float64(tc.premium)
			}
			if taxableDelta != expectedTaxable {
				t.Fatalf("tax base delta: %v want %v", taxableDelta, expectedTaxable)
			}
			taxDelta := quote["taxTotalMinor"].(float64) - baseline["taxTotalMinor"].(float64)
			if quote["totalMinor"].(float64)-baseline["totalMinor"].(float64) != float64(tc.premium)+taxDelta {
				t.Fatal("premium not charged exactly once")
			}
		})
	}
}

func TestCommercialInsuranceConditionsAndCompositeRules(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := env.NewTenant(t, env.Geography(t), harness.TenantOptions{Code: "COMPOSITE"})
	versionID := configureCommercialInsuranceRules(t, env, tn,
		map[string]any{"code": "COVER", "name": "Value cover", "surchargeType": "INSURANCE", "calcType": "PERCENTAGE", "percentageBp": 250, "appliesTo": "DECLARED_VALUE", "conditions": map[string]any{"minDeclaredValueMinor": 10000}},
		map[string]any{"code": "EXTRA", "name": "Additional cover", "surchargeType": "INSURANCE", "calcType": "FIXED", "valueMinor": 400, "appliesTo": "DECLARED_VALUE", "conditions": map[string]any{"minDeclaredValueMinor": 10000}},
	)
	body := tn.BookingBody(map[string]any{"insuranceRequired": true, "declaredValueMinor": 5000})
	if got := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body); got.ErrorCode() != "INSURANCE_RATE_NOT_CONFIGURED" {
		t.Fatalf("unmatched rule used: %d %s", got.Status, got.Raw)
	}
	body["declaredValueMinor"] = 20000
	offer := acceptPreview(t, env, tn, body)["quote"].(map[string]any)["insurance"].(map[string]any)
	if offer["premiumMinor"] != float64(900) || offer["rateBp"] != nil || len(offer["rules"].([]any)) != 2 {
		t.Fatalf("composite not preserved: %+v", offer)
	}
	// Publishing also protects the rule configuration at the storage layer.
	if _, err := env.DB.Pool.Exec(context.Background(), `UPDATE surcharge_rules SET courier_service_id=$1 WHERE rate_card_version_id=$2`, tn.ServiceID, tn.RateCardVersionID); err == nil {
		t.Fatal("active rules should be immutable")
	}
	read := env.Do(t, "GET", "/api/v1/rate-cards/versions/"+versionID, tn.AdminAccessTok, nil)
	for _, raw := range read.Body["surcharges"].([]any) {
		rule := raw.(map[string]any)
		if rule["type"] == "INSURANCE" && rule["conditions"].(map[string]any)["requiresInsurance"] != true {
			t.Fatal("admin API did not enforce explicit request")
		}
	}
}

func TestCustomsValuationBeforeRouteValidation(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CUSTOMSCALC"})
	body := commercialBooking(tn)
	declaration := body["customs"].(map[string]any)
	const path = "/api/v1/shipments/customs/preview"
	if got := env.Do(t, "POST", path, "", declaration); got.Status != 401 {
		t.Fatalf("unauthenticated valuation: %d", got.Status)
	}
	_, _, denied := env.NewUser(t, tn.OrgID, "no-booking@example.test", "", nil)
	if got := env.Do(t, "POST", path, denied, declaration); got.Status != 403 {
		t.Fatalf("unpermitted valuation: %d", got.Status)
	}
	got := env.Do(t, "POST", path, tn.AdminAccessTok, declaration)
	if got.Status != 200 {
		t.Fatalf("standalone valuation: %d %s", got.Status, got.Raw)
	}
	for field, want := range map[string]float64{"goodsSubtotalMinor": 500000, "declaredValueMinor": 450000, "totalBeforeInsuranceMinor": 462000} {
		if got.Body[field] != want {
			t.Fatalf("%s: %v want %v", field, got.Body[field], want)
		}
	}
	if got.Body["insuranceMinor"] != nil || got.Body["invoiceTotalMinor"] != nil {
		t.Fatal("standalone valuation claimed final insurance or invoice total")
	}
	body["sender"].(map[string]any)["pincode"] = "999999"
	failed := env.Do(t, "POST", "/api/v1/shipments/preview", tn.AdminAccessTok, body)
	if failed.Status != 422 {
		t.Fatalf("unconfigured postcode: %d %s", failed.Status, failed.Raw)
	}
	details := failed.Body["error"].(map[string]any)["details"].(map[string]any)
	if details["field"] != "sender.pincode" || details["postalCode"] != "999999" || details["countryCode"] != "NG" {
		t.Fatalf("postal details: %+v", details)
	}
	if got := env.Do(t, "POST", path, tn.AdminAccessTok, declaration); got.Status != 200 {
		t.Fatalf("valuation blocked by route: %d", got.Status)
	}
	declaration["discountMinor"] = 500001
	if got := env.Do(t, "POST", path, tn.AdminAccessTok, declaration); got.Status != 422 {
		t.Fatalf("excess discount accepted: %d", got.Status)
	}
	declaration["discountMinor"] = 0
	declaration["freightMinor"] = 1_000_000_000_000
	if got := env.Do(t, "POST", path, tn.AdminAccessTok, declaration); got.Status != 422 {
		t.Fatalf("excess total accepted: %d", got.Status)
	}
	var count int
	if err := env.DB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM shipments WHERE organization_id=$1`, tn.OrgID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("valuation wrote shipment: %d %v", count, err)
	}
}
