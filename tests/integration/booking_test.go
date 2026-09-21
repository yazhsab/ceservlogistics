package integration

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/tests/harness"
)

// TestBookingProducesCompleteRecord asserts the whole Release 1 booking
// contract in one pass: AWB shape, snapshots, event history, audit trail, and
// the arithmetic that produced the price.
func TestBookingProducesCompleteRecord(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "BOOK1"})
	ctx := context.Background()

	resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(map[string]any{
		"referenceNumber": "PO-12345",
	}))
	if resp.Status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Status, resp.Raw)
	}

	shipmentID, _ := resp.Body["id"].(string)
	awb, _ := resp.Body["awb"].(string)
	if !shipment.ValidAWB(awb) {
		t.Errorf("AWB %q does not match the documented format", awb)
	}
	if got := resp.Body["status"]; got != "BOOKED" {
		t.Errorf("expected status BOOKED, got %v", got)
	}
	if got := resp.Headers.Get("Location"); got != "/api/v1/shipments/"+shipmentID {
		t.Errorf("expected a Location header, got %q", got)
	}

	// Weight: 500 g actual, 200x150x100 mm at divisor 5000 gives 600 g
	// volumetric, and the 500 g rounding step lifts the chargeable weight to
	// 1000 g.
	if got := resp.Body["chargeableWeightGrams"]; got != float64(1000) {
		t.Errorf("expected chargeable weight 1000 g, got %v", got)
	}
	if got := resp.Body["volumetricWeightGrams"]; got != float64(600) {
		t.Errorf("expected volumetric weight 600 g, got %v", got)
	}

	// Price: base 5000 + one additional 500 g step at 2500 = 7500 freight;
	// 18.5% fuel = 1388 (half-up); 7.5% VAT on 8888 = 667 (half-up: 666.6);
	// total 9555.
	//
	// The figures are spelled out rather than recomputed because that is the
	// point: they pin the arithmetic, including the half-up rounding applied
	// once per derived line. Recomputing them here would test nothing.
	charges, _ := resp.Body["charges"].(map[string]any)
	for field, want := range map[string]float64{
		"freightMinor": 7500, "surchargeTotalMinor": 1388,
		"taxableMinor": 8888, "taxTotalMinor": 667, "totalMinor": 9555,
	} {
		if got := charges[field]; got != want {
			t.Errorf("charges.%s = %v, want %v", field, got, want)
		}
	}
	if charges["rateCardCode"] != "RETAIL" || charges["rateCardVersion"] != float64(1) {
		t.Errorf("charge snapshot must record the exact rate card version, got %v v%v",
			charges["rateCardCode"], charges["rateCardVersion"])
	}
	lineItems, _ := charges["lineItems"].([]any)
	if len(lineItems) != 3 {
		t.Errorf("expected freight, fuel and tax line items, got %d: %v", len(lineItems), lineItems)
	}

	// Route snapshot with a full decision trace.
	route, _ := resp.Body["route"].(map[string]any)
	if route["resolutionSource"] != "GENERIC_ROUTE" {
		t.Errorf("expected GENERIC_ROUTE, got %v", route["resolutionSource"])
	}
	if route["explanationId"] == nil || route["explanationId"] == "" {
		t.Error("route snapshot must carry a routing explanation identifier")
	}

	// Address snapshots are copied, not referenced.
	addresses, _ := resp.Body["addresses"].(map[string]any)
	sender, _ := addresses["sender"].(map[string]any)
	if sender["pincode"] != tn.OriginPincode {
		t.Errorf("sender snapshot pincode = %v, want %s", sender["pincode"], tn.OriginPincode)
	}

	// Exactly one BOOKED event, at sequence 1.
	events := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID+"/events", tn.AdminAccessTok, nil)
	eventList, _ := events.Body["data"].([]any)
	if len(eventList) != 1 {
		t.Fatalf("expected exactly one event after booking, got %d", len(eventList))
	}
	first, _ := eventList[0].(map[string]any)
	if first["eventType"] != "BOOKED" || first["sequence"] != float64(1) {
		t.Errorf("expected BOOKED at sequence 1, got %v at %v", first["eventType"], first["sequence"])
	}

	// The booking is audited.
	var auditCount int
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT count(*)::int FROM audit_events WHERE action = 'shipment.booked' AND resource_public_id = $1`,
		shipmentID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 1 {
		t.Errorf("expected one shipment.booked audit event, got %d", auditCount)
	}
}

func TestBookedShipmentCorrectionKeepsAWBAndOriginalSnapshots(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "EDIT1"})
	ctx := context.Background()

	booked := env.Do(t, http.MethodPost, "/api/v1/shipments", tn.AdminAccessTok,
		tn.BookingBody(map[string]any{"referenceNumber": "WEB-OLD"}))
	if booked.Status != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", booked.Status, booked.Raw)
	}
	shipmentID, _ := booked.Body["id"].(string)
	awb, _ := booked.Body["awb"].(string)
	version, _ := booked.Body["version"].(float64)
	total := booked.Body["totalAmountMinor"]

	correction := map[string]any{
		"expectedVersion":     int(version),
		"reason":              "Corrected recipient contact details",
		"referenceNumber":     "WEB-CORRECTED",
		"contentDescription":  "Corrected documents",
		"specialInstructions": "Call the corrected recipient before delivery",
		"isFragile":           true,
		"sender": map[string]any{
			"contactName": "Corrected Sender", "phone": "+2348011111111",
			"line1": "15 Corrected Origin Street",
		},
		"recipient": map[string]any{
			"contactName": "Corrected Recipient", "phone": "+2348022222222",
			"line1": "27 Corrected Destination Road", "landmark": "Blue gate",
		},
	}
	updated := env.Do(t, http.MethodPatch, "/api/v1/shipments/"+shipmentID,
		tn.AdminAccessTok, correction)
	if updated.Status != http.StatusOK {
		t.Fatalf("correction failed: %d %s", updated.Status, updated.Raw)
	}
	if updated.Body["awb"] != awb {
		t.Fatalf("correction changed AWB: got %v want %s", updated.Body["awb"], awb)
	}
	if updated.Body["totalAmountMinor"] != total {
		t.Fatalf("correction changed price: got %v want %v", updated.Body["totalAmountMinor"], total)
	}
	if updated.Body["referenceNumber"] != "WEB-CORRECTED" || updated.Body["isFragile"] != true {
		t.Fatalf("corrected shipment fields missing: %v", updated.Body)
	}
	addresses, _ := updated.Body["addresses"].(map[string]any)
	recipient, _ := addresses["recipient"].(map[string]any)
	if recipient["phone"] != "+2348022222222" || recipient["pincode"] != tn.DestPincode {
		t.Fatalf("recipient correction or locked route missing: %v", recipient)
	}

	label := env.Do(t, http.MethodGet, "/api/v1/shipments/"+shipmentID+"/label",
		tn.AdminAccessTok, nil)
	labelRecipient, _ := label.Body["recipient"].(map[string]any)
	if label.Status != http.StatusOK || labelRecipient["name"] != "Corrected Recipient" ||
		labelRecipient["line1"] != "27 Corrected Destination Road" {
		t.Fatalf("waybill did not use corrected details: %d %v", label.Status, label.Body)
	}

	var originalName string
	if err := env.DB.Pool.QueryRow(ctx, `
		SELECT a.contact_name FROM shipment_address_snapshots a
		JOIN shipments s ON s.id = a.shipment_id
		WHERE s.public_id = $1 AND a.role = 'RECIPIENT'`, shipmentID).Scan(&originalName); err != nil {
		t.Fatal(err)
	}
	if originalName != "Recipient Name" {
		t.Fatalf("immutable booking snapshot was rewritten: %q", originalName)
	}
	var correctionCount, auditCount int
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT count(*) FROM shipment_address_corrections c JOIN shipments s ON s.id=c.shipment_id WHERE s.public_id=$1`,
		shipmentID).Scan(&correctionCount); err != nil {
		t.Fatal(err)
	}
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_events WHERE action='shipment.corrected' AND resource_public_id=$1`,
		shipmentID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if correctionCount != 2 || auditCount != 1 {
		t.Fatalf("correction evidence missing: address rows=%d audit rows=%d", correctionCount, auditCount)
	}

	stale := env.Do(t, http.MethodPatch, "/api/v1/shipments/"+shipmentID,
		tn.AdminAccessTok, correction)
	if stale.Status != http.StatusConflict || stale.ErrorCode() != "CONCURRENT_MODIFICATION" {
		t.Fatalf("stale correction accepted: %d %s", stale.Status, stale.Raw)
	}
}

func TestBookingUsesOneCountryForPostcodeResolutionAndSnapshots(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CTRY"})

	// Lowercase explicit input is normalized. The same NG value that selected
	// the postcode records must be frozen on both immutable address snapshots.
	body := tn.BookingBody(nil)
	body["sender"].(map[string]any)["countryCode"] = "ng"
	body["recipient"].(map[string]any)["countryCode"] = "NG"
	created := env.Do(t, http.MethodPost, "/api/v1/shipments", tn.AdminAccessTok, body,
		[2]string{"Idempotency-Key", harness.RandomKey()})
	if created.Status != http.StatusCreated {
		t.Fatalf("Nigerian booking: %d %s", created.Status, created.Raw)
	}
	shipmentID, _ := created.Body["id"].(string)
	var ngSnapshots int
	if err := env.DB.Pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM shipment_address_snapshots a
		JOIN shipments s ON s.id = a.shipment_id
		WHERE s.organization_id = $1 AND s.public_id = $2 AND a.country_code = 'NG'`,
		tn.OrgID, shipmentID).Scan(&ngSnapshots); err != nil {
		t.Fatal(err)
	}
	if ngSnapshots != 2 {
		t.Fatalf("NG snapshots = %d, want sender and recipient (2)", ngSnapshots)
	}

	// The same numeric postcode exists only in the Nigerian fixture. Marking
	// the address IN must not silently resolve the Nigerian row.
	wrongCountry := tn.BookingBody(nil)
	wrongCountry["sender"].(map[string]any)["countryCode"] = "IN"
	refused := env.Do(t, http.MethodPost, "/api/v1/shipments", tn.AdminAccessTok, wrongCountry,
		[2]string{"Idempotency-Key", harness.RandomKey()})
	if refused.Status != http.StatusUnprocessableEntity {
		t.Fatalf("explicit IN postcode resolved as Nigerian: %d %s", refused.Status, refused.Raw)
	}
}

