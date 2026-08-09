package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// JobTypeSend is the queue job that delivers one notification.
const JobTypeSend = "notification.send"

// Error codes.
const (
	CodeNoTemplate    = "NOTIFICATION_NO_TEMPLATE"
	CodeNoAddress     = "NOTIFICATION_NO_ADDRESS"
	CodeNotRetryable  = "NOTIFICATION_NOT_RETRYABLE"
	CodeAlreadySent   = "NOTIFICATION_ALREADY_SENT"
	CodeNoSender      = "NOTIFICATION_NO_SENDER"
	CodeInvalidStatus = "NOTIFICATION_INVALID_STATUS"
)

// Suppression reasons. A suppressed notification is a normal outcome, recorded
// so an operator can answer "why didn't the customer get a message?".
const (
	SuppressOptedOut   = "RECIPIENT_OPTED_OUT"
	SuppressNoTemplate = "NO_TEMPLATE_CONFIGURED"
	SuppressNoAddress  = "NO_VALID_ADDRESS"
	SuppressNoSender   = "CHANNEL_NOT_CONFIGURED"
	SuppressQuietHours = "QUIET_HOURS"
	SuppressDuplicate  = "DUPLICATE_SUPPRESSED"
)

// Service raises and delivers notifications.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	jobs    *jobs.Enqueuer
	senders *Registry
	audit   *audit.Recorder
	log     *slog.Logger

	// TrackingBaseURL is interpolated into templates as {{trackingUrl}}.
	TrackingBaseURL string
}

func NewService(
	db *database.DB, q *dbgen.Queries, enq *jobs.Enqueuer,
	senders *Registry, rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, jobs: enq, senders: senders, audit: rec, log: log}
}

// Request raises a notification.
type Request struct {
	EventType string
	Channel   string

	RecipientType    string
	RecipientID      *int64
	RecipientAddress string
	RecipientName    string
	Locale           string

	Variables map[string]string

	ShipmentID   *int64
	InvoiceID    *int64
	SettlementID *int64
	Reference    string

	// DedupeKey collapses repeated raises of the same event onto one message.
	// Strongly recommended: a status transition retried by a mobile client
	// would otherwise text the customer twice.
	DedupeKey string
}

