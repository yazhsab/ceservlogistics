// Package serviceability answers "can we carry this, by what path, by when"
// (M04).
//
// Determinism is the central requirement (Constitution §20). Two calls with the
// same inputs against the same configuration must always produce the same
// answer, on any replica, in any plan. That is achieved by:
//
//   - every candidate query ending its ORDER BY with the row id, so ties never
//     depend on physical row order;
//   - a fixed precedence between sources, encoded once in Resolve and asserted
//     by tests rather than emerging from query order;
//   - a decision trace (Explanation) recorded for every resolution, so a past
//     answer can be defended months later.
package serviceability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/network"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/product"
)

// Resolution sources, in descending precedence.
const (
	SourceOverride     = "OVERRIDE"
	SourceServiceRoute = "SERVICE_ROUTE"
	SourceGenericRoute = "GENERIC_ROUTE"
	SourceFallback     = "FALLBACK_ROUTE"
	SourceLocal        = "LOCAL_DELIVERY"
)

// Reason codes returned when a lane is not serviceable. These are part of the
// published contract; the frontend maps them to user-facing guidance.
const (
	ReasonOriginNotServed      = "ORIGIN_NOT_SERVICEABLE"
	ReasonDestinationNotServed = "DESTINATION_NOT_SERVICEABLE"
	ReasonNoRoute              = "NO_ROUTE_AVAILABLE"
	ReasonDeniedByRule         = "DENIED_BY_RULE"
	ReasonOriginClosed         = "ORIGIN_TEMPORARILY_CLOSED"
	ReasonDestinationClosed    = "DESTINATION_TEMPORARILY_CLOSED"
	ReasonNoOriginHub          = "ORIGIN_HUB_NOT_CONFIGURED"
	ReasonNoDestinationHub     = "DESTINATION_HUB_NOT_CONFIGURED"
	ReasonUnknownPincode       = "PINCODE_NOT_FOUND"
)

// Request is one serviceability question.
type Request struct {
	OrganizationID int64
	OriginPincode  string
	DestPincode    string
	ServiceCode    string
	At             time.Time
	IncludeExplain bool
}

// FacilityRef identifies a facility in a result.
type FacilityRef struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	UnitType string `json:"unitType"`
}

// Leg is one movement in the transit path.
type Leg struct {
	Sequence     int32  `json:"sequence"`
	FromCode     string `json:"from"`
	FromName     string `json:"fromName,omitempty"`
	ToCode       string `json:"to"`
	ToName       string `json:"toName,omitempty"`
	Mode         string `json:"mode"`
	TransitHours int32  `json:"transitHours"`
	Partner      string `json:"partner,omitempty"`
}

// Restriction is a constraint the caller must respect for this lane.
type Restriction struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Result is the answer to a serviceability question.
type Result struct {
	Serviceable   bool   `json:"serviceable"`
	ReasonCode    string `json:"reasonCode,omitempty"`
	ReasonMessage string `json:"reasonMessage,omitempty"`

	ServiceCode string `json:"serviceCode"`
	ServiceName string `json:"serviceName,omitempty"`
	ServiceMode string `json:"serviceMode,omitempty"`

	Origin      LocationInfo `json:"origin"`
	Destination LocationInfo `json:"destination"`

	OriginBranch      *FacilityRef `json:"originBranch,omitempty"`
	OriginHub         *FacilityRef `json:"originHub,omitempty"`
	DestinationHub    *FacilityRef `json:"destinationHub,omitempty"`
	DestinationBranch *FacilityRef `json:"destinationBranch,omitempty"`

	RouteID          string `json:"routeId,omitempty"`
	RouteCode        string `json:"routeCode,omitempty"`
	ResolutionSource string `json:"resolutionSource,omitempty"`
	Legs             []Leg  `json:"legs"`

	TransitHours       int32      `json:"transitHours,omitempty"`
	SLAHours           int32      `json:"slaHours,omitempty"`
	PromisedDeliveryAt *time.Time `json:"promisedDeliveryAt,omitempty"`
	CutoffApplied      bool       `json:"cutoffApplied"`

	Restrictions  []Restriction `json:"restrictions"`
	ExplanationID string        `json:"explanationId"`
	Explanation   *Explanation  `json:"explanation,omitempty"`

	// Internal identifiers used by booking; not serialised to clients.
	routeDefinitionID   *int64
	originBranchID      *int64
	originHubID         *int64
	destinationHubID    *int64
	destinationBranchID *int64
	originPincodeID     int64
	destPincodeID       int64
	originZone          *geography.ZoneAssignment
	destZone            *geography.ZoneAssignment
	originIsRemote      bool
	destIsRemote        bool
	originStateID       int64
	destStateID         int64
	includeExplain      bool
}

