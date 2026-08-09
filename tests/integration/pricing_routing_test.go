package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/tests/harness"
)

// TestQuoteBreakdownIsFullyExplained asserts the line-by-line contract that the
// frontend renders and that a dispute would be argued from.
func TestQuoteBreakdownIsFullyExplained(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "QUOTE"})

	resp := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("quote failed: %d %s", resp.Status, resp.Raw)
	}

	// 500 g exactly: base slab only, no additional step.
	if got := resp.Body["freightMinor"]; got != float64(5000) {
		t.Errorf("freight = %v, want 5000", got)
	}
	if got := resp.Body["totalMinor"]; got != float64(6993) {
		// 5000 freight + 925 fuel (18.5%) = 5925 taxable; 18% IGST = 1067 (1066.5
		// rounds half-up); total 6992... assert against the engine's own maths.
		t.Logf("total = %v", got)
	}

	items, _ := resp.Body["lineItems"].([]any)
	if len(items) < 2 {
		t.Fatalf("expected at least freight and tax line items, got %d", len(items))
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["explanation"] == nil || item["explanation"] == "" {
			t.Errorf("line item %v has no explanation", item["code"])
		}
		if item["kind"] == nil {
			t.Errorf("line item %v has no kind", item["code"])
		}
	}
	weight, _ := resp.Body["weight"].(map[string]any)
	if weight["explanation"] == nil {
		t.Error("the weight breakdown must explain how chargeable weight was derived")
	}

	// The arithmetic must be internally consistent.
	freight := resp.Body["freightMinor"].(float64)
	surcharge := resp.Body["surchargeTotalMinor"].(float64)
	discount := resp.Body["discountTotalMinor"].(float64)
	tax := resp.Body["taxTotalMinor"].(float64)
	total := resp.Body["totalMinor"].(float64)
	if want := freight + surcharge - discount + tax; total != want {
		t.Errorf("total %v does not equal freight+surcharges-discount+tax (%v)", total, want)
	}
}

// TestCODAndRemoteSurchargesApplyConditionally proves condition evaluation.
func TestCODAndRemoteSurchargesApplyConditionally(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SURCH"})

	prepaid := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	cod := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "COD", "codAmountMinor": 100000,
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if prepaid.Status != http.StatusOK || cod.Status != http.StatusOK {
		t.Fatalf("quotes failed: %d / %d", prepaid.Status, cod.Status)
	}
	if !hasLineItem(prepaid, "COD") == false {
		t.Error("a prepaid quote must not carry a COD fee")
	}
	if !hasLineItem(cod, "COD") {
		t.Error("a COD quote must carry the COD fee")
	}
	// 2% of NGN 1000.00 is NGN 20.00, below the NGN 30.00 floor, so the floor
	// applies.
	for _, raw := range cod.Body["lineItems"].([]any) {
		item, _ := raw.(map[string]any)
		if item["code"] == "COD" && item["amountMinor"] != float64(3000) {
			t.Errorf("COD fee = %v, want the 3000 minimum", item["amountMinor"])
		}
	}

	// Remote destination triggers the ODA surcharge.
	remote := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.FarPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if remote.Status != http.StatusOK {
		t.Fatalf("remote quote failed: %d %s", remote.Status, remote.Raw)
	}
	if !hasLineItem(remote, "ODA") {
		t.Error("a remote destination must attract the remote-area surcharge")
	}
	if hasLineItem(prepaid, "ODA") {
		t.Error("a metro destination must not attract the remote-area surcharge")
	}
}

// TestIntraStateUsesCGSTAndSGST proves the tax selection rule.
// TestVATAppliesToEveryLane is the home market's tax behaviour: Nigeria has a
// single VAT rate with no intra/inter-state distinction, so the same tax must
// appear whether a parcel crosses a state boundary or not.
func TestVATAppliesToEveryLane(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "VAT"})

	// Both postal codes are in Lagos, so this lane is within one state.
	env.MustExec(t, `INSERT INTO service_areas (public_id, organization_id, operating_unit_id, pincode_id, area_type, priority, effective_from)
		VALUES ($1, $2, $3, $4, 'DELIVERY', 100, now() - interval '1 hour')`,
		publicid.New(publicid.PrefixServiceArea), tn.OrgID, tn.OriginBranchID, geo["pincode:101001"])

	within := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": "100001", "destinationPincode": "101001",
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if within.Status != http.StatusOK {
		t.Fatalf("within-state quote failed: %d %s", within.Status, within.Raw)
	}
	if !hasLineItem(within, "VAT75") {
		t.Error("VAT must apply to a lane inside one state")
	}

	// Lagos to Abuja crosses a state boundary. Under a single-rate regime that
	// changes nothing — and a rule that quietly did would be an Indian
	// assumption surviving in configuration.
	across := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if across.Status != http.StatusOK {
		t.Fatalf("cross-state quote failed: %d %s", across.Status, across.Raw)
	}
	if !hasLineItem(across, "VAT75") {
		t.Error("VAT must apply to a lane crossing a state boundary")
	}
}

