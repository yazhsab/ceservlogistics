package opsapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/pickup"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/scanning"
	"github.com/ceserve/courier-os/internal/tenant"
)

func tenantPrincipal(r *http.Request) (*tenant.Principal, error) { return tenant.Require(r) }

// ---------------------------------------------------------------------------
// M09 Pickup
// ---------------------------------------------------------------------------

func (h *Handler) pickupRoutes(r chi.Router) {
	r.Get("/", require("pickup.read", h.listPickups))
	r.Post("/", require("pickup.create", h.createPickup))
	r.Get("/my-stops", require("pickup.read", h.agentStops))
	r.Get("/{pickupId}", require("pickup.read", h.getPickup))
	r.Post("/{pickupId}/assign", require("pickup.assign", h.assignPickup))
	r.Post("/{pickupId}/complete", require("pickup.complete", h.completePickup))
	r.Post("/{pickupId}/cancel", require("pickup.cancel", h.cancelPickup))
	r.Post("/assignments/{assignmentId}/respond", require("pickup.respond", h.respondPickup))
	r.Post("/assignments/{assignmentId}/arrive", require("pickup.complete", h.arrivePickup))
}

type createPickupRequest struct {
	CustomerID    string         `json:"customerId"`
	PickupType    string         `json:"pickupType"`
	AddressID     string         `json:"addressId,omitempty"`
	ContactName   string         `json:"contactName,omitempty"`
	ContactPhone  string         `json:"contactPhone,omitempty"`
	AltPhone      string         `json:"altPhone,omitempty"`
	Line1         string         `json:"line1,omitempty"`
	Line2         string         `json:"line2,omitempty"`
	Landmark      string         `json:"landmark,omitempty"`
	City          string         `json:"city,omitempty"`
	State         string         `json:"state,omitempty"`
	Pincode       string         `json:"pincode,omitempty"`
	Latitude      *float64       `json:"latitude,omitempty"`
	Longitude     *float64       `json:"longitude,omitempty"`
	ScheduledDate string         `json:"scheduledDate"`
	WindowStart   string         `json:"windowStart"`
	WindowEnd     string         `json:"windowEnd"`
	ExpectedPiece int            `json:"expectedPieceCount"`
	ExpectedGrams *int           `json:"expectedWeightGrams,omitempty"`
	Instructions  string         `json:"specialInstructions,omitempty"`
	MaxAttempts   int            `json:"maxAttempts,omitempty"`
	ShipmentIDs   []string       `json:"shipmentIds,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[createPickupRequest](w, r)
	if err != nil {
		return err
	}

	v := validator()
	v.PublicID("customerId", req.CustomerID, publicid.PrefixCustomer, true)
	pickupType := v.Enum("pickupType", req.PickupType,
		[]string{"SCHEDULED", "ON_DEMAND", "BULK", "RECURRING"}, true)
	scheduled := ops.ParseDate(v, "scheduledDate", req.ScheduledDate, true)
	start := ops.ParseTime(v, "windowStart", req.WindowStart)
	end := ops.ParseTime(v, "windowEnd", req.WindowEnd)
	if start == nil {
		v.Add("windowStart", "is required")
	}
	if end == nil {
		v.Add("windowEnd", "is required")
	}
	if start != nil && end != nil && !end.After(*start) {
		v.Add("windowEnd", "must be after the start of the window")
	}
	// A pickup window that has already closed is a scheduling error the
	// dispatcher should see now, not when the agent arrives.
	if end != nil && end.Before(time.Now().Add(-time.Hour)) {
		v.Add("windowEnd", "is in the past")
	}
	if req.AddressID == "" {
		v.Required("line1", req.Line1)
		v.Required("contactName", req.ContactName)
		v.Phone("contactPhone", req.ContactPhone, true)
		v.Pincode("pincode", req.Pincode)
	} else {
		v.PublicID("addressId", req.AddressID, publicid.PrefixCustomerAddress, false)
	}
	v.IntRange("expectedPieceCount", maxInt(req.ExpectedPiece, 1), 1, 5000)
	if req.MaxAttempts != 0 {
		v.IntRange("maxAttempts", req.MaxAttempts, 1, 10)
	}
	for i, id := range req.ShipmentIDs {
		v.PublicID(fieldIdx("shipmentIds", i), id, publicid.PrefixShipment, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	detail, err := h.pickup.Create(r.Context(), p, pickup.CreateInput{
		CustomerID: req.CustomerID, PickupType: pickupType, AddressID: req.AddressID,
		ContactName: req.ContactName, ContactPhone: req.ContactPhone, AltPhone: req.AltPhone,
		Line1: req.Line1, Line2: req.Line2, Landmark: req.Landmark,
		City: req.City, State: req.State, Pincode: req.Pincode,
		Latitude: req.Latitude, Longitude: req.Longitude,
		ScheduledDate: scheduled, WindowStart: *start, WindowEnd: *end,
		ExpectedPiece: maxInt(req.ExpectedPiece, 1), ExpectedGrams: req.ExpectedGrams,
		Instructions: req.Instructions, MaxAttempts: defaultInt(req.MaxAttempts, 3),
		ShipmentIDs: req.ShipmentIDs, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/pickups/"+detail.ID, detail)
}

func (h *Handler) listPickups(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := pickup.ListFilter{
		Status: httpx.Query(r, "status"), PickupType: httpx.Query(r, "pickupType"),
		Cursor: cursor, Limit: limit,
	}
	if d := httpx.Query(r, "scheduledDate"); d != "" {
		v := validator()
		date := ops.ParseDate(v, "scheduledDate", d, true)
		if err := v.Err(); err != nil {
			return err
		}
		f.ScheduledDate = &date
	}
	page, err := h.pickup.List(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "pickupId", publicid.PrefixPickupRequest, "Pickup request")
	if err != nil {
		return err
	}
	detail, err := h.pickup.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) agentStops(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, _, err := ops.Page(r, 50)
	if err != nil {
		return err
	}
	var date *time.Time
	if d := httpx.Query(r, "date"); d != "" {
		v := validator()
		parsed := ops.ParseDate(v, "date", d, true)
		if err := v.Err(); err != nil {
			return err
		}
		date = &parsed
	}
	onlyOpen, err := httpx.QueryBool(r, "onlyOpen", true)
	if err != nil {
		return err
	}
	// An agent always sees their own list. A dispatcher may look at someone
	// else's, which needs pickup.assign.
	agentID := p.UserID
	if other := httpx.Query(r, "agentId"); other != "" {
		if err := p.Require("pickup.assign"); err != nil {
			return err
		}
		id, lErr := h.lookupUserID(r, other)
		if lErr != nil {
			return lErr
		}
		agentID = id
	}
	stops, err := h.pickup.AgentWorkList(r.Context(), p, agentID, date, onlyOpen, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": stops})
}

type assignPickupRequest struct {
	AgentID      string `json:"agentId"`
	RunID        string `json:"runId,omitempty"`
	StopSequence *int   `json:"stopSequence,omitempty"`
}

func (h *Handler) assignPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "pickupId", publicid.PrefixPickupRequest, "Pickup request")
	if err != nil {
		return err
	}
	req, err := body[assignPickupRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("agentId", req.AgentID, publicid.PrefixUser, true)
	v.PublicID("runId", req.RunID, publicid.PrefixPickupRun, false)
	if req.StopSequence != nil {
		v.IntRange("stopSequence", *req.StopSequence, 1, 500)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.pickup.Assign(r.Context(), p, pickup.AssignInput{
		RequestID: id, AgentUserID: req.AgentID, RunID: req.RunID, StopSequence: req.StopSequence,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type respondPickupRequest struct {
	Accept bool   `json:"accept"`
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) respondPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "assignmentId", publicid.PrefixPickupAssignment, "Pickup assignment")
	if err != nil {
		return err
	}
	req, err := body[respondPickupRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	if !req.Accept {
		v.Text("reason", req.Reason, 3, 500, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.pickup.Respond(r.Context(), p, pickup.RespondInput{
		AssignmentID: id, Accept: req.Accept, Reason: req.Reason,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type arrivePickupRequest struct {
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
}

func (h *Handler) arrivePickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "assignmentId", publicid.PrefixPickupAssignment, "Pickup assignment")
	if err != nil {
		return err
	}
	req, err := body[arrivePickupRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.pickup.Arrive(r.Context(), p, pickup.ArriveInput{
		AssignmentID: id, Latitude: req.Latitude, Longitude: req.Longitude,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type completePickupRequest struct {
	Outcome       string   `json:"outcome"`
	CollectedIDs  []string `json:"collectedShipmentIds,omitempty"`
	LoosePieces   int      `json:"loosePieces,omitempty"`
	WeightGrams   *int     `json:"weightGrams,omitempty"`
	FailureReason string   `json:"failureReasonCode,omitempty"`
	Remarks       string   `json:"remarks,omitempty"`
	OccurredAt    string   `json:"occurredAt,omitempty"`
	NextAttemptAt string   `json:"nextAttemptAt,omitempty"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
}

