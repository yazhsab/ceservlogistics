// Package ledger implements M22: the double-entry accounting system of record.
//
// Every monetary fact in Release 3 becomes a journal transaction here.
// Commission, COD, settlement and billing do not each keep their own idea of
// what is owed; they post, and then they read balances back out.
//
// The invariant — SUM(debits) = SUM(credits) for every posted transaction — is
// enforced in three places, deliberately:
//
//	Builder.Balanced()   in Go, so a caller gets a clear error before any I/O
//	deferred constraint  in PostgreSQL, at COMMIT, against any writer including psql
//	AssertBalanced()     as a whole-ledger check, used by tests and health probes
//
// The Go check is a convenience. The database check is the guarantee. If they
// ever disagree, the database is right.
//
// # Debits and credits
//
// Entries carry separate debit_minor and credit_minor columns rather than a
// signed amount. That makes "which side" explicit in every row a human reads,
// makes the balance check a plain SUM comparison, and makes a sign error
// impossible to express. Which side increases an account is a property of the
// account, not of the caller:
//
//	ASSET, EXPENSE                normal balance DEBIT   — debit increases
//	LIABILITY, EQUITY, REVENUE    normal balance CREDIT  — credit increases
//
// # Idempotency
//
// Finance retries. Every posting carries a natural key of
// (source_type, source_id, purpose), backed by a partial unique index. Posting
// "the delivery commission for shipment 4711" twice returns the first
// transaction rather than paying twice — and that holds even if the HTTP
// idempotency layer was bypassed entirely (§21).
package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/money"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Account types.
const (
	TypeAsset     = "ASSET"
	TypeLiability = "LIABILITY"
	TypeEquity    = "EQUITY"
	TypeRevenue   = "REVENUE"
	TypeExpense   = "EXPENSE"
)

// Normal balances.
const (
	NormalDebit  = "DEBIT"
	NormalCredit = "CREDIT"
)

// Transaction statuses.
const (
	StatusDraft    = "DRAFT"
	StatusPosted   = "POSTED"
	StatusReversed = "REVERSED"
)

// Source types. These name what caused a posting and, with purpose, form the
// idempotency key.
const (
	SourceCommission     = "COMMISSION"
	SourceCOD            = "COD"
	SourceSettlement     = "SETTLEMENT"
	SourceInvoice        = "INVOICE"
	SourceCreditNote     = "CREDIT_NOTE"
	SourceDebitNote      = "DEBIT_NOTE"
	SourcePayment        = "PAYMENT"
	SourceAdjustment     = "ADJUSTMENT"
	SourceReversal       = "REVERSAL"
	SourceOpeningBalance = "OPENING_BALANCE"
	SourceManual         = "MANUAL"
)

// Party types for subsidiary accounts.
const (
	PartyFranchise     = "FRANCHISE"
	PartyCustomer      = "CUSTOMER"
	PartyOperatingUnit = "OPERATING_UNIT"
	PartyAgent         = "AGENT"
	PartyCarrier       = "CARRIER"
)

// System account codes, seeded by migration 0020. Referenced from Go by
// constant so a rename in the database cannot silently repoint a posting.
const (
	AcctCash                   = "1000"
	AcctCODReceivableAgent     = "1100"
	AcctCODReceivableBranch    = "1110"
	AcctCODReceivableFranchise = "1120"
	AcctTradeReceivable        = "1200"
	AcctFranchiseReceivable    = "1300"
	AcctCODShortageRecoverable = "1400"
	AcctCODPayableConsignor    = "2000"
	AcctCommissionPayable      = "2100"
	AcctFranchisePayable       = "2200"
	AcctTaxPayable             = "2300"
	AcctWithholdingPayable     = "2310"
	AcctCustomerAdvances       = "2400"
	AcctCODExcessPayable       = "2500"
	AcctRetainedEarnings       = "3000"
	AcctOpeningBalanceEquity   = "3100"
	AcctFreightRevenue         = "4000"
	AcctSurchargeRevenue       = "4100"
	AcctCODFeeRevenue          = "4200"
	AcctOtherRevenue           = "4300"
	AcctCommissionExpense      = "5000"
	AcctIncentiveExpense       = "5100"
	AcctCODShortageWrittenOff  = "5200"
	AcctDiscountAllowed        = "5300"
)

