package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Administration of the notification module: what gets sent, to whom, and what
// happened to the messages that did not arrive.
//
// The delivery path deliberately never fails a business operation, which means
// a suppressed or dead-lettered message is invisible unless somebody can go and
// look. That is what this file is for. Without it the module is a black box
// that swallows problems quietly, which is the worst property a notification
// system can have.

// Permissions.
const (
	PermRead       = "notification.read"
	PermTemplate   = "notification.template"
	PermPreference = "notification.preference"
	PermRetry      = "notification.retry"
)

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

// TemplateInput describes a template being created.
type TemplateInput struct {
	Code      string
	Name      string
	EventType string
	Channel   string
	Locale    string
	Subject   string
	Body      string
	// ProviderRef is the handle the provider needs to accept this message: an
	// approved WhatsApp template name, or a registered SMS sender id. Optional,
	// because a provider that needs none should not be made to invent one.
	ProviderRef string
}

// CreateTemplate registers a message template.
//
// The placeholders in the body are extracted and stored rather than taken from
// the caller: a declared variable list that disagrees with the body is a lie
// the first recipient discovers.
func (s *Service) CreateTemplate(
	ctx context.Context, p *tenant.Principal, in TemplateInput,
) (*dbgen.NotificationTemplate, error) {
	if err := validateTemplate(in); err != nil {
		return nil, err
	}
	if in.Locale == "" {
		in.Locale = "en"
	}

	vars, err := json.Marshal(Placeholders(in.Subject + " " + in.Body))
	if err != nil {
		vars = []byte("[]")
	}

	created, err := s.q.CreateNotificationTemplate(ctx, dbgen.CreateNotificationTemplateParams{
		PublicID: publicid.New("ntt"), OrganizationID: p.OrganizationID,
		Code: in.Code, Name: in.Name, EventType: in.EventType, Channel: in.Channel,
		Locale: in.Locale, Subject: ops.Optional(in.Subject), Body: in.Body,
		ProviderRef: ops.Optional(in.ProviderRef),
		Variables:   vars, Metadata: []byte("{}"), IsActive: true,
		CreatedBy: p.ActorUserID(),
	})
	if err != nil {
		// Two different uniqueness rules, and both are user mistakes rather
		// than internal failures: the code must be unique, and only one active
		// template may serve a given (event, channel, locale) — otherwise
		// ResolveTemplate would have to pick one arbitrarily and the tenant
		// would never know which text a customer received.
		switch {
		case ops.IsUnique(err, "notification_templates_lookup_idx"):
			return nil, apierr.Duplicate(
				"A template already serves this event, channel and locale. "+
					"Deactivate it first, or edit it instead.").
				WithDetail("eventType", in.EventType).
				WithDetail("channel", in.Channel).
				WithDetail("locale", in.Locale)
		case ops.IsUnique(err, "notification_templates_code_key"),
			ops.IsUnique(err, "notification_templates_org_code_idx"):
			return nil, apierr.Duplicate("A template with this code already exists.")
		}
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "notification.template.created", ResourceType: "notification_template",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		Metadata: map[string]any{
			"code": in.Code, "eventType": in.EventType, "channel": in.Channel,
		},
	}))
	return &created, nil
}

// TemplateUpdate is a partial change. A nil field is left alone.
type TemplateUpdate struct {
	Name        *string
	Subject     *string
	Body        *string
	IsActive    *bool
	ProviderRef *string
}

