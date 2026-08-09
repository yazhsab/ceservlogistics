package integration

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/commission"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/settlement"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/tests/harness"
)

func settlementServices(env *harness.Env) (*settlement.Service, *commission.Service, *ledger.Service) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := audit.NewRecorder(env.Queries, log)
	led := ledger.NewService(env.DB, env.Queries, rec, log)
	return settlement.NewService(env.DB, env.Queries, led, rec, log, nil),
		commission.NewService(env.DB, env.Queries, led, rec, log, nil),
		led
}

// franchiseFixture creates a franchise owning the origin branch, with an active
// agreement, plus a commission rule that pays a flat amount per delivery.
//
// The franchise owns the *origin* branch, which is fine for tests that call
// commission.Calculate directly with an explicit recipient. It is the wrong
// shape for a test that delivers a parcel end to end: the rule's recipient_role
// is DESTINATION_FRANCHISE, so a delivery only earns when the franchise owns
// the branch that delivered. Use franchiseFixtureAt for that.
func franchiseFixture(t *testing.T, env *harness.Env, tn *harness.Tenant) (int64, string) {
	t.Helper()
	return franchiseFixtureAt(t, env, tn, tn.OriginBranchID)
}

// franchiseFixtureAt is franchiseFixture with the operating unit named.
func franchiseFixtureAt(
	t *testing.T, env *harness.Env, tn *harness.Tenant, unitID int64,
) (int64, string) {
	t.Helper()
	ctx := context.Background()

	var franchiseID int64
	var franchisePubID string
	err := env.DB.Pool.QueryRow(ctx, `
		INSERT INTO franchises (public_id, organization_id, code, name, operating_unit_id,
			category, owner_name, owner_phone, status, onboarded_at)
		VALUES (gen_seed_public_id('frn'), $1, 'FRN-'||upper(substr(md5(random()::text),1,6)),
			'Test Franchise', $2, 'GOLD', 'Owner', '9876500000', 'ACTIVE', now())
		RETURNING id, public_id`, tn.OrgID, unitID).Scan(&franchiseID, &franchisePubID)
	if err != nil {
		t.Fatalf("create franchise: %v", err)
	}

	if _, err := env.DB.Pool.Exec(ctx, `
		INSERT INTO franchise_agreements (public_id, organization_id, franchise_id,
			agreement_number, status, effective_from, currency, settlement_cycle,
			commission_plan_code)
		VALUES (gen_seed_public_id('agr'), $1, $2, 'AGR-'||upper(substr(md5(random()::text),1,6)),
			'ACTIVE', current_date - 365, 'NGN', 'MONTHLY', 'STANDARD')`,
		tn.OrgID, franchiseID); err != nil {
		t.Fatalf("create agreement: %v", err)
	}

	// A flat ₦42.50 delivery commission, so the arithmetic in the assertions is
	// obvious rather than derived.
	var schemeID int64
	mustQueryRow(t, env, `SELECT id FROM commission_schemes WHERE organization_id=$1 AND code='STANDARD'`,
		[]any{tn.OrgID}, &schemeID)

	var ruleID int64
	err = env.DB.Pool.QueryRow(ctx, `
		INSERT INTO commission_rules (public_id, organization_id, scheme_id, code, name,
			commission_type, recipient_role, franchise_id, status)
		VALUES (gen_seed_public_id('crl'), $1, $2, 'DEL-'||upper(substr(md5(random()::text),1,6)),
			'Delivery commission', 'DELIVERY', 'DESTINATION_FRANCHISE', $3, 'ACTIVE')
		RETURNING id`, tn.OrgID, schemeID, franchiseID).Scan(&ruleID)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := env.DB.Pool.Exec(ctx, `
		INSERT INTO commission_rule_versions (public_id, organization_id, rule_id, version_no,
			calculation_method, currency, fixed_amount_minor, effective_from, status)
		VALUES (gen_seed_public_id('crv'), $1, $2, 1, 'FIXED', 'NGN', 4250,
			current_date - 365, 'ACTIVE')`, tn.OrgID, ruleID); err != nil {
		t.Fatalf("create rule version: %v", err)
	}

	return franchiseID, franchisePubID
}

