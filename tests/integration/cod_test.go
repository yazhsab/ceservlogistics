package integration

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/cod"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/tests/harness"
)

// These tests walk cash along the real custody chain and check the ledger after
// every hop. The property that matters is that the operational record (who
// holds what) and the books (what it is worth) are written by different code
// paths and must still agree — CustodyPosition compares them, and a mismatch is
// the bug class these tests exist to catch.

func codService(env *harness.Env) (*cod.Service, *ledger.Service) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := audit.NewRecorder(env.Queries, log)
	led := ledger.NewService(env.DB, env.Queries, rec, log)
	return cod.NewService(env.DB, env.Queries, led, rec, log, nil), led
}

// codShipment books a COD shipment directly in the database and opens its
// obligation, which is what the booking path will do once M23 is wired to it.
func codShipment(
	t *testing.T, env *harness.Env, p *tenant.Principal, tn *harness.Tenant,
	svc *cod.Service, amountMinor int64,
) dbgen.CodObligation {
	t.Helper()
	ctx := context.Background()

	var shipmentID int64
	var awb string
	err := env.DB.Pool.QueryRow(ctx, `
		INSERT INTO shipments (public_id, organization_id, awb, customer_id, booking_unit_id,
			courier_service_id, payment_mode, current_status, movement_direction,
			origin_pincode, destination_pincode, piece_count,
			actual_weight_grams, chargeable_weight_grams,
			currency, cod_amount_minor, total_amount_minor, content_description,
			origin_branch_id, destination_branch_id, booked_at)
		SELECT gen_seed_public_id('shp'), $1,
			'COD' || lpad((nextval(pg_get_serial_sequence('shipments','id')))::text, 12, '0'),
			c.id, $2, $5, 'COD', 'DELIVERED', 'FORWARD', '100001', '900001', 1,
			500, 500, 'NGN', $3, $3, 'COD test', $2, $4, now()
		FROM customers c
		WHERE c.organization_id = $1
		LIMIT 1
		RETURNING id, awb`,
		tn.OrgID, tn.OriginBranchID, amountMinor, tn.DestBranchID, tn.ServiceID).Scan(&shipmentID, &awb)
	if err != nil {
		t.Fatalf("seed COD shipment: %v", err)
	}

	sh, err := env.Queries.GetShipmentByID(ctx, dbgen.GetShipmentByIDParams{
		OrganizationID: tn.OrgID, ID: shipmentID,
	})
	if err != nil {
		t.Fatalf("read shipment: %v", err)
	}

	var obligation *dbgen.CodObligation
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var oErr error
		obligation, oErr = svc.OpenObligation(ctx, tx, p, sh)
		return oErr
	}); err != nil {
		t.Fatalf("open obligation: %v", err)
	}
	return *obligation
}

