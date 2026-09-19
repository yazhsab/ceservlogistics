package financeapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/settlement"
	"github.com/ceserve/courier-os/internal/tenant"
)

// ---------------------------------------------------------------------------
// M24 Settlement
// ---------------------------------------------------------------------------

func (h *Handler) settlementRoutes(r chi.Router) {
	r.Get("/", require("settlement.read", h.listSettlements))
	r.Post("/", require("settlement.calculate", h.generateSettlement))
	r.Get("/{settlementId}", require("settlement.read", h.getSettlement))
	r.Post("/{settlementId}/recalculate", require("settlement.calculate", h.recalculateSettlement))
	r.Post("/{settlementId}/submit", require("settlement.submit", h.submitSettlement))
	r.Post("/{settlementId}/approve", require("settlement.approve", h.approveSettlement))
	r.Post("/{settlementId}/payments", require("settlement.pay", h.paySettlement))
	r.Post("/{settlementId}/cancel", require("settlement.cancel", h.cancelSettlement))

	// Adjustments — the correction path for a statement that can no longer be
	// recalculated. Raising and deciding are separate permissions because they
	// are separate people (§26).
	r.Get("/{settlementId}/adjustments",
		require("settlement.read", h.listSettlementAdjustments))
	r.Post("/{settlementId}/adjustments",
		require("settlement.adjust_request", h.raiseSettlementAdjustment))
}

// AdjustmentRoutes mounts the decision half of the adjustment workflow.
//
// Mounted at /settlement-adjustments rather than under a settlement, because an
// approver works from a queue of everything awaiting them, not from one
// statement at a time.
func (h *Handler) adjustmentRoutes(r chi.Router) {
	r.Get("/", require("settlement.read", h.listAllSettlementAdjustments))
	r.Get("/{adjustmentId}", require("settlement.read", h.getSettlementAdjustment))
	r.Post("/{adjustmentId}/approve",
		require("settlement.adjust_approve", h.approveSettlementAdjustment))
	r.Post("/{adjustmentId}/reject",
		require("settlement.adjust_approve", h.rejectSettlementAdjustment))
}

type raiseAdjustmentRequest struct {
	AdjustmentType string `json:"adjustmentType"`
	AmountMinor    int64  `json:"amountMinor"`
	Reason         string `json:"reason"`
}

// raiseSettlementAdjustment records a correction. It moves no money.
func (h *Handler) raiseSettlementAdjustment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	settlementID, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	req, err := body[raiseAdjustmentRequest](w, r)
	if err != nil {
		return err
	}
	adj, err := h.settlement.RaiseAdjustment(r.Context(), p, settlement.AdjustmentRequest{
		SettlementID: settlementID, Type: req.AdjustmentType,
		AmountMinor: req.AmountMinor, Reason: req.Reason,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/settlement-adjustments/"+adj.PublicID,
		adjustmentView(*adj))
}

type adjustmentDecisionRequest struct {
	Comment string `json:"comment,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (h *Handler) approveSettlementAdjustment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "adjustmentId", "sad", "Settlement adjustment")
	if err != nil {
		return err
	}
	req, err := body[adjustmentDecisionRequest](w, r)
	if err != nil {
		return err
	}
	adj, err := h.settlement.ApproveAdjustment(r.Context(), p, id, req.Comment)
	if err != nil {
		return err
	}
	return httpx.OK(w, adjustmentView(*adj))
}

func (h *Handler) rejectSettlementAdjustment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "adjustmentId", "sad", "Settlement adjustment")
	if err != nil {
		return err
	}
	req, err := body[adjustmentDecisionRequest](w, r)
	if err != nil {
		return err
	}
	adj, err := h.settlement.RejectAdjustment(r.Context(), p, id, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, adjustmentView(*adj))
}

func (h *Handler) getSettlementAdjustment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "adjustmentId", "sad", "Settlement adjustment")
	if err != nil {
		return err
	}
	row, err := h.settlement.GetAdjustment(r.Context(), p, id)
	if err != nil {
		return err
	}
	out := map[string]any{
		"id": row.PublicID, "adjustmentType": row.AdjustmentType,
		"amountMinor": row.AmountMinor, "currency": row.Currency,
		"reason": row.Reason, "status": row.Status,
		"requestedBy": row.RequestedByName, "requestedAt": row.RequestedAt,
		"approvedBy": row.ApprovedByName, "approvedAt": row.ApprovedAt,
		"rejectionReason": row.RejectionReason,
		"createdAt":       row.CreatedAt,
	}
	// Present once the correction has posted its own journal, which only
	// happens against a settlement that was already frozen.
	if row.JournalTransactionID != nil {
		out["posted"] = true
	}
	return httpx.OK(w, out)
}

func (h *Handler) listSettlementAdjustments(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	settlementID, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	return h.renderAdjustments(w, r, p, settlementID)
}

func (h *Handler) listAllSettlementAdjustments(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	return h.renderAdjustments(w, r, p, httpx.Query(r, "settlementId"))
}

func (h *Handler) renderAdjustments(
	w http.ResponseWriter, r *http.Request, p *tenant.Principal, settlementID string,
) error {
	status, err := httpx.QueryEnum(r, "status", []string{
		settlement.AdjPending, settlement.AdjApproved,
		settlement.AdjRejected, settlement.AdjApplied,
	})
	if err != nil {
		return err
	}
	rows, err := h.settlement.ListAdjustments(r.Context(), p, settlementID, status, limit(r))
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		items = append(items, map[string]any{
			"id": a.PublicID, "settlementNumber": a.SettlementNumber,
			"settlementStatus": a.SettlementStatus,
			"adjustmentType":   a.AdjustmentType, "amountMinor": a.AmountMinor,
			"currency": a.Currency, "reason": a.Reason, "status": a.Status,
			"requestedBy": a.RequestedByName, "requestedAt": a.RequestedAt,
			"approvedBy": a.ApprovedByName, "approvedAt": a.ApprovedAt,
			"rejectionReason": a.RejectionReason,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func adjustmentView(a dbgen.SettlementAdjustment) map[string]any {
	return map[string]any{
		"id": a.PublicID, "adjustmentType": a.AdjustmentType,
		"amountMinor": a.AmountMinor, "currency": a.Currency,
		"reason": a.Reason, "status": a.Status,
		"requestedAt": a.RequestedAt, "approvedAt": a.ApprovedAt,
		"rejectionReason": a.RejectionReason,
		"posted":          a.JournalTransactionID != nil,
	}
}

type generateSettlementRequest struct {
	FranchiseID string `json:"franchiseId"`
	PeriodType  string `json:"periodType,omitempty"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
	Notes       string `json:"notes,omitempty"`
}

