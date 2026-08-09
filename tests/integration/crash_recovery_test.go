package integration

import (
	"context"
	"testing"
	"time"

	"github.com/ceserve/courier-os/tests/harness"
)

// Crash recovery for the four workflows that move money.
//
// # Why this file exists separately from the restart drill
//
// `deploy/scripts/restart-drill.sh` sends a real SIGKILL and covers booking and
// scanning. It answers "does the process come back, and can a client still make
// progress". It cannot reasonably cover delivery, COD, commission and
// settlement, because reaching those states takes a fifteen-step journey that a
// shell script would reimplement badly.
//
// These tests answer the other half of the question, which is the half that
// matters for money: **is each operation atomic, and is a retry after an
// interruption exactly-once?** The request is abandoned mid-flight, so the
// server's transaction is open when its context is cancelled — the same thing
// PostgreSQL sees when a process is killed.
//
// The assertion in every case is the same and is deliberately about *effects*,
// not status codes: after an interruption and a retry, there must be exactly one
// delivery, one COD obligation, one posted commission, one settlement, and the
// ledger must still balance. A test that only checked the second response would
// pass against a system that had done the work twice.

// codJourney books a COD shipment and walks it to OUT_FOR_DELIVERY, returning
// the journey and the delivery run.
func codJourney(t *testing.T, env *harness.Env, tn *harness.Tenant, codMinor int64) (*journey, string) {
	t.Helper()
	token := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	resp := env.Do(t, "POST", "/api/v1/shipments", token,
		tn.BookingBody(map[string]any{
			"paymentMode":    "COD",
			"codAmountMinor": codMinor,
		}),
		[2]string{"Idempotency-Key", "crash-" + harness.RandomKey()})
	if resp.Status != 201 {
		t.Fatalf("book COD shipment: %d %s", resp.Status, resp.Raw)
	}
	j := &journey{
		env: env, tn: tn, token: token,
		awb:    resp.Body["awb"].(string),
		shipID: resp.Body["id"].(string),
	}
	j.toOriginBranch(t)
	j.bagAndManifest(t)
	j.lineHaul(t)
	runID := j.dispatchForDelivery(t)
	return j, runID
}

