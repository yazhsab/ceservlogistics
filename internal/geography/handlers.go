package geography

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/go-chi/chi/v5"
)

// Handler exposes geography and zone endpoints.
type Handler struct {
	svc     *Service
	audit   *audit.Recorder
	imports *ImportService
}

// NewHandler builds the geography handler.
func NewHandler(svc *Service, rec *audit.Recorder, imp *ImportService) *Handler {
	return &Handler{svc: svc, audit: rec, imports: imp}
}

// Routes mounts geography endpoints under /geography.
func (h *Handler) Routes(r chi.Router) {
	read := auth.RequirePermission("geography.read")
	r.With(read).Get("/countries", httpx.Wrap(h.listCountries))
	r.With(read).Get("/states", httpx.Wrap(h.listStates))
	r.With(read).Get("/cities", httpx.Wrap(h.listCities))
	r.With(read).Get("/districts", httpx.Wrap(h.listDistricts))
	r.With(read).Get("/pincodes", httpx.Wrap(h.searchPincodes))
	r.With(read).Get("/pincodes/{code}", httpx.Wrap(h.getPincode))
	r.With(read).Get("/pincodes/{code}/localities", httpx.Wrap(h.listLocalities))

	// Resolving an area name to a postcode. Mounted last among the reads
	// because it is the one a booking form calls on every keystroke.
	r.With(read).Get("/places", httpx.Wrap(h.searchPlaces))

	// Global PIN code maintenance is restricted to the platform operator: the
	// dataset is shared by every tenant.
	r.With(auth.RequirePermission("pincode.manage")).Patch("/pincodes/{code}", httpx.Wrap(h.setPincodeStatus))

	r.Route("/zones", func(zr chi.Router) {
		zr.With(auth.RequirePermission("zone.read")).Get("/", httpx.Wrap(h.listZones))
		zr.With(auth.RequirePermission("zone.manage")).Post("/", httpx.Wrap(h.createZone))
		zr.With(auth.RequirePermission("zone.read")).Get("/{zoneId}", httpx.Wrap(h.getZone))
		zr.With(auth.RequirePermission("zone.manage")).Patch("/{zoneId}", httpx.Wrap(h.updateZone))
	})

	r.Route("/zone-mappings", func(zr chi.Router) {
		zr.With(auth.RequirePermission("zone.read")).Get("/", httpx.Wrap(h.listZoneMappings))
		zr.With(auth.RequirePermission("zone.manage")).Put("/", httpx.Wrap(h.upsertZoneMapping))
		zr.With(auth.RequirePermission("zone.manage")).Delete("/{mappingId}", httpx.Wrap(h.deleteZoneMapping))
	})

	h.imports.Routes(r)
}

// ---- reference lookups -----------------------------------------------------

type countryView struct {
	ID        string `json:"id"`
	ISO2      string `json:"iso2"`
	ISO3      string `json:"iso3"`
	Name      string `json:"name"`
	PhoneCode string `json:"phoneCode"`
	Currency  string `json:"currency"`
}

func (h *Handler) listCountries(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.svc.q.ListCountries(r.Context())
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]countryView, 0, len(rows))
	for _, c := range rows {
		out = append(out, countryView{
			ID: c.PublicID, ISO2: c.Iso2, ISO3: c.Iso3, Name: c.Name,
			PhoneCode: c.PhoneCode, Currency: c.Currency,
		})
	}
	return httpx.OK(w, map[string]any{"data": out})
}

type stateView struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	GSTStateCode string `json:"gstStateCode,omitempty"`
}

func (h *Handler) listStates(w http.ResponseWriter, r *http.Request) error {
	country := httpx.QueryDefault(r, "country", DefaultCountry)
	rows, err := h.svc.q.ListStates(r.Context(), country)
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]stateView, 0, len(rows))
	for _, s := range rows {
		v := stateView{ID: s.PublicID, Code: s.Code, Name: s.Name}
		if s.GstStateCode != nil {
			v.GSTStateCode = *s.GstStateCode
		}
		out = append(out, v)
	}
	return httpx.OK(w, map[string]any{"data": out})
}