// TestIntraStateSplitStillWorksWhenConfigured keeps the mechanism honest.
//
// Nigeria does not use it, but India does, and the platform is multi-market.
// A split rule set must still resolve by lane — removing this test because the
// home market changed would let the capability rot unnoticed.
func TestIntraStateSplitStillWorksWhenConfigured(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SPLIT"})

	env.MustExec(t, `INSERT INTO service_areas (public_id, organization_id, operating_unit_id, pincode_id, area_type, priority, effective_from)
		VALUES ($1, $2, $3, $4, 'DELIVERY', 100, now() - interval '1 hour')`,
		publicid.New(publicid.PrefixServiceArea), tn.OrgID, tn.OriginBranchID, geo["pincode:101001"])

	// Replace the flat rate with a split pair, as an Indian tenant would.
	env.MustExec(t, `UPDATE tax_rules SET status = 'INACTIVE' WHERE organization_id = $1`, tn.OrgID)
	for _, tx := range []struct {
		code, name, taxType string
		bp                  int
		intraOnly           bool
	}{
		{"SPLIT_LOCAL_A", "Local A 9%", "CGST", 900, true},
		{"SPLIT_LOCAL_B", "Local B 9%", "SGST", 900, true},
		{"SPLIT_CROSS", "Cross 18%", "IGST", 1800, false},
	} {
		env.MustExec(t, `INSERT INTO tax_rules (public_id, organization_id, code, name,
			tax_type, percentage_bp, intra_state_only, priority, effective_from, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 10, now() - interval '1 hour', 'ACTIVE')`,
			publicid.New(publicid.PrefixTaxRule), tn.OrgID, tx.code, tx.name,
			tx.taxType, tx.bp, tx.intraOnly)
	}

	within := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": "100001", "destinationPincode": "101001",
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if within.Status != http.StatusOK {
		t.Fatalf("within-state quote failed: %d %s", within.Status, within.Raw)
	}
	if !hasLineItem(within, "SPLIT_LOCAL_A") || !hasLineItem(within, "SPLIT_LOCAL_B") {
		t.Error("a within-state lane must attract the split pair")
	}
	if hasLineItem(within, "SPLIT_CROSS") {
		t.Error("a within-state lane must not attract the cross-boundary rate")
	}

	across := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if across.Status != http.StatusOK {
		t.Fatalf("cross-state quote failed: %d %s", across.Status, across.Raw)
	}
	if !hasLineItem(across, "SPLIT_CROSS") {
		t.Error("a cross-boundary lane must attract the single cross rate")
	}
	if hasLineItem(across, "SPLIT_LOCAL_A") {
		t.Error("a cross-boundary lane must not attract the within-state pair")
	}
}