// Raise creates a notification and queues it for delivery.
//
// Runs inside the caller's transaction, so raising is atomic with the business
// event. It never contacts a provider, never blocks, and never returns an error
// that should abort the caller: an unsendable notification is *suppressed* and
// recorded, because failing a delivery because a template is missing would be
// absurd.
func (s *Service) Raise(
	ctx context.Context, tx pgx.Tx, p *tenant.Principal, in Request,
) (*dbgen.Notification, error) {
	q := s.q.WithTx(tx)

	if in.Locale == "" {
		in.Locale = "en"
	}
	if in.Channel == "" || in.EventType == "" {
		return nil, apierr.BadRequest("A notification needs an event type and a channel.")
	}

	// Dedupe probe. The partial unique index is the guarantee; this makes the
	// common repeat clean rather than surfacing a constraint violation.
	if in.DedupeKey != "" {
		if existing, err := q.FindNotificationByDedupe(ctx, dbgen.FindNotificationByDedupeParams{
			OrganizationID: p.OrganizationID, DedupeKey: ops.Optional(in.DedupeKey),
		}); err == nil {
			return &existing, nil
		} else if !ops.IsNoRows(err) {
			return nil, apierr.Internal(err)
		}
	}

	// Decide whether this can be sent at all. Each branch produces a
	// SUPPRESSED row rather than an error, so the reason is visible later.
	suppress := ""

	if !ValidAddress(in.Channel, in.RecipientAddress) {
		suppress = SuppressNoAddress
	}
	if suppress == "" {
		if _, ok := s.senders.For(in.Channel); !ok {
			suppress = SuppressNoSender
		}
	}

	var template *dbgen.NotificationTemplate
	if suppress == "" {
		t, err := q.ResolveTemplate(ctx, dbgen.ResolveTemplateParams{
			OrganizationID: p.OrganizationID, EventType: in.EventType,
			Channel: in.Channel, Locale: in.Locale,
		})
		if err != nil {
			if !ops.IsNoRows(err) {
				return nil, apierr.Internal(err)
			}
			suppress = SuppressNoTemplate
		} else {
			template = &t
		}
	}

	if suppress == "" && !s.wants(ctx, q, p, in) {
		suppress = SuppressOptedOut
	}

	subject, bodyText := "", ""
	var templateID *int64
	var templateCode, providerRef *string
	if template != nil {
		templateID, templateCode = &template.ID, &template.Code
		// Snapshotted, not looked up at send time: a template re-approved next
		// month must not change what a message sent today was addressed with.
		providerRef = template.ProviderRef
		if template.Subject != nil {
			subject = Render(*template.Subject, in.Variables)
		}
		bodyText = Render(template.Body, in.Variables)
	} else {
		// A suppressed notification still records what would have been sent, so
		// the row is meaningful when an operator investigates.
		bodyText = fmt.Sprintf("[suppressed: %s] %s", suppress, in.EventType)
	}

	vars, err := json.Marshal(in.Variables)
	if err != nil {
		vars = []byte("{}")
	}

	status := StatusPending
	// A suppressed message is never sent, so it carries no schedule. A pending
	// one leaves next_attempt_at nil, which the query fills with the database's
	// own now() — see CreateNotification for why the app clock is not used.
	if suppress != "" {
		status = StatusSuppressed
	}

	notification, err := q.CreateNotification(ctx, dbgen.CreateNotificationParams{
		PublicID:         publicid.New("ntf"),
		OrganizationID:   p.OrganizationID,
		EventType:        in.EventType,
		Channel:          in.Channel,
		TemplateID:       templateID,
		TemplateCode:     templateCode,
		Locale:           in.Locale,
		RecipientType:    in.RecipientType,
		RecipientID:      in.RecipientID,
		RecipientAddress: in.RecipientAddress,
		RecipientName:    ops.Optional(in.RecipientName),
		Subject:          ops.Optional(subject),
		Body:             bodyText,
		Variables:        vars,
		ShipmentID:       in.ShipmentID,
		InvoiceID:        in.InvoiceID,
		SettlementID:     in.SettlementID,
		Reference:        ops.Optional(in.Reference),
		Status:           status,
		SuppressedReason: ops.Optional(suppress),
		MaxAttempts:      5,
		DedupeKey:        ops.Optional(in.DedupeKey),
		RequestID:        ops.Optional(httpx.RequestID(ctx)),
		ProviderRef:      providerRef,
	})
	if err != nil {
		// Lost a race with an identical concurrent raise.
		if ops.IsUnique(err, "notifications_dedupe_idx") && in.DedupeKey != "" {
			if existing, fErr := q.FindNotificationByDedupe(ctx, dbgen.FindNotificationByDedupeParams{
				OrganizationID: p.OrganizationID, DedupeKey: ops.Optional(in.DedupeKey),
			}); fErr == nil {
				return &existing, nil
			}
		}
		return nil, apierr.Internal(err)
	}

	if status == StatusSuppressed {
		return &notification, nil
	}

	// Enqueue in the same transaction. If the caller rolls back, the job never
	// existed — no orphan message for a booking that did not happen.
	if _, err := s.jobs.EnqueueTx(ctx, tx, JobTypeSend,
		map[string]any{
			"organizationId": p.OrganizationID,
			"notificationId": notification.ID,
		},
		jobs.EnqueueOptions{
			OrganizationID: &p.OrganizationID,
			Queue:          "notifications",
			DedupeKey:      "ntf:" + notification.PublicID,
			MaxAttempts:    6,
		}); err != nil {
		// The row exists and the sweeper will pick it up even without a job, so
		// a queueing failure degrades latency rather than losing the message.
		s.log.Warn("notification queued without a job",
			"notificationId", notification.PublicID, "error", err)
	}

	return &notification, nil
}

