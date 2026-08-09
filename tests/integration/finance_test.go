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
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/tests/harness"
)

// The tests here exercise the ledger against a real PostgreSQL connection,
// because the guarantees that matter most — the deferred balance constraint,
// the immutability triggers, the idempotency index — live in the database and
// cannot be proven by a unit test.

func financeEnv(t *testing.T) (*harness.Env, *harness.Tenant, *tenant.Principal) {
	t.Helper()
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{})

	p := &tenant.Principal{
		UserID:               tn.AdminUserID,
		OrganizationID:       tn.OrgID,
		OrganizationCurrency: "NGN",
	}
	return env, tn, p
}

func ledgerService(env *harness.Env) *ledger.Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return ledger.NewService(env.DB, env.Queries, audit.NewRecorder(env.Queries, log), log)
}

// TestChartOfAccountsIsProvisioned proves a new tenant can post on day one.
// The gap this guards against is real: the migration seeds accounts for
// organizations that existed when it ran, and a tenant created afterwards
// would otherwise have none.
func TestChartOfAccountsIsProvisioned(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)

	accounts, total, err := svc.ListAccounts(context.Background(), p, "", "", "", nil, 100, 0)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if total < 20 {
		t.Fatalf("a new tenant has %d ledger accounts, want the full default chart", total)
	}

	// The codes the Go constants refer to must exist, or every posting fails.
	required := []string{
		ledger.AcctCash, ledger.AcctCommissionExpense, ledger.AcctFranchisePayable,
		ledger.AcctCODPayableConsignor, ledger.AcctTradeReceivable, ledger.AcctFreightRevenue,
		ledger.AcctCODReceivableAgent, ledger.AcctTaxPayable,
	}
	present := map[string]bool{}
	for _, a := range accounts {
		present[a.Code] = true
	}
	for _, code := range required {
		if !present[code] {
			t.Errorf("account %s is referenced from Go but missing from the chart", code)
		}
	}

	// And an open period, or nothing can be posted.
	periods, err := svc.ListPeriods(context.Background(), p, 10)
	if err != nil {
		t.Fatalf("list periods: %v", err)
	}
	if len(periods) == 0 {
		t.Fatal("a new tenant has no accounting period, so it cannot post")
	}
}

// TestPostingBalancesAndIsReadableBack is the happy path: a two-legged
// transaction posts, the accounts move by the right amounts in the right
// directions, and the trial balance agrees with itself.
func TestPostingBalancesAndIsReadableBack(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	var result *ledger.Result
	err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var pErr error
		result, pErr = svc.Post(ctx, tx, p, ledger.Posting{
			SourceType: ledger.SourceManual, SourceID: ptrInt64(1),
			Purpose: "TEST_SALE", Description: "Freight billed to a customer",
			Reason: "integration test", Currency: "NGN",
			PostingDate: time.Now().UTC(),
			Legs: []ledger.Leg{
				{AccountCode: ledger.AcctTradeReceivable, Debit: 67850},
				{AccountCode: ledger.AcctFreightRevenue, Credit: 57500},
				{AccountCode: ledger.AcctTaxPayable, Credit: 10350},
			},
		})
		return pErr
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if result.Transaction.Status != ledger.StatusPosted {
		t.Fatalf("status = %s, want POSTED", result.Transaction.Status)
	}
	if result.Transaction.TotalMinor != 67850 {
		t.Fatalf("total = %d, want 67850", result.Transaction.TotalMinor)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(result.Entries))
	}

	// A receivable is an ASSET: a debit increases it, so its balance is positive.
	assertBalance(t, svc, p, ledger.AcctTradeReceivable, 67850)
	// Revenue is CREDIT-normal: a credit increases it, so it too reports positive.
	assertBalance(t, svc, p, ledger.AcctFreightRevenue, 57500)
	assertBalance(t, svc, p, ledger.AcctTaxPayable, 10350)

	tb, err := svc.TrialBalance(ctx, p, nil, false)
	if err != nil {
		t.Fatalf("trial balance: %v", err)
	}
	if !tb.Balanced {
		t.Fatalf("trial balance does not balance: debits %d, credits %d",
			tb.TotalDebit, tb.TotalCredit)
	}
	if tb.TotalDebit != 67850 {
		t.Fatalf("trial balance debits = %d, want 67850", tb.TotalDebit)
	}

	if err := svc.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("whole-ledger invariant failed: %v", err)
	}
}