// earnCommission books a delivery commission for the franchise.
func earnCommission(
	t *testing.T, env *harness.Env, p *tenant.Principal, tn *harness.Tenant,
	comm *commission.Service, franchiseID int64, at time.Time,
) int64 {
	t.Helper()
	ctx := context.Background()

	var shipmentID int64
	err := env.DB.Pool.QueryRow(ctx, `
		INSERT INTO shipments (public_id, organization_id, awb, customer_id, booking_unit_id,
			courier_service_id, payment_mode, current_status, movement_direction,
			origin_pincode, destination_pincode, piece_count, actual_weight_grams,
			chargeable_weight_grams, currency, total_amount_minor, content_description,
			origin_branch_id, destination_branch_id, booked_at)
		SELECT gen_seed_public_id('shp'), $1,
			'STL' || lpad((nextval(pg_get_serial_sequence('shipments','id')))::text, 12, '0'),
			c.id, $2, $3, 'PREPAID', 'DELIVERED', 'FORWARD', '100001', '900001', 1,
			500, 500, 'NGN', 50000, 'settlement test', $2, $4, $5
		FROM customers c WHERE c.organization_id = $1 LIMIT 1
		RETURNING id`,
		tn.OrgID, tn.OriginBranchID, tn.ServiceID, tn.DestBranchID, at).Scan(&shipmentID)
	if err != nil {
		t.Fatalf("seed shipment: %v", err)
	}

	var calc *dbgen.CommissionCalculation
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var cErr error
		calc, _, cErr = comm.Calculate(ctx, tx, p, commission.CalculateRequest{
			Facts: commission.Facts{
				CommissionType: commission.TypeDelivery,
				FranchiseID:    &franchiseID,
			},
			Basis:           commission.Basis{FreightMinor: 50000, TotalMinor: 50000, ShipmentCount: 1},
			ShipmentID:      &shipmentID,
			QualifyingEvent: "DELIVERED",
			QualifiedAt:     at,
			RecipientType:   "FRANCHISE",
			RecipientID:     franchiseID,
			FranchiseID:     &franchiseID,
			PostImmediately: true,
		})
		return cErr
	}); err != nil {
		t.Fatalf("calculate commission: %v", err)
	}
	return calc.AmountMinor
}

func monthWindow() (time.Time, time.Time) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// TestSettlementIsReproducible is the property §26 demands: recalculating over
// unchanged sources produces identical lines and an identical net.
func TestSettlementIsReproducible(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()

	var expected int64
	for i := 0; i < 3; i++ {
		expected += earnCommission(t, env, p, tn, comm, franchiseID, start.AddDate(0, 0, i))
	}

	first, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if first.Settlement.NetAmountMinor != expected {
		t.Fatalf("net = %d, want %d (3 × ₹42.50)", first.Settlement.NetAmountMinor, expected)
	}
	if len(first.Lines) != 3 {
		t.Fatalf("%d lines, want 3", len(first.Lines))
	}

	// Every line must name its source, or the statement cannot be audited.
	for _, l := range first.Lines {
		if l.SourceID == nil || l.SourcePublicID == nil {
			t.Fatalf("line %d (%s) has no source reference", l.LineNo, l.Category)
		}
	}

	second, err := stl.Recalculate(ctx, p, first.Settlement.PublicID)
	if err != nil {
		t.Fatalf("recalculate: %v", err)
	}

	if second.Settlement.NetAmountMinor != first.Settlement.NetAmountMinor {
		t.Fatalf("recalculation changed the net: %d then %d",
			first.Settlement.NetAmountMinor, second.Settlement.NetAmountMinor)
	}
	if first.Settlement.CalculationHash == nil || second.Settlement.CalculationHash == nil {
		t.Fatal("a calculated settlement must carry a calculation hash")
	}
	if *first.Settlement.CalculationHash != *second.Settlement.CalculationHash {
		t.Fatalf("recalculation produced a different hash: %s then %s",
			*first.Settlement.CalculationHash, *second.Settlement.CalculationHash)
	}
	if len(second.Lines) != len(first.Lines) {
		t.Fatalf("recalculation produced %d lines, first run produced %d",
			len(second.Lines), len(first.Lines))
	}
	for i := range first.Lines {
		if first.Lines[i].AmountMinor != second.Lines[i].AmountMinor ||
			first.Lines[i].Category != second.Lines[i].Category {
			t.Fatalf("line %d differs between runs: %s/%d vs %s/%d", i+1,
				first.Lines[i].Category, first.Lines[i].AmountMinor,
				second.Lines[i].Category, second.Lines[i].AmountMinor)
		}
	}
}

