package financeapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/cod"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
)

// ---------------------------------------------------------------------------
// M23 COD
// ---------------------------------------------------------------------------

func (h *Handler) codRoutes(r chi.Router) {
	r.Get("/obligations", require("cod.read", h.listObligations))
	r.Get("/obligations/{obligationId}", require("cod.read", h.getObligation))
	r.Get("/summary", require("cod.read", h.codSummary))
	r.Get("/custody/{partyType}/{partyId}", require("cod.read", h.custodyPosition))

	r.Post("/collections", require("cod.collect", h.recordCollection))

	r.Post("/transfers", require("cod.transfer", h.declareTransfer))
	r.Post("/transfers/{transferId}/accept", require("cod.accept", h.acceptTransfer))

	r.Post("/reconciliations", require("cod.reconcile", h.openCODReconciliation))
	r.Post("/reconciliations/{reconciliationId}/count", require("cod.reconcile", h.recordCount))
	r.Post("/reconciliations/{reconciliationId}/complete", require("cod.reconcile", h.completeCODReconciliation))

	r.Post("/remittances", require("cod.remit", h.createRemittance))
	r.Post("/remittances/{remittanceId}/confirm", require("cod.remit", h.confirmRemittance))

	// Maker and checker are separate permissions so an organization can grant
	// them to different people; the row constraints then guarantee two
	// different people actually acted.
	r.Post("/adjustments", require("cod.adjust_request", h.requestCODAdjustment))
	r.Post("/adjustments/{adjustmentId}/approve", require("cod.adjust_approve", h.approveCODAdjustment))

	r.Post("/disputes", require("cod.dispute", h.raiseDispute))
	r.Post("/disputes/{disputeId}/resolve", require("cod.dispute", h.resolveDispute))
}

func (h *Handler) listObligations(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	rows, err := h.cod.ListObligations(r.Context(), p,
		r.URL.Query().Get("status"), r.URL.Query().Get("custodianType"),
		r.URL.Query().Get("awb"),
		optInt64(r, "custodianId"), optInt64(r, "franchiseId"), optInt64(r, "cursor"),
		limit(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": projectRows(rows, obligationListResponse)})
}

func (h *Handler) getObligation(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "obligationId", "cod", "COD obligation")
	if err != nil {
		return err
	}
	obligation, collections, err := h.cod.GetObligation(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"obligation": obligationDetailResponse(*obligation), "collections": projectRows(collections, codCollectionResponse)})
}

func (h *Handler) codSummary(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	summary, err := h.cod.Summary(r.Context(), p, optInt64(r, "franchiseId"))
	if err != nil {
		return err
	}
	return httpx.OK(w, summary)
}

// custodyPosition reports what a party holds, from the obligations and from the
// ledger, and says whether the two agree.
func (h *Handler) custodyPosition(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	partyType := chi.URLParam(r, "partyType")
	partyID, err := pathInt64(r, "partyId")
	if err != nil {
		return err
	}
	position, err := h.cod.CustodyPosition(r.Context(), p, partyType, partyID)
	if err != nil {
		return err
	}
	return httpx.OK(w, position)
}

type collectionRequest struct {
	ShipmentID      string `json:"shipmentId"`
	AmountMinor     int64  `json:"amountMinor"`
	PaymentMode     string `json:"paymentMode"`
	Reference       string `json:"reference,omitempty"`
	CollectedAt     string `json:"collectedAt,omitempty"`
	OperatingUnitID string `json:"operatingUnitId,omitempty"`
}