// TestUnbalancedPostingIsRefusedByTheDatabase bypasses the Go check entirely
// and writes the legs by hand, proving the deferred constraint is the real
// guarantee rather than the service's arithmetic.
func TestUnbalancedPostingIsRefusedByTheDatabase(t *testing.T) {
	env, tn, _ := financeEnv(t)
	ctx := context.Background()

	var periodID, cashID, revenueID int64
	mustQueryRow(t, env, `SELECT id FROM accounting_periods WHERE organization_id=$1 LIMIT 1`,
		[]any{tn.OrgID}, &periodID)
	mustQueryRow(t, env, `SELECT id FROM ledger_accounts WHERE organization_id=$1 AND code='1000'`,
		[]any{tn.OrgID}, &cashID)
	mustQueryRow(t, env, `SELECT id FROM ledger_accounts WHERE organization_id=$1 AND code='4000'`,
		[]any{tn.OrgID}, &revenueID)

	err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var txnID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO journal_transactions (public_id, organization_id, transaction_number,
				period_id, posting_date, status, currency, source_type, purpose, description,
				reason, total_minor, entry_count, posted_at)
			VALUES (gen_seed_public_id('jrn'), $1, 'RAW-1', $2, current_date, 'POSTED', 'NGN',
				'MANUAL', 'raw', 'hand-written', 'test', 10000, 2, now())
			RETURNING id`, tn.OrgID, periodID).Scan(&txnID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO journal_entries (public_id, organization_id, transaction_id, account_id,
				line_no, debit_minor, credit_minor, currency)
			VALUES (gen_seed_public_id('jen'), $1, $2, $3, 1, 10000, 0, 'NGN')`,
			tn.OrgID, txnID, cashID); err != nil {
			return err
		}
		// Deliberately short by 4000.
		_, err := tx.Exec(ctx, `
			INSERT INTO journal_entries (public_id, organization_id, transaction_id, account_id,
				line_no, debit_minor, credit_minor, currency)
			VALUES (gen_seed_public_id('jen'), $1, $2, $3, 2, 0, 6000, 'NGN')`,
			tn.OrgID, txnID, revenueID)
		return err
	})

	if err == nil {
		t.Fatal("the database accepted an unbalanced transaction written directly in SQL")
	}
	if !containsAny(err.Error(), "LEDGER_UNBALANCED", "does not balance") {
		t.Fatalf("expected the balance constraint to fire, got: %v", err)
	}
}

// TestPostedJournalCannotBeEditedOrDeleted proves immutability against raw SQL,
// which is what "posted journals are immutable" has to mean to be worth
// anything.
func TestPostedJournalCannotBeEditedOrDeleted(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	var result *ledger.Result
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var pErr error
		result, pErr = svc.Post(ctx, tx, p, simplePosting(2, "IMMUTABLE_TEST"))
		return pErr
	}); err != nil {
		t.Fatalf("post: %v", err)
	}
	txnID := result.Transaction.ID

	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{
		{"edit the total", `UPDATE journal_transactions SET total_minor=999 WHERE id=$1`, []any{txnID}},
		{"edit the description", `UPDATE journal_transactions SET description='changed' WHERE id=$1`, []any{txnID}},
		{"delete the transaction", `DELETE FROM journal_transactions WHERE id=$1`, []any{txnID}},
		{"edit an entry", `UPDATE journal_entries SET debit_minor=1 WHERE transaction_id=$1`, []any{txnID}},
		{"delete an entry", `DELETE FROM journal_entries WHERE transaction_id=$1`, []any{txnID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.DB.Pool.Exec(ctx, tc.sql, tc.args...)
			if err == nil {
				t.Fatalf("%s succeeded against a posted journal", tc.name)
			}
			if !containsAny(err.Error(), "JOURNAL_IMMUTABLE", "append-only") {
				t.Fatalf("expected the immutability guard, got: %v", err)
			}
		})
	}
}

