package serviceability

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
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

var legModes = []string{"ROAD", "AIR", "RAIL", "PARTNER"}

type legRequest struct {
	Sequence     int    `json:"sequence"`
	FromUnitCode string `json:"fromUnitCode"`
	ToUnitCode   string `json:"toUnitCode"`
	Mode         string `json:"mode"`
	TransitHours int    `json:"transitHours"`
	PartnerName  string `json:"partnerName,omitempty"`
}

type createRouteRequest struct {
	Code                string       `json:"code"`
	Name                string       `json:"name"`
	OriginUnitCode      string       `json:"originUnitCode"`
	DestinationUnitCode string       `json:"destinationUnitCode"`
	ServiceCode         string       `json:"serviceCode,omitempty"`
	Priority            int          `json:"priority,omitempty"`
	TransitHours        int          `json:"transitHours"`
	CutoffTime          string       `json:"cutoffTime,omitempty"`
	IsFallback          bool         `json:"isFallback,omitempty"`
	EffectiveFrom       *time.Time   `json:"effectiveFrom,omitempty"`
	EffectiveTo         *time.Time   `json:"effectiveTo,omitempty"`
	Legs                []legRequest `json:"legs"`
}

type updateRouteRequest struct {
	Name         *string    `json:"name,omitempty"`
	Priority     *int       `json:"priority,omitempty"`
	TransitHours *int       `json:"transitHours,omitempty"`
	CutoffTime   *string    `json:"cutoffTime,omitempty"`
	IsFallback   *bool      `json:"isFallback,omitempty"`
	EffectiveTo  *time.Time `json:"effectiveTo,omitempty"`
	Status       *string    `json:"status,omitempty"`
}

