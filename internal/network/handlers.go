package network

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler exposes network administration endpoints.
type Handler struct {
	svc   *Service
	audit *audit.Recorder
}

// NewHandler builds the network handler.
func NewHandler(svc *Service, rec *audit.Recorder) *Handler {
	return &Handler{svc: svc, audit: rec}
}

// Routes mounts network endpoints under /network.
func (h *Handler) Routes(r chi.Router) {
	r.Route("/regions", func(rr chi.Router) {
		rr.With(auth.RequirePermission("region.read")).Get("/", httpx.Wrap(h.listRegions))
		rr.With(auth.RequirePermission("region.manage")).Post("/", httpx.Wrap(h.createRegion))
		rr.With(auth.RequirePermission("region.manage")).Patch("/{regionId}", httpx.Wrap(h.updateRegion))
	})

	r.Route("/operating-units", func(ur chi.Router) {
		ur.With(auth.RequirePermission("operating_unit.read")).Get("/", httpx.Wrap(h.listUnits))
		ur.With(auth.RequirePermission("operating_unit.manage")).Post("/", httpx.Wrap(h.createUnit))
		ur.With(auth.RequirePermission("operating_unit.read")).Get("/hierarchy", httpx.Wrap(h.hierarchy))
		ur.With(auth.RequirePermission("operating_unit.read")).Get("/{unitId}", httpx.Wrap(h.getUnit))
		ur.With(auth.RequirePermission("operating_unit.manage")).Patch("/{unitId}", httpx.Wrap(h.updateUnit))
		ur.With(auth.RequirePermission("operating_unit.manage")).Post("/{unitId}/deactivate", httpx.Wrap(h.deactivateUnit))
		ur.With(auth.RequirePermission("operating_unit.read")).Get("/{unitId}/capabilities", httpx.Wrap(h.listCapabilities))
		ur.With(auth.RequirePermission("operating_unit.manage")).Put("/{unitId}/capabilities", httpx.Wrap(h.setCapability))
	})

	// Convenience projections over the same table, so the frontend does not
	// have to know that a hub and a branch are one entity with a discriminator.
	r.With(auth.RequirePermission("operating_unit.read")).Get("/hubs", httpx.Wrap(h.listHubs))
	r.With(auth.RequirePermission("operating_unit.read")).Get("/branches", httpx.Wrap(h.listBranches))

	r.Route("/franchises", func(fr chi.Router) {
		fr.With(auth.RequirePermission("franchise.read")).Get("/", httpx.Wrap(h.listFranchises))
		fr.With(auth.RequirePermission("franchise.manage")).Post("/", httpx.Wrap(h.createFranchise))
		fr.With(auth.RequirePermission("franchise.read")).Get("/{franchiseId}", httpx.Wrap(h.getFranchise))
		fr.With(auth.RequirePermission("franchise.manage")).Patch("/{franchiseId}", httpx.Wrap(h.updateFranchise))
		fr.With(auth.RequirePermission("franchise_agreement.read")).Get("/{franchiseId}/agreements", httpx.Wrap(h.listAgreements))
		fr.With(auth.RequirePermission("franchise_agreement.manage")).Post("/{franchiseId}/agreements", httpx.Wrap(h.createAgreement))
	})
}

// ---- views -----------------------------------------------------------------

// UnitView is the API representation of an operating unit.
type UnitView struct {
	ID             string           `json:"id"`
	Code           string           `json:"code"`
	Name           string           `json:"name"`
	UnitType       string           `json:"unitType"`
	Status         string           `json:"status"`
	Parent         *UnitRef         `json:"parent,omitempty"`
	RegionCode     string           `json:"regionCode,omitempty"`
	Address        AddressView      `json:"address"`
	Contact        ContactView      `json:"contact"`
	OperatingHours map[string]any   `json:"operatingHours,omitempty"`
	EffectiveFrom  time.Time        `json:"effectiveFrom"`
	EffectiveTo    *time.Time       `json:"effectiveTo,omitempty"`
	Capabilities   []CapabilityView `json:"capabilities,omitempty"`
	Version        int32            `json:"version"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

// UnitRef is a compact reference to another unit.
type UnitRef struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	UnitType string `json:"unitType,omitempty"`
}

// AddressView is the postal address of a facility.
type AddressView struct {
	Line1     string   `json:"line1"`
	Line2     string   `json:"line2,omitempty"`
	Landmark  string   `json:"landmark,omitempty"`
	City      string   `json:"city,omitempty"`
	State     string   `json:"state,omitempty"`
	Pincode   string   `json:"pincode"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
}

