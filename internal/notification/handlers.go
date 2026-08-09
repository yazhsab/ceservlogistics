package notification

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Handler serves the notification administration surface.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts /notifications.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", require(PermRead, h.list))
	r.Get("/health", require(PermRead, h.health))
	r.Get("/channels", require(PermRead, h.channels))
	r.Get("/{notificationId}", require(PermRead, h.get))
	r.Post("/{notificationId}/retry", require(PermRetry, h.retry))
	r.Post("/{notificationId}/cancel", require(PermRetry, h.cancel))

	r.Route("/templates", func(tr chi.Router) {
		tr.Get("/", require(PermRead, h.listTemplates))
		tr.Post("/", require(PermTemplate, h.createTemplate))
		tr.Get("/{templateId}", require(PermRead, h.getTemplate))
		tr.Patch("/{templateId}", require(PermTemplate, h.updateTemplate))
		tr.Post("/{templateId}/preview", require(PermTemplate, h.previewTemplate))
	})

	r.Route("/preferences", func(pr chi.Router) {
		pr.Get("/", require(PermRead, h.listPreferences))
		pr.Post("/", require(PermPreference, h.setPreference))
	})
}

func require(permission string, next httpx.Handler) http.HandlerFunc {
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := tenant.Require(r)
		if err != nil {
			return err
		}
		if err := p.Require(permission); err != nil {
			return err
		}
		return next(w, r)
	})
}

// ---------------------------------------------------------------------------
// Outbox
// ---------------------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	status, err := httpx.QueryEnum(r, "status", []string{
		StatusPending, StatusSending, StatusSent, StatusDelivered,
		StatusFailed, StatusDeadLetter, StatusSuppressed, StatusCancelled,
	})
	if err != nil {
		return err
	}
	channel, err := httpx.QueryEnum(r, "channel",
		[]string{ChannelSMS, ChannelEmail, ChannelWhatsApp, ChannelPush})
	if err != nil {
		return err
	}
	limit, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	cursor, err := httpx.QueryInt(r, "cursor", 0, 0, 1<<62)
	if err != nil {
		return err
	}
	var cursorID *int64
	if cursor > 0 {
		c := int64(cursor)
		cursorID = &c
	}

	rows, err := h.svc.List(r.Context(), p, ListFilter{
		Status: status, Channel: channel, EventType: httpx.Query(r, "eventType"),
		CursorID: cursorID, Limit: int32(limit),
	})
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	var next int64
	for _, n := range rows {
		items = append(items, notificationView(n))
		next = n.ID
	}
	out := map[string]any{"data": items}
	if len(rows) == limit && next > 0 {
		out["nextCursor"] = next
	}
	return httpx.OK(w, out)
}

// notificationView never includes the rendered body's recipient address in full
// for a channel that carries one — the address is already on the row and an
// operator screen does not need it duplicated — but it does include the
// suppression reason, which is the field the screen exists for.
func notificationView(n dbgen.ListNotificationsRow) map[string]any {
	return map[string]any{
		"id": n.PublicID, "eventType": n.EventType, "channel": n.Channel,
		"status": n.Status, "suppressedReason": n.SuppressedReason,
		"recipientType": n.RecipientType, "recipientName": n.RecipientName,
		"subject": n.Subject, "attemptCount": n.AttemptCount,
		"lastError": n.LastError, "nextAttemptAt": n.NextAttemptAt,
		"sentAt": n.SentAt, "deliveredAt": n.DeliveredAt, "failedAt": n.FailedAt,
		"awb": n.Awb, "reference": n.Reference, "createdAt": n.CreatedAt,
	}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "notificationId", "ntf", "Notification")
	if err != nil {
		return err
	}
	n, attempts, err := h.svc.Get(r.Context(), p, id)
	if err != nil {
		return err
	}
	tries := make([]map[string]any, 0, len(attempts))
	for _, a := range attempts {
		tries = append(tries, map[string]any{
			"attemptNo": a.AttemptNo, "provider": a.Provider, "outcome": a.Outcome,
			"providerMessageId": a.ProviderMessageID, "providerStatus": a.ProviderStatus,
			"errorMessage": a.ErrorMessage, "retryable": a.Retryable,
			"durationMs": a.DurationMs, "attemptedAt": a.AttemptedAt,
		})
	}
	return httpx.OK(w, map[string]any{
		"id": n.PublicID, "eventType": n.EventType, "channel": n.Channel,
		"status": n.Status, "suppressedReason": n.SuppressedReason,
		"recipientType": n.RecipientType, "recipientAddress": n.RecipientAddress,
		"recipientName": n.RecipientName, "locale": n.Locale,
		"templateCode": n.TemplateCode, "subject": n.Subject,
		// The rendered text, so an operator can see exactly what was sent
		// rather than reconstructing it from the template and the variables.
		"body":       n.Body,
		"awb":        n.Awb,
		"attempts":   tries,
		"createdAt":  n.CreatedAt,
		"sentAt":     n.SentAt,
		"failedAt":   n.FailedAt,
		"lastError":  n.LastError,
		"maxRetries": n.MaxAttempts,
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	hours, err := httpx.QueryInt(r, "hours", 24, 1, 720)
	if err != nil {
		return err
	}
	counts, err := h.svc.Health(r.Context(), p, time.Duration(hours)*time.Hour)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"windowHours": hours, "byStatus": counts,
		"configuredChannels": h.svc.Channels(),
	})
}

