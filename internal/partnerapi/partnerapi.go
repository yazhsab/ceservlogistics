// Package partnerapi is the outward-facing HTTP surface for partner
// integrations (M32).
//
// # What makes this different from the internal API
//
// Everything under /api/v1 is authenticated by a user session and authorized by
// permissions. Everything under /api/v1/partner is authenticated by an API key
// and authorized by *scopes*. The two systems never mix: a partner cannot
// acquire a person's rights by holding a credential, and no role can be granted
// a partner scope by accident.
//
// The endpoints themselves call the same services the internal API calls. A
// partner booking a shipment goes through shipment.Booker, is priced by the
// same rate card, is routed by the same resolver and lands in the same tables.
// There is no "partner shipment" — a second code path would be a second set of
// business rules to keep in step, and they would not stay in step.
//
// # Three gates, in order
//
//  1. Authenticate — the key must exist, verify, be active, and be called from
//     a permitted network.
//  2. Rate limit — per key, using the key's own limit where it has one.
//  3. Scope — checked per endpoint, naming the scope that was missing.
//
// Usage is recorded after the response is written, outside the request's
// transaction, because a hot partner key must not serialise its own traffic on
// one row and a logging failure must never fail the request it describes.
package partnerapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/pickup"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/idempotency"
	"github.com/ceserve/courier-os/internal/platform/security"
	"github.com/ceserve/courier-os/internal/pod"
	"github.com/ceserve/courier-os/internal/pricing"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/internal/tracking"
)

// Handler serves both the partner-facing API and its management surface.
type Handler struct {
	keys     *partner.KeyService
	webhooks *partner.WebhookService

	routing  *serviceability.Resolver
	pricing  *pricing.Handler
	booker   *shipment.Booker
	tracking *tracking.Service
	pickup   *pickup.Service
	pod      *pod.Service

	idempotency *idempotency.Executor
	limiter     *security.Limiter
	audit       *audit.Recorder
	log         *slog.Logger

	rateLimitEnabled bool
	// defaultRateLimit applies to a key that does not set its own.
	defaultRateLimit int
}

// Services groups what the transport needs.
type Services struct {
	Keys     *partner.KeyService
	Webhooks *partner.WebhookService

	Routing  *serviceability.Resolver
	Pricing  *pricing.Handler
	Booker   *shipment.Booker
	Tracking *tracking.Service
	Pickup   *pickup.Service
	POD      *pod.Service

	Idempotency *idempotency.Executor
	Limiter     *security.Limiter
	Audit       *audit.Recorder
	Logger      *slog.Logger

	RateLimitEnabled bool
	DefaultRateLimit int
}

// New builds the partner transport.
func New(s Services) *Handler {
	limit := s.DefaultRateLimit
	if limit <= 0 {
		limit = 120
	}
	return &Handler{
		keys: s.Keys, webhooks: s.Webhooks,
		routing: s.Routing, pricing: s.Pricing, booker: s.Booker,
		tracking: s.Tracking, pickup: s.Pickup, pod: s.POD,
		idempotency: s.Idempotency, limiter: s.Limiter, audit: s.Audit, log: s.Logger,
		rateLimitEnabled: s.RateLimitEnabled, defaultRateLimit: limit,
	}
}

// ---------------------------------------------------------------------------
// Authentication context
// ---------------------------------------------------------------------------

type ctxKey int

const authKey ctxKey = iota

func withAuth(ctx context.Context, a *partner.Authenticated) context.Context {
	return context.WithValue(ctx, authKey, a)
}

// authFrom returns the verified key for the request.
func authFrom(r *http.Request) (*partner.Authenticated, error) {
	a, _ := r.Context().Value(authKey).(*partner.Authenticated)
	if a == nil {
		return nil, apierr.Unauthorized(partner.CodeInvalidKey, "An API credential is required.")
	}
	return a, nil
}

// AuthHeader is where the credential goes.
//
// Bearer, not a bespoke header: every HTTP client already knows how to send it,
// and proxies already know not to log it.
const AuthHeader = "Authorization"

// Authenticate verifies the API key and installs both the partner credential
// and a tenant principal derived from it.
//
// The principal is what every downstream service reads for tenancy. It carries
// no permissions at all — a partner's rights come from its scopes, checked at
// the route — and no user id, because there is no person behind an API key.
func (h *Handler) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r.Header.Get(AuthHeader))
		if token == "" {
			httpx.WriteError(w, r, apierr.Unauthorized(partner.CodeInvalidKey,
				"An API credential is required. Send it as: Authorization: Bearer <keyId>.<secret>"))
			return
		}

		clientIP := net.ParseIP(httpx.ClientIP(r.Context()))
		auth, err := h.keys.Verify(r.Context(), token, clientIP)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}

		// A partner principal: tenant identity only. HasUnscopedRole is true
		// because a key belongs to the organization rather than to a branch —
		// an integration books for the whole network — while the tenant
		// predicate on every query still confines it to its own organization.
		p := tenant.NewPrincipal(tenant.Principal{
			OrganizationID:        auth.Principal.OrganizationID,
			OrganizationPublicID:  auth.Principal.OrganizationPublicID,
			OrganizationCode:      auth.Principal.OrganizationCode,
			OrganizationCurrency:  auth.Principal.OrganizationCurrency,
			OrganizationTimezone:  auth.Principal.OrganizationTimezone,
			OrganizationCountry:   auth.Principal.OrganizationCountry,
			OrganizationAWBPrefix: auth.Principal.OrganizationAWBPrefix,
			HasUnscopedRole:       true,
			IsPartner:             true,
			PartnerKeyID:          auth.KeyID,
			PartnerKeyName:        auth.KeyName,
		}, nil)
		auth.Principal = p

		ctx := withAuth(tenant.WithPrincipal(r.Context(), p), auth)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RateLimit throttles per key rather than per IP.
