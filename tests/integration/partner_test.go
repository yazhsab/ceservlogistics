package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/partner"
	"github.com/ceserve/courier-os/tests/harness"
)

// M32. Two credential systems face outward here — an API key and a signed
// webhook — and both are held by another company's systems. Everything below is
// about the boundary: what a key may do, what it may see, how fast it may ask,
// and whether a consumer can tell our request from a forged one.

// issueKey creates a partner key and returns the token to authenticate with.
func issueKey(
	t *testing.T, env *harness.Env, tn *harness.Tenant, name string, scopes []string,
) (token, publicID string) {
	t.Helper()
	resp := env.Do(t, http.MethodPost, "/api/v1/api-keys", tn.AdminAccessTok, map[string]any{
		"name": name, "scopes": scopes,
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("issue key: %d %s", resp.Status, resp.Raw)
	}
	token, _ = resp.Body["token"].(string)
	key, _ := resp.Body["key"].(map[string]any)
	publicID, _ = key["id"].(string)
	if token == "" || publicID == "" {
		t.Fatalf("issue key returned no credential: %s", resp.Raw)
	}
	return token, publicID
}

func partnerTenant(t *testing.T, env *harness.Env) *harness.Tenant {
	t.Helper()
	return env.NewTenant(t, env.Geography(t), harness.TenantOptions{})
}

// ---------------------------------------------------------------------------
// Credential handling
// ---------------------------------------------------------------------------

func TestPartnerKeySecretIsShownOnceAndNeverAgain(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	token, publicID := issueKey(t, env, tn, "aggregator", []string{partner.ScopeTrackingRead})

	list := env.Do(t, http.MethodGet, "/api/v1/api-keys", tn.AdminAccessTok, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("list keys: %d %s", list.Status, list.Raw)
	}
	// The listing must never carry anything that could reconstruct the
	// credential — not the secret, not the hash.
	for _, forbidden := range []string{token, "secretHash", "$argon2"} {
		if strings.Contains(list.Raw, forbidden) {
			t.Fatalf("the key listing leaked %q", forbidden)
		}
	}

	// And the stored form must be a digest, not the secret.
	//
	// SHA-256 rather than a password KDF is correct here and is the reason the
	// partner surface is not paying 85ms of Argon2 on every request: the secret
	// is 32 bytes of randomness, so there is nothing to brute-force. What
	// matters is that the plaintext is unrecoverable from a database
	// disclosure, which is what this asserts.
	var stored string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT secret_hash FROM api_keys WHERE public_id = $1`, publicID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "sha256$") {
		t.Fatalf("the stored secret is not a self-describing digest: %q", stored)
	}
	_, secret, _ := strings.Cut(token, ".")
	if strings.Contains(stored, secret) || strings.Contains(stored, token) {
		t.Fatal("the plaintext secret is in the database")
	}
}

func TestPartnerKeyAuthentication(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	token, _ := issueKey(t, env, tn, "auth-test", []string{partner.ScopeTrackingRead})

	for _, tc := range []struct {
		name  string
		token string
		want  string
	}{
		{"no credential", "", partner.CodeInvalidKey},
		{"malformed, no separator", "garbage", partner.CodeInvalidKey},
		{"unknown key id", "ck_nosuchkey.secret", partner.CodeInvalidKey},
		{"right key id, wrong secret", splitKeyID(token) + ".wrongsecret", partner.CodeInvalidKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", tc.token, nil)
			if resp.Status != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", resp.Status, resp.Raw)
			}
			if got := resp.ErrorCode(); got != tc.want {
				t.Fatalf("error code = %s, want %s", got, tc.want)
			}
		})
	}

	// The real credential works.
	ok := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", token, nil)
	if ok.Status != http.StatusOK {
		t.Fatalf("a valid key was refused: %d %s", ok.Status, ok.Raw)
	}
}

func TestRevokedKeyStopsWorkingAndCannotBeReinstated(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	token, publicID := issueKey(t, env, tn, "to-be-revoked", []string{partner.ScopeTrackingRead})

	if resp := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", token, nil); resp.Status != http.StatusOK {
		t.Fatalf("key should work before revocation: %d", resp.Status)
	}

	rev := env.Do(t, http.MethodPost, "/api/v1/api-keys/"+publicID+"/revoke",
		tn.AdminAccessTok, map[string]any{"reason": "credential posted to a public repository"})
	if rev.Status != http.StatusOK {
		t.Fatalf("revoke: %d %s", rev.Status, rev.Raw)
	}

	after := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", token, nil)
	if after.Status != http.StatusUnauthorized || after.ErrorCode() != partner.CodeKeyRevoked {
		t.Fatalf("a revoked key still authenticates: %d %s", after.Status, after.Raw)
	}

	// A leaked credential must never come back, whatever anyone types into the
	// database. The trigger is the guarantee; the API has no route for it at all.
	_, err := env.DB.Pool.Exec(context.Background(),
		`UPDATE api_keys SET status = 'ACTIVE' WHERE public_id = $1`, publicID)
	if err == nil {
		t.Fatal("a revoked API key was brought back to life")
	}

	// Revocation is audited, with the reason, because "when did this key stop
	// working and why" is the first question after an incident.
	var n int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events
		  WHERE action = 'apikey.revoked' AND resource_public_id = $1
		    AND reason IS NOT NULL`, publicID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("revocation audit records = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Scope enforcement
// ---------------------------------------------------------------------------

func TestScopeEnforcementOnEveryPartnerEndpoint(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	// A key holding exactly one scope. Every endpoint that needs a different
	// one must refuse it, and the refusal must name what was missing.
	token, _ := issueKey(t, env, tn, "narrow", []string{partner.ScopeTrackingRead})

	for _, tc := range []struct {
		method, path string
		body         any
		wantScope    string
	}{
		{http.MethodPost, "/api/v1/partner/serviceability", map[string]any{
			"originPincode": tn.OriginPincode, "destinationPincode": tn.DestPincode,
			"serviceCode": tn.ServiceCode,
		}, partner.ScopeServiceabilityRead},
		{http.MethodPost, "/api/v1/partner/quotes", map[string]any{}, partner.ScopePricingRead},
		{http.MethodPost, "/api/v1/partner/shipments", map[string]any{}, partner.ScopeShipmentCreate},
		{http.MethodGet, "/api/v1/partner/shipments/shp_00000000000000000000000000", nil,
			partner.ScopeShipmentRead},
		{http.MethodPost, "/api/v1/partner/shipments/shp_00000000000000000000000000/cancel",
			map[string]any{"reason": "no longer needed"}, partner.ScopeShipmentCancel},
		{http.MethodGet, "/api/v1/partner/shipments/shp_00000000000000000000000000/label", nil,
			partner.ScopeLabelRead},
		{http.MethodGet, "/api/v1/partner/shipments/shp_00000000000000000000000000/pod", nil,
			partner.ScopePODRead},
		{http.MethodPost, "/api/v1/partner/pickups", map[string]any{}, partner.ScopePickupCreate},
		{http.MethodGet, "/api/v1/partner/pickups/pkr_00000000000000000000000000", nil,
			partner.ScopePickupRead},
		{http.MethodGet, "/api/v1/partner/webhooks/endpoints", nil, partner.ScopeWebhookManage},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := env.Do(t, tc.method, tc.path, token, tc.body)
			if resp.Status != http.StatusForbidden {
				t.Fatalf("expected 403 for a missing scope, got %d: %s", resp.Status, resp.Raw)
			}
			if resp.ErrorCode() != "FORBIDDEN" {
				t.Fatalf("error code = %s", resp.ErrorCode())
			}
			// Naming the scope is the difference between a partner fixing their
			// integration in a minute and opening a support ticket.
			if !strings.Contains(resp.Raw, tc.wantScope) {
				t.Fatalf("the 403 does not name %s: %s", tc.wantScope, resp.Raw)
			}
		})
	}

	// The one scope it does hold still works — the gate is per endpoint, not a
	// blanket refusal.
	ok := env.Do(t, http.MethodGet, "/api/v1/partner/tracking/NOSUCHAWB", token, nil)
	if ok.Status == http.StatusForbidden {
		t.Fatalf("the granted scope was refused: %s", ok.Raw)
	}
}

func TestPartnerCannotReachTheInternalAPI(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	// Every scope there is. Scopes are not permissions, so even a maximally
	// privileged key must not be able to act as a user.
	token, _ := issueKey(t, env, tn, "everything", partner.AllScopes)

	for _, path := range []string{
		"/api/v1/users",
		"/api/v1/roles",
		"/api/v1/api-keys",
		"/api/v1/shipments",
		"/api/v1/ledger/accounts",
		"/api/v1/settlements",
		"/api/v1/audit-events",
	} {
		resp := env.Do(t, http.MethodGet, path, token, nil)
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("GET %s with an API key returned %d, want 401: %s",
				path, resp.Status, resp.Raw)
		}
	}
}