// TestReversalLeavesTheOriginalIntactAndNetsToZero is the correction workflow:
// the mistake stays visible, the mirror cancels it, and the account returns to
// where it started.
func TestReversalLeavesTheOriginalIntactAndNetsToZero(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	var original *ledger.Result
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var pErr error
		original, pErr = svc.Post(ctx, tx, p, simplePosting(3, "REVERSAL_TEST"))
		return pErr
	}); err != nil {
		t.Fatalf("post: %v", err)
	}
	assertBalance(t, svc, p, ledger.AcctCash, 50000)

	var reversal *ledger.Result
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var rErr error
		reversal, rErr = svc.Reverse(ctx, tx, p, original.Transaction.ID,
			"posted against the wrong customer", time.Now().UTC())
		return rErr
	}); err != nil {
		t.Fatalf("reverse: %v", err)
	}

	if reversal.Transaction.ReversesID == nil || *reversal.Transaction.ReversesID != original.Transaction.ID {
		t.Fatal("the reversal does not point at the transaction it reverses")
	}

	// The original is still there, still POSTED-shaped, now marked REVERSED.
	var status string
	mustQueryRow(t, env, `SELECT status FROM journal_transactions WHERE id=$1`,
		[]any{original.Transaction.ID}, &status)
	if status != ledger.StatusReversed {
		t.Fatalf("original status = %s, want REVERSED", status)
	}
	var entryCount int64
	mustQueryRow(t, env, `SELECT count(*) FROM journal_entries WHERE transaction_id=$1`,
		[]any{original.Transaction.ID}, &entryCount)
	if entryCount != 2 {
		t.Fatalf("the original's entries were removed: %d remain, want 2", entryCount)
	}

	// A REVERSED transaction is excluded from balances, and its mirror cancels
	// what it did, so the account is back where it started.
	assertBalance(t, svc, p, ledger.AcctCash, 0)

	if err := svc.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after reversal: %v", err)
	}

	// Reversing twice is refused rather than double-crediting.
	err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		_, e := svc.Reverse(ctx, tx, p, original.Transaction.ID, "second attempt at reversal", time.Now().UTC())
		return e
	})
	if err == nil {
		t.Fatal("a journal was reversed twice")
	}
}

// TestPostingIsIdempotentOnItsNaturalKey proves the finance idempotency rule:
// the same source posted twice moves money once.
func TestPostingIsIdempotentOnItsNaturalKey(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	post := func() (*ledger.Result, error) {
		var r *ledger.Result
		err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
			var e error
			r, e = svc.Post(ctx, tx, p, ledger.Posting{
				SourceType: ledger.SourceCommission, SourceID: ptrInt64(4242),
				Purpose: "DELIVERY", Description: "Delivery commission",
				Currency: "NGN", PostingDate: time.Now().UTC(),
				Legs: []ledger.Leg{
					{AccountCode: ledger.AcctCommissionExpense, Debit: 4250},
					{AccountCode: ledger.AcctFranchisePayable, Credit: 4250,
						PartyType: ledger.PartyFranchise, PartyID: 7},
				},
			})
			return e
		})
		return r, err
	}

	first, err := post()
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	if first.Replayed {
		t.Fatal("the first post was reported as a replay")
	}

	second, err := post()
	if err != nil {
		t.Fatalf("second post: %v", err)
	}
	if !second.Replayed {
		t.Fatal("the second post was not recognised as a duplicate")
	}
	if second.Transaction.ID != first.Transaction.ID {
		t.Fatalf("the retry created transaction %d, want the original %d",
			second.Transaction.ID, first.Transaction.ID)
	}

	// The franchise is owed 4250 once, not twice.
	balance, err := svc.PartyBalance(ctx, p, ledger.PartyFranchise, 7, nil)
	if err != nil {
		t.Fatalf("party balance: %v", err)
	}
	if balance != 4250 {
		t.Fatalf("franchise balance = %d, want 4250 — the commission was paid more than once", balance)
	}
}

