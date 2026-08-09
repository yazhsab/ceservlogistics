package opsapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/bagging"
	"github.com/ceserve/courier-os/internal/hubops"
	"github.com/ceserve/courier-os/internal/linehaul"
	"github.com/ceserve/courier-os/internal/manifest"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
)

// ---------------------------------------------------------------------------
// M11 Bagging
// ---------------------------------------------------------------------------

func (h *Handler) bagRoutes(r chi.Router) {
	r.Get("/", require("bag.read", h.listBags))
	r.Post("/", require("bag.manage", h.createBag))
	r.Get("/by-barcode/{barcode}", require("bag.read", h.getBagByBarcode))
	r.Get("/{bagId}", require("bag.read", h.getBag))
	r.Post("/{bagId}/items", require("bag.manage", h.addBagItems))
	r.Delete("/{bagId}/items/{barcode}", require("bag.manage", h.removeBagItem))
	r.Post("/{bagId}/close", require("bag.close", h.closeBag))
	r.Post("/{bagId}/dispatch", require("bag.dispatch", h.dispatchBag))
	r.Post("/{bagId}/receive", require("bag.receive", h.receiveBag))
	r.Post("/{bagId}/open", require("bag.open", h.openBag))
	r.Post("/{bagId}/verify-seal", require("bag.receive", h.verifyBagSeal))
	r.Post("/{bagId}/exception-correction", require("bag.exception_edit", h.correctBag))
}

type createBagRequest struct {
	OriginUnitID      string         `json:"originUnitId,omitempty"`
	DestinationUnitID string         `json:"destinationUnitId"`
	BagType           string         `json:"bagType,omitempty"`
	ServiceCode       string         `json:"serviceCode,omitempty"`
	Direction         string         `json:"direction,omitempty"`
	MaxWeightGrams    *int           `json:"maxWeightGrams,omitempty"`
	MaxShipments      *int           `json:"maxShipments,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createBag(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[createBagRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("destinationUnitId", req.DestinationUnitID, publicid.PrefixOperatingUnit, true)
	v.PublicID("originUnitId", req.OriginUnitID, publicid.PrefixOperatingUnit, false)
	v.Enum("bagType", req.BagType, []string{"STANDARD", "SECURE", "FRAGILE", "DOCUMENT", "RETURN"}, false)
	v.Enum("direction", req.Direction, []string{"FORWARD", "REVERSE"}, false)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.bagging.Create(r.Context(), p, bagging.CreateInput{
		OriginUnitID: req.OriginUnitID, DestinationUnitID: req.DestinationUnitID,
		BagType: req.BagType, ServiceCode: req.ServiceCode, Direction: req.Direction,
		MaxWeightGrams: req.MaxWeightGrams, MaxShipments: req.MaxShipments,
		Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/bags/"+detail.ID, detail)
}

func (h *Handler) listBags(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := bagging.ListFilter{
		Status: httpx.Query(r, "status"), Direction: httpx.Query(r, "direction"),
		Cursor: cursor, Limit: limit,
	}
	if u := httpx.Query(r, "originUnitId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.OriginID = &facility.ID
	}
	if u := httpx.Query(r, "destinationUnitId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.DestID = &facility.ID
	}
	page, err := h.bagging.List(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getBag(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	detail, err := h.bagging.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) getBagByBarcode(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	code, err := httpx.PathParam(r, "barcode", 64)
	if err != nil {
		return err
	}
	detail, err := h.bagging.GetByBarcode(r.Context(), p, code)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type bagItemsRequest struct {
	Barcodes []string `json:"barcodes"`
}

func (h *Handler) addBagItems(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	req, err := body[bagItemsRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcodes := ops.Barcodes(v, "barcodes", req.Barcodes, 500)
	if err := v.Err(); err != nil {
		return err
	}
	result, err := h.bagging.AddItems(r.Context(), p, bagging.AddItemsInput{
		BagID: id, Barcodes: barcodes, Device: ops.DeviceFrom(r),
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

type removeBagItemRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) removeBagItem(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	barcode, err := httpx.PathParam(r, "barcode", 64)
	if err != nil {
		return err
	}
	reason := httpx.QueryDefault(r, "reason", "Removed at the counter")
	detail, err := h.bagging.RemoveItem(r.Context(), p, id, barcode, reason, ops.DeviceFrom(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type closeBagRequest struct {
	SealNumbers      []string `json:"sealNumbers"`
	SealType         string   `json:"sealType,omitempty"`
	GrossWeightGrams *int     `json:"grossWeightGrams,omitempty"`
}

func (h *Handler) closeBag(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	req, err := body[closeBagRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	for i, s := range req.SealNumbers {
		v.Text(fieldIdx("sealNumbers", i), s, 3, 64, true)
	}
	v.Enum("sealType", req.SealType, []string{"PLASTIC", "METAL", "ELECTRONIC", "TAPE"}, false)
	if req.GrossWeightGrams != nil {
		v.IntRange("grossWeightGrams", *req.GrossWeightGrams, 1, 2_000_000)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.bagging.Close(r.Context(), p, bagging.CloseInput{
		BagID: id, SealNumbers: req.SealNumbers, SealType: req.SealType,
		GrossWeightGrams: req.GrossWeightGrams,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) dispatchBag(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	detail, err := h.bagging.Dispatch(r.Context(), p, id, ops.DeviceFrom(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) receiveBag(w http.ResponseWriter, r *http.Request) error {
	return h.bagFacilityAction(w, r, h.bagging.Receive)
}

func (h *Handler) openBag(w http.ResponseWriter, r *http.Request) error {
	return h.bagFacilityAction(w, r, h.bagging.OpenAtDestination)
}

type bagFacilityFunc func(
	ctx contextT, p principalT, bagID string, facility *ops.Facility, device ops.Device,
) (*bagging.Detail, error)

func (h *Handler) bagFacilityAction(w http.ResponseWriter, r *http.Request, fn bagFacilityFunc) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	detail, err := fn(r.Context(), rc.Principal, id, rc.Facility, rc.Device)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type verifySealRequest struct {
	SealNumber string `json:"sealNumber"`
	Result     string `json:"result"`
	Remarks    string `json:"remarks,omitempty"`
}

func (h *Handler) verifyBagSeal(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	req, err := body[verifySealRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.Text("sealNumber", req.SealNumber, 3, 64, true)
	result := v.Enum("result", req.Result, []string{"INTACT", "BROKEN", "MISSING", "MISMATCH"}, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.bagging.VerifySeal(r.Context(), rc.Principal, bagging.VerifySealInput{
		BagID: id, SealNumber: req.SealNumber, Result: result,
		Remarks: req.Remarks, Facility: rc.Facility,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type correctBagRequest struct {
	Barcode     string `json:"barcode"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	ExceptionID string `json:"exceptionId"`
}