func TestAUserSessionCannotUseThePartnerSurface(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	// The reverse direction. A session token is not an API key, and presenting
	// one must not authenticate a partner request.
	resp := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("a user session authenticated a partner request: %d %s", resp.Status, resp.Raw)
	}
}

// ---------------------------------------------------------------------------
// Tenant isolation
// ---------------------------------------------------------------------------

func TestPartnerKeyCannotSeeAnotherTenant(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})

	// Beta books a shipment through its own staff API.
	booked := env.Do(t, http.MethodPost, "/api/v1/shipments", beta.AdminAccessTok,
		beta.BookingBody(nil), [2]string{"Idempotency-Key", harness.RandomKey()})
	if booked.Status != http.StatusCreated {
		t.Fatalf("beta booking: %d %s", booked.Status, booked.Raw)
	}
	betaShipment, _ := booked.Body["id"].(string)
	betaAWB, _ := booked.Body["awb"].(string)

	// Alpha's key holds every scope — and still must not see beta's parcel.
	alphaToken, _ := issueKey(t, env, alpha, "alpha-key", partner.AllScopes)

	for _, tc := range []struct{ name, method, path string }{
		{"read", http.MethodGet, "/api/v1/partner/shipments/" + betaShipment},
		{"label", http.MethodGet, "/api/v1/partner/shipments/" + betaShipment + "/label"},
		{"pod", http.MethodGet, "/api/v1/partner/shipments/" + betaShipment + "/pod"},
		{"track", http.MethodGet, "/api/v1/partner/tracking/" + betaAWB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.Do(t, tc.method, tc.path, alphaToken, nil)
			// 404, not 403: a cross-tenant probe must not be able to confirm
			// that the object exists somewhere.
			if resp.Status != http.StatusNotFound {
				t.Fatalf("cross-tenant %s returned %d, want 404: %s",
					tc.name, resp.Status, resp.Raw)
			}
			if strings.Contains(resp.Raw, betaAWB) {
				t.Fatalf("the 404 leaked beta's AWB: %s", resp.Raw)
			}
		})
	}

	cancel := env.Do(t, http.MethodPost,
		"/api/v1/partner/shipments/"+betaShipment+"/cancel", alphaToken,
		map[string]any{"reason": "attempting a cross-tenant cancellation"})
	if cancel.Status != http.StatusNotFound {
		t.Fatalf("cross-tenant cancel returned %d, want 404: %s", cancel.Status, cancel.Raw)
	}

	// And beta's shipment is untouched.
	after := env.Do(t, http.MethodGet, "/api/v1/shipments/"+betaShipment, beta.AdminAccessTok, nil)
	if status, _ := after.Body["status"].(string); status != "BOOKED" {
		t.Fatalf("beta's shipment status is now %q", status)
	}
}