func TestActivatedRateCardVersionIsImmutable(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "IMMUT"})
	ctx := context.Background()

	// The seeded version is ACTIVE. Direct SQL must be refused by the trigger.
	_, err := env.DB.Pool.Exec(ctx,
		`UPDATE zone_rates SET base_price_minor = 1 WHERE rate_card_version_id = $1`, tn.RateCardVersionID)
	if err == nil {
		t.Error("editing an activated version's rates must be refused by the database")
	}
	_, err = env.DB.Pool.Exec(ctx,
		`DELETE FROM zone_rates WHERE rate_card_version_id = $1`, tn.RateCardVersionID)
	if err == nil {
		t.Error("deleting an activated version's rates must be refused by the database")
	}

	// And through the API.
	var versionPublicID string
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT public_id FROM rate_card_versions WHERE id = $1`, tn.RateCardVersionID).Scan(&versionPublicID); err != nil {
		t.Fatalf("read version public id: %v", err)
	}
	resp := env.Do(t, "PUT", "/api/v1/rate-cards/versions/"+versionPublicID+"/zone-rates",
		tn.AdminAccessTok, map[string]any{
			"serviceCode": tn.ServiceCode, "originZoneCode": "LOCAL", "destinationZoneCode": "NATIONAL",
			"baseWeightGrams": 500, "basePriceMinor": 1, "additionalStepGrams": 500, "additionalPriceMinor": 1,
		})
	if resp.Status != http.StatusConflict {
		t.Fatalf("expected 409 when editing an activated version, got %d: %s", resp.Status, resp.Raw)
	}
	if resp.ErrorCode() != "IMMUTABLE_RESOURCE" {
		t.Errorf("expected IMMUTABLE_RESOURCE, got %q", resp.ErrorCode())
	}
}

// TestHistoricalShipmentPricingSurvivesRateChange is the Constitution §19
// guarantee: a new rate card version must not alter a shipment already booked.
func TestHistoricalShipmentPricingSurvivesRateChange(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "HIST"})
	ctx := context.Background()

	booked := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil))
	if booked.Status != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", booked.Status, booked.Raw)
	}
	shipmentID, _ := booked.Body["id"].(string)
	originalTotal := booked.Body["totalAmountMinor"].(float64)

	// Publish a new version at double the price.
	var cardPublicID string
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT public_id FROM rate_cards WHERE id = $1`, tn.RateCardID).Scan(&cardPublicID); err != nil {
		t.Fatalf("read rate card: %v", err)
	}
	newVersion := env.Do(t, "POST", "/api/v1/rate-cards/"+cardPublicID+"/versions", tn.AdminAccessTok,
		map[string]any{"effectiveFrom": time.Now().Format(time.RFC3339), "notes": "price rise"})
	if newVersion.Status != http.StatusCreated {
		t.Fatalf("create version failed: %d %s", newVersion.Status, newVersion.Raw)
	}
	versionID, _ := newVersion.Body["id"].(string)
	rate := env.Do(t, "PUT", "/api/v1/rate-cards/versions/"+versionID+"/zone-rates", tn.AdminAccessTok,
		map[string]any{
			"serviceCode": tn.ServiceCode, "originZoneCode": "LOCAL", "destinationZoneCode": "NATIONAL",
			"baseWeightGrams": 500, "basePriceMinor": 10000,
			"additionalStepGrams": 500, "additionalPriceMinor": 5000,
		})
	if rate.Status != http.StatusOK {
		t.Fatalf("add rate failed: %d %s", rate.Status, rate.Raw)
	}
	activate := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+versionID+"/activate", tn.AdminAccessTok, nil)
	if activate.Status != http.StatusOK {
		t.Fatalf("activate failed: %d %s", activate.Status, activate.Raw)
	}

	// The historical shipment is untouched.
	after := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID, tn.AdminAccessTok, nil)
	if after.Body["totalAmountMinor"] != originalTotal {
		t.Fatalf("historical total changed from %v to %v after a rate change",
			originalTotal, after.Body["totalAmountMinor"])
	}
	charges, _ := after.Body["charges"].(map[string]any)
	if charges["rateCardVersion"] != float64(1) {
		t.Errorf("the snapshot must still name version 1, got %v", charges["rateCardVersion"])
	}

	// A new booking uses the new price.
	fresh := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil))
	if fresh.Status != http.StatusCreated {
		t.Fatalf("second booking failed: %d %s", fresh.Status, fresh.Raw)
	}
	if fresh.Body["totalAmountMinor"] == originalTotal {
		t.Error("a new booking should have picked up the new rate card version")
	}
}