// TestConcurrentIdenticalPostingsPayOnce is the adversarial version: twelve
// simultaneous retries of one commission, as a flaky mobile network would
// produce.
func TestConcurrentIdenticalPostingsPayOnce(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	const attempts = 12
	var wg sync.WaitGroup
	results := make([]error, attempts)

	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = env.DB.InTx(ctx, func(tx pgx.Tx) error {
				_, e := svc.Post(ctx, tx, p, ledger.Posting{
					SourceType: ledger.SourceCommission, SourceID: ptrInt64(9999),
					Purpose: "BOOKING", Description: "Booking commission",
					Currency: "NGN", PostingDate: time.Now().UTC(),
					Legs: []ledger.Leg{
						{AccountCode: ledger.AcctCommissionExpense, Debit: 1500},
						{AccountCode: ledger.AcctFranchisePayable, Credit: 1500,
							PartyType: ledger.PartyFranchise, PartyID: 11},
					},
				})
				return e
			})
		}(i)
	}
	wg.Wait()

	// Some attempts may fail on serialisation; what must not happen is two
	// successful transactions for the same source.
	var rows int64
	mustQueryRow(t, env, `
		SELECT count(*) FROM journal_transactions
		WHERE organization_id=$1 AND source_type='COMMISSION' AND source_id=9999
		  AND purpose='BOOKING' AND status='POSTED'`,
		[]any{p.OrganizationID}, &rows)
	if rows != 1 {
		t.Fatalf("%d posted transactions for one commission; exactly 1 was required", rows)
	}

	balance, err := svc.PartyBalance(ctx, p, ledger.PartyFranchise, 11, nil)
	if err != nil {
		t.Fatalf("party balance: %v", err)
	}
	if balance != 1500 {
		t.Fatalf("franchise balance = %d after %d concurrent retries, want 1500", balance, attempts)
	}

	if err := svc.AssertBalanced(ctx, p); err != nil {
		t.Fatalf("ledger unbalanced after concurrent posting: %v", err)
	}
}

// TestClosedPeriodRefusesPostings proves the period gate.
func TestClosedPeriodRefusesPostings(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	periods, err := svc.ListPeriods(ctx, p, 5)
	if err != nil || len(periods) == 0 {
		t.Fatalf("list periods: %v", err)
	}
	if _, err := svc.ClosePeriod(ctx, p, periods[0].PublicID, "month end"); err != nil {
		t.Fatalf("close period: %v", err)
	}

	err = env.DB.InTx(ctx, func(tx pgx.Tx) error {
		_, e := svc.Post(ctx, tx, p, simplePosting(5, "AFTER_CLOSE"))
		return e
	})
	if err == nil {
		t.Fatal("a posting was accepted into a closed period")
	}
	if !containsAny(err.Error(), "PERIOD_CLOSED", "closed") {
		t.Fatalf("expected a closed-period refusal, got: %v", err)
	}
}

// TestLedgerIsTenantIsolated proves one tenant cannot see or touch another's
// accounts and journals.
func TestLedgerIsTenantIsolated(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)

	a := env.NewTenant(t, geo, harness.TenantOptions{})
	b := env.NewTenant(t, geo, harness.TenantOptions{})

	pa := &tenant.Principal{UserID: a.AdminUserID, OrganizationID: a.OrgID, OrganizationCurrency: "NGN"}
	pb := &tenant.Principal{UserID: b.AdminUserID, OrganizationID: b.OrgID, OrganizationCurrency: "NGN"}
	svc := ledgerService(env)
	ctx := context.Background()

	// Tenant A posts.
	var res *ledger.Result
	if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
		var e error
		res, e = svc.Post(ctx, tx, pa, simplePosting(6, "ISOLATION"))
		return e
	}); err != nil {
		t.Fatalf("tenant A post: %v", err)
	}

	// Tenant B cannot read A's journal by its public id.
	if _, err := env.Queries.GetJournalByPublicID(ctx, dbgen.GetJournalByPublicIDParams{
		OrganizationID: pb.OrganizationID, PublicID: res.Transaction.PublicID,
	}); err == nil {
		t.Fatal("tenant B read tenant A's journal transaction")
	}

	// B's trial balance is unaffected by A's posting.
	tb, err := svc.TrialBalance(ctx, pb, nil, false)
	if err != nil {
		t.Fatalf("tenant B trial balance: %v", err)
	}
	if tb.TotalDebit != 0 || tb.TotalCredit != 0 {
		t.Fatalf("tenant B sees %d/%d from tenant A's posting", tb.TotalDebit, tb.TotalCredit)
	}

	// And A's own numbers are intact.
	tbA, err := svc.TrialBalance(ctx, pa, nil, false)
	if err != nil {
		t.Fatalf("tenant A trial balance: %v", err)
	}
	if !tbA.Balanced || tbA.TotalDebit != 50000 {
		t.Fatalf("tenant A trial balance = %d debits (balanced=%v), want 50000 and balanced",
			tbA.TotalDebit, tbA.Balanced)
	}
}

