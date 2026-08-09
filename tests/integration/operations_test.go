package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/tests/harness"
)

// The six workflow scenarios from the Release 2 brief, each run end to end
// against a real database through the HTTP API. Nothing is stubbed: every
// assertion below is about what the production code actually does.

// journey drives a shipment through the physical network and returns the
// identifiers each stage produced, so the scenarios can share the long
// prologue without hiding what they are asserting.
type journey struct {
	env    *harness.Env
	tn     *harness.Tenant
	token  string
	awb    string
	shipID string
	bagID  string
	mftID  string
	tripID string
}

func newJourney(t *testing.T, env *harness.Env, tn *harness.Tenant) *journey {
	t.Helper()
	token := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	resp := env.Do(t, "POST", "/api/v1/shipments", token, tn.BookingBody(nil),
		[2]string{"Idempotency-Key", "journey-" + harness.RandomKey()})
	if resp.Status != 201 {
		t.Fatalf("book shipment: %d %s", resp.Status, resp.Raw)
	}
	return &journey{
		env: env, tn: tn, token: token,
		awb:    resp.Body["awb"].(string),
		shipID: resp.Body["id"].(string),
	}
}

// post is a small wrapper that fails the test on an unexpected status, which
// keeps the scenario bodies readable.
func (j *journey) post(t *testing.T, path string, body any, want int, headers ...[2]string) harness.Response {
	t.Helper()
	resp := j.env.Do(t, "POST", path, j.token, body, headers...)
	if resp.Status != want {
		t.Fatalf("POST %s = %d (want %d): %s", path, resp.Status, want, resp.Raw)
	}
	return resp
}

func (j *journey) get(t *testing.T, path string, want int) harness.Response {
	t.Helper()
	resp := j.env.Do(t, "GET", path, j.token, nil)
	if resp.Status != want {
		t.Fatalf("GET %s = %d (want %d): %s", path, resp.Status, want, resp.Raw)
	}
	return resp
}

// status reads the shipment's current lifecycle state.
func (j *journey) status(t *testing.T) string {
	t.Helper()
	resp := j.get(t, "/api/v1/shipments/"+j.shipID, 200)
	return resp.Body["status"].(string)
}

// scan performs one operational scan and asserts it was accepted.
func (j *journey) scan(t *testing.T, scanType, unit string, extra map[string]any) harness.Response {
	t.Helper()
	payload := map[string]any{
		"barcode": j.awb, "scanType": scanType, "operatingUnitId": unit,
	}
	for k, v := range extra {
		payload[k] = v
	}
	resp := j.post(t, "/api/v1/scans", payload, 200)
	if outcome := resp.Body["outcome"]; outcome != "ACCEPTED" {
		t.Fatalf("%s scan at %s was %v: %s", scanType, unit, outcome, resp.Raw)
	}
	return resp
}

// toOriginBranch takes the parcel from BOOKED to ORIGIN_BRANCH_RECEIVED.
func (j *journey) toOriginBranch(t *testing.T) {
	t.Helper()
	j.scan(t, "RECEIVE", j.tn.OriginBranchPubID, nil)
	if got := j.status(t); got != "ORIGIN_BRANCH_RECEIVED" {
		t.Fatalf("after origin receive: %s", got)
	}
}