// LocationInfo describes one end of a lane.
type LocationInfo struct {
	Pincode  string `json:"pincode"`
	City     string `json:"city,omitempty"`
	State    string `json:"state,omitempty"`
	ZoneCode string `json:"zoneCode,omitempty"`
	ZoneName string `json:"zoneName,omitempty"`
	IsRemote bool   `json:"isRemote"`
}

// Explanation is the decision trace: every step, every candidate, and why the
// winner won. It is stored on the shipment at booking and cached for the
// administrator debug endpoint.
type Explanation struct {
	ID         string         `json:"id"`
	ResolvedAt time.Time      `json:"resolvedAt"`
	Request    map[string]any `json:"request"`
	Steps      []ExplainStep  `json:"steps"`
}

// ExplainStep is one stage of resolution.
type ExplainStep struct {
	Step       string             `json:"step"`
	Outcome    string             `json:"outcome"`
	Detail     string             `json:"detail,omitempty"`
	Candidates []ExplainCandidate `json:"candidates,omitempty"`
}

// ExplainCandidate is one option considered at a step.
type ExplainCandidate struct {
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
	Priority int32  `json:"priority,omitempty"`
	Selected bool   `json:"selected"`
	Reason   string `json:"reason,omitempty"`
}

// Resolver answers serviceability questions.
type Resolver struct {
	q          *dbgen.Queries
	geo        *geography.Service
	network    *network.Service
	product    *product.Service
	cache      *cache.Cache
	log        *slog.Logger
	metrics    *telemetry.Metrics
	ttl        time.Duration
	explainTTL time.Duration
}

// NewResolver builds the routing resolver.
func NewResolver(
	q *dbgen.Queries, geo *geography.Service, net *network.Service, prod *product.Service,
	c *cache.Cache, log *slog.Logger, m *telemetry.Metrics, ttl, explainTTL time.Duration,
) *Resolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if explainTTL <= 0 {
		explainTTL = time.Hour
	}
	return &Resolver{
		q: q, geo: geo, network: net, product: prod, cache: c,
		log: log, metrics: m, ttl: ttl, explainTTL: explainTTL,
	}
}

