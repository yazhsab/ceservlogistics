package serviceability

import (
	"encoding/json"
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

// Handler exposes serviceability lookup, configuration and the debug endpoint.
type Handler struct {
	resolver *Resolver
	db       *database.DB
	q        *dbgen.Queries
	geo      *geography.Service
	audit    *audit.Recorder
}

// NewHandler builds the serviceability handler.
func NewHandler(res *Resolver, db *database.DB, q *dbgen.Queries, geo *geography.Service, rec *audit.Recorder) *Handler {
	return &Handler{resolver: res, db: db, q: q, geo: geo, audit: rec}
}

// Routes mounts serviceability endpoints under /serviceability.
func (h *Handler) Routes(r chi.Router) {
	r.With(auth.RequirePermission("serviceability.check")).Post("/check", httpx.Wrap(h.check))
	r.With(auth.RequirePermission("serviceability.check")).Get("/check", httpx.Wrap(h.checkQuery))

	r.Route("/service-areas", func(sr chi.Router) {
		sr.With(auth.RequirePermission("service_area.read")).Get("/", httpx.Wrap(h.listServiceAreas))
		sr.With(auth.RequirePermission("service_area.manage")).Post("/", httpx.Wrap(h.createServiceArea))
		sr.With(auth.RequirePermission("service_area.manage")).Patch("/{areaId}", httpx.Wrap(h.updateServiceArea))
	})

	r.Route("/rules", func(rr chi.Router) {
		rr.With(auth.RequirePermission("route.read")).Get("/", httpx.Wrap(h.listRules))
		rr.With(auth.RequirePermission("route.manage")).Post("/", httpx.Wrap(h.createRule))
		rr.With(auth.RequirePermission("route.manage")).Patch("/{ruleId}", httpx.Wrap(h.setRuleStatus))
	})

	r.Route("/closures", func(cr chi.Router) {
		cr.With(auth.RequirePermission("route.read")).Get("/", httpx.Wrap(h.listClosures))
		cr.With(auth.RequirePermission("closure.manage")).Post("/", httpx.Wrap(h.createClosure))
		cr.With(auth.RequirePermission("closure.manage")).Delete("/{closureId}", httpx.Wrap(h.cancelClosure))
	})

	// Administrator-only decision tracing.
	r.With(auth.RequirePermission("routing.debug")).Post("/debug", httpx.Wrap(h.debug))
	r.With(auth.RequirePermission("routing.debug")).Get("/explanations/{explanationId}", httpx.Wrap(h.getExplanation))
}

// RouteRoutes mounts route configuration under /routes.
func (h *Handler) RouteRoutes(r chi.Router) {
	r.With(auth.RequirePermission("route.read")).Get("/", httpx.Wrap(h.listRoutes))
	r.With(auth.RequirePermission("route.manage")).Post("/", httpx.Wrap(h.createRoute))
	r.With(auth.RequirePermission("route.read")).Get("/{routeId}", httpx.Wrap(h.getRoute))
	r.With(auth.RequirePermission("route.manage")).Patch("/{routeId}", httpx.Wrap(h.updateRoute))
	r.Route("/overrides", func(or chi.Router) {
		or.With(auth.RequirePermission("route.read")).Get("/", httpx.Wrap(h.listOverrides))
		or.With(auth.RequirePermission("route.manage")).Post("/", httpx.Wrap(h.createOverride))
		or.With(auth.RequirePermission("route.manage")).Delete("/{overrideId}", httpx.Wrap(h.cancelOverride))
	})
}

// ---- lookup ----------------------------------------------------------------

type checkRequest struct {
	OriginPincode      string     `json:"originPincode"`
	DestinationPincode string     `json:"destinationPincode"`
	ServiceCode        string     `json:"serviceCode"`
	At                 *time.Time `json:"at,omitempty"`
}

func (h *Handler) check(w http.ResponseWriter, r *http.Request) error {
	var req checkRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	return h.runCheck(w, r, req, false)
}

func (h *Handler) checkQuery(w http.ResponseWriter, r *http.Request) error {
	at, err := httpx.QueryTime(r, "at")
	if err != nil {
		return err
	}
	return h.runCheck(w, r, checkRequest{
		OriginPincode:      httpx.Query(r, "originPincode"),
		DestinationPincode: httpx.Query(r, "destinationPincode"),
		ServiceCode:        httpx.Query(r, "serviceCode"),
		At:                 at,
	}, false)
}

func (h *Handler) debug(w http.ResponseWriter, r *http.Request) error {
	var req checkRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	return h.runCheck(w, r, req, true)
}

func (h *Handler) runCheck(w http.ResponseWriter, r *http.Request, req checkRequest, explain bool) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	v := validate.New()
	origin := v.Pincode("originPincode", req.OriginPincode)
	dest := v.Pincode("destinationPincode", req.DestinationPincode)
	service := v.Code("serviceCode", req.ServiceCode)
	if err := v.Err(); err != nil {
		return err
	}
	at := time.Now()
	if req.At != nil {
		at = *req.At
	}
	res, err := h.resolver.Resolve(r.Context(), Request{
		OrganizationID: p.OrganizationID,
		OriginPincode:  origin, DestPincode: dest, ServiceCode: service,
		At: at, IncludeExplain: explain,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, res)
}

func (h *Handler) getExplanation(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathPublicID(r, "explanationId", publicid.PrefixExplanation, "Routing explanation")
	if err != nil {
		return err
	}
	exp, err := h.resolver.GetExplanation(r.Context(), id)
	if err != nil {
		return err
	}
	return httpx.OK(w, exp)
}

// ---- service areas ---------------------------------------------------------

var areaTypes = []string{"PICKUP", "DELIVERY", "BOTH"}

type createServiceAreaRequest struct {
	OperatingUnitID string     `json:"operatingUnitId"`
	Pincode         string     `json:"pincode"`
	AreaType        string     `json:"areaType"`
	Priority        int        `json:"priority,omitempty"`
	IsRemote        bool       `json:"isRemote,omitempty"`
	CutoffTime      string     `json:"cutoffTime,omitempty"`
	EffectiveFrom   *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo     *time.Time `json:"effectiveTo,omitempty"`
}

type updateServiceAreaRequest struct {
	Priority    *int       `json:"priority,omitempty"`
	IsRemote    *bool      `json:"isRemote,omitempty"`
	CutoffTime  *string    `json:"cutoffTime,omitempty"`
	EffectiveTo *time.Time `json:"effectiveTo,omitempty"`
	Status      *string    `json:"status,omitempty"`
}

func (h *Handler) listServiceAreas(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListServiceAreasParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if pin := httpx.Query(r, "pincode"); pin != "" {
		params.Pincode = &pin
	}
	if at, aErr := httpx.QueryEnum(r, "areaType", areaTypes); aErr != nil {
		return aErr
	} else if at != "" {
		params.AreaType = &at
	}
	if st, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	}
	if unitID := httpx.Query(r, "operatingUnitId"); unitID != "" {
		unit, uErr := h.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
			PublicID: unitID, OrganizationID: p.OrganizationID,
		})
		if uErr != nil {
			return apierr.NotFound("Operating unit")
		}
		params.OperatingUnitID = &unit.ID
	}

	rows, err := h.q.ListServiceAreas(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		total = a.TotalCount
		items = append(items, map[string]any{
			"id": a.PublicID, "pincode": a.Pincode, "areaType": a.AreaType,
			"priority": a.Priority, "isRemote": a.IsRemote, "cutoffTime": a.CutoffTime,
			"status": a.Status, "effectiveFrom": a.EffectiveFrom, "effectiveTo": a.EffectiveTo,
			"operatingUnit": map[string]any{"code": a.UnitCode, "name": a.UnitName, "unitType": a.UnitType},
			"city":          a.CityName, "state": a.StateName,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "pincode", "asc"))
}

