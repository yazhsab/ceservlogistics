// Package api wires every module into one HTTP surface.
//
// This is the only place that knows the full module graph. Individual modules
// depend on the platform and on each other's services, never on the router, so
// the dependency direction stays one-way and a module can be tested without
// standing up the whole API.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/analytics"
	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/bagging"
	"github.com/ceserve/courier-os/internal/billing"
	"github.com/ceserve/courier-os/internal/cod"
	"github.com/ceserve/courier-os/internal/collection"
	"github.com/ceserve/courier-os/internal/commission"
	"github.com/ceserve/courier-os/internal/customer"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/delivery"
	"github.com/ceserve/courier-os/internal/financeapi"
	"github.com/ceserve/courier-os/internal/financeops"
	"github.com/ceserve/courier-os/internal/geography"
	"github.com/ceserve/courier-os/internal/hubops"
	"github.com/ceserve/courier-os/internal/ledger"
	"github.com/ceserve/courier-os/internal/linehaul"
	"github.com/ceserve/courier-os/internal/manifest"
	"github.com/ceserve/courier-os/internal/ndr"
	"github.com/ceserve/courier-os/internal/network"
	"github.com/ceserve/courier-os/internal/notification"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/opsapi"
	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/partnerapi"
	"github.com/ceserve/courier-os/internal/pickup"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/idempotency"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/security"
	"github.com/ceserve/courier-os/internal/platform/storage"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/pod"
	"github.com/ceserve/courier-os/internal/portal"
	"github.com/ceserve/courier-os/internal/portalapi"
	"github.com/ceserve/courier-os/internal/pricing"
	"github.com/ceserve/courier-os/internal/product"
	"github.com/ceserve/courier-os/internal/reporting"
	"github.com/ceserve/courier-os/internal/rto"
	"github.com/ceserve/courier-os/internal/scanning"
	"github.com/ceserve/courier-os/internal/serviceability"
	"github.com/ceserve/courier-os/internal/settlement"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/internal/tracking"
)

// Dependencies are the process-level resources the API needs.
type Dependencies struct {
	Config    *config.Config
	DB        *database.DB
	Cache     *cache.Cache
	Logger    *slog.Logger
	Telemetry *telemetry.Provider
	// Storage holds POD artifacts and generated documents. Release 2 requires
	// it; the caller decides whether that is S3 or the filesystem.
	Storage storage.Store
}

// Application holds every constructed module, so tests can reach services
// directly instead of going through HTTP when that is the clearer assertion.
type Application struct {
	Config  *config.Config
	DB      *database.DB
	Cache   *cache.Cache
	Queries *dbgen.Queries
	Logger  *slog.Logger
	Metrics *telemetry.Metrics

	Audit           *audit.Recorder
	Auth            *auth.Service
	Geography       *geography.Service
	GeographyImport *geography.ImportService
	Network         *network.Service
	Product         *product.Service
	Routing         *serviceability.Resolver
	Pricing         *pricing.Engine
	Customer        *customer.Service
	Booker          *shipment.Booker
	Jobs            *jobs.Enqueuer
	Idempotency     *idempotency.Executor

	// Release 2 — physical operations.
	Transitioner *shipment.Transitioner
	Units        *ops.Resolver
	Codes        *ops.CodeAllocator
	Pickup       *pickup.Service
	Scanning     *scanning.Service
	Bagging      *bagging.Service
	Manifest     *manifest.Service
	Linehaul     *linehaul.Service
	HubOps       *hubops.Service
	Delivery     *delivery.Service
	NDR          *ndr.Service
	RTO          *rto.Service
	POD          *pod.Service
	Tracking     *tracking.Service

	// Release 3 finance. Every module here posts through Ledger, which is the
	// accounting system of record; none of them keeps its own idea of a balance.
	Ledger     *ledger.Service
	Commission *commission.Service
	COD        *cod.Service
	Settlement *settlement.Service
	Billing    *billing.Service
	Collection *collection.Service

	// Release 4 productization.
	Analytics    *analytics.Service
	Notification *notification.Service
	Portal       *portal.Service
	Reporting    *reporting.Service
	// Storage is the object store the POD and report modules write to. Held on
	// the application so tests can assert what actually landed in the bucket.
	Storage  storage.Store
	Senders  *notification.Registry
	APIKeys  *partner.KeyService
	Webhooks *partner.WebhookService

	handler http.Handler
}