// ---------------------------------------------------------------------------
// Booking through the partner surface
// ---------------------------------------------------------------------------

func TestPartnerBookingRequiresAnIdempotencyKeyAndReplaysOnRetry(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	token, _ := issueKey(t, env, tn, "booker",
		[]string{partner.ScopeShipmentCreate, partner.ScopeShipmentRead})

	body := tn.BookingBody(nil)

	// Unkeyed is refused. A partner's retry loop cannot see a duplicate the way
	// a human clerk can, so the key is mandatory here even though it is
	// optional on the internal endpoint.
	unkeyed := env.Do(t, http.MethodPost, "/api/v1/partner/shipments", token, body)
	if unkeyed.Status != http.StatusBadRequest {
		t.Fatalf("an unkeyed partner booking was accepted: %d %s", unkeyed.Status, unkeyed.Raw)
	}

	key := harness.RandomKey()
	first := env.Do(t, http.MethodPost, "/api/v1/partner/shipments", token, body,
		[2]string{"Idempotency-Key", key})
	if first.Status != http.StatusCreated {
		t.Fatalf("partner booking: %d %s", first.Status, first.Raw)
	}
	awb, _ := first.Body["awb"].(string)
	if awb == "" {
		t.Fatal("the booking returned no AWB")
	}

	second := env.Do(t, http.MethodPost, "/api/v1/partner/shipments", token, body,
		[2]string{"Idempotency-Key", key})
	if second.Status != http.StatusCreated {
		t.Fatalf("replay: %d %s", second.Status, second.Raw)
	}
	if second.Headers.Get("Idempotent-Replay") != "true" {
		t.Fatal("a retry was not marked as a replay")
	}
	if awb2, _ := second.Body["awb"].(string); awb2 != awb {
		t.Fatalf("a retry produced a second parcel: %s then %s", awb, awb2)
	}

	// Exactly one shipment exists.
	var n int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM shipments WHERE organization_id = $1`, tn.OrgID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("shipments = %d, want 1", n)
	}
}

func TestPartnerBookingIsAttributedToTheKeyNotAUser(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	token, _ := issueKey(t, env, tn, "attribution",
		[]string{partner.ScopeShipmentCreate, partner.ScopeShipmentCancel})

	created := env.Do(t, http.MethodPost, "/api/v1/partner/shipments", token,
		tn.BookingBody(nil), [2]string{"Idempotency-Key", harness.RandomKey()})
	if created.Status != http.StatusCreated {
		t.Fatalf("booking: %d %s", created.Status, created.Raw)
	}
	shipmentID, _ := created.Body["id"].(string)

	cancelled := env.Do(t, http.MethodPost,
		"/api/v1/partner/shipments/"+shipmentID+"/cancel", token,
		map[string]any{"reason": "customer changed their mind"})
	if cancelled.Status != http.StatusOK {
		t.Fatalf("cancel: %d %s", cancelled.Status, cancelled.Raw)
	}

	// The audit trail says API_KEY with no user, and names the key. Attributing
	// a partner's action to a person — or to user zero — would be a lie in the
	// one record that exists to answer "who did this".
	var actorType, actorLabel string
	var actorUser *int64
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT actor_type, actor_label, actor_user_id FROM audit_events
		  WHERE action = 'shipment.cancelled' AND resource_public_id = $1`,
		shipmentID).Scan(&actorType, &actorLabel, &actorUser); err != nil {
		t.Fatal(err)
	}
	if actorType != "API_KEY" {
		t.Fatalf("actor_type = %s, want API_KEY", actorType)
	}
	if actorUser != nil {
		t.Fatalf("a partner action was attributed to user %d", *actorUser)
	}
	if !strings.Contains(actorLabel, "attribution") {
		t.Fatalf("actor_label %q does not name the key", actorLabel)
	}

	// So does the shipment event, which is the operational history.
	var eventActor string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT e.actor_type FROM shipment_events e
		   JOIN shipments s ON s.id = e.shipment_id
		  WHERE s.public_id = $1 AND e.to_status = 'CANCELLED'`,
		shipmentID).Scan(&eventActor); err != nil {
		t.Fatal(err)
	}
	// PARTNER, not API_KEY: shipment_events and audit_events have different
	// actor vocabularies, and neither CHECK was widened to please the other.
	if eventActor != "PARTNER" {
		t.Fatalf("shipment event actor_type = %s, want PARTNER", eventActor)
	}
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

func TestRateLimitIsPerKeyNotPerAddress(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	if !env.Config.Security.RateLimitEnabled {
		t.Skip("rate limiting is disabled in this configuration")
	}
	tn := partnerTenant(t, env)

	// A deliberately tiny allowance, so the test is about the mechanism rather
	// than about volume.
	resp := env.Do(t, http.MethodPost, "/api/v1/api-keys", tn.AdminAccessTok, map[string]any{
		"name": "throttled", "scopes": []string{partner.ScopeTrackingRead},
		"rateLimitPerMinute": 5,
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("issue: %d %s", resp.Status, resp.Raw)
	}
	throttled, _ := resp.Body["token"].(string)
	generous, _ := issueKey(t, env, tn, "generous", []string{partner.ScopeTrackingRead})

	var limited int
	for i := 0; i < 12; i++ {
		r := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", throttled, nil)
		if r.Status == http.StatusTooManyRequests {
			limited++
			if r.Headers.Get("Retry-After") == "" {
				t.Error("a 429 carried no Retry-After header")
			}
		}
	}
	if limited == 0 {
		t.Fatal("a key with a 5/minute limit was never throttled in 12 requests")
	}

	// The second key shares the client address and must be unaffected: one
	// partner exhausting its budget cannot take another one down.
	other := env.Do(t, http.MethodGet, "/api/v1/partner/whoami", generous, nil)
	if other.Status != http.StatusOK {
		t.Fatalf("a second key was throttled by the first: %d %s", other.Status, other.Raw)
	}
	if got := other.Headers.Get("X-RateLimit-Limit"); got == "5" {
		t.Fatalf("the second key inherited the first key's limit of %s", got)
	}
}

func TestPartnerRequestsAreRecordedForBilling(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	token, publicID := issueKey(t, env, tn, "metered", []string{partner.ScopeTrackingRead})

	for i := 0; i < 3; i++ {
		env.Do(t, http.MethodGet, "/api/v1/partner/whoami", token, nil)
	}

	// Usage is written after the response on a detached context, so give it a
	// moment rather than racing it.
	deadline := time.Now().Add(5 * time.Second)
	var count int64
	for time.Now().Before(deadline) {
		if err := env.DB.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM api_key_requests r JOIN api_keys k ON k.id = r.api_key_id
			  WHERE k.public_id = $1`, publicID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if count < 3 {
		t.Fatalf("recorded %d partner requests, want at least 3", count)
	}

	usage := env.Do(t, http.MethodGet, "/api/v1/api-keys/"+publicID+"/usage", tn.AdminAccessTok, nil)
	if usage.Status != http.StatusOK {
		t.Fatalf("usage: %d %s", usage.Status, usage.Raw)
	}
	if n, _ := usage.Body["requestCount"].(float64); n < 3 {
		t.Fatalf("usage summary reports %v requests", usage.Body["requestCount"])
	}
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