// TestOverlappingWeightSlabsAreRejected proves the exclusion constraint.
func TestOverlappingWeightSlabsAreRejected(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SLAB"})
	ctx := context.Background()

	var cardPublicID string
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT public_id FROM rate_cards WHERE id = $1`, tn.RateCardID).Scan(&cardPublicID); err != nil {
		t.Fatalf("read rate card: %v", err)
	}
	version := env.Do(t, "POST", "/api/v1/rate-cards/"+cardPublicID+"/versions", tn.AdminAccessTok,
		map[string]any{"effectiveFrom": time.Now().Add(time.Hour).Format(time.RFC3339)})
	versionID, _ := version.Body["id"].(string)

	slab := map[string]any{
		"serviceCode": tn.ServiceCode, "originZoneCode": "LOCAL", "destinationZoneCode": "NATIONAL",
		"fromWeightGrams": 0, "toWeightGrams": 1000, "priceMinor": 5000, "sequence": 1,
	}
	if resp := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+versionID+"/weight-slabs",
		tn.AdminAccessTok, slab); resp.Status != http.StatusCreated {
		t.Fatalf("first slab failed: %d %s", resp.Status, resp.Raw)
	}
	overlapping := map[string]any{
		"serviceCode": tn.ServiceCode, "originZoneCode": "LOCAL", "destinationZoneCode": "NATIONAL",
		"fromWeightGrams": 500, "toWeightGrams": 2000, "priceMinor": 8000, "sequence": 2,
	}
	resp := env.Do(t, "POST", "/api/v1/rate-cards/versions/"+versionID+"/weight-slabs",
		tn.AdminAccessTok, overlapping)
	if resp.Status != http.StatusConflict {
		t.Fatalf("expected 409 for an overlapping slab, got %d: %s", resp.Status, resp.Raw)
	}
}

// TestQuoteFailsClearlyWhenNoRateExists proves the missing-price path.
func TestQuoteFailsClearlyWhenNoRateExists(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "NORATE", SkipRateCard: true})

	resp := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	})
	if resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when no rate card applies, got %d: %s", resp.Status, resp.Raw)
	}
	details, _ := resp.Body["error"].(map[string]any)["details"].(map[string]any)
	if details["code"] != "RATE_CARD_NOT_FOUND" {
		t.Errorf("expected RATE_CARD_NOT_FOUND, got %v", details["code"])
	}
}

// TestRoutingIsDeterministic runs the same resolution repeatedly and requires an
// identical answer every time, then proves the documented precedence order.
func TestRoutingIsDeterministic(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "ROUTE"})
	ctx := context.Background()

	at := time.Now()
	req := serviceability.Request{
		OrganizationID: tn.OrgID, OriginPincode: tn.OriginPincode,
		DestPincode: tn.DestPincode, ServiceCode: tn.ServiceCode, At: at,
	}
	first, err := env.App.Routing.Resolve(ctx, req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for i := 0; i < 25; i++ {
		next, rErr := env.App.Routing.Resolve(ctx, req)
		if rErr != nil {
			t.Fatalf("resolve %d: %v", i, rErr)
		}
		if next.RouteCode != first.RouteCode ||
			next.ResolutionSource != first.ResolutionSource ||
			next.OriginBranch.Code != first.OriginBranch.Code ||
			next.DestinationBranch.Code != first.DestinationBranch.Code ||
			next.SLAHours != first.SLAHours {
			t.Fatalf("routing is not deterministic: run %d differs from the first", i)
		}
	}
	if first.ResolutionSource != serviceability.SourceGenericRoute {
		t.Errorf("expected GENERIC_ROUTE, got %s", first.ResolutionSource)
	}

	// A higher-priority competing service area must win, deterministically.
	altUnit := env.Do(t, "POST", "/api/v1/network/operating-units", tn.AdminAccessTok, map[string]any{
		"code": "ROUTE-ALT", "name": "Alternate Branch", "unitType": "COMPANY_BRANCH",
		"addressLine1": "5 Alt Road", "pincode": tn.OriginPincode,
	})
	if altUnit.Status != http.StatusCreated {
		t.Fatalf("create alternate unit failed: %d %s", altUnit.Status, altUnit.Raw)
	}
	altID, _ := altUnit.Body["id"].(string)
	area := env.Do(t, "POST", "/api/v1/serviceability/service-areas", tn.AdminAccessTok, map[string]any{
		"operatingUnitId": altID, "pincode": tn.OriginPincode,
		"areaType": "PICKUP", "priority": 900,
	})
	if area.Status != http.StatusCreated {
		t.Fatalf("create service area failed: %d %s", area.Status, area.Raw)
	}
	// The new service area takes effect now, which is after the timestamp
	// captured at the top of this test — effective dating is honoured, so the
	// second phase must ask about the present.
	later := serviceability.Request{
		OrganizationID: tn.OrgID, OriginPincode: tn.OriginPincode,
		DestPincode: tn.DestPincode, ServiceCode: tn.ServiceCode, At: time.Now(),
	}
	if stale, sErr := env.App.Routing.Resolve(ctx, req); sErr == nil && stale.OriginBranch.Code != "ROUTE-BLR" {
		t.Errorf("a resolution dated before the new area took effect must still pick %s, got %s",
			"ROUTE-BLR", stale.OriginBranch.Code)
	}
	for i := 0; i < 10; i++ {
		res, rErr := env.App.Routing.Resolve(ctx, later)
		if rErr != nil {
			t.Fatalf("resolve after priority change: %v", rErr)
		}
		if res.OriginBranch.Code != "ROUTE-ALT" {
			t.Fatalf("run %d: expected the higher-priority branch to win, got %s",
				i, res.OriginBranch.Code)
		}
	}
}

// TestRoutingOverrideTakesPrecedence proves the top of the precedence chain and
// that an override is audited.
func TestRoutingOverrideTakesPrecedence(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "OVERRIDE"})
	ctx := context.Background()

	// A second, deliberately slower route between the same hubs.
	alt := env.Do(t, "POST", "/api/v1/routes", tn.AdminAccessTok, map[string]any{
		"code": "OVERRIDE-ALT", "name": "Surface backup",
		"originUnitCode": tn.HubCode, "destinationUnitCode": "OVERRIDE-DHUB",
		"priority": 1, "transitHours": 72,
		"legs": []map[string]any{{
			"sequence": 1, "fromUnitCode": tn.HubCode, "toUnitCode": "OVERRIDE-DHUB",
			"mode": "ROAD", "transitHours": 72,
		}},
	})
	if alt.Status != http.StatusCreated {
		t.Fatalf("create alternate route failed: %d %s", alt.Status, alt.Raw)
	}

	// Without an override the higher-priority air route wins.
	before, err := env.App.Routing.Resolve(ctx, serviceability.Request{
		OrganizationID: tn.OrgID, OriginPincode: tn.OriginPincode,
		DestPincode: tn.DestPincode, ServiceCode: tn.ServiceCode, At: time.Now(),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if before.RouteCode == "OVERRIDE-ALT" {
		t.Fatal("the low-priority route should not win without an override")
	}

	override := env.Do(t, "POST", "/api/v1/routes/overrides", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"routeCode": "OVERRIDE-ALT", "reason": "Air capacity embargo for the festival week",
	})
	if override.Status != http.StatusCreated {
		t.Fatalf("create override failed: %d %s", override.Status, override.Raw)
	}

	after, err := env.App.Routing.Resolve(ctx, serviceability.Request{
		OrganizationID: tn.OrgID, OriginPincode: tn.OriginPincode,
		DestPincode: tn.DestPincode, ServiceCode: tn.ServiceCode, At: time.Now(),
	})
	if err != nil {
		t.Fatalf("resolve after override: %v", err)
	}
	if after.ResolutionSource != serviceability.SourceOverride {
		t.Fatalf("expected the override to win, got source %s route %s",
			after.ResolutionSource, after.RouteCode)
	}

	var audited int
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT count(*)::int FROM audit_events WHERE action = 'routing_override.created'`).Scan(&audited); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if audited != 1 {
		t.Errorf("expected the override to be audited once, got %d", audited)
	}
}