// TestSettlementGenerationIsIdempotent proves a retried generation does not
// produce a second statement for the same period.
func TestSettlementGenerationIsIdempotent(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)

	req := settlement.GenerateRequest{FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end}

	first, err := stl.Generate(ctx, p, req)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	second, err := stl.Generate(ctx, p, req)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Settlement.ID != first.Settlement.ID {
		t.Fatalf("the retry created settlement %d, want the original %d",
			second.Settlement.ID, first.Settlement.ID)
	}

	var count int64
	mustQueryRow(t, env, `
		SELECT count(*) FROM settlements
		WHERE organization_id=$1 AND franchise_id=$2 AND status <> 'CANCELLED'`,
		[]any{p.OrganizationID, franchiseID}, &count)
	if count != 1 {
		t.Fatalf("%d settlements for one franchise-period, want 1", count)
	}
}

// TestConcurrentSettlementGenerationProducesOne is the adversarial version:
// eight simultaneous generation requests for the same franchise and period.
func TestConcurrentSettlementGenerationProducesOne(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)

	const attempts = 8
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			_, _ = stl.Generate(ctx, p, settlement.GenerateRequest{
				FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
			})
		}()
	}
	wg.Wait()

	var count int64
	mustQueryRow(t, env, `
		SELECT count(*) FROM settlements
		WHERE organization_id=$1 AND franchise_id=$2 AND status <> 'CANCELLED'`,
		[]any{p.OrganizationID, franchiseID}, &count)
	if count != 1 {
		t.Fatalf("%d settlements after %d concurrent generations, want exactly 1", count, attempts)
	}

	// And the commission was not double-counted onto duplicate lines.
	var lineCount int64
	mustQueryRow(t, env, `
		SELECT count(*) FROM settlement_lines l
		JOIN settlements s ON s.id = l.settlement_id
		WHERE s.organization_id=$1 AND s.franchise_id=$2`,
		[]any{p.OrganizationID, franchiseID}, &lineCount)
	if lineCount != 1 {
		t.Fatalf("%d settlement lines after concurrent generation, want 1", lineCount)
	}
}

// TestApprovedSettlementCannotSilentlyRecalculate is the §26 rule that stops a
// statement changing after both sides agreed it.
func TestApprovedSettlementCannotSilentlyRecalculate(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, led := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earned := earnCommission(t, env, p, tn, comm, franchiseID, start)

	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Maker/checker: the calculator cannot approve.
	if _, err := stl.Approve(ctx, p, res.Settlement.PublicID, "looks fine"); err == nil {
		t.Fatal("the person who calculated a settlement approved it themselves")
	}

	checkerID, _, _ := env.NewUser(t, tn.OrgID,
		strings.ToLower("stlchk-"+harness.RandomKey()[:8])+"@test.local", "FINANCE_MANAGER", nil)
	checker := &tenant.Principal{
		UserID: checkerID, OrganizationID: tn.OrgID, OrganizationCurrency: "NGN",
	}
	approved, err := stl.Approve(ctx, checker, res.Settlement.PublicID, "reviewed and agreed")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != settlement.StatusApproved {
		t.Fatalf("status = %s, want APPROVED", approved.Status)
	}

	// The franchise is now owed the net.
	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, earned,
		"the franchise should be owed the approved net")

	// Recalculation is refused.
	if _, err := stl.Recalculate(ctx, p, res.Settlement.PublicID); err == nil {
		t.Fatal("an approved settlement was recalculated")
	}

	// And raw SQL cannot change the numbers either.
	_, rawErr := env.DB.Pool.Exec(ctx,
		`UPDATE settlements SET net_amount_minor = 1 WHERE id = $1`, res.Settlement.ID)
	if rawErr == nil {
		t.Fatal("an approved settlement's net was edited directly")
	}
	if !containsAny(rawErr.Error(), "SETTLEMENT_IMMUTABLE", "adjustment") {
		t.Fatalf("expected the settlement guard, got: %v", rawErr)
	}

	// Its lines are frozen too.
	_, rawErr = env.DB.Pool.Exec(ctx,
		`DELETE FROM settlement_lines WHERE settlement_id = $1`, res.Settlement.ID)
	if rawErr == nil {
		t.Fatal("an approved settlement's lines were deleted")
	}
}