//
// Per IP would be wrong in both directions: several partners behind one NAT
// would share a budget, and one partner spread across a fleet would get many.
// The key's own limit wins where it has one, so a high-volume integration can
// be raised without lifting the ceiling for everybody.
func (h *Handler) RateLimit(next http.Handler) http.Handler {
	if !h.rateLimitEnabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, err := authFrom(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		limit := int(auth.RateLimit)
		if limit <= 0 {
			limit = h.defaultRateLimit
		}
		res := h.limiter.Allow(r.Context(), "apikey:"+strconv.FormatInt(auth.KeyID, 10), limit)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(int64(res.RetryAfter.Seconds()), 10))
		if !res.Allowed {
			w.Header().Set("Retry-After", strconv.FormatInt(int64(res.RetryAfter.Seconds()), 10))
			httpx.WriteError(w, r, apierr.RateLimited(
				"This API key has exceeded its request rate. Retry after the interval in Retry-After."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RecordUsage writes one row per partner request.
//
// It runs after the response, on a context detached from the request's, so a
// client that hangs up mid-response still leaves a usage record — otherwise the
// requests most worth investigating would be exactly the ones not written down.
func (h *Handler) RecordUsage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, err := authFrom(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(rec, r)
		elapsed := int32(time.Since(started).Milliseconds())

		route := chi.RouteContext(r.Context()).RoutePattern()
		requestID := httpx.RequestID(r.Context())
		clientIP := net.ParseIP(httpx.ClientIP(r.Context()))

		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		h.keys.RecordUsage(ctx, auth.Principal.OrganizationID, auth.KeyID,
			r.Method, route, int32(rec.status), elapsed, requestID, "", clientIP)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.status = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(b)
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// Routes mounts the partner-facing API. The caller wraps this group in
// Authenticate, RateLimit and RecordUsage.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/serviceability", scoped(partner.ScopeServiceabilityRead, h.checkServiceability))
	r.Post("/quotes", scoped(partner.ScopePricingRead, h.quote))

	r.Route("/shipments", func(sr chi.Router) {
		sr.Post("/", scoped(partner.ScopeShipmentCreate, h.createShipment))
		sr.Get("/{shipmentId}", scoped(partner.ScopeShipmentRead, h.getShipment))
		sr.Post("/{shipmentId}/cancel", scoped(partner.ScopeShipmentCancel, h.cancelShipment))
		sr.Get("/{shipmentId}/label", scoped(partner.ScopeLabelRead, h.label))
		sr.Get("/{shipmentId}/pod", scoped(partner.ScopePODRead, h.proofOfDelivery))
	})

	r.Get("/tracking/{awb}", scoped(partner.ScopeTrackingRead, h.track))

	r.Route("/pickups", func(pr chi.Router) {
		pr.Post("/", scoped(partner.ScopePickupCreate, h.createPickup))
		pr.Get("/{pickupId}", scoped(partner.ScopePickupRead, h.getPickup))
	})

	// A partner may manage its own webhook endpoints without a person logging
	// in, which is what makes an integration self-service.
	r.Route("/webhooks", func(wr chi.Router) {
		wr.Get("/endpoints", scoped(partner.ScopeWebhookManage, h.listEndpointsForKey))
		wr.Post("/endpoints", scoped(partner.ScopeWebhookManage, h.createEndpointForKey))
		wr.Get("/deliveries", scoped(partner.ScopeWebhookManage, h.listDeliveriesForKey))
	})

	// Self-description, so an integrator can confirm what their key can do
	// without waiting on support. Needs no scope: it reveals only what the
	// caller already holds.
	r.Get("/whoami", httpx.Wrap(h.whoami))
}

// scoped gates a handler on an API scope.
func scoped(scope string, h httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		auth, err := authFrom(r)
		if err != nil {
			return err
		}
		if err := auth.Require(scope); err != nil {
			return err
		}
		return h(w, r)
	})
}

// principal returns the tenant identity for a partner request.
func principal(r *http.Request) (*tenant.Principal, error) {
	auth, err := authFrom(r)
	if err != nil {
		return nil, err
	}
	return auth.Principal, nil
}

func bearer(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	// A bare token is accepted too. Rejecting it would be defensible, but the
	// most common integration bug is a missing "Bearer " and the error it
	// produces ("credential not valid") sends people hunting for the wrong
	// problem.
	return strings.TrimSpace(header)
}