type cityView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Tier  string `json:"tier"`
	State string `json:"state"`
}

func (h *Handler) listCities(w http.ResponseWriter, r *http.Request) error {
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 1000)
	if err != nil {
		return err
	}
	params := dbgen.ListCitiesParams{RowLimit: int32(limit), RowOffset: int32((page - 1) * limit)}
	if s := httpx.Query(r, "search"); s != "" {
		params.Search = &s
	}
	if sid := httpx.Query(r, "stateId"); sid != "" {
		state, sErr := h.resolveStateID(r, sid)
		if sErr != nil {
			return sErr
		}
		params.StateID = &state
	}
	rows, err := h.svc.q.ListCities(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]cityView, 0, len(rows))
	for _, c := range rows {
		out = append(out, cityView{ID: c.PublicID, Name: c.Name, Tier: c.Tier, State: c.StateName})
	}
	return httpx.OK(w, map[string]any{"data": out})
}

func (h *Handler) resolveStateID(r *http.Request, publicID string) (int64, error) {
	if !publicid.Valid(publicid.PrefixState, publicID) {
		return 0, apierr.Validation("stateId is not a valid state identifier.", nil)
	}
	rows, err := h.svc.q.ListStates(r.Context(), DefaultCountry)
	if err != nil {
		return 0, apierr.Internal(err)
	}
	for _, s := range rows {
		if s.PublicID == publicID {
			return s.ID, nil
		}
	}
	return 0, apierr.NotFound("State")
}

func (h *Handler) getPincode(w http.ResponseWriter, r *http.Request) error {
	code, err := httpx.PathParam(r, "code", 12)
	if err != nil {
		return err
	}
	country := httpx.QueryDefault(r, "country", DefaultCountry)
	p, err := h.svc.LookupPincode(r.Context(), code, country)
	if err != nil {
		return err
	}

	// Tenant users additionally see their own zone assignment, which is what
	// booking and pricing will actually use.
	body := map[string]any{"pincode": p.View()}
	if principal := tenant.FromContext(r.Context()); principal != nil {
		if zone, zErr := h.svc.ResolveZone(r.Context(), principal.OrganizationID, p.ID, time.Now()); zErr == nil {
			body["zone"] = zone
			body["effectiveRemote"] = zone.IsRemote
		} else {
			body["zone"] = nil
			body["effectiveRemote"] = p.IsRemote
		}
	}
	return httpx.OK(w, body)
}

type pincodeListItem struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	State      string `json:"state"`
	City       string `json:"city,omitempty"`
	CityTier   string `json:"cityTier,omitempty"`
	OfficeName string `json:"officeName,omitempty"`
	IsRemote   bool   `json:"isRemote"`
	Status     string `json:"status"`
}

func (h *Handler) searchPincodes(w http.ResponseWriter, r *http.Request) error {
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 1000)
	if err != nil {
		return err
	}
	params := dbgen.SearchPincodesParams{RowLimit: int32(limit), RowOffset: int32((page - 1) * limit)}
	if prefix := httpx.Query(r, "code"); prefix != "" {
		if len(prefix) > 12 {
			return apierr.Validation("code prefix is too long.", nil)
		}
		params.CodePrefix = &prefix
	}
	remote, err := httpx.QueryBool(r, "isRemote", false)
	if err != nil {
		return err
	}
	if httpx.Query(r, "isRemote") != "" {
		params.IsRemote = &remote
	}

	rows, err := h.svc.q.SearchPincodes(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]pincodeListItem, 0, len(rows))
	for _, p := range rows {
		total = p.TotalCount
		item := pincodeListItem{
			ID: p.PublicID, Code: p.Code, State: p.StateName,
			IsRemote: p.IsRemote, Status: p.Status,
		}
		if p.CityName != nil {
			item.City = *p.CityName
		}
		if p.CityTier != nil {
			item.CityTier = *p.CityTier
		}
		if p.OfficeName != nil {
			item.OfficeName = *p.OfficeName
		}
		items = append(items, item)
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "code", "asc"))
}