// wants resolves whether the recipient has opted out of this notification.
//
// The default is to send: a consignee who has never expressed a preference
// still wants to know their parcel is arriving. Only an explicit disable
// suppresses.
func (s *Service) wants(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, in Request,
) bool {
	partyKey := ""
	if in.RecipientID == nil {
		partyKey = strings.ToLower(strings.TrimSpace(in.RecipientAddress))
	}
	pref, err := q.FindPreference(ctx, dbgen.FindPreferenceParams{
		OrganizationID: p.OrganizationID,
		PartyType:      in.RecipientType,
		Channel:        in.Channel,
		PartyID:        in.RecipientID,
		PartyKey:       ops.Optional(partyKey),
		EventType:      ops.Optional(in.EventType),
	})
	if err != nil {
		return true // no preference recorded
	}
	return pref.Enabled
}

// Deliver sends one notification. Called by the worker, never on a request path.
func (s *Service) Deliver(ctx context.Context, orgID, notificationID int64) error {
	// Claim it. The CAS is what stops two workers sending the same message when
	// a job is delivered twice.
	claimed, err := s.q.LockNotificationForSend(ctx, dbgen.LockNotificationForSendParams{
		OrganizationID: orgID, ID: notificationID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			// Already sent, cancelled, or out of attempts. Not an error: the
			// job has nothing left to do.
			return nil
		}
		return err
	}

	sender, ok := s.senders.For(claimed.Channel)
	if !ok {
		_, err := s.q.MarkNotificationFailed(ctx, dbgen.MarkNotificationFailedParams{
			OrganizationID: orgID, ID: claimed.ID,
			Retryable:    false,
			ErrorMessage: ops.Optional("no sender configured for channel " + claimed.Channel),
		})
		return err
	}

	msg := Message{
		Channel: claimed.Channel, To: claimed.RecipientAddress,
		Locale: claimed.Locale, Variables: decodeVars(claimed.Variables),
		Body: claimed.Body,
	}
	if claimed.ProviderRef != nil {
		// The registered sender id or approved template name. A provider that
		// requires one rejects the send without it.
		msg.ProviderRef = *claimed.ProviderRef
	}
	if claimed.Subject != nil {
		msg.Subject = *claimed.Subject
	}
	if claimed.RecipientName != nil {
		msg.ToName = *claimed.RecipientName
	}
	if claimed.RecipientName != nil {
		msg.ToName = *claimed.RecipientName
	}
	if claimed.Subject != nil {
		msg.Subject = *claimed.Subject
	}

	// Bound each attempt so one hung provider cannot stall the queue.
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	started := time.Now()
	result, sendErr := sender.Send(sendCtx, msg)
	elapsed := int32(time.Since(started).Milliseconds())

	outcome, retryable, providerStatus, errText := OutcomeSent, true, "", ""
	if sendErr != nil {
		se := classify(sendErr)
		outcome, retryable, providerStatus = se.Outcome, se.Retryable, se.ProviderStatus
		errText = se.Error()
	} else if result != nil {
		providerStatus = result.ProviderStatus
	}

	var providerMessageID *string
	if result != nil && result.ProviderMessageID != "" {
		providerMessageID = &result.ProviderMessageID
	}

	if _, aErr := s.q.CreateNotificationAttempt(ctx, dbgen.CreateNotificationAttemptParams{
		PublicID:          publicid.New("nta"),
		OrganizationID:    orgID,
		NotificationID:    claimed.ID,
		AttemptNo:         claimed.AttemptCount,
		Provider:          sender.Name(),
		Outcome:           outcome,
		ProviderMessageID: providerMessageID,
		ProviderStatus:    ops.Optional(providerStatus),
		ErrorMessage:      ops.Optional(errText),
		Retryable:         retryable,
		DurationMs:        &elapsed,
	}); aErr != nil {
		s.log.Warn("could not record a notification attempt",
			"notificationId", claimed.PublicID, "error", aErr)
	}

	if sendErr == nil {
		_, err := s.q.MarkNotificationSent(ctx, dbgen.MarkNotificationSentParams{
			OrganizationID: orgID, ID: claimed.ID,
			Provider:          ops.Optional(sender.Name()),
			ProviderMessageID: providerMessageID,
		})
		return err
	}

	// One schedule, used twice: stored on the row so an operator can see when
	// the next attempt is due, and handed to the queue below so that is when it
	// actually happens.
	//
	// The delay, not the wake-up time: the database adds it to its own clock.
	retryIn := backoff(int(claimed.AttemptCount))
	if _, err := s.q.MarkNotificationFailed(ctx, dbgen.MarkNotificationFailedParams{
		OrganizationID: orgID, ID: claimed.ID,
		Retryable:    retryable,
		ErrorMessage: ops.Optional(errText),
		RetryAfter:   interval(retryIn),
	}); err != nil {
		return err
	}

	// A permanent failure is not a job failure: the message is dead-lettered
	// and there is nothing for the queue to retry. Returning an error here
	// would make the job retry a send that can never succeed.
	if !retryable {
		s.log.Info("notification dead-lettered",
			"notificationId", claimed.PublicID, "channel", claimed.Channel,
			"outcome", outcome, "error", errText)
		return nil
	}
	return jobs.RetryAfter(retryIn,
		fmt.Errorf("notification %s: %w", claimed.PublicID, sendErr))
}

