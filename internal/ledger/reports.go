package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// AccountView is a chart-of-accounts row as the API presents it.
type AccountView struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	AccountType   string  `json:"accountType"`
	NormalBalance string  `json:"normalBalance"`
	Currency      string  `json:"currency"`
	PartyType     *string `json:"partyType,omitempty"`
	IsSystem      bool    `json:"isSystem"`
	IsActive      bool    `json:"isActive"`
	Description   *string `json:"description,omitempty"`
	// Present only on the balance endpoints, so a list stays cheap.
	BalanceMinor *int64 `json:"balanceMinor,omitempty"`
	DebitMinor   *int64 `json:"totalDebitMinor,omitempty"`
	CreditMinor  *int64 `json:"totalCreditMinor,omitempty"`
}

// StatementLine is one movement on an account, with the balance after it.
type StatementLine struct {
	EntryID             string  `json:"id"`
	TransactionID       string  `json:"transactionId"`
	TransactionNumber   string  `json:"transactionNumber"`
	PostingDate         string  `json:"postingDate"`
	Description         string  `json:"description"`
	SourceType          string  `json:"sourceType"`
	SourceID            *string `json:"sourceId,omitempty"`
	Purpose             string  `json:"purpose"`
	DebitMinor          int64   `json:"debitMinor"`
	CreditMinor         int64   `json:"creditMinor"`
	RunningBalanceMinor int64   `json:"runningBalanceMinor"`
	Currency            string  `json:"currency"`
	Memo                *string `json:"memo,omitempty"`
}

// TrialBalanceRow is one line of the trial balance.
type TrialBalanceRow struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	AccountType   string `json:"accountType"`
	NormalBalance string `json:"normalBalance"`
	DebitMinor    int64  `json:"totalDebitMinor"`
	CreditMinor   int64  `json:"totalCreditMinor"`
	BalanceMinor  int64  `json:"balanceMinor"`
}

// TrialBalance is the report plus its own proof of correctness.
type TrialBalance struct {
	AsOf        string            `json:"asOf"`
	Currency    string            `json:"currency"`
	Rows        []TrialBalanceRow `json:"rows"`
	TotalDebit  int64             `json:"totalDebitMinor"`
	TotalCredit int64             `json:"totalCreditMinor"`
	// Balanced is the headline. It must be true; a false here means the ledger
	// has been corrupted by something that bypassed the constraints, and is a
	// production incident rather than a report.
	Balanced        bool  `json:"balanced"`
	DifferenceMinor int64 `json:"differenceMinor"`
}