// channels reports which channels can actually send.
//
// A channel with no adapter suppresses every message on it. That is the single
// most common cause of "notifications are not working", and it is invisible
// without this.
func (h *Handler) channels(w http.ResponseWriter, r *http.Request) error {
	all := []string{ChannelSMS, ChannelEmail, ChannelWhatsApp, ChannelPush}
	configured := map[string]bool{}
	for _, c := range h.svc.Channels() {
		configured[c] = true
	}
	items := make([]map[string]any, 0, len(all))
	for _, c := range all {
		items = append(items, map[string]any{"channel": c, "configured": configured[c]})
	}
	return httpx.OK(w, map[string]any{"data": items})
}

type reasonRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "notificationId", "ntf", "Notification")
	if err != nil {
		return err
	}
	n, err := h.svc.Retry(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.Accepted(w, map[string]any{
		"id": n.PublicID, "status": n.Status, "attemptCount": n.AttemptCount,
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "notificationId", "ntf", "Notification")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	n, err := h.svc.Cancel(r.Context(), p, id, req.Reason)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"id": n.PublicID, "status": n.Status})
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

type templateRequest struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	EventType string `json:"eventType"`
	Channel   string `json:"channel"`
	Locale    string `json:"locale,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Body      string `json:"body"`
	// The provider handle: an approved WhatsApp template name, or a registered
	// SMS sender id. Required by some providers, ignored by others.
	ProviderRef string `json:"providerRef,omitempty"`
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req templateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	v.Code("code", req.Code)
	v.Text("name", req.Name, 2, 120, true)
	v.Text("body", req.Body, 1, 4000, true)
	if err := v.Err(); err != nil {
		return err
	}

	t, err := h.svc.CreateTemplate(r.Context(), p, TemplateInput{
		Code: req.Code, Name: req.Name, EventType: req.EventType,
		Channel: req.Channel, Locale: req.Locale,
		Subject: req.Subject, Body: req.Body, ProviderRef: req.ProviderRef,
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, "/api/v1/notifications/templates/"+t.PublicID, templateView(*t))
}

type templateUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Subject     *string `json:"subject,omitempty"`
	Body        *string `json:"body,omitempty"`
	IsActive    *bool   `json:"isActive,omitempty"`
	ProviderRef *string `json:"providerRef,omitempty"`
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "templateId", "ntt", "Notification template")
	if err != nil {
		return err
	}
	var req templateUpdateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.Name == nil && req.Subject == nil && req.Body == nil &&
		req.IsActive == nil && req.ProviderRef == nil {
		return apierr.Validation("Nothing to update.", nil)
	}
	t, err := h.svc.UpdateTemplate(r.Context(), p, id, TemplateUpdate{
		Name: req.Name, Subject: req.Subject, Body: req.Body,
		IsActive: req.IsActive, ProviderRef: req.ProviderRef,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, templateView(*t))
}

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := httpx.QueryInt(r, "limit", 100, 1, 200)
	if err != nil {
		return err
	}
	offset, err := httpx.QueryInt(r, "offset", 0, 0, 100000)
	if err != nil {
		return err
	}
	rows, err := h.svc.ListTemplates(r.Context(), p,
		httpx.Query(r, "eventType"), httpx.Query(r, "channel"),
		int32(limit), int32(offset))
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		items = append(items, templateView(t))
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "templateId", "ntt", "Notification template")
	if err != nil {
		return err
	}
	t, err := h.svc.GetTemplate(r.Context(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(w, templateView(*t))
}

type previewRequest struct {
	Variables map[string]string `json:"variables"`
}

// previewTemplate renders a template without sending it.
func (h *Handler) previewTemplate(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathPublicID(r, "templateId", "ntt", "Notification template")
	if err != nil {
		return err
	}
	var req previewRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	subject, body, missing, err := h.svc.Preview(r.Context(), p, id, req.Variables)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{
		"subject": subject, "body": body,
		// Named explicitly rather than left as an unreplaced {{placeholder}} in
		// the body, so a template with a typo is obvious here instead of in a
		// customer's inbox.
		"missingVariables": missing,
	})
}

func templateView(t dbgen.NotificationTemplate) map[string]any {
	return map[string]any{
		"id": t.PublicID, "code": t.Code, "name": t.Name,
		"eventType": t.EventType, "channel": t.Channel, "locale": t.Locale,
		"subject": t.Subject, "body": t.Body, "variables": rawJSON(t.Variables),
		"providerRef": t.ProviderRef,
		"isActive":    t.IsActive, "createdAt": t.CreatedAt, "updatedAt": t.UpdatedAt,
	}
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

type preferenceRequest struct {
	PartyType string `json:"partyType"`
	PartyID   string `json:"partyId,omitempty"`
	PartyKey  string `json:"partyKey,omitempty"`
	Channel   string `json:"channel"`
	EventType string `json:"eventType,omitempty"`
	Enabled   bool   `json:"enabled"`
	Locale    string `json:"locale,omitempty"`
}

func (h *Handler) setPreference(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	var req preferenceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := validate.New()
	partyType := v.Enum("partyType", req.PartyType,
		[]string{"CUSTOMER", "USER", "FRANCHISE", "RECIPIENT"}, true)
	if err := v.Err(); err != nil {
		return err
	}

	// A party named by public id is resolved server-side; the client never
	// supplies an internal key.
	var partyID *int64
	if req.PartyID != "" {
		id, rErr := h.svc.resolveParty(r.Context(), p, partyType, req.PartyID)
		if rErr != nil {
			return rErr
		}
		partyID = &id
	}

	pref, err := h.svc.SetPreference(r.Context(), p, PreferenceInput{
		PartyType: partyType, PartyID: partyID, PartyKey: req.PartyKey,
		Channel: req.Channel, EventType: req.EventType,
		Enabled: req.Enabled, Locale: req.Locale,
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, preferenceView(*pref))
}

func (h *Handler) listPreferences(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	partyType, err := httpx.QueryEnum(r, "partyType",
		[]string{"CUSTOMER", "USER", "FRANCHISE", "RECIPIENT"})
	if err != nil {
		return err
	}
	partyPublicID := httpx.Query(r, "partyId")
	if partyType == "" || partyPublicID == "" {
		return apierr.Validation("Both partyType and partyId are required.", nil)
	}
	partyID, err := h.svc.resolveParty(r.Context(), p, partyType, partyPublicID)
	if err != nil {
		return err
	}
	rows, err := h.svc.ListPreferences(r.Context(), p, partyType, partyID)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, pref := range rows {
		items = append(items, preferenceView(pref))
	}
	return httpx.OK(w, map[string]any{"data": items})
}

func preferenceView(p dbgen.NotificationPreference) map[string]any {
	return map[string]any{
		"id": p.PublicID, "partyType": p.PartyType, "channel": p.Channel,
		"eventType": p.EventType, "enabled": p.Enabled, "locale": p.Locale,
	}
}