// Error codes returned to clients.
const (
	CodeUnbalanced       = "LEDGER_UNBALANCED"
	CodePeriodClosed     = "PERIOD_CLOSED"
	CodeAccountMissing   = "LEDGER_ACCOUNT_MISSING"
	CodeAlreadyPosted    = "LEDGER_ALREADY_POSTED"
	CodeAlreadyReversed  = "LEDGER_ALREADY_REVERSED"
	CodeNotPosted        = "LEDGER_NOT_POSTED"
	CodeCurrencyMismatch = "LEDGER_CURRENCY_MISMATCH"
)

// Service is the only supported way to write to the ledger.
type Service struct {
	db    *database.DB
	q     *dbgen.Queries
	audit *audit.Recorder
	log   *slog.Logger
}

func NewService(db *database.DB, q *dbgen.Queries, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{db: db, q: q, audit: rec, log: log}
}

// Leg is one side of a transaction: an amount against an account.
//
// Callers name the account by code rather than by id, because codes are stable
// across environments and are what the chart of accounts documents.
type Leg struct {
	AccountCode string
	// Set exactly one of Debit or Credit. Both zero, or both non-zero, is a
	// programming error and is rejected before any I/O.
	Debit  int64
	Credit int64

	// Subsidiary routing. When PartyType is set the leg posts to that party's
	// own account under the named control account, creating it on first use.
	PartyType string
	PartyID   int64

	// Reporting dimensions, carried onto the entry.
	FranchiseID     *int64
	OperatingUnitID *int64
	CustomerID      *int64
	ShipmentID      *int64

	Memo string
}

// Posting describes a transaction to write.
type Posting struct {
	// SourceType, SourceID and Purpose form the idempotency key. A repeated
	// posting with the same three returns the existing transaction.
	SourceType     string
	SourceID       *int64
	SourcePublicID string
	Purpose        string

	Description string
	// Required for MANUAL, ADJUSTMENT and REVERSAL postings, and enforced by a
	// CHECK constraint as well as here.
	Reason string

	Currency    string
	PostingDate time.Time
	Legs        []Leg

	Metadata map[string]any
}

// Result is what a caller needs after posting: the transaction and whether it
// was created now or replayed from an earlier identical call.
type Result struct {
	Transaction dbgen.JournalTransaction
	Entries     []dbgen.JournalEntry
	// Replayed is true when the natural key already existed. Callers use it to
	// decide whether to emit a duplicate-suppressed log line rather than to
	// change behaviour: either way the money moved exactly once.
	Replayed bool
}