// New constructs the application graph.
func New(deps Dependencies) *Application {
	cfg := deps.Config
	q := dbgen.New(deps.DB.Pool)
	metrics := deps.Telemetry.Metrics

	rec := audit.NewRecorder(q, deps.Logger)
	enqueuer := jobs.NewEnqueuer(q)
	idem := idempotency.NewExecutor(deps.DB, q)

	authSvc := auth.NewService(deps.DB, q, deps.Cache, rec, cfg.Auth, deps.Logger, metrics)
	geoSvc := geography.NewService(deps.DB, q, deps.Cache, deps.Logger, metrics, cfg.Booking.RoutingCacheTTL)
	importSvc := geography.NewImportService(deps.DB, q, geoSvc, enqueuer, rec, deps.Logger, cfg.HTTP.MaxUploadBytes)
	netSvc := network.NewService(deps.DB, q, deps.Logger)
	prodSvc := product.NewService(deps.DB, q, deps.Logger)
	routing := serviceability.NewResolver(q, geoSvc, netSvc, prodSvc, deps.Cache, deps.Logger, metrics,
		cfg.Booking.RoutingCacheTTL, cfg.Booking.ExplanationTTL)
	priceEngine := pricing.NewEngine(deps.DB, q, deps.Logger)
	custSvc := customer.NewService(deps.DB, q, deps.Logger)
	booker := shipment.NewBooker(deps.DB, q, geoSvc, prodSvc, routing, priceEngine, custSvc,
		rec, deps.Logger, metrics, cfg.Booking.MaxPackagesPerShip)

	// Release 2 operational modules. The transition engine is shared: every
	// state change in the release goes through it, which is what keeps custody,
	// permission and audit checks in exactly one place.
	transitioner := shipment.NewTransitioner(q, rec, deps.Logger, metrics)
	units := ops.NewResolver(q)
	codes := ops.NewCodeAllocator(q)

	pickupSvc := pickup.NewService(deps.DB, q, geoSvc, routing, units, codes, transitioner, rec, deps.Logger)
	scanSvc := scanning.NewService(deps.DB, q, units, transitioner, rec, deps.Logger, metrics)
	bagSvc := bagging.NewService(deps.DB, q, units, codes, transitioner, rec, deps.Logger)
	manifestSvc := manifest.NewService(deps.DB, q, units, codes, transitioner, enqueuer, rec, deps.Logger)
	linehaulSvc := linehaul.NewService(deps.DB, q, units, codes, transitioner, rec, deps.Logger)
	hubSvc := hubops.NewService(deps.DB, q, units, codes, transitioner, rec, deps.Logger)
	deliverySvc := delivery.NewService(deps.DB, q, units, codes, transitioner, rec, deps.Logger, metrics)
	ndrSvc := ndr.NewService(deps.DB, q, units, codes, transitioner, rec, deps.Logger, metrics)
	rtoSvc := rto.NewService(deps.DB, q, units, codes, routing, transitioner, rec, deps.Logger)
	podSvc := pod.NewService(deps.DB, q, deps.Storage, units, transitioner, rec, deps.Logger,
		cfg.HTTP.MaxUploadBytes)
	trackSvc := tracking.NewService(q, deps.Cache, deps.Logger, metrics, cfg.Booking.RoutingCacheTTL)

	// Release 3 finance. Ledger is constructed first because the other four
	// take it: commission, COD, settlement and billing all post through it
	// rather than tracking balances of their own.
	ledgerSvc := ledger.NewService(deps.DB, q, rec, deps.Logger)
	commissionSvc := commission.NewService(deps.DB, q, ledgerSvc, rec, deps.Logger, metrics)
	codSvc := cod.NewService(deps.DB, q, ledgerSvc, rec, deps.Logger, metrics)
	settlementSvc := settlement.NewService(deps.DB, q, ledgerSvc, rec, deps.Logger, metrics)
	billingSvc := billing.NewService(deps.DB, q, ledgerSvc, rec, deps.Logger)
	collectionSvc := collection.NewService(deps.DB, q, rec)

	// Release 4. Notifications and webhooks are both outbox consumers of the
	// transition engine: they write rows inside the state change's transaction
	// and contact the outside world later, from a worker.
	senders := notification.NewRegistry()
	for _, channel := range []string{
		notification.ChannelSMS, notification.ChannelEmail, notification.ChannelWhatsApp,
		notification.ChannelPush,
	} {
		// Every channel starts on a sender that records what it would have
		// sent. That is deliberately not "no sender": a missing sender
		// suppresses the notification, and an operator would rather see the
		// message that was composed than a row saying nothing happened.
		senders.Register(notification.NewLogSender(channel, deps.Logger))
	}
	// Real providers replace the logging sender for the channels they serve.
	// A misconfigured one is refused at startup rather than registered and left
	// to fail every message, which would read as "the provider is down".
	registerProviders(senders, cfg.Notification, deps.Logger)
	notifySvc := notification.NewService(deps.DB, q, enqueuer, senders, rec, deps.Logger)
	analyticsSvc := analytics.NewService(deps.DB, q, enqueuer, deps.Logger)
	portalSvc := portal.NewService(deps.DB, q, deps.Logger)
	reportSvc := reporting.NewService(deps.DB, q, enqueuer, deps.Storage, rec, deps.Logger)
	keySvc := partner.NewKeyService(deps.DB, q, security.NewHasher(security.DefaultArgon2Params()),
		rec, deps.Logger)
	webhookSvc := partner.NewWebhookService(deps.DB, q, enqueuer, rec, deps.Logger)

	// Every observer runs inside the transition's transaction. Order is fixed
	// and deliberate.
	//
	// Finance first, and it is the one observer allowed to fail the transition.
	// A delivered COD parcel with no record of the cash the agent is holding is
	// the worst state this system can reach, so if the liability cannot be
	// written the delivery does not complete and the agent retries. The other
	// two are notifications: a partner integration failing must never stop a
	// consignee's delivery text, and neither may stop the parcel.
	financeOps := financeops.New(q, codSvc, commissionSvc, deps.Logger)
	transitioner.Observe(financeOps.ShipmentObserver())
	transitioner.Observe(notifySvc.ShipmentObserver(cfg.Booking.PublicTrackingURL))
	transitioner.Observe(webhookSvc.ShipmentObserver())
	// Cancellation does not run through the engine, so it is given the same
	// observers explicitly.
	booker.SetTransitioner(transitioner)

	// The three cross-module hooks. They are function values rather than direct
	// imports because delivery -> ndr -> rto is a chain, and wiring it here
	// keeps the packages acyclic while making the dependency explicit.
	deliverySvc.SetNDROpener(ndrSvc.OpenFromDeliveryFailure)
	ndrSvc.SetRTOInitiator(func(
		ctx context.Context, tx pgx.Tx, p *tenant.Principal,
		sh dbgen.Shipment, ndrCaseID *int64, reasonCode, notes string, byPolicy bool,
	) (string, error) {
		if byPolicy {
			return rtoSvc.InitiateByPolicy(ctx, tx, p, sh, ndrCaseID, reasonCode, notes)
		}
		return rtoSvc.Initiate(ctx, tx, p, sh, ndrCaseID, reasonCode, notes)
	})

	app := &Application{
		Config: cfg, DB: deps.DB, Cache: deps.Cache, Queries: q,
		Logger: deps.Logger, Metrics: metrics,
		Audit: rec, Auth: authSvc, Geography: geoSvc, GeographyImport: importSvc,
		Network: netSvc, Product: prodSvc, Routing: routing, Pricing: priceEngine,
		Customer: custSvc, Booker: booker, Jobs: enqueuer, Idempotency: idem,

		Transitioner: transitioner, Units: units, Codes: codes,
		Pickup: pickupSvc, Scanning: scanSvc, Bagging: bagSvc, Manifest: manifestSvc,
		Linehaul: linehaulSvc, HubOps: hubSvc, Delivery: deliverySvc,
		NDR: ndrSvc, RTO: rtoSvc, POD: podSvc, Tracking: trackSvc,

		Ledger: ledgerSvc, Commission: commissionSvc, COD: codSvc,
		Settlement: settlementSvc, Billing: billingSvc, Collection: collectionSvc,

		Analytics: analyticsSvc, Notification: notifySvc, Senders: senders,
		Portal: portalSvc, Reporting: reportSvc, Storage: deps.Storage,
		APIKeys: keySvc, Webhooks: webhookSvc,
	}
	app.handler = app.buildRouter(deps)
	return app
}