func (h *Handler) createServiceArea(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createServiceAreaRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	unitPublicID := v.PublicID("operatingUnitId", req.OperatingUnitID, publicid.PrefixOperatingUnit, true)
	pin := v.Pincode("pincode", req.Pincode)
	areaType := v.Enum("areaType", req.AreaType, areaTypes, true)
	if req.Priority == 0 {
		req.Priority = 100
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	if err := v.Err(); err != nil {
		return err
	}

	unit, err := h.q.GetOperatingUnitByPublicID(r.Context(), dbgen.GetOperatingUnitByPublicIDParams{
		PublicID: unitPublicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Validation("The operating unit does not exist.", nil)
		}
		return apierr.Internal(err)
	}
	pincode, err := h.geo.LookupPincode(r.Context(), pin, geography.DefaultCountry)
	if err != nil {
		return err
	}

	params := dbgen.CreateServiceAreaParams{
		PublicID: publicid.New(publicid.PrefixServiceArea), OrganizationID: p.OrganizationID,
		OperatingUnitID: unit.ID, PincodeID: pincode.ID, AreaType: areaType,
		Priority: int32(req.Priority), IsRemote: req.IsRemote,
		EffectiveFrom: time.Now(), EffectiveTo: req.EffectiveTo, CreatedBy: &p.UserID,
	}
	if req.CutoffTime != "" {
		params.CutoffTime = &req.CutoffTime
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}

	area, err := h.q.CreateServiceArea(r.Context(), params)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return apierr.Duplicate("This unit already serves that PIN code for the selected area type.")
		}
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionServiceAreaCreated, ResourceType: "service_area",
		ResourceID: &area.ID, ResourcePublicID: area.PublicID, OperatingUnitID: &unit.ID,
		After: map[string]any{"pincode": pin, "areaType": areaType, "priority": req.Priority},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": area.PublicID, "pincode": pin, "areaType": area.AreaType,
		"priority": area.Priority, "isRemote": area.IsRemote, "status": area.Status,
	})
}

