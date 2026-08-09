package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/notification"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/tenant"
	"github.com/ceserve/courier-os/tests/harness"
)

// recordingSender is a Sender that reports whatever a test tells it to, and
// remembers what it was asked to send.
//
// This is how provider failure is exercised: a real adapter cannot be made to
// fail on demand, and the behaviour that matters — retry classification,
// attempt recording, dead-lettering — is the service's, not the provider's.
type recordingSender struct {
	mu       sync.Mutex
	channel  string
	sent     []notification.Message
	failWith error
	// failFirst makes the first n attempts fail, so a test can prove a message
	// recovers rather than only that it retries.
	failFirst int
	calls     int
}

func (s *recordingSender) Name() string    { return "recording" }
func (s *recordingSender) Channel() string { return s.channel }

func (s *recordingSender) Send(ctx context.Context, msg notification.Message) (*notification.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.failFirst > 0 && s.calls <= s.failFirst {
		return nil, notification.Transient(notification.OutcomeTimeout, "504",
			errors.New("gateway timeout"))
	}
	if s.failWith != nil {
		return nil, s.failWith
	}
	s.sent = append(s.sent, msg)
	return &notification.Result{ProviderMessageID: "rec-1", ProviderStatus: "queued"}, nil
}

func (s *recordingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func notificationService(env *harness.Env, senders ...notification.Sender) *notification.Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := notification.NewRegistry()
	for _, s := range senders {
		reg.Register(s)
	}
	return notification.NewService(env.DB, env.Queries, jobs.NewEnqueuer(env.Queries), reg,
		audit.NewRecorder(env.Queries, log), log)
}

func raise(
	t *testing.T, env *harness.Env, svc *notification.Service,
	p *tenant.Principal, in notification.Request,
) *dbgen.Notification {
	t.Helper()
	var out *dbgen.Notification
	if err := env.DB.InTx(context.Background(), func(tx pgx.Tx) error {
		var e error
		out, e = svc.Raise(context.Background(), tx, p, in)
		return e
	}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	return out
}

// TestNotificationIsQueuedNotSent proves the rule the whole module is built
// around: raising a notification does not contact a provider.
func TestNotificationIsQueuedNotSent(t *testing.T) {
	env, _, p := financeEnv(t)
	sender := &recordingSender{channel: notification.ChannelSMS}
	svc := notificationService(env, sender)

	n := raise(t, env, svc, p, notification.Request{
		EventType:        notification.EventShipmentBooked,
		Channel:          notification.ChannelSMS,
		RecipientType:    "CONSIGNEE",
		RecipientAddress: "+919876543210",
		Variables: map[string]string{
			"awb": "DMO260808000042", "organizationName": "Demo", "trackingUrl": "https://t.example/x",
		},
	})

	if n.Status != notification.StatusPending {
		t.Fatalf("status = %s, want PENDING", n.Status)
	}
	if sender.count() != 0 {
		t.Fatal("raising a notification contacted the provider on the request path")
	}
	// The body was rendered at raise time from the seeded template.
	if n.Body == "" || n.TemplateCode == nil {
		t.Fatalf("no template was resolved: body=%q template=%v", n.Body, n.TemplateCode)
	}
	if !containsAny(n.Body, "DMO260808000042") {
		t.Fatalf("the AWB was not substituted into the body: %q", n.Body)
	}

	// A job was queued to do the actual sending.
	var queued int64
	mustQueryRow(t, env, `SELECT count(*) FROM jobs WHERE job_type='notification.send'`,
		nil, &queued)
	if queued != 1 {
		t.Fatalf("%d delivery jobs queued, want 1", queued)
	}
}

// TestDeliverySendsAndRecordsAnAttempt walks the worker path.
func TestDeliverySendsAndRecordsAnAttempt(t *testing.T) {
	env, _, p := financeEnv(t)
	sender := &recordingSender{channel: notification.ChannelSMS}
	svc := notificationService(env, sender)
	ctx := context.Background()

	n := raise(t, env, svc, p, notification.Request{
		EventType: notification.EventShipmentDelivered, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "X1", "deliveredAt": "today", "receivedBy": "Asha"},
	})

	if err := svc.Deliver(ctx, p.OrganizationID, n.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("provider was called %d times, want 1", sender.count())
	}

	var status string
	mustQueryRow(t, env, `SELECT status FROM notifications WHERE id=$1`, []any{n.ID}, &status)
	if status != notification.StatusSent {
		t.Fatalf("status = %s, want SENT", status)
	}

	var attempts int64
	var outcome string
	mustQueryRow(t, env,
		`SELECT count(*), max(outcome) FROM notification_attempts WHERE notification_id=$1`,
		[]any{n.ID}, &attempts, &outcome)
	if attempts != 1 || outcome != notification.OutcomeSent {
		t.Fatalf("%d attempts with outcome %s, want 1 SENT", attempts, outcome)
	}
}