// Handler returns the fully wired HTTP handler.
func (a *Application) Handler() http.Handler { return a.handler }

func (a *Application) buildRouter(deps Dependencies) http.Handler {
	cfg := a.Config
	limiter := security.NewLimiter(a.Cache, cfg.Security.RateLimitPerMinute, a.Logger)

	authHandler := auth.NewHandler(a.Auth, cfg.Auth, cfg.IsProduction())
	userHandler := auth.NewUserHandler(a.Auth)
	roleHandler := auth.NewRoleHandler(a.Auth)
	geoHandler := geography.NewHandler(a.Geography, a.Audit, a.GeographyImport)
	netHandler := network.NewHandler(a.Network, a.Audit)
	prodHandler := product.NewHandler(a.Product, a.Audit)
	svcHandler := serviceability.NewHandler(a.Routing, a.DB, a.Queries, a.Geography, a.Audit)
	priceHandler := pricing.NewHandler(a.Pricing, a.Queries, a.Geography, a.Product, a.Audit)
	custHandler := customer.NewHandler(a.Customer, a.Geography, a.Audit)
	shipHandler := shipment.NewHandler(a.Booker, a.Idempotency, cfg.Booking.MaxPackagesPerShip)
	opsHandler := opsapi.New(opsapi.Services{
		Units: a.Units, Pickup: a.Pickup, Scanning: a.Scanning, Bagging: a.Bagging,
		Manifest: a.Manifest, Linehaul: a.Linehaul, HubOps: a.HubOps,
		Delivery: a.Delivery, NDR: a.NDR, RTO: a.RTO, POD: a.POD, Tracking: a.Tracking,
	})

	finHandler := financeapi.New(financeapi.Services{
		Ledger: a.Ledger, Commission: a.Commission, COD: a.COD,
		Settlement: a.Settlement, Billing: a.Billing,
	})

	commandHandler := analytics.NewHandler(a.Analytics, a.Units)
	notifyHandler := notification.NewHandler(a.Notification)
	reportHandler := reporting.NewHandler(a.Reporting, a.Queries)
	collectionHandler := collection.NewHandler(a.Collection)
	portalHandler := portalapi.New(portalapi.Services{
		Portal: a.Portal, Booker: a.Booker, Tracking: a.Tracking,
		Units: a.Units, Queries: a.Queries,
	})

	partnerHandler := partnerapi.New(partnerapi.Services{
		Keys: a.APIKeys, Webhooks: a.Webhooks,
		Routing: a.Routing, Pricing: priceHandler, Booker: a.Booker,
		Tracking: a.Tracking, Pickup: a.Pickup, POD: a.POD,
		Idempotency: a.Idempotency, Limiter: limiter, Audit: a.Audit, Logger: a.Logger,
		RateLimitEnabled: cfg.Security.RateLimitEnabled,
		DefaultRateLimit: cfg.Security.PartnerRateLimitPerMinute,
	})

	r := chi.NewRouter()
	r.NotFound(httpx.NotFoundHandler())
	r.MethodNotAllowed(httpx.MethodNotAllowedHandler())

	// Order matters: correlation and panic recovery wrap everything, security
	// headers and CORS run before any handler, and the body limit applies
	// before a payload is read.
	r.Use(httpx.RequestContext(cfg.HTTP.TrustedProxyCIDRs))
	r.Use(httpx.Recoverer)
	r.Use(httpx.SecurityHeaders(cfg.Security))
	r.Use(httpx.CORS(cfg.HTTP))
	r.Use(httpx.AccessLog(func(req *http.Request, status int, d time.Duration) {
		a.Metrics.ObserveHTTP(chi.RouteContext(req.Context()).RoutePattern(), req.Method, status, d)
	}))
	r.Use(middleware.StripSlashes)
	r.Use(routePatternRecorder)

	a.mountOperational(r, deps)

	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Use(httpx.BodyLimit(cfg.HTTP.MaxBodyBytes))
		v1.Use(httpx.Timeout(cfg.HTTP.WriteTimeout - time.Second))

		// /auth carries both anonymous and authenticated endpoints, so the two
		// middleware stacks are applied as sibling groups inside one mount
		// rather than by mounting the same path twice.
		v1.Route("/auth", func(ar chi.Router) {
			ar.Group(func(pub chi.Router) {
				// Credential endpoints are throttled by client IP, since there
				// is no session to key on yet.
				pub.Use(limiter.Middleware(cfg.Security.RateLimitEnabled, nil))
				authHandler.Routes(pub)
			})
			ar.Group(func(priv chi.Router) {
				priv.Use(a.Auth.Authenticate)
				priv.Use(limiter.Middleware(cfg.Security.RateLimitEnabled, auth.RateLimitKey))
				authHandler.AuthenticatedRoutes(priv)
			})
		})

		// Everything else requires a valid access token.
		v1.Group(func(priv chi.Router) {
			priv.Use(a.Auth.Authenticate)
			priv.Use(limiter.Middleware(cfg.Security.RateLimitEnabled, auth.RateLimitKey))

			priv.Route("/users", userHandler.Routes)
			priv.Route("/roles", roleHandler.Routes)
			priv.Route("/permissions", roleHandler.PermissionRoutes)
			priv.Route("/organization", a.organizationRoutes)
			priv.Route("/audit-events", a.auditRoutes)

			priv.Route("/network", netHandler.Routes)
			priv.Route("/geography", geoHandler.Routes)
			priv.Route("/courier-services", prodHandler.Routes)
			priv.Route("/serviceability", svcHandler.Routes)
			priv.Route("/routes", svcHandler.RouteRoutes)
			priv.Route("/pricing", priceHandler.Routes)
			priv.Route("/rate-cards", priceHandler.RateCardRoutes)
			priv.Route("/tax-rules", priceHandler.TaxRoutes)
			priv.Route("/customers", custHandler.Routes)
			priv.Route("/shipments", shipHandler.Routes)

			// Release 2 operational surface.
			opsHandler.Routes(priv)
			finHandler.Routes(priv)

			// Release 4 administration of the partner surface. Issuing a
			// credential is a user act governed by permissions; no API key can
			// reach these routes however many scopes it holds.
			// The command centre (M30). Read-only: it answers questions, and
			// every action it surfaces is taken through the module that owns it.
			priv.Route("/command-centre", commandHandler.Routes)
			priv.Route("/notifications", notifyHandler.Routes)
			priv.Route("/reports", reportHandler.Routes)
			priv.Route("/franchise-collections", collectionHandler.Routes)

			// The three audience surfaces. Each one is scoped by who is asking
			// rather than by what they ask for, so they are mounted inside the
			// same authenticated group as everything else and differ only in
			// the permission and the scope rule they apply.
			priv.Route("/portal/customer", portalHandler.CustomerRoutes)
			priv.Route("/portal/franchise", portalHandler.FranchiseRoutes)
			priv.Route("/console", portalHandler.ConsoleRoutes)

			priv.Route("/api-keys", partnerHandler.KeyRoutes)
			priv.Route("/webhooks", partnerHandler.WebhookRoutes)
		})

		// The partner surface. A sibling group, not a nested one: it runs a
		// completely different authentication stack — API key rather than
		// session, scopes rather than permissions — and mounting it inside the
		// user group would make it far too easy to grant one the other's rights.
		v1.Route("/partner", func(ext chi.Router) {
			ext.Use(partnerHandler.Authenticate)
			ext.Use(partnerHandler.RateLimit)
			ext.Use(partnerHandler.RecordUsage)
			partnerHandler.Routes(ext)
		})

		// Public tracking is the one business endpoint with no token, so it is
		// mounted outside the authenticated group and rate limited by client IP
		// far more tightly than the rest of the API (§M20).
		v1.Route("/track", func(pub chi.Router) {
			pub.Use(limiter.Middleware(cfg.Security.RateLimitEnabled, nil))
			opsHandler.PublicRoutes(pub)
		})
	})

	return r
}

