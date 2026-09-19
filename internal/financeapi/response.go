package financeapi

import (
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"net/http"
	"time"
)

func projectRows[T, U any](rows []T, project func(T) U) []U {
	out := make([]U, 0, len(rows))
	for _, row := range rows {
		out = append(out, project(row))
	}
	return out
}
func dateOnly(value *time.Time) *string {
	if value == nil {
		return nil
	}
	date := value.Format("2006-01-02")
	return &date
}

// Mutations return base rows. Read the tenant-scoped joined header to give the
// same public shape as the register and include human-readable party names.
func (h *Handler) settlementHeader(r *http.Request, id string) (settlementView, error) {
	p, err := principal(r)
	if err != nil {
		return settlementView{}, err
	}
	row, err := dbgen.New(h.ledger.DB().Pool).GetSettlementByPublicID(r.Context(), dbgen.GetSettlementByPublicIDParams{OrganizationID: p.OrganizationID, PublicID: id})
	if err != nil {
		return settlementView{}, ops.NotFoundOr(err, "Settlement")
	}
	return settlementDetailResponse(row), nil
}
func (h *Handler) invoiceHeader(r *http.Request, id string) (invoiceView, error) {
	p, err := principal(r)
	if err != nil {
		return invoiceView{}, err
	}
	row, err := dbgen.New(h.ledger.DB().Pool).GetInvoiceByPublicID(r.Context(), dbgen.GetInvoiceByPublicIDParams{OrganizationID: p.OrganizationID, PublicID: id})
	if err != nil {
		return invoiceView{}, ops.NotFoundOr(err, "Invoice")
	}
	return invoiceDetailResponse(row), nil
}
func (h *Handler) creditNoteHeader(r *http.Request, id string) (creditNoteView, error) {
	p, err := principal(r)
	if err != nil {
		return creditNoteView{}, err
	}
	row, err := dbgen.New(h.ledger.DB().Pool).GetCreditNoteByPublicID(r.Context(), dbgen.GetCreditNoteByPublicIDParams{OrganizationID: p.OrganizationID, PublicID: id})
	if err != nil {
		return creditNoteView{}, ops.NotFoundOr(err, "Credit note")
	}
	return creditNoteResponse(row), nil
}
