// Package opsapi is the HTTP transport for the Release 2 operational modules.
//
// Release 1 put handlers inside each module, which works when a module owns a
// self-contained resource. Release 2 does not look like that: a scan touches
// shipments, bags and exceptions; a manifest dispatch drives bags, trips and
// shipments; a delivery failure opens an NDR case which may start an RTO. Those
// modules already depend on one another through explicit hooks, and giving each
// its own handler package would have meant either duplicating the request
// plumbing twelve times or introducing import cycles between transport layers.
//
// So the twelve operational modules keep their business logic pure — no
// net/http, no chi — and this package owns the request parsing, validation,
// permission checks and response shaping for all of them. The trade is one
// larger transport package against twelve smaller ones that would each need the
// same facility resolution, device envelope and pagination handling. See ADR
// 0011.
package opsapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/bagging"
	"github.com/ceserve/courier-os/internal/delivery"
	"github.com/ceserve/courier-os/internal/hubops"
	"github.com/ceserve/courier-os/internal/linehaul"
	"github.com/ceserve/courier-os/internal/manifest"
	"github.com/ceserve/courier-os/internal/ndr"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/pickup"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/pod"
	"github.com/ceserve/courier-os/internal/rto"
	"github.com/ceserve/courier-os/internal/scanning"
	"github.com/ceserve/courier-os/internal/tracking"
)

// Handler serves every Release 2 operational endpoint.
type Handler struct {
	units    *ops.Resolver
	pickup   *pickup.Service
	scanning *scanning.Service
	bagging  *bagging.Service
	manifest *manifest.Service
	linehaul *linehaul.Service
	hubops   *hubops.Service
	delivery *delivery.Service
	ndr      *ndr.Service
	rto      *rto.Service
	pod      *pod.Service
	tracking *tracking.Service
}

// Services groups the module services the transport needs.
type Services struct {
	Units    *ops.Resolver
	Pickup   *pickup.Service
	Scanning *scanning.Service
	Bagging  *bagging.Service
	Manifest *manifest.Service
	Linehaul *linehaul.Service
	HubOps   *hubops.Service
	Delivery *delivery.Service
	NDR      *ndr.Service
	RTO      *rto.Service
	POD      *pod.Service
	Tracking *tracking.Service
}

// New builds the operational transport.
func New(s Services) *Handler {
	return &Handler{
		units: s.Units, pickup: s.Pickup, scanning: s.Scanning, bagging: s.Bagging,
		manifest: s.Manifest, linehaul: s.Linehaul, hubops: s.HubOps,
		delivery: s.Delivery, ndr: s.NDR, rto: s.RTO, pod: s.POD, tracking: s.Tracking,
	}
}

// require wraps a handler in a permission check.
//
// The permission named here is the coarse gate; the fine-grained checks —
// custody, operating-unit scope, object ownership — live in the services, where
// they can see the object being acted on. Both are needed: a permission alone
// would let a Bangalore branch manager scan a Delhi parcel.
func require(permission string, h httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := tenantPrincipal(r)
		if err != nil {
			return err
		}
		if err := p.Require(permission); err != nil {
			return err
		}
		return h(w, r)
	})
}

// requireAny gates on holding at least one of several permissions, for
// endpoints a field agent and a dispatcher both use.
func requireAny(h httpx.Handler, permissions ...string) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := tenantPrincipal(r)
		if err != nil {
			return err
		}
		if !p.CanAny(permissions...) {
			return apierr.Forbidden("You do not have permission to perform this action.").
				WithDetail("requiredPermissions", permissions)
		}
		return h(w, r)
	})
}

// Routes mounts the authenticated operational surface.
func (h *Handler) Routes(r chi.Router) {
	r.Route("/pickups", h.pickupRoutes)
	r.Route("/pickup-runs", h.pickupRunRoutes)
	r.Route("/scans", h.scanRoutes)
	r.Route("/bags", h.bagRoutes)
	r.Route("/manifests", h.manifestRoutes)
	r.Route("/carriers", h.carrierRoutes)
	r.Route("/vehicles", h.vehicleRoutes)
	r.Route("/drivers", h.driverRoutes)
	r.Route("/trips", h.tripRoutes)
	r.Route("/hub", h.hubRoutes)
	r.Route("/exceptions", h.exceptionRoutes)
	r.Route("/reconciliations", h.reconciliationRoutes)
	r.Route("/delivery-runs", h.deliveryRunRoutes)
	r.Route("/deliveries", h.deliveryRoutes)
	r.Route("/ndr", h.ndrRoutes)
	r.Route("/rto", h.rtoRoutes)
	r.Route("/pod", h.podRoutes)
}

// PublicRoutes mounts the unauthenticated tracking surface.
//
// It is deliberately separate: everything here is reachable without a token, so
// it should be obvious in the router which endpoints those are.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/{awb}", httpx.Wrap(h.track))
}

// ---------------------------------------------------------------------------
// shared request helpers
// ---------------------------------------------------------------------------

// body decodes a JSON request with unknown-field rejection, which is the
// mass-assignment control (§35): a client cannot smuggle a field the handler
// did not intend to accept.
func body[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var dst T
	if err := httpx.DecodeJSON(w, r, &dst); err != nil {
		return dst, err
	}
	return dst, nil
}

// pathID reads and validates a prefixed public identifier from the URL.
func pathID(r *http.Request, param, prefix, resource string) (string, error) {
	return httpx.PathPublicID(r, param, prefix, resource)
}

func validator() *validate.Validator { return validate.New() }
