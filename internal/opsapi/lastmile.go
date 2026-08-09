package opsapi

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/delivery"
	"github.com/ceserve/courier-os/internal/ndr"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/pod"
	"github.com/ceserve/courier-os/internal/rto"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Aliases so the bag facility action signature stays readable.
type contextT = context.Context
type principalT = *tenant.Principal

// ---------------------------------------------------------------------------
// M15, M16 Delivery
// ---------------------------------------------------------------------------

func (h *Handler) deliveryRunRoutes(r chi.Router) {
	r.Get("/", require("delivery.read", h.listDeliveryRuns))
	r.Post("/", require("delivery.manage", h.createDeliveryRun))
	r.Get("/{runId}", require("delivery.read", h.getDeliveryRun))
	r.Post("/{runId}/stops", require("delivery.manage", h.addDeliveryStops))
	r.Post("/{runId}/assign", require("delivery.assign", h.assignDeliveryRun))
	r.Post("/{runId}/dispatch", require("delivery.dispatch", h.dispatchDeliveryRun))
}

func (h *Handler) deliveryRoutes(r chi.Router) {
	r.Get("/queue", require("delivery.read", h.deliveryQueue))
	r.Post("/attempts", require("delivery.complete", h.recordAttempt))
	r.Post("/otp", require("delivery.otp_issue", h.issueOTP))
}

type createDeliveryRunRequest struct {
	BranchID string         `json:"branchId,omitempty"`
	AgentID  string         `json:"agentId"`
	RunDate  string         `json:"runDate"`
	Vehicle  string         `json:"vehicleReference,omitempty"`
	Barcodes []string       `json:"barcodes,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createDeliveryRun(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[createDeliveryRunRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("agentId", req.AgentID, publicid.PrefixUser, true)
	v.PublicID("branchId", req.BranchID, publicid.PrefixOperatingUnit, false)
	runDate := ops.ParseDate(v, "runDate", req.RunDate, true)
	for i, b := range req.Barcodes {
		ops.ValidBarcode(v, fieldIdx("barcodes", i), b, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.delivery.CreateRun(r.Context(), p, delivery.CreateRunInput{
		BranchID: req.BranchID, AgentID: req.AgentID, RunDate: runDate,
		Vehicle: req.Vehicle, Barcodes: req.Barcodes, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/delivery-runs/"+detail.ID, detail)
}

func (h *Handler) listDeliveryRuns(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := delivery.RunFilter{Status: httpx.Query(r, "status"), Cursor: cursor, Limit: limit}
	if d := httpx.Query(r, "runDate"); d != "" {
		v := validator()
		parsed := ops.ParseDate(v, "runDate", d, true)
		if err := v.Err(); err != nil {
			return err
		}
		f.RunDate = &parsed
	}
	// An agent asking for "mine" is the common case on the field app.
	if httpx.Query(r, "mine") == "true" {
		id := p.UserID
		f.AgentID = &id
	}
	page, err := h.delivery.ListRuns(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getDeliveryRun(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "runId", publicid.PrefixDeliveryRun, "Delivery run")
	if err != nil {
		return err
	}
	detail, err := h.delivery.GetRun(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type addStopsRequest struct {
	Barcodes []string `json:"barcodes"`
}

func (h *Handler) addDeliveryStops(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "runId", publicid.PrefixDeliveryRun, "Delivery run")
	if err != nil {
		return err
	}
	req, err := body[addStopsRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcodes := ops.Barcodes(v, "barcodes", req.Barcodes, 300)
	if err := v.Err(); err != nil {
		return err
	}
	detail, results, err := h.delivery.AddStops(r.Context(), p, delivery.AddStopsInput{
		RunID: id, Barcodes: barcodes,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"run": detail, "results": results})
}

type assignRunRequest struct {
	AgentID string `json:"agentId"`
}

func (h *Handler) assignDeliveryRun(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "runId", publicid.PrefixDeliveryRun, "Delivery run")
	if err != nil {
		return err
	}
	req, err := body[assignRunRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("agentId", req.AgentID, publicid.PrefixUser, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.delivery.Assign(r.Context(), p, id, req.AgentID)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) dispatchDeliveryRun(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "runId", publicid.PrefixDeliveryRun, "Delivery run")
	if err != nil {
		return err
	}
	detail, err := h.delivery.Dispatch(r.Context(), p, id, ops.DeviceFrom(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) deliveryQueue(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	limit, _, err := ops.Page(r, 50)
	if err != nil {
		return err
	}
	includeHeld, err := httpx.QueryBool(r, "includeHeld", false)
	if err != nil {
		return err
	}
	excludeAssigned, err := httpx.QueryBool(r, "excludeAssigned", true)
	if err != nil {
		return err
	}
	items, err := h.delivery.Queue(r.Context(), rc.Principal, rc.Facility, delivery.QueueFilter{
		Status: httpx.Query(r, "status"), IncludeHeld: includeHeld,
		ExcludeAssigned: excludeAssigned, Limit: limit,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"facility": ops.RefOf(rc.Facility), "data": items})
}

type attemptRequest struct {
	Barcode      string   `json:"barcode"`
	Outcome      string   `json:"outcome"`
	RunID        string   `json:"runId,omitempty"`
	Recipient    string   `json:"recipientName,omitempty"`
	Relationship string   `json:"recipientRelationship,omitempty"`
	Phone        string   `json:"recipientPhone,omitempty"`
	OTP          string   `json:"otp,omitempty"`
	CODCollected int64    `json:"codCollectedMinor,omitempty"`
	CODMode      string   `json:"codPaymentMode,omitempty"`
	CODReference string   `json:"codReference,omitempty"`
	FailureCode  string   `json:"failureReasonCode,omitempty"`
	Remarks      string   `json:"remarks,omitempty"`
	OccurredAt   string   `json:"occurredAt,omitempty"`
	NextAttempt  string   `json:"nextAttemptAt,omitempty"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	Accuracy     *int     `json:"locationAccuracyM,omitempty"`
}