// routePatternRecorder publishes the matched chi pattern so access logs and
// metrics use a bounded label instead of the raw path, which would explode
// cardinality on every shipment id.
func routePatternRecorder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			*r = *r.WithContext(httpx.WithRoutePattern(r.Context(), rctx.RoutePattern()))
		}
	})
}

// Ready reports dependency health for the readiness probe.
func (a *Application) Ready(ctx context.Context) (bool, map[string]any) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	checks := map[string]any{}
	healthy := true

	if err := a.DB.Ping(ctx); err != nil {
		healthy = false
		checks["database"] = map[string]any{"status": "down", "error": err.Error()}
	} else {
		stats := a.DB.Stats()
		checks["database"] = map[string]any{
			"status": "up", "acquiredConns": stats.AcquiredConns,
			"idleConns": stats.IdleConns, "maxConns": stats.MaxConns,
		}
	}

	// Redis is a cache, not a system of record, so an outage degrades rather
	// than fails readiness unless the deployment declared it required.
	if err := a.Cache.Ping(ctx); err != nil {
		status := "degraded"
		if a.Config.Redis.Required {
			healthy = false
			status = "down"
		}
		checks["cache"] = map[string]any{"status": status, "error": err.Error()}
	} else {
		checks["cache"] = map[string]any{"status": "up"}
	}
	return healthy, checks
}