// TestSettlementPaymentTracksProgress walks approval → part payment → full
// payment and checks the ledger and status at each step.
func TestSettlementPaymentTracksProgress(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, led := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	var total int64
	for i := 0; i < 4; i++ {
		total += earnCommission(t, env, p, tn, comm, franchiseID, start.AddDate(0, 0, i))
	}

	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	checkerID, _, _ := env.NewUser(t, tn.OrgID,
		strings.ToLower("paychk-"+harness.RandomKey()[:8])+"@test.local", "FINANCE_MANAGER", nil)
	checker := &tenant.Principal{UserID: checkerID, OrganizationID: tn.OrgID, OrganizationCurrency: "NGN"}
	if _, err := stl.Approve(ctx, checker, res.Settlement.PublicID, "agreed"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Part payment.
	half := total / 2
	_, updated, err := stl.Pay(ctx, p, settlement.PayRequest{
		SettlementID: res.Settlement.PublicID, AmountMinor: half,
		PaymentMode: "BANK_TRANSFER", Reference: "NEFT-1", PaidOn: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("part payment: %v", err)
	}
	if updated.Status != settlement.StatusPartiallyPaid {
		t.Fatalf("after a part payment status = %s, want PARTIALLY_PAID", updated.Status)
	}
	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, total-half,
		"the franchise should still be owed the unpaid half")

	// Overpaying the remainder is refused.
	if _, _, err := stl.Pay(ctx, p, settlement.PayRequest{
		SettlementID: res.Settlement.PublicID, AmountMinor: total,
		PaymentMode: "BANK_TRANSFER", PaidOn: time.Now().UTC(),
	}); err == nil {
		t.Fatal("a settlement was overpaid")
	}

	// Settle the rest.
	_, final, err := stl.Pay(ctx, p, settlement.PayRequest{
		SettlementID: res.Settlement.PublicID, AmountMinor: total - half,
		PaymentMode: "BANK_TRANSFER", Reference: "NEFT-2", PaidOn: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("final payment: %v", err)
	}
	if final.Status != settlement.StatusPaid {
		t.Fatalf("after full payment status = %s, want PAID", final.Status)
	}
	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, 0,
		"the franchise should be square once fully paid")

	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after settlement payment: %v", err)
	}
}

// TestCommissionIsSweptOnceAcrossPeriods proves a commission cannot appear on
// two statements — the failure that would pay a franchise twice.
func TestCommissionIsSweptOnceAcrossPeriods(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)

	first, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("first period: %v", err)
	}
	if len(first.Lines) != 1 {
		t.Fatalf("first settlement has %d lines, want 1", len(first.Lines))
	}

	// A second, later period must not pick the same commission up again.
	nextStart := end.AddDate(0, 0, 1)
	nextEnd := nextStart.AddDate(0, 1, -1)
	second, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: nextStart, PeriodEnd: nextEnd,
	})
	if err != nil {
		t.Fatalf("second period: %v", err)
	}
	for _, l := range second.Lines {
		if l.SourceType == settlement.SrcCommission {
			t.Fatalf("commission %v was swept onto a second settlement", l.SourcePublicID)
		}
	}
}

// TestCancelledSettlementReleasesItsCommission proves commission is not
// stranded when a statement is voided.
func TestCancelledSettlementReleasesItsCommission(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earned := earnCommission(t, env, p, tn, comm, franchiseID, start)

	first, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := stl.Cancel(ctx, p, first.Settlement.PublicID,
		"generated for the wrong period by mistake"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// The commission is free again, so a fresh statement picks it up.
	again, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("regenerate after cancel: %v", err)
	}
	if again.Settlement.ID == first.Settlement.ID {
		t.Fatal("the cancelled settlement was reused")
	}
	if again.Settlement.NetAmountMinor != earned {
		t.Fatalf("regenerated net = %d, want %d — the commission was stranded",
			again.Settlement.NetAmountMinor, earned)
	}
}

