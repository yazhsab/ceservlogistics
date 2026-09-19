package integration

import (
	"testing"
	"time"

	"github.com/ceserve/courier-os/tests/harness"
)

// Is the finance core actually connected to operations?
//
// The commission engine (M21), the COD custody model (M23) and settlement (M24)
// are each built and each individually tested. This file asks a different
// question, which no other test asks: **when a COD parcel is delivered, does any
// of it happen?**
//
// The question came out of writing the crash-recovery tests. Interrupting a COD
// collection needs an obligation to collect against, and after delivering a COD
// shipment through the full operational journey there was no obligation at all.
// `cod_test.go` seeds one by calling the service directly, with the comment
// "which is what the booking path will do once M23 is wired to it" — so the gap
// was known and recorded in a place nobody would look.
//
// These tests were written first as failing tests that documented the gap in
// their own output rather than in a comment. `internal/financeops` now closes
// it — a transition observer that opens the COD liability and raises the
// commission inside the state-change transaction — and these are the tests that
// prove it, in the terms a franchise owner would use: was the cash recorded,
// was the commission earned, does the settlement have lines.

func TestDeliveringACODShipmentRecordsTheCashOwed(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "WCOD"})

	j, runID := codJourney(t, env, tn, 250000)
	shipmentID := scalar(t, env, `SELECT id FROM shipments WHERE awb = $1`, j.awb)

	resp := j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 250000, "codPaymentMode": "CASH",
	}, 200, [2]string{"Idempotency-Key", "deliver-" + harness.RandomKey()})

	// The attempt itself records the cash. That part works.
	onAttempt := scalar(t, env,
		`SELECT coalesce(sum(cod_collected_minor), 0) FROM delivery_attempts WHERE shipment_id = $1`,
		shipmentID)
	if onAttempt != 250000 {
		t.Errorf("delivery attempt recorded %d minor collected, want 250000 (%s)", onAttempt, resp.Raw)
	}

	// The custody model is the question.
	obligations := scalar(t, env,
		`SELECT count(*) FROM cod_obligations WHERE shipment_id = $1`, shipmentID)
	collections := scalar(t, env,
		`SELECT count(*) FROM cod_collections c
		 JOIN cod_obligations o ON o.id = c.obligation_id WHERE o.shipment_id = $1`, shipmentID)

	// Exactly one: the liability is opened at delivery and a retried delivery
	// must not open a second one for the same parcel.
	if obligations != 1 {
		t.Errorf("%d COD obligations for one delivered COD parcel, want 1. Without it there is no "+
			"record of cash owed, no custody chain and nothing to reconcile — the amount would live "+
			"only on the delivery attempt. (collections=%d)", obligations, collections)
	}
}

func TestDeliveringAShipmentRaisesFranchiseCommission(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "WCOM"})
	// The delivering branch, because the rule pays the DESTINATION_FRANCHISE.
	franchiseFixtureAt(t, env, tn, tn.DestBranchID)

	j, runID := codJourney(t, env, tn, 120000)
	j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 120000, "codPaymentMode": "CASH",
	}, 200, [2]string{"Idempotency-Key", "deliver-" + harness.RandomKey()})

	calcs := scalar(t, env, `SELECT count(*) FROM commission_calculations`)
	entries := scalar(t, env, `SELECT count(*) FROM commission_entries`)

	if calcs != 1 {
		t.Errorf("%d commission calculations for one delivery, want 1. Franchise settlements are "+
			"built from these, so none means a settlement with nothing to settle. (entries=%d)",
			calcs, entries)
	}
	// Posted in the same transaction, so it is visible to the ledger and to
	// settlement rather than sitting calculated-but-invisible.
	if entries == 0 {
		t.Errorf("commission was calculated but never posted: %d ledger entries", entries)
	}
}

// The consequence of the two gaps above, stated as its own test because it is
// the one a franchise owner would notice.
func TestASettlementForRealDeliveryWorkIsNotEmpty(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "WSET"})
	_, franchisePubID := franchiseFixtureAt(t, env, tn, tn.DestBranchID)

	j, runID := codJourney(t, env, tn, 90000)
	j.post(t, "/api/v1/deliveries/attempts", map[string]any{
		"barcode": j.awb, "outcome": "DELIVERED", "runId": runID,
		"recipientName": "R Sharma", "recipientRelationship": "SELF",
		"codCollectedMinor": 90000, "codPaymentMode": "CASH",
	}, 200, [2]string{"Idempotency-Key", "deliver-" + harness.RandomKey()})

	admin := env.Login(t, tn.AdminEmail, tn.AdminPassword)
	resp := env.Do(t, "POST", "/api/v1/settlements", admin, map[string]any{
		"franchiseId": franchisePubID,
		"periodStart": time.Now().UTC().Format("2006-01-02"),
		"periodEnd":   time.Now().UTC().Format("2006-01-02"),
	}, [2]string{"Idempotency-Key", "settle-" + harness.RandomKey()})
	if resp.Status != 201 && resp.Status != 200 {
		t.Fatalf("generate settlement: %d %s", resp.Status, resp.Raw)
	}

	lines := scalar(t, env, `SELECT count(*) FROM settlement_lines`)
	if lines == 0 {
		t.Errorf("a settlement generated for a period containing a real delivery has no lines. "+
			"An arithmetically correct, economically empty settlement is what this whole path "+
			"exists to prevent. (response %s)", resp.Raw)
	}
	// And it must actually owe something.
	net := scalar(t, env, `SELECT coalesce(max(commission_minor), 0) FROM settlements`)
	if net == 0 {
		t.Errorf("settlement has lines but zero commission — the amounts did not carry through")
	}
}