func (h *Handler) updateServiceArea(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	areaID, err := httpx.PathPublicID(r, "areaId", publicid.PrefixServiceArea, "Service area")
	if err != nil {
		return err
	}
	var req updateServiceAreaRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	params := dbgen.UpdateServiceAreaParams{PublicID: areaID, OrganizationID: p.OrganizationID}
	if req.Priority != nil {
		v.IntRange("priority", *req.Priority, 0, 1000)
		pr := int32(*req.Priority)
		params.Priority = &pr
	}
	params.IsRemote = req.IsRemote
	params.CutoffTime = req.CutoffTime
	params.EffectiveTo = req.EffectiveTo
	if req.Status != nil {
		st := v.Enum("status", *req.Status, []string{"ACTIVE", "INACTIVE"}, true)
		params.Status = &st
	}
	if err := v.Err(); err != nil {
		return err
	}
	area, err := h.q.UpdateServiceArea(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Service area")
		}
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionServiceAreaUpdated, ResourceType: "service_area",
		ResourceID: &area.ID, ResourcePublicID: area.PublicID,
		After: map[string]any{"priority": area.Priority, "isRemote": area.IsRemote, "status": area.Status},
	}))
	return httpx.OK(w, map[string]any{
		"id": area.PublicID, "priority": area.Priority,
		"isRemote": area.IsRemote, "status": area.Status,
	})
}

// ---- rules -----------------------------------------------------------------