// bagAndManifest builds a bag and a manifest at the origin branch, addressed to
// the destination branch, and closes both.
func (j *journey) bagAndManifest(t *testing.T) {
	t.Helper()
	bag := j.post(t, "/api/v1/bags", map[string]any{
		"originUnitId": j.tn.OriginBranchPubID, "destinationUnitId": j.tn.DestBranchPubID,
	}, 201)
	j.bagID = bag.Body["id"].(string)

	added := j.post(t, "/api/v1/bags/"+j.bagID+"/items",
		map[string]any{"barcodes": []string{j.awb}}, 200)
	if added.Body["added"].(float64) != 1 {
		t.Fatalf("bagging refused the shipment: %s", added.Raw)
	}
	if got := j.status(t); got != "ORIGIN_BAGGED" {
		t.Fatalf("after bagging: %s", got)
	}

	closed := j.post(t, "/api/v1/bags/"+j.bagID+"/close",
		map[string]any{"sealNumbers": []string{"SEAL-" + harness.RandomKey()[:8]}}, 200)
	if closed.Body["status"] != "CLOSED" {
		t.Fatalf("bag not closed: %s", closed.Raw)
	}

	mft := j.post(t, "/api/v1/manifests", map[string]any{
		"originUnitId": j.tn.OriginBranchPubID, "destinationUnitId": j.tn.DestBranchPubID,
	}, 201)
	j.mftID = mft.Body["id"].(string)

	bagCode := closed.Body["barcode"].(string)
	loaded := j.post(t, "/api/v1/manifests/"+j.mftID+"/contents",
		map[string]any{"bagCodes": []string{bagCode}}, 200)
	if loaded.Body["added"].(float64) != 1 {
		t.Fatalf("manifest refused the bag: %s", loaded.Raw)
	}
	j.post(t, "/api/v1/manifests/"+j.mftID+"/close", map[string]any{}, 200)
}

// lineHaul plans a trip, attaches the manifest, and moves it to the destination.
func (j *journey) lineHaul(t *testing.T) {
	t.Helper()
	now := time.Now()
	trip := j.post(t, "/api/v1/trips", map[string]any{
		"mode": "ROAD", "originUnitId": j.tn.OriginBranchPubID,
		"destinationUnitId":  j.tn.DestBranchPubID,
		"scheduledDeparture": now.Format(time.RFC3339),
		"scheduledArrival":   now.Add(6 * time.Hour).Format(time.RFC3339),
	}, 201)
	j.tripID = trip.Body["id"].(string)

	j.post(t, "/api/v1/trips/"+j.tripID+"/manifests",
		map[string]any{"manifestId": j.mftID}, 200)

	// A road trip cannot leave without a vehicle and driver: that is the
	// departure validation from §17, asserted here rather than assumed.
	refused := j.env.Do(t, "POST", "/api/v1/trips/"+j.tripID+"/depart", j.token, map[string]any{})
	if refused.Status != 409 || refused.ErrorCode() != "TRIP_NO_VEHICLE" {
		t.Fatalf("expected departure without a vehicle to be refused, got %d %s",
			refused.Status, refused.Raw)
	}

	carrier := j.post(t, "/api/v1/carriers", map[string]any{
		"code": "CAR" + harness.RandomKey()[:5], "name": "Own Fleet",
		"carrierType": "OWN", "modes": []string{"ROAD"},
	}, 201)
	vehicle := j.post(t, "/api/v1/vehicles", map[string]any{
		"registrationNumber": "KA01" + harness.RandomKey()[:6],
		"vehicleType":        "TRUCK", "carrierId": carrier.Body["id"],
	}, 201)
	driver := j.post(t, "/api/v1/drivers", map[string]any{
		"code": "DRV" + harness.RandomKey()[:5], "fullName": "Test Driver",
		"phone": "9876543210", "carrierId": carrier.Body["id"],
	}, 201)
	j.post(t, "/api/v1/trips/"+j.tripID+"/assign", map[string]any{
		"vehicleId": vehicle.Body["id"], "driverId": driver.Body["id"],
	}, 200)

	j.post(t, "/api/v1/trips/"+j.tripID+"/depart", map[string]any{}, 200)
	if got := j.status(t); got != "IN_TRANSIT" {
		t.Fatalf("after departure: %s", got)
	}

	j.post(t, "/api/v1/trips/"+j.tripID+"/arrive",
		map[string]any{}, 200, [2]string{"X-Operating-Unit", j.tn.DestBranchPubID})
	j.post(t, "/api/v1/manifests/"+j.mftID+"/receive",
		map[string]any{}, 200, [2]string{"X-Operating-Unit", j.tn.DestBranchPubID})

	if got := j.status(t); got != "DESTINATION_BRANCH_RECEIVED" {
		t.Fatalf("after manifest receipt: %s", got)
	}
}