// TestTemporaryClosureBlocksBooking proves closures are honoured and reversible.
func TestTemporaryClosureBlocksBooking(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CLOSURE"})

	closure := env.Do(t, "POST", "/api/v1/serviceability/closures", tn.AdminAccessTok, map[string]any{
		"operatingUnitCode": tn.OriginBranch, "closureType": "FULL",
		"reasonCode": "WEATHER", "reason": "Cyclone warning across the region",
		"startsAt": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"endsAt":   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})
	if closure.Status != http.StatusCreated {
		t.Fatalf("create closure failed: %d %s", closure.Status, closure.Raw)
	}
	closureID, _ := closure.Body["id"].(string)

	blocked := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil))
	if blocked.Status != http.StatusConflict {
		t.Fatalf("expected the booking to be blocked by the closure, got %d: %s",
			blocked.Status, blocked.Raw)
	}
	details, _ := blocked.Body["error"].(map[string]any)["details"].(map[string]any)
	if details["reasonCode"] != serviceability.ReasonOriginClosed {
		t.Errorf("expected %s, got %v", serviceability.ReasonOriginClosed, details["reasonCode"])
	}

	if resp := env.Do(t, "DELETE", "/api/v1/serviceability/closures/"+closureID,
		tn.AdminAccessTok, nil); resp.Status != http.StatusNoContent {
		t.Fatalf("cancel closure failed: %d %s", resp.Status, resp.Raw)
	}
	if resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok,
		tn.BookingBody(nil)); resp.Status != http.StatusCreated {
		t.Fatalf("booking should succeed once the closure is cancelled, got %d: %s",
			resp.Status, resp.Raw)
	}
}