// consumer is a partner's endpoint. It verifies our signature exactly the way
// the contract tells a real integrator to, which is what makes this a test of
// the published scheme rather than of our own helper.
type consumer struct {
	mu        sync.Mutex
	server    *httptest.Server
	secret    string
	received  []map[string]any
	eventIDs  []string
	failFirst int
	calls     int
	badSigs   int
}

func newConsumer(t *testing.T) *consumer {
	t.Helper()
	c := &consumer{}
	c.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		c.mu.Lock()
		c.calls++
		attempt := c.calls
		secret := c.secret
		c.mu.Unlock()

		ts, _ := strconv.ParseInt(r.Header.Get(partner.HeaderTimestamp), 10, 64)
		if secret != "" && !partner.VerifySignature(secret, r.Header.Get(partner.HeaderSignature), ts, body) {
			c.mu.Lock()
			c.badSigs++
			c.mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		c.mu.Lock()
		failFirst := c.failFirst
		c.mu.Unlock()
		if attempt <= failFirst {
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		c.mu.Lock()
		c.received = append(c.received, payload)
		if id, ok := payload["id"].(string); ok {
			c.eventIDs = append(c.eventIDs, id)
		}
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(c.server.Close)
	return c
}

func (c *consumer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.received)
}

func TestWebhookEndpointMustBeHTTPS(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	resp := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
		"name": "plaintext", "url": "http://partner.example.com/hook",
		"events": []string{partner.EventShipmentDelivered},
	})
	if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
		t.Fatalf("a plaintext webhook URL was accepted: %d %s", resp.Status, resp.Raw)
	}

	// The database refuses it too, so a code path that forgot the check could
	// not create one either.
	_, err := env.DB.Pool.Exec(context.Background(),
		`INSERT INTO webhook_endpoints (public_id, organization_id, name, url, signing_secret)
		 VALUES (gen_seed_public_id('whe'), $1, 'direct', 'http://insecure.example.com', 'x')`,
		tn.OrgID)
	if err == nil {
		t.Fatal("the database accepted a plaintext webhook URL")
	}
}