// dispatchForDelivery puts the parcel on a run and sends it out.
func (j *journey) dispatchForDelivery(t *testing.T) string {
	t.Helper()
	_, agentPublicID, agentToken := j.env.NewUser(t, j.tn.OrgID,
		"agent-"+strings.ToLower(harness.RandomKey()[:8])+"@test.local",
		"DELIVERY_AGENT", &j.tn.DestBranchID)

	run := j.post(t, "/api/v1/delivery-runs", map[string]any{
		"branchId": j.tn.DestBranchPubID, "agentId": agentPublicID,
		"runDate": time.Now().Format("2006-01-02"), "barcodes": []string{j.awb},
	}, 201)
	runID := run.Body["id"].(string)

	j.post(t, "/api/v1/delivery-runs/"+runID+"/dispatch", map[string]any{}, 200)
	if got := j.status(t); got != "OUT_FOR_DELIVERY" {
		t.Fatalf("after dispatch: %s", got)
	}
	// The agent's own token is what completes the delivery, so custody is
	// genuinely checked rather than bypassed by an admin.
	j.token = agentToken
	return runID
}

// TestScenario1BookingToPODIsWalkable is the full forward journey.
func TestScenario1BookingToPODIsWalkable(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)
	j.bagAndManifest(t)
	j.lineHaul(t)
	runID := j.dispatchForDelivery(t)

	// Delivery.
	attempt := j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
	}, 200, [2]string{"Idempotency-Key", "deliver-" + harness.RandomKey()})
	if attempt.Body["toStatus"] != "DELIVERED" {
		t.Fatalf("delivery did not complete: %s", attempt.Raw)
	}

	// POD with a real image upload, stored and checksummed.
	pod := env.UploadPOD(t, j.token, j.awb, tn.DestBranchPubID)
	if pod.Status != 201 {
		t.Fatalf("submit POD: %d %s", pod.Status, pod.Raw)
	}
	artifacts := pod.Body["artifacts"].([]any)
	if len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %d", len(artifacts))
	}
	artifact := artifacts[0].(map[string]any)
	if artifact["checksumSha256"] == "" || artifact["mimeType"] != "image/png" {
		t.Errorf("artifact metadata is wrong: %#v", artifact)
	}
	if pod.Body["signatureCaptured"] != true {
		t.Error("signature flag should be set from the uploaded artifact")
	}

	// Public tracking shows a delivered milestone and leaks nothing.
	track := env.Do(t, "GET", "/api/v1/track/"+j.awb, "", nil)
	if track.Status != 200 {
		t.Fatalf("public tracking: %d %s", track.Status, track.Raw)
	}
	if track.Body["milestone"] != "DELIVERED" {
		t.Errorf("milestone = %v", track.Body["milestone"])
	}
	assertPublicSafe(t, track.Raw)

	events := track.Body["events"].([]any)
	if len(events) < 4 {
		t.Errorf("expected a multi-stage timeline, got %d events", len(events))
	}
}