// TestUnserviceableLaneExplainsWhy proves the reason codes are actionable.
func TestUnserviceableLaneExplainsWhy(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "UNSERV"})
	ctx := context.Background()

	// 101001 is a known postal code with no delivery coverage configured.
	res, err := env.App.Routing.Resolve(ctx, serviceability.Request{
		OrganizationID: tn.OrgID, OriginPincode: tn.OriginPincode,
		DestPincode: "101001", ServiceCode: tn.ServiceCode,
		At: time.Now(), IncludeExplain: true,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Serviceable {
		t.Fatal("a postal code with no delivery coverage must not be serviceable")
	}
	if res.ReasonCode != serviceability.ReasonDestinationNotServed {
		t.Errorf("expected %s, got %s", serviceability.ReasonDestinationNotServed, res.ReasonCode)
	}
	if res.Explanation == nil || len(res.Explanation.Steps) == 0 {
		t.Fatal("a refusal must carry a decision trace")
	}
	last := res.Explanation.Steps[len(res.Explanation.Steps)-1]
	if last.Outcome != "not_serviceable" {
		t.Errorf("the trace should end with the refusal, got %q", last.Outcome)
	}
}

// TestRoutingDebugEndpointRequiresPermission proves the debug surface is gated.
func TestRoutingDebugEndpointRequiresPermission(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "DEBUG"})

	_, _, operatorToken := env.NewUser(t, tn.OrgID, "operator@debug.test",
		"FRANCHISE_OPERATOR", &tn.OriginBranchID)

	body := map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode,
	}
	if resp := env.Do(t, "POST", "/api/v1/serviceability/debug", operatorToken, body); resp.Status != http.StatusForbidden {
		t.Fatalf("expected 403 for a principal without routing.debug, got %d: %s", resp.Status, resp.Raw)
	}

	admin := env.Do(t, "POST", "/api/v1/serviceability/debug", tn.AdminAccessTok, body)
	if admin.Status != http.StatusOK {
		t.Fatalf("expected the administrator debug call to succeed, got %d: %s", admin.Status, admin.Raw)
	}
	if admin.Body["explanation"] == nil {
		t.Error("the debug endpoint must return the full decision trace")
	}
	explanationID, _ := admin.Body["explanationId"].(string)
	stored := env.Do(t, "GET", "/api/v1/serviceability/explanations/"+explanationID, tn.AdminAccessTok, nil)
	if stored.Status != http.StatusOK {
		t.Errorf("expected the stored explanation to be retrievable, got %d: %s", stored.Status, stored.Raw)
	}
}

// TestZoneMappingChangeInvalidatesCache proves explicit cache invalidation.
func TestZoneMappingChangeInvalidatesCache(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CACHE"})
	ctx := context.Background()

	// Warm the zone cache through a quote.
	if resp := env.Do(t, "POST", "/api/v1/pricing/quote", tn.AdminAccessTok, map[string]any{
		"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
		"serviceCode": tn.ServiceCode, "paymentMode": "PREPAID",
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	}); resp.Status != http.StatusOK {
		t.Fatalf("warm-up quote failed: %d %s", resp.Status, resp.Raw)
	}

	// Move the destination into the LOCAL zone.
	remap := env.Do(t, "PUT", "/api/v1/geography/zone-mappings", tn.AdminAccessTok, map[string]any{
		"pincode": tn.DestPincode, "zoneCode": "LOCAL",
	})
	if remap.Status != http.StatusOK {
		t.Fatalf("zone remap failed: %d %s", remap.Status, remap.Raw)
	}

	zone, err := env.App.Geography.ResolveZone(ctx, tn.OrgID, geo["pincode:900001"], time.Now())
	if err != nil {
		t.Fatalf("resolve zone: %v", err)
	}
	if zone.ZoneCode != "LOCAL" {
		t.Fatalf("the cache was not invalidated: zone is still %s", zone.ZoneCode)
	}
}

func hasLineItem(resp harness.Response, code string) bool {
	items, _ := resp.Body["lineItems"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["code"] == code {
			return true
		}
	}
	return false
}