// UpdateTemplate edits a template and re-derives its variable list.
func (s *Service) UpdateTemplate(
	ctx context.Context, p *tenant.Principal, publicID string, in TemplateUpdate,
) (*dbgen.NotificationTemplate, error) {
	existing, err := s.q.GetTemplateByPublicID(ctx, dbgen.GetTemplateByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Notification template")
	}

	// Re-derive the variables from whatever the body will be after the edit,
	// so the declared list cannot drift from the text.
	body := existing.Body
	if in.Body != nil {
		body = *in.Body
	}
	subject := ""
	if existing.Subject != nil {
		subject = *existing.Subject
	}
	if in.Subject != nil {
		subject = *in.Subject
	}
	vars, mErr := json.Marshal(Placeholders(subject + " " + body))
	if mErr != nil {
		vars = existing.Variables
	}

	updated, err := s.q.UpdateNotificationTemplate(ctx, dbgen.UpdateNotificationTemplateParams{
		OrganizationID: p.OrganizationID, ID: existing.ID,
		Name: in.Name, Subject: in.Subject, Body: in.Body,
		Variables: vars, IsActive: in.IsActive, ProviderRef: in.ProviderRef,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "notification.template.updated", ResourceType: "notification_template",
		ResourceID: &existing.ID, ResourcePublicID: existing.PublicID,
		Before: map[string]any{"body": existing.Body, "isActive": existing.IsActive},
		After:  map[string]any{"body": updated.Body, "isActive": updated.IsActive},
	}))
	return &updated, nil
}