// ContactView is the facility contact block.
type ContactView struct {
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	Email string `json:"email,omitempty"`
}

// CapabilityView is one capability toggle.
type CapabilityView struct {
	Capability string `json:"capability"`
	Enabled    bool   `json:"enabled"`
}

// ---- regions ---------------------------------------------------------------

type createRegionRequest struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	ParentRegionID string `json:"parentRegionId,omitempty"`
}

type updateRegionRequest struct {
	Name           *string `json:"name,omitempty"`
	ParentRegionID *string `json:"parentRegionId,omitempty"`
	Status         *string `json:"status,omitempty"`
}

func (h *Handler) listRegions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListRegionsParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if status, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if status != "" {
		params.Status = &status
	}
	rows, err := h.svc.q.ListRegions(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, rg := range rows {
		total = rg.TotalCount
		item := map[string]any{
			"id": rg.PublicID, "code": rg.Code, "name": rg.Name, "status": rg.Status,
		}
		if rg.ParentCode != nil {
			item["parent"] = map[string]string{"code": *rg.ParentCode, "name": derefStr(rg.ParentName)}
		}
		items = append(items, item)
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "code", "asc"))
}

func (h *Handler) createRegion(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createRegionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	v.PublicID("parentRegionId", req.ParentRegionID, publicid.PrefixRegion, false)
	if err := v.Err(); err != nil {
		return err
	}

	var parentID *int64
	if req.ParentRegionID != "" {
		parent, pErr := h.svc.q.GetRegionByPublicID(r.Context(), dbgen.GetRegionByPublicIDParams{
			PublicID: req.ParentRegionID, OrganizationID: p.OrganizationID,
		})
		if pErr != nil {
			if database.IsNoRows(pErr) {
				return apierr.Validation("The parent region does not exist.", nil)
			}
			return apierr.Internal(pErr)
		}
		parentID = &parent.ID
	}

	region, err := h.svc.q.CreateRegion(r.Context(), dbgen.CreateRegionParams{
		PublicID: publicid.New(publicid.PrefixRegion), OrganizationID: p.OrganizationID,
		Code: code, Name: name, ParentRegionID: parentID,
	})
	if err != nil {
		if database.IsUniqueViolation(err, "regions_code_unique") {
			return apierr.Duplicate("A region with this code already exists.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRegionCreated, ResourceType: "region",
		ResourceID: &region.ID, ResourcePublicID: region.PublicID,
		After: map[string]any{"code": region.Code, "name": region.Name},
	}))
	return httpx.Created(w, "/api/v1/network/regions/"+region.PublicID, map[string]any{
		"id": region.PublicID, "code": region.Code, "name": region.Name, "status": region.Status,
	})
}

func (h *Handler) updateRegion(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	regionID, err := httpx.PathPublicID(r, "regionId", publicid.PrefixRegion, "Region")
	if err != nil {
		return err
	}
	var req updateRegionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	params := dbgen.UpdateRegionParams{PublicID: regionID, OrganizationID: p.OrganizationID}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 120, true)
		params.Name = &n
	}
	if req.Status != nil {
		st := v.Enum("status", *req.Status, []string{"ACTIVE", "INACTIVE"}, true)
		params.Status = &st
	}
	if err := v.Err(); err != nil {
		return err
	}
	if req.ParentRegionID != nil && *req.ParentRegionID != "" {
		parent, pErr := h.svc.q.GetRegionByPublicID(r.Context(), dbgen.GetRegionByPublicIDParams{
			PublicID: *req.ParentRegionID, OrganizationID: p.OrganizationID,
		})
		if pErr != nil {
			return apierr.Validation("The parent region does not exist.", nil)
		}
		params.ParentRegionID = &parent.ID
	}
	region, err := h.svc.q.UpdateRegion(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Region")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRegionUpdated, ResourceType: "region",
		ResourceID: &region.ID, ResourcePublicID: region.PublicID,
		After: map[string]any{"name": region.Name, "status": region.Status},
	}))
	return httpx.OK(w, map[string]any{
		"id": region.PublicID, "code": region.Code, "name": region.Name, "status": region.Status,
	})
}