// TestScenario2NDRReattemptThenDelivered walks a failure and recovery.
func TestScenario2NDRReattemptThenDelivered(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)
	j.bagAndManifest(t)
	j.lineHaul(t)
	runID := j.dispatchForDelivery(t)

	failed := j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "FAILED", "runId": runID,
		"failureReasonCode": "CUSTOMER_NOT_AVAILABLE",
		"remarks":           "Nobody at the address",
	}, 200, [2]string{"Idempotency-Key", "fail-" + harness.RandomKey()})

	caseID, _ := failed.Body["ndrCaseId"].(string)
	if caseID == "" {
		t.Fatal("a failed delivery should have opened an NDR case")
	}
	if got := j.status(t); got != "NDR" {
		t.Fatalf("after failure the shipment should be NDR, got %s", got)
	}

	// The admin sets the next action; the agent cannot configure the workflow.
	admin := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	resp := env.Do(t, "GET", "/api/v1/ndr/"+caseID, admin, nil)
	if resp.Status != 200 {
		t.Fatalf("read NDR case: %d %s", resp.Status, resp.Raw)
	}
	if resp.Body["attemptCount"].(float64) != 1 {
		t.Errorf("attemptCount = %v, want 1", resp.Body["attemptCount"])
	}
	attempts := resp.Body["attempts"].([]any)
	if len(attempts) != 1 {
		t.Errorf("expected the failed visit to be recorded, got %d attempts", len(attempts))
	}

	action := env.Do(t, "POST", "/api/v1/ndr/"+caseID+"/action", admin, map[string]any{
		"action": "REATTEMPT", "instructions": "Customer will be home after 6pm",
	})
	if action.Status != 200 {
		t.Fatalf("set NDR action: %d %s", action.Status, action.Raw)
	}
	if action.Body["currentAction"] != "REATTEMPT" {
		t.Errorf("currentAction = %v", action.Body["currentAction"])
	}

	j.token = admin
	if got := j.status(t); got != "DESTINATION_BRANCH_RECEIVED" {
		t.Fatalf("a reattempt should return the parcel to the branch shelf, got %s", got)
	}

	// Second run, second attempt, delivered.
	runID2 := j.dispatchForDelivery(t)
	ok := j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID2,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
	}, 200, [2]string{"Idempotency-Key", "deliver2-" + harness.RandomKey()})
	if ok.Body["toStatus"] != "DELIVERED" {
		t.Fatalf("second attempt did not deliver: %s", ok.Raw)
	}
	if ok.Body["attemptNumber"].(float64) != 2 {
		t.Errorf("attemptNumber = %v, want 2", ok.Body["attemptNumber"])
	}
}

// TestScenario3NDRToRTOReturnsToSender walks the reverse journey.
func TestScenario3NDRToRTOReturnsToSender(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)
	j.bagAndManifest(t)
	j.lineHaul(t)
	runID := j.dispatchForDelivery(t)

	failed := j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "FAILED", "runId": runID,
		"failureReasonCode": "CUSTOMER_REFUSED", "remarks": "Consignee refused",
	}, 200, [2]string{"Idempotency-Key", "refuse-" + harness.RandomKey()})

	// CUSTOMER_REFUSED is configured with max_attempts 1 and auto RTO, so the
	// return should have started on the first failure without an operator.
	caseID := failed.Body["ndrCaseId"].(string)
	admin := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	j.token = admin

	ndrCase := j.get(t, "/api/v1/ndr/"+caseID, 200)
	if ndrCase.Body["status"] != "RESOLVED_RTO" {
		t.Fatalf("a refused delivery should auto-RTO, case status = %v", ndrCase.Body["status"])
	}
	if got := j.status(t); got != "RTO_INITIATED" {
		t.Fatalf("shipment should be RTO_INITIATED, got %s", got)
	}

	rtoList := j.get(t, "/api/v1/rto", 200)
	cases := rtoList.Body["data"].([]any)
	if len(cases) != 1 {
		t.Fatalf("expected one RTO case, got %d", len(cases))
	}
	rtoID := cases[0].(map[string]any)["id"].(string)

	detail := j.get(t, "/api/v1/rto/"+rtoID, 200)
	legs := detail.Body["legs"].([]any)
	if len(legs) == 0 {
		t.Fatal("the reverse route should have at least one leg")
	}
	if detail.Body["returnAddress"] == nil {
		t.Error("the return address should come from the sender snapshot")
	}

	// Reverse movement: dispatch, receive at the returning branch, hand back.
	j.post(t, "/api/v1/rto/"+rtoID+"/dispatch", map[string]any{}, 200,
		[2]string{"X-Operating-Unit", tn.DestBranchPubID})
	if got := j.status(t); got != "RTO_IN_TRANSIT" {
		t.Fatalf("after RTO dispatch: %s", got)
	}

	j.post(t, "/api/v1/rto/receive", map[string]any{"barcode": j.awb}, 200,
		[2]string{"X-Operating-Unit", tn.OriginBranchPubID})

	// Receiving on the return leg records the movement without changing the
	// lifecycle status, which is the design in ADR 0009.
	if got := j.status(t); got != "RTO_IN_TRANSIT" {
		t.Fatalf("a reverse receipt should not change the status, got %s", got)
	}

	done := j.post(t, "/api/v1/rto/"+rtoID+"/complete", map[string]any{
		"outcome": "RETURNED", "receivedBy": "Original sender",
	}, 200, [2]string{"X-Operating-Unit", tn.OriginBranchPubID})
	if done.Body["status"] != "RETURNED" {
		t.Fatalf("RTO not completed: %s", done.Raw)
	}
	if got := j.status(t); got != "RTO_DELIVERED" {
		t.Fatalf("final status = %s, want RTO_DELIVERED", got)
	}

	// Public tracking says "returned" without exposing the reason.
	track := env.Do(t, "GET", "/api/v1/track/"+j.awb, "", nil)
	if track.Body["milestone"] != "RETURNED" {
		t.Errorf("public milestone = %v", track.Body["milestone"])
	}
	assertPublicSafe(t, track.Raw)
}