// TestSettlementIsTenantIsolated proves one tenant cannot settle another's
// franchise.
func TestSettlementIsTenantIsolated(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	a := env.NewTenant(t, geo, harness.TenantOptions{})
	b := env.NewTenant(t, geo, harness.TenantOptions{})

	pa := &tenant.Principal{UserID: a.AdminUserID, OrganizationID: a.OrgID, OrganizationCurrency: "NGN"}
	pb := &tenant.Principal{UserID: b.AdminUserID, OrganizationID: b.OrgID, OrganizationCurrency: "NGN"}
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, a)
	start, end := monthWindow()
	earnCommission(t, env, pa, a, comm, franchiseID, start)

	// B cannot generate against A's franchise.
	if _, err := stl.Generate(ctx, pb, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	}); err == nil {
		t.Fatal("tenant B generated a settlement for tenant A's franchise")
	}

	res, err := stl.Generate(ctx, pa, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("tenant A generate: %v", err)
	}
	// B cannot read or approve it.
	if _, err := stl.Approve(ctx, pb, res.Settlement.PublicID, "not mine"); err == nil {
		t.Fatal("tenant B approved tenant A's settlement")
	}
}

// ---------------------------------------------------------------------------
// Settlement adjustments — the correction path
// ---------------------------------------------------------------------------

// The rule these pin down is §26: an approved settlement cannot silently
// recalculate, so a correction has to be an explicit, approved, audited
// adjustment. Until this workflow existed the service told an operator to
// "raise an adjustment instead" and there was no way to do so.

// newChecker returns a second finance principal, for the maker/checker half.
func newChecker(t *testing.T, env *harness.Env, tn *harness.Tenant) *tenant.Principal {
	t.Helper()
	id, _, _ := env.NewUser(t, tn.OrgID,
		strings.ToLower("adjchk-"+harness.RandomKey()[:8])+"@test.local",
		"FINANCE_MANAGER", nil)
	return &tenant.Principal{
		UserID: id, OrganizationID: tn.OrgID, OrganizationCurrency: "NGN",
	}
}

func TestAdjustmentOnAFrozenSettlementPostsItsOwnJournal(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, led := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earned := earnCommission(t, env, p, tn, comm, franchiseID, start)

	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	checker := newChecker(t, env, tn)
	if _, err := stl.Approve(ctx, checker, res.Settlement.PublicID, "agreed"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, earned,
		"the franchise should be owed the approved net")

	// The statement is frozen and cannot be recalculated. The correction path
	// is an adjustment.
	const correction int64 = 25_000
	adj, err := stl.RaiseAdjustment(ctx, p, settlement.AdjustmentRequest{
		SettlementID: res.Settlement.PublicID, Type: "CORRECTION",
		AmountMinor: correction, Reason: "under-counted three delivery commissions",
	})
	if err != nil {
		t.Fatalf("raise adjustment: %v", err)
	}
	if adj.Status != settlement.AdjPending {
		t.Fatalf("a fresh adjustment is %s, want PENDING_APPROVAL", adj.Status)
	}

	// Raising moves no money. That is the whole point of the split.
	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, earned,
		"raising an adjustment must not move money")

	approved, err := stl.ApproveAdjustment(ctx, checker, adj.PublicID, "verified against the scans")
	if err != nil {
		t.Fatalf("approve adjustment: %v", err)
	}
	// Against a frozen statement the correction posts immediately, so it is
	// APPLIED rather than merely APPROVED.
	if approved.Status != settlement.AdjApplied {
		t.Fatalf("status = %s, want APPLIED against a frozen settlement", approved.Status)
	}
	if approved.JournalTransactionID == nil {
		t.Fatal("an applied adjustment carries no journal reference")
	}

	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, earned+correction,
		"the approved adjustment should have moved the franchise balance")

	// The ledger still balances after the correction.
	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("the ledger is unbalanced after an adjustment: %v", err)
	}

	// And the settlement itself was not edited — the correction sits beside it.
	var net int64
	if err := env.DB.Pool.QueryRow(ctx,
		`SELECT net_amount_minor FROM settlements WHERE id = $1`,
		res.Settlement.ID).Scan(&net); err != nil {
		t.Fatal(err)
	}
	if net != earned {
		t.Fatalf("the frozen settlement's net changed to %d; history must not be edited", net)
	}
}

