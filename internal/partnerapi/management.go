package partnerapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Permissions for the management surface. These are user permissions, not
// partner scopes: issuing a credential is an act of security administration and
// no API key can perform it, however many scopes it holds.
const (
	PermKeyRead       = "apikey.read"
	PermKeyManage     = "apikey.manage"
	PermWebhookRead   = "webhook.read"
	PermWebhookManage = "webhook.manage"
	PermWebhookReplay = "webhook.replay"
)

// KeyRoutes mounts /api-keys inside the authenticated user surface.
func (h *Handler) KeyRoutes(r chi.Router) {
	r.Get("/", require(PermKeyRead, h.listKeys))
	r.Post("/", require(PermKeyManage, h.issueKey))
	r.Get("/{keyId}/usage", require(PermKeyRead, h.keyUsage))
	r.Post("/{keyId}/revoke", require(PermKeyManage, h.revokeKey))
	r.Post("/{keyId}/suspend", require(PermKeyManage, h.suspendKey))
}

// WebhookRoutes mounts /webhooks inside the authenticated user surface.
func (h *Handler) WebhookRoutes(r chi.Router) {
	r.Get("/endpoints", require(PermWebhookRead, h.listEndpoints))
	r.Post("/endpoints", require(PermWebhookManage, h.createEndpoint))
	r.Post("/endpoints/{endpointId}/status", require(PermWebhookManage, h.setEndpointStatus))
	r.Get("/endpoints/{endpointId}/subscriptions", require(PermWebhookRead, h.listSubscriptions))
	r.Post("/endpoints/{endpointId}/subscriptions", require(PermWebhookManage, h.subscribe))
	r.Delete("/endpoints/{endpointId}/subscriptions/{eventType}", require(PermWebhookManage, h.unsubscribe))

	r.Get("/deliveries", require(PermWebhookRead, h.listDeliveries))
	r.Get("/deliveries/{deliveryId}", require(PermWebhookRead, h.getDelivery))
	r.Get("/deliveries/{deliveryId}/attempts", require(PermWebhookRead, h.listAttempts))
	r.Post("/deliveries/{deliveryId}/replay", require(PermWebhookReplay, h.replayDelivery))

	r.Get("/events", require(PermWebhookRead, h.listEventTypes))
}

func require(permission string, h httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := tenant.Require(r)
		if err != nil {
			return err
		}
		if err := p.Require(permission); err != nil {
			return err
		}
		return h(w, r)
	})
}

// ---------------------------------------------------------------------------
// API keys
// ---------------------------------------------------------------------------