// RegisterHandlers wires the delivery job into a worker.
func (s *Service) RegisterHandlers(w *jobs.Worker) {
	w.Register(JobTypeSend, func(ctx context.Context, job jobs.Job) error {
		var payload struct {
			OrganizationID int64 `json:"organizationId"`
			NotificationID int64 `json:"notificationId"`
		}
		if err := job.Decode(&payload); err != nil {
			return err
		}
		return s.Deliver(ctx, payload.OrganizationID, payload.NotificationID)
	})
}

// ---------------------------------------------------------------------------
// Template rendering
// ---------------------------------------------------------------------------

var placeholder = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// Render substitutes {{variable}} placeholders.
//
// An unknown placeholder renders as empty rather than leaving "{{awb}}" in a
// customer-facing message. A gap is better than a visible template artefact —
// and MissingVariables exists so an editor can catch it before it ships.
func Render(template string, vars map[string]string) string {
	return placeholder.ReplaceAllStringFunc(template, func(match string) string {
		name := placeholder.FindStringSubmatch(match)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return ""
	})
}

// MissingVariables lists placeholders a template uses that the supplied
// variables do not cover. Used to validate a template at configuration time.
func MissingVariables(template string, vars map[string]string) []string {
	seen := map[string]bool{}
	var missing []string
	for _, m := range placeholder.FindAllStringSubmatch(template, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, ok := vars[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// Placeholders lists every variable a template refers to.
func Placeholders(template string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(template, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func decodeVars(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// interval converts a Go duration to the pgtype form the queries take.
func interval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

// SweepDue re-enqueues notifications that are due but have no job behind them.
//
// Raise() logs and continues when the enqueue fails, because a queue problem
// must not roll back the shipment that caused the message. The price of that
// choice is a row nobody will ever pick up; this is what collects it.
//
// Re-enqueueing is safe to repeat: the job dedupe key is the notification's
// public id, so a row that already has a live job produces nothing.
func (s *Service) SweepDue(ctx context.Context, limit int32) (int, error) {
	rows, err := s.q.ListDueNotifications(ctx, dbgen.ListDueNotificationsParams{
		RowLimit: limit,
	})
	if err != nil {
		return 0, err
	}
	requeued := 0
	for _, row := range rows {
		id, err := s.jobs.Enqueue(ctx, JobTypeSend, map[string]any{
			"organizationId": row.OrganizationID,
			"notificationId": row.ID,
		}, jobs.EnqueueOptions{
			OrganizationID: &row.OrganizationID,
			Queue:          "notifications",
			DedupeKey:      "ntf:" + row.PublicID,
			MaxAttempts:    5,
		})
		if err != nil {
			s.log.Warn("could not re-enqueue a due notification",
				"notificationId", row.PublicID, "error", err)
			continue
		}
		if id != "" {
			requeued++
		}
	}
	return requeued, nil
}