// TestShipmentEventsAreAppendOnly proves the database refuses to rewrite
// operational history, independently of application code.
func TestShipmentEventsAreAppendOnly(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "APPEND"})
	ctx := context.Background()

	resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil))
	if resp.Status != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", resp.Status, resp.Raw)
	}

	if _, err := env.DB.Pool.Exec(ctx,
		`UPDATE shipment_events SET description = 'tampered'`); err == nil {
		t.Error("shipment_events must reject UPDATE")
	}
	if _, err := env.DB.Pool.Exec(ctx, `DELETE FROM shipment_events`); err == nil {
		t.Error("shipment_events must reject DELETE")
	}
	if _, err := env.DB.Pool.Exec(ctx,
		`UPDATE shipment_charge_snapshots SET total_minor = 1`); err == nil {
		t.Error("shipment_charge_snapshots must reject UPDATE")
	}
	if _, err := env.DB.Pool.Exec(ctx, `UPDATE audit_events SET action = 'x.y'`); err == nil {
		t.Error("audit_events must reject UPDATE")
	}
}

// TestIdempotentBookingReplays proves a retried booking returns the original
// shipment instead of creating a second one.
func TestIdempotentBookingReplays(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "IDEM1"})

	key := [2]string{"Idempotency-Key", "booking-retry-0001"}
	body := tn.BookingBody(nil)

	first := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body, key)
	if first.Status != http.StatusCreated {
		t.Fatalf("first booking failed: %d %s", first.Status, first.Raw)
	}
	second := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body, key)
	if second.Status != http.StatusCreated {
		t.Fatalf("replay should return the original response, got %d: %s", second.Status, second.Raw)
	}
	if second.Headers.Get("Idempotent-Replay") != "true" {
		t.Error("expected the replay to be flagged with Idempotent-Replay: true")
	}
	if first.Body["awb"] != second.Body["awb"] || first.Body["id"] != second.Body["id"] {
		t.Fatalf("replay returned a different shipment: %v vs %v", first.Body["awb"], second.Body["awb"])
	}

	list := env.Do(t, "GET", "/api/v1/shipments", tn.AdminAccessTok, nil)
	data, _ := list.Body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected exactly one shipment after the retry, got %d", len(data))
	}
}