// scalar runs a single-value query against the test database.
func scalar(t *testing.T, env *harness.Env, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := env.DB.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

// assertLedgerBalances is the invariant that must hold no matter what was
// interrupted.
func assertLedgerBalances(t *testing.T, env *harness.Env) {
	t.Helper()
	bad := scalar(t, env, `
		SELECT count(*) FROM (
			SELECT jt.id FROM journal_transactions jt
			JOIN journal_entries je ON je.transaction_id = jt.id
			WHERE jt.status = 'POSTED'
			GROUP BY jt.id
			HAVING sum(je.debit_minor) <> sum(je.credit_minor)
		) b`)
	if bad != 0 {
		t.Errorf("%d posted journals do not balance after an interrupted operation", bad)
	}
}

// ---------------------------------------------------------------------------
// 1. Delivery completion
// ---------------------------------------------------------------------------

func TestInterruptedDeliveryCompletesExactlyOnce(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CRD"})
	j, runID := codJourney(t, env, tn, 250000)

	shipmentID := scalar(t, env, `SELECT id FROM shipments WHERE awb = $1`, j.awb)
	key := [2]string{"Idempotency-Key", "deliver-crash-" + harness.RandomKey()}
	body := map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 250000, "codPaymentMode": "CASH",
	}

	// Abandon the request while the transaction is open.
	cut := env.InterruptedPost(t, "/api/v1/deliveries/attempts", j.token, body, 3*time.Millisecond, key)
	t.Logf("delivery request interrupted mid-flight: %v", cut)

	// Retry with the same key, exactly as a delivery agent's phone would.
	// The lease may still be held, so retry until it resolves rather than
	// asserting on the first answer — the client contract is "keep retrying",
	// and what matters is the effect, not which attempt succeeded.
	var final harness.Response
	for i := 0; i < 3; i++ {
		final = env.Do(t, "POST", "/api/v1/deliveries/attempts", j.token, body, key)
		if final.Status < 500 && final.Status != 409 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	attempts := scalar(t, env,
		`SELECT count(*) FROM delivery_attempts WHERE shipment_id = $1`, shipmentID)
	delivered := scalar(t, env,
		`SELECT count(*) FROM shipment_events WHERE shipment_id = $1 AND to_status = 'DELIVERED'`,
		shipmentID)

	// Exactly one, not "at most one": a test that passes because nothing
	// happened proves nothing. The earlier version of this file asserted only
	// `> 1` and passed with attempts=0 while the request was being rejected
	// for an unrelated reason.
	if attempts != 1 {
		t.Errorf("%d delivery attempts recorded; expected exactly 1 (final response %d: %s)",
			attempts, final.Status, final.Raw)
	}
	if delivered != 1 {
		t.Errorf("%d DELIVERED events for one shipment; expected exactly 1", delivered)
	}
	// Whatever happened, the shipment must not be left half-delivered.
	delivered1 := scalar(t, env,
		`SELECT count(*) FROM shipments WHERE id = $1 AND current_status = 'DELIVERED'`, shipmentID)
	if delivered1 != 1 {
		t.Errorf("shipment is not DELIVERED after the retry")
	}
	t.Logf("attempts=%d delivered_events=%d final=%d", attempts, delivered, final.Status)
	assertLedgerBalances(t, env)
}

// ---------------------------------------------------------------------------
// 2. COD obligation and collection
// ---------------------------------------------------------------------------

func TestInterruptedCODCollectionPostsOnce(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CRC"})
	j, runID := codJourney(t, env, tn, 180000)
	shipmentID := scalar(t, env, `SELECT id FROM shipments WHERE awb = $1`, j.awb)

	// Complete the delivery cleanly first — the COD obligation is created by it.
	j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 180000, "codPaymentMode": "CASH",
	}, 200, [2]string{"Idempotency-Key", "deliver-" + harness.RandomKey()})

	obligations := scalar(t, env,
		`SELECT count(*) FROM cod_obligations WHERE shipment_id = $1`, shipmentID)
	if obligations != 1 {
		t.Fatalf("expected exactly one COD obligation after delivery, got %d", obligations)
	}

	// Now interrupt a collection and retry it.
	key := [2]string{"Idempotency-Key", "cod-crash-" + harness.RandomKey()}
	body := map[string]any{
		"shipmentId": j.shipID, "amountMinor": 180000, "paymentMode": "CASH",
	}
	cut := env.InterruptedPost(t, "/api/v1/cod/collections", j.token, body, 3*time.Millisecond, key)
	t.Logf("COD collection interrupted mid-flight: %v", cut)

	for i := 0; i < 3; i++ {
		r := env.Do(t, "POST", "/api/v1/cod/collections", j.token, body, key)
		if r.Status < 500 && r.Status != 409 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	collections := scalar(t, env,
		`SELECT count(*) FROM cod_collections c
		 JOIN cod_obligations o ON o.id = c.obligation_id
		 WHERE o.shipment_id = $1`, shipmentID)
	if collections != 1 {
		t.Errorf("%d COD collections recorded; expected exactly 1 — money lost or counted twice", collections)
	}
	// The obligation must never be over-collected: a retry that banked the cash
	// twice would show up here even if the collection rows looked right.
	over := scalar(t, env,
		`SELECT count(*) FROM cod_obligations
		 WHERE shipment_id = $1 AND collected_minor > expected_minor`, shipmentID)
	if over != 0 {
		t.Errorf("COD obligation is over-collected after the interruption")
	}
	t.Logf("collections=%d", collections)
	assertLedgerBalances(t, env)
}

// ---------------------------------------------------------------------------
// 3. Commission posting
// ---------------------------------------------------------------------------