func (h *Handler) recordCollection(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[collectionRequest](w, r)
	if err != nil {
		return err
	}
	if in.ShipmentID == "" {
		return apierr.Validation("A collection needs a shipment.", nil)
	}

	shipmentID, err := h.cod.ResolveShipmentID(r.Context(), p, in.ShipmentID)
	if err != nil {
		return err
	}

	collectedAt := time.Now().UTC()
	if in.CollectedAt != "" {
		t, tErr := time.Parse(time.RFC3339, in.CollectedAt)
		if tErr != nil {
			return apierr.Validation("Invalid collectedAt.",
				map[string]any{"collectedAt": "expected RFC3339"})
		}
		collectedAt = t
	}

	var result *cod.CollectResult
	if err := h.txn(r, func(tx pgx.Tx) error {
		var cErr error
		result, cErr = h.cod.RecordCollection(r.Context(), tx, p, cod.CollectRequest{
			ShipmentID: shipmentID, AmountMinor: in.AmountMinor,
			PaymentMode: in.PaymentMode, Reference: in.Reference,
			CollectedAt: collectedAt,
			Device:      ops.DeviceFrom(r),
		})
		return cErr
	}); err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"obligation": obligationResponse(result.Obligation), "collection": codCollectionResponse(result.Collection),
		"duplicate": result.Duplicate,
	})
}

type transferRequest struct {
	FromType      string   `json:"fromType"`
	FromID        int64    `json:"fromId"`
	ToType        string   `json:"toType"`
	ToID          int64    `json:"toId"`
	ObligationIDs []string `json:"obligationIds"`
	Notes         string   `json:"notes,omitempty"`
}