func (h *Handler) listRoutes(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListRouteDefinitionsParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if st, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	}
	for _, spec := range []struct {
		param  string
		target **int64
	}{
		{"originUnitCode", &params.OriginUnitID},
		{"destinationUnitCode", &params.DestinationUnitID},
	} {
		code := httpx.Query(r, spec.param)
		if code == "" {
			continue
		}
		unit, uErr := h.q.GetOperatingUnitByCode(r.Context(), dbgen.GetOperatingUnitByCodeParams{
			OrganizationID: p.OrganizationID, Code: code,
		})
		if uErr != nil {
			return apierr.NotFound("Operating unit")
		}
		*spec.target = &unit.ID
	}

	rows, err := h.q.ListRouteDefinitions(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	routeIDs := make([]int64, 0, len(rows))
	for _, rt := range rows {
		routeIDs = append(routeIDs, rt.ID)
	}
	// One batched query for all legs avoids an N+1 across the page.
	legsByRoute := map[int64][]map[string]any{}
	if len(routeIDs) > 0 {
		legRows, lErr := h.q.ListRouteLegsForRoutes(r.Context(), routeIDs)
		if lErr != nil {
			return apierr.Internal(lErr)
		}
		for _, l := range legRows {
			legsByRoute[l.RouteDefinitionID] = append(legsByRoute[l.RouteDefinitionID], map[string]any{
				"sequence": l.Sequence, "from": l.FromCode, "to": l.ToCode,
				"mode": l.Mode, "transitHours": l.TransitHours, "partnerName": l.PartnerName,
			})
		}
	}
	items := make([]map[string]any, 0, len(rows))
	for _, rt := range rows {
		total = rt.TotalCount
		legs := legsByRoute[rt.ID]
		if legs == nil {
			legs = []map[string]any{}
		}
		items = append(items, map[string]any{
			"id": rt.PublicID, "code": rt.Code, "name": rt.Name,
			"originUnitCode": rt.OriginCode, "destinationUnitCode": rt.DestinationCode,
			"serviceCode": rt.ServiceCode, "priority": rt.Priority,
			"transitHours": rt.TransitHours, "isFallback": rt.IsFallback,
			"cutoffTime": rt.CutoffTime, "status": rt.Status,
			"effectiveFrom": rt.EffectiveFrom, "effectiveTo": rt.EffectiveTo,
			"legs": legs,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "code", "asc"))
}

func (h *Handler) getRoute(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	routeID, err := httpx.PathPublicID(r, "routeId", publicid.PrefixRoute, "Route")
	if err != nil {
		return err
	}
	rt, err := h.q.GetRouteDefinitionByPublicID(r.Context(), dbgen.GetRouteDefinitionByPublicIDParams{
		PublicID: routeID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Route")
		}
		return apierr.Internal(err)
	}
	legs, err := h.q.ListRouteLegs(r.Context(), rt.ID)
	if err != nil {
		return apierr.Internal(err)
	}
	legViews := make([]map[string]any, 0, len(legs))
	for _, l := range legs {
		legViews = append(legViews, map[string]any{
			"id": l.PublicID, "sequence": l.Sequence,
			"from": map[string]string{"code": l.FromCode, "name": l.FromName},
			"to":   map[string]string{"code": l.ToCode, "name": l.ToName},
			"mode": l.Mode, "transitHours": l.TransitHours, "partnerName": l.PartnerName,
		})
	}
	return httpx.OK(w, map[string]any{
		"id": rt.PublicID, "code": rt.Code, "name": rt.Name,
		"origin":      map[string]string{"code": rt.OriginCode, "name": rt.OriginName},
		"destination": map[string]string{"code": rt.DestinationCode, "name": rt.DestinationName},
		"serviceCode": rt.ServiceCode, "priority": rt.Priority, "transitHours": rt.TransitHours,
		"cutoffTime": rt.CutoffTime, "isFallback": rt.IsFallback, "status": rt.Status,
		"effectiveFrom": rt.EffectiveFrom, "effectiveTo": rt.EffectiveTo, "legs": legViews,
	})
}

func (h *Handler) createRoute(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createRouteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	code := v.Code("code", req.Code)
	name := v.Text("name", req.Name, 2, 160, true)
	originCode := v.Code("originUnitCode", req.OriginUnitCode)
	destCode := v.Code("destinationUnitCode", req.DestinationUnitCode)
	if req.Priority == 0 {
		req.Priority = 100
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	v.IntRange("transitHours", req.TransitHours, 1, 8760)
	if originCode == destCode {
		v.Add("destinationUnitCode", "Origin and destination must differ.")
	}
	if len(req.Legs) == 0 {
		v.Add("legs", "At least one leg is required so the physical path is explicit.")
	}
	if len(req.Legs) > 20 {
		v.Add("legs", "A route may not have more than 20 legs.")
	}
	for i, l := range req.Legs {
		v.Enum(fmt.Sprintf("legs[%d].mode", i), l.Mode, legModes, true)
		v.IntRange(fmt.Sprintf("legs[%d].transitHours", i), l.TransitHours, 1, 8760)
		v.Code(fmt.Sprintf("legs[%d].fromUnitCode", i), l.FromUnitCode)
		v.Code(fmt.Sprintf("legs[%d].toUnitCode", i), l.ToUnitCode)
	}
	if err := v.Err(); err != nil {
		return err
	}

	units, err := h.resolveUnitCodes(r, p.OrganizationID, collectUnitCodes(req))
	if err != nil {
		return err
	}
	originID, destID := units[originCode], units[destCode]

	params := dbgen.CreateRouteDefinitionParams{
		PublicID: publicid.New(publicid.PrefixRoute), OrganizationID: p.OrganizationID,
		Code: code, Name: name, OriginUnitID: originID, DestinationUnitID: destID,
		Priority: int32(req.Priority), TransitHours: int32(req.TransitHours),
		IsFallback: req.IsFallback, EffectiveFrom: time.Now(), EffectiveTo: req.EffectiveTo,
		CreatedBy: &p.UserID,
	}
	if req.CutoffTime != "" {
		params.CutoffTime = &req.CutoffTime
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}
	if req.ServiceCode != "" {
		svc, sErr := h.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: req.ServiceCode,
		})
		if sErr != nil {
			return apierr.Validation("The courier service does not exist.", nil)
		}
		params.CourierServiceID = &svc.ID
	}

	var route dbgen.RouteDefinition
	// The route and its legs are created together: a route with no legs would
	// resolve but carry no physical path, which downstream custody checks in
	// Release 2 would then reject at scan time instead of at configuration time.
	err = h.db.InTx(r.Context(), func(tx pgx.Tx) error {
		qtx := h.q.WithTx(tx)
		var cErr error
		route, cErr = qtx.CreateRouteDefinition(r.Context(), params)
		if cErr != nil {
			if database.IsUniqueViolation(cErr, "route_definitions_code_unique") {
				return apierr.Duplicate("A route with this code already exists.")
			}
			return apierr.Internal(cErr)
		}
		for i, l := range req.Legs {
			seq := l.Sequence
			if seq == 0 {
				seq = i + 1
			}
			legParams := dbgen.CreateRouteLegParams{
				PublicID: publicid.New(publicid.PrefixRouteLeg), OrganizationID: p.OrganizationID,
				RouteDefinitionID: route.ID, Sequence: int32(seq),
				FromUnitID: units[l.FromUnitCode], ToUnitID: units[l.ToUnitCode],
				Mode: l.Mode, TransitHours: int32(l.TransitHours),
			}
			if l.PartnerName != "" {
				legParams.PartnerName = &l.PartnerName
			}
			if _, lErr := qtx.CreateRouteLeg(r.Context(), legParams); lErr != nil {
				if database.IsUniqueViolation(lErr) {
					return apierr.Validation("Leg sequence numbers must be unique within a route.",
						map[string]any{"sequence": seq})
				}
				return apierr.Internal(lErr)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRouteCreated, ResourceType: "route_definition",
		ResourceID: &route.ID, ResourcePublicID: route.PublicID,
		After: map[string]any{
			"code": route.Code, "origin": originCode, "destination": destCode,
			"transitHours": route.TransitHours, "legs": len(req.Legs),
		},
	}))
	return httpx.Created(w, "/api/v1/routes/"+route.PublicID, map[string]any{
		"id": route.PublicID, "code": route.Code, "name": route.Name,
		"transitHours": route.TransitHours, "status": route.Status, "legs": len(req.Legs),
	})
}

func (h *Handler) updateRoute(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	routeID, err := httpx.PathPublicID(r, "routeId", publicid.PrefixRoute, "Route")
	if err != nil {
		return err
	}
	var req updateRouteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	params := dbgen.UpdateRouteDefinitionParams{PublicID: routeID, OrganizationID: p.OrganizationID}
	if req.Name != nil {
		n := v.Text("name", *req.Name, 2, 160, true)
		params.Name = &n
	}
	if req.Priority != nil {
		v.IntRange("priority", *req.Priority, 0, 1000)
		pr := int32(*req.Priority)
		params.Priority = &pr
	}
	if req.TransitHours != nil {
		v.IntRange("transitHours", *req.TransitHours, 1, 8760)
		th := int32(*req.TransitHours)
		params.TransitHours = &th
	}
	params.CutoffTime = req.CutoffTime
	params.IsFallback = req.IsFallback
	params.EffectiveTo = req.EffectiveTo
	if req.Status != nil {
		st := v.Enum("status", *req.Status, []string{"ACTIVE", "INACTIVE"}, true)
		params.Status = &st
	}
	if err := v.Err(); err != nil {
		return err
	}
	route, err := h.q.UpdateRouteDefinition(r.Context(), params)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Route")
		}
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRouteUpdated, ResourceType: "route_definition",
		ResourceID: &route.ID, ResourcePublicID: route.PublicID,
		After: map[string]any{
			"priority": route.Priority, "transitHours": route.TransitHours, "status": route.Status,
		},
	}))
	return httpx.OK(w, map[string]any{
		"id": route.PublicID, "code": route.Code, "priority": route.Priority,
		"transitHours": route.TransitHours, "status": route.Status,
	})
}