func (h *Handler) recordAttempt(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	req, err := body[attemptRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", req.Barcode, true)
	outcome := v.Enum("outcome", req.Outcome,
		[]string{"DELIVERED", "FAILED", "RESCHEDULED", "CANCELLED"}, true)
	if outcome == "DELIVERED" {
		v.Text("recipientName", req.Recipient, 2, 120, true)
		v.Enum("recipientRelationship", req.Relationship,
			[]string{"SELF", "FAMILY", "NEIGHBOUR", "SECURITY", "RECEPTION", "COLLEAGUE", "OTHER"}, false)
	}
	if outcome == "FAILED" || outcome == "RESCHEDULED" {
		v.Required("failureReasonCode", req.FailureCode)
	}
	if req.CODCollected != 0 {
		v.NonNegativeMinor("codCollectedMinor", req.CODCollected)
		v.Enum("codPaymentMode", req.CODMode, delivery.CODPaymentModes, true)
	}
	if req.OTP != "" && (len(req.OTP) < 4 || len(req.OTP) > 8) {
		v.Add("otp", "must be between 4 and 8 digits")
	}
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	occurred := ops.ParseTime(v, "occurredAt", req.OccurredAt)
	next := ops.ParseTime(v, "nextAttemptAt", req.NextAttempt)
	v.PublicID("runId", req.RunID, publicid.PrefixDeliveryRun, false)
	if err := v.Err(); err != nil {
		return err
	}
	// A delivery must be replay-safe: either an idempotency key or a device
	// event id has to be present.
	if _, err := ops.RequireIdempotencyKey(r, rc.Device); err != nil {
		return err
	}

	result, err := h.delivery.RecordAttempt(r.Context(), rc.Principal, delivery.AttemptInput{
		Barcode: barcode, Outcome: outcome, RunID: req.RunID, Facility: rc.Facility,
		Device: rc.Device, Latitude: req.Latitude, Longitude: req.Longitude, Accuracy: req.Accuracy,
		RecipientName: req.Recipient, RecipientRelationship: req.Relationship,
		RecipientPhone: req.Phone, OTP: req.OTP,
		CODCollectedMinor: req.CODCollected, CODPaymentMode: req.CODMode,
		CODReference: req.CODReference, FailureReasonCode: req.FailureCode,
		Remarks: req.Remarks, OccurredAt: occurred, NextAttemptAt: next,
	})
	if err != nil {
		return err
	}
	if result.Replayed {
		w.Header().Set("Idempotent-Replay", "true")
	}
	return httpx.OK(w, result)
}

type issueOTPRequest struct {
	Barcode string `json:"barcode"`
}

func (h *Handler) issueOTP(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[issueOTPRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", req.Barcode, true)
	if err := v.Err(); err != nil {
		return err
	}
	code, masked, expires, err := h.delivery.IssueOTP(r.Context(), p, barcode)
	if err != nil {
		return err
	}
	// The code is returned once, to the caller who will send it. It is stored
	// only as a digest and can never be read back.
	return httpx.OK(w, map[string]any{
		"otp": code, "sentToMasked": masked, "expiresAt": expires,
		"note": "This code is shown once and cannot be retrieved again.",
	})
}

// ---------------------------------------------------------------------------
// M17 NDR
// ---------------------------------------------------------------------------

func (h *Handler) ndrRoutes(r chi.Router) {
	r.Get("/", require("ndr.read", h.listNDRCases))
	r.Get("/reasons", require("ndr.read", h.listNDRReasons))
	r.Post("/reasons", require("ndr.config", h.createNDRReason))
	r.Patch("/reasons/{reasonId}", require("ndr.config", h.updateNDRReason))
	r.Get("/{caseId}", require("ndr.read", h.getNDRCase))
	r.Post("/{caseId}/action", require("ndr.manage", h.setNDRAction))
}

func (h *Handler) listNDRCases(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := ndr.CaseFilter{
		Status: httpx.Query(r, "status"), ReasonCode: httpx.Query(r, "reasonCode"),
		Action: httpx.Query(r, "action"), Cursor: cursor, Limit: limit,
	}
	if u := httpx.Query(r, "branchId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.BranchID = &facility.ID
	}
	page, err := h.ndr.ListCases(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getNDRCase(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "caseId", publicid.PrefixNDRCase, "NDR case")
	if err != nil {
		return err
	}
	detail, err := h.ndr.GetCase(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type ndrActionRequest struct {
	Action       string `json:"action"`
	RequestedBy  string `json:"requestedBy,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	ScheduledFor string `json:"scheduledFor,omitempty"`
	ContactName  string `json:"contactName,omitempty"`
	ContactPhone string `json:"contactPhone,omitempty"`
	ContactNotes string `json:"contactNotes,omitempty"`
	Line1        string `json:"correctedLine1,omitempty"`
	Line2        string `json:"correctedLine2,omitempty"`
	Landmark     string `json:"correctedLandmark,omitempty"`
	Pincode      string `json:"correctedPincode,omitempty"`
	Phone        string `json:"correctedPhone,omitempty"`
	AssignTo     string `json:"assignToUserId,omitempty"`
}

func (h *Handler) setNDRAction(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "caseId", publicid.PrefixNDRCase, "NDR case")
	if err != nil {
		return err
	}
	req, err := body[ndrActionRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	action := v.Enum("action", req.Action, ndr.Actions, true)
	v.Enum("requestedBy", req.RequestedBy,
		[]string{"OPERATIONS", "CUSTOMER", "CONSIGNEE", "SYSTEM", "PARTNER"}, false)
	scheduled := ops.ParseTime(v, "scheduledFor", req.ScheduledFor)
	if req.Pincode != "" {
		v.Pincode("correctedPincode", req.Pincode)
	}
	if req.Phone != "" {
		v.Phone("correctedPhone", req.Phone, false)
	}
	v.PublicID("assignToUserId", req.AssignTo, publicid.PrefixUser, false)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.ndr.SetAction(r.Context(), p, ndr.SetActionInput{
		CaseID: id, Action: action, RequestedBy: req.RequestedBy,
		Instructions: req.Instructions, ScheduledFor: scheduled,
		ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		ContactNotes: req.ContactNotes,
		Line1:        req.Line1, Line2: req.Line2, Landmark: req.Landmark,
		Pincode: req.Pincode, Phone: req.Phone, AssignTo: req.AssignTo,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) listNDRReasons(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	activeOnly, err := httpx.QueryBool(r, "activeOnly", true)
	if err != nil {
		return err
	}
	reasons, err := h.ndr.ListReasons(r.Context(), p, activeOnly)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": reasons})
}

type ndrReasonRequest struct {
	Code            string `json:"code,omitempty"`
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	Category        string `json:"category,omitempty"`
	DefaultAction   string `json:"defaultAction,omitempty"`
	MaxAttempts     int    `json:"maxAttempts,omitempty"`
	CustomerFault   bool   `json:"isCustomerFault,omitempty"`
	NeedsEvidence   bool   `json:"requiresEvidence,omitempty"`
	AutoRTOAfterMax bool   `json:"autoRtoAfterMax,omitempty"`
	DisplayOrder    int    `json:"displayOrder,omitempty"`
	IsActive        *bool  `json:"isActive,omitempty"`
}

var ndrCategories = []string{
	"CUSTOMER_UNAVAILABLE", "ADDRESS_PROBLEM", "REFUSED", "PAYMENT", "ACCESS",
	"OPERATIONAL", "WEATHER", "DAMAGE", "OTHER",
}

func (h *Handler) createNDRReason(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[ndrReasonRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	category := v.Enum("category", req.Category, ndrCategories, true)
	action := v.Enum("defaultAction", req.DefaultAction, ndr.Actions, true)
	v.IntRange("maxAttempts", defaultInt(req.MaxAttempts, 3), 1, 10)
	if err := v.Err(); err != nil {
		return err
	}
	reason, err := h.ndr.CreateReason(r.Context(), p, ndr.UpsertReasonInput{
		Code: code, Name: name, Description: req.Description, Category: category,
		DefaultAction: action, MaxAttempts: defaultInt(req.MaxAttempts, 3),
		CustomerFault: req.CustomerFault, NeedsEvidence: req.NeedsEvidence,
		AutoRTOAfterMax: req.AutoRTOAfterMax, DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "", reason)
}

func (h *Handler) updateNDRReason(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "reasonId", publicid.PrefixNDRReason, "NDR reason")
	if err != nil {
		return err
	}
	req, err := body[ndrReasonRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.Enum("category", req.Category, ndrCategories, false)
	v.Enum("defaultAction", req.DefaultAction, ndr.Actions, false)
	if req.MaxAttempts != 0 {
		v.IntRange("maxAttempts", req.MaxAttempts, 1, 10)
	}
	if err := v.Err(); err != nil {
		return err
	}
	reason, err := h.ndr.UpdateReason(r.Context(), p, ndr.UpsertReasonInput{
		ID: id, Name: req.Name, Description: req.Description, Category: req.Category,
		DefaultAction: req.DefaultAction, MaxAttempts: req.MaxAttempts,
		IsActive: req.IsActive,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, reason)
}

// ---------------------------------------------------------------------------
// M18 RTO
// ---------------------------------------------------------------------------

func (h *Handler) rtoRoutes(r chi.Router) {
	r.Get("/", require("rto.read", h.listRTOCases))
	r.Post("/", require("rto.manage", h.initiateRTO))
	r.Get("/{caseId}", require("rto.read", h.getRTOCase))
	r.Post("/{caseId}/dispatch", require("rto.manage", h.dispatchRTO))
	r.Post("/{caseId}/complete", require("rto.manage", h.completeRTO))
	r.Post("/receive", require("rto.manage", h.receiveRTO))
}

type initiateRTORequest struct {
	Barcode    string `json:"barcode"`
	ReasonCode string `json:"reasonCode,omitempty"`
	Notes      string `json:"notes,omitempty"`
}

func (h *Handler) initiateRTO(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[initiateRTORequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", req.Barcode, true)
	notes := v.Text("notes", req.Notes, 5, 500, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.rto.InitiateStandalone(r.Context(), p, barcode, req.ReasonCode, notes)
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/rto/"+detail.ID, detail)
}

func (h *Handler) listRTOCases(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	page, err := h.rto.ListCases(r.Context(), p, rto.CaseFilter{
		Status: httpx.Query(r, "status"), Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getRTOCase(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "caseId", publicid.PrefixRTOCase, "RTO case")
	if err != nil {
		return err
	}
	detail, err := h.rto.GetCase(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) dispatchRTO(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	id, err := pathID(r, "caseId", publicid.PrefixRTOCase, "RTO case")
	if err != nil {
		return err
	}
	detail, err := h.rto.Dispatch(r.Context(), rc.Principal, id, rc.Facility, rc.Device)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type receiveRTORequest struct {
	Barcode string `json:"barcode"`
}

func (h *Handler) receiveRTO(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	req, err := body[receiveRTORequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", req.Barcode, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.rto.ReceiveAtFacility(r.Context(), rc.Principal, barcode, rc.Facility, rc.Device)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type completeRTORequest struct {
	Outcome    string `json:"outcome"`
	ReceivedBy string `json:"receivedBy,omitempty"`
	Remarks    string `json:"remarks,omitempty"`
}

func (h *Handler) completeRTO(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	id, err := pathID(r, "caseId", publicid.PrefixRTOCase, "RTO case")
	if err != nil {
		return err
	}
	req, err := body[completeRTORequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	outcome := v.Enum("outcome", req.Outcome, []string{"RETURNED", "FAILED"}, true)
	if outcome == "RETURNED" {
		v.Text("receivedBy", req.ReceivedBy, 2, 120, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.rto.Complete(r.Context(), rc.Principal, rto.CompleteInput{
		CaseID: id, Outcome: outcome, ReceivedBy: req.ReceivedBy,
		Remarks: req.Remarks, Facility: rc.Facility, Device: rc.Device,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---------------------------------------------------------------------------
// M19 POD
// ---------------------------------------------------------------------------

func (h *Handler) podRoutes(r chi.Router) {
	r.Post("/", require("pod.submit", h.submitPOD))
	r.Get("/{podId}", require("pod.read", h.getPOD))
	r.Get("/{podId}/artifacts/{artifactId}/download", require("pod.read", h.downloadArtifact))
}

// submitPOD accepts multipart/form-data: the evidence is binary, so JSON would
// mean base64 and a third more bytes over a mobile connection.
func (h *Handler) submitPOD(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return apierr.New(http.StatusUnsupportedMediaType, apierr.CodeUnsupportedMedia,
			"Submit proof of delivery as multipart/form-data.")
	}
	// Bound what is buffered in memory; larger parts spill to a temp file.
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge,
			"The upload is larger than this endpoint accepts.")
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	form := r.MultipartForm.Value
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", first(form["barcode"]), true)
	podType := v.Enum("podType", first(form["podType"]), pod.PODTypes, false)
	recipient := v.Text("recipientName", first(form["recipientName"]), 2, 120, true)
	relationship := v.Enum("recipientRelationship", first(form["recipientRelationship"]),
		[]string{"SELF", "FAMILY", "NEIGHBOUR", "SECURITY", "RECEPTION", "COLLEAGUE", "OTHER"}, false)
	lat := parseFloatPtr(v, "latitude", first(form["latitude"]))
	lng := parseFloatPtr(v, "longitude", first(form["longitude"]))
	v.Latitude("latitude", lat)
	v.Longitude("longitude", lng)
	deliveredAt := ops.ParseTime(v, "deliveredAt", first(form["deliveredAt"]))

	uploads := make([]pod.ArtifactUpload, 0, 4)
	for _, kind := range pod.ArtifactTypes {
		field := strings.ToLower(kind)
		for _, fh := range r.MultipartForm.File[field] {
			uploads = append(uploads, pod.ArtifactUpload{
				Type: kind, Caption: first(form[field+"Caption"]), Header: fh,
			})
		}
	}
	if len(uploads) == 0 {
		v.Add("artifacts", "at least one signature or photo is required as evidence")
	}
	if err := v.Err(); err != nil {
		return err
	}

	detail, err := h.pod.Submit(r.Context(), rc.Principal, pod.SubmitInput{
		Barcode: barcode, PODType: podType, Recipient: recipient,
		Relationship: relationship, Phone: first(form["recipientPhone"]),
		IDType: first(form["recipientIdType"]), IDNumber: first(form["recipientIdNumber"]),
		Latitude: lat, Longitude: lng, Remarks: first(form["remarks"]),
		DeliveredAt: deliveredAt, Facility: rc.Facility, Device: rc.Device,
		Artifacts: uploads,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/pod/"+detail.ID, detail)
}

func (h *Handler) getPOD(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "podId", publicid.PrefixProofOfDelivery, "Proof of delivery")
	if err != nil {
		return err
	}
	detail, err := h.pod.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) downloadArtifact(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	podID, err := pathID(r, "podId", publicid.PrefixProofOfDelivery, "Proof of delivery")
	if err != nil {
		return err
	}
	artifactID, err := pathID(r, "artifactId", publicid.PrefixPODArtifact, "POD artifact")
	if err != nil {
		return err
	}
	url, rc, mime, err := h.pod.Download(r.Context(), p, podID, artifactID)
	if err != nil {
		return err
	}
	if url != "" {
		// A redirect to a short-lived signed URL keeps the bytes off the API,
		// which matters on a 4-vCPU box serving photos.
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
		return nil
	}
	defer rc.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
	return nil
}

// ---------------------------------------------------------------------------
// M20 Public tracking
// ---------------------------------------------------------------------------

func (h *Handler) track(w http.ResponseWriter, r *http.Request) error {
	awb, err := httpx.PathParam(r, "awb", 32)
	if err != nil {
		return err
	}
	result, err := h.tracking.Track(r.Context(), awb)
	if err != nil {
		return err
	}
	// A short public cache: the same parcel is polled repeatedly by the same
	// person watching a delivery.
	w.Header().Set("Cache-Control", "public, max-age=60")
	return httpx.OK(w, result)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func parseFloatPtr(v *validate.Validator, field, raw string) *float64 {
	if raw == "" {
		return nil
	}
	var f float64
	var neg bool
	i := 0
	if raw[0] == '-' {
		neg, i = true, 1
	}
	seenDot, frac := false, 1.0
	for ; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '.' && !seenDot:
			seenDot = true
		case c >= '0' && c <= '9':
			if seenDot {
				frac /= 10
				f += float64(c-'0') * frac
			} else {
				f = f*10 + float64(c-'0')
			}
		default:
			v.Add(field, "must be a number")
			return nil
		}
	}
	if neg {
		f = -f
	}
	return &f
}