// ---- operating units -------------------------------------------------------

type createUnitRequest struct {
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	UnitType       string         `json:"unitType"`
	ParentUnitID   string         `json:"parentUnitId,omitempty"`
	RegionID       string         `json:"regionId,omitempty"`
	AddressLine1   string         `json:"addressLine1"`
	AddressLine2   string         `json:"addressLine2,omitempty"`
	Landmark       string         `json:"landmark,omitempty"`
	Pincode        string         `json:"pincode"`
	Latitude       *float64       `json:"latitude,omitempty"`
	Longitude      *float64       `json:"longitude,omitempty"`
	ContactName    string         `json:"contactName,omitempty"`
	ContactPhone   string         `json:"contactPhone,omitempty"`
	ContactEmail   string         `json:"contactEmail,omitempty"`
	OperatingHours map[string]any `json:"operatingHours,omitempty"`
	EffectiveFrom  *time.Time     `json:"effectiveFrom,omitempty"`
	Capabilities   []string       `json:"capabilities,omitempty"`
}

type updateUnitRequest struct {
	Name            *string        `json:"name,omitempty"`
	ParentUnitID    *string        `json:"parentUnitId,omitempty"`
	RegionID        *string        `json:"regionId,omitempty"`
	AddressLine1    *string        `json:"addressLine1,omitempty"`
	AddressLine2    *string        `json:"addressLine2,omitempty"`
	Landmark        *string        `json:"landmark,omitempty"`
	Pincode         *string        `json:"pincode,omitempty"`
	Latitude        *float64       `json:"latitude,omitempty"`
	Longitude       *float64       `json:"longitude,omitempty"`
	ContactName     *string        `json:"contactName,omitempty"`
	ContactPhone    *string        `json:"contactPhone,omitempty"`
	ContactEmail    *string        `json:"contactEmail,omitempty"`
	OperatingHours  map[string]any `json:"operatingHours,omitempty"`
	Status          *string        `json:"status,omitempty"`
	ExpectedVersion int32          `json:"expectedVersion"`
}

func (h *Handler) listUnits(w http.ResponseWriter, r *http.Request) error {
	return h.listUnitsFiltered(w, r, nil)
}

func (h *Handler) listHubs(w http.ResponseWriter, r *http.Request) error {
	return h.listUnitsFiltered(w, r, HubTypes)
}

func (h *Handler) listBranches(w http.ResponseWriter, r *http.Request) error {
	return h.listUnitsFiltered(w, r, BranchTypes)
}