func (h *Handler) correctBag(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	id, err := pathID(r, "bagId", publicid.PrefixBag, "Bag")
	if err != nil {
		return err
	}
	req, err := body[correctBagRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	barcode := ops.ValidBarcode(v, "barcode", req.Barcode, true)
	action := v.Enum("action", req.Action, []string{"REMOVE"}, true)
	reason := v.Text("reason", req.Reason, 5, 500, true)
	v.PublicID("exceptionId", req.ExceptionID, publicid.PrefixException, true)
	if err := v.Err(); err != nil {
		return err
	}
	excID, err := h.hubops.ExceptionID(r.Context(), rc.Principal, req.ExceptionID)
	if err != nil {
		return err
	}
	detail, err := h.bagging.Correct(r.Context(), rc.Principal, bagging.CorrectionInput{
		BagID: id, Barcode: barcode, Action: action, Reason: reason,
		ExceptionID: &excID, Facility: rc.Facility,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---------------------------------------------------------------------------
// M12 Manifest
// ---------------------------------------------------------------------------

func (h *Handler) manifestRoutes(r chi.Router) {
	r.Get("/", require("manifest.read", h.listManifests))
	r.Post("/", require("manifest.manage", h.createManifest))
	r.Get("/{manifestId}", require("manifest.read", h.getManifest))
	r.Post("/{manifestId}/contents", require("manifest.manage", h.addManifestContent))
	r.Delete("/{manifestId}/contents", require("manifest.manage", h.removeManifestContent))
	r.Post("/{manifestId}/close", require("manifest.close", h.closeManifest))
	r.Post("/{manifestId}/dispatch", require("manifest.dispatch", h.dispatchManifest))
	r.Post("/{manifestId}/receive", require("manifest.receive", h.receiveManifest))
}

type createManifestRequest struct {
	OriginUnitID      string         `json:"originUnitId,omitempty"`
	DestinationUnitID string         `json:"destinationUnitId"`
	TripID            string         `json:"tripId,omitempty"`
	Direction         string         `json:"direction,omitempty"`
	Remarks           string         `json:"remarks,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createManifest(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[createManifestRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("destinationUnitId", req.DestinationUnitID, publicid.PrefixOperatingUnit, true)
	v.PublicID("originUnitId", req.OriginUnitID, publicid.PrefixOperatingUnit, false)
	v.PublicID("tripId", req.TripID, publicid.PrefixTrip, false)
	v.Enum("direction", req.Direction, []string{"FORWARD", "REVERSE"}, false)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.manifest.Create(r.Context(), p, manifest.CreateInput{
		OriginUnitID: req.OriginUnitID, DestinationUnitID: req.DestinationUnitID,
		TripID: req.TripID, Direction: req.Direction, Remarks: req.Remarks,
		Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/manifests/"+detail.ID, detail)
}

func (h *Handler) listManifests(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := manifest.ListFilter{Status: httpx.Query(r, "status"), Cursor: cursor, Limit: limit}
	if u := httpx.Query(r, "originUnitId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.OriginID = &facility.ID
	}
	if u := httpx.Query(r, "destinationUnitId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.DestID = &facility.ID
	}
	page, err := h.manifest.List(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getManifest(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "manifestId", publicid.PrefixManifest, "Manifest")
	if err != nil {
		return err
	}
	detail, err := h.manifest.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type manifestContentRequest struct {
	BagCodes []string `json:"bagCodes,omitempty"`
	Barcodes []string `json:"barcodes,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Ref      string   `json:"reference,omitempty"`
}

func (h *Handler) addManifestContent(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "manifestId", publicid.PrefixManifest, "Manifest")
	if err != nil {
		return err
	}
	req, err := body[manifestContentRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	if len(req.BagCodes) == 0 && len(req.Barcodes) == 0 {
		v.Add("bagCodes", "supply at least one bag code or shipment barcode")
	}
	for i, c := range req.BagCodes {
		ops.ValidBarcode(v, fieldIdx("bagCodes", i), c, true)
	}
	for i, c := range req.Barcodes {
		ops.ValidBarcode(v, fieldIdx("barcodes", i), c, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	result, err := h.manifest.AddContent(r.Context(), p, manifest.AddContentInput{
		ManifestID: id, BagCodes: req.BagCodes, Barcodes: req.Barcodes,
		Device: ops.DeviceFrom(r),
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

func (h *Handler) removeManifestContent(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "manifestId", publicid.PrefixManifest, "Manifest")
	if err != nil {
		return err
	}
	req, err := body[manifestContentRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	kind := v.Enum("kind", req.Kind, []string{"BAG", "SHIPMENT"}, true)
	ref := ops.ValidBarcode(v, "reference", req.Ref, true)
	reason := v.Text("reason", req.Reason, 3, 500, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.manifest.RemoveContent(r.Context(), p, id, ref, kind, reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type closeManifestRequest struct {
	DeclaredWeightGrams *int64 `json:"declaredWeightGrams,omitempty"`
}

func (h *Handler) closeManifest(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "manifestId", publicid.PrefixManifest, "Manifest")
	if err != nil {
		return err
	}
	req, err := body[closeManifestRequest](w, r)
	if err != nil {
		return err
	}
	detail, err := h.manifest.Close(r.Context(), p, manifest.CloseInput{
		ManifestID: id, DeclaredWeightGrams: req.DeclaredWeightGrams,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) dispatchManifest(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "manifestId", publicid.PrefixManifest, "Manifest")
	if err != nil {
		return err
	}
	detail, err := h.manifest.Dispatch(r.Context(), p, id, ops.DeviceFrom(r))
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) receiveManifest(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	id, err := pathID(r, "manifestId", publicid.PrefixManifest, "Manifest")
	if err != nil {
		return err
	}
	detail, err := h.manifest.Receive(r.Context(), rc.Principal, id, rc.Facility, rc.Device)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---------------------------------------------------------------------------
// M13 Line haul
// ---------------------------------------------------------------------------

func (h *Handler) carrierRoutes(r chi.Router) {
	r.Get("/", require("carrier.read", h.listCarriers))
	r.Post("/", require("carrier.manage", h.createCarrier))
}

func (h *Handler) vehicleRoutes(r chi.Router) {
	r.Get("/", require("vehicle.read", h.listVehicles))
	r.Post("/", require("vehicle.manage", h.createVehicle))
}

func (h *Handler) driverRoutes(r chi.Router) {
	r.Get("/", require("driver.read", h.listDrivers))
	r.Post("/", require("driver.manage", h.createDriver))
}

func (h *Handler) tripRoutes(r chi.Router) {
	r.Get("/", require("trip.read", h.listTrips))
	r.Post("/", require("trip.manage", h.createTrip))
	r.Get("/{tripId}", require("trip.read", h.getTrip))
	r.Post("/{tripId}/assign", require("trip.manage", h.assignTrip))
	r.Post("/{tripId}/manifests", require("trip.manage", h.attachManifest))
	r.Post("/{tripId}/depart", require("linehaul.depart", h.departTrip))
	r.Post("/{tripId}/arrive", require("linehaul.arrive", h.arriveTrip))
	r.Post("/{tripId}/close", require("linehaul.arrive", h.closeTrip))
	r.Post("/{tripId}/cancel", require("trip.cancel", h.cancelTrip))
}

type createTripRequest struct {
	Mode               string             `json:"mode"`
	OriginUnitID       string             `json:"originUnitId,omitempty"`
	DestinationUnitID  string             `json:"destinationUnitId"`
	Direction          string             `json:"direction,omitempty"`
	CarrierID          string             `json:"carrierId,omitempty"`
	VehicleID          string             `json:"vehicleId,omitempty"`
	DriverID           string             `json:"driverId,omitempty"`
	ExternalReference  string             `json:"externalReference,omitempty"`
	ScheduledDeparture string             `json:"scheduledDeparture"`
	ScheduledArrival   string             `json:"scheduledArrival"`
	Legs               []createTripLegReq `json:"legs,omitempty"`
	Metadata           map[string]any     `json:"metadata,omitempty"`
}

type createTripLegReq struct {
	OriginUnitID       string `json:"originUnitId"`
	DestinationUnitID  string `json:"destinationUnitId"`
	ScheduledDeparture string `json:"scheduledDeparture"`
	ScheduledArrival   string `json:"scheduledArrival"`
	DistanceKM         *int   `json:"distanceKm,omitempty"`
}

func (h *Handler) createTrip(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[createTripRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	mode := v.Enum("mode", req.Mode, linehaul.AllModes, true)
	v.PublicID("destinationUnitId", req.DestinationUnitID, publicid.PrefixOperatingUnit, true)
	v.PublicID("originUnitId", req.OriginUnitID, publicid.PrefixOperatingUnit, false)
	v.PublicID("carrierId", req.CarrierID, publicid.PrefixCarrier, false)
	v.PublicID("vehicleId", req.VehicleID, publicid.PrefixVehicle, false)
	v.PublicID("driverId", req.DriverID, publicid.PrefixDriver, false)
	dep := ops.ParseTime(v, "scheduledDeparture", req.ScheduledDeparture)
	arr := ops.ParseTime(v, "scheduledArrival", req.ScheduledArrival)
	if dep == nil {
		v.Add("scheduledDeparture", "is required")
	}
	if arr == nil {
		v.Add("scheduledArrival", "is required")
	}
	if dep != nil && arr != nil && !arr.After(*dep) {
		v.Add("scheduledArrival", "must be after the departure")
	}
	legs := make([]linehaul.LegInput, 0, len(req.Legs))
	for i, l := range req.Legs {
		field := fieldIdx("legs", i)
		v.PublicID(field+".originUnitId", l.OriginUnitID, publicid.PrefixOperatingUnit, true)
		v.PublicID(field+".destinationUnitId", l.DestinationUnitID, publicid.PrefixOperatingUnit, true)
		ld := ops.ParseTime(v, field+".scheduledDeparture", l.ScheduledDeparture)
		la := ops.ParseTime(v, field+".scheduledArrival", l.ScheduledArrival)
		if ld == nil || la == nil {
			continue
		}
		legs = append(legs, linehaul.LegInput{
			OriginUnitID: l.OriginUnitID, DestinationUnitID: l.DestinationUnitID,
			ScheduledDeparture: *ld, ScheduledArrival: *la, DistanceKM: l.DistanceKM,
		})
	}
	if err := v.Err(); err != nil {
		return err
	}

	detail, err := h.linehaul.CreateTrip(r.Context(), p, linehaul.CreateTripInput{
		Mode: mode, OriginUnitID: req.OriginUnitID, DestinationUnitID: req.DestinationUnitID,
		Direction: req.Direction, CarrierID: req.CarrierID, VehicleID: req.VehicleID,
		DriverID: req.DriverID, ExternalReference: req.ExternalReference,
		ScheduledDeparture: *dep, ScheduledArrival: *arr, Legs: legs, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/trips/"+detail.ID, detail)
}

func (h *Handler) listTrips(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := linehaul.TripFilter{
		Status: httpx.Query(r, "status"), Mode: httpx.Query(r, "mode"),
		Cursor: cursor, Limit: limit,
	}
	if u := httpx.Query(r, "originUnitId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.OriginID = &facility.ID
	}
	page, err := h.linehaul.ListTrips(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getTrip(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	detail, err := h.linehaul.GetTrip(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type assignTripRequest struct {
	VehicleID string `json:"vehicleId,omitempty"`
	DriverID  string `json:"driverId,omitempty"`
	CarrierID string `json:"carrierId,omitempty"`
	Reference string `json:"externalReference,omitempty"`
}

func (h *Handler) assignTrip(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	req, err := body[assignTripRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("vehicleId", req.VehicleID, publicid.PrefixVehicle, false)
	v.PublicID("driverId", req.DriverID, publicid.PrefixDriver, false)
	v.PublicID("carrierId", req.CarrierID, publicid.PrefixCarrier, false)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.linehaul.Assign(r.Context(), p, linehaul.AssignInput{
		TripID: id, VehicleID: req.VehicleID, DriverID: req.DriverID,
		CarrierID: req.CarrierID, Reference: req.Reference,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type attachManifestRequest struct {
	ManifestID  string `json:"manifestId"`
	LegSequence *int   `json:"legSequence,omitempty"`
}

func (h *Handler) attachManifest(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	req, err := body[attachManifestRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	v.PublicID("manifestId", req.ManifestID, publicid.PrefixManifest, true)
	if req.LegSequence != nil {
		v.IntRange("legSequence", *req.LegSequence, 1, 50)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.linehaul.AttachManifest(r.Context(), p, id, req.ManifestID, req.LegSequence)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type departTripRequest struct {
	OdometerKM *int   `json:"odometerKm,omitempty"`
	SealNumber string `json:"sealNumber,omitempty"`
	OccurredAt string `json:"occurredAt,omitempty"`
}

func (h *Handler) departTrip(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	req, err := body[departTripRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	occurred := ops.ParseTime(v, "occurredAt", req.OccurredAt)
	if req.OdometerKM != nil {
		v.IntRange("odometerKm", *req.OdometerKM, 0, 10_000_000)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.linehaul.Depart(r.Context(), p, linehaul.DepartInput{
		TripID: id, OdometerKM: req.OdometerKM, SealNumber: req.SealNumber,
		OccurredAt: occurred, Device: ops.DeviceFrom(r),
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type arriveTripRequest struct {
	OdometerKM  *int   `json:"odometerKm,omitempty"`
	OccurredAt  string `json:"occurredAt,omitempty"`
	LegSequence *int   `json:"legSequence,omitempty"`
}

func (h *Handler) arriveTrip(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	req, err := body[arriveTripRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	occurred := ops.ParseTime(v, "occurredAt", req.OccurredAt)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.linehaul.Arrive(r.Context(), rc.Principal, linehaul.ArriveInput{
		TripID: id, Facility: rc.Facility, OdometerKM: req.OdometerKM,
		OccurredAt: occurred, Device: rc.Device, LegSequence: req.LegSequence,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) closeTrip(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	detail, err := h.linehaul.Close(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type cancelTripRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) cancelTrip(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "tripId", publicid.PrefixTrip, "Trip")
	if err != nil {
		return err
	}
	req, err := body[cancelTripRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	reason := v.Text("reason", req.Reason, 5, 500, true)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.linehaul.Cancel(r.Context(), p, id, reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

// ---------------------------------------------------------------------------
// M14 Hub operations
// ---------------------------------------------------------------------------

func (h *Handler) hubRoutes(r chi.Router) {
	r.Get("/summary", require("hub.dashboard", h.hubSummary))
	r.Get("/inbound", require("hub.dashboard", h.hubInbound))
	r.Get("/custody", require("hub.dashboard", h.hubCustody))
	r.Get("/throughput", require("hub.dashboard", h.hubThroughput))
}

func (h *Handler) hubSummary(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	summary, err := h.hubops.Workload(r.Context(), rc.Principal, rc.Facility)
	if err != nil {
		return err
	}
	return httpx.OK(w, summary)
}

func (h *Handler) hubInbound(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	limit, _, err := ops.Page(r, 50)
	if err != nil {
		return err
	}
	manifests, err := h.manifest.ExpectedInbound(r.Context(), rc.Principal, rc.Facility.ID, limit)
	if err != nil {
		return err
	}
	trips, err := h.linehaul.InboundTrips(r.Context(), rc.Principal, rc.Facility.ID, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"facility": ops.RefOf(rc.Facility), "manifests": manifests, "trips": trips,
	})
}

func (h *Handler) hubCustody(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 50)
	if err != nil {
		return err
	}
	page, err := h.hubops.CustodyStocktake(r.Context(), rc.Principal, rc.Facility,
		httpx.Query(r, "status"), cursor, limit)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) hubThroughput(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	hours, err := httpx.QueryInt(r, "hours", 24, 1, 168)
	if err != nil {
		return err
	}
	buckets, err := h.hubops.Throughput(r.Context(), rc.Principal, rc.Facility,
		time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": buckets})
}

func (h *Handler) exceptionRoutes(r chi.Router) {
	r.Get("/", require("exception.read", h.listExceptions))
	r.Post("/", require("exception.create", h.raiseException))
	r.Get("/{exceptionId}", require("exception.read", h.getException))
	r.Post("/{exceptionId}/resolve", require("exception.resolve", h.resolveException))
}

type raiseExceptionRequest struct {
	Type        string         `json:"exceptionType"`
	Severity    string         `json:"severity,omitempty"`
	Barcode     string         `json:"barcode,omitempty"`
	BagCode     string         `json:"bagCode,omitempty"`
	Description string         `json:"description"`
	Expected    *int           `json:"expectedCount,omitempty"`
	Actual      *int           `json:"actualCount,omitempty"`
	AssignTo    string         `json:"assignToUserId,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) raiseException(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	req, err := body[raiseExceptionRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	kind := v.Enum("exceptionType", req.Type, hubops.ExceptionTypes, true)
	v.Enum("severity", req.Severity, []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"}, false)
	description := v.Text("description", req.Description, 5, 1000, true)
	v.PublicID("assignToUserId", req.AssignTo, publicid.PrefixUser, false)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.hubops.Raise(r.Context(), rc.Principal, hubops.RaiseInput{
		Type: kind, Severity: req.Severity, Facility: rc.Facility,
		Barcode: req.Barcode, BagCode: req.BagCode, Description: description,
		Expected: req.Expected, Actual: req.Actual, AssignTo: req.AssignTo,
		Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/exceptions/"+detail.ID, detail)
}

func (h *Handler) listExceptions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	f := hubops.ExceptionFilter{
		Status: httpx.Query(r, "status"), Type: httpx.Query(r, "exceptionType"),
		Severity: httpx.Query(r, "severity"), Cursor: cursor, Limit: limit,
	}
	if u := httpx.Query(r, "operatingUnitId"); u != "" {
		facility, fErr := h.units.Facility(r.Context(), p, u)
		if fErr != nil {
			return fErr
		}
		f.UnitID = &facility.ID
	}
	page, err := h.hubops.ListExceptions(r.Context(), p, f)
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getException(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "exceptionId", publicid.PrefixException, "Exception")
	if err != nil {
		return err
	}
	detail, err := h.hubops.GetException(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

type resolveExceptionRequest struct {
	Status   string `json:"status"`
	Action   string `json:"resolutionAction,omitempty"`
	Notes    string `json:"resolutionNotes,omitempty"`
	AssignTo string `json:"assignToUserId,omitempty"`
}

func (h *Handler) resolveException(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "exceptionId", publicid.PrefixException, "Exception")
	if err != nil {
		return err
	}
	req, err := body[resolveExceptionRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	status := v.Enum("status", req.Status,
		[]string{"INVESTIGATING", "RESOLVED", "WRITTEN_OFF", "CANCELLED"}, true)
	v.Enum("resolutionAction", req.Action, hubops.ResolutionActions, false)
	v.PublicID("assignToUserId", req.AssignTo, publicid.PrefixUser, false)
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.hubops.Resolve(r.Context(), p, hubops.ResolveInput{
		ExceptionID: id, Status: status, Action: req.Action,
		Notes: req.Notes, AssignTo: req.AssignTo,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) reconciliationRoutes(r chi.Router) {
	r.Get("/", require("reconciliation.manage", h.listReconciliations))
	r.Post("/", require("reconciliation.manage", h.startReconciliation))
	r.Get("/{reconciliationId}", require("reconciliation.manage", h.getReconciliation))
	r.Post("/{reconciliationId}/scan", require("reconciliation.manage", h.scanReconciliation))
	r.Post("/{reconciliationId}/complete", require("reconciliation.manage", h.completeReconciliation))
}

type startReconciliationRequest struct {
	SubjectType string `json:"subjectType"`
	BagID       string `json:"bagId,omitempty"`
	ManifestID  string `json:"manifestId,omitempty"`
}

func (h *Handler) startReconciliation(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, true)
	if err != nil {
		return err
	}
	req, err := body[startReconciliationRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	subject := v.Enum("subjectType", req.SubjectType, []string{"BAG", "MANIFEST"}, true)
	if subject == "BAG" {
		v.PublicID("bagId", req.BagID, publicid.PrefixBag, true)
	}
	if subject == "MANIFEST" {
		v.PublicID("manifestId", req.ManifestID, publicid.PrefixManifest, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, err := h.hubops.StartReconciliation(r.Context(), rc.Principal, hubops.StartReconciliationInput{
		SubjectType: subject, BagID: req.BagID, ManifestID: req.ManifestID, Facility: rc.Facility,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/reconciliations/"+detail.ID, detail)
}

type reconciliationScanRequest struct {
	Barcodes []string `json:"barcodes"`
	Damaged  []string `json:"damagedBarcodes,omitempty"`
}

func (h *Handler) scanReconciliation(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	id, err := pathID(r, "reconciliationId", publicid.PrefixReconciliation, "Reconciliation")
	if err != nil {
		return err
	}
	req, err := body[reconciliationScanRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	if len(req.Barcodes) == 0 && len(req.Damaged) == 0 {
		v.Add("barcodes", "supply at least one barcode")
	}
	for i, b := range req.Barcodes {
		ops.ValidBarcode(v, fieldIdx("barcodes", i), b, true)
	}
	for i, b := range req.Damaged {
		ops.ValidBarcode(v, fieldIdx("damagedBarcodes", i), b, true)
	}
	if err := v.Err(); err != nil {
		return err
	}
	detail, results, err := h.hubops.Scan(r.Context(), rc.Principal, hubops.ScanInput{
		ReconciliationID: id, Barcodes: req.Barcodes, Damaged: req.Damaged,
		Facility: rc.Facility,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"reconciliation": detail, "results": results})
}

type completeReconciliationRequest struct {
	Remarks string `json:"remarks,omitempty"`
}

func (h *Handler) completeReconciliation(w http.ResponseWriter, r *http.Request) error {
	rc, err := h.units.Request(r, false)
	if err != nil {
		return err
	}
	id, err := pathID(r, "reconciliationId", publicid.PrefixReconciliation, "Reconciliation")
	if err != nil {
		return err
	}
	req, err := body[completeReconciliationRequest](w, r)
	if err != nil {
		return err
	}
	detail, err := h.hubops.Complete(r.Context(), rc.Principal, id, req.Remarks, rc.Facility)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}

func (h *Handler) listReconciliations(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, cursor, err := ops.Page(r, 25)
	if err != nil {
		return err
	}
	page, err := h.hubops.ListReconciliations(r.Context(), p, hubops.ReconciliationFilter{
		Status: httpx.Query(r, "status"), Cursor: cursor, Limit: limit,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, page)
}

func (h *Handler) getReconciliation(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "reconciliationId", publicid.PrefixReconciliation, "Reconciliation")
	if err != nil {
		return err
	}
	detail, err := h.hubops.GetReconciliation(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, detail)
}