func TestWebhookSignatureIsVerifiableByAConsumer(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	c := newConsumer(t)

	created := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
		"name": "verifier", "url": c.server.URL,
		"events": []string{partner.EventShipmentBooked},
	})
	if created.Status != http.StatusCreated {
		t.Fatalf("create endpoint: %d %s", created.Status, created.Raw)
	}
	secret, _ := created.Body["signingSecret"].(string)
	if secret == "" {
		t.Fatal("no signing secret was returned")
	}
	c.mu.Lock()
	c.secret = secret
	c.mu.Unlock()

	// The endpoint uses a self-signed certificate, so the delivery client must
	// be the test server's. Deliver through the service directly rather than
	// through the worker, which is what this test is about.
	env.App.Webhooks.SetHTTPClient(c.server.Client())

	bookOne(t, env, tn)
	drainWebhooks(t, env, tn.OrgID)

	if c.count() != 1 {
		t.Fatalf("consumer received %d events, want 1", c.count())
	}
	if c.badSigs != 0 {
		t.Fatalf("%d deliveries failed signature verification", c.badSigs)
	}

	got := c.received[0]
	if got["type"] != partner.EventShipmentBooked {
		t.Fatalf("event type = %v", got["type"])
	}
	data, _ := got["data"].(map[string]any)
	if data["awb"] == nil || data["shipmentId"] == nil {
		t.Fatalf("the payload is missing shipment identity: %v", got)
	}
}