// Post writes a balanced transaction inside the caller's transaction.
//
// It takes a pgx.Tx rather than opening its own, because a posting is almost
// never the whole story: a COD collection writes the collection row, updates
// the obligation and posts the journal, and either all three happen or none do.
func (s *Service) Post(ctx context.Context, tx pgx.Tx, p *tenant.Principal, in Posting) (*Result, error) {
	q := s.q.WithTx(tx)

	if err := in.validate(); err != nil {
		return nil, err
	}

	// Idempotency probe. The partial unique index is the real guarantee; this
	// lookup exists so the common retry returns the original cleanly instead of
	// surfacing a constraint violation.
	if in.SourceID != nil {
		existing, err := q.FindJournalBySource(ctx, dbgen.FindJournalBySourceParams{
			OrganizationID: p.OrganizationID,
			SourceType:     in.SourceType,
			SourceID:       in.SourceID,
			Purpose:        in.Purpose,
		})
		if err == nil {
			entries, eErr := q.ListJournalEntries(ctx, existing.ID)
			if eErr != nil {
				return nil, apierr.Internal(eErr)
			}
			return &Result{Transaction: existing, Entries: toEntries(entries), Replayed: true}, nil
		}
		if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}

	period, err := s.periodFor(ctx, q, p.OrganizationID, in.PostingDate)
	if err != nil {
		return nil, err
	}

	total, err := totalOf(in.Legs)
	if err != nil {
		return nil, err
	}

	number, err := s.allocateNumber(ctx, q, p.OrganizationID, in.PostingDate)
	if err != nil {
		return nil, err
	}

	meta, err := marshalMeta(in.Metadata)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	txn, err := q.CreateJournalTransaction(ctx, dbgen.CreateJournalTransactionParams{
		PublicID:          publicid.New("jrn"),
		OrganizationID:    p.OrganizationID,
		TransactionNumber: number,
		PeriodID:          period.ID,
		PostingDate:       in.PostingDate,
		Status:            StatusPosted,
		Currency:          in.Currency,
		SourceType:        in.SourceType,
		SourceID:          in.SourceID,
		SourcePublicID:    ops.Optional(in.SourcePublicID),
		Purpose:           in.Purpose,
		Description:       in.Description,
		Reason:            ops.Optional(in.Reason),
		TotalMinor:        total,
		EntryCount:        int32(len(in.Legs)),
		PostedAt:          &now,
		PostedBy:          &p.UserID,
		CreatedBy:         &p.UserID,
		RequestID:         ops.Optional(requestIDFrom(ctx)),
		Metadata:          meta,
	})
	if err != nil {
		// The unique index fires when two identical postings race. The loser
		// re-reads the winner's row, so both callers observe one payment.
		if ops.IsUnique(err, "journal_transactions_source_idx") && in.SourceID != nil {
			existing, fErr := q.FindJournalBySource(ctx, dbgen.FindJournalBySourceParams{
				OrganizationID: p.OrganizationID,
				SourceType:     in.SourceType,
				SourceID:       in.SourceID,
				Purpose:        in.Purpose,
			})
			if fErr == nil {
				entries, _ := q.ListJournalEntries(ctx, existing.ID)
				return &Result{Transaction: existing, Entries: toEntries(entries), Replayed: true}, nil
			}
		}
		if msg, ok := ops.TriggerMessage(err); ok {
			return nil, apierr.Conflict(CodePeriodClosed, msg)
		}
		return nil, apierr.Internal(err)
	}

	entries := make([]dbgen.JournalEntry, 0, len(in.Legs))
	for i, leg := range in.Legs {
		account, aErr := s.resolveAccount(ctx, q, p, leg, in.Currency)
		if aErr != nil {
			return nil, aErr
		}
		entry, eErr := q.CreateJournalEntry(ctx, dbgen.CreateJournalEntryParams{
			PublicID:        publicid.New("jen"),
			OrganizationID:  p.OrganizationID,
			TransactionID:   txn.ID,
			AccountID:       account.ID,
			LineNo:          int32(i + 1),
			DebitMinor:      leg.Debit,
			CreditMinor:     leg.Credit,
			Currency:        in.Currency,
			FranchiseID:     leg.FranchiseID,
			OperatingUnitID: leg.OperatingUnitID,
			CustomerID:      leg.CustomerID,
			ShipmentID:      leg.ShipmentID,
			Memo:            ops.Optional(leg.Memo),
			Metadata:        []byte("{}"),
		})
		if eErr != nil {
			return nil, apierr.Internal(eErr)
		}
		entries = append(entries, entry)
	}

	return &Result{Transaction: txn, Entries: entries}, nil
}