// TestTransientFailureRetriesAndThenSucceeds is the provider-failure test the
// specification asks for.
func TestTransientFailureRetriesAndThenSucceeds(t *testing.T) {
	env, _, p := financeEnv(t)
	sender := &recordingSender{channel: notification.ChannelSMS, failFirst: 2}
	svc := notificationService(env, sender)
	ctx := context.Background()

	n := raise(t, env, svc, p, notification.Request{
		EventType: notification.EventShipmentDelivered, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "X2"},
	})

	// Two failures. Each returns an error so the job queue retries.
	for i := 0; i < 2; i++ {
		if err := svc.Deliver(ctx, p.OrganizationID, n.ID); err == nil {
			t.Fatalf("attempt %d: a transient failure should surface as an error so the job retries", i+1)
		}
		var status string
		mustQueryRow(t, env, `SELECT status FROM notifications WHERE id=$1`, []any{n.ID}, &status)
		if status != notification.StatusPending {
			t.Fatalf("after a retryable failure status = %s, want PENDING", status)
		}
	}

	// Third attempt succeeds.
	if err := svc.Deliver(ctx, p.OrganizationID, n.ID); err != nil {
		t.Fatalf("third attempt: %v", err)
	}

	var status string
	var attemptCount int32
	mustQueryRow(t, env, `SELECT status, attempt_count FROM notifications WHERE id=$1`,
		[]any{n.ID}, &status, &attemptCount)
	if status != notification.StatusSent {
		t.Fatalf("status = %s, want SENT after recovery", status)
	}
	if attemptCount != 3 {
		t.Fatalf("attempt_count = %d, want 3", attemptCount)
	}

	// Every attempt is recorded, including the failures — this is what a
	// support engineer reads when a customer says they got nothing.
	var attempts int64
	mustQueryRow(t, env, `SELECT count(*) FROM notification_attempts WHERE notification_id=$1`,
		[]any{n.ID}, &attempts)
	if attempts != 3 {
		t.Fatalf("%d attempts recorded, want 3", attempts)
	}
	var failed int64
	mustQueryRow(t, env,
		`SELECT count(*) FROM notification_attempts WHERE notification_id=$1 AND outcome='TIMEOUT'`,
		[]any{n.ID}, &failed)
	if failed != 2 {
		t.Fatalf("%d timeout attempts recorded, want 2", failed)
	}
}

// TestPermanentFailureDeadLettersWithoutRetrying proves the adapter's retry
// decision is respected: a rejected address must not burn the attempt budget.
func TestPermanentFailureDeadLettersWithoutRetrying(t *testing.T) {
	env, _, p := financeEnv(t)
	sender := &recordingSender{
		channel: notification.ChannelSMS,
		failWith: notification.Permanent(notification.OutcomeRejected, "21211",
			errors.New("invalid 'To' number")),
	}
	svc := notificationService(env, sender)
	ctx := context.Background()

	n := raise(t, env, svc, p, notification.Request{
		EventType: notification.EventShipmentDelivered, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "X3"},
	})

	// A permanent failure returns nil: there is nothing for the job queue to
	// retry, so making the job fail would just burn its own budget too.
	if err := svc.Deliver(ctx, p.OrganizationID, n.ID); err != nil {
		t.Fatalf("a permanent failure should not surface as a job error: %v", err)
	}

	var status string
	var attemptCount int32
	mustQueryRow(t, env, `SELECT status, attempt_count FROM notifications WHERE id=$1`,
		[]any{n.ID}, &status, &attemptCount)
	if status != notification.StatusDeadLetter {
		t.Fatalf("status = %s, want DEAD_LETTER", status)
	}
	if attemptCount != 1 {
		t.Fatalf("attempt_count = %d — a permanent failure must not be retried", attemptCount)
	}

	// The provider's own code is preserved verbatim for support.
	var providerStatus string
	mustQueryRow(t, env,
		`SELECT provider_status FROM notification_attempts WHERE notification_id=$1`,
		[]any{n.ID}, &providerStatus)
	if providerStatus != "21211" {
		t.Fatalf("provider status = %q, want it preserved", providerStatus)
	}
}