// TrialBalance builds the report and checks it against itself.
func (s *Service) TrialBalance(
	ctx context.Context, p *tenant.Principal, asOf *time.Time, includeZero bool,
) (*TrialBalance, error) {
	rows, err := s.q.GetTrialBalance(ctx, dbgen.GetTrialBalanceParams{
		OrganizationID: p.OrganizationID,
		AsOf:           asOf,
		IncludeZero:    &includeZero,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	out := &TrialBalance{
		Currency: p.OrganizationCurrency,
		Rows:     make([]TrialBalanceRow, 0, len(rows)),
		AsOf:     "",
	}
	if asOf != nil {
		out.AsOf = asOf.Format("2006-01-02")
	} else {
		out.AsOf = time.Now().UTC().Format("2006-01-02")
	}

	for _, r := range rows {
		out.Rows = append(out.Rows, TrialBalanceRow{
			Code: r.Code, Name: r.Name, AccountType: r.AccountType,
			NormalBalance: r.NormalBalance,
			DebitMinor:    r.TotalDebit, CreditMinor: r.TotalCredit,
			BalanceMinor: r.BalanceMinor,
		})
		out.TotalDebit += r.TotalDebit
		out.TotalCredit += r.TotalCredit
	}
	out.DifferenceMinor = out.TotalDebit - out.TotalCredit
	out.Balanced = out.DifferenceMinor == 0
	return out, nil
}

// AssertBalanced is the whole-ledger invariant as a callable check.
//
// It exists for three consumers: the invariant tests, an operational health
// endpoint, and anything that wants to fail loudly before trusting a report.
func (s *Service) AssertBalanced(ctx context.Context, p *tenant.Principal) error {
	row, err := s.q.AssertLedgerBalanced(ctx, p.OrganizationID)
	if err != nil {
		return apierr.Internal(err)
	}
	if row.Difference != 0 {
		return apierr.Conflict(CodeUnbalanced,
			fmt.Sprintf("The ledger does not balance: debits %d, credits %d, difference %d across %d transactions.",
				row.TotalDebit, row.TotalCredit, row.Difference, row.TransactionCount))
	}
	return nil
}

// UnbalancedTransactions lists any posted transaction whose legs disagree.
// The deferred constraint makes this impossible; a non-empty result is a bug.
func (s *Service) UnbalancedTransactions(
	ctx context.Context, p *tenant.Principal,
) ([]dbgen.FindUnbalancedTransactionsRow, error) {
	rows, err := s.q.FindUnbalancedTransactions(ctx, p.OrganizationID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// AccountBalance returns one account's balance, signed by its normal side.
func (s *Service) AccountBalance(
	ctx context.Context, p *tenant.Principal, publicID string, asOf *time.Time,
) (*AccountView, error) {
	account, err := s.q.GetAccountByPublicID(ctx, dbgen.GetAccountByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Ledger account")
	}

	view := accountView(account)
	bal, err := s.q.GetAccountBalance(ctx, dbgen.GetAccountBalanceParams{
		OrganizationID: p.OrganizationID, ID: account.ID, AsOf: asOf,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			// No entries yet. A zero balance is the correct answer, not an error.
			zero := int64(0)
			view.BalanceMinor, view.DebitMinor, view.CreditMinor = &zero, &zero, &zero
			return &view, nil
		}
		return nil, apierr.Internal(err)
	}
	view.BalanceMinor = &bal.BalanceMinor
	view.DebitMinor = &bal.TotalDebit
	view.CreditMinor = &bal.TotalCredit
	return &view, nil
}

// PartyBalance is what one counterparty owes or is owed across all its accounts.
func (s *Service) PartyBalance(
	ctx context.Context, p *tenant.Principal, partyType string, partyID int64, asOf *time.Time,
) (int64, error) {
	row, err := s.q.GetPartyBalance(ctx, dbgen.GetPartyBalanceParams{
		OrganizationID: p.OrganizationID,
		PartyType:      &partyType,
		PartyID:        &partyID,
		AsOf:           asOf,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return 0, nil
		}
		return 0, apierr.Internal(err)
	}
	return row.BalanceMinor, nil
}

// Statement returns an account's movements with a running balance.
func (s *Service) Statement(
	ctx context.Context, p *tenant.Principal, publicID string,
	from, to *time.Time, limit, offset int32,
) ([]StatementLine, int64, *AccountView, error) {
	account, err := s.q.GetAccountByPublicID(ctx, dbgen.GetAccountByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, 0, nil, ops.NotFoundOr(err, "Ledger account")
	}

	rows, err := s.q.GetAccountStatement(ctx, dbgen.GetAccountStatementParams{
		OrganizationID: p.OrganizationID, AccountID: account.ID,
		FromDate: from, ToDate: to, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, nil, apierr.Internal(err)
	}
	total, err := s.q.CountAccountStatement(ctx, dbgen.CountAccountStatementParams{
		OrganizationID: p.OrganizationID, AccountID: account.ID, FromDate: from, ToDate: to,
	})
	if err != nil {
		return nil, 0, nil, apierr.Internal(err)
	}

	lines := make([]StatementLine, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, StatementLine{
			EntryID: r.PublicID, TransactionID: r.TransactionPublicID,
			TransactionNumber: r.TransactionNumber,
			PostingDate:       r.PostingDate.Format("2006-01-02"),
			Description:       r.Description, SourceType: r.SourceType,
			SourceID: r.SourcePublicID, Purpose: r.Purpose,
			DebitMinor: r.DebitMinor, CreditMinor: r.CreditMinor,
			RunningBalanceMinor: r.RunningBalanceMinor,
			Currency:            r.Currency, Memo: r.Memo,
		})
	}
	view := accountView(account)
	return lines, total, &view, nil
}

// CreateAccount adds a chart-of-accounts entry.
func (s *Service) CreateAccount(
	ctx context.Context, p *tenant.Principal,
	code, name, accountType, description string,
) (*AccountView, error) {
	normal, err := normalBalanceFor(accountType)
	if err != nil {
		return nil, err
	}
	account, err := s.q.CreateLedgerAccount(ctx, dbgen.CreateLedgerAccountParams{
		PublicID:       publicid.New("lac"),
		OrganizationID: p.OrganizationID,
		Code:           code,
		Name:           name,
		AccountType:    accountType,
		NormalBalance:  normal,
		Currency:       p.OrganizationCurrency,
		IsSystem:       false,
		Description:    ops.Optional(description),
		Metadata:       []byte("{}"),
		CreatedBy:      &p.UserID,
	})
	if err != nil {
		if ops.IsUnique(err, "ledger_accounts_code_idx") {
			return nil, apierr.Conflict(apierr.CodeConflict,
				fmt.Sprintf("Account code %s is already in use.", code))
		}
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "ledger.account.created", ResourceType: "ledger_account",
		ResourceID: &account.ID, ResourcePublicID: account.PublicID,
		Metadata: map[string]any{"code": code, "accountType": accountType},
	}))

	view := accountView(account)
	return &view, nil
}

// ListAccounts returns the chart of accounts.
func (s *Service) ListAccounts(
	ctx context.Context, p *tenant.Principal,
	accountType, partyType, search string, activeOnly *bool, limit, offset int32,
) ([]AccountView, int64, error) {
	rows, err := s.q.ListLedgerAccounts(ctx, dbgen.ListLedgerAccountsParams{
		OrganizationID: p.OrganizationID,
		AccountType:    ops.Optional(accountType),
		PartyType:      ops.Optional(partyType),
		IsActive:       activeOnly,
		Search:         ops.Optional(search),
		Limit:          limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	total, err := s.q.CountLedgerAccounts(ctx, dbgen.CountLedgerAccountsParams{
		OrganizationID: p.OrganizationID,
		AccountType:    ops.Optional(accountType),
		PartyType:      ops.Optional(partyType),
		IsActive:       activeOnly,
		Search:         ops.Optional(search),
	})
	if err != nil {
		return nil, 0, apierr.Internal(err)
	}
	out := make([]AccountView, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountView(r))
	}
	return out, total, nil
}

// ---------------------------------------------------------------------------
// Accounting periods
// ---------------------------------------------------------------------------

// EnsurePeriod returns the period covering a date, creating a calendar-month
// period if none exists. Used by provisioning and by postings on a fresh
// tenant so finance is not blocked on a manual setup step.
func (s *Service) EnsurePeriod(
	ctx context.Context, tx pgx.Tx, orgID int64, date time.Time,
) (*dbgen.AccountingPeriod, error) {
	q := s.q.WithTx(tx)
	period, err := q.GetPeriodForDate(ctx, dbgen.GetPeriodForDateParams{
		OrganizationID: orgID, Column2: date,
	})
	if err == nil {
		return &period, nil
	}
	if !ops.IsNoRows(err) {
		return nil, apierr.Internal(err)
	}

	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	created, err := q.CreateAccountingPeriod(ctx, dbgen.CreateAccountingPeriodParams{
		PublicID:       publicid.New("acp"),
		OrganizationID: orgID,
		Code:           start.Format("2006-01"),
		StartsOn:       start,
		EndsOn:         end,
		Status:         ops.Optional("OPEN"),
	})
	if err != nil {
		// Another request created it first; the exclusion constraint says so.
		if again, gErr := q.GetPeriodForDate(ctx, dbgen.GetPeriodForDateParams{
			OrganizationID: orgID, Column2: date,
		}); gErr == nil {
			return &again, nil
		}
		return nil, apierr.Internal(err)
	}
	return &created, nil
}

// ClosePeriod freezes a period against further posting.
func (s *Service) ClosePeriod(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.AccountingPeriod, error) {
	if len([]rune(reason)) < 3 {
		return nil, apierr.Validation("Closing a period requires a reason.",
			map[string]any{"reason": "at least 3 characters"})
	}
	period, err := s.q.GetPeriodByPublicID(ctx, dbgen.GetPeriodByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Accounting period")
	}

	// Refuse to close over a broken ledger: a trial balance that does not
	// balance must be fixed before the period it belongs to is frozen.
	if err := s.AssertBalanced(ctx, p); err != nil {
		return nil, err
	}

	closed, err := s.q.CloseAccountingPeriod(ctx, dbgen.CloseAccountingPeriodParams{
		OrganizationID: p.OrganizationID, ID: period.ID,
		ClosedBy: &p.UserID, CloseReason: &reason,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(apierr.CodeConflict,
				fmt.Sprintf("Period %s is already closed.", period.Code))
		}
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "ledger.period.closed", ResourceType: "accounting_period",
		ResourceID: &closed.ID, ResourcePublicID: closed.PublicID, Reason: reason,
		Metadata: map[string]any{"code": closed.Code},
	}))
	return &closed, nil
}

// ReopenPeriod unfreezes a closed period. Audited loudly: reopening a closed
// period changes numbers that have already been reported.
func (s *Service) ReopenPeriod(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.AccountingPeriod, error) {
	if len([]rune(reason)) < 10 {
		return nil, apierr.Validation("Reopening a closed period requires a substantive reason.",
			map[string]any{"reason": "at least 10 characters"})
	}
	period, err := s.q.GetPeriodByPublicID(ctx, dbgen.GetPeriodByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Accounting period")
	}
	reopened, err := s.q.ReopenAccountingPeriod(ctx, dbgen.ReopenAccountingPeriodParams{
		OrganizationID: p.OrganizationID, ID: period.ID, CloseReason: &reason,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(apierr.CodeConflict,
				fmt.Sprintf("Period %s is not closed.", period.Code))
		}
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "ledger.period.reopened", ResourceType: "accounting_period",
		ResourceID: &reopened.ID, ResourcePublicID: reopened.PublicID, Reason: reason,
		Metadata: map[string]any{"code": reopened.Code},
	}))
	return &reopened, nil
}

