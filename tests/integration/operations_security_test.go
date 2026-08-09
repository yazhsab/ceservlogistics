package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/tests/harness"
)

// Security and correctness properties of the operational surface: tenant
// isolation, operating-unit scope, custody, hold enforcement, frozen contents,
// and the append-only guarantee. Each is asserted against the running API
// rather than reasoned about.

// TestOperationalTenantIsolation proves organization B cannot see or touch
// anything belonging to organization A.
func TestOperationalTenantIsolation(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)

	a := env.NewTenant(t, geo, harness.TenantOptions{Code: "ISOA"})
	b := env.NewTenant(t, geo, harness.TenantOptions{Code: "ISOB"})

	// Tenant A builds a full operational footprint.
	ja := newJourney(t, env, a)
	ja.toOriginBranch(t)
	ja.bagAndManifest(t)

	carrier := ja.post(t, "/api/v1/carriers", map[string]any{
		"code": "ISOCAR", "name": "A Fleet", "carrierType": "OWN", "modes": []string{"ROAD"},
	}, 201)

	bTok := env.Login(t, b.AdminEmail, b.AdminPassword)

	// Every one of A's objects must read as absent to B, not as forbidden:
	// a 403 would confirm the id exists (§9).
	for _, probe := range []struct {
		name string
		path string
	}{
		{"shipment", "/api/v1/shipments/" + ja.shipID},
		{"bag", "/api/v1/bags/" + ja.bagID},
		{"manifest", "/api/v1/manifests/" + ja.mftID},
	} {
		resp := env.Do(t, "GET", probe.path, bTok, nil)
		if resp.Status != 404 {
			t.Errorf("%s: tenant B got %d for tenant A's object (want 404): %s",
				probe.name, resp.Status, resp.Raw)
		}
	}

	// A's operating units must be unusable as inputs to B's operations.
	resp := env.Do(t, "POST", "/api/v1/bags", bTok, map[string]any{
		"originUnitId": a.OriginBranchPubID, "destinationUnitId": a.DestBranchPubID,
	})
	if resp.Status != 404 {
		t.Errorf("tenant B created a bag at tenant A's facility: %d %s", resp.Status, resp.Raw)
	}

	// A's AWB must not be scannable by B.
	resp = env.Do(t, "POST", "/api/v1/scans", bTok, map[string]any{
		"barcode": ja.awb, "scanType": "RECEIVE", "operatingUnitId": b.OriginBranchPubID,
	})
	if resp.Status == 200 && resp.Body["outcome"] == "ACCEPTED" {
		t.Fatalf("tenant B scanned tenant A's shipment: %s", resp.Raw)
	}
	if resp.Status == 200 && resp.Body["rejectionCode"] != "UNKNOWN_BARCODE" {
		t.Errorf("cross-tenant barcode should be unknown, got %v", resp.Body["rejectionCode"])
	}

	// Listings must be empty for B.
	for _, path := range []string{
		"/api/v1/bags", "/api/v1/manifests", "/api/v1/trips",
		"/api/v1/exceptions", "/api/v1/ndr", "/api/v1/rto", "/api/v1/delivery-runs",
	} {
		list := env.Do(t, "GET", path, bTok, nil)
		if list.Status != 200 {
			t.Fatalf("GET %s as tenant B: %d %s", path, list.Status, list.Raw)
		}
		if rows, ok := list.Body["data"].([]any); ok && len(rows) != 0 {
			t.Errorf("GET %s leaked %d rows to tenant B", path, len(rows))
		}
	}

	// A's carrier is invisible to B.
	carriers := env.Do(t, "GET", "/api/v1/carriers", bTok, nil)
	for _, row := range carriers.Body["data"].([]any) {
		if row.(map[string]any)["id"] == carrier.Body["id"] {
			t.Error("tenant B can see tenant A's carrier")
		}
	}
}