// Reverse writes the mirror image of a posted transaction and marks the
// original REVERSED.
//
// Reversal is the only way to undo a posting (§23). The original stays exactly
// as it was, so an auditor sees both the mistake and the correction rather than
// a rewritten past.
func (s *Service) Reverse(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal,
	transactionID int64, reason string, postingDate time.Time,
) (*Result, error) {
	q := s.q.WithTx(tx)

	if len([]rune(reason)) < 3 {
		return nil, apierr.Validation("A reversal needs a reason.",
			map[string]any{"reason": "at least 3 characters"})
	}

	// Lock first: two concurrent reversals of the same journal must serialise,
	// or both would write a mirror and the account would be doubly credited.
	original, err := q.LockJournalForReversal(ctx, dbgen.LockJournalForReversalParams{
		OrganizationID: p.OrganizationID, ID: transactionID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Journal transaction")
	}
	switch original.Status {
	case StatusReversed:
		return nil, apierr.Conflict(CodeAlreadyReversed,
			fmt.Sprintf("Journal %s has already been reversed.", original.TransactionNumber))
	case StatusDraft:
		return nil, apierr.Conflict(CodeNotPosted,
			fmt.Sprintf("Journal %s is a draft and has nothing to reverse.", original.TransactionNumber))
	}

	originalEntries, err := q.ListJournalEntries(ctx, original.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	if len(originalEntries) == 0 {
		return nil, apierr.Internal(errors.New("ledger: posted transaction has no entries"))
	}

	if postingDate.IsZero() {
		postingDate = time.Now().UTC()
	}
	period, err := s.periodFor(ctx, q, p.OrganizationID, postingDate)
	if err != nil {
		return nil, err
	}
	number, err := s.allocateNumber(ctx, q, p.OrganizationID, postingDate)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	reversal, err := q.CreateJournalTransaction(ctx, dbgen.CreateJournalTransactionParams{
		PublicID:          publicid.New("jrn"),
		OrganizationID:    p.OrganizationID,
		TransactionNumber: number,
		PeriodID:          period.ID,
		PostingDate:       postingDate,
		Status:            StatusPosted,
		Currency:          original.Currency,
		SourceType:        SourceReversal,
		SourceID:          &original.ID,
		SourcePublicID:    &original.PublicID,
		Purpose:           "REVERSAL:" + original.Purpose,
		Description:       "Reversal of " + original.TransactionNumber + ": " + original.Description,
		Reason:            &reason,
		ReversesID:        &original.ID,
		TotalMinor:        original.TotalMinor,
		EntryCount:        int32(len(originalEntries)),
		PostedAt:          &now,
		PostedBy:          &p.UserID,
		CreatedBy:         &p.UserID,
		RequestID:         ops.Optional(requestIDFrom(ctx)),
		Metadata:          []byte("{}"),
	})
	if err != nil {
		if msg, ok := ops.TriggerMessage(err); ok {
			return nil, apierr.Conflict(CodePeriodClosed, msg)
		}
		return nil, apierr.Internal(err)
	}

	// Every leg, with the sides swapped.
	entries := make([]dbgen.JournalEntry, 0, len(originalEntries))
	for i, oe := range originalEntries {
		entry, eErr := q.CreateJournalEntry(ctx, dbgen.CreateJournalEntryParams{
			PublicID:        publicid.New("jen"),
			OrganizationID:  p.OrganizationID,
			TransactionID:   reversal.ID,
			AccountID:       oe.AccountID,
			LineNo:          int32(i + 1),
			DebitMinor:      oe.CreditMinor, // swapped
			CreditMinor:     oe.DebitMinor,  // swapped
			Currency:        oe.Currency,
			FranchiseID:     oe.FranchiseID,
			OperatingUnitID: oe.OperatingUnitID,
			CustomerID:      oe.CustomerID,
			ShipmentID:      oe.ShipmentID,
			Memo:            ops.Optional("Reversal of line " + fmt.Sprint(oe.LineNo)),
			Metadata:        []byte("{}"),
		})
		if eErr != nil {
			return nil, apierr.Internal(eErr)
		}
		entries = append(entries, entry)
	}

	if _, err := q.MarkJournalReversed(ctx, dbgen.MarkJournalReversedParams{
		OrganizationID: p.OrganizationID, ID: original.ID, ReversedByID: &reversal.ID,
	}); err != nil {
		return nil, apierr.Internal(err)
	}

	if err := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
		Action:       "ledger.journal.reversed",
		ResourceType: "journal_transaction",
		ResourceID:   &original.ID, ResourcePublicID: original.PublicID,
		Reason: reason,
		Metadata: map[string]any{
			"originalNumber": original.TransactionNumber,
			"reversalNumber": reversal.TransactionNumber,
			"amountMinor":    original.TotalMinor,
			"currency":       original.Currency,
		},
	})); err != nil {
		return nil, err
	}

	return &Result{Transaction: reversal, Entries: entries}, nil
}