func (h *Handler) listLocalities(w http.ResponseWriter, r *http.Request) error {
	code, err := httpx.PathParam(r, "code", 12)
	if err != nil {
		return err
	}
	p, err := h.svc.LookupPincode(r.Context(), code, httpx.QueryDefault(r, "country", DefaultCountry))
	if err != nil {
		return err
	}
	rows, err := h.svc.q.ListLocalities(r.Context(), p.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	out := make([]map[string]string, 0, len(rows))
	for _, l := range rows {
		out = append(out, map[string]string{"id": l.PublicID, "name": l.Name})
	}
	return httpx.OK(w, map[string]any{"data": out})
}

type setPincodeStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) setPincodeStatus(w http.ResponseWriter, r *http.Request) error {
	code, err := httpx.PathParam(r, "code", 12)
	if err != nil {
		return err
	}
	var req setPincodeStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	status := v.Enum("status", req.Status, []string{"ACTIVE", "INACTIVE"}, true)
	if err := v.Err(); err != nil {
		return err
	}
	country := httpx.QueryDefault(r, "country", DefaultCountry)
	existing, err := h.svc.LookupPincode(r.Context(), code, country)
	if err != nil {
		return err
	}
	updated, err := h.svc.q.SetPincodeStatus(r.Context(), dbgen.SetPincodeStatusParams{
		PublicID: existing.PublicID, Status: status,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	h.svc.InvalidatePincode(r.Context(), country, code)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionPincodeUpdated, ResourceType: "pincode",
		ResourcePublicID: updated.PublicID, Reason: req.Reason,
		Before: map[string]any{"status": existing.Status},
		After:  map[string]any{"status": updated.Status},
	}))
	return httpx.OK(w, map[string]any{"code": updated.Code, "status": updated.Status})
}

// ---- zones -----------------------------------------------------------------

type zoneView struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	SortOrder   int32     `json:"sortOrder"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

var zoneTypes = []string{"LOCAL", "INTRA_STATE", "METRO", "REGIONAL", "NATIONAL", "SPECIAL", "INTERNATIONAL"}

type createZoneRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sortOrder,omitempty"`
}