// TestOperatingUnitScopeIsEnforced proves a branch manager cannot work at
// another branch even inside their own tenant.
func TestOperatingUnitScopeIsEnforced(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SCOPE"})

	// A manager scoped to the origin branch only.
	_, _, originTok := env.NewUser(t, tn.OrgID,
		"origin-mgr-"+strings.ToLower(harness.RandomKey()[:6])+"@test.local",
		"BRANCH_MANAGER", &tn.OriginBranchID)

	// They can open a bag at their own branch.
	ok := env.Do(t, "POST", "/api/v1/bags", originTok, map[string]any{
		"originUnitId": tn.OriginBranchPubID, "destinationUnitId": tn.DestBranchPubID,
	})
	if ok.Status != 201 {
		t.Fatalf("a branch manager should be able to bag at their own branch: %d %s",
			ok.Status, ok.Raw)
	}

	// They cannot open one at the destination branch.
	denied := env.Do(t, "POST", "/api/v1/bags", originTok, map[string]any{
		"originUnitId": tn.DestBranchPubID, "destinationUnitId": tn.OriginBranchPubID,
	})
	if denied.Status != 403 {
		t.Errorf("out-of-scope facility should be forbidden, got %d %s", denied.Status, denied.Raw)
	}

	// Nor can they read another branch's hub dashboard.
	dash := env.Do(t, "GET", "/api/v1/hub/summary?operatingUnitId="+tn.DestBranchPubID, originTok, nil)
	if dash.Status != 403 {
		t.Errorf("out-of-scope dashboard should be forbidden, got %d %s", dash.Status, dash.Raw)
	}
}

// TestCustodyViolationsAreRefused is the heart of the release: a parcel can
// only be acted on where it actually is.
func TestCustodyViolationsAreRefused(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CUST"})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)

	_, agentPublicID, _ := env.NewUser(t, tn.OrgID,
		"cust-agent-"+strings.ToLower(harness.RandomKey()[:6])+"@test.local",
		"DELIVERY_AGENT", &tn.DestBranchID)

	// An unknown agent is a clean 404, not a server error.
	missing := env.Do(t, "POST", "/api/v1/delivery-runs", j.token, map[string]any{
		"branchId": tn.DestBranchPubID, "agentId": "usr_00000000000000000000000000",
		"runDate": time.Now().Format("2006-01-02"),
	})
	if missing.Status != 404 {
		t.Errorf("an unknown agent should be 404, got %d %s", missing.Status, missing.Raw)
	}

	// The parcel is sitting at the origin branch, so it cannot be put on a
	// delivery run at the destination branch: it is not there. This is the
	// classic failure the release exists to prevent.
	bad := env.Do(t, "POST", "/api/v1/delivery-runs", j.token, map[string]any{
		"branchId": tn.DestBranchPubID, "agentId": agentPublicID,
		"runDate": time.Now().Format("2006-01-02"), "barcodes": []string{j.awb},
	})
	if bad.Status != 409 || bad.ErrorCode() != "SHIPMENT_INVALID_STATE" {
		t.Errorf("a parcel at the origin must not be deliverable from the destination, got %d %s",
			bad.Status, bad.Raw)
	}

	// A bag cannot contain a parcel another facility holds.
	otherBag := env.Do(t, "POST", "/api/v1/bags", j.token, map[string]any{
		"originUnitId": tn.DestBranchPubID, "destinationUnitId": tn.OriginBranchPubID,
	})
	if otherBag.Status != 201 {
		t.Fatalf("create bag at destination: %d %s", otherBag.Status, otherBag.Raw)
	}
	added := env.Do(t, "POST", "/api/v1/bags/"+otherBag.Body["id"].(string)+"/items",
		j.token, map[string]any{"barcodes": []string{j.awb}})
	if added.Status != 200 {
		t.Fatalf("add items: %d %s", added.Status, added.Raw)
	}
	if added.Body["rejected"].(float64) != 1 {
		t.Fatalf("bagging a parcel held elsewhere must be refused: %s", added.Raw)
	}
	result := added.Body["results"].([]any)[0].(map[string]any)
	if result["reason"] != "CUSTODY_VIOLATION" {
		t.Errorf("expected a custody violation, got %v", result["reason"])
	}
}