// TestIdempotencyKeyReuseWithDifferentBodyIsRejected proves the request digest
// check: the same key with a different payload must never replay.
func TestIdempotencyKeyReuseWithDifferentBodyIsRejected(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "IDEM2"})

	key := [2]string{"Idempotency-Key", "booking-reuse-0001"}
	if resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok,
		tn.BookingBody(nil), key); resp.Status != http.StatusCreated {
		t.Fatalf("first booking failed: %d %s", resp.Status, resp.Raw)
	}

	different := tn.BookingBody(map[string]any{"contentDescription": "Something else entirely"})
	resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, different, key)
	if resp.Status != http.StatusConflict {
		t.Fatalf("expected 409 for key reuse with a different body, got %d: %s", resp.Status, resp.Raw)
	}
	if code := resp.ErrorCode(); code != "IDEMPOTENCY_KEY_REUSED" {
		t.Errorf("expected IDEMPOTENCY_KEY_REUSED, got %q", code)
	}
}

// TestConcurrentBookingsProduceUniqueAWBs is the Constitution §22 proof.
//
// 120 concurrent bookings must produce 120 distinct AWBs and 120 shipments,
// with no duplicates and no lost writes. The AWB allocator is a single atomic
// UPSERT, so this exercises PostgreSQL's row locking rather than any
// application-level coordination.
func TestConcurrentBookingsProduceUniqueAWBs(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CONC1"})
	ctx := context.Background()

	const concurrency = 120
	var wg sync.WaitGroup
	var mu sync.Mutex
	awbs := make(map[string]int, concurrency)
	failures := make([]string, 0)

	// Warm the caches so the burst measures allocation contention rather than
	// a thundering herd of identical cache misses.
	if resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil)); resp.Status != http.StatusCreated {
		t.Fatalf("warm-up booking failed: %d %s", resp.Status, resp.Raw)
	}

	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			body := tn.BookingBody(map[string]any{
				"referenceNumber": fmt.Sprintf("CONC-%04d", i),
			})
			resp := env.DoRaw(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body)
			mu.Lock()
			defer mu.Unlock()
			if resp.Status != http.StatusCreated {
				failures = append(failures, fmt.Sprintf("#%d -> %d %s", i, resp.Status, resp.Raw))
				return
			}
			awb, _ := resp.Body["awb"].(string)
			awbs[awb]++
		}(i)
	}
	close(start)
	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("%d/%d concurrent bookings failed; first few: %v",
			len(failures), concurrency, failures[:min(3, len(failures))])
	}
	if len(awbs) != concurrency {
		for awb, count := range awbs {
			if count > 1 {
				t.Errorf("AWB %s was issued %d times", awb, count)
			}
		}
		t.Fatalf("expected %d distinct AWBs, got %d", concurrency, len(awbs))
	}

	var total, distinct int
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT count(*)::int, count(DISTINCT awb)::int FROM shipments WHERE organization_id = $1`,
		tn.OrgID).Scan(&total, &distinct); err != nil {
		t.Fatalf("count shipments: %v", err)
	}
	if total != concurrency+1 || distinct != total {
		t.Fatalf("expected %d shipments with distinct AWBs, got %d rows / %d distinct",
			concurrency+1, total, distinct)
	}
}

// TestConcurrentIdempotentBookingsCreateOne proves that a client retrying the
// same key in parallel — the classic mobile-network double submit — produces
// exactly one shipment.
func TestConcurrentIdempotentBookingsCreateOne(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "IDEM3"})
	ctx := context.Background()

	const attempts = 20
	key := [2]string{"Idempotency-Key", "parallel-retry-0001"}
	body := tn.BookingBody(nil)

	var wg sync.WaitGroup
	var mu sync.Mutex
	created, conflicts := 0, 0
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp := env.DoRaw(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, body, key)
			mu.Lock()
			defer mu.Unlock()
			switch resp.Status {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
				conflicts++
			default:
				t.Errorf("unexpected status %d: %s", resp.Status, resp.Raw)
			}
		}()
	}
	close(start)
	wg.Wait()

	// Every caller either gets the created/replayed response or a clear
	// in-progress conflict; nobody gets a second shipment.
	if created+conflicts != attempts {
		t.Fatalf("accounted for %d of %d attempts", created+conflicts, attempts)
	}
	var shipments int
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT count(*)::int FROM shipments WHERE organization_id = $1`, tn.OrgID).Scan(&shipments); err != nil {
		t.Fatalf("count shipments: %v", err)
	}
	if shipments != 1 {
		t.Fatalf("expected exactly one shipment from %d concurrent identical requests, got %d",
			attempts, shipments)
	}
}