// ListPeriods returns the accounting calendar.
func (s *Service) ListPeriods(
	ctx context.Context, p *tenant.Principal, limit int32,
) ([]dbgen.AccountingPeriod, error) {
	rows, err := s.q.ListAccountingPeriods(ctx, dbgen.ListAccountingPeriodsParams{
		OrganizationID: p.OrganizationID, Limit: limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

func normalBalanceFor(accountType string) (string, error) {
	switch accountType {
	case TypeAsset, TypeExpense:
		return NormalDebit, nil
	case TypeLiability, TypeEquity, TypeRevenue:
		return NormalCredit, nil
	}
	return "", apierr.Validation("Unknown account type.",
		map[string]any{"accountType": accountType,
			"allowed": []string{TypeAsset, TypeLiability, TypeEquity, TypeRevenue, TypeExpense}})
}

func accountView(a dbgen.LedgerAccount) AccountView {
	return AccountView{
		ID: a.PublicID, Code: a.Code, Name: a.Name,
		AccountType: a.AccountType, NormalBalance: a.NormalBalance,
		Currency: a.Currency, PartyType: a.PartyType,
		IsSystem: a.IsSystem, IsActive: a.IsActive, Description: a.Description,
	}
}

// RollupBalance returns a control account's balance including every subsidiary
// account beneath it.
//
// Use this for "what is our total COD exposure?" style questions. AccountBalance
// answers "what is on this exact account", which for a control account with
// subsidiaries is zero — correct, but rarely the question being asked.
func (s *Service) RollupBalance(
	ctx context.Context, p *tenant.Principal, code string, asOf *time.Time,
) (int64, int64, error) {
	row, err := s.q.GetAccountRollupBalance(ctx, dbgen.GetAccountRollupBalanceParams{
		OrganizationID: p.OrganizationID, Code: code, AsOf: asOf,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return 0, 0, nil
		}
		return 0, 0, apierr.Internal(err)
	}
	return row.BalanceMinor, row.AccountCount, nil
}

// GetJournal returns a posted transaction with its entries.
func (s *Service) GetJournal(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetJournalByPublicIDRow, []dbgen.ListJournalEntriesRow, error) {
	txn, err := s.q.GetJournalByPublicID(ctx, dbgen.GetJournalByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, nil, ops.NotFoundOr(err, "Journal transaction")
	}
	entries, err := s.q.ListJournalEntries(ctx, txn.ID)
	if err != nil {
		return nil, nil, apierr.Internal(err)
	}
	return &txn, entries, nil
}

// ListJournals returns the journal register.
func (s *Service) ListJournals(
	ctx context.Context, p *tenant.Principal,
	status, sourceType string, from, to *time.Time, cursor *int64, limit int32,
) ([]dbgen.ListJournalTransactionsRow, error) {
	rows, err := s.q.ListJournalTransactions(ctx, dbgen.ListJournalTransactionsParams{
		OrganizationID: p.OrganizationID,
		Status:         ops.Optional(status),
		SourceType:     ops.Optional(sourceType),
		FromDate:       from, ToDate: to,
		CursorID: cursor, Limit: limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// DB exposes the pool so the transport can open a transaction around a posting
// that spans more than one service call.
func (s *Service) DB() *database.DB { return s.db }