func (h *Handler) listUnitsFiltered(w http.ResponseWriter, r *http.Request, forcedTypes []string) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListOperatingUnitsParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	types := forcedTypes
	if types == nil {
		requested, tErr := httpx.QueryEnumList(r, "unitType", UnitTypes, len(UnitTypes))
		if tErr != nil {
			return tErr
		}
		types = requested
	}
	if len(types) > 0 {
		params.UnitTypes = types
	}
	if status, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE", "SUSPENDED"}); sErr != nil {
		return sErr
	} else if status != "" {
		params.Status = &status
	}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}
	if pin := httpx.Query(r, "pincode"); pin != "" {
		params.Pincode = &pin
	}
	// A principal whose roles are all unit-scoped only sees its own facilities.
	if scope := p.UnitScope("operating_unit.manage"); scope != nil {
		params.ScopedUnitIds = scope
	}

	rows, err := h.svc.q.ListOperatingUnits(r.Context(), params)
	if err != nil {
		return apierr.Internal(fmt.Errorf("list operating units: %w", err))
	}
	var total int64
	items := make([]UnitView, 0, len(rows))
	for _, u := range rows {
		total = u.TotalCount
		view := UnitView{
			ID: u.PublicID, Code: u.Code, Name: u.Name, UnitType: u.UnitType, Status: u.Status,
			Address: AddressView{
				Line1: u.AddressLine1, Line2: derefStr(u.AddressLine2), Landmark: derefStr(u.Landmark),
				City: derefStr(u.CityName), State: derefStr(u.StateName), Pincode: u.Pincode,
				Latitude: u.Latitude, Longitude: u.Longitude,
			},
			Contact: ContactView{
				Name: derefStr(u.ContactName), Phone: derefStr(u.ContactPhone), Email: derefStr(u.ContactEmail),
			},
			EffectiveFrom: u.EffectiveFrom, EffectiveTo: u.EffectiveTo,
			Version: u.Version, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
			RegionCode: derefStr(u.RegionCode),
		}
		if u.ParentCode != nil {
			view.Parent = &UnitRef{Code: *u.ParentCode, Name: derefStr(u.ParentName)}
		}
		items = append(items, view)
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "code", "asc"))
}

func (h *Handler) getUnit(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	unitID, err := httpx.PathPublicID(r, "unitId", publicid.PrefixOperatingUnit, "Operating unit")
	if err != nil {
		return err
	}
	u, err := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: unitID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Operating unit")
		}
		return apierr.Internal(err)
	}
	caps, err := h.svc.q.ListOperatingUnitCapabilities(r.Context(), u.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	view := UnitView{
		ID: u.PublicID, Code: u.Code, Name: u.Name, UnitType: u.UnitType, Status: u.Status,
		RegionCode: derefStr(u.RegionCode),
		Address: AddressView{
			Line1: u.AddressLine1, Line2: derefStr(u.AddressLine2), Landmark: derefStr(u.Landmark),
			City: derefStr(u.CityName), State: derefStr(u.StateName), Pincode: u.Pincode,
			Latitude: u.Latitude, Longitude: u.Longitude,
		},
		Contact: ContactView{
			Name: derefStr(u.ContactName), Phone: derefStr(u.ContactPhone), Email: derefStr(u.ContactEmail),
		},
		EffectiveFrom: u.EffectiveFrom, EffectiveTo: u.EffectiveTo,
		Version: u.Version, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
	view.OperatingHours = decodeJSONMap(u.OperatingHours)
	if u.ParentPublicID != nil {
		view.Parent = &UnitRef{
			ID: *u.ParentPublicID, Code: derefStr(u.ParentCode),
			Name: derefStr(u.ParentName), UnitType: derefStr(u.ParentUnitType),
		}
	}
	for _, c := range caps {
		view.Capabilities = append(view.Capabilities, CapabilityView{Capability: c.Capability, Enabled: c.Enabled})
	}
	return httpx.OK(w, view)
}

func (h *Handler) createUnit(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createUnitRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 160, true)
	unitType := v.Enum("unitType", req.UnitType, UnitTypes, true)
	line1 := v.Text("addressLine1", req.AddressLine1, 3, 200, true)
	pin := v.Pincode("pincode", req.Pincode)
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	phone := v.Phone("contactPhone", req.ContactPhone, false)
	var email string
	if req.ContactEmail != "" {
		email = v.Email("contactEmail", req.ContactEmail)
	}
	for i, c := range req.Capabilities {
		v.Enum(fmt.Sprintf("capabilities[%d]", i), c, Capabilities, true)
	}
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.CreateOperatingUnitParams{
		PublicID: publicid.New(publicid.PrefixOperatingUnit), OrganizationID: p.OrganizationID,
		Code: code, Name: name, UnitType: unitType,
		AddressLine1: line1, AddressLine2: optional(req.AddressLine2), Landmark: optional(req.Landmark),
		Pincode: pin, Latitude: req.Latitude, Longitude: req.Longitude,
		ContactName: optional(req.ContactName), ContactPhone: optional(phone), ContactEmail: optional(email),
		OperatingHours: encodeJSONMap(req.OperatingHours),
		EffectiveFrom:  time.Now(),
		CreatedBy:      &p.UserID,
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}

	// The PIN code must exist in the shared dataset so routing and
	// serviceability can join on it later.
	// The organization's own market, not a hardcoded one: a Nigerian tenant
	// creating a Lagos branch was refused because the lookup only ever asked
	// for Indian postcodes.
	pincodeID, err := h.svc.q.GetPincodeIDByCode(r.Context(), dbgen.GetPincodeIDByCodeParams{
		Code: pin, CountryIso2: orgCountry(p),
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Validation("This postal code is not present in the platform dataset.",
				map[string]any{"pincode": pin})
		}
		return apierr.Internal(err)
	}
	params.PincodeID = &pincodeID

	if req.ParentUnitID != "" {
		parent, pErr := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: req.ParentUnitID, OrganizationID: p.OrganizationID,
		})
		if pErr != nil {
			return apierr.Validation("The parent operating unit does not exist.", nil)
		}
		params.ParentUnitID = &parent.ID
	}
	if req.RegionID != "" {
		region, rErr := h.svc.q.GetRegionByPublicID(r.Context(), dbgen.GetRegionByPublicIDParams{
			PublicID: req.RegionID, OrganizationID: p.OrganizationID,
		})
		if rErr != nil {
			return apierr.Validation("The region does not exist.", nil)
		}
		params.RegionID = &region.ID
	}

	unit, err := h.svc.q.CreateOperatingUnit(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err, "operating_units_code_unique") {
			return apierr.Duplicate("An operating unit with this code already exists.")
		}
		return apierr.Internal(fmt.Errorf("create operating unit: %w", err))
	}
	for _, c := range req.Capabilities {
		if _, cErr := h.svc.q.SetOperatingUnitCapability(r.Context(), dbgen.SetOperatingUnitCapabilityParams{
			OrganizationID: p.OrganizationID, OperatingUnitID: unit.ID,
			Capability: c, Enabled: true, Metadata: []byte("{}"),
		}); cErr != nil {
			return apierr.Internal(cErr)
		}
	}

	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionOperatingUnitCreated, ResourceType: "operating_unit",
		ResourceID: &unit.ID, ResourcePublicID: unit.PublicID, OperatingUnitID: &unit.ID,
		After: map[string]any{"code": unit.Code, "name": unit.Name, "unitType": unit.UnitType},
	}))
	return httpx.Created(w, "/api/v1/network/operating-units/"+unit.PublicID, map[string]any{
		"id": unit.PublicID, "code": unit.Code, "name": unit.Name,
		"unitType": unit.UnitType, "status": unit.Status, "version": unit.Version,
	})
}