// TestCODCustodyChainKeepsBooksAndOperationsInStep is the main scenario:
// collect at the door, hand to the branch, hand to the franchise, remit to head
// office — checking the ledger at every hop.
func TestCODCustodyChainKeepsBooksAndOperationsInStep(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, led := codService(env)
	ctx := context.Background()

	const amount = 200000 // ₹2,000
	obligation := codShipment(t, env, p, tn, svc, amount)

	if obligation.Status != cod.StatusExpected {
		t.Fatalf("a new obligation is %s, want EXPECTED", obligation.Status)
	}

	// --- 1. Collection at the door ------------------------------------------
	agentID := tn.AdminUserID
	var collect *cod.CollectResult
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var cErr error
		collect, cErr = svc.RecordCollection(ctx, tx, p, cod.CollectRequest{
			ShipmentID: obligation.ShipmentID, AmountMinor: amount,
			PaymentMode: "CASH", CollectedAt: time.Now().UTC(),
			AgentUserID: &agentID,
		})
		return cErr
	}); err != nil {
		t.Fatalf("record collection: %v", err)
	}
	if collect.Obligation.Status != cod.StatusAgentCollected {
		t.Fatalf("after collection status = %s, want AGENT_COLLECTED", collect.Obligation.Status)
	}

	// The agent owes the network; the network owes the consignor.
	assertPartyBalance(t, led, p, ledger.PartyAgent, agentID, amount,
		"the agent should hold the cash")
	assertAccount(t, led, p, ledger.AcctCODPayableConsignor, amount,
		"the consignor should be owed the cash")
	assertCustodyAgrees(t, svc, p, cod.PartyAgent, agentID)

	// --- 2. Agent hands in to the branch ------------------------------------
	branchID := tn.OriginBranchID
	transfer, _, err := svc.DeclareTransfer(ctx, p, cod.DeclareTransferRequest{
		FromType: cod.PartyAgent, FromID: agentID,
		ToType: cod.PartyUnit, ToID: branchID,
		ObligationIDs: []string{obligation.PublicID},
	})
	if err != nil {
		t.Fatalf("declare transfer: %v", err)
	}
	if transfer.DeclaredMinor != amount {
		t.Fatalf("declared = %d, want %d", transfer.DeclaredMinor, amount)
	}

	if _, err := svc.AcceptTransfer(ctx, p, cod.AcceptTransferRequest{
		TransferID: transfer.PublicID, AcceptedMinor: amount,
	}); err != nil {
		t.Fatalf("accept transfer: %v", err)
	}

	// The receivable has moved from the agent to the branch.
	assertPartyBalance(t, led, p, ledger.PartyAgent, agentID, 0,
		"the agent should be clear after handing in")
	assertPartyBalance(t, led, p, ledger.PartyOperatingUnit, branchID, amount,
		"the branch should now hold the cash")
	assertCustodyAgrees(t, svc, p, cod.PartyUnit, branchID)

	// --- 3. Remit to head office --------------------------------------------
	remittance, err := svc.CreateRemittance(ctx, p, cod.RemitRequest{
		FromType: cod.PartyUnit, FromID: branchID,
		BeneficiaryType: "HEAD_OFFICE",
		ObligationIDs:   []string{obligation.PublicID},
		PaymentMode:     "BANK_TRANSFER", Reference: "NEFT-0001",
		PaidOn: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create remittance: %v", err)
	}
	if _, err := svc.ConfirmRemittance(ctx, p, remittance.PublicID); err != nil {
		t.Fatalf("confirm remittance: %v", err)
	}

	// The branch is clear and head office holds the cash.
	assertPartyBalance(t, led, p, ledger.PartyOperatingUnit, branchID, 0,
		"the branch should be clear after remitting")
	assertAccount(t, led, p, ledger.AcctCash, amount,
		"head office should hold the banked cash")
	// The consignor is still owed — remitting to head office does not pay the
	// sender, and conflating the two is exactly the error this separation avoids.
	assertAccount(t, led, p, ledger.AcctCODPayableConsignor, amount,
		"the consignor is still owed until they are actually paid")

	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after the custody chain: %v", err)
	}
}

// TestCODCollectionIsIdempotentOnDeliveryAttempt proves the doorstep retry is
// safe — the single most-retried finance operation in the platform.
func TestCODCollectionIsIdempotentOnDeliveryAttempt(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, led := codService(env)
	ctx := context.Background()

	const amount = 150000
	obligation := codShipment(t, env, p, tn, svc, amount)

	collect := func() (*cod.CollectResult, error) {
		var r *cod.CollectResult
		err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
			var e error
			r, e = svc.RecordCollection(ctx, tx, p, cod.CollectRequest{
				ShipmentID: obligation.ShipmentID, AmountMinor: amount,
				PaymentMode: "UPI", Reference: "upi-ref-1",
				CollectedAt: time.Now().UTC(),
				Device:      opsDevice("handheld-7", "evt-42"),
			})
			return e
		})
		return r, err
	}

	first, err := collect()
	if err != nil {
		t.Fatalf("first collection: %v", err)
	}
	if first.Duplicate {
		t.Fatal("the first collection was reported as a duplicate")
	}

	second, err := collect()
	if err != nil {
		t.Fatalf("replayed collection: %v", err)
	}
	if !second.Duplicate {
		t.Fatal("the replayed collection was not recognised as a duplicate")
	}
	if second.Collection.ID != first.Collection.ID {
		t.Fatalf("the replay created collection %d, want the original %d",
			second.Collection.ID, first.Collection.ID)
	}

	// One collection row, and the consignor is owed the amount once.
	var rows int64
	mustQueryRow(t, env, `SELECT count(*) FROM cod_collections WHERE obligation_id=$1`,
		[]any{obligation.ID}, &rows)
	if rows != 1 {
		t.Fatalf("%d collection rows for one doorstep payment, want 1", rows)
	}
	assertAccount(t, led, p, ledger.AcctCODPayableConsignor, amount,
		"a replayed collection must not owe the consignor twice")
}