func TestWebhookRetriesUntilTheConsumerRecovers(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	c := newConsumer(t)
	c.failFirst = 2

	created := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
		"name": "flaky", "url": c.server.URL,
		"events": []string{partner.EventShipmentBooked},
	})
	if created.Status != http.StatusCreated {
		t.Fatalf("create endpoint: %d %s", created.Status, created.Raw)
	}
	secret, _ := created.Body["signingSecret"].(string)
	c.mu.Lock()
	c.secret = secret
	c.mu.Unlock()
	env.App.Webhooks.SetHTTPClient(c.server.Client())

	bookOne(t, env, tn)

	// Three passes: two 502s, then success. The backoff schedule is not waited
	// out — the delivery is driven directly, which is what the worker does once
	// next_attempt_at arrives.
	for i := 0; i < 3; i++ {
		forceDeliverable(t, env, tn.OrgID)
		drainWebhooks(t, env, tn.OrgID)
	}

	if c.count() != 1 {
		t.Fatalf("consumer accepted %d events, want exactly 1 after recovery", c.count())
	}

	var status string
	var attempts int32
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT status, attempt_count FROM webhook_deliveries WHERE organization_id = $1`,
		tn.OrgID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != partner.DeliveryDelivered {
		t.Fatalf("delivery status = %s, want DELIVERED", status)
	}
	if attempts != 3 {
		t.Fatalf("attempt_count = %d, want 3", attempts)
	}

	// Every attempt is on the record, including the failures. That history is
	// the evidence in a "you never called us" dispute.
	var recorded int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM webhook_attempts WHERE organization_id = $1`,
		tn.OrgID).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 3 {
		t.Fatalf("recorded attempts = %d, want 3", recorded)
	}
}