func (h *Handler) updateUnit(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	unitID, err := httpx.PathPublicID(r, "unitId", publicid.PrefixOperatingUnit, "Operating unit")
	if err != nil {
		return err
	}
	var req updateUnitRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.ExpectedVersion < 1 {
		return apierr.Validation("expectedVersion is required and must be the version you last read.", nil)
	}

	existing, err := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: unitID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Operating unit")
		}
		return apierr.Internal(err)
	}

	v := validate.New()
	params := dbgen.UpdateOperatingUnitParams{
		PublicID: unitID, OrganizationID: p.OrganizationID, ExpectedVersion: req.ExpectedVersion,
	}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 160, true)
		params.Name = &n
	}
	if req.AddressLine1 != nil {
		l := v.Text("addressLine1", *req.AddressLine1, 3, 200, true)
		params.AddressLine1 = &l
	}
	params.AddressLine2 = req.AddressLine2
	params.Landmark = req.Landmark
	if req.Pincode != nil {
		pin := v.Pincode("pincode", *req.Pincode)
		params.Pincode = &pin
		if pinID, pErr := h.svc.q.GetPincodeIDByCode(r.Context(), dbgen.GetPincodeIDByCodeParams{
			Code: pin, CountryIso2: orgCountry(p),
		}); pErr == nil {
			params.PincodeID = &pinID
		} else {
			v.Add("pincode", "This postal code is not present in the platform dataset.")
		}
	}
	v.Latitude("latitude", req.Latitude)
	v.Longitude("longitude", req.Longitude)
	params.Latitude = req.Latitude
	params.Longitude = req.Longitude
	params.ContactName = req.ContactName
	if req.ContactPhone != nil {
		ph := v.Phone("contactPhone", *req.ContactPhone, false)
		params.ContactPhone = &ph
	}
	if req.ContactEmail != nil {
		em := v.Email("contactEmail", *req.ContactEmail)
		params.ContactEmail = &em
	}
	if req.OperatingHours != nil {
		params.OperatingHours = encodeJSONMap(req.OperatingHours)
	}
	if req.Status != nil {
		st := v.Enum("status", *req.Status, []string{"ACTIVE", "SUSPENDED"}, true)
		params.Status = &st
	}
	if req.ParentUnitID != nil {
		if *req.ParentUnitID == "" {
			params.ClearParent = true
		} else {
			parent, pErr := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
				PublicID: *req.ParentUnitID, OrganizationID: p.OrganizationID,
			})
			if pErr != nil {
				v.Add("parentUnitId", "The parent operating unit does not exist.")
			} else if parent.ID == existing.ID {
				v.Add("parentUnitId", "A unit cannot be its own parent.")
			} else {
				params.ParentUnitID = &parent.ID
			}
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	unit, err := h.svc.q.UpdateOperatingUnit(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			// Either the row is gone or the version moved on. Distinguishing the
			// two costs another query; the concurrency answer is the useful one.
			return apierr.Conflict(apierr.CodeConcurrentModification,
				"This operating unit was modified by someone else. Reload it and try again.").
				WithDetail("expectedVersion", req.ExpectedVersion).
				WithDetail("currentVersion", existing.Version)
		}
		return apierr.Internal(err)
	}

	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionOperatingUnitUpdated, ResourceType: "operating_unit",
		ResourceID: &unit.ID, ResourcePublicID: unit.PublicID, OperatingUnitID: &unit.ID,
		Before: map[string]any{"name": existing.Name, "status": existing.Status, "pincode": existing.Pincode},
		After:  map[string]any{"name": unit.Name, "status": unit.Status, "pincode": unit.Pincode},
	}))
	return httpx.OK(w, map[string]any{
		"id": unit.PublicID, "code": unit.Code, "name": unit.Name,
		"unitType": unit.UnitType, "status": unit.Status, "version": unit.Version,
	})
}

type deactivateRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) deactivateUnit(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	unitID, err := httpx.PathPublicID(r, "unitId", publicid.PrefixOperatingUnit, "Operating unit")
	if err != nil {
		return err
	}
	var req deactivateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	reason := v.Text("reason", req.Reason, 5, 500, true)
	if err := v.Err(); err != nil {
		return err
	}

	existing, err := h.svc.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: unitID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Operating unit")
		}
		return apierr.Internal(err)
	}

	// Constitution §M02: never hard-delete a unit referenced by transactional
	// records, and refuse to orphan children or break active configuration.
	refs, err := h.svc.q.CountOperatingUnitReferences(r.Context(), &existing.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	if refs.ChildCount > 0 {
		return apierr.Conflict(apierr.CodeResourceInUse,
			"This unit still has child units. Reassign or deactivate them first.").
			WithDetail("childUnits", refs.ChildCount)
	}
	if refs.ServiceAreaCount > 0 || refs.RouteCount > 0 {
		return apierr.Conflict(apierr.CodeResourceInUse,
			"This unit is still referenced by active service areas or routes. Remove them first.").
			WithDetail("activeServiceAreas", refs.ServiceAreaCount).
			WithDetail("activeRoutes", refs.RouteCount)
	}

	unit, err := h.svc.q.DeactivateOperatingUnit(r.Context(), dbgen.DeactivateOperatingUnitParams{
		PublicID: unitID, OrganizationID: p.OrganizationID, DeactivatedBy: &p.UserID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Conflict(apierr.CodeConflict, "This operating unit is already inactive.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionOperatingUnitDeactivated, ResourceType: "operating_unit",
		ResourceID: &unit.ID, ResourcePublicID: unit.PublicID, OperatingUnitID: &unit.ID,
		Reason: reason,
		Before: map[string]any{"status": existing.Status},
		After:  map[string]any{"status": unit.Status, "shipmentsRetained": refs.ShipmentCount},
	}))
	return httpx.OK(w, map[string]any{
		"id": unit.PublicID, "status": unit.Status,
		"retainedShipments": refs.ShipmentCount,
	})
}

