package financeapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
)

// ---------------------------------------------------------------------------
// M22 Ledger
// ---------------------------------------------------------------------------

func (h *Handler) ledgerRoutes(r chi.Router) {
	r.Get("/accounts", require("ledger.read", h.listAccounts))
	r.Post("/accounts", require("ledger.account_manage", h.createAccount))
	r.Get("/accounts/{accountId}", require("ledger.read", h.getAccountBalance))
	r.Get("/accounts/{accountId}/statement", require("ledger.read", h.accountStatement))

	r.Get("/journals", require("ledger.read", h.listJournals))
	r.Post("/journals", require("ledger.post", h.postJournal))
	r.Get("/journals/{journalId}", require("ledger.read", h.getJournal))
	r.Post("/journals/{journalId}/reverse", require("ledger.reverse", h.reverseJournal))

	r.Get("/trial-balance", require("ledger.read", h.trialBalance))
	// The invariant as an endpoint. Finance and monitoring both want to ask
	// "are the books consistent?" without reading every entry.
	r.Get("/health", require("ledger.read", h.ledgerHealth))

	r.Get("/periods", require("ledger.read", h.listPeriods))
	r.Post("/periods/{periodId}/close", require("ledger.period_manage", h.closePeriod))
	r.Post("/periods/{periodId}/reopen", require("ledger.period_manage", h.reopenPeriod))
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	var activeOnly *bool
	if r.URL.Query().Get("activeOnly") == "true" {
		t := true
		activeOnly = &t
	}
	rows, total, err := h.ledger.ListAccounts(r.Context(), p,
		r.URL.Query().Get("accountType"), r.URL.Query().Get("partyType"),
		r.URL.Query().Get("search"), activeOnly, limit(r), offset(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows, "total": total})
}

type createAccountRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	AccountType string `json:"accountType"`
	Description string `json:"description"`
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[createAccountRequest](w, r)
	if err != nil {
		return err
	}
	if in.Code == "" || in.Name == "" {
		return apierr.Validation("An account needs a code and a name.", nil)
	}
	account, err := h.ledger.CreateAccount(r.Context(), p, in.Code, in.Name, in.AccountType, in.Description)
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/ledger/accounts/"+account.ID, account)
}

func (h *Handler) getAccountBalance(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "accountId", "lac", "Ledger account")
	if err != nil {
		return err
	}
	asOf, err := optDate(r, "asOf")
	if err != nil {
		return err
	}
	view, err := h.ledger.AccountBalance(r.Context(), p, id, asOf)
	if err != nil {
		return err
	}
	return httpx.OK(w, view)
}

func (h *Handler) accountStatement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "accountId", "lac", "Ledger account")
	if err != nil {
		return err
	}
	from, err := optDate(r, "from")
	if err != nil {
		return err
	}
	to, err := optDate(r, "to")
	if err != nil {
		return err
	}
	lines, total, account, err := h.ledger.Statement(r.Context(), p, id, from, to, limit(r), offset(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"account": account, "data": lines, "total": total,
	})
}

type postJournalRequest struct {
	Description string `json:"description"`
	Reason      string `json:"reason"`
	PostingDate string `json:"postingDate"`
	Currency    string `json:"currency"`
	Legs        []struct {
		AccountCode string `json:"accountCode"`
		DebitMinor  int64  `json:"debitMinor"`
		CreditMinor int64  `json:"creditMinor"`
		PartyType   string `json:"partyType,omitempty"`
		PartyID     int64  `json:"partyId,omitempty"`
		Memo        string `json:"memo,omitempty"`
	} `json:"legs"`
}