// ListTemplates returns the tenant's templates.
func (s *Service) ListTemplates(
	ctx context.Context, p *tenant.Principal, eventType, channel string, limit, offset int32,
) ([]dbgen.NotificationTemplate, error) {
	rows, err := s.q.ListNotificationTemplates(ctx, dbgen.ListNotificationTemplatesParams{
		OrganizationID: p.OrganizationID,
		EventType:      ops.Optional(eventType), Channel: ops.Optional(channel),
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// GetTemplate returns one template.
func (s *Service) GetTemplate(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.NotificationTemplate, error) {
	t, err := s.q.GetTemplateByPublicID(ctx, dbgen.GetTemplateByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Notification template")
	}
	return &t, nil
}

// Preview renders a template against sample variables without sending it.
//
// The one way to find out that a template references a variable nothing
// supplies is to render it. Doing that here, before the template is used, beats
// doing it accidentally in a customer's SMS.
func (s *Service) Preview(
	ctx context.Context, p *tenant.Principal, publicID string, vars map[string]string,
) (subject, body string, missing []string, err error) {
	t, err := s.GetTemplate(ctx, p, publicID)
	if err != nil {
		return "", "", nil, err
	}
	rawSubject := ""
	if t.Subject != nil {
		rawSubject = *t.Subject
	}

	// Missing variables come from the *template*, not from the rendered output.
	// Render substitutes what it can and clears what it cannot, so asking the
	// rendered text which placeholders went unfilled always answers "none" —
	// which is precisely the question this endpoint exists to answer.
	missing = MissingVariables(rawSubject+" "+t.Body, vars)
	if missing == nil {
		missing = []string{}
	}

	subject = Render(rawSubject, vars)
	body = Render(t.Body, vars)
	return subject, body, missing, nil
}

func validateTemplate(in TemplateInput) error {
	fields := map[string]any{}
	if in.Code == "" {
		fields["code"] = "is required"
	}
	if in.Name == "" {
		fields["name"] = "is required"
	}
	if in.Body == "" {
		fields["body"] = "is required"
	}
	if !validChannel(in.Channel) {
		fields["channel"] = "must be one of SMS, EMAIL, WHATSAPP, PUSH"
	}
	if in.EventType == "" {
		fields["eventType"] = "is required"
	}
	// An email with no subject line arrives looking like spam.
	if in.Channel == ChannelEmail && in.Subject == "" {
		fields["subject"] = "is required for the EMAIL channel"
	}
	if len(fields) > 0 {
		return apierr.Validation("The template is not valid.", fields)
	}
	return nil
}

func validChannel(c string) bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelWhatsApp, ChannelPush:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

// PreferenceInput is one opt-in or opt-out.
type PreferenceInput struct {
	PartyType string
	PartyID   *int64
	PartyKey  string
	Channel   string
	// EventType empty means the preference covers every event on the channel.
	EventType string
	Enabled   bool
	Locale    string
}

// SetPreference records a recipient's choice.
//
// A blanket row and an event-specific row can both exist; the event-specific
// one wins. That is what lets somebody keep delivery notifications while
// turning off marketing without listing every event they still want.
func (s *Service) SetPreference(
	ctx context.Context, p *tenant.Principal, in PreferenceInput,
) (*dbgen.NotificationPreference, error) {
	if !validChannel(in.Channel) {
		return nil, apierr.Validation("Unknown channel.",
			map[string]any{"channel": in.Channel})
	}
	if in.PartyID == nil && in.PartyKey == "" {
		return nil, apierr.Validation(
			"A preference needs a party: either an internal id or an address key.", nil)
	}
	if in.Locale == "" {
		in.Locale = "en"
	}

	pref, err := s.q.UpsertNotificationPreference(ctx, dbgen.UpsertNotificationPreferenceParams{
		PublicID: publicid.New("ntp"), OrganizationID: p.OrganizationID,
		PartyType: in.PartyType, PartyID: in.PartyID, PartyKey: ops.Optional(in.PartyKey),
		Channel: in.Channel, EventType: ops.Optional(in.EventType),
		Enabled: in.Enabled, Locale: in.Locale,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	// Opting out is a consent decision, so it is audited: "we kept texting them
	// after they asked us to stop" is a question that gets asked.
	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "notification.preference.set", ResourceType: "notification_preference",
		ResourceID: &pref.ID, ResourcePublicID: pref.PublicID,
		Metadata: map[string]any{
			"partyType": in.PartyType, "channel": in.Channel,
			"eventType": in.EventType, "enabled": in.Enabled,
		},
	}))
	return &pref, nil
}

// ListPreferences returns a party's choices.
func (s *Service) ListPreferences(
	ctx context.Context, p *tenant.Principal, partyType string, partyID int64,
) ([]dbgen.NotificationPreference, error) {
	rows, err := s.q.ListPreferences(ctx, dbgen.ListPreferencesParams{
		OrganizationID: p.OrganizationID, PartyType: partyType, PartyID: &partyID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// The outbox: what was sent, what was not, and why
// ---------------------------------------------------------------------------

// ListFilter narrows the notification listing.
type ListFilter struct {
	Status     string
	Channel    string
	EventType  string
	ShipmentID *int64
	CursorID   *int64
	Limit      int32
}

// List returns notifications, newest first, by keyset cursor.
func (s *Service) List(
	ctx context.Context, p *tenant.Principal, f ListFilter,
) ([]dbgen.ListNotificationsRow, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := s.q.ListNotifications(ctx, dbgen.ListNotificationsParams{
		OrganizationID: p.OrganizationID,
		Status:         ops.Optional(f.Status), Channel: ops.Optional(f.Channel),
		EventType: ops.Optional(f.EventType), ShipmentID: f.ShipmentID,
		CursorID: f.CursorID, Limit: f.Limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// Get returns one notification with its attempt history.
func (s *Service) Get(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetNotificationByPublicIDRow, []dbgen.NotificationAttempt, error) {
	n, err := s.q.GetNotificationByPublicID(ctx, dbgen.GetNotificationByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, nil, ops.NotFoundOr(err, "Notification")
	}
	attempts, err := s.q.ListNotificationAttempts(ctx, n.ID)
	if err != nil {
		return nil, nil, apierr.Internal(err)
	}
	return &n, attempts, nil
}

// Health summarises recent delivery outcomes.
func (s *Service) Health(
	ctx context.Context, p *tenant.Principal, window time.Duration,
) (map[string]int64, error) {
	rows, err := s.q.CountNotificationsByStatus(ctx, dbgen.CountNotificationsByStatusParams{
		OrganizationID: p.OrganizationID, Since: time.Now().Add(-window),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Status] = r.Count
	}
	return out, nil
}

// Retry puts a dead-lettered message back on the queue.
//
// It resets the attempt budget, because an operator retrying by hand has
// presumably fixed whatever was wrong — a provider outage, a missing template,
// a corrected address — and the old budget is evidence about the old problem.
func (s *Service) Retry(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.Notification, error) {
	n, err := s.q.GetNotificationByPublicID(ctx, dbgen.GetNotificationByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Notification")
	}
	retried, err := s.q.RetryNotification(ctx, dbgen.RetryNotificationParams{
		OrganizationID: p.OrganizationID, ID: n.ID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(apierr.CodeConflict,
				"Only a failed or dead-lettered notification can be retried.").
				WithDetail("status", n.Status)
		}
		return nil, apierr.Internal(err)
	}

	if _, err := s.jobs.Enqueue(ctx, JobTypeSend, map[string]any{
		"organizationId": p.OrganizationID, "notificationId": retried.ID,
	}, jobs.EnqueueOptions{
		OrganizationID: &p.OrganizationID, Queue: "notifications",
		// A fresh dedupe key: the original send's key belongs to the attempt
		// that failed, and reusing it would deduplicate this retry away.
		DedupeKey:   "ntf-retry:" + retried.PublicID + ":" + publicid.New("x"),
		MaxAttempts: 5,
	}); err != nil {
		s.log.Warn("retry queued without a job", "notificationId", retried.PublicID, "error", err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "notification.retried", ResourceType: "notification",
		ResourceID: &n.ID, ResourcePublicID: n.PublicID,
		Before: map[string]any{"status": n.Status, "lastError": n.LastError},
	}))
	return &retried, nil
}

// Cancel stops a message that has not been sent.
func (s *Service) Cancel(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.Notification, error) {
	n, err := s.q.GetNotificationByPublicID(ctx, dbgen.GetNotificationByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Notification")
	}
	cancelled, err := s.q.CancelNotification(ctx, dbgen.CancelNotificationParams{
		OrganizationID: p.OrganizationID, ID: n.ID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(apierr.CodeConflict,
				"This notification has already been sent and cannot be cancelled.").
				WithDetail("status", n.Status)
		}
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "notification.cancelled", ResourceType: "notification",
		ResourceID: &n.ID, ResourcePublicID: n.PublicID, Reason: reason,
	}))
	return &cancelled, nil
}

// Channels reports which channels have a sender registered.
//
// Worth exposing: a channel with no adapter suppresses every message on it, and
// "notifications are not arriving" is otherwise a long investigation.
func (s *Service) Channels() []string { return s.senders.Channels() }

// resolveParty turns a public identifier into the internal key a preference
// row uses.
//
// The client never supplies an internal id (§11), and the lookup is
// tenant-scoped, so a preference cannot be written against another tenant's
// customer by guessing a number.
func (s *Service) resolveParty(
	ctx context.Context, p *tenant.Principal, partyType, publicID string,
) (int64, error) {
	switch partyType {
	case "CUSTOMER":
		c, err := s.q.GetCustomerByPublicID(ctx, dbgen.GetCustomerByPublicIDParams{
			PublicID: publicID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			return 0, ops.NotFoundOr(err, "Customer")
		}
		if !p.IsCustomerInScope(c.ID) {
			return 0, apierr.NotFound("Customer")
		}
		return c.ID, nil
	case "USER":
		u, err := s.q.GetUserByPublicID(ctx, dbgen.GetUserByPublicIDParams{
			PublicID: publicID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			return 0, ops.NotFoundOr(err, "User")
		}
		return u.ID, nil
	case "FRANCHISE":
		f, err := s.q.GetFranchiseByPublicID(ctx, dbgen.GetFranchiseByPublicIDParams{
			PublicID: publicID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			return 0, ops.NotFoundOr(err, "Franchise")
		}
		return f.ID, nil
	default:
		// A RECIPIENT is a bare phone number or address with no row of its own,
		// so it is keyed by partyKey rather than by id.
		return 0, apierr.Validation(
			"This party type is identified by partyKey, not partyId.",
			map[string]any{"partyType": partyType})
	}
}

// rawJSON passes stored JSON through without re-encoding it.
func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}