type createRuleRequest struct {
	Name                string         `json:"name"`
	RuleType            string         `json:"ruleType"`
	ServiceCode         string         `json:"serviceCode,omitempty"`
	OriginZoneCode      string         `json:"originZoneCode,omitempty"`
	DestinationZoneCode string         `json:"destinationZoneCode,omitempty"`
	OriginPincode       string         `json:"originPincode,omitempty"`
	DestinationPincode  string         `json:"destinationPincode,omitempty"`
	Priority            int            `json:"priority,omitempty"`
	Restrictions        map[string]any `json:"restrictions,omitempty"`
	ReasonCode          string         `json:"reasonCode,omitempty"`
	Message             string         `json:"message,omitempty"`
	EffectiveFrom       *time.Time     `json:"effectiveFrom,omitempty"`
	EffectiveTo         *time.Time     `json:"effectiveTo,omitempty"`
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListServiceabilityRulesParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if rt, tErr := httpx.QueryEnum(r, "ruleType", []string{"ALLOW", "DENY"}); tErr != nil {
		return tErr
	} else if rt != "" {
		params.RuleType = &rt
	}
	rows, err := h.q.ListServiceabilityRules(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, rule := range rows {
		total = rule.TotalCount
		items = append(items, map[string]any{
			"id": rule.PublicID, "name": rule.Name, "ruleType": rule.RuleType,
			"priority": rule.Priority, "status": rule.Status,
			"serviceCode": rule.ServiceCode, "originZoneCode": rule.OriginZoneCode,
			"destinationZoneCode": rule.DestinationZoneCode,
			"originPincode":       rule.OriginPincode, "destinationPincode": rule.DestinationPincode,
			"restrictions": decodeMap(rule.Restrictions), "reasonCode": rule.ReasonCode,
			"message": rule.Message, "effectiveFrom": rule.EffectiveFrom, "effectiveTo": rule.EffectiveTo,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "priority", "desc"))
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createRuleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	name := v.Text("name", req.Name, 3, 160, true)
	ruleType := v.Enum("ruleType", req.RuleType, []string{"ALLOW", "DENY"}, true)
	if req.Priority == 0 {
		req.Priority = 100
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.CreateServiceabilityRuleParams{
		PublicID: publicid.New(publicid.PrefixServiceabilityRul), OrganizationID: p.OrganizationID,
		Name: name, RuleType: ruleType, Priority: int32(req.Priority),
		Restrictions: encodeMap(req.Restrictions), EffectiveFrom: time.Now(),
		EffectiveTo: req.EffectiveTo, CreatedBy: &p.UserID,
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}
	if req.ReasonCode != "" {
		params.ReasonCode = &req.ReasonCode
	}
	if req.Message != "" {
		params.Message = &req.Message
	}
	scoped := false
	if req.ServiceCode != "" {
		svc, sErr := h.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: req.ServiceCode,
		})
		if sErr != nil {
			return apierr.Validation("The courier service does not exist.", nil)
		}
		params.CourierServiceID, scoped = &svc.ID, true
	}
	for _, spec := range []struct {
		code   string
		target **int64
	}{
		{req.OriginZoneCode, &params.OriginZoneID},
		{req.DestinationZoneCode, &params.DestinationZoneID},
	} {
		if spec.code == "" {
			continue
		}
		zone, zErr := h.q.GetZoneByCode(r.Context(), dbgen.GetZoneByCodeParams{
			OrganizationID: p.OrganizationID, Code: spec.code,
		})
		if zErr != nil {
			return apierr.Validation("The zone does not exist.", map[string]any{"zoneCode": spec.code})
		}
		*spec.target, scoped = &zone.ID, true
	}
	for _, spec := range []struct {
		pin    string
		target **int64
	}{
		{req.OriginPincode, &params.OriginPincodeID},
		{req.DestinationPincode, &params.DestinationPincodeID},
	} {
		if spec.pin == "" {
			continue
		}
		pincode, pErr := h.geo.LookupPincode(r.Context(), spec.pin, geography.DefaultCountry)
		if pErr != nil {
			return pErr
		}
		*spec.target, scoped = &pincode.ID, true
	}
	if !scoped {
		return apierr.Validation(
			"A rule must be scoped by at least one of service, zone or PIN code; an unscoped rule would apply to the entire network.", nil)
	}

	rule, err := h.q.CreateServiceabilityRule(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionServiceabilityRuleCreated, ResourceType: "serviceability_rule",
		ResourceID: &rule.ID, ResourcePublicID: rule.PublicID,
		After: map[string]any{"name": rule.Name, "ruleType": rule.RuleType, "priority": rule.Priority},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": rule.PublicID, "name": rule.Name, "ruleType": rule.RuleType,
		"priority": rule.Priority, "status": rule.Status,
	})
}

type setStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) setRuleStatus(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	ruleID, err := httpx.PathPublicID(r, "ruleId", publicid.PrefixServiceabilityRul, "Serviceability rule")
	if err != nil {
		return err
	}
	var req setStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	status := v.Enum("status", req.Status, []string{"ACTIVE", "INACTIVE"}, true)
	if err := v.Err(); err != nil {
		return err
	}
	rule, err := h.q.SetServiceabilityRuleStatus(r.Context(), dbgen.SetServiceabilityRuleStatusParams{
		PublicID: ruleID, OrganizationID: p.OrganizationID, Status: status,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Serviceability rule")
		}
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionServiceabilityRuleUpdated, ResourceType: "serviceability_rule",
		ResourceID: &rule.ID, ResourcePublicID: rule.PublicID, Reason: req.Reason,
		After: map[string]any{"status": rule.Status},
	}))
	return httpx.OK(w, map[string]any{"id": rule.PublicID, "status": rule.Status})
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

func encodeMap(m map[string]any) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decodeMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