// TestCODShortageStaysChargeableToTheHolder is the variance case: a branch
// counts less than the agent declared, and the difference must remain somebody's
// responsibility rather than evaporating.
func TestCODShortageStaysChargeableToTheHolder(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, led := codService(env)
	ctx := context.Background()

	const declared = 200000
	const counted = 180000
	const short = declared - counted

	obligation := codShipment(t, env, p, tn, svc, declared)
	agentID := tn.AdminUserID

	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		_, e := svc.RecordCollection(ctx, tx, p, cod.CollectRequest{
			ShipmentID: obligation.ShipmentID, AmountMinor: declared,
			PaymentMode: "CASH", CollectedAt: time.Now().UTC(), AgentUserID: &agentID,
		})
		return e
	}); err != nil {
		t.Fatalf("collect: %v", err)
	}

	transfer, _, err := svc.DeclareTransfer(ctx, p, cod.DeclareTransferRequest{
		FromType: cod.PartyAgent, FromID: agentID,
		ToType: cod.PartyUnit, ToID: tn.OriginBranchID,
		ObligationIDs: []string{obligation.PublicID},
	})
	if err != nil {
		t.Fatalf("declare: %v", err)
	}

	// A variance without an explanation is refused.
	if _, err := svc.AcceptTransfer(ctx, p, cod.AcceptTransferRequest{
		TransferID: transfer.PublicID, AcceptedMinor: counted,
	}); err == nil {
		t.Fatal("a short hand-in was accepted without a reason")
	}

	accepted, err := svc.AcceptTransfer(ctx, p, cod.AcceptTransferRequest{
		TransferID: transfer.PublicID, AcceptedMinor: counted,
		VarianceReason: "envelope was ₹200 short at the counter",
	})
	if err != nil {
		t.Fatalf("accept with variance: %v", err)
	}
	if accepted.VarianceMinor != -short {
		t.Fatalf("variance = %d, want %d", accepted.VarianceMinor, -short)
	}

	// The branch holds what it counted.
	assertPartyBalance(t, led, p, ledger.PartyOperatingUnit, tn.OriginBranchID, counted,
		"the branch should hold only what it counted")
	// The agent's COD receivable is cleared by the hand-in, but the shortfall
	// follows them into COD Shortage Recoverable. PartyBalance aggregates every
	// account bound to a party, so it correctly still shows the agent owing the
	// missing ₹200 — which is the whole point: the money does not evaporate
	// just because the envelope was handed over.
	assertPartyBalance(t, led, p, ledger.PartyAgent, agentID, short,
		"the agent should still owe the shortfall after handing in")
	// Read the rollup, not the bare control account: the shortfall posts to the
	// agent's subsidiary account beneath 1400, which is where a per-party
	// recoverable belongs.
	rollup, subs, err := led.RollupBalance(ctx, p, ledger.AcctCODShortageRecoverable, nil)
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if rollup != short {
		t.Fatalf("COD shortage recoverable rolls up to %d across %d account(s), want %d — "+
			"the shortfall must remain recoverable from somebody, not vanish", rollup, subs, short)
	}

	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after a shortage: %v", err)
	}
}

// TestCODAdjustmentRequiresASecondPerson proves maker/checker on the operation
// that forgives money.
func TestCODAdjustmentRequiresASecondPerson(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, _ := codService(env)
	ctx := context.Background()

	obligation := codShipment(t, env, p, tn, svc, 100000)

	adj, err := svc.RequestAdjustment(ctx, p, cod.AdjustmentRequest{
		ObligationID:    obligation.PublicID,
		Type:            cod.AdjShortageWriteOff,
		AmountMinor:     5000,
		LiablePartyType: cod.PartyAgent,
		LiablePartyID:   &tn.AdminUserID,
		Reason:          "shortfall accepted as unrecoverable after investigation",
	})
	if err != nil {
		t.Fatalf("request adjustment: %v", err)
	}
	if adj.Status != "PENDING_APPROVAL" {
		t.Fatalf("a new adjustment is %s, want PENDING_APPROVAL", adj.Status)
	}

	// The maker cannot approve their own request.
	if _, err := svc.ApproveAdjustment(ctx, p, adj.PublicID); err == nil {
		t.Fatal("a user approved their own COD adjustment")
	}

	// A different person can.
	otherID, _, _ := env.NewUser(t, tn.OrgID,
		strings.ToLower("checker-"+harness.RandomKey()[:8])+"@test.local", "FINANCE_MANAGER", nil)
	checker := &tenant.Principal{
		UserID: otherID, OrganizationID: tn.OrgID, OrganizationCurrency: "NGN",
	}
	approved, err := svc.ApproveAdjustment(ctx, checker, adj.PublicID)
	if err != nil {
		t.Fatalf("approve by a second person: %v", err)
	}
	if approved.Status != "POSTED" {
		t.Fatalf("approved adjustment is %s, want POSTED", approved.Status)
	}
	if approved.JournalTransactionID == nil {
		t.Fatal("an approved adjustment must post to the ledger")
	}

	// And the database refuses self-approval even from raw SQL.
	_, rawErr := env.DB.Pool.Exec(ctx, `
		UPDATE cod_adjustments SET approved_by = requested_by, approved_at = now()
		WHERE id = $1`, adj.ID)
	if rawErr == nil {
		t.Fatal("raw SQL was able to set the approver to the requester")
	}
}