type issueKeyRequest struct {
	Name         string     `json:"name"`
	Scopes       []string   `json:"scopes"`
	AllowedCIDRs []string   `json:"allowedCidrs,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	RateLimit    *int32     `json:"rateLimitPerMinute,omitempty"`
}

// issueKey returns the plaintext secret exactly once.
func (h *Handler) issueKey(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req issueKeyRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	name := v.Text("name", req.Name, 2, 120, true)
	if req.RateLimit != nil {
		v.IntRange("rateLimitPerMinute", int(*req.RateLimit), 1, 100_000)
	}
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		v.Add("expiresAt", "must be in the future")
	}
	if err := v.Err(); err != nil {
		return err
	}

	issued, err := h.keys.Issue(r.Context(), p, partner.IssueRequest{
		Name: name, Scopes: req.Scopes, AllowedCIDRs: req.AllowedCIDRs,
		ExpiresAt: req.ExpiresAt, RateLimit: req.RateLimit,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/api-keys/"+issued.Key.PublicID, map[string]any{
		"key": keyView(issued.Key),
		// The one and only time this leaves the platform.
		"secret": issued.Secret,
		"token":  issued.Token,
		"warning": "Store this token now. It is hashed on the server and cannot " +
			"be shown again; a lost key must be replaced.",
	})
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status",
		[]string{partner.KeyActive, partner.KeySuspended, partner.KeyRevoked, partner.KeyExpired})
	if err != nil {
		return err
	}
	limit, offset := page(r)
	rows, err := h.keys.List(r.Context(), p, status, limit, offset)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	var total int64
	for _, k := range rows {
		total = k.TotalCount
		items = append(items, map[string]any{
			"id": k.PublicID, "name": k.Name, "keyId": k.KeyID,
			"secretHint": k.SecretHint, "scopes": k.Scopes,
			"allowedCidrs": k.AllowedCidrs, "status": k.Status,
			"expiresAt": k.ExpiresAt, "lastUsedAt": k.LastUsedAt,
			"requestCount": k.RequestCount, "rateLimitPerMinute": k.RateLimitPerMinute,
			"revokedAt": k.RevokedAt, "revokeReason": k.RevokeReason,
			"createdAt": k.CreatedAt,
		})
	}
	return httpx.OK(w, offsetPage(items, total, limit, offset))
}

type revokeRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) revokeKey(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "keyId", "apk", "API key")
	if err != nil {
		return err
	}
	var req revokeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	revoked, err := h.keys.Revoke(r.Context(), p, id, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, keyView(*revoked))
}

type suspendRequest struct {
	Suspend bool   `json:"suspend"`
	Reason  string `json:"reason,omitempty"`
}

// suspendKey pauses a key without burning it.
//
// Distinct from revoke on purpose: a suspected leak that turns out to be a
// misconfigured proxy should not force a partner through a key rotation.
func (h *Handler) suspendKey(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "keyId", "apk", "API key")
	if err != nil {
		return err
	}
	var req suspendRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	updated, err := h.keys.Suspend(r.Context(), p, id, req.Suspend, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, keyView(*updated))
}

func (h *Handler) keyUsage(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "keyId", "apk", "API key")
	if err != nil {
		return err
	}
	hours, err := httpx.QueryInt(r, "hours", 24, 1, 720)
	if err != nil {
		return err
	}
	summary, recent, err := h.keys.Usage(r.Context(), p, id, time.Duration(hours)*time.Hour)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(recent))
	for _, q := range recent {
		items = append(items, map[string]any{
			"method": q.Method, "route": q.Route, "statusCode": q.StatusCode,
			"durationMs": q.DurationMs, "errorCode": q.ErrorCode,
			"occurredAt": q.OccurredAt,
		})
	}
	return httpx.OK(w, map[string]any{
		"windowHours":   hours,
		"requestCount":  summary.RequestCount,
		"errorCount":    summary.ErrorCount,
		"avgDurationMs": summary.AvgDurationMs,
		"recent":        items,
	})
}

func keyView(k dbgen.ApiKey) map[string]any {
	return map[string]any{
		"id": k.PublicID, "name": k.Name, "keyId": k.KeyID,
		"secretHint": k.SecretHint, "scopes": k.Scopes,
		"allowedCidrs": k.AllowedCidrs, "status": k.Status,
		"expiresAt": k.ExpiresAt, "rateLimitPerMinute": k.RateLimitPerMinute,
		"revokedAt": k.RevokedAt, "revokeReason": k.RevokeReason,
		"createdAt": k.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Webhook endpoints
// ---------------------------------------------------------------------------

type createEndpointRequest struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

func (h *Handler) createEndpoint(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req createEndpointRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	return h.createEndpointFor(w, r, p, req)
}

func (h *Handler) createEndpointFor(
	w http.ResponseWriter, r *http.Request, p *tenant.Principal, req createEndpointRequest,
) error {
	v := validate.New()
	name := v.Text("name", req.Name, 2, 120, true)
	v.Required("url", req.URL)
	if len(req.Events) == 0 {
		v.Add("events", "Subscribe to at least one event, or the endpoint will never be called.")
	}
	for i, e := range req.Events {
		if !partner.IsKnownEvent(e) {
			v.Addf(fieldIdx("events", i), "Unknown event type %q.", e)
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	endpoint, secret, err := h.webhooks.CreateEndpoint(r.Context(), p, name, req.URL, req.Events)
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/webhooks/endpoints/"+endpoint.PublicID, map[string]any{
		"endpoint": endpointView(*endpoint),
		// Shown once, like an API key secret: it is the shared key the consumer
		// verifies our signature with.
		"signingSecret": secret,
		"signatureHeader": map[string]any{
			"header":       partner.HeaderSignature,
			"scheme":       "v1=HMAC_SHA256(secret, \"<timestamp>.<body>\")",
			"timestamp":    partner.HeaderTimestamp,
			"toleranceSec": int(partner.SignatureTolerance().Seconds()),
		},
		"warning": "Store this signing secret now. It cannot be shown again.",
	})
}

func (h *Handler) listEndpoints(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, offset := page(r)
	rows, err := h.webhooks.ListEndpoints(r.Context(), p, limit, offset)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	var total int64
	for _, e := range rows {
		total = e.TotalCount
		items = append(items, map[string]any{
			"id": e.PublicID, "name": e.Name, "url": e.Url, "status": e.Status,
			"consecutiveFailures": e.ConsecutiveFailures, "disabledReason": e.DisabledReason,
			"lastSuccessAt": e.LastSuccessAt, "lastFailureAt": e.LastFailureAt,
			"maxAttempts": e.MaxAttempts, "timeoutSeconds": e.TimeoutSeconds,
			"createdAt": e.CreatedAt,
		})
	}
	return httpx.OK(w, offsetPage(items, total, limit, offset))
}

type endpointStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) setEndpointStatus(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "endpointId", "whe", "Webhook endpoint")
	if err != nil {
		return err
	}
	var req endpointStatusRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	status := v.Enum("status", req.Status, []string{"ACTIVE", "PAUSED", "DISABLED"}, true)
	if err := v.Err(); err != nil {
		return err
	}
	updated, err := h.webhooks.SetEndpointStatus(r.Context(), p, id, status, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, endpointView(*updated))
}

type subscribeRequest struct {
	EventType string `json:"eventType"`
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "endpointId", "whe", "Webhook endpoint")
	if err != nil {
		return err
	}
	var req subscribeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if !partner.IsKnownEvent(req.EventType) {
		return apierr.Validation("Unknown event type.",
			map[string]any{"eventType": req.EventType, "allowed": partner.AllEvents})
	}
	sub, err := h.webhooks.Subscribe(r.Context(), p, id, req.EventType)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"id": sub.PublicID, "eventType": sub.EventType, "isActive": sub.IsActive,
	})
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "endpointId", "whe", "Webhook endpoint")
	if err != nil {
		return err
	}
	eventType := chi.URLParam(r, "eventType")
	if err := h.webhooks.Unsubscribe(r.Context(), p, id, eventType); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "endpointId", "whe", "Webhook endpoint")
	if err != nil {
		return err
	}
	subs, err := h.webhooks.ListSubscriptions(r.Context(), p, id)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(subs))
	for _, s := range subs {
		items = append(items, map[string]any{
			"id": s.PublicID, "eventType": s.EventType, "isActive": s.IsActive,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func (h *Handler) listEventTypes(w http.ResponseWriter, r *http.Request) error {
	return httpx.OK(w, map[string]any{"data": partner.AllEvents})
}

func endpointView(e dbgen.WebhookEndpoint) map[string]any {
	return map[string]any{
		"id": e.PublicID, "name": e.Name, "url": e.Url, "status": e.Status,
		"consecutiveFailures": e.ConsecutiveFailures, "disabledReason": e.DisabledReason,
		"maxAttempts": e.MaxAttempts, "timeoutSeconds": e.TimeoutSeconds,
		"createdAt": e.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Deliveries
// ---------------------------------------------------------------------------

func (h *Handler) listDeliveries(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	return h.renderDeliveries(w, r, p)
}

func (h *Handler) renderDeliveries(w http.ResponseWriter, r *http.Request, p *tenant.Principal) error {
	status, err := httpx.QueryEnum(r, "status", []string{
		partner.DeliveryPending, partner.DeliverySending, partner.DeliveryDelivered,
		partner.DeliveryFailed, partner.DeliveryDeadLetter, partner.DeliveryCancelled,
	})
	if err != nil {
		return err
	}
	limit, _ := page(r)
	cursor, err := httpx.QueryInt(r, "cursor", 0, 0, 1<<62)
	if err != nil {
		return err
	}
	var cursorID *int64
	if cursor > 0 {
		c := int64(cursor)
		cursorID = &c
	}

	var endpointID *int64
	if raw := httpx.Query(r, "endpointId"); raw != "" {
		ep, eErr := h.webhooks.GetEndpoint(r.Context(), p, raw)
		if eErr != nil {
			return eErr
		}
		endpointID = &ep.ID
	}

	rows, err := h.webhooks.ListDeliveries(r.Context(), p, partner.DeliveryFilter{
		EndpointID: endpointID, Status: status,
		EventType: httpx.Query(r, "eventType"), CursorID: cursorID, Limit: limit,
	})
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	var next int64
	for _, d := range rows {
		items = append(items, map[string]any{
			"id": d.PublicID, "endpoint": d.EndpointName, "eventType": d.EventType,
			"eventId": d.EventID, "status": d.Status, "attemptCount": d.AttemptCount,
			"lastStatusCode": d.LastStatusCode, "lastError": d.LastError,
			"nextAttemptAt": d.NextAttemptAt, "deliveredAt": d.DeliveredAt,
			"createdAt": d.CreatedAt,
		})
		next = d.ID
	}
	out := map[string]any{"data": items}
	if int32(len(rows)) == limit && next > 0 {
		out["nextCursor"] = next
	}
	return httpx.OK(w, out)
}

func (h *Handler) getDelivery(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "deliveryId", "whd", "Webhook delivery")
	if err != nil {
		return err
	}
	d, err := h.webhooks.GetDelivery(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"id": d.PublicID, "endpoint": d.EndpointName, "url": d.Url,
		"eventType": d.EventType, "eventId": d.EventID, "status": d.Status,
		"attemptCount": d.AttemptCount, "lastStatusCode": d.LastStatusCode,
		"lastError": d.LastError, "nextAttemptAt": d.NextAttemptAt,
		"deliveredAt": d.DeliveredAt, "createdAt": d.CreatedAt,
		// The exact bytes that were signed and sent. This is what makes a
		// signature dispute settleable.
		"payload": rawJSON(d.Payload),
	})
}

func (h *Handler) listAttempts(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "deliveryId", "whd", "Webhook delivery")
	if err != nil {
		return err
	}
	attempts, err := h.webhooks.ListAttempts(r.Context(), p, id)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(attempts))
	for _, a := range attempts {
		items = append(items, map[string]any{
			"attemptNo": a.AttemptNo, "statusCode": a.StatusCode,
			"responseBody": a.ResponseBody, "errorMessage": a.ErrorMessage,
			"durationMs": a.DurationMs, "attemptedAt": a.AttemptedAt,
		})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func (h *Handler) replayDelivery(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "deliveryId", "whd", "Webhook delivery")
	if err != nil {
		return err
	}
	replay, err := h.webhooks.Replay(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.Accepted(w, map[string]any{
		"id": replay.PublicID, "replayOf": id, "status": replay.Status,
		"eventType": replay.EventType, "eventId": replay.EventID,
	})
}

// ---------------------------------------------------------------------------
// The same three, reachable by an API key with webhook:manage
// ---------------------------------------------------------------------------

func (h *Handler) listEndpointsForKey(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	limit, offset := page(r)
	rows, err := h.webhooks.ListEndpoints(r.Context(), p, limit, offset)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		items = append(items, map[string]any{
			"id": e.PublicID, "name": e.Name, "url": e.Url, "status": e.Status,
			"lastSuccessAt": e.LastSuccessAt, "lastFailureAt": e.LastFailureAt,
			"createdAt": e.CreatedAt,
		})
	}
	total := int64(len(items))
	if len(rows) > 0 {
		total = rows[0].TotalCount
	}
	return httpx.OK(w, offsetPage(items, total, limit, offset))
}

func (h *Handler) createEndpointForKey(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	var req createEndpointRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	return h.createEndpointFor(w, r, p, req)
}

func (h *Handler) listDeliveriesForKey(w http.ResponseWriter, r *http.Request) error {
	p, err := principal(r)
	if err != nil {
		return err
	}
	return h.renderDeliveries(w, r, p)
}

// offsetPage wraps an offset listing with the metadata a client needs to page.
//
// Without a total, a caller has to request the maximum and guess whether there
// is another page — which is what the frontend was doing before this existed.
func offsetPage(items []map[string]any, total int64, limit, offset int32) map[string]any {
	return map[string]any{
		"data": items,
		"pagination": map[string]any{
			"limit": limit, "offset": offset, "totalItems": total,
			"hasMore": int64(offset)+int64(len(items)) < total,
		},
	}
}

// page reads a bounded page window. Every list endpoint paginates (§27).
func page(r *http.Request) (limit, offset int32) {
	l, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		l = 50
	}
	o, err := httpx.QueryInt(r, "offset", 0, 0, 100_000)
	if err != nil {
		o = 0
	}
	return int32(l), int32(o)
}