func TestAdjustmentOnAnOpenSettlementDoesNotPostTwice(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, led := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earned := earnCommission(t, env, p, tn, comm, franchiseID, start)

	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The statement is still CALCULATED, so nothing has posted. An approved
	// adjustment here must be folded into the recalculated net and must NOT
	// post a journal of its own — doing both would count it twice.
	//
	// The penalty is kept small enough that the net stays positive. That is
	// deliberate: PartyBalance normalises by each account's normal balance, so a
	// negative net posts to the franchise *receivable* and reads back positive.
	// Straddling that boundary would make this test about the sign convention
	// rather than about double-counting.
	penalty := -earned / 4
	adj, err := stl.RaiseAdjustment(ctx, p, settlement.AdjustmentRequest{
		SettlementID: res.Settlement.PublicID, Type: "PENALTY",
		AmountMinor: penalty, Reason: "late remittance penalty for the period",
	})
	if err != nil {
		t.Fatalf("raise adjustment: %v", err)
	}
	checker := newChecker(t, env, tn)
	approved, err := stl.ApproveAdjustment(ctx, checker, adj.PublicID, "confirmed")
	if err != nil {
		t.Fatalf("approve adjustment: %v", err)
	}
	if approved.Status != settlement.AdjApproved {
		t.Fatalf("status = %s, want APPROVED (not APPLIED) on an open settlement",
			approved.Status)
	}
	if approved.JournalTransactionID != nil {
		t.Fatal("an adjustment on an open settlement posted its own journal; " +
			"the settlement's approval will post the net and this double-counts")
	}

	// Recalculating picks it up as a line.
	recalculated, err := stl.Recalculate(ctx, p, res.Settlement.PublicID)
	if err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	if recalculated.Settlement.NetAmountMinor != earned+penalty {
		t.Fatalf("net = %d, want %d (earned %d + penalty %d)",
			recalculated.Settlement.NetAmountMinor, earned+penalty, earned, penalty)
	}
	if earned+penalty <= 0 {
		t.Fatalf("the fixture produced a non-positive net (%d); see the note above",
			earned+penalty)
	}

	// Approving the settlement posts the net *once*, including the penalty.
	if _, err := stl.Approve(ctx, checker, res.Settlement.PublicID, "agreed"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	assertPartyBalance(t, led, p, ledger.PartyFranchise, franchiseID, earned+penalty,
		"the penalty should be counted exactly once")
	if err := led.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("the ledger is unbalanced: %v", err)
	}
}

func TestAdjustmentNeedsTwoPeople(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)
	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	adj, err := stl.RaiseAdjustment(ctx, p, settlement.AdjustmentRequest{
		SettlementID: res.Settlement.PublicID, Type: "GOODWILL",
		AmountMinor: 5_000, Reason: "goodwill credit agreed with the franchise",
	})
	if err != nil {
		t.Fatalf("raise adjustment: %v", err)
	}

	// The requester cannot approve their own correction.
	if _, err := stl.ApproveAdjustment(ctx, p, adj.PublicID, "self-approving"); err == nil {
		t.Fatal("the person who raised an adjustment approved it themselves")
	}
	// Nor quietly withdraw it by rejecting it, which leaves the same hole.
	if _, err := stl.RejectAdjustment(ctx, p, adj.PublicID, "changed my mind"); err == nil {
		t.Fatal("the requester rejected their own adjustment")
	}

	// The database refuses it too, so a code path that forgot the check could
	// not do it either.
	_, rawErr := env.DB.Pool.Exec(ctx,
		`UPDATE settlement_adjustments SET status='APPROVED', approved_by=requested_by,
		        approved_at=now() WHERE id = $1`, adj.ID)
	if rawErr == nil {
		t.Fatal("the maker/checker CHECK constraint did not fire")
	}

	// A second person can.
	checker := newChecker(t, env, tn)
	approved, err := stl.ApproveAdjustment(ctx, checker, adj.PublicID, "reviewed")
	if err != nil {
		t.Fatalf("approve by a second person: %v", err)
	}
	if approved.ApprovedBy == nil || *approved.ApprovedBy == p.UserID {
		t.Fatal("the approver was not recorded, or is the requester")
	}
}

