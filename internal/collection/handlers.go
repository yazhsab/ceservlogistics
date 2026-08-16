package collection

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/tenant"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.With(require(PermRead)).Get("/", httpx.Wrap(h.list))
	r.With(require(PermRecord)).Post("/", httpx.Wrap(h.record))
}

func require(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := tenant.Require(r)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			if err = p.Require(permission); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type recordRequest struct {
	ShipmentID  string `json:"shipmentId"`
	AmountMinor int64  `json:"amountMinor"`
	PaymentMode string `json:"paymentMode"`
	Reference   string `json:"reference,omitempty"`
	CollectedAt string `json:"collectedAt,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

func (h *Handler) record(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var in recordRequest
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	when := time.Now().UTC()
	if in.CollectedAt != "" {
		parsed, err := time.Parse(time.RFC3339, in.CollectedAt)
		if err != nil {
			return apierr.Validation("collectedAt must be RFC3339.", nil)
		}
		when = parsed
	}
	row, replayed, err := h.svc.Record(r.Context(), p, RecordRequest{ShipmentID: in.ShipmentID, AmountMinor: in.AmountMinor, PaymentMode: in.PaymentMode, Reference: in.Reference, CollectedAt: when, Notes: in.Notes})
	if err != nil {
		return err
	}
	view := map[string]any{
		"publicId": row.PublicID, "shipmentId": row.ShipmentID,
		"franchiseId": row.FranchiseID, "operatingUnitId": row.OperatingUnitID,
		"amountMinor": row.AmountMinor, "currency": row.Currency,
		"paymentMode": row.PaymentMode, "reference": row.Reference,
		"status": row.Status, "collectedAt": row.CollectedAt, "settlementId": row.SettlementID,
	}
	return httpx.Created(w, "/api/v1/franchise-collections/"+row.PublicID, map[string]any{"collection": view, "replayed": replayed})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 32)
	limit64, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 32)
	if limit64 <= 0 || limit64 > 100 {
		limit64 = 50
	}
	rows, err := h.svc.List(r.Context(), p, r.URL.Query().Get("status"), int32(offset), int32(limit64))
	if err != nil {
		return err
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"publicId": row.PublicID, "shipmentId": row.ShipmentID, "awb": row.Awb,
			"franchiseId": row.FranchiseID, "franchiseCode": row.FranchiseCode,
			"franchiseName": row.FranchiseName, "operatingUnitId": row.OperatingUnitID,
			"unitCode": row.UnitCode, "amountMinor": row.AmountMinor, "currency": row.Currency,
			"paymentMode": row.PaymentMode, "reference": row.Reference, "status": row.Status,
			"collectedAt": row.CollectedAt, "collectedByName": row.CollectedByName,
			"settlementId": row.SettlementID, "remittedAt": row.RemittedAt, "notes": row.Notes,
		})
	}
	return httpx.OK(w, map[string]any{"data": data})
}