// Resolve answers a serviceability question.
//
// The order of operations is fixed and each stage short-circuits with a reason
// code, so a "not serviceable" answer always says which stage rejected it.
func (r *Resolver) Resolve(ctx context.Context, req Request) (*Result, error) {
	if req.At.IsZero() {
		req.At = time.Now()
	}
	explanation := &Explanation{
		ID:         publicid.New(publicid.PrefixExplanation),
		ResolvedAt: req.At,
		Request: map[string]any{
			"originPincode": req.OriginPincode, "destinationPincode": req.DestPincode,
			"serviceCode": req.ServiceCode, "at": req.At,
		},
	}
	res := &Result{
		ServiceCode: req.ServiceCode, ExplanationID: explanation.ID,
		Legs: []Leg{}, Restrictions: []Restriction{},
		includeExplain: req.IncludeExplain,
	}

	// 1. Resolve both PIN codes.
	origin, err := r.geo.LookupPincode(ctx, req.OriginPincode, geography.DefaultCountry)
	if err != nil {
		return r.notServiceable(ctx, res, explanation, ReasonUnknownPincode,
			"The origin PIN code is not recognised."), nil
	}
	dest, err := r.geo.LookupPincode(ctx, req.DestPincode, geography.DefaultCountry)
	if err != nil {
		return r.notServiceable(ctx, res, explanation, ReasonUnknownPincode,
			"The destination PIN code is not recognised."), nil
	}
	res.originPincodeID, res.destPincodeID = origin.ID, dest.ID
	res.originStateID, res.destStateID = origin.StateID, dest.StateID
	res.Origin = LocationInfo{Pincode: origin.Code, City: origin.CityName, State: origin.StateName, IsRemote: origin.IsRemote}
	res.Destination = LocationInfo{Pincode: dest.Code, City: dest.CityName, State: dest.StateName, IsRemote: dest.IsRemote}
	explanation.Steps = append(explanation.Steps, ExplainStep{
		Step: "resolve_pincodes", Outcome: "ok",
		Detail: fmt.Sprintf("%s (%s) -> %s (%s)", origin.Code, origin.StateName, dest.Code, dest.StateName),
	})

	// 2. Resolve the product.
	svc, err := r.product.GetActiveByCode(ctx, req.OrganizationID, req.ServiceCode, req.At)
	if err != nil {
		return r.notServiceable(ctx, res, explanation, ReasonDeniedByRule,
			"This courier service is not available."), nil
	}
	res.ServiceName, res.ServiceMode = svc.Name, svc.Mode
	explanation.Steps = append(explanation.Steps, ExplainStep{
		Step: "resolve_service", Outcome: "ok", Detail: svc.Code + " (" + svc.Mode + ")",
	})

	// 3. Zone assignment. Missing zone mapping is a configuration gap, not a
	// routing failure, so pricing later reports it precisely.
	if z, zErr := r.geo.ResolveZone(ctx, req.OrganizationID, origin.ID, req.At); zErr == nil {
		res.originZone, res.Origin.ZoneCode, res.Origin.ZoneName = z, z.ZoneCode, z.ZoneName
		res.Origin.IsRemote, res.originIsRemote = z.IsRemote, z.IsRemote
	} else {
		res.originIsRemote = origin.IsRemote
	}
	if z, zErr := r.geo.ResolveZone(ctx, req.OrganizationID, dest.ID, req.At); zErr == nil {
		res.destZone, res.Destination.ZoneCode, res.Destination.ZoneName = z, z.ZoneCode, z.ZoneName
		res.Destination.IsRemote, res.destIsRemote = z.IsRemote, z.IsRemote
	} else {
		res.destIsRemote = dest.IsRemote
	}

	// 4. Serving facilities. Pickup at origin, delivery at destination.
	originBranch, originSA, err := r.resolveServingUnit(ctx, req.OrganizationID, origin.ID, "PICKUP", req.At, explanation, "origin")
	if err != nil {
		return nil, err
	}
	if originBranch == nil {
		return r.notServiceable(ctx, res, explanation, ReasonOriginNotServed,
			"No branch currently serves pickups at the origin PIN code."), nil
	}
	destBranch, _, err := r.resolveServingUnit(ctx, req.OrganizationID, dest.ID, "DELIVERY", req.At, explanation, "destination")
	if err != nil {
		return nil, err
	}
	if destBranch == nil {
		return r.notServiceable(ctx, res, explanation, ReasonDestinationNotServed,
			"No branch currently serves deliveries at the destination PIN code."), nil
	}
	res.OriginBranch = facilityRef(originBranch)
	res.DestinationBranch = facilityRef(destBranch)
	res.originBranchID, res.destinationBranchID = &originBranch.ID, &destBranch.ID
	if originSA != nil && originSA.IsRemote {
		res.Origin.IsRemote, res.originIsRemote = true, true
	}

	// 5. Temporary closures at either end.
	closed, closureStep := r.checkClosures(ctx, req, []int64{originBranch.ID, destBranch.ID},
		[]int64{origin.ID, dest.ID}, originBranch.ID, destBranch.ID, origin.ID, dest.ID)
	explanation.Steps = append(explanation.Steps, closureStep)
	if closed != nil {
		return r.notServiceable(ctx, res, explanation, closed.Code, closed.Message), nil
	}

	// 6. Allow/deny rules.
	restrictions, denial, err := r.applyRules(ctx, req, svc.ID, res, explanation)
	if err != nil {
		return nil, err
	}
	if denial != nil {
		res.Restrictions = restrictions
		return r.notServiceable(ctx, res, explanation, denial.Code, denial.Message), nil
	}
	res.Restrictions = restrictions

	// 7. Route.
	if err := r.resolveRoute(ctx, req, svc, originBranch, destBranch, res, explanation); err != nil {
		return nil, err
	}
	if res.ResolutionSource == "" {
		return r.notServiceable(ctx, res, explanation, ReasonNoRoute,
			"No route is configured between these facilities for the selected service."), nil
	}

	// 8. SLA and promised delivery.
	r.applySLA(req, svc, res, explanation)

	res.Serviceable = true
	r.storeExplanation(ctx, explanation)
	if req.IncludeExplain {
		res.Explanation = explanation
	}
	r.metrics.RecordBusiness("serviceability", "serviceable")
	return res, nil
}