// ---- overrides -------------------------------------------------------------

type createOverrideRequest struct {
	OriginPincode      string     `json:"originPincode"`
	DestinationPincode string     `json:"destinationPincode"`
	ServiceCode        string     `json:"serviceCode,omitempty"`
	RouteCode          string     `json:"routeCode"`
	Priority           int        `json:"priority,omitempty"`
	Reason             string     `json:"reason"`
	EffectiveFrom      *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveTo        *time.Time `json:"effectiveTo,omitempty"`
}

func (h *Handler) listOverrides(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	params := dbgen.ListRoutingOverridesParams{
		OrganizationID: p.OrganizationID,
		RowLimit:       int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if st, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "INACTIVE"}); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	}
	if pin := httpx.Query(r, "originPincode"); pin != "" {
		params.OriginPincode = &pin
	}
	if pin := httpx.Query(r, "destinationPincode"); pin != "" {
		params.DestinationPincode = &pin
	}
	rows, err := h.q.ListRoutingOverrides(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, o := range rows {
		total = o.TotalCount
		items = append(items, map[string]any{
			"id": o.PublicID, "originPincode": o.OriginPincode,
			"destinationPincode": o.DestinationPincode, "serviceCode": o.ServiceCode,
			"routeCode": o.RouteCode, "priority": o.Priority, "reason": o.Reason,
			"status": o.Status, "effectiveFrom": o.EffectiveFrom, "effectiveTo": o.EffectiveTo,
			"createdBy": o.CreatedByName,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "priority", "desc"))
}

func (h *Handler) createOverride(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createOverrideRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	originPin := v.Pincode("originPincode", req.OriginPincode)
	destPin := v.Pincode("destinationPincode", req.DestinationPincode)
	routeCode := v.Code("routeCode", req.RouteCode)
	// An override bypasses the configured network; requiring a substantive
	// reason keeps the audit trail useful when someone asks why a parcel took
	// an unusual path.
	reason := v.Text("reason", req.Reason, 5, 500, true)
	if req.Priority == 0 {
		req.Priority = 500
	}
	v.IntRange("priority", req.Priority, 0, 1000)
	if err := v.Err(); err != nil {
		return err
	}

	originPincode, err := h.geo.LookupPincode(r.Context(), originPin, geography.DefaultCountry)
	if err != nil {
		return err
	}
	destPincode, err := h.geo.LookupPincode(r.Context(), destPin, geography.DefaultCountry)
	if err != nil {
		return err
	}
	routes, err := h.q.ListRouteDefinitions(r.Context(), dbgen.ListRouteDefinitionsParams{
		OrganizationID: p.OrganizationID, RowLimit: 1000, RowOffset: 0,
	})
	if err != nil {
		return apierr.Internal(err)
	}
	var routeID int64
	for _, rt := range routes {
		if rt.Code == routeCode {
			routeID = rt.ID
			break
		}
	}
	if routeID == 0 {
		return apierr.Validation("The route does not exist.", map[string]any{"routeCode": routeCode})
	}

	params := dbgen.CreateRoutingOverrideParams{
		PublicID: publicid.New(publicid.PrefixRoutingOverride), OrganizationID: p.OrganizationID,
		OriginPincodeID: originPincode.ID, DestinationPincodeID: destPincode.ID,
		RouteDefinitionID: routeID, Priority: int32(req.Priority), Reason: reason,
		EffectiveFrom: time.Now(), EffectiveTo: req.EffectiveTo, CreatedBy: &p.UserID,
	}
	if req.EffectiveFrom != nil {
		params.EffectiveFrom = *req.EffectiveFrom
	}
	if req.ServiceCode != "" {
		svc, sErr := h.q.GetCourierServiceByCode(r.Context(), dbgen.GetCourierServiceByCodeParams{
			OrganizationID: p.OrganizationID, Code: req.ServiceCode,
		})
		if sErr != nil {
			return apierr.Validation("The courier service does not exist.", nil)
		}
		params.CourierServiceID = &svc.ID
	}

	override, err := h.q.CreateRoutingOverride(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoutingOverrideCreated, ResourceType: "routing_override",
		ResourceID: &override.ID, ResourcePublicID: override.PublicID, Reason: reason,
		After: map[string]any{
			"originPincode": originPin, "destinationPincode": destPin,
			"routeCode": routeCode, "priority": override.Priority,
		},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": override.PublicID, "routeCode": routeCode, "priority": override.Priority,
		"status": override.Status, "reason": override.Reason,
	})
}