// TestCancellationIsGuardedByTheStateMachine covers the legal-transition rules
// and the concurrency control around them.
func TestCancellationIsGuardedByTheStateMachine(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CANCEL"})

	booking := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil))
	if booking.Status != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", booking.Status, booking.Raw)
	}
	shipmentID, _ := booking.Body["id"].(string)

	// A reason is mandatory.
	if resp := env.Do(t, "POST", "/api/v1/shipments/"+shipmentID+"/cancel", tn.AdminAccessTok,
		map[string]any{"reason": "no"}); resp.Status != http.StatusUnprocessableEntity {
		t.Errorf("expected a validation failure for a too-short reason, got %d", resp.Status)
	}

	cancelled := env.Do(t, "POST", "/api/v1/shipments/"+shipmentID+"/cancel", tn.AdminAccessTok,
		map[string]any{"reason": "Customer changed their mind"})
	if cancelled.Status != http.StatusOK {
		t.Fatalf("cancel failed: %d %s", cancelled.Status, cancelled.Raw)
	}
	if cancelled.Body["status"] != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %v", cancelled.Body["status"])
	}

	// CANCELLED is terminal.
	again := env.Do(t, "POST", "/api/v1/shipments/"+shipmentID+"/cancel", tn.AdminAccessTok,
		map[string]any{"reason": "Trying again for no reason"})
	if again.Status != http.StatusConflict {
		t.Fatalf("expected 409 on a terminal shipment, got %d: %s", again.Status, again.Raw)
	}
	if code := again.ErrorCode(); code != "SHIPMENT_INVALID_STATE" {
		t.Errorf("expected SHIPMENT_INVALID_STATE, got %q", code)
	}

	// A cancelled shipment produces no label.
	if resp := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID+"/label", tn.AdminAccessTok, nil); resp.Status != http.StatusConflict {
		t.Errorf("expected 409 when labelling a cancelled shipment, got %d", resp.Status)
	}

	// The cancellation is a second append-only event.
	events := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID+"/events", tn.AdminAccessTok, nil)
	list, _ := events.Body["data"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected two events after cancellation, got %d", len(list))
	}
}