// TestCODIsTenantIsolated proves one tenant cannot reach another's cash.
func TestCODIsTenantIsolated(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	a := env.NewTenant(t, geo, harness.TenantOptions{})
	b := env.NewTenant(t, geo, harness.TenantOptions{})

	pa := &tenant.Principal{UserID: a.AdminUserID, OrganizationID: a.OrgID, OrganizationCurrency: "NGN"}
	pb := &tenant.Principal{UserID: b.AdminUserID, OrganizationID: b.OrgID, OrganizationCurrency: "NGN"}
	svc, _ := codService(env)
	ctx := context.Background()

	obligation := codShipment(t, env, pa, a, svc, 250000)

	// B cannot read A's obligation.
	if _, _, err := svc.GetObligation(ctx, pb, obligation.PublicID); err == nil {
		t.Fatal("tenant B read tenant A's COD obligation")
	}
	// B cannot include it in a transfer.
	if _, _, err := svc.DeclareTransfer(ctx, pb, cod.DeclareTransferRequest{
		FromType: cod.PartyAgent, FromID: b.AdminUserID,
		ToType: cod.PartyUnit, ToID: b.OriginBranchID,
		ObligationIDs: []string{obligation.PublicID},
	}); err == nil {
		t.Fatal("tenant B moved tenant A's COD into its own custody")
	}
	// B's COD summary is empty.
	summary, err := svc.Summary(ctx, pb, nil)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.ExpectedCount != 0 || summary.ExpectedMinor != 0 {
		t.Fatalf("tenant B sees %d COD shipments worth %d from tenant A",
			summary.ExpectedCount, summary.ExpectedMinor)
	}
}

// TestExpectedCODAmountIsFixedAtBooking proves the amount owed cannot be edited
// after the fact, which is how COD fraud would otherwise be hidden.
func TestExpectedCODAmountIsFixedAtBooking(t *testing.T) {
	env, tn, p := financeEnv(t)
	svc, _ := codService(env)
	ctx := context.Background()

	obligation := codShipment(t, env, p, tn, svc, 200000)

	_, err := env.DB.Pool.Exec(ctx,
		`UPDATE cod_obligations SET expected_minor = 100 WHERE id = $1`, obligation.ID)
	if err == nil {
		t.Fatal("the expected COD amount was edited after booking")
	}
	if !containsAny(err.Error(), "COD_IMMUTABLE", "fixed at booking") {
		t.Fatalf("expected the immutability guard, got: %v", err)
	}

	_, err = env.DB.Pool.Exec(ctx, `DELETE FROM cod_obligations WHERE id = $1`, obligation.ID)
	if err == nil {
		t.Fatal("a COD obligation was deleted")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func opsDevice(id, eventID string) ops.Device {
	return ops.Device{ID: id, EventID: eventID, Source: "MOBILE"}
}

func assertPartyBalance(
	t *testing.T, led *ledger.Service, p *tenant.Principal,
	partyType string, partyID int64, want int64, why string,
) {
	t.Helper()
	got, err := led.PartyBalance(context.Background(), p, partyType, partyID, nil)
	if err != nil {
		t.Fatalf("party balance: %v", err)
	}
	if got != want {
		t.Fatalf("%s: %s %d balance = %d, want %d", why, partyType, partyID, got, want)
	}
}

func assertAccount(
	t *testing.T, led *ledger.Service, p *tenant.Principal,
	code string, want int64, why string,
) {
	t.Helper()
	accounts, _, err := led.ListAccounts(context.Background(), p, "", "", code, nil, 10, 0)
	if err != nil {
		t.Fatalf("find account %s: %v", code, err)
	}
	var publicID string
	for _, a := range accounts {
		if a.Code == code {
			publicID = a.ID
		}
	}
	if publicID == "" {
		t.Fatalf("account %s not found", code)
	}
	view, err := led.AccountBalance(context.Background(), p, publicID, nil)
	if err != nil {
		t.Fatalf("balance of %s: %v", code, err)
	}
	got := int64(0)
	if view.BalanceMinor != nil {
		got = *view.BalanceMinor
	}
	if got != want {
		t.Fatalf("%s: account %s balance = %d, want %d", why, code, got, want)
	}
}

// assertCustodyAgrees checks the operational record against the ledger. They
// are written by different code paths, so agreement is meaningful.
func assertCustodyAgrees(
	t *testing.T, svc *cod.Service, p *tenant.Principal, partyType string, partyID int64,
) {
	t.Helper()
	pos, err := svc.CustodyPosition(context.Background(), p, partyType, partyID)
	if err != nil {
		t.Fatalf("custody position: %v", err)
	}
	if !pos.Reconciled {
		t.Fatalf("%s %d holds %d by the obligations but %d by the ledger — the books and operations have diverged",
			partyType, partyID, pos.HeldMinor, pos.LedgerBalanceMinor)
	}
}