func (h *Handler) cancelOverride(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	overrideID, err := httpx.PathPublicID(r, "overrideId", publicid.PrefixRoutingOverride, "Routing override")
	if err != nil {
		return err
	}
	override, err := h.q.SetRoutingOverrideStatus(r.Context(), dbgen.SetRoutingOverrideStatusParams{
		PublicID: overrideID, OrganizationID: p.OrganizationID, Status: "INACTIVE",
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Routing override")
		}
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionRoutingOverrideUpdated, ResourceType: "routing_override",
		ResourceID: &override.ID, ResourcePublicID: override.PublicID,
		After: map[string]any{"status": override.Status},
	}))
	return httpx.NoContent(w)
}

// ---- closures --------------------------------------------------------------

type createClosureRequest struct {
	OperatingUnitCode string    `json:"operatingUnitCode,omitempty"`
	Pincode           string    `json:"pincode,omitempty"`
	ClosureType       string    `json:"closureType"`
	ReasonCode        string    `json:"reasonCode"`
	Reason            string    `json:"reason"`
	StartsAt          time.Time `json:"startsAt"`
	EndsAt            time.Time `json:"endsAt"`
}

var (
	closureTypes       = []string{"FULL", "PICKUP_ONLY", "DELIVERY_ONLY"}
	closureReasonCodes = []string{"WEATHER", "STRIKE", "HOLIDAY", "LAW_AND_ORDER", "INFRASTRUCTURE", "OTHER"}
)