// TestScenario4BagMismatchRaisesExceptions proves reconciliation works.
func TestScenario4BagMismatchRaisesExceptions(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})

	// Two shipments in a bag; only one arrives.
	a := newJourney(t, env, tn)
	a.toOriginBranch(t)
	b := newJourney(t, env, tn)
	b.toOriginBranch(t)

	bag := a.post(t, "/api/v1/bags", map[string]any{
		"originUnitId": tn.OriginBranchPubID, "destinationUnitId": tn.DestBranchPubID,
	}, 201)
	bagID := bag.Body["id"].(string)
	added := a.post(t, "/api/v1/bags/"+bagID+"/items",
		map[string]any{"barcodes": []string{a.awb, b.awb}}, 200)
	if added.Body["added"].(float64) != 2 {
		t.Fatalf("both shipments should have been bagged: %s", added.Raw)
	}
	closed := a.post(t, "/api/v1/bags/"+bagID+"/close",
		map[string]any{"sealNumbers": []string{"SEAL-" + harness.RandomKey()[:8]}}, 200)

	// Move the bag to the destination and open it.
	a.post(t, "/api/v1/bags/"+bagID+"/dispatch", map[string]any{}, 200)
	a.post(t, "/api/v1/bags/"+bagID+"/receive", map[string]any{}, 200,
		[2]string{"X-Operating-Unit", tn.DestBranchPubID})
	a.post(t, "/api/v1/bags/"+bagID+"/open", map[string]any{}, 200,
		[2]string{"X-Operating-Unit", tn.DestBranchPubID})

	// Count it: scan one declared shipment and one that was never declared.
	rec := a.post(t, "/api/v1/reconciliations", map[string]any{
		"subjectType": "BAG", "bagId": bagID,
	}, 201, [2]string{"X-Operating-Unit", tn.DestBranchPubID})
	recID := rec.Body["id"].(string)
	if rec.Body["expectedCount"].(float64) != 2 {
		t.Fatalf("the count should expect both declared shipments, got %v",
			rec.Body["expectedCount"])
	}

	stranger := newJourney(t, env, tn)
	scanned := a.post(t, "/api/v1/reconciliations/"+recID+"/scan", map[string]any{
		"barcodes": []string{a.awb, stranger.awb},
	}, 200, [2]string{"X-Operating-Unit", tn.DestBranchPubID})

	results := scanned.Body["results"].([]any)
	outcomes := map[string]string{}
	for _, r := range results {
		row := r.(map[string]any)
		outcomes[row["barcode"].(string)] = row["outcome"].(string)
	}
	if outcomes[a.awb] != "MATCHED" {
		t.Errorf("declared shipment should match, got %s", outcomes[a.awb])
	}
	if outcomes[stranger.awb] != "EXCESS" {
		t.Errorf("undeclared shipment should be EXCESS, got %s", outcomes[stranger.awb])
	}

	done := a.post(t, "/api/v1/reconciliations/"+recID+"/complete",
		map[string]any{"remarks": "One parcel short"}, 200,
		[2]string{"X-Operating-Unit", tn.DestBranchPubID})
	if done.Body["status"] != "COMPLETED_WITH_EXCEPTIONS" {
		t.Fatalf("status = %v, want COMPLETED_WITH_EXCEPTIONS", done.Body["status"])
	}
	if done.Body["missingCount"].(float64) != 1 {
		t.Errorf("missingCount = %v, want 1", done.Body["missingCount"])
	}
	if done.Body["excessCount"].(float64) != 1 {
		t.Errorf("excessCount = %v, want 1", done.Body["excessCount"])
	}

	// Both discrepancies must have raised exceptions with an owner.
	excs := a.get(t, "/api/v1/exceptions?status=OPEN", 200)
	raised := excs.Body["data"].([]any)
	types := map[string]bool{}
	for _, e := range raised {
		types[e.(map[string]any)["exceptionType"].(string)] = true
	}
	if !types["MISSING"] {
		t.Error("a declared-but-absent shipment should raise a MISSING exception")
	}
	if !types["EXCESS"] {
		t.Error("an undeclared shipment should raise an EXCESS exception")
	}

	// The closure declaration is unchanged: it still says two shipments.
	after := a.get(t, "/api/v1/bags/"+bagID, 200)
	declared := after.Body["declaredContents"].(map[string]any)
	if declared["shipmentCount"].(float64) != 2 {
		t.Errorf("the closure declaration must not be rewritten by reconciliation, got %v",
			declared["shipmentCount"])
	}
	_ = closed
}