func TestAdjustmentDecisionIsFinalAndAudited(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)
	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	checker := newChecker(t, env, tn)

	adj, err := stl.RaiseAdjustment(ctx, p, settlement.AdjustmentRequest{
		SettlementID: res.Settlement.PublicID, Type: "RECOVERY",
		AmountMinor: -7_500, Reason: "recovering an overpayment from the prior period",
	})
	if err != nil {
		t.Fatalf("raise adjustment: %v", err)
	}

	rejected, err := stl.RejectAdjustment(ctx, checker, adj.PublicID,
		"the prior period was already corrected")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != settlement.AdjRejected {
		t.Fatalf("status = %s, want REJECTED", rejected.Status)
	}
	if rejected.RejectionReason == nil || *rejected.RejectionReason == "" {
		t.Fatal("a rejection carries no reason; 'why was this refused' has no answer")
	}

	// A decided adjustment cannot be decided again.
	if _, err := stl.ApproveAdjustment(ctx, checker, adj.PublicID, "actually yes"); err == nil {
		t.Fatal("a rejected adjustment was then approved")
	}

	// Both halves of the workflow are on the audit trail with their reasons.
	for _, action := range []string{
		"settlement.adjustment.raised", "settlement.adjustment.rejected",
	} {
		var n int
		if err := env.DB.Pool.QueryRow(ctx,
			`SELECT count(*) FROM audit_events
			  WHERE action = $1 AND resource_public_id = $2
			    AND reason IS NOT NULL AND length(reason) > 0`,
			action, adj.PublicID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s audit records = %d, want 1 with a reason", action, n)
		}
	}
}

func TestAdjustmentValidation(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)
	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	id := res.Settlement.PublicID

	for _, tc := range []struct {
		name string
		in   settlement.AdjustmentRequest
	}{
		{"unknown type", settlement.AdjustmentRequest{
			SettlementID: id, Type: "NOT_A_TYPE", AmountMinor: 100,
			Reason: "a perfectly good reason here"}},
		{"zero amount", settlement.AdjustmentRequest{
			SettlementID: id, Type: "CORRECTION", AmountMinor: 0,
			Reason: "a perfectly good reason here"}},
		{"reason too short", settlement.AdjustmentRequest{
			SettlementID: id, Type: "CORRECTION", AmountMinor: 100, Reason: "oops"}},
		{"no such settlement", settlement.AdjustmentRequest{
			SettlementID: "stl_00000000000000000000000000", Type: "CORRECTION",
			AmountMinor: 100, Reason: "a perfectly good reason here"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := stl.RaiseAdjustment(ctx, p, tc.in); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

func TestAdjustmentsAreTenantScoped(t *testing.T) {
	env, tn, p := financeEnv(t)
	stl, comm, _ := settlementServices(env)
	ctx := context.Background()

	franchiseID, franchisePubID := franchiseFixture(t, env, tn)
	start, end := monthWindow()
	earnCommission(t, env, p, tn, comm, franchiseID, start)
	res, err := stl.Generate(ctx, p, settlement.GenerateRequest{
		FranchiseID: franchisePubID, PeriodStart: start, PeriodEnd: end,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	adj, err := stl.RaiseAdjustment(ctx, p, settlement.AdjustmentRequest{
		SettlementID: res.Settlement.PublicID, Type: "CORRECTION",
		AmountMinor: 1_000, Reason: "a correction for the tenant isolation test",
	})
	if err != nil {
		t.Fatalf("raise adjustment: %v", err)
	}

	// Another tenant's finance manager can neither read nor decide it.
	other := env.NewTenant(t, env.Geography(t), harness.TenantOptions{})
	intruder := &tenant.Principal{
		UserID: other.AdminUserID, OrganizationID: other.OrgID, OrganizationCurrency: "NGN",
	}
	if _, err := stl.GetAdjustment(ctx, intruder, adj.PublicID); err == nil {
		t.Fatal("another tenant read the adjustment")
	}
	if _, err := stl.ApproveAdjustment(ctx, intruder, adj.PublicID, "approving yours"); err == nil {
		t.Fatal("another tenant approved the adjustment")
	}
	rows, err := stl.ListAdjustments(ctx, intruder, "", "", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("another tenant sees %d adjustments", len(rows))
	}
}