// postJournal writes a manual journal.
//
// Manual posting is deliberately narrow: it always carries source type MANUAL
// and requires a reason, so an operator-entered journal is distinguishable from
// one a module produced and is never mistaken for an automated posting.
func (h *Handler) postJournal(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[postJournalRequest](w, r)
	if err != nil {
		return err
	}

	posting := ledger.Posting{
		SourceType:  ledger.SourceManual,
		Purpose:     "MANUAL",
		Description: in.Description,
		Reason:      in.Reason,
		Currency:    in.Currency,
	}
	if posting.Currency == "" {
		posting.Currency = p.OrganizationCurrency
	}
	if in.PostingDate != "" {
		d, dErr := reqDate(in.PostingDate, "postingDate")
		if dErr != nil {
			return dErr
		}
		posting.PostingDate = d
	}
	for _, leg := range in.Legs {
		l := ledger.Leg{
			AccountCode: leg.AccountCode, Debit: leg.DebitMinor, Credit: leg.CreditMinor,
			PartyType: leg.PartyType, PartyID: leg.PartyID, Memo: leg.Memo,
		}
		posting.Legs = append(posting.Legs, l)
	}

	var result *ledger.Result
	if err := h.txn(r, func(tx pgx.Tx) error {
		var pErr error
		result, pErr = h.ledger.Post(r.Context(), tx, p, posting)
		return pErr
	}); err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/ledger/journals/"+result.Transaction.PublicID,
		map[string]any{"transaction": result.Transaction, "entries": result.Entries})
}

func (h *Handler) getJournal(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "journalId", "jrn", "Journal transaction")
	if err != nil {
		return err
	}
	txn, entries, err := h.ledger.GetJournal(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"transaction": txn, "entries": entries})
}

type reverseRequest struct {
	Reason      string `json:"reason"`
	PostingDate string `json:"postingDate"`
}

func (h *Handler) reverseJournal(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "journalId", "jrn", "Journal transaction")
	if err != nil {
		return err
	}
	in, err := body[reverseRequest](w, r)
	if err != nil {
		return err
	}

	txn, _, err := h.ledger.GetJournal(r.Context(), p, id)
	if err != nil {
		return err
	}
	postingDate := time.Now().UTC()
	if in.PostingDate != "" {
		d, dErr := reqDate(in.PostingDate, "postingDate")
		if dErr != nil {
			return dErr
		}
		postingDate = d
	}

	var result *ledger.Result
	if err := h.txn(r, func(tx pgx.Tx) error {
		var rErr error
		result, rErr = h.ledger.Reverse(r.Context(), tx, p, txn.ID, in.Reason, postingDate)
		return rErr
	}); err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"reversal": result.Transaction, "entries": result.Entries,
	})
}

func (h *Handler) listJournals(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	from, err := optDate(r, "from")
	if err != nil {
		return err
	}
	to, err := optDate(r, "to")
	if err != nil {
		return err
	}
	rows, err := h.ledger.ListJournals(r.Context(), p,
		r.URL.Query().Get("status"), r.URL.Query().Get("sourceType"),
		from, to, optInt64(r, "cursor"), limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

func (h *Handler) trialBalance(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	asOf, err := optDate(r, "asOf")
	if err != nil {
		return err
	}
	tb, err := h.ledger.TrialBalance(r.Context(), p, asOf,
		r.URL.Query().Get("includeZero") == "true")
	if err != nil {
		return err
	}
	return httpx.OK(w, tb)
}

// ledgerHealth reports whether the books balance.
//
// A 200 with balanced=false is deliberately not an error status: the caller
// asked a question and got a truthful answer. Monitoring alerts on the field,
// not on the status code.
func (h *Handler) ledgerHealth(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	unbalanced, err := h.ledger.UnbalancedTransactions(r.Context(), p)
	if err != nil {
		return err
	}
	balanced := len(unbalanced) == 0
	if bErr := h.ledger.AssertBalanced(r.Context(), p); bErr != nil {
		balanced = false
	}
	return httpx.OK(w, map[string]any{
		"balanced":               balanced,
		"unbalancedTransactions": unbalanced,
		"unbalancedCount":        len(unbalanced),
	})
}

func (h *Handler) listPeriods(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	rows, err := h.ledger.ListPeriods(r.Context(), p, limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) closePeriod(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "periodId", "acp", "Accounting period")
	if err != nil {
		return err
	}
	in, err := body[reasonRequest](w, r)
	if err != nil {
		return err
	}
	period, err := h.ledger.ClosePeriod(r.Context(), p, id, in.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, period)
}

func (h *Handler) reopenPeriod(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "periodId", "acp", "Accounting period")
	if err != nil {
		return err
	}
	in, err := body[reasonRequest](w, r)
	if err != nil {
		return err
	}
	period, err := h.ledger.ReopenPeriod(r.Context(), p, id, in.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, period)
}