// TestSubsidiaryAccountsAreCreatedOncePerParty proves the on-demand subsidiary
// account creation does not produce a duplicate per posting.
func TestSubsidiaryAccountsAreCreatedOncePerParty(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := ledgerService(env)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := env.DB.InTx(ctx, func(tx pgx.Tx) error {
			_, e := svc.Post(ctx, tx, p, ledger.Posting{
				SourceType: ledger.SourceCommission, SourceID: ptrInt64(int64(100 + i)),
				Purpose: "DELIVERY", Description: "Delivery commission",
				Currency: "NGN", PostingDate: time.Now().UTC(),
				Legs: []ledger.Leg{
					{AccountCode: ledger.AcctCommissionExpense, Debit: 1000},
					{AccountCode: ledger.AcctFranchisePayable, Credit: 1000,
						PartyType: ledger.PartyFranchise, PartyID: 42},
				},
			})
			return e
		}); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}

	var accounts int64
	mustQueryRow(t, env, `
		SELECT count(*) FROM ledger_accounts
		WHERE organization_id=$1 AND party_type='FRANCHISE' AND party_id=42`,
		[]any{p.OrganizationID}, &accounts)
	if accounts != 1 {
		t.Fatalf("%d subsidiary accounts for one franchise, want exactly 1", accounts)
	}

	balance, err := svc.PartyBalance(ctx, p, ledger.PartyFranchise, 42, nil)
	if err != nil {
		t.Fatalf("party balance: %v", err)
	}
	if balance != 5000 {
		t.Fatalf("franchise balance = %d, want 5000 across five postings", balance)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func simplePosting(sourceID int64, purpose string) ledger.Posting {
	return ledger.Posting{
		SourceType: ledger.SourceManual, SourceID: &sourceID, Purpose: purpose,
		Description: "Test posting", Reason: "integration test",
		Currency: "NGN", PostingDate: time.Now().UTC(),
		Legs: []ledger.Leg{
			{AccountCode: ledger.AcctCash, Debit: 50000},
			{AccountCode: ledger.AcctFreightRevenue, Credit: 50000},
		},
	}
}

func assertBalance(t *testing.T, svc *ledger.Service, p *tenant.Principal, code string, want int64) {
	t.Helper()
	accounts, _, err := svc.ListAccounts(context.Background(), p, "", "", code, nil, 10, 0)
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
	view, err := svc.AccountBalance(context.Background(), p, publicID, nil)
	if err != nil {
		t.Fatalf("balance of %s: %v", code, err)
	}
	got := int64(0)
	if view.BalanceMinor != nil {
		got = *view.BalanceMinor
	}
	if got != want {
		t.Fatalf("account %s balance = %d, want %d", code, got, want)
	}
}

func mustQueryRow(t *testing.T, env *harness.Env, sql string, args []any, dest ...any) {
	t.Helper()
	if err := env.DB.Pool.QueryRow(context.Background(), sql, args...).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", truncateSQL(sql), err)
	}
}

func truncateSQL(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[:60] + "..."
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func ptrInt64(v int64) *int64 { return &v }
