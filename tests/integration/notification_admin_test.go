package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

// M26's administration surface. The delivery path deliberately never fails a
// business operation, which means a suppressed or dead-lettered message is
// invisible unless somebody can go and look. These tests are about whether they
// can.

func TestNotificationOutboxShowsWhatWasSuppressedAndWhy(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// Booking raises customer notifications through the transition observer.
	bookOne(t, env, tn)

	resp := env.Do(t, http.MethodGet, "/api/v1/notifications", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("outbox: %d %s", resp.Status, resp.Raw)
	}
	items, _ := resp.Body["data"].([]any)
	if len(items) == 0 {
		t.Fatal("booking raised no notifications")
	}

	// Whatever their outcome, each row must say what it was for and — if it was
	// not sent — why. A silent row is the failure mode this surface exists to
	// prevent.
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if et, _ := item["eventType"].(string); et == "" {
			t.Error("a notification has no event type")
		}
		status, _ := item["status"].(string)
		if status == "SUPPRESSED" {
			if reason, _ := item["suppressedReason"].(string); reason == "" {
				t.Error("a suppressed notification does not say why")
			}
		}
	}
}

func TestNotificationHealthAndChannels(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	health := env.Do(t, http.MethodGet, "/api/v1/notifications/health", tn.AdminAccessTok, nil)
	if health.Status != http.StatusOK {
		t.Fatalf("health: %d %s", health.Status, health.Raw)
	}
	if _, ok := health.Body["byStatus"]; !ok {
		t.Error("health reports no status breakdown")
	}

	// A channel with no adapter suppresses every message on it, which is the
	// most common cause of "notifications are not working" and is otherwise
	// invisible.
	channels := env.Do(t, http.MethodGet, "/api/v1/notifications/channels",
		tn.AdminAccessTok, nil)
	if channels.Status != http.StatusOK {
		t.Fatalf("channels: %d %s", channels.Status, channels.Raw)
	}
	items, _ := channels.Body["data"].([]any)
	if len(items) != 4 {
		t.Fatalf("the channel list has %d entries, want 4", len(items))
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if _, ok := item["configured"]; !ok {
			t.Errorf("channel %v does not say whether it is configured", item["channel"])
		}
	}
}

func TestTemplatePreviewNamesMissingVariables(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	created := env.Do(t, http.MethodPost, "/api/v1/notifications/templates",
		tn.AdminAccessTok, map[string]any{
			"code": "TEST_TPL", "name": "Test", "eventType": "SHIPMENT_BOOKED",
			// WHATSAPP, because the seed already provides SMS and EMAIL for this
			// event and only one active template may serve a given combination.
			"channel": "WHATSAPP",
			"body":    "Parcel {{awb}} for {{recipientName}} — {{missingOne}}",
		})
	if created.Status != http.StatusCreated {
		t.Fatalf("create template: %d %s", created.Status, created.Raw)
	}
	id, _ := created.Body["id"].(string)

	// The declared variable list is derived from the body, not taken from the
	// caller: a list that disagreed with the text would be a lie the first
	// recipient discovers.
	vars, _ := created.Body["variables"].([]any)
	if len(vars) != 3 {
		t.Fatalf("the template declares %d variables, want 3: %v", len(vars), vars)
	}

	preview := env.Do(t, http.MethodPost,
		"/api/v1/notifications/templates/"+id+"/preview", tn.AdminAccessTok,
		map[string]any{"variables": map[string]string{
			"awb": "QYN260808000001", "recipientName": "Asha",
		}})
	if preview.Status != http.StatusOK {
		t.Fatalf("preview: %d %s", preview.Status, preview.Raw)
	}
	body, _ := preview.Body["body"].(string)
	if !strings.Contains(body, "QYN260808000001") || !strings.Contains(body, "Asha") {
		t.Fatalf("the preview did not render: %q", body)
	}
	missing, _ := preview.Body["missingVariables"].([]any)
	if len(missing) != 1 {
		t.Fatalf("the preview reports %d missing variables, want 1: %v", len(missing), missing)
	}
	if got, _ := missing[0].(string); got != "missingOne" {
		t.Fatalf("missing variable = %q", got)
	}
}

func TestEmailTemplateNeedsASubject(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// An email with no subject line arrives looking like spam.
	resp := env.Do(t, http.MethodPost, "/api/v1/notifications/templates",
		tn.AdminAccessTok, map[string]any{
			"code": "NOSUBJ", "name": "No subject", "eventType": "SHIPMENT_BOOKED",
			"channel": "EMAIL", "body": "Hello",
		})
	if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
		t.Fatalf("an email template with no subject was accepted: %d %s", resp.Status, resp.Raw)
	}
	if !strings.Contains(resp.Raw, "subject") {
		t.Errorf("the refusal does not name the field: %s", resp.Raw)
	}
}