func (h *Handler) declareTransfer(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[transferRequest](w, r)
	if err != nil {
		return err
	}
	transfer, items, err := h.cod.DeclareTransfer(r.Context(), p, cod.DeclareTransferRequest{
		FromType: in.FromType, FromID: in.FromID,
		ToType: in.ToType, ToID: in.ToID,
		ObligationIDs: in.ObligationIDs, Notes: in.Notes,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", map[string]any{"transfer": codTransferResponse(*transfer), "items": projectRows(items, codTransferItemResponse)})
}

type acceptRequest struct {
	AcceptedMinor  int64  `json:"acceptedMinor"`
	VarianceReason string `json:"varianceReason,omitempty"`
}

func (h *Handler) acceptTransfer(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "transferId", "cdt", "Custody transfer")
	if err != nil {
		return err
	}
	in, err := body[acceptRequest](w, r)
	if err != nil {
		return err
	}
	transfer, err := h.cod.AcceptTransfer(r.Context(), p, cod.AcceptTransferRequest{
		TransferID: id, AcceptedMinor: in.AcceptedMinor, VarianceReason: in.VarianceReason,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, codTransferResponse(*transfer))
}

type openReconRequest struct {
	PartyType   string `json:"partyType"`
	PartyID     int64  `json:"partyId"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
	Notes       string `json:"notes,omitempty"`
}

func (h *Handler) openCODReconciliation(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[openReconRequest](w, r)
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
	recon, expected, err := h.cod.OpenReconciliation(r.Context(), p, cod.OpenReconciliationRequest{
		PartyType: in.PartyType, PartyID: in.PartyID,
		PeriodStart: start, PeriodEnd: end, Notes: in.Notes,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", map[string]any{"reconciliation": codReconciliationResponse(*recon), "expected": projectRows(expected, obligationExpectedResponse)})
}

type countRequest struct {
	ObligationID string `json:"obligationId"`
	CountedMinor int64  `json:"countedMinor"`
	Notes        string `json:"notes,omitempty"`
}

func (h *Handler) recordCount(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "reconciliationId", "cdr", "COD reconciliation")
	if err != nil {
		return err
	}
	in, err := body[countRequest](w, r)
	if err != nil {
		return err
	}
	item, err := h.cod.RecordCount(r.Context(), p, cod.CountRequest{
		ReconciliationID: id, ObligationID: in.ObligationID,
		CountedMinor: in.CountedMinor, Notes: in.Notes,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, codCountResponse(*item))
}

type completeReconRequest struct {
	VarianceReason string `json:"varianceReason,omitempty"`
}

func (h *Handler) completeCODReconciliation(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "reconciliationId", "cdr", "COD reconciliation")
	if err != nil {
		return err
	}
	in, err := body[completeReconRequest](w, r)
	if err != nil {
		return err
	}
	recon, err := h.cod.CompleteReconciliation(r.Context(), p, cod.CompleteReconciliationRequest{
		ReconciliationID: id, VarianceReason: in.VarianceReason,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, codReconciliationResponse(*recon))
}

type remitRequest struct {
	FromType        string   `json:"fromType"`
	FromID          int64    `json:"fromId"`
	BeneficiaryType string   `json:"beneficiaryType"`
	BeneficiaryID   *int64   `json:"beneficiaryId,omitempty"`
	ObligationIDs   []string `json:"obligationIds"`
	PaymentMode     string   `json:"paymentMode"`
	Reference       string   `json:"reference,omitempty"`
	PaidOn          string   `json:"paidOn,omitempty"`
}

func (h *Handler) createRemittance(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[remitRequest](w, r)
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
	remittance, err := h.cod.CreateRemittance(r.Context(), p, cod.RemitRequest{
		FromType: in.FromType, FromID: in.FromID,
		BeneficiaryType: in.BeneficiaryType, BeneficiaryID: in.BeneficiaryID,
		ObligationIDs: in.ObligationIDs, PaymentMode: in.PaymentMode,
		Reference: in.Reference, PaidOn: paidOn,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", codRemittanceResponse(*remittance))
}

func (h *Handler) confirmRemittance(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "remittanceId", "cdm", "COD remittance")
	if err != nil {
		return err
	}
	remittance, err := h.cod.ConfirmRemittance(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, codRemittanceResponse(*remittance))
}

type codAdjustmentRequest struct {
	ObligationID    string `json:"obligationId,omitempty"`
	Type            string `json:"adjustmentType"`
	AmountMinor     int64  `json:"amountMinor"`
	LiablePartyType string `json:"liablePartyType,omitempty"`
	LiablePartyID   *int64 `json:"liablePartyId,omitempty"`
	Reason          string `json:"reason"`
}

func (h *Handler) requestCODAdjustment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[codAdjustmentRequest](w, r)
	if err != nil {
		return err
	}
	adj, err := h.cod.RequestAdjustment(r.Context(), p, cod.AdjustmentRequest{
		ObligationID: in.ObligationID, Type: in.Type, AmountMinor: in.AmountMinor,
		LiablePartyType: in.LiablePartyType, LiablePartyID: in.LiablePartyID,
		Reason: in.Reason,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", codAdjustmentResponse(*adj))
}

func (h *Handler) approveCODAdjustment(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "adjustmentId", "cda", "COD adjustment")
	if err != nil {
		return err
	}
	adj, err := h.cod.ApproveAdjustment(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, codAdjustmentResponse(*adj))
}

type disputeRequest struct {
	ObligationID  string `json:"obligationId"`
	RaisedByType  string `json:"raisedByType"`
	RaisedByID    *int64 `json:"raisedById,omitempty"`
	DisputedMinor int64  `json:"disputedMinor"`
	Category      string `json:"category"`
	Description   string `json:"description"`
}

func (h *Handler) raiseDispute(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	in, err := body[disputeRequest](w, r)
	if err != nil {
		return err
	}
	dispute, err := h.cod.RaiseDispute(r.Context(), p, cod.DisputeRequest{
		ObligationID: in.ObligationID, RaisedByType: in.RaisedByType,
		RaisedByID: in.RaisedByID, DisputedMinor: in.DisputedMinor,
		Category: in.Category, Description: in.Description,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", codDisputeResponse(*dispute))
}

type resolveDisputeRequest struct {
	Status        string `json:"status"`
	Resolution    string `json:"resolution"`
	ResolvedMinor *int64 `json:"resolvedMinor,omitempty"`
	AdjustmentID  string `json:"adjustmentId,omitempty"`
}

func (h *Handler) resolveDispute(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "disputeId", "cdd", "COD dispute")
	if err != nil {
		return err
	}
	in, err := body[resolveDisputeRequest](w, r)
	if err != nil {
		return err
	}
	dispute, err := h.cod.ResolveDispute(r.Context(), p, cod.ResolveDisputeRequest{
		DisputeID: id, Status: in.Status, Resolution: in.Resolution,
		ResolvedMinor: in.ResolvedMinor, AdjustmentID: in.AdjustmentID,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, codDisputeResponse(*dispute))
}