// TestSuppressionIsRecordedRatherThanFailing covers the four reasons a message
// is never attempted. Each must produce an explicable row, not an error.
func TestSuppressionIsRecordedRatherThanFailing(t *testing.T) {
	env, _, p := financeEnv(t)
	ctx := context.Background()

	t.Run("no valid address", func(t *testing.T) {
		svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})
		n := raise(t, env, svc, p, notification.Request{
			EventType: notification.EventShipmentBooked, Channel: notification.ChannelSMS,
			RecipientType: "CONSIGNEE", RecipientAddress: "not-a-number",
		})
		if n.Status != notification.StatusSuppressed {
			t.Fatalf("status = %s, want SUPPRESSED", n.Status)
		}
		if n.SuppressedReason == nil || *n.SuppressedReason != notification.SuppressNoAddress {
			t.Fatalf("reason = %v, want NO_VALID_ADDRESS", n.SuppressedReason)
		}
	})

	t.Run("channel not configured", func(t *testing.T) {
		// No sender registered for WhatsApp.
		svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})
		n := raise(t, env, svc, p, notification.Request{
			EventType: notification.EventShipmentBooked, Channel: notification.ChannelWhatsApp,
			RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		})
		if n.SuppressedReason == nil || *n.SuppressedReason != notification.SuppressNoSender {
			t.Fatalf("reason = %v, want CHANNEL_NOT_CONFIGURED", n.SuppressedReason)
		}
	})

	t.Run("no template", func(t *testing.T) {
		svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})
		n := raise(t, env, svc, p, notification.Request{
			EventType: "AN_EVENT_WITH_NO_TEMPLATE", Channel: notification.ChannelSMS,
			RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		})
		if n.SuppressedReason == nil || *n.SuppressedReason != notification.SuppressNoTemplate {
			t.Fatalf("reason = %v, want NO_TEMPLATE_CONFIGURED", n.SuppressedReason)
		}
	})

	t.Run("recipient opted out", func(t *testing.T) {
		svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})
		if _, err := env.DB.Pool.Exec(ctx, `
			INSERT INTO notification_preferences (public_id, organization_id, party_type,
				party_key, channel, enabled)
			VALUES (gen_seed_public_id('npf'), $1, 'CONSIGNEE', '+919999900000', 'SMS', false)`,
			p.OrganizationID); err != nil {
			t.Fatalf("seed preference: %v", err)
		}
		n := raise(t, env, svc, p, notification.Request{
			EventType: notification.EventShipmentBooked, Channel: notification.ChannelSMS,
			RecipientType: "CONSIGNEE", RecipientAddress: "+919999900000",
		})
		if n.SuppressedReason == nil || *n.SuppressedReason != notification.SuppressOptedOut {
			t.Fatalf("reason = %v, want RECIPIENT_OPTED_OUT", n.SuppressedReason)
		}
	})

	// A suppressed notification never queues a job.
	var jobCount int64
	mustQueryRow(t, env, `SELECT count(*) FROM jobs WHERE job_type='notification.send'`,
		nil, &jobCount)
	if jobCount != 0 {
		t.Fatalf("%d jobs queued for suppressed notifications, want 0", jobCount)
	}
}