// resolveAccount finds the account a leg posts to, creating the subsidiary
// account for a party on first use.
func (s *Service) resolveAccount(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, leg Leg, currency string,
) (*dbgen.LedgerAccount, error) {
	control, err := q.GetAccountByCode(ctx, dbgen.GetAccountByCodeParams{
		OrganizationID: p.OrganizationID, Code: leg.AccountCode,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeAccountMissing,
				fmt.Sprintf("Ledger account %s does not exist. The chart of accounts may not be provisioned.", leg.AccountCode))
		}
		return nil, apierr.Internal(err)
	}

	if leg.PartyType == "" {
		if control.Currency != currency {
			return nil, apierr.Conflict(CodeCurrencyMismatch,
				fmt.Sprintf("Account %s is held in %s and cannot take a %s posting.",
					control.Code, control.Currency, currency))
		}
		return &control, nil
	}

	// Subsidiary account, created on first use so onboarding a franchise needs
	// no separate chart-of-accounts step.
	sub, err := q.FindPartyAccount(ctx, dbgen.FindPartyAccountParams{
		OrganizationID: p.OrganizationID,
		ParentID:       &control.ID,
		PartyType:      &leg.PartyType,
		PartyID:        &leg.PartyID,
	})
	if err == nil {
		return &sub, nil
	}
	if !ops.IsNoRows(err) {
		return nil, apierr.Internal(err)
	}

	created, err := q.CreateLedgerAccount(ctx, dbgen.CreateLedgerAccountParams{
		PublicID:       publicid.New("lac"),
		OrganizationID: p.OrganizationID,
		Code:           fmt.Sprintf("%s.%s.%d", control.Code, shortParty(leg.PartyType), leg.PartyID),
		Name:           fmt.Sprintf("%s — %s %d", control.Name, titleParty(leg.PartyType), leg.PartyID),
		AccountType:    control.AccountType,
		NormalBalance:  control.NormalBalance,
		Currency:       currency,
		PartyType:      &leg.PartyType,
		PartyID:        &leg.PartyID,
		ParentID:       &control.ID,
		IsSystem:       true,
		Description:    ops.Optional("Subsidiary account created on first posting."),
		Metadata:       []byte("{}"),
		CreatedBy:      &p.UserID,
	})
	if err != nil {
		// Lost the race to create it; the winner's row is what we want.
		if sub, fErr := q.FindPartyAccount(ctx, dbgen.FindPartyAccountParams{
			OrganizationID: p.OrganizationID,
			ParentID:       &control.ID,
			PartyType:      &leg.PartyType,
			PartyID:        &leg.PartyID,
		}); fErr == nil {
			return &sub, nil
		}
		return nil, apierr.Internal(err)
	}
	return &created, nil
}

// periodFor resolves a posting date to its accounting period and refuses a
// closed one. The database enforces this too; here it produces a better error.
func (s *Service) periodFor(
	ctx context.Context, q *dbgen.Queries, orgID int64, date time.Time,
) (*dbgen.AccountingPeriod, error) {
	period, err := q.GetPeriodForDate(ctx, dbgen.GetPeriodForDateParams{
		OrganizationID: orgID, Column2: date,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodePeriodClosed,
				fmt.Sprintf("No accounting period covers %s. Create one before posting.",
					date.Format("2006-01-02")))
		}
		return nil, apierr.Internal(err)
	}
	if period.Status == "CLOSED" {
		return nil, apierr.Conflict(CodePeriodClosed,
			fmt.Sprintf("Accounting period %s is closed and cannot receive postings.", period.Code))
	}
	return &period, nil
}

// allocateNumber produces the next human-readable journal number for the month.
func (s *Service) allocateNumber(
	ctx context.Context, q *dbgen.Queries, orgID int64, date time.Time,
) (string, error) {
	scope := date.Format("200601")
	next, err := q.AllocateJournalNumber(ctx, dbgen.AllocateJournalNumberParams{
		OrganizationID: orgID, ScopeKey: scope,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return "", apierr.Conflict(apierr.CodeConflict,
				"The journal number sequence is exhausted for this month.")
		}
		return "", apierr.Internal(err)
	}
	return fmt.Sprintf("JV-%s-%06d", scope, next), nil
}