// TestLabelPayloadIsScannable checks the barcode and QR payloads plus ZPL.
func TestLabelPayloadIsScannable(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "LABEL"})

	booking := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, tn.BookingBody(nil))
	shipmentID, _ := booking.Body["id"].(string)
	awb, _ := booking.Body["awb"].(string)

	label := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID+"/label", tn.AdminAccessTok, nil)
	if label.Status != http.StatusOK {
		t.Fatalf("label failed: %d %s", label.Status, label.Raw)
	}
	if label.Body["barcodePayload"] != awb {
		t.Errorf("barcode payload must be the AWB, got %v", label.Body["barcodePayload"])
	}
	qr, _ := label.Body["qrPayload"].(string)
	if qr == "" || qr[:5] != "CSV1|" {
		t.Errorf("QR payload must be version-prefixed, got %q", qr)
	}
	pieces, _ := label.Body["pieces"].([]any)
	if len(pieces) != 1 {
		t.Fatalf("expected one piece, got %d", len(pieces))
	}

	zpl := env.Do(t, "GET", "/api/v1/shipments/"+shipmentID+"/label?format=ZPL", tn.AdminAccessTok, nil)
	if zpl.Status != http.StatusOK {
		t.Fatalf("ZPL label failed: %d", zpl.Status)
	}
	if len(zpl.Raw) < 100 || zpl.Raw[:3] != "^XA" {
		t.Errorf("expected a ZPL document starting with ^XA, got %.40q", zpl.Raw)
	}
}

// TestCursorPaginationIsStable proves the keyset listing walks every row
// exactly once.
func TestCursorPaginationIsStable(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "PAGE"})

	const total = 12
	for i := 0; i < total; i++ {
		resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok,
			tn.BookingBody(map[string]any{"referenceNumber": fmt.Sprintf("PAGE-%03d", i)}))
		if resp.Status != http.StatusCreated {
			t.Fatalf("booking %d failed: %d %s", i, resp.Status, resp.Raw)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		path := "/api/v1/shipments?limit=5"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		resp := env.Do(t, "GET", path, tn.AdminAccessTok, nil)
		if resp.Status != http.StatusOK {
			t.Fatalf("list failed: %d %s", resp.Status, resp.Raw)
		}
		data, _ := resp.Body["data"].([]any)
		for _, item := range data {
			row, _ := item.(map[string]any)
			id, _ := row["id"].(string)
			if seen[id] {
				t.Fatalf("shipment %s appeared on more than one page", id)
			}
			seen[id] = true
		}
		meta, _ := resp.Body["pagination"].(map[string]any)
		if hasMore, _ := meta["hasMore"].(bool); !hasMore {
			break
		}
		cursor, _ = meta["nextCursor"].(string)
		if cursor == "" {
			t.Fatal("hasMore was true but no nextCursor was returned")
		}
	}
	if len(seen) != total {
		t.Fatalf("expected to page through %d shipments, saw %d", total, len(seen))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