func (h *Handler) listClosures(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, page, err := listWindow(r)
	if err != nil {
		return err
	}
	activeOnly, err := httpx.QueryBool(r, "activeOnly", false)
	if err != nil {
		return err
	}
	params := dbgen.ListTemporaryClosuresParams{
		OrganizationID: p.OrganizationID, ActiveOnly: &activeOnly,
		RowLimit: int32(limit), RowOffset: int32((page - 1) * limit),
	}
	if st, sErr := httpx.QueryEnum(r, "status", []string{"ACTIVE", "CANCELLED"}); sErr != nil {
		return sErr
	} else if st != "" {
		params.Status = &st
	}
	rows, err := h.q.ListTemporaryClosures(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		total = c.TotalCount
		items = append(items, map[string]any{
			"id": c.PublicID, "closureType": c.ClosureType, "reasonCode": c.ReasonCode,
			"reason": c.Reason, "startsAt": c.StartsAt, "endsAt": c.EndsAt, "status": c.Status,
			"operatingUnitCode": c.UnitCode, "operatingUnitName": c.UnitName, "pincode": c.Pincode,
		})
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "startsAt", "desc"))
}

func (h *Handler) createClosure(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createClosureRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	closureType := v.Enum("closureType", req.ClosureType, closureTypes, true)
	reasonCode := v.Enum("reasonCode", req.ReasonCode, closureReasonCodes, true)
	reason := v.Text("reason", req.Reason, 5, 500, true)
	if req.StartsAt.IsZero() {
		v.Add("startsAt", "This field is required.")
	}
	if req.EndsAt.IsZero() {
		v.Add("endsAt", "This field is required.")
	}
	if !req.EndsAt.IsZero() && !req.StartsAt.IsZero() && !req.EndsAt.After(req.StartsAt) {
		v.Add("endsAt", "Must be after startsAt.")
	}
	// The schema enforces exactly one target; validating here produces a field
	// error instead of a constraint violation.
	if (req.OperatingUnitCode == "") == (req.Pincode == "") {
		v.Add("operatingUnitCode", "Supply exactly one of operatingUnitCode or pincode.")
	}
	if err := v.Err(); err != nil {
		return err
	}

	params := dbgen.CreateTemporaryClosureParams{
		PublicID: publicid.New(publicid.PrefixTemporaryClosure), OrganizationID: p.OrganizationID,
		ClosureType: closureType, ReasonCode: reasonCode, Reason: reason,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, CreatedBy: &p.UserID,
	}
	if req.OperatingUnitCode != "" {
		unit, uErr := h.q.GetOperatingUnitByCode(r.Context(), dbgen.GetOperatingUnitByCodeParams{
			OrganizationID: p.OrganizationID, Code: req.OperatingUnitCode,
		})
		if uErr != nil {
			return apierr.Validation("The operating unit does not exist.", nil)
		}
		params.OperatingUnitID = &unit.ID
	} else {
		pincode, pErr := h.geo.LookupPincode(r.Context(), req.Pincode, geography.DefaultCountry)
		if pErr != nil {
			return pErr
		}
		params.PincodeID = &pincode.ID
	}

	closure, err := h.q.CreateTemporaryClosure(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionClosureDeclared, ResourceType: "temporary_closure",
		ResourceID: &closure.ID, ResourcePublicID: closure.PublicID, Reason: reason,
		OperatingUnitID: closure.OperatingUnitID,
		After: map[string]any{
			"closureType": closure.ClosureType, "reasonCode": closure.ReasonCode,
			"startsAt": closure.StartsAt, "endsAt": closure.EndsAt,
		},
	}))
	return httpx.Created(w, "", map[string]any{
		"id": closure.PublicID, "closureType": closure.ClosureType,
		"startsAt": closure.StartsAt, "endsAt": closure.EndsAt, "status": closure.Status,
	})
}