// resolveServingUnit picks the facility that serves a PIN code.
//
// Precedence is priority DESC, then a specific area_type over the catch-all
// BOTH, then the lowest id. The candidate list is recorded either way so an
// operator can see which competing coverage lost.
func (r *Resolver) resolveServingUnit(
	ctx context.Context, orgID, pincodeID int64, areaType string, at time.Time,
	exp *Explanation, label string,
) (*network.Unit, *dbgen.ResolveServingUnitsRow, error) {
	rows, err := r.q.ResolveServingUnits(ctx, dbgen.ResolveServingUnitsParams{
		OrganizationID: orgID, PincodeID: pincodeID, AreaType: areaType,
		AsOf: at, RowLimit: 10,
	})
	if err != nil {
		return nil, nil, apierr.Internal(fmt.Errorf("resolve serving units: %w", err))
	}
	step := ExplainStep{Step: "resolve_" + label + "_branch"}
	if len(rows) == 0 {
		step.Outcome = "no_candidates"
		exp.Steps = append(exp.Steps, step)
		return nil, nil, nil
	}
	for i, row := range rows {
		step.Candidates = append(step.Candidates, ExplainCandidate{
			ID: row.UnitPublicID, Code: row.UnitCode, Priority: row.Priority,
			Selected: i == 0,
			Reason:   fmt.Sprintf("areaType=%s priority=%d", row.AreaType, row.Priority),
		})
	}
	step.Outcome = "selected"
	step.Detail = rows[0].UnitCode
	exp.Steps = append(exp.Steps, step)

	winner := rows[0]
	return &network.Unit{
		ID: winner.UnitID, PublicID: winner.UnitPublicID, Code: winner.UnitCode,
		Name: winner.UnitName, UnitType: winner.UnitType, Status: winner.UnitStatus,
		ParentUnitID: winner.ParentUnitID,
		CutoffTime:   derefStr(winner.CutoffTime),
	}, &winner, nil
}

// checkClosures reports the first closure that blocks the lane.
func (r *Resolver) checkClosures(
	ctx context.Context, req Request, unitIDs, pincodeIDs []int64,
	originUnitID, destUnitID, originPinID, destPinID int64,
) (*Restriction, ExplainStep) {
	step := ExplainStep{Step: "check_closures", Outcome: "none"}
	rows, err := r.q.FindActiveClosures(ctx, dbgen.FindActiveClosuresParams{
		OrganizationID: req.OrganizationID, AsOf: req.At,
		UnitIds: unitIDs, PincodeIds: pincodeIDs,
	})
	if err != nil {
		r.log.Warn("closure lookup failed; treating lane as open",
			slog.String("error", err.Error()))
		step.Outcome = "lookup_failed"
		return nil, step
	}
	for _, c := range rows {
		affectsOrigin := (c.OperatingUnitID != nil && *c.OperatingUnitID == originUnitID) ||
			(c.PincodeID != nil && *c.PincodeID == originPinID)
		affectsDest := (c.OperatingUnitID != nil && *c.OperatingUnitID == destUnitID) ||
			(c.PincodeID != nil && *c.PincodeID == destPinID)

		// A PICKUP_ONLY closure blocks the origin; DELIVERY_ONLY blocks the
		// destination; FULL blocks either end it touches.
		if affectsOrigin && (c.ClosureType == "FULL" || c.ClosureType == "PICKUP_ONLY") {
			step.Outcome = "origin_closed"
			step.Detail = c.Reason
			return &Restriction{Code: ReasonOriginClosed, Message: closureMessage(c, "origin")}, step
		}
		if affectsDest && (c.ClosureType == "FULL" || c.ClosureType == "DELIVERY_ONLY") {
			step.Outcome = "destination_closed"
			step.Detail = c.Reason
			return &Restriction{Code: ReasonDestinationClosed, Message: closureMessage(c, "destination")}, step
		}
	}
	return nil, step
}

func closureMessage(c dbgen.FindActiveClosuresRow, end string) string {
	return fmt.Sprintf("Service at the %s is temporarily suspended until %s (%s).",
		end, c.EndsAt.Format(time.RFC3339), strings.ToLower(strings.ReplaceAll(c.ReasonCode, "_", " ")))
}