func (h *Handler) completePickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "pickupId", publicid.PrefixPickupRequest, "Pickup request")
	if err != nil {
		return err
	}
	req, err := body[completePickupRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	outcome := v.Enum("outcome", req.Outcome,
		[]string{"COMPLETED", "PARTIAL", "FAILED", "RESCHEDULED", "CANCELLED_ON_SITE"}, true)
	if outcome == "FAILED" || outcome == "RESCHEDULED" {
		v.Enum("failureReasonCode", req.FailureReason, pickupFailureReasons, true)
	}
	for i, sid := range req.CollectedIDs {
		v.PublicID(fieldIdx("collectedShipmentIds", i), sid, publicid.PrefixShipment, true)
	}
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	occurred := ops.ParseTime(v, "occurredAt", req.OccurredAt)
	next := ops.ParseTime(v, "nextAttemptAt", req.NextAttemptAt)
	if req.LoosePieces != 0 {
		v.IntRange("loosePieces", req.LoosePieces, 0, 5000)
	}
	if err := v.Err(); err != nil {
		return err
	}

	detail, err := h.pickup.Complete(r.Context(), p, pickup.CompleteInput{
		RequestID: id, Outcome: outcome, CollectedShipmentIDs: req.CollectedIDs,
		LoosePieces: req.LoosePieces, WeightGrams: req.WeightGrams,
		FailureReason: req.FailureReason, Remarks: req.Remarks,
		OccurredAt: occurred, NextAttemptAt: next,
		Latitude: req.Latitude, Longitude: req.Longitude,
		Device: ops.DeviceFrom(r),
	})
	if err != nil {
		return err
	}
	if detail.ReplayedAttemptID != "" {
		w.Header().Set("Idempotent-Replay", "true")
	}
	return httpx.OK(w, detail)
}

type cancelPickupRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) cancelPickup(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "pickupId", publicid.PrefixPickupRequest, "Pickup request")
	if err != nil {
		return err
	}
	req, err := body[cancelPickupRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	reason := v.Text("reason", req.Reason, 3, 500, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.pickup.Cancel(r.Context(), p, id, reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

var pickupFailureReasons = []string{
	"CUSTOMER_NOT_AVAILABLE", "PREMISES_CLOSED", "SHIPMENT_NOT_READY",
	"ADDRESS_NOT_FOUND", "CUSTOMER_CANCELLED", "VEHICLE_CAPACITY",
	"RESTRICTED_ACCESS", "WEATHER", "OTHER",
}

func (h *Handler) pickupRunRoutes(r chi.Router) {
	r.Get("/", require("pickup.read", h.listPickupRuns))
	r.Post("/", require("pickup.assign", h.createPickupRun))
}

type createPickupRunRequest struct {
	BranchID string `json:"branchId,omitempty"`
	AgentID  string `json:"agentId"`
	RunDate  string `json:"runDate"`
	Vehicle  string `json:"vehicleReference,omitempty"`
}

func (h *Handler) createPickupRun(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[createPickupRunRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("agentId", req.AgentID, publicid.PrefixUser, true)
	runDate := ops.ParseDate(v, "runDate", req.RunDate, true)
	if err := v.Err(); err != nil {
		return err
	}
	branch, err := h.units.RequireFacility(r.Context(), p, req.BranchID)
	if err != nil {
		return err
	}
	agentID, err := h.lookupUserID(r, req.AgentID)
	if err != nil {
		return err
	}
	detail, err := h.pickup.CreateRun(r.Context(), p, pickup.CreateRunInput{
		Branch: branch, AgentUserID: agentID, RunDate: runDate, Vehicle: req.Vehicle,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/pickup-runs/"+detail.ID, detail)
}

func (h *Handler) listPickupRuns(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, _, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	offset, err := httpx.QueryInt(r, "offset", 0, 0, 10000)
	if err != nil {
		return err
	}
	var date *time.Time
	if d := httpx.Query(r, "runDate"); d != "" {
		v := validator()
		parsed := ops.ParseDate(v, "runDate", d, true)
		if err := v.Err(); err != nil {
			return err
		}
		date = &parsed
	}
	runs, err := h.pickup.ListRuns(r.Context(), p, httpx.Query(r, "status"), date, limit, offset)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": runs})
}

// ---------------------------------------------------------------------------
// M10 Scanning
// ---------------------------------------------------------------------------

func (h *Handler) scanRoutes(r chi.Router) {
	r.Post("/", h.scanEndpoint())
	r.Post("/bulk", h.bulkScanEndpoint())
	r.Get("/", require("scan.read", h.listScans))
}

type scanRequest struct {
	Barcode         string   `json:"barcode"`
	ScanType        string   `json:"scanType"`
	OperatingUnitID string   `json:"operatingUnitId,omitempty"`
	OccurredAt      string   `json:"occurredAt,omitempty"`
	Latitude        *float64 `json:"latitude,omitempty"`
	Longitude       *float64 `json:"longitude,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	ReasonCode      string   `json:"reasonCode,omitempty"`
	Remarks         string   `json:"remarks,omitempty"`
	SortDestination string   `json:"sortDestination,omitempty"`
	Override        bool     `json:"override,omitempty"`
}

// scanEndpoint gates on any scan permission; the service checks the specific
// one for the scan type, because a user may hold scan.inbound but not
// scan.exception.
func (h *Handler) scanEndpoint() http.HandlerFunc {
	return requireAny(h.scan,
		"scan.inbound", "scan.outbound", "scan.sort", "scan.hold", "scan.exception")
}

func (h *Handler) bulkScanEndpoint() http.HandlerFunc {
	return requireAny(h.bulkScan,
		"scan.inbound", "scan.outbound", "scan.sort", "scan.hold", "scan.exception")
}

func (h *Handler) scan(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[scanRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", req.Barcode, true)
	scanType := v.Enum("scanType", req.ScanType, scanTypeStrings(), true)
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	occurred := ops.ParseTime(v, "occurredAt", req.OccurredAt)
	if err := v.Err(); err != nil {
		return err
	}

	facility, err := h.units.RequireFacility(r.Context(), p, req.OperatingUnitID)
	if err != nil {
		return err
	}
	result, err := h.scanning.Scan(r.Context(), p, scanning.Input{
		Barcode: barcode, ScanType: scanning.ScanType(scanType), Facility: facility,
		Device: ops.DeviceFrom(r), OccurredAt: occurred,
		Latitude: req.Latitude, Longitude: req.Longitude,
		Reason: req.Reason, ReasonCode: req.ReasonCode, Remarks: req.Remarks,
		SortDestination: req.SortDestination, Override: req.Override,
	})
	if err != nil {
		return err
	}
	// A rejected scan is a successful API call reporting an operational refusal:
	// the scan was recorded, and the handset needs the detail to show the
	// operator what to do. A 4xx would lose that distinction.
	return httpx.OK(w, result)
}

type bulkScanRequest struct {
	ScanType        string              `json:"scanType"`
	OperatingUnitID string              `json:"operatingUnitId,omitempty"`
	Latitude        *float64            `json:"latitude,omitempty"`
	Longitude       *float64            `json:"longitude,omitempty"`
	Override        bool                `json:"override,omitempty"`
	Items           []scanning.BulkItem `json:"items"`
}

func (h *Handler) bulkScan(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[bulkScanRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	scanType := v.Enum("scanType", req.ScanType, scanTypeStrings(), true)
	if len(req.Items) == 0 {
		v.Add("items", "must contain at least one barcode")
	}
	if len(req.Items) > h.scanning.MaxBulk() {
		v.Addf("items", "must contain at most %d barcodes", h.scanning.MaxBulk())
	}
	for i, item := range req.Items {
		ops.ValidBarcode(v, fieldIdx("items", i)+".barcode", item.Barcode, true)
	}
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	if err := v.Err(); err != nil {
		return err
	}

	facility, err := h.units.RequireFacility(r.Context(), p, req.OperatingUnitID)
	if err != nil {
		return err
	}
	result, err := h.scanning.BulkScan(r.Context(), p, scanning.BulkInput{
		ScanType: scanning.ScanType(scanType), Facility: facility,
		Device: ops.DeviceFrom(r), Latitude: req.Latitude, Longitude: req.Longitude,
		Override: req.Override, Items: req.Items,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func (h *Handler) listScans(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 50)
	if err != nil {
		return err
	}
	f := scanning.HistoryFilter{
		ScanType: httpx.Query(r, "scanType"), Outcome: httpx.Query(r, "outcome"),
		Cursor: cursor, Limit: limit,
	}
	if unit := httpx.Query(r, "operatingUnitId"); unit != "" {
		facility, fErr := h.units.Facility(r.Context(), p, unit)
		if fErr != nil {
			return fErr
		}
		f.UnitID = &facility.ID
	}
	page, err := h.scanning.History(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func scanTypeStrings() []string {
	out := make([]string, 0, len(scanning.AllScanTypes))
	for _, t := range scanning.AllScanTypes {
		out = append(out, string(t))
	}
	return out
}

func (h *Handler) lookupUserID(r *http.Request, publicID string) (int64, error) {
	p, err := tenantPrincipal(r)
	if err != nil {
		return 0, err
	}
	id, err := h.units.UserID(r.Context(), p.OrganizationID, publicID)
	if err != nil {
		return 0, apierr.NotFound("User")
	}
	return id, nil
}

func fieldIdx(field string, i int) string {
	return field + "[" + itoa(i) + "]"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

func maxInt(v, min int) int {
	if v < min {
		return min
	}
	return v
}

func defaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