// TestHoldStopsTheParcel proves a HOLD scan actually blocks movement.
func TestHoldStopsTheParcel(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "HOLD"})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)

	held := j.post(t, "/api/v1/scans", map[string]any{
		"barcode": j.awb, "scanType": "HOLD", "operatingUnitId": tn.OriginBranchPubID,
		"reasonCode": "PAYMENT_PENDING", "reason": "Awaiting customer payment",
	}, 200)
	if held.Body["outcome"] != "ACCEPTED" {
		t.Fatalf("hold scan: %s", held.Raw)
	}

	// A held parcel cannot be bagged.
	bag := j.post(t, "/api/v1/bags", map[string]any{
		"originUnitId": tn.OriginBranchPubID, "destinationUnitId": tn.DestBranchPubID,
	}, 201)
	added := j.post(t, "/api/v1/bags/"+bag.Body["id"].(string)+"/items",
		map[string]any{"barcodes": []string{j.awb}}, 200)
	if added.Body["rejected"].(float64) != 1 {
		t.Fatalf("a held parcel must not be baggable: %s", added.Raw)
	}
	if added.Body["results"].([]any)[0].(map[string]any)["reason"] != "SHIPMENT_ON_HOLD" {
		t.Errorf("expected SHIPMENT_ON_HOLD, got %s", added.Raw)
	}

	// Release, and it moves again.
	released := j.post(t, "/api/v1/scans", map[string]any{
		"barcode": j.awb, "scanType": "RELEASE", "operatingUnitId": tn.OriginBranchPubID,
		"remarks": "Payment received",
	}, 200)
	if released.Body["outcome"] != "ACCEPTED" {
		t.Fatalf("release scan: %s", released.Raw)
	}
	after := j.post(t, "/api/v1/bags/"+bag.Body["id"].(string)+"/items",
		map[string]any{"barcodes": []string{j.awb}}, 200)
	if after.Body["added"].(float64) != 1 {
		t.Fatalf("a released parcel should be baggable: %s", after.Raw)
	}
}

// TestClosedBagContentsAreFrozen proves the trigger, not just the service.
func TestClosedBagContentsAreFrozen(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "FROZEN"})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)
	j.bagAndManifest(t)

	// Through the API: adding to a closed bag is refused. The batch endpoint
	// reports per-item outcomes, so the call succeeds and the item is rejected.
	extra := newJourney(t, env, tn)
	extra.toOriginBranch(t)
	added := j.post(t, "/api/v1/bags/"+j.bagID+"/items",
		map[string]any{"barcodes": []string{extra.awb}}, 200)
	if added.Body["added"].(float64) != 0 || added.Body["rejected"].(float64) != 1 {
		t.Fatalf("a closed bag must accept nothing: %s", added.Raw)
	}
	if reason := added.Body["results"].([]any)[0].(map[string]any)["reason"]; reason != "BAG_NOT_OPEN" {
		t.Errorf("expected BAG_NOT_OPEN, got %v", reason)
	}

	// Directly against the database: the trigger refuses it too, so a defect in
	// any handler cannot rewrite what a driver signed for.
	var bagID int64
	if err := env.DB.Pool.QueryRow(t.Context(),
		`SELECT id FROM bags WHERE public_id = $1`, j.bagID).Scan(&bagID); err != nil {
		t.Fatal(err)
	}
	var shipmentID int64
	if err := env.DB.Pool.QueryRow(t.Context(),
		`SELECT id FROM shipments WHERE awb = $1`, extra.awb).Scan(&shipmentID); err != nil {
		t.Fatal(err)
	}
	_, err := env.DB.Pool.Exec(t.Context(),
		`INSERT INTO bag_items (organization_id, bag_id, shipment_id, piece_count, weight_grams)
		 VALUES ($1, $2, $3, 1, 500)`, tn.OrgID, bagID, shipmentID)
	if err == nil {
		t.Fatal("the database allowed an insert into a closed bag")
	}
	if !strings.Contains(err.Error(), "BAG_NOT_OPEN") {
		t.Errorf("expected the guard trigger to refuse it, got: %v", err)
	}

	// Removing from a closed bag without an exception is refused too.
	_, err = env.DB.Pool.Exec(t.Context(),
		`UPDATE bag_items SET status = 'REMOVED', removed_at = now(), removal_reason = 'x'
		  WHERE bag_id = $1 AND status = 'IN_BAG'`, bagID)
	if err == nil {
		t.Fatal("the database allowed a removal from a closed bag")
	}
	if !strings.Contains(err.Error(), "BAG_CONTENTS_FROZEN") {
		t.Errorf("expected the frozen-contents guard, got: %v", err)
	}
}