// applyRules evaluates allow/deny rules for the lane.
//
// DENY is evaluated before ALLOW at every priority: an embargo must not be
// defeated by an equally-ranked permissive rule.
func (r *Resolver) applyRules(
	ctx context.Context, req Request, serviceID int64, res *Result, exp *Explanation,
) ([]Restriction, *Restriction, error) {
	// These scope columns are nullable in the rule table ("NULL matches any"),
	// so sqlc types the comparison parameters as pointers.
	params := dbgen.MatchServiceabilityRulesParams{
		OrganizationID:       req.OrganizationID,
		AsOf:                 req.At,
		CourierServiceID:     &serviceID,
		OriginPincodeID:      &res.originPincodeID,
		DestinationPincodeID: &res.destPincodeID,
		OriginStateID:        &res.originStateID,
		DestinationStateID:   &res.destStateID,
		RowLimit:             50,
	}
	if res.originZone != nil {
		params.OriginZoneID = &res.originZone.ZoneID
	}
	if res.destZone != nil {
		params.DestinationZoneID = &res.destZone.ZoneID
	}
	rows, err := r.q.MatchServiceabilityRules(ctx, params)
	if err != nil {
		return nil, nil, apierr.Internal(fmt.Errorf("match serviceability rules: %w", err))
	}

	step := ExplainStep{Step: "apply_rules", Outcome: "no_rules"}
	var restrictions []Restriction
	for _, rule := range rows {
		step.Candidates = append(step.Candidates, ExplainCandidate{
			ID: rule.PublicID, Code: rule.RuleType, Priority: rule.Priority,
			Selected: rule.RuleType == "DENY", Reason: rule.Name,
		})
		if rule.RuleType == "DENY" {
			step.Outcome = "denied"
			step.Detail = rule.Name
			exp.Steps = append(exp.Steps, step)
			msg := "This lane is not served for the selected service."
			if rule.Message != nil && *rule.Message != "" {
				msg = *rule.Message
			}
			code := ReasonDeniedByRule
			if rule.ReasonCode != nil && *rule.ReasonCode != "" {
				code = *rule.ReasonCode
			}
			return restrictions, &Restriction{Code: code, Message: msg}, nil
		}
		// ALLOW rules may still carry constraints the booking must satisfy.
		if details := decodeRestrictions(rule.Restrictions); len(details) > 0 {
			restrictions = append(restrictions, Restriction{
				Code: "LANE_RESTRICTION", Message: rule.Name, Details: details,
			})
		}
	}
	if len(rows) > 0 {
		step.Outcome = "allowed"
	}
	exp.Steps = append(exp.Steps, step)
	if restrictions == nil {
		restrictions = []Restriction{}
	}
	return restrictions, nil, nil
}

// resolveRoute selects the transit path.
//
// Precedence, highest first:
//  1. a manual routing override for this exact lane;
//  2. a route bound to the requested product;
//  3. a generic route;
//  4. a fallback route;
//  5. an implicit local route when both ends share a hub.
func (r *Resolver) resolveRoute(
	ctx context.Context, req Request, svc *dbgen.CourierService,
	originBranch, destBranch *network.Unit, res *Result, exp *Explanation,
) error {
	// 1. Override.
	override, err := r.q.MatchRoutingOverride(ctx, dbgen.MatchRoutingOverrideParams{
		OrganizationID:       req.OrganizationID,
		OriginPincodeID:      res.originPincodeID,
		DestinationPincodeID: res.destPincodeID,
		CourierServiceID:     &svc.ID,
		AsOf:                 req.At,
	})
	if err == nil {
		res.routeDefinitionID = &override.RouteDefinitionID
		res.RouteCode = override.RouteCode
		res.ResolutionSource = SourceOverride
		res.TransitHours = override.RouteTransitHours
		exp.Steps = append(exp.Steps, ExplainStep{
			Step: "resolve_route", Outcome: "override",
			Detail: fmt.Sprintf("override %s selected: %s", override.PublicID, override.Reason),
			Candidates: []ExplainCandidate{{
				ID: override.PublicID, Code: override.RouteCode,
				Priority: override.Priority, Selected: true, Reason: override.Reason,
			}},
		})
		return r.attachRouteDetail(ctx, req.OrganizationID, override.RouteDefinitionID, originBranch, destBranch, res)
	}
	if !database.IsNoRows(err) {
		return apierr.Internal(fmt.Errorf("match routing override: %w", err))
	}

	// 2-4. Hub-to-hub routes.
	originHub, err := r.network.ResolveHub(ctx, req.OrganizationID, originBranch.ID)
	if err != nil {
		return err
	}
	destHub, err := r.network.ResolveHub(ctx, req.OrganizationID, destBranch.ID)
	if err != nil {
		return err
	}
	if originHub != nil {
		res.OriginHub = &FacilityRef{ID: originHub.PublicID, Code: originHub.Code, Name: originHub.Name, UnitType: originHub.UnitType}
		res.originHubID = &originHub.ID
	}
	if destHub != nil {
		res.DestinationHub = &FacilityRef{ID: destHub.PublicID, Code: destHub.Code, Name: destHub.Name, UnitType: destHub.UnitType}
		res.destinationHubID = &destHub.ID
	}

	// 5. Same hub on both ends means an intra-hub delivery with no line haul.
	if originHub != nil && destHub != nil && originHub.ID == destHub.ID {
		res.ResolutionSource = SourceLocal
		res.RouteCode = originHub.Code + "-LOCAL"
		res.TransitHours = localTransitHours(svc)
		res.Legs = []Leg{{
			Sequence: 1, FromCode: originBranch.Code, FromName: originBranch.Name,
			ToCode: destBranch.Code, ToName: destBranch.Name,
			Mode: "ROAD", TransitHours: res.TransitHours,
		}}
		exp.Steps = append(exp.Steps, ExplainStep{
			Step: "resolve_route", Outcome: "local_delivery",
			Detail: "origin and destination share hub " + originHub.Code,
		})
		return nil
	}
	if originHub == nil {
		exp.Steps = append(exp.Steps, ExplainStep{
			Step: "resolve_route", Outcome: "no_origin_hub",
			Detail: originBranch.Code + " has no hub ancestor",
		})
		res.ReasonCode = ReasonNoOriginHub
		return nil
	}
	if destHub == nil {
		exp.Steps = append(exp.Steps, ExplainStep{
			Step: "resolve_route", Outcome: "no_destination_hub",
			Detail: destBranch.Code + " has no hub ancestor",
		})
		res.ReasonCode = ReasonNoDestinationHub
		return nil
	}

	rows, err := r.q.MatchRoutes(ctx, dbgen.MatchRoutesParams{
		OrganizationID:    req.OrganizationID,
		OriginUnitID:      originHub.ID,
		DestinationUnitID: destHub.ID,
		CourierServiceID:  &svc.ID,
		AsOf:              req.At,
		RowLimit:          20,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("match routes: %w", err))
	}
	step := ExplainStep{Step: "resolve_route", Outcome: "no_route"}
	if len(rows) == 0 {
		exp.Steps = append(exp.Steps, step)
		return nil
	}
	for i, route := range rows {
		reason := "generic"
		if route.CourierServiceID != nil {
			reason = "service-specific"
		}
		if route.IsFallback {
			reason += ", fallback"
		}
		step.Candidates = append(step.Candidates, ExplainCandidate{
			ID: route.PublicID, Code: route.Code, Priority: route.Priority,
			Selected: i == 0, Reason: reason,
		})
	}
	winner := rows[0]
	switch {
	case winner.IsFallback:
		res.ResolutionSource = SourceFallback
	case winner.CourierServiceID != nil:
		res.ResolutionSource = SourceServiceRoute
	default:
		res.ResolutionSource = SourceGenericRoute
	}
	res.routeDefinitionID = &winner.ID
	res.RouteID = winner.PublicID
	res.RouteCode = winner.Code
	res.TransitHours = winner.TransitHours
	step.Outcome = "selected"
	step.Detail = winner.Code
	exp.Steps = append(exp.Steps, step)

	return r.attachRouteDetail(ctx, req.OrganizationID, winner.ID, originBranch, destBranch, res)
}