func (h *Handler) cancelClosure(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	closureID, err := httpx.PathPublicID(r, "closureId", publicid.PrefixTemporaryClosure, "Temporary closure")
	if err != nil {
		return err
	}
	closure, err := h.q.CancelTemporaryClosure(r.Context(), dbgen.CancelTemporaryClosureParams{
		PublicID: closureID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Temporary closure")
		}
		return apierr.Internal(err)
	}
	h.resolver.InvalidateOrganization(r.Context(), p.OrganizationID)
	h.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionClosureCancelled, ResourceType: "temporary_closure",
		ResourceID: &closure.ID, ResourcePublicID: closure.PublicID,
		After: map[string]any{"status": closure.Status},
	}))
	return httpx.NoContent(w)
}

// ---- helpers ---------------------------------------------------------------

// resolveUnitCodes maps every unit code referenced by a route to its id in one
// pass, so leg creation does not issue a query per leg.
func (h *Handler) resolveUnitCodes(r *http.Request, orgID int64, codes []string) (map[string]int64, error) {
	out := make(map[string]int64, len(codes))
	for _, code := range codes {
		if _, done := out[code]; done {
			continue
		}
		unit, err := h.q.GetOperatingUnitByCode(r.Context(), dbgen.GetOperatingUnitByCodeParams{
			OrganizationID: orgID, Code: code,
		})
		if err != nil {
			if database.IsNoRows(err) {
				return nil, apierr.Validation("An operating unit referenced by the route does not exist.",
					map[string]any{"unitCode": code})
			}
			return nil, apierr.Internal(err)
		}
		out[code] = unit.ID
	}
	return out, nil
}

func collectUnitCodes(req createRouteRequest) []string {
	codes := []string{req.OriginUnitCode, req.DestinationUnitCode}
	for _, l := range req.Legs {
		codes = append(codes, l.FromUnitCode, l.ToUnitCode)
	}
	return codes
}
