package financeapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/billing"
	"github.com/ceserve/courier-os/internal/platform/httpx"
)

// ---------------------------------------------------------------------------
// M25 Billing
// ---------------------------------------------------------------------------

func (h *Handler) invoiceRoutes(r chi.Router) {
	r.Get("/", require("invoice.read", h.listInvoices))
	r.Post("/", require("invoice.create", h.draftInvoice))
	r.Get("/{invoiceId}", require("invoice.read", h.getInvoice))
	r.Get("/{invoiceId}/credit-notes", require("invoice.read", h.listInvoiceCreditNotes))
	r.Post("/{invoiceId}/issue", require("invoice.issue", h.issueInvoice))
	r.Post("/{invoiceId}/payments", require("invoice.payment", h.payInvoice))
	r.Get("/outstanding/{customerId}", require("invoice.read", h.customerOutstanding))
}

func (h *Handler) creditNoteRoutes(r chi.Router) {
	r.Post("/", require("creditnote.create", h.raiseCreditNote))
	// Issuing is the checker half: it approves, numbers and posts in one act,
	// and the service refuses it if the same person raised the note.
	r.Post("/{noteId}/issue", require("creditnote.approve", h.issueCreditNote))
}

type draftInvoiceRequest struct {
	CustomerID  string `json:"customerId"`
	BillingMode string `json:"billingMode,omitempty"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
	IssueDate   string `json:"issueDate,omitempty"`
	Summary     bool   `json:"summary,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Taxes       []struct {
		Code     string         `json:"code"`
		Name     string         `json:"name"`
		RateBp   int32          `json:"rateBp"`
		Metadata map[string]any `json:"metadata,omitempty"`
	} `json:"taxes,omitempty"`
}

func (h *Handler) listInvoiceCreditNotes(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "invoiceId", "inv", "Invoice")
	if err != nil {
		return err
	}
	n := limit(r)
	rows, invoiceNumber, err := h.billing.ListInvoiceCreditNotes(r.Context(), p, id, r.URL.Query().Get("cursor"), n)
	if err != nil {
		return err
	}
	hasMore := len(rows) > int(n)
	if hasMore {
		rows = rows[:n]
	}
	var nextCursor string
	if hasMore && len(rows) > 0 {
		nextCursor = rows[len(rows)-1].PublicID
	}
	data := make([]creditNoteView, 0, len(rows))
	for _, row := range rows {
		data = append(data, creditNoteListResponse(row, invoiceNumber))
	}
	return httpx.OK(w, map[string]any{"data": data, "pagination": map[string]any{"hasMore": hasMore, "nextCursor": nextCursor}})
}

func (h *Handler) draftInvoice(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[draftInvoiceRequest](w, r)
	if err != nil {
		return err
	}
	start, err := reqDate(in.PeriodStart, "periodStart")
	if err != nil {
		return err
	}
	end, err := reqDate(in.PeriodEnd, "periodEnd")
	if err != nil {
		return err
	}
	issueDate := time.Now().UTC()
	if in.IssueDate != "" {
		d, dErr := reqDate(in.IssueDate, "issueDate")
		if dErr != nil {
			return dErr
		}
		issueDate = d
	}

	taxes := make([]billing.TaxComponentSpec, 0, len(in.Taxes))
	for _, t := range in.Taxes {
		taxes = append(taxes, billing.TaxComponentSpec{
			Code: t.Code, Name: t.Name, RateBp: t.RateBp, Metadata: t.Metadata,
		})
	}

	res, err := h.billing.Draft(r.Context(), p, billing.DraftRequest{
		CustomerID: in.CustomerID, BillingMode: in.BillingMode,
		PeriodStart: start, PeriodEnd: end, IssueDate: issueDate,
		Summary: in.Summary, Taxes: taxes, Notes: in.Notes,
	})
	if err != nil {
		return err
	}
	header, err := h.invoiceHeader(r, res.Invoice.PublicID)
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/invoices/"+res.Invoice.PublicID, map[string]any{
		"invoice": header, "lines": projectRows(res.Lines, invoiceLineResponse), "taxes": projectRows(res.Taxes, invoiceTaxResponse),
	})
}