// TestDuplicateRaiseCollapsesOntoOneMessage proves a retried event does not
// text the customer twice.
func TestDuplicateRaiseCollapsesOntoOneMessage(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})

	req := notification.Request{
		EventType: notification.EventShipmentOutForDelivery, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "X4"},
		DedupeKey: "ofd:X4",
	}

	first := raise(t, env, svc, p, req)
	second := raise(t, env, svc, p, req)

	if second.ID != first.ID {
		t.Fatalf("the repeat created notification %d, want the original %d", second.ID, first.ID)
	}
	var count int64
	mustQueryRow(t, env, `SELECT count(*) FROM notifications WHERE dedupe_key='ofd:X4'`,
		nil, &count)
	if count != 1 {
		t.Fatalf("%d notifications for one deduped event, want 1", count)
	}
}

// TestConcurrentDeliverySendsOnce proves the claim CAS holds when a job is
// delivered to two workers at once.
func TestConcurrentDeliverySendsOnce(t *testing.T) {
	env, _, p := financeEnv(t)
	sender := &recordingSender{channel: notification.ChannelSMS}
	svc := notificationService(env, sender)
	ctx := context.Background()

	n := raise(t, env, svc, p, notification.Request{
		EventType: notification.EventShipmentDelivered, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "X5"},
	})

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_ = svc.Deliver(ctx, p.OrganizationID, n.ID)
		}()
	}
	wg.Wait()

	if sender.count() != 1 {
		t.Fatalf("the provider was called %d times for one notification, want 1", sender.count())
	}
}

// TestNotificationsAreTenantIsolated proves one tenant cannot see another's
// messages, which would leak customer phone numbers and addresses.
func TestNotificationsAreTenantIsolated(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	a := env.NewTenant(t, geo, harness.TenantOptions{})
	b := env.NewTenant(t, geo, harness.TenantOptions{})

	pa := &tenant.Principal{UserID: a.AdminUserID, OrganizationID: a.OrgID, OrganizationCurrency: "NGN"}
	pb := &tenant.Principal{UserID: b.AdminUserID, OrganizationID: b.OrgID, OrganizationCurrency: "NGN"}
	svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})
	ctx := context.Background()

	n := raise(t, env, svc, pa, notification.Request{
		EventType: notification.EventShipmentBooked, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "A1"},
	})

	// B cannot read it.
	if _, err := env.Queries.GetNotificationByPublicID(ctx, dbgen.GetNotificationByPublicIDParams{
		OrganizationID: pb.OrganizationID, PublicID: n.PublicID,
	}); err == nil {
		t.Fatal("tenant B read tenant A's notification")
	}
	// B cannot deliver it — the claim is scoped by organization.
	if err := svc.Deliver(ctx, pb.OrganizationID, n.ID); err != nil {
		t.Fatalf("cross-tenant deliver should be a no-op, got: %v", err)
	}
	var status string
	mustQueryRow(t, env, `SELECT status FROM notifications WHERE id=$1`, []any{n.ID}, &status)
	if status != notification.StatusPending {
		t.Fatalf("tenant B advanced tenant A's notification to %s", status)
	}
}

// TestNotificationAttemptsAreAppendOnly proves delivery history cannot be
// rewritten — it is the evidence for "we did tell the customer".
func TestNotificationAttemptsAreAppendOnly(t *testing.T) {
	env, _, p := financeEnv(t)
	svc := notificationService(env, &recordingSender{channel: notification.ChannelSMS})
	ctx := context.Background()

	n := raise(t, env, svc, p, notification.Request{
		EventType: notification.EventShipmentDelivered, Channel: notification.ChannelSMS,
		RecipientType: "CONSIGNEE", RecipientAddress: "+919876543210",
		Variables: map[string]string{"awb": "X6"},
	})
	if err := svc.Deliver(ctx, p.OrganizationID, n.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := env.DB.Pool.Exec(ctx,
		`UPDATE notification_attempts SET outcome='FAILED' WHERE notification_id=$1`,
		n.ID); err == nil {
		t.Fatal("a notification attempt was edited")
	}
	if _, err := env.DB.Pool.Exec(ctx,
		`DELETE FROM notification_attempts WHERE notification_id=$1`, n.ID); err == nil {
		t.Fatal("a notification attempt was deleted")
	}
}