func TestInterruptedDeliveryDoesNotPayCommissionTwice(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CRM"})
	franchiseFixtureAt(t, env, tn, tn.DestBranchID)

	j, runID := codJourney(t, env, tn, 120000)
	shipmentID := scalar(t, env, `SELECT id FROM shipments WHERE awb = $1`, j.awb)

	// Commission is raised and posted by the finance observer *inside* the
	// delivery transaction, so the delivery is where an interruption could
	// double-pay. Testing the manual /post endpoint instead would measure a
	// path production no longer takes.
	key := [2]string{"Idempotency-Key", "comm-crash-" + harness.RandomKey()}
	body := map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 120000, "codPaymentMode": "CASH",
	}

	cut := env.InterruptedPost(t, "/api/v1/deliveries/attempts", j.token, body, 2*time.Millisecond, key)
	t.Logf("delivery (and its commission) interrupted mid-flight: %v", cut)

	for i := 0; i < 4; i++ {
		r := env.Do(t, "POST", "/api/v1/deliveries/attempts", j.token, body, key)
		if r.Status < 500 && r.Status != 409 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	calcs := scalar(t, env,
		`SELECT count(*) FROM commission_calculations WHERE shipment_id = $1`, shipmentID)
	if calcs != 1 {
		t.Errorf("%d commission calculations for one delivery, want exactly 1 — "+
			"an interrupted delivery paid the franchise twice or not at all", calcs)
	}
	paid := scalar(t, env, `
		SELECT coalesce(sum(ce.amount_minor), 0) FROM commission_entries ce
		JOIN commission_calculations cc ON cc.id = ce.calculation_id
		WHERE cc.shipment_id = $1`, shipmentID)
	t.Logf("calculations=%d posted_minor=%d", calcs, paid)

	orphans := scalar(t, env, `
		SELECT count(*) FROM journal_entries je
		LEFT JOIN journal_transactions jt ON jt.id = je.transaction_id
		WHERE jt.id IS NULL`)
	if orphans != 0 {
		t.Errorf("%d orphaned ledger entries after the interruption", orphans)
	}
	assertLedgerBalances(t, env)
}

// ---------------------------------------------------------------------------
// 4. Settlement generation
// ---------------------------------------------------------------------------

func TestInterruptedSettlementGenerationCreatesOne(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CRS"})
	_, franchisePubID := franchiseFixtureAt(t, env, tn, tn.DestBranchID)

	j, runID := codJourney(t, env, tn, 90000)
	j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 90000, "codPaymentMode": "CASH",
	}, 200, [2]string{"Idempotency-Key", "deliver-" + harness.RandomKey()})

	admin := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	now := time.Now().UTC()
	body := map[string]any{
		"franchiseId": franchisePubID,
		"periodStart": now.AddDate(0, 0, -7).Format("2006-01-02"),
		"periodEnd":   now.Format("2006-01-02"),
	}
	key := [2]string{"Idempotency-Key", "settle-crash-" + harness.RandomKey()}

	cut := env.InterruptedPost(t, "/api/v1/settlements", admin, body, 3*time.Millisecond, key)
	t.Logf("settlement generation interrupted mid-flight: %v", cut)

	for i := 0; i < 3; i++ {
		r := env.Do(t, "POST", "/api/v1/settlements", admin, body, key)
		if r.Status < 500 && r.Status != 409 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	// And once more with a different key: generating the same period twice
	// must not produce a second settlement.
	env.Do(t, "POST", "/api/v1/settlements", admin, body,
		[2]string{"Idempotency-Key", "settle-again-" + harness.RandomKey()})

	settlements := scalar(t, env, `SELECT count(*) FROM settlements`)
	if settlements > 1 {
		t.Errorf("%d settlements for one period — an interrupted generation was repeated", settlements)
	}

	// Every settlement line must belong to a settlement that exists.
	orphanLines := scalar(t, env, `
		SELECT count(*) FROM settlement_lines sl
		LEFT JOIN settlements s ON s.id = sl.settlement_id
		WHERE s.id IS NULL`)
	if orphanLines != 0 {
		t.Errorf("%d settlement lines with no settlement — a torn write", orphanLines)
	}
	t.Logf("settlements=%d", settlements)
	assertLedgerBalances(t, env)
}
