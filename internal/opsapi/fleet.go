package opsapi

import (
	"net/http"

	"github.com/ceserve/courier-os/internal/linehaul"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
)

// Carrier, vehicle and driver management. These are configuration rather than
// operations, so they are plain CRUD with an offset listing: the volumes are
// small enough that keyset pagination would be ceremony.

type carrierRequest struct {
	Code         string         `json:"code"`
	Name         string         `json:"name"`
	CarrierType  string         `json:"carrierType"`
	Modes        []string       `json:"modes"`
	ContactName  string         `json:"contactName,omitempty"`
	ContactPhone string         `json:"contactPhone,omitempty"`
	ContactEmail string         `json:"contactEmail,omitempty"`
	GSTNumber    string         `json:"gstNumber,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createCarrier(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[carrierRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 160, true)
	kind := v.Enum("carrierType", req.CarrierType,
		[]string{"OWN", "CONTRACTED", "PARTNER", "COURIER_PARTNER"}, true)
	if len(req.Modes) == 0 {
		v.Add("modes", "must list at least one transport mode")
	}
	for i, m := range req.Modes {
		v.Enum(fieldIdx("modes", i), m, linehaul.AllModes, true)
	}
	v.Phone("contactPhone", req.ContactPhone, false)
	if req.ContactEmail != "" {
		v.Email("contactEmail", req.ContactEmail)
	}
	if err := v.Err(); err != nil {
		return err
	}
	out, err := h.linehaul.CreateCarrier(r.Context(), p, linehaul.CarrierInput{
		Code: code, Name: name, CarrierType: kind, Modes: req.Modes,
		ContactName: req.ContactName, ContactPhone: req.ContactPhone,
		ContactEmail: req.ContactEmail, GSTNumber: req.GSTNumber, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/carriers/"+out.ID, out)
}

func (h *Handler) listCarriers(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, offset, err := offsetPage(r)
	if err != nil {
		return err
	}
	rows, err := h.linehaul.ListCarriers(r.Context(), p, httpx.Query(r, "carrierType"), limit, offset)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

type vehicleRequest struct {
	CarrierID      string         `json:"carrierId,omitempty"`
	Registration   string         `json:"registrationNumber"`
	VehicleType    string         `json:"vehicleType"`
	CapacityGrams  *int64         `json:"capacityWeightGrams,omitempty"`
	CapacityVolume *int64         `json:"capacityVolumeCc,omitempty"`
	BaseUnitID     string         `json:"baseUnitId,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createVehicle(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[vehicleRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	reg := v.Text("registrationNumber", req.Registration, 4, 24, true)
	kind := v.Enum("vehicleType", req.VehicleType,
		[]string{"BIKE", "VAN", "TEMPO", "TRUCK", "CONTAINER", "TRAILER", "OTHER"}, true)
	v.PublicID("carrierId", req.CarrierID, publicid.PrefixCarrier, false)
	v.PublicID("baseUnitId", req.BaseUnitID, publicid.PrefixOperatingUnit, false)
	if err := v.Err(); err != nil {
		return err
	}
	out, err := h.linehaul.CreateVehicle(r.Context(), p, linehaul.VehicleInput{
		CarrierID: req.CarrierID, Registration: reg, VehicleType: kind,
		CapacityGrams: req.CapacityGrams, CapacityVolume: req.CapacityVolume,
		BaseUnitID: req.BaseUnitID, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/vehicles/"+out.ID, out)
}

func (h *Handler) listVehicles(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, offset, err := offsetPage(r)
	if err != nil {
		return err
	}
	rows, err := h.linehaul.ListVehicles(r.Context(), p, httpx.Query(r, "vehicleType"), limit, offset)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

type driverRequest struct {
	CarrierID     string         `json:"carrierId,omitempty"`
	UserID        string         `json:"userId,omitempty"`
	Code          string         `json:"code"`
	FullName      string         `json:"fullName"`
	Phone         string         `json:"phone"`
	LicenceNumber string         `json:"licenceNumber,omitempty"`
	BaseUnitID    string         `json:"baseUnitId,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (h *Handler) createDriver(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	req, err := body[driverRequest](w, r)
	if err != nil {
		return err
	}
	v := validator()
	code := v.Code("code", req.Code)
	name := v.Text("fullName", req.FullName, 2, 160, true)
	phone := v.Phone("phone", req.Phone, true)
	v.PublicID("carrierId", req.CarrierID, publicid.PrefixCarrier, false)
	v.PublicID("userId", req.UserID, publicid.PrefixUser, false)
	v.PublicID("baseUnitId", req.BaseUnitID, publicid.PrefixOperatingUnit, false)
	if err := v.Err(); err != nil {
		return err
	}
	out, err := h.linehaul.CreateDriver(r.Context(), p, linehaul.DriverInput{
		CarrierID: req.CarrierID, UserID: req.UserID, Code: code,
		FullName: name, Phone: phone, LicenceNumber: req.LicenceNumber,
		BaseUnitID: req.BaseUnitID, Metadata: req.Metadata,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/drivers/"+out.ID, out)
}

func (h *Handler) listDrivers(w http.ResponseWriter, r *http.Request) error {
	p, err := tenantPrincipal(r)
	if err != nil {
		return err
	}
	limit, offset, err := offsetPage(r)
	if err != nil {
		return err
	}
	rows, err := h.linehaul.ListDrivers(r.Context(), p, limit, offset)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"data": rows})
}

func offsetPage(r *http.Request) (int, int, error) {
	limit, err := httpx.QueryInt(r, "limit", 25, 1, 100)
	if err != nil {
		return 0, 0, err
	}
	offset, err := httpx.QueryInt(r, "offset", 0, 0, 10000)
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}