// TestScenario5DuplicateScansAndRetriesAreSafe covers offline retry.
func TestScenario5DuplicateScansAndRetriesAreSafe(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})
	j := newJourney(t, env, tn)

	device := [2]string{"X-Device-Id", "handset-" + harness.RandomKey()[:8]}
	event := [2]string{"X-Device-Event-Id", "evt-" + harness.RandomKey()}

	first := j.post(t, "/api/v1/scans", map[string]any{
		"barcode": j.awb, "scanType": "RECEIVE", "operatingUnitId": tn.OriginBranchPubID,
	}, 200, device, event)
	if first.Body["outcome"] != "ACCEPTED" {
		t.Fatalf("first scan: %s", first.Raw)
	}

	// The same device event replayed: same answer, no second event.
	replay := j.post(t, "/api/v1/scans", map[string]any{
		"barcode": j.awb, "scanType": "RECEIVE", "operatingUnitId": tn.OriginBranchPubID,
	}, 200, device, event)
	if replay.Body["outcome"] != "DUPLICATE" {
		t.Fatalf("a replayed device event should be reported as duplicate, got %v", replay.Body["outcome"])
	}
	if replay.Body["scanId"] != first.Body["scanId"] {
		t.Error("a replay should return the original scan record")
	}

	// A genuinely repeated scan without a device key is refused on state, and
	// the refusal is recorded rather than discarded.
	repeat := j.post(t, "/api/v1/scans", map[string]any{
		"barcode": j.awb, "scanType": "RECEIVE", "operatingUnitId": tn.OriginBranchPubID,
	}, 200)
	if repeat.Body["outcome"] != "REJECTED" {
		t.Fatalf("re-receiving at the same facility should be rejected, got %v", repeat.Body["outcome"])
	}
	if repeat.Body["rejectionCode"] == "" {
		t.Error("a rejection must carry a code the handset can act on")
	}

	history := j.get(t, "/api/v1/scans?limit=20", 200)
	rows := history.Body["data"].([]any)
	rejected := 0
	for _, row := range rows {
		if row.(map[string]any)["outcome"] == "REJECTED" {
			rejected++
		}
	}
	if rejected == 0 {
		t.Error("rejected scans must be recorded so a supervisor can see them")
	}

	// The shipment moved exactly once.
	events := j.get(t, "/api/v1/shipments/"+j.shipID+"/events", 200)
	received := 0
	for _, e := range events.Body["data"].([]any) {
		if e.(map[string]any)["toStatus"] == "ORIGIN_BRANCH_RECEIVED" {
			received++
		}
	}
	if received != 1 {
		t.Fatalf("expected exactly one receive event, got %d", received)
	}
}