func TestNotificationsAreTenantScoped(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})
	bookOne(t, env, beta)

	resp := env.Do(t, http.MethodGet, "/api/v1/notifications", alpha.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("outbox: %d %s", resp.Status, resp.Raw)
	}
	items, _ := resp.Body["data"].([]any)
	if len(items) != 0 {
		t.Fatalf("alpha sees %d of beta's notifications", len(items))
	}

	// Templates too: beta's seeded set must not appear to alpha as extra rows.
	var alphaCount, betaCount int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM notification_templates WHERE organization_id = $1`,
		alpha.OrgID).Scan(&alphaCount); err != nil {
		t.Fatal(err)
	}
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM notification_templates WHERE organization_id = $1`,
		beta.OrgID).Scan(&betaCount); err != nil {
		t.Fatal(err)
	}
	list := env.Do(t, http.MethodGet, "/api/v1/notifications/templates",
		alpha.AdminAccessTok, nil)
	tpls, _ := list.Body["data"].([]any)
	if len(tpls) != alphaCount {
		t.Fatalf("alpha's template listing shows %d, want its own %d (beta has %d)",
			len(tpls), alphaCount, betaCount)
	}
}

func TestNotificationRetryAndCancel(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)
	bookOne(t, env, tn)

	// Find a pending notification and cancel it: a message not yet sent can be
	// stopped, one already sent cannot.
	var publicID, status string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT public_id, status FROM notifications WHERE organization_id = $1
		  ORDER BY id LIMIT 1`, tn.OrgID).Scan(&publicID, &status); err != nil {
		t.Skipf("no notification was raised to act on: %v", err)
	}

	if status == "PENDING" {
		cancel := env.Do(t, http.MethodPost,
			"/api/v1/notifications/"+publicID+"/cancel", tn.AdminAccessTok,
			map[string]any{"reason": "raised in error"})
		if cancel.Status != http.StatusOK {
			t.Fatalf("cancel: %d %s", cancel.Status, cancel.Raw)
		}
		if got, _ := cancel.Body["status"].(string); got != "CANCELLED" {
			t.Fatalf("status after cancel = %q", got)
		}
		// Cancelling is a decision somebody made about a customer message, so
		// it is on the audit trail.
		var audits int
		if err := env.DB.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM audit_events
			  WHERE action = 'notification.cancelled' AND resource_public_id = $1`,
			publicID).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if audits != 1 {
			t.Fatalf("cancellation audit records = %d, want 1", audits)
		}
	}

	// A dead-lettered message can be retried; a sent one cannot.
	env.MustExec(t,
		`UPDATE notifications SET status = 'SENT', sent_at = now()
		  WHERE organization_id = $1 AND public_id = $2`, tn.OrgID, publicID)
	retry := env.Do(t, http.MethodPost,
		"/api/v1/notifications/"+publicID+"/retry", tn.AdminAccessTok, nil)
	if retry.Status != http.StatusConflict {
		t.Fatalf("retrying a sent notification returned %d, want 409: %s",
			retry.Status, retry.Raw)
	}
}

func TestNotificationAdminNeedsPermissions(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// A delivery agent has no business reading the tenant's message history.
	_, _, token := env.NewUser(t, tn.OrgID, "notif-agent@example.com",
		"DELIVERY_AGENT", &tn.OriginBranchID)

	for _, tc := range []struct{ method, path, perm string }{
		{http.MethodGet, "/api/v1/notifications", "notification.read"},
		{http.MethodGet, "/api/v1/notifications/templates", "notification.read"},
		{http.MethodPost, "/api/v1/notifications/templates", "notification.template"},
	} {
		var body any
		if tc.method == http.MethodPost {
			body = map[string]any{}
		}
		resp := env.Do(t, tc.method, tc.path, token, body)
		if resp.Status != http.StatusForbidden {
			t.Errorf("%s %s returned %d, want 403", tc.method, tc.path, resp.Status)
		}
		if !strings.Contains(resp.Raw, tc.perm) {
			t.Errorf("%s %s does not name %s: %s", tc.method, tc.path, tc.perm, resp.Raw)
		}
	}
}

func TestDuplicateTemplateIsAConflictNotAnError(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := commandTenant(t, env)

	// The seed already serves SHIPMENT_BOOKED over SMS in English. A second
	// active template for the same combination would leave ResolveTemplate
	// picking one arbitrarily, and the tenant would never know which text a
	// customer received — so it is refused, and refused *clearly*.
	resp := env.Do(t, http.MethodPost, "/api/v1/notifications/templates",
		tn.AdminAccessTok, map[string]any{
			"code": "DUPLICATE_SMS", "name": "Duplicate", "eventType": "SHIPMENT_BOOKED",
			"channel": "SMS", "body": "Another booking message for {{awb}}",
		})
	if resp.Status != http.StatusConflict {
		t.Fatalf("a duplicate template returned %d, want 409: %s", resp.Status, resp.Raw)
	}
	// The message has to say which combination collided, or an operator cannot
	// tell what to deactivate.
	for _, want := range []string{"SHIPMENT_BOOKED", "SMS"} {
		if !strings.Contains(resp.Raw, want) {
			t.Errorf("the conflict does not name %s: %s", want, resp.Raw)
		}
	}
}