// registerProviders swaps in real senders for the channels that are configured.
//
// Configuration failures are logged and skipped rather than fatal: a bad SMS
// key must not stop the API from booking shipments, and the channels endpoint
// reports which channels ended up configured so the gap is visible.
func registerProviders(
	senders *notification.Registry, cfg config.NotificationConfig, log *slog.Logger,
) {
	if cfg.TermiiAPIKey == "" {
		return
	}

	if cfg.TermiiEnableSMS {
		sms, err := notification.NewTermiiSender(notification.TermiiConfig{
			APIKey: cfg.TermiiAPIKey, BaseURL: cfg.TermiiBaseURL,
			SenderID: cfg.TermiiSenderID, Channel: cfg.TermiiSMSChannel,
			Kind: notification.ChannelSMS,
		})
		if err != nil {
			log.Error("Termii SMS is not configured; the channel stays on its logging sender",
				slog.String("error", err.Error()))
		} else {
			senders.Register(sms)
			log.Info("Termii registered for SMS", slog.String("senderId", cfg.TermiiSenderID))
		}
	}

	if cfg.TermiiWhatsAppFrom != "" {
		wa, err := notification.NewTermiiSender(notification.TermiiConfig{
			APIKey: cfg.TermiiAPIKey, BaseURL: cfg.TermiiBaseURL,
			SenderID: cfg.TermiiWhatsAppFrom, Kind: notification.ChannelWhatsApp,
		})
		if err != nil {
			log.Error("Termii WhatsApp is not configured; the channel stays on its logging sender",
				slog.String("error", err.Error()))
		} else {
			senders.Register(wa)
			log.Info("Termii registered for WhatsApp")
		}
	}
}