// attachRouteDetail loads the legs of the chosen route.
func (r *Resolver) attachRouteDetail(
	ctx context.Context, orgID, routeID int64, originBranch, destBranch *network.Unit, res *Result,
) error {
	route, err := r.q.GetRouteDefinitionByID(ctx, dbgen.GetRouteDefinitionByIDParams{
		ID: routeID, OrganizationID: orgID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.Internal(fmt.Errorf("route %d referenced but missing", routeID))
		}
		return apierr.Internal(err)
	}
	res.RouteID = route.PublicID
	res.RouteCode = route.Code
	if res.TransitHours == 0 {
		res.TransitHours = route.TransitHours
	}
	if res.originHubID == nil {
		res.originHubID = &route.OriginUnitID
	}
	if res.destinationHubID == nil {
		res.destinationHubID = &route.DestinationUnitID
	}

	legs, err := r.q.ListRouteLegs(ctx, routeID)
	if err != nil {
		return apierr.Internal(err)
	}
	res.Legs = make([]Leg, 0, len(legs)+2)
	// First mile: origin branch to the route's first facility.
	if len(legs) > 0 && legs[0].FromCode != originBranch.Code {
		res.Legs = append(res.Legs, Leg{
			Sequence: 0, FromCode: originBranch.Code, FromName: originBranch.Name,
			ToCode: legs[0].FromCode, ToName: legs[0].FromName, Mode: "ROAD", TransitHours: 0,
		})
	}
	var legHours int32
	for _, l := range legs {
		leg := Leg{
			Sequence: l.Sequence, FromCode: l.FromCode, FromName: l.FromName,
			ToCode: l.ToCode, ToName: l.ToName, Mode: l.Mode, TransitHours: l.TransitHours,
		}
		if l.PartnerName != nil {
			leg.Partner = *l.PartnerName
		}
		legHours += l.TransitHours
		res.Legs = append(res.Legs, leg)
	}
	// Last mile: route's final facility to the destination branch.
	if len(legs) > 0 && legs[len(legs)-1].ToCode != destBranch.Code {
		res.Legs = append(res.Legs, Leg{
			Sequence: legs[len(legs)-1].Sequence + 1,
			FromCode: legs[len(legs)-1].ToCode, FromName: legs[len(legs)-1].ToName,
			ToCode: destBranch.Code, ToName: destBranch.Name, Mode: "ROAD", TransitHours: 0,
		})
	}
	// A route whose legs disagree with its headline transit_hours is a
	// configuration error; the legs are authoritative because they describe the
	// physical movement.
	if legHours > 0 && legHours != route.TransitHours {
		res.TransitHours = legHours
	}
	sort.SliceStable(res.Legs, func(i, j int) bool { return res.Legs[i].Sequence < res.Legs[j].Sequence })
	return nil
}