// validate checks a posting before any I/O.
func (in *Posting) validate() error {
	if in.SourceType == "" || in.Purpose == "" {
		return apierr.BadRequest("A posting needs a source type and a purpose.")
	}
	if in.Description == "" {
		return apierr.BadRequest("A posting needs a description.")
	}
	// Supported, not merely well-formed: an unrecognised code would be summed
	// and reported as though it were money.
	if !money.Currency(in.Currency).Supported() {
		return apierr.Validation("Unsupported currency.",
			map[string]any{"currency": in.Currency, "supported": money.SupportedCodes()})
	}
	if len(in.Legs) < 2 {
		return apierr.Validation("A journal transaction needs at least two legs.",
			map[string]any{"legs": len(in.Legs)})
	}
	switch in.SourceType {
	case SourceManual, SourceAdjustment, SourceReversal:
		if len([]rune(in.Reason)) < 3 {
			return apierr.Validation("This posting requires a reason.",
				map[string]any{"reason": "at least 3 characters"})
		}
	}
	if in.PostingDate.IsZero() {
		in.PostingDate = time.Now().UTC()
	}
	for i, leg := range in.Legs {
		if leg.AccountCode == "" {
			return apierr.Validation("Every leg needs an account.",
				map[string]any{"line": i + 1})
		}
		if leg.Debit < 0 || leg.Credit < 0 {
			return apierr.Validation("Amounts cannot be negative; use the other side instead.",
				map[string]any{"line": i + 1})
		}
		if (leg.Debit == 0) == (leg.Credit == 0) {
			return apierr.Validation("Every leg must be either a debit or a credit, not both and not neither.",
				map[string]any{"line": i + 1, "debitMinor": leg.Debit, "creditMinor": leg.Credit})
		}
		if leg.PartyType != "" && leg.PartyID == 0 {
			return apierr.Validation("A party-scoped leg needs a party id.",
				map[string]any{"line": i + 1})
		}
	}
	return nil
}

// totalOf returns the transaction total and refuses an unbalanced set of legs.
// This is the Go half of the invariant; the database enforces it again at
// COMMIT, which is what actually guarantees it.
func totalOf(legs []Leg) (int64, error) {
	var debits, credits int64
	for _, leg := range legs {
		debits += leg.Debit
		credits += leg.Credit
	}
	if debits != credits {
		return 0, apierr.Conflict(CodeUnbalanced,
			fmt.Sprintf("The transaction does not balance: debits %d, credits %d.", debits, credits)).
			WithDetail("totalDebitMinor", debits).
			WithDetail("totalCreditMinor", credits).
			WithDetail("differenceMinor", debits-credits)
	}
	if debits == 0 {
		return 0, apierr.Validation("A transaction cannot be for zero.", nil)
	}
	return debits, nil
}

func toEntries(rows []dbgen.ListJournalEntriesRow) []dbgen.JournalEntry {
	out := make([]dbgen.JournalEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbgen.JournalEntry{
			ID: r.ID, PublicID: r.PublicID, OrganizationID: r.OrganizationID,
			TransactionID: r.TransactionID, AccountID: r.AccountID, LineNo: r.LineNo,
			DebitMinor: r.DebitMinor, CreditMinor: r.CreditMinor, Currency: r.Currency,
			FranchiseID: r.FranchiseID, OperatingUnitID: r.OperatingUnitID,
			CustomerID: r.CustomerID, ShipmentID: r.ShipmentID,
			Memo: r.Memo, Metadata: r.Metadata, CreatedAt: r.CreatedAt,
		})
	}
	return out
}

func marshalMeta(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("ledger: marshal metadata: %w", err))
	}
	return b, nil
}

func shortParty(t string) string {
	switch t {
	case PartyFranchise:
		return "FRN"
	case PartyCustomer:
		return "CUS"
	case PartyOperatingUnit:
		return "OPU"
	case PartyAgent:
		return "AGT"
	case PartyCarrier:
		return "CAR"
	}
	return "OTH"
}

func titleParty(t string) string {
	switch t {
	case PartyFranchise:
		return "franchise"
	case PartyCustomer:
		return "customer"
	case PartyOperatingUnit:
		return "unit"
	case PartyAgent:
		return "agent"
	case PartyCarrier:
		return "carrier"
	}
	return "party"
}

func requestIDFrom(ctx context.Context) string { return httpx.RequestID(ctx) }