func (h *Handler) issueInvoice(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "invoiceId", "inv", "Invoice")
	if err != nil {
		return err
	}
	invoice, err := h.billing.Issue(r.Context(), p, id)
	if err != nil {
		return err
	}
	header, err := h.invoiceHeader(r, invoice.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, header)
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "invoiceId", "inv", "Invoice")
	if err != nil {
		return err
	}
	res, err := h.billing.GetInvoice(r.Context(), p, id)
	if err != nil {
		return err
	}
	header, err := h.invoiceHeader(r, res.Invoice.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"invoice": header, "lines": projectRows(res.Lines, invoiceLineResponse), "taxes": projectRows(res.Taxes, invoiceTaxResponse),
	})
}

func (h *Handler) listInvoices(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	rows, err := h.billing.ListInvoices(r.Context(), p,
		r.URL.Query().Get("status"), optInt64(r, "customerId"),
		optInt64(r, "cursor"), limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": projectRows(rows, invoiceListResponse)})
}

type invoicePaymentRequest struct {
	AmountMinor int64  `json:"amountMinor"`
	PaymentMode string `json:"paymentMode"`
	Reference   string `json:"reference,omitempty"`
	ReceivedOn  string `json:"receivedOn,omitempty"`
}

func (h *Handler) payInvoice(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "invoiceId", "inv", "Invoice")
	if err != nil {
		return err
	}
	in, err := body[invoicePaymentRequest](w, r)
	if err != nil {
		return err
	}
	receivedOn := time.Now().UTC()
	if in.ReceivedOn != "" {
		d, dErr := reqDate(in.ReceivedOn, "receivedOn")
		if dErr != nil {
			return dErr
		}
		receivedOn = d
	}
	payment, invoice, err := h.billing.RecordPayment(r.Context(), p, billing.PayRequest{
		InvoiceID: id, AmountMinor: in.AmountMinor,
		PaymentMode: in.PaymentMode, Reference: in.Reference, ReceivedOn: receivedOn,
	})
	if err != nil {
		return err
	}
	header, err := h.invoiceHeader(r, invoice.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"payment": invoicePaymentResponse(*payment), "invoice": header})
}

func (h *Handler) customerOutstanding(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "customerId", "cus", "Customer")
	if err != nil {
		return err
	}
	out, err := h.billing.CustomerOutstanding(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type creditNoteRequest struct {
	InvoiceID   string `json:"invoiceId"`
	NoteType    string `json:"noteType,omitempty"`
	ReasonCode  string `json:"reasonCode"`
	Reason      string `json:"reason"`
	AmountMinor int64  `json:"amountMinor"`
	TaxMinor    int64  `json:"taxMinor,omitempty"`
	IssueDate   string `json:"issueDate,omitempty"`
}

func (h *Handler) raiseCreditNote(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[creditNoteRequest](w, r)
	if err != nil {
		return err
	}
	issueDate := time.Now().UTC()
	if in.IssueDate != "" {
		d, dErr := reqDate(in.IssueDate, "issueDate")
		if dErr != nil {
			return dErr
		}
		issueDate = d
	}
	note, err := h.billing.RaiseCreditNote(r.Context(), p, billing.CreditNoteRequest{
		InvoiceID: in.InvoiceID, NoteType: in.NoteType,
		ReasonCode: in.ReasonCode, Reason: in.Reason,
		AmountMinor: in.AmountMinor, TaxMinor: in.TaxMinor, IssueDate: issueDate,
	})
	if err != nil {
		return err
	}
	header, err := h.creditNoteHeader(r, note.PublicID)
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/credit-notes/"+note.PublicID, header)
}

func (h *Handler) issueCreditNote(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "noteId", "crn", "Credit note")
	if err != nil {
		return err
	}
	note, err := h.billing.IssueCreditNote(r.Context(), p, id)
	if err != nil {
		return err
	}
	header, err := h.creditNoteHeader(r, note.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, header)
}