// applySLA computes the promised delivery time.
//
// The promise is transit time plus the product's remote-area uplift, measured
// from the booking instant, or from the next day when the booking is after the
// applicable cut-off.
func (r *Resolver) applySLA(req Request, svc *dbgen.CourierService, res *Result, exp *Explanation) {
	rules := product.ParseSLARules(svc.SlaRules)

	sla := res.TransitHours
	if sla == 0 {
		sla = svc.SlaTransitHours
	}
	if rules.MinTransitHours > 0 && sla < int32(rules.MinTransitHours) {
		sla = int32(rules.MinTransitHours)
	}
	if (res.originIsRemote || res.destIsRemote) && rules.RemoteAreaExtraHours > 0 {
		sla += int32(rules.RemoteAreaExtraHours)
	}

	start := req.At
	cutoff := rules.CutoffTime
	if svc.CutoffTime != nil && *svc.CutoffTime != "" {
		cutoff = *svc.CutoffTime
	}
	if cutoff != "" && afterCutoff(req.At, cutoff) {
		// Past the cut-off the parcel joins tomorrow's despatch.
		start = nextDayAtCutoff(req.At, cutoff)
		res.CutoffApplied = true
	}

	promised := start.Add(time.Duration(sla) * time.Hour)
	if rules.ExcludeSundays {
		for promised.Weekday() == time.Sunday {
			promised = promised.Add(24 * time.Hour)
		}
	}
	res.SLAHours = sla
	res.PromisedDeliveryAt = &promised
	exp.Steps = append(exp.Steps, ExplainStep{
		Step: "compute_sla", Outcome: "ok",
		Detail: fmt.Sprintf("transit=%dh sla=%dh cutoffApplied=%t promised=%s",
			res.TransitHours, sla, res.CutoffApplied, promised.Format(time.RFC3339)),
	})
}

func (r *Resolver) notServiceable(
	ctx context.Context, res *Result, exp *Explanation, code, message string,
) *Result {
	res.Serviceable = false
	if res.ReasonCode != "" && code == ReasonNoRoute {
		// A more specific reason was recorded during route resolution.
		code = res.ReasonCode
	}
	res.ReasonCode = code
	res.ReasonMessage = message
	exp.Steps = append(exp.Steps, ExplainStep{Step: "result", Outcome: "not_serviceable", Detail: code})
	r.storeExplanation(ctx, exp)
	// A refusal is exactly when the trace matters most, so it is attached on
	// the same terms as a success.
	if res.includeExplain {
		res.Explanation = exp
	}
	r.metrics.RecordBusiness("serviceability", "not_serviceable")
	return res
}

// storeExplanation caches the trace so the administrator debug endpoint can
// retrieve it by id. Losing it costs an explanation, never a booking.
func (r *Resolver) storeExplanation(ctx context.Context, exp *Explanation) {
	r.cache.SetJSON(ctx, r.explanationKey(exp.ID), exp, r.explainTTL)
}

// GetExplanation retrieves a stored decision trace.
func (r *Resolver) GetExplanation(ctx context.Context, id string) (*Explanation, error) {
	if !publicid.Valid(publicid.PrefixExplanation, id) {
		return nil, apierr.NotFound("Routing explanation")
	}
	var exp Explanation
	if err := r.cache.GetJSON(ctx, r.explanationKey(id), &exp); err != nil {
		return nil, apierr.NotFound("Routing explanation").
			WithDetail("hint", "Explanations are retained for a limited time. Re-run the routing debug endpoint to produce a fresh trace.")
	}
	return &exp, nil
}

func (r *Resolver) explanationKey(id string) string { return r.cache.Key("routing-explain", id) }