type updateZoneRequest struct {
	Name        *string `json:"name,omitempty"`
	Type        *string `json:"type,omitempty"`
	Description *string `json:"description,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	Status      *string `json:"status,omitempty"`
}

func (h *Handler) listZones(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 1000)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"})
	if err != nil {
		return err
	}
	params := dbgen.ListZonesParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if status != "" {
		params.Status = &status
	}
	rows, err := h.svc.q.ListZones(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]zoneView, 0, len(rows))
	for _, z := range rows {
		total = z.TotalCount
		items = append(items, zoneView{
			ID: z.PublicID, Code: z.Code, Name: z.Name, Type: z.ZoneType,
			Description: z.Description, SortOrder: z.SortOrder, Status: z.Status, CreatedAt: z.CreatedAt,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "sortOrder", "asc"))
}

func (h *Handler) getZone(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	zonePublicID, err := httpx.PathPublicID(r, "zoneId", publicid.PrefixZone, "Zone")
	if err != nil {
		return err
	}
	z, err := h.svc.q.GetZoneByPublicID(r.Context(), dbgen.GetZoneByPublicIDParams{
		PublicID: zonePublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Zone")
		}
		return apierr.Internal(err)
	}
	refs, err := h.svc.q.CountZoneReferences(r.Context(), z.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	return httpx.OK(w, map[string]any{
		"zone": zoneView{
			ID: z.PublicID, Code: z.Code, Name: z.Name, Type: z.ZoneType,
			Description: z.Description, SortOrder: z.SortOrder, Status: z.Status, CreatedAt: z.CreatedAt,
		},
		"usage": map[string]any{
			"pincodeMappings": refs.MappingCount,
			"rateCardEntries": refs.RateCount,
		},
	})
}

func (h *Handler) createZone(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createZoneRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 120, true)
	zoneType := v.Enum("type", req.Type, zoneTypes, true)
	description := v.Text("description", req.Description, 0, 500, false)
	v.IntRange("sortOrder", req.SortOrder, 0, 10_000)
	if err := v.Err(); err != nil {
		return err
	}
	z, err := h.svc.q.CreateZone(r.Context(), dbgen.CreateZoneParams{
		PublicID: publicid.New(publicid.PrefixZone), OrganizationID: p.OrganizationID,
		Code: code, Name: name, ZoneType: zoneType, Description: description,
		SortOrder: int32(req.SortOrder),
	})
	if err != nil {
		if database.IsUniqueViolation(err, "zones_code_unique") {
			return apierr.Duplicate("A zone with this code already exists.")
		}
		return apierr.Internal(err)
	}
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionZoneCreated, ResourceType: "zone",
		ResourceID: &z.ID, ResourcePublicID: z.PublicID,
		After: map[string]any{"code": z.Code, "name": z.Name, "type": z.ZoneType},
	}))
	return httpx.Created(w, "/api/v1/geography/zones/"+z.PublicID, zoneView{
		ID: z.PublicID, Code: z.Code, Name: z.Name, Type: z.ZoneType,
		Description: z.Description, SortOrder: z.SortOrder, Status: z.Status, CreatedAt: z.CreatedAt,
	})
}

func (h *Handler) updateZone(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	zonePublicID, err := httpx.PathPublicID(r, "zoneId", publicid.PrefixZone, "Zone")
	if err != nil {
		return err
	}
	var req updateZoneRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	params := dbgen.UpdateZoneParams{PublicID: zonePublicID, OrganizationID: p.OrganizationID}
	if req.Name != nil {
		name := v.Text("name", *req.Name, 2, 120, true)
		params.Name = &name
	}
	if req.Type != nil {
		t := v.Enum("type", *req.Type, zoneTypes, true)
		params.ZoneType = &t
	}
	if req.Description != nil {
		d := v.Text("description", *req.Description, 0, 500, false)
		params.Description = &d
	}
	if req.SortOrder != nil {
		v.IntRange("sortOrder", *req.SortOrder, 0, 10_000)
		so := int32(*req.SortOrder)
		params.SortOrder = &so
	}
	if req.Status != nil {
		st := v.Enum("status", *req.Status, []string{"ACTIVE", "INACTIVE"}, true)
		params.Status = &st
	}
	if err := v.Err(); err != nil {
		return err
	}

	z, err := h.svc.q.UpdateZone(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Zone")
		}
		return apierr.Internal(err)
	}
	// Zone attributes feed pricing lookups indirectly through mappings, so the
	// tenant's cached zone decisions are dropped wholesale.
	h.svc.InvalidateOrganizationZones(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionZoneUpdated, ResourceType: "zone",
		ResourceID: &z.ID, ResourcePublicID: z.PublicID,
		After: map[string]any{"name": z.Name, "type": z.ZoneType, "status": z.Status},
	}))
	return httpx.OK(w, zoneView{
		ID: z.PublicID, Code: z.Code, Name: z.Name, Type: z.ZoneType,
		Description: z.Description, SortOrder: z.SortOrder, Status: z.Status, CreatedAt: z.CreatedAt,
	})
}

// ---- zone mappings ---------------------------------------------------------

type upsertZoneMappingRequest struct {
	CountryCode string `json:"countryCode,omitempty"`
	Pincode     string `json:"pincode"`
	ZoneCode    string `json:"zoneCode"`
	IsRemote    *bool  `json:"isRemote,omitempty"`
}

func (h *Handler) upsertZoneMapping(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req upsertZoneMappingRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	country := v.CountryCode("countryCode", validate.AddressCountry(req.CountryCode, p.OrganizationCountry), true)
	pin := v.PostalCode("pincode", req.Pincode, country)
	zoneCode := v.Code("zoneCode", req.ZoneCode)
	if err := v.Err(); err != nil {
		return err
	}

	pincode, err := h.svc.LookupPincode(r.Context(), pin, country)
	if err != nil {
		return err
	}
	zone, err := h.svc.q.GetZoneByCode(r.Context(), dbgen.GetZoneByCodeParams{
		OrganizationID: p.OrganizationID, Code: zoneCode,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Validation("The zone does not exist in your organization.",
				map[string]any{"zoneCode": zoneCode})
		}
		return apierr.Internal(err)
	}
	if zone.Status != "ACTIVE" {
		return apierr.Validation("The zone is inactive.", map[string]any{"zoneCode": zoneCode})
	}

	mapping, err := h.svc.q.UpsertPincodeZoneMapping(r.Context(), dbgen.UpsertPincodeZoneMappingParams{
		PublicID: publicid.New(publicid.PrefixZoneMapping), OrganizationID: p.OrganizationID,
		PincodeID: pincode.ID, ZoneID: zone.ID, IsRemoteOverride: req.IsRemote,
		EffectiveFrom: time.Now(), CreatedBy: &p.UserID,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("upsert zone mapping: %w", err))
	}
	h.svc.InvalidateZoneMapping(r.Context(), p.OrganizationID, pincode.ID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionZoneMappingChanged, ResourceType: "pincode_zone_mapping",
		ResourceID: &mapping.ID, ResourcePublicID: mapping.PublicID,
		After: map[string]any{"pincode": pin, "zoneCode": zoneCode, "isRemote": req.IsRemote},
	}))
	return httpx.OK(w, map[string]any{
		"id": mapping.PublicID, "pincode": pin, "zoneCode": zoneCode,
		"isRemoteOverride": mapping.IsRemoteOverride, "effectiveFrom": mapping.EffectiveFrom,
	})
}

func (h *Handler) listZoneMappings(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 10_000)
	if err != nil {
		return err
	}
	params := dbgen.ListPincodeZoneMappingsParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if prefix := httpx.Query(r, "pincode"); prefix != "" {
		params.PincodePrefix = &prefix
	}
	if zoneCode := httpx.Query(r, "zoneCode"); zoneCode != "" {
		zone, zErr := h.svc.q.GetZoneByCode(r.Context(), dbgen.GetZoneByCodeParams{
			OrganizationID: p.OrganizationID, Code: zoneCode,
		})
		if zErr != nil {
			if database.IsNoRows(zErr) {
				return apierr.NotFound("Zone")
			}
			return apierr.Internal(zErr)
		}
		params.ZoneID = &zone.ID
	}
	rows, err := h.svc.q.ListPincodeZoneMappings(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		total = m.TotalCount
		effectiveRemote := m.PincodeIsRemote
		if m.IsRemoteOverride != nil {
			effectiveRemote = *m.IsRemoteOverride
		}
		item := map[string]any{
			"id": m.PublicID, "pincode": m.Pincode, "zoneCode": m.ZoneCode, "zoneName": m.ZoneName,
			"state": m.StateName, "isRemoteOverride": m.IsRemoteOverride,
			"effectiveRemote": effectiveRemote, "effectiveFrom": m.EffectiveFrom,
		}
		if m.CityName != nil {
			item["city"] = *m.CityName
		}
		items = append(items, item)
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "pincode", "asc"))
}

func (h *Handler) deleteZoneMapping(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	mappingID, err := httpx.PathPublicID(r, "mappingId", publicid.PrefixZoneMapping, "Zone mapping")
	if err != nil {
		return err
	}
	affected, err := h.svc.q.DeactivatePincodeZoneMapping(r.Context(), dbgen.DeactivatePincodeZoneMappingParams{
		PublicID: mappingID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	if affected == 0 {
		return apierr.NotFound("Zone mapping")
	}
	h.svc.InvalidateOrganizationZones(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionZoneMappingChanged, ResourceType: "pincode_zone_mapping",
		ResourcePublicID: mappingID, After: map[string]any{"status": "INACTIVE"},
	}))
	return httpx.NoContent(w)
}