func (h *Handler) generateSettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[generateSettlementRequest](w, r)
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
	res, err := h.settlement.Generate(r.Context(), p, settlement.GenerateRequest{
		FranchiseID: in.FranchiseID, PeriodType: in.PeriodType,
		PeriodStart: start, PeriodEnd: end, Notes: in.Notes,
	})
	if err != nil {
		return err
	}
	// A replayed generation returns 200 rather than 201: nothing was created,
	// and a client retrying should not believe it made a second statement.
	header, err := h.settlementHeader(r, res.Settlement.PublicID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"settlement": header, "lines": projectRows(res.Lines, settlementLineResponse), "replayed": res.Replayed,
	}
	if res.Replayed {
		return httpx.OK(w, payload)
	}
	return httpx.Created(w, "/api/v1/settlements/"+res.Settlement.PublicID, payload)
}

func (h *Handler) recalculateSettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	res, err := h.settlement.Recalculate(r.Context(), p, id)
	if err != nil {
		return err
	}
	header, err := h.settlementHeader(r, res.Settlement.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"settlement": header, "lines": projectRows(res.Lines, settlementLineResponse)})
}

func (h *Handler) getSettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	res, err := h.settlement.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"settlement": settlementDetailResponse(res.Settlement), "lines": projectRows(res.Lines, settlementLineResponse), "byCategory": projectRows(res.ByCategory, settlementCategoryResponse), "approvals": projectRows(res.Approvals, settlementApprovalResponse), "payments": projectRows(res.Payments, settlementPaymentListResponse)})
}

func (h *Handler) listSettlements(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	rows, err := h.settlement.List(r.Context(), p,
		r.URL.Query().Get("status"), optInt64(r, "franchiseId"),
		optInt64(r, "cursor"), limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": projectRows(rows, settlementListResponse)})
}

type commentRequest struct {
	Comment string `json:"comment,omitempty"`
}

func (h *Handler) submitSettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	in, err := body[commentRequest](w, r)
	if err != nil {
		return err
	}
	stl, err := h.settlement.Submit(r.Context(), p, id, in.Comment)
	if err != nil {
		return err
	}
	header, err := h.settlementHeader(r, stl.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, header)
}

func (h *Handler) approveSettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	in, err := body[commentRequest](w, r)
	if err != nil {
		return err
	}
	stl, err := h.settlement.Approve(r.Context(), p, id, in.Comment)
	if err != nil {
		return err
	}
	header, err := h.settlementHeader(r, stl.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, header)
}

type settlementPaymentRequest struct {
	AmountMinor int64  `json:"amountMinor"`
	PaymentMode string `json:"paymentMode"`
	Reference   string `json:"reference,omitempty"`
	PaidOn      string `json:"paidOn,omitempty"`
}

func (h *Handler) paySettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	in, err := body[settlementPaymentRequest](w, r)
	if err != nil {
		return err
	}
	paidOn := time.Now().UTC()
	if in.PaidOn != "" {
		d, dErr := reqDate(in.PaidOn, "paidOn")
		if dErr != nil {
			return dErr
		}
		paidOn = d
	}
	payment, stl, err := h.settlement.Pay(r.Context(), p, settlement.PayRequest{
		SettlementID: id, AmountMinor: in.AmountMinor,
		PaymentMode: in.PaymentMode, Reference: in.Reference, PaidOn: paidOn,
	})
	if err != nil {
		return err
	}
	header, err := h.settlementHeader(r, stl.PublicID)
	if err != nil {
		return err
	}
	return httpx.Created(w, "", map[string]any{"payment": settlementPaymentResponse(*payment), "settlement": header})
}

func (h *Handler) cancelSettlement(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "settlementId", "stl", "Settlement")
	if err != nil {
		return err
	}
	in, err := body[reasonRequest](w, r)
	if err != nil {
		return err
	}
	stl, err := h.settlement.Cancel(r.Context(), p, id, in.Reason)
	if err != nil {
		return err
	}
	header, err := h.settlementHeader(r, stl.PublicID)
	if err != nil {
		return err
	}
	return httpx.OK(w, header)
}