func TestWebhookDeadLettersAfterTheAttemptBudget(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	c := newConsumer(t)
	c.failFirst = 1000 // never recovers

	created := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
		"name": "dead", "url": c.server.URL,
		"events": []string{partner.EventShipmentBooked},
	})
	if created.Status != http.StatusCreated {
		t.Fatalf("create endpoint: %d %s", created.Status, created.Raw)
	}
	secret, _ := created.Body["signingSecret"].(string)
	c.mu.Lock()
	c.secret = secret
	c.mu.Unlock()
	env.App.Webhooks.SetHTTPClient(c.server.Client())

	bookOne(t, env, tn)
	for i := 0; i < 8; i++ {
		forceDeliverable(t, env, tn.OrgID)
		drainWebhooks(t, env, tn.OrgID)
	}

	var status string
	var attempts int32
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT status, attempt_count FROM webhook_deliveries WHERE organization_id = $1`,
		tn.OrgID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != partner.DeliveryDeadLetter {
		t.Fatalf("status = %s after %d attempts, want DEAD_LETTER", status, attempts)
	}
	// It stops rather than retrying forever: a partner whose server has been
	// down for a day must not still be generating traffic.
	if attempts > 6 {
		t.Fatalf("attempt_count = %d, past the endpoint's budget of 6", attempts)
	}
}

func TestOneBusinessEventProducesOneDeliveryPerEndpoint(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	// Two endpoints, both subscribed. Each gets exactly one delivery — fan-out
	// is per subscriber, deduplication is per (endpoint, event).
	for _, name := range []string{"first", "second"} {
		c := newConsumer(t)
		resp := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
			"name": name, "url": c.server.URL,
			"events": []string{partner.EventShipmentBooked},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, resp.Status, resp.Raw)
		}
	}

	bookOne(t, env, tn)

	var n int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM webhook_deliveries WHERE organization_id = $1`,
		tn.OrgID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deliveries = %d, want one per subscribed endpoint (2)", n)
	}

	// A second insert with the same (endpoint, event_id) is refused by the
	// index, which is what stops a retried transition telling a consumer twice.
	var endpointID int64
	var eventID string
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT endpoint_id, event_id FROM webhook_deliveries
		  WHERE organization_id = $1 LIMIT 1`, tn.OrgID).Scan(&endpointID, &eventID); err != nil {
		t.Fatal(err)
	}
	_, err := env.DB.Pool.Exec(context.Background(),
		`INSERT INTO webhook_deliveries (public_id, organization_id, endpoint_id,
		     event_type, event_id, payload)
		 VALUES (gen_seed_public_id('whd'), $1, $2, 'shipment.booked', $3, '{}'::jsonb)`,
		tn.OrgID, endpointID, eventID)
	if err == nil {
		t.Fatal("a duplicate delivery for the same event was accepted")
	}
}

func TestWebhookReplayIsANewDeliveryNotAReset(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)
	c := newConsumer(t)

	created := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
		"name": "replayed", "url": c.server.URL,
		"events": []string{partner.EventShipmentBooked},
	})
	if created.Status != http.StatusCreated {
		t.Fatalf("create endpoint: %d %s", created.Status, created.Raw)
	}
	secret, _ := created.Body["signingSecret"].(string)
	c.mu.Lock()
	c.secret = secret
	c.mu.Unlock()
	env.App.Webhooks.SetHTTPClient(c.server.Client())

	bookOne(t, env, tn)
	drainWebhooks(t, env, tn.OrgID)

	list := env.Do(t, http.MethodGet, "/api/v1/webhooks/deliveries", tn.AdminAccessTok, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("list deliveries: %d %s", list.Status, list.Raw)
	}
	items, _ := list.Body["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("deliveries listed = %d, want 1", len(items))
	}
	original, _ := items[0].(map[string]any)
	originalID, _ := original["id"].(string)

	replay := env.Do(t, http.MethodPost,
		"/api/v1/webhooks/deliveries/"+originalID+"/replay", tn.AdminAccessTok, nil)
	if replay.Status != http.StatusAccepted {
		t.Fatalf("replay: %d %s", replay.Status, replay.Raw)
	}
	drainWebhooks(t, env, tn.OrgID)

	// Two delivery rows: the original's attempt history is evidence and must
	// survive a replay.
	var rows, replays int
	if err := env.DB.Pool.QueryRow(context.Background(),
		`SELECT count(*), count(*) FILTER (WHERE replay_of_id IS NOT NULL)
		   FROM webhook_deliveries WHERE organization_id = $1`,
		tn.OrgID).Scan(&rows, &replays); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || replays != 1 {
		t.Fatalf("deliveries = %d (%d replays), want 2 (1)", rows, replays)
	}

	// The consumer saw the same event id twice — deliberately. Replay exists
	// for the case where the consumer lost the first one, and their own dedupe
	// on Webhook-Id is what protects them when they did not.
	if c.count() != 2 {
		t.Fatalf("consumer calls = %d, want 2", c.count())
	}
	if c.badSigs != 0 {
		t.Fatalf("%d replayed deliveries failed signature verification", c.badSigs)
	}
	if c.eventIDs[0] != c.eventIDs[1] {
		t.Fatalf("a replay changed the event id: %s then %s", c.eventIDs[0], c.eventIDs[1])
	}
}

func TestWebhookDeliveriesAreTenantScoped(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	alpha := env.NewTenant(t, geo, harness.TenantOptions{})
	beta := env.NewTenant(t, geo, harness.TenantOptions{})

	c := newConsumer(t)
	if resp := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", beta.AdminAccessTok,
		map[string]any{
			"name": "beta-hook", "url": c.server.URL,
			"events": []string{partner.EventShipmentBooked},
		}); resp.Status != http.StatusCreated {
		t.Fatalf("create beta endpoint: %d %s", resp.Status, resp.Raw)
	}
	bookOne(t, env, beta)

	// Alpha's admin sees none of it, and beta's own endpoint is not listed to
	// them either.
	list := env.Do(t, http.MethodGet, "/api/v1/webhooks/deliveries", alpha.AdminAccessTok, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("list: %d %s", list.Status, list.Raw)
	}
	if items, _ := list.Body["data"].([]any); len(items) != 0 {
		t.Fatalf("alpha sees %d of beta's deliveries", len(items))
	}
	endpoints := env.Do(t, http.MethodGet, "/api/v1/webhooks/endpoints", alpha.AdminAccessTok, nil)
	if items, _ := endpoints.Body["data"].([]any); len(items) != 0 {
		t.Fatalf("alpha sees %d of beta's endpoints", len(items))
	}
}

func TestUnknownEventTypeIsRejectedAtSubscription(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	resp := env.Do(t, http.MethodPost, "/api/v1/webhooks/endpoints", tn.AdminAccessTok, map[string]any{
		"name": "typo", "url": "https://partner.example.com/hook",
		"events": []string{"shipment.deliverd"},
	})
	if resp.Status != http.StatusUnprocessableEntity && resp.Status != http.StatusBadRequest {
		t.Fatalf("a misspelled event type was accepted: %d %s", resp.Status, resp.Raw)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// book creates one shipment through the staff API, which is what raises the
// shipment.booked event the webhook tests observe.
func bookOne(t *testing.T, env *harness.Env, tn *harness.Tenant) string {
	t.Helper()
	resp := env.Do(t, http.MethodPost, "/api/v1/shipments", tn.AdminAccessTok,
		tn.BookingBody(nil), [2]string{"Idempotency-Key", harness.RandomKey()})
	if resp.Status != http.StatusCreated {
		t.Fatalf("booking: %d %s", resp.Status, resp.Raw)
	}
	awb, _ := resp.Body["awb"].(string)
	return awb
}

// drainWebhooks delivers every pending webhook for a tenant, synchronously.
//
// The worker would do this on its own schedule; a test that waited for it would
// be slow and flaky, and what is being tested is the delivery behaviour rather
// than the queue's timing.
func drainWebhooks(t *testing.T, env *harness.Env, orgID int64) {
	t.Helper()
	rows, err := env.DB.Pool.Query(context.Background(),
		`SELECT id FROM webhook_deliveries
		  WHERE organization_id = $1 AND status = 'PENDING'
		    AND (next_attempt_at IS NULL OR next_attempt_at <= now())
		  ORDER BY id`, orgID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		// A failed delivery returns an error by design — that is what makes the
		// job retry — so it is not a test failure.
		_ = env.App.Webhooks.Deliver(context.Background(), orgID, id)
	}
}

// forceDeliverable brings a pending delivery's next attempt forward, so a
// retry can be exercised without waiting out the backoff.
func forceDeliverable(t *testing.T, env *harness.Env, orgID int64) {
	t.Helper()
	env.MustExec(t,
		`UPDATE webhook_deliveries SET next_attempt_at = now()
		  WHERE organization_id = $1 AND status = 'PENDING'`, orgID)
}

func splitKeyID(token string) string {
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			return token[:i]
		}
	}
	return token
}

func TestIntegrationRegistersReportTheirTotal(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	for i := 0; i < 5; i++ {
		issueKey(t, env, tn, "key-"+strconv.Itoa(i), []string{partner.ScopeTrackingRead})
	}

	// An offset listing without a total leaves a client requesting the maximum
	// and guessing whether there is another page.
	resp := env.Do(t, http.MethodGet, "/api/v1/api-keys?limit=2", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("list: %d %s", resp.Status, resp.Raw)
	}
	page, ok := resp.Body["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("the listing carries no pagination metadata: %s", resp.Raw)
	}
	if total := int64(page["totalItems"].(float64)); total != 5 {
		t.Fatalf("totalItems = %d, want 5", total)
	}
	if more, _ := page["hasMore"].(bool); !more {
		t.Fatal("hasMore is false on the first of three pages")
	}

	// The last page says so.
	last := env.Do(t, http.MethodGet, "/api/v1/api-keys?limit=2&offset=4",
		tn.AdminAccessTok, nil)
	lastPage, _ := last.Body["pagination"].(map[string]any)
	if more, _ := lastPage["hasMore"].(bool); more {
		t.Fatal("hasMore is true on the last page")
	}
}

func TestSessionCarriesItsPortalSubject(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	tn := partnerTenant(t, env)

	// A staff login is not a customer user, whatever its permissions say.
	staff := env.Do(t, http.MethodGet, "/api/v1/auth/me", tn.AdminAccessTok, nil)
	if staff.Status != http.StatusOK {
		t.Fatalf("me: %d %s", staff.Status, staff.Raw)
	}
	subject, ok := staff.Body["portal"].(map[string]any)
	if !ok {
		t.Fatalf("the profile carries no portal subject: %s", staff.Raw)
	}
	if isCustomer, _ := subject["isCustomerUser"].(bool); isCustomer {
		t.Fatal("a staff login is reported as a customer user")
	}
	if subject["franchise"] != nil {
		t.Fatal("a head-office login was bound to a franchise")
	}

	// A customer login names the accounts it acts for, so the client does not
	// have to infer them.
	token := portalUser(t, env, tn, "subject@example.com", tn.CustomerID)
	portal := env.Do(t, http.MethodGet, "/api/v1/auth/me", token, nil)
	portalSubject, _ := portal.Body["portal"].(map[string]any)
	if isCustomer, _ := portalSubject["isCustomerUser"].(bool); !isCustomer {
		t.Fatalf("a linked customer login is not reported as one: %s", portal.Raw)
	}
	customers, _ := portalSubject["customers"].([]any)
	if len(customers) != 1 {
		t.Fatalf("the subject names %d accounts, want 1", len(customers))
	}
	first, _ := customers[0].(map[string]any)
	if id, _ := first["id"].(string); !strings.HasPrefix(id, "cus_") {
		t.Fatalf("the account is not identified by public id: %v", first)
	}

	// A franchise operator's session names its franchise.
	franchiseID, fToken := franchiseUser(t, env, tn, "SUBJFRN", tn.OriginBranchID)
	_ = franchiseID
	fResp := env.Do(t, http.MethodGet, "/api/v1/auth/me", fToken, nil)
	fSubject, _ := fResp.Body["portal"].(map[string]any)
	franchise, ok := fSubject["franchise"].(map[string]any)
	if !ok {
		t.Fatalf("a franchise operator's session names no franchise: %s", fResp.Raw)
	}
	if code, _ := franchise["code"].(string); code != "SUBJFRN" {
		t.Fatalf("franchise code = %q, want SUBJFRN", code)
	}
}