// TestScenario6ConcurrentDeliveryProducesOneSuccess is the concurrency proof.
func TestScenario6ConcurrentDeliveryProducesOneSuccess(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})

	j := newJourney(t, env, tn)
	j.toOriginBranch(t)
	j.bagAndManifest(t)
	j.lineHaul(t)
	runID := j.dispatchForDelivery(t)

	const attempts = 12
	type outcome struct {
		status int
		code   string
		body   string
	}
	results := make(chan outcome, attempts)
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		go func(n int) {
			<-start
			// Distinct idempotency keys: this tests the concurrency guard, not
			// the idempotency ledger.
			resp := env.Do(t, "POST", "/api/v1/deliveries/attempts", j.token, map[string]any{
				"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
				"recipientName": fmt.Sprintf("Recipient %d", n), "recipientRelationship": "SELF",
			}, [2]string{"Idempotency-Key", fmt.Sprintf("race-%d-%s", n, harness.RandomKey())})
			results <- outcome{resp.Status, resp.ErrorCode(), resp.Raw}
		}(i)
	}
	close(start)

	succeeded, conflicts := 0, 0
	for i := 0; i < attempts; i++ {
		r := <-results
		switch {
		case r.status == 200:
			succeeded++
		case r.status == 409:
			conflicts++
		default:
			t.Errorf("unexpected response %d: %s", r.status, r.body)
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one concurrent delivery must succeed, got %d", succeeded)
	}
	if conflicts != attempts-1 {
		t.Errorf("the other %d attempts should conflict, got %d", attempts-1, conflicts)
	}

	// Exactly one successful attempt exists in the database.
	var successes int
	if err := env.DB.Pool.QueryRow(t.Context(),
		`SELECT count(*) FROM delivery_attempts da
		   JOIN shipments s ON s.id = da.shipment_id
		  WHERE s.awb = $1 AND da.outcome = 'DELIVERED'`, j.awb).Scan(&successes); err != nil {
		t.Fatal(err)
	}
	if successes != 1 {
		t.Fatalf("delivery_attempts holds %d successes for one shipment", successes)
	}
	if got := j.status(t); got != "DELIVERED" {
		t.Fatalf("final status = %s", got)
	}
}

// assertPublicSafe checks that a public tracking payload leaks nothing.
func assertPublicSafe(t *testing.T, raw string) {
	t.Helper()
	// Internal identifier prefixes must never appear in a public response
	// (§M20). The AWB is the only identifier a consignee gets.
	for _, forbidden := range []string{
		`"shp_`, `"ou_`, `"cus_`, `"evt_`, `"bag_`, `"mft_`, `"trp_`, `"drn_`,
		"internalRemarks", "actorName", "operatingUnitId", "rateCard",
		"totalAmountMinor", "customerCode",
	} {
		if contains(raw, forbidden) {
			t.Errorf("public tracking response leaks %q: %s", forbidden, raw)
		}
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