func (h *Handler) hierarchy(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	params := dbgen.GetOperatingUnitHierarchyParams{
		OrganizationID: p.OrganizationID, MaxDepth: 10,
	}
	if root := httpx.Query(r, "rootId"); root != "" {
		if !publicid.Valid(publicid.PrefixOperatingUnit, root) {
			return apierr.Validation("rootId is not a valid operating unit identifier.", nil)
		}
		params.RootPublicID = &root
	}
	depth, err := httpx.QueryInt(r, "maxDepth", 10, 1, 20)
	if err != nil {
		return err
	}
	params.MaxDepth = int32(depth)

	rows, err := h.svc.q.GetOperatingUnitHierarchy(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, n := range rows {
		items = append(items, map[string]any{
			"id": n.PublicID, "code": n.Code, "name": n.Name, "unitType": n.UnitType,
			"status": n.Status, "depth": n.Depth, "path": n.Path, "pincode": n.Pincode,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

type setCapabilityRequest struct {
	Capability string `json:"capability"`
	Enabled    bool   `json:"enabled"`
}

func (h *Handler) listCapabilities(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	unitID, err := httpx.PathPublicID(r, "unitId", publicid.PrefixOperatingUnit, "Operating unit")
	if err != nil {
		return err
	}
	unit, err := h.svc.GetUnitByPublicID(r.Context(), p.OrganizationID, unitID)
	if err != nil {
		return err
	}
	rows, err := h.svc.q.ListOperatingUnitCapabilities(r.Context(), unit.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]CapabilityView, 0, len(rows))
	for _, c := range rows {
		out = append(out, CapabilityView{Capability: c.Capability, Enabled: c.Enabled})
	}
	return httpx.OK(w, map[string]any{"data": out, "available": Capabilities})
}

func (h *Handler) setCapability(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	unitID, err := httpx.PathPublicID(r, "unitId", publicid.PrefixOperatingUnit, "Operating unit")
	if err != nil {
		return err
	}
	var req setCapabilityRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	capability := v.Enum("capability", req.Capability, Capabilities, true)
	if err := v.Err(); err != nil {
		return err
	}
	unit, err := h.svc.GetUnitByPublicID(r.Context(), p.OrganizationID, unitID)
	if err != nil {
		return err
	}
	row, err := h.svc.q.SetOperatingUnitCapability(r.Context(), dbgen.SetOperatingUnitCapabilityParams{
		OrganizationID: p.OrganizationID, OperatingUnitID: unit.ID,
		Capability: capability, Enabled: req.Enabled, Metadata: []byte("{}"),
	})
	if err != nil {
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionCapabilityChanged, ResourceType: "operating_unit",
		ResourceID: &unit.ID, ResourcePublicID: unit.PublicID, OperatingUnitID: &unit.ID,
		After: map[string]any{"capability": capability, "enabled": req.Enabled},
	}))
	return httpx.OK(w, CapabilityView{Capability: row.Capability, Enabled: row.Enabled})
}

// ---- helpers ---------------------------------------------------------------

func listWindow(r *http.Request) (limit, page int, err error) {
	limit, err = pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return 0, 0, err
	}
	page, err = httpx.QueryInt(r, "page", 1, 1, 10_000)
	if err != nil {
		return 0, 0, err
	}
	return limit, page, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// orgCountry returns the market a principal's organization operates in, falling
// back to the platform default for a principal built before the column existed.
func orgCountry(p *tenant.Principal) string {
	if p != nil && p.OrganizationCountry != "" {
		return p.OrganizationCountry
	}
	return geography.DefaultCountry
}