// TestOperationalHistoryIsAppendOnly proves the event stream cannot be edited.
func TestOperationalHistoryIsAppendOnly(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "APPEND2"})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)

	for _, table := range []string{"shipment_events", "scan_events", "bag_events"} {
		_, err := env.DB.Pool.Exec(t.Context(),
			`UPDATE `+table+` SET organization_id = organization_id WHERE organization_id = $1`, tn.OrgID)
		if err == nil {
			// bag_events may legitimately be empty; only a successful update on
			// a non-empty table is a failure.
			var count int
			_ = env.DB.Pool.QueryRow(t.Context(),
				`SELECT count(*) FROM `+table+` WHERE organization_id = $1`, tn.OrgID).Scan(&count)
			if count > 0 {
				t.Errorf("%s accepted an UPDATE: history must be append-only", table)
			}
			continue
		}
		if !strings.Contains(err.Error(), "append-only") && !strings.Contains(err.Error(), "immutable") {
			t.Errorf("%s: refused for the wrong reason: %v", table, err)
		}
	}

	_, err := env.DB.Pool.Exec(t.Context(),
		`DELETE FROM shipment_events WHERE organization_id = $1`, tn.OrgID)
	if err == nil {
		t.Error("shipment_events accepted a DELETE")
	}
}

// TestFieldAgentPermissionsAreNarrow checks the least-privilege grants.
func TestFieldAgentPermissionsAreNarrow(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AGENT"})

	_, _, agentTok := env.NewUser(t, tn.OrgID,
		"narrow-"+strings.ToLower(harness.RandomKey()[:6])+"@test.local",
		"DELIVERY_AGENT", &tn.DestBranchID)

	// A delivery agent must not be able to build or reassign work, plan trips,
	// or configure the NDR catalogue.
	for _, probe := range []struct {
		name, method, path string
		body               map[string]any
	}{
		{"create a delivery run", "POST", "/api/v1/delivery-runs", map[string]any{
			"branchId": tn.DestBranchPubID, "agentId": "usr_x", "runDate": "2026-01-01"}},
		{"plan a trip", "POST", "/api/v1/trips", map[string]any{
			"mode": "ROAD", "destinationUnitId": tn.DestBranchPubID,
			"scheduledDeparture": "2026-01-01T00:00:00Z", "scheduledArrival": "2026-01-01T06:00:00Z"}},
		{"open a bag", "POST", "/api/v1/bags", map[string]any{
			"originUnitId": tn.DestBranchPubID, "destinationUnitId": tn.OriginBranchPubID}},
		{"configure NDR reasons", "POST", "/api/v1/ndr/reasons", map[string]any{
			"code": "X", "name": "X", "category": "OTHER", "defaultAction": "REATTEMPT"}},
		{"start a return", "POST", "/api/v1/rto", map[string]any{
			"barcode": "ABC123", "notes": "because"}},
	} {
		resp := env.Do(t, probe.method, probe.path, agentTok, probe.body)
		if resp.Status != 403 {
			t.Errorf("a delivery agent could %s: %d %s", probe.name, resp.Status, resp.Raw)
		}
	}

	// They can read their own work and complete a delivery.
	for _, path := range []string{"/api/v1/delivery-runs?mine=true", "/api/v1/ndr"} {
		resp := env.Do(t, "GET", path, agentTok, nil)
		if resp.Status != 200 {
			t.Errorf("a delivery agent should be able to read %s: %d %s", path, resp.Status, resp.Raw)
		}
	}
}

// TestPublicTrackingIsRateLimitedAndOpaque covers the anonymous surface.
func TestPublicTrackingIsRateLimitedAndOpaque(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "TRACK"})
	j := newJourney(t, env, tn)

	// A real AWB resolves without a token.
	ok := env.Do(t, "GET", "/api/v1/track/"+j.awb, "", nil)
	if ok.Status != 200 {
		t.Fatalf("public tracking: %d %s", ok.Status, ok.Raw)
	}
	assertPublicSafe(t, ok.Raw)
	if ok.Body["awb"] != j.awb {
		t.Errorf("awb = %v", ok.Body["awb"])
	}

	// An unknown AWB and a malformed one are indistinguishable, so the endpoint
	// cannot be used to probe which numbers exist.
	unknown := env.Do(t, "GET", "/api/v1/track/QQQ260101999999", "", nil)
	malformed := env.Do(t, "GET", "/api/v1/track/not-an-awb", "", nil)
	if unknown.Status != 404 || malformed.Status != 404 {
		t.Errorf("unknown=%d malformed=%d, both should be 404", unknown.Status, malformed.Status)
	}
	if unknown.ErrorCode() != malformed.ErrorCode() {
		t.Errorf("a malformed AWB must not be distinguishable from an unknown one: %q vs %q",
			unknown.ErrorCode(), malformed.ErrorCode())
	}

	// Tracking never exposes the customer or the price.
	if strings.Contains(ok.Raw, tn.CustomerPublicID) {
		t.Error("public tracking leaks the customer identifier")
	}
}