// ResolvePickupBranch returns the branch that serves pickups at a PIN code.
//
// Release 2 needs this without a full origin-to-destination resolution: a
// pickup has no destination yet. It reuses the same service-area precedence as
// booking — priority, then specificity, then lowest id — so a pickup lands at
// the same branch that would have been chosen as the origin for a shipment
// booked from that address.
func (r *Resolver) ResolvePickupBranch(
	ctx context.Context, orgID int64, pincode string, at time.Time,
) (*int64, error) {
	pin, err := r.geo.RequireActivePincode(ctx, pincode, geography.DefaultCountry)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ResolveServingUnits(ctx, dbgen.ResolveServingUnitsParams{
		OrganizationID: orgID, PincodeID: pin.ID, AreaType: "PICKUP",
		AsOf: at, RowLimit: 1,
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("resolve pickup branch: %w", err))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	id := rows[0].UnitID
	return &id, nil
}

// ResolveDeliveryBranch returns the branch that serves deliveries at a PIN
// code. RTO uses it to find where a returned parcel should be handed back.
func (r *Resolver) ResolveDeliveryBranch(
	ctx context.Context, orgID int64, pincode string, at time.Time,
) (*int64, error) {
	pin, err := r.geo.RequireActivePincode(ctx, pincode, geography.DefaultCountry)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ResolveServingUnits(ctx, dbgen.ResolveServingUnitsParams{
		OrganizationID: orgID, PincodeID: pin.ID, AreaType: "DELIVERY",
		AsOf: at, RowLimit: 1,
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("resolve delivery branch: %w", err))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	id := rows[0].UnitID
	return &id, nil
}

// InvalidateOrganization drops every cached routing answer for a tenant.
// Called whenever service areas, rules, routes, overrides or closures change,
// because any of them can alter a previously cached decision.
func (r *Resolver) InvalidateOrganization(ctx context.Context, orgID int64) {
	r.cache.DeletePrefix(ctx, r.cache.Key("routing", fmt.Sprint(orgID))+":")
}

// ---- helpers ---------------------------------------------------------------

func facilityRef(u *network.Unit) *FacilityRef {
	if u == nil {
		return nil
	}
	return &FacilityRef{ID: u.PublicID, Code: u.Code, Name: u.Name, UnitType: u.UnitType}
}

// localTransitHours is the intra-hub promise: the product's own SLA, capped at
// 24 hours because a same-hub delivery should never inherit a national transit
// time.
func localTransitHours(svc *dbgen.CourierService) int32 {
	if svc.SlaTransitHours < 24 {
		return svc.SlaTransitHours
	}
	return 24
}

func afterCutoff(at time.Time, cutoff string) bool {
	h, m, ok := parseHHMM(cutoff)
	if !ok {
		return false
	}
	return at.Hour() > h || (at.Hour() == h && at.Minute() >= m)
}

func nextDayAtCutoff(at time.Time, cutoff string) time.Time {
	h, m, ok := parseHHMM(cutoff)
	if !ok {
		return at
	}
	next := at.AddDate(0, 0, 1)
	return time.Date(next.Year(), next.Month(), next.Day(), h, m, 0, 0, at.Location())
}

func parseHHMM(s string) (hour, minute int, ok bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func decodeRestrictions(raw []byte) map[string]any {
	if len(raw) == 0 || string(raw) == "{}" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- accessors for internal identifiers ------------------------------------
//
// The resolved database ids are unexported so they cannot leak into a JSON
// response; booking needs them to write foreign keys, so they are exposed
// through explicit accessors instead.

// RouteDefinitionID returns the matched route's internal id, or nil.
func (r *Result) RouteDefinitionID() *int64 { return r.routeDefinitionID }

// OriginBranchID returns the resolved origin branch id, or nil.
func (r *Result) OriginBranchID() *int64 { return r.originBranchID }

// OriginHubID returns the resolved origin hub id, or nil.
func (r *Result) OriginHubID() *int64 { return r.originHubID }

// DestinationHubID returns the resolved destination hub id, or nil.
func (r *Result) DestinationHubID() *int64 { return r.destinationHubID }

// DestinationBranchID returns the resolved destination branch id, or nil.
func (r *Result) DestinationBranchID() *int64 { return r.destinationBranchID }

// OriginPincodeID returns the resolved origin PIN code id.
func (r *Result) OriginPincodeID() int64 { return r.originPincodeID }

// DestinationPincodeID returns the resolved destination PIN code id.
func (r *Result) DestinationPincodeID() int64 { return r.destPincodeID }

// OriginZoneID returns the tenant's origin zone id, or 0 when unmapped.
func (r *Result) OriginZoneID() int64 {
	if r.originZone == nil {
		return 0
	}
	return r.originZone.ZoneID
}

// DestinationZoneID returns the tenant's destination zone id, or 0 when unmapped.
func (r *Result) DestinationZoneID() int64 {
	if r.destZone == nil {
		return 0
	}
	return r.destZone.ZoneID
}
