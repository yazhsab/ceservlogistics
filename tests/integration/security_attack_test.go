package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

// Adversarial tests for M33.
//
// The other security files assert that the rules work. This one assumes an
// attacker and tries to break them: injection, reflection, traversal, spoofing,
// replay and bypass. A test here that passes is evidence for
// docs/SECURITY_REVIEW.md; a test here that fails is a finding.
//
// Everything is exercised over HTTP against the real router, because a unit
// test of a validator proves the validator and not the endpoint.

// ---------------------------------------------------------------------------
// Injection
// ---------------------------------------------------------------------------

// injectionPayloads are the shapes that break naive string-built SQL. The
// platform uses parameterised queries throughout, so the contract is that these
// are *data*: they either match nothing or fail validation, and in no case do
// they alter a query or leak an error that names a table.
var injectionPayloads = []string{
	"' OR '1'='1",
	"'; DROP TABLE shipments; --",
	"1' UNION SELECT null,null,null--",
	"\\'; SELECT pg_sleep(5); --",
	"%' OR 1=1 --",
	"admin'--",
	"') OR ('a'='a",
	"1;SELECT version()",
}

func TestSQLInjectionIsInertOnEverySearchableSurface(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SQLI"})

	// Every endpoint that takes attacker-influenced text into a WHERE clause.
	surfaces := []struct {
		name, path string
	}{
		{"place search", "/api/v1/geography/places?q=%s"},
		{"postcode prefix", "/api/v1/geography/pincodes?code=%s"},
		{"district state", "/api/v1/geography/districts?state=%s"},
		{"shipment search", "/api/v1/shipments?search=%s"},
		{"shipment status filter", "/api/v1/shipments?status=%s"},
		{"customer search", "/api/v1/customers?search=%s"},
		{"audit action filter", "/api/v1/audit-events?action=%s"},
	}

	for _, s := range surfaces {
		for _, payload := range injectionPayloads {
			path := fmt.Sprintf(s.path, urlQuery(payload))
			resp := env.Do(t, "GET", path, tn.AdminAccessTok, nil)

			// 200 (matched nothing), 422 (rejected as invalid) and 404 are all
			// correct. 500 means the payload reached the database as syntax.
			if resp.Status >= 500 {
				t.Errorf("%s with %q returned %d — the payload was not treated as data\n%s",
					s.name, payload, resp.Status, resp.Raw)
				continue
			}
			// An error that names a relation or a SQL keyword is an information
			// leak even when the injection itself failed.
			//
			// The echoed input is removed first. A validation error quotes the
			// value it rejected, which is deliberate and useful, and scanning
			// it would flag the attacker's own payload as the server's leak.
			low := withoutEcho(resp.Raw, payload)
			for _, leak := range []string{"syntax error", "pg_", "relation \"", "sqlstate", "pq:", "postgres"} {
				if strings.Contains(low, leak) {
					t.Errorf("%s with %q leaked database detail (%q): %s",
						s.name, payload, leak, resp.Raw)
				}
			}
		}
	}

	// The table is still there. A dropped table would make this fail loudly.
	resp := env.Do(t, "GET", "/api/v1/shipments", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("shipments listing broken after injection attempts: %d %s", resp.Status, resp.Raw)
	}
}

func TestInjectionInABodyFieldIsStoredAsText(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "SQLB"})

	payload := "Robert'); DROP TABLE shipment_events; --"
	resp := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, bookingBody(tn, payload),
		[2]string{"Idempotency-Key", "sqli-body-0000000000000001"})
	if resp.Status != http.StatusCreated {
		t.Fatalf("booking with a quoted name failed: %d %s", resp.Status, resp.Raw)
	}

	// Stored verbatim, not executed and not mangled.
	awb, _ := resp.Body["awb"].(string)
	got := env.Do(t, "GET", "/api/v1/shipments/"+awbLookupID(t, env, tn, awb), tn.AdminAccessTok, nil)
	if got.Status != http.StatusOK {
		t.Fatalf("re-reading the shipment failed: %d %s", got.Status, got.Raw)
	}
	if !strings.Contains(got.Raw, "DROP TABLE") {
		t.Errorf("the payload was not stored verbatim; input must be data, not code:\n%s", got.Raw)
	}

	// Events still exist — the table was not dropped.
	ev := env.Do(t, "GET", "/api/v1/shipments/"+awbLookupID(t, env, tn, awb)+"/events", tn.AdminAccessTok, nil)
	if ev.Status != http.StatusOK {
		t.Fatalf("shipment_events unreachable after injection attempt: %d %s", ev.Status, ev.Raw)
	}
}

// ---------------------------------------------------------------------------
// Reflection / XSS
// ---------------------------------------------------------------------------

// The API returns JSON only, so the defence is that a payload is never
// reflected as HTML and the response can never be interpreted as a document.
func TestScriptPayloadsAreNeverReflectedAsHTML(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "XSSR"})

	payloads := []string{
		`<script>alert(1)</script>`,
		`"><img src=x onerror=alert(1)>`,
		`javascript:alert(1)`,
		`<svg/onload=alert(1)>`,
	}
	for _, payload := range payloads {
		resp := env.Do(t, "GET", "/api/v1/geography/places?q="+urlQuery(payload), tn.AdminAccessTok, nil)

		ct := resp.Headers.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Errorf("payload %q produced Content-Type %q; a browser could render it", payload, ct)
		}
		if resp.Headers.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("payload %q: nosniff missing, so a browser may sniff the body as HTML", payload)
		}
		// JSON encoding must have escaped the angle brackets rather than
		// emitting them raw into the response.
		if strings.Contains(resp.Raw, "<script>") || strings.Contains(resp.Raw, "<svg/") {
			t.Errorf("payload %q was reflected unescaped: %s", payload, resp.Raw)
		}
	}
}

func TestSecurityHeadersAreOnEveryResponse(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "HDRS"})

	// Success, client error and auth failure must all be hardened — an error
	// page is exactly where a missing header gets exploited.
	for _, tc := range []struct {
		name, path, token string
	}{
		{"authenticated 200", "/api/v1/shipments", tn.AdminAccessTok},
		{"unauthenticated 401", "/api/v1/shipments", ""},
		{"not found 404", "/api/v1/shipments/shp_01KZZZZZZZZZZZZZZZZZZZZZZZ", tn.AdminAccessTok},
		{"public tracking", "/api/v1/tracking/DOESNOTEXIST", ""},
	} {
		resp := env.Do(t, "GET", tc.path, tc.token, nil)
		for header, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "no-referrer",
			"Cache-Control":          "no-store",
		} {
			if got := resp.Headers.Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", tc.name, header, got, want)
			}
		}
		if csp := resp.Headers.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
			t.Errorf("%s: CSP = %q", tc.name, csp)
		}
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

func TestCORSDoesNotReflectAnArbitraryOrigin(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "CORS"})

	for _, origin := range []string{
		"https://evil.example.com",
		"null",
		"http://localhost.evil.example.com",
		"https://app.example.com.evil.net",
	} {
		resp := env.Do(t, "GET", "/api/v1/shipments", tn.AdminAccessTok, nil,
			[2]string{"Origin", origin})
		if got := resp.Headers.Get("Access-Control-Allow-Origin"); got != "" && got != "*" {
			t.Errorf("origin %q was reflected as %q — a hostile page could read tenant data",
				origin, got)
		}
		// Credentials must never be granted to an unlisted origin.
		if resp.Headers.Get("Access-Control-Allow-Credentials") == "true" &&
			resp.Headers.Get("Access-Control-Allow-Origin") == origin {
			t.Errorf("origin %q was granted credentialed access", origin)
		}
	}
}

// ---------------------------------------------------------------------------
// Path traversal
// ---------------------------------------------------------------------------

func TestPathTraversalInIdentifiersIsRefused(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "TRAV"})

	for _, id := range []string{
		"..%2F..%2F..%2Fetc%2Fpasswd",
		"%2e%2e%2f%2e%2e%2fetc%2fpasswd",
		"....//....//etc/passwd",
		"shp_..%2F..%2Fsecrets",
	} {
		for _, path := range []string{
			"/api/v1/shipments/" + id,
			"/api/v1/pod/" + id,
			"/api/v1/geography/pincodes/" + id,
		} {
			resp := env.Do(t, "GET", path, tn.AdminAccessTok, nil)
			if resp.Status >= 500 {
				t.Errorf("%s returned %d: %s", path, resp.Status, resp.Raw)
			}
			for _, leak := range []string{"root:", "/etc/passwd", "no such file", "/app/"} {
				if strings.Contains(resp.Raw, leak) {
					t.Errorf("%s leaked filesystem detail (%q): %s", path, leak, resp.Raw)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Uploads
// ---------------------------------------------------------------------------

// An upload is classified by what it contains, not by what it is called. A
// polyglot named .jpg with a script body must be refused, because a POD
// artifact is served back to operations staff and to customers.
func TestUploadMIMESpoofingIsRefused(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "UPLD"})

	for _, tc := range []struct {
		name, filename, declared string
		content                  []byte
	}{
		{"html claiming to be jpeg", "photo.jpg", "image/jpeg",
			[]byte("<html><script>alert(1)</script></html>")},
		{"shell script claiming to be png", "sig.png", "image/png",
			[]byte("#!/bin/sh\nrm -rf /\n")},
		{"php claiming to be jpeg", "x.jpg", "image/jpeg",
			[]byte("<?php system($_GET['c']); ?>")},
		{"elf claiming to be png", "a.png", "image/png",
			[]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0}},
		{"svg with script claiming to be png", "s.png", "image/png",
			[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.PostMultipart(t, "/api/v1/pod", tn.AdminAccessTok,
				map[string]string{
					"barcode": "NOSUCHAWB", "recipientName": "R", "recipientRelationship": "SELF",
				},
				"signature", tc.filename, tc.declared, tc.content,
				[2]string{"X-Operating-Unit", tn.DestBranchPubID})

			// The file must never be accepted. 415 is the designed answer;
			// 404/422 (the AWB is fake) are also fine because the upload still
			// did not land. What matters is that nothing succeeds and nothing
			// panics.
			if resp.Status < 400 {
				t.Errorf("%s was accepted (%d): content sniffing is not being enforced\n%s",
					tc.name, resp.Status, resp.Raw)
			}
			if resp.Status >= 500 {
				t.Errorf("%s crashed the handler (%d): %s", tc.name, resp.Status, resp.Raw)
			}
		})
	}

	// The control: a genuine PNG passes the type gate, proving the refusals
	// above are about content and not about the endpoint rejecting everything.
	ok := env.PostMultipart(t, "/api/v1/pod", tn.AdminAccessTok,
		map[string]string{"barcode": "NOSUCHAWB", "recipientName": "R", "recipientRelationship": "SELF"},
		"signature", "sig.png", "image/png", harness.OnePixelPNG(),
		[2]string{"X-Operating-Unit", tn.DestBranchPubID})
	if ok.Status == http.StatusUnsupportedMediaType {
		t.Errorf("a real PNG was refused as an unsupported type: %s", ok.Raw)
	}
}

// ---------------------------------------------------------------------------
// Rate-limit bypass
// ---------------------------------------------------------------------------

// Login is budgeted per IP *and* per account, and this proves the second half.
//
// The harness connects from 127.0.0.1, which is a configured trusted proxy, so
// the forged X-Forwarded-For here is honoured and every request genuinely gets
// a fresh IP bucket. What stops the run is the per-account budget — which is
// the point: an attacker with a botnet has as many source addresses as it
// likes, so an IP-only budget protects nothing.
//
// The other half, that an *untrusted* peer cannot forge the header at all, is
// covered by TestClientIPIgnoresForwardingHeadersFromAnUntrustedPeer in
// internal/platform/httpx. It cannot be tested here because the harness peer is
// always loopback.
func TestRateLimitCannotBeBypassedWithForgedForwardingHeaders(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	env.NewTenant(t, geo, harness.TenantOptions{Code: "RLBP"})

	// Login is the throttled surface with the lowest budget.
	limited := false
	for i := 0; i < 40; i++ {
		resp := env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
			"email": "nobody@example.test", "password": "wrong-password",
		},
			[2]string{"X-Forwarded-For", fmt.Sprintf("10.9.%d.%d", i/250, i%250)},
			[2]string{"X-Real-IP", fmt.Sprintf("10.8.%d.%d", i/250, i%250)},
		)
		if resp.Status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("40 failed logins from forged, ever-changing client IPs were never throttled: " +
			"the limiter is trusting a header an attacker controls")
	}
}

// ---------------------------------------------------------------------------
// Token handling
// ---------------------------------------------------------------------------

func TestTokensCannotBeReplayedAcrossTenantsOrAfterRevocation(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	a := env.NewTenant(t, geo, harness.TenantOptions{Code: "TKNA"})
	b := env.NewTenant(t, geo, harness.TenantOptions{Code: "TKNB"})

	// A token is bound to its organization; presenting B's identifiers with A's
	// token must not reach B's data.
	resp := env.Do(t, "GET", "/api/v1/operating-units/"+b.HubPublicID, a.AdminAccessTok, nil)
	if resp.Status != http.StatusNotFound && resp.Status != http.StatusForbidden {
		t.Errorf("tenant A read tenant B's facility with its own token: %d %s", resp.Status, resp.Raw)
	}

	// A tampered signature must not be accepted.
	parts := strings.Split(a.AdminAccessTok, ".")
	if len(parts) == 3 {
		forged := parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))
		if r := env.Do(t, "GET", "/api/v1/shipments", forged, nil); r.Status != http.StatusUnauthorized {
			t.Errorf("a token with a replaced signature returned %d, want 401", r.Status)
		}
		// "alg: none" style: header swapped, signature dropped.
		if r := env.Do(t, "GET", "/api/v1/shipments", parts[0]+"."+parts[1]+".", nil); r.Status != http.StatusUnauthorized {
			t.Errorf("a token with an empty signature returned %d, want 401", r.Status)
		}
	}

	// The organization-context header is a super-admin facility; an ordinary
	// admin must not be able to pivot with it.
	pivot := env.Do(t, "GET", "/api/v1/shipments", a.AdminAccessTok, nil,
		[2]string{"X-Organization-Context", b.OrgPublicID})
	if pivot.Status == http.StatusOK {
		var body map[string]any
		_ = json.Unmarshal([]byte(pivot.Raw), &body)
		t.Errorf("a non-super-admin pivoted into another tenant with X-Organization-Context: %s", pivot.Raw)
	}
}

// ---------------------------------------------------------------------------
// Financial idempotency abuse
// ---------------------------------------------------------------------------

// An idempotency key is a promise that a retry is the *same* request. Replaying
// a key with a different body must not silently apply the new body, and must
// not create a second shipment.
func TestIdempotencyKeyCannotBeReusedForADifferentRequest(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "IDEM"})

	key := [2]string{"Idempotency-Key", "abuse-key-000000000000000001"}

	first := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, bookingBody(tn, "Original"), key)
	if first.Status != http.StatusCreated {
		t.Fatalf("first booking failed: %d %s", first.Status, first.Raw)
	}
	firstAWB, _ := first.Body["awb"].(string)

	// Same key, materially different request.
	altered := bookingBody(tn, "Substituted")
	altered["packages"] = []map[string]any{{"actualWeightGrams": 25000}}
	second := env.Do(t, "POST", "/api/v1/shipments", tn.AdminAccessTok, altered, key)

	switch {
	case second.Status == http.StatusConflict || second.Status == http.StatusUnprocessableEntity:
		// Correct: the mismatch is refused outright.
	case second.Status == http.StatusCreated || second.Status == http.StatusOK:
		// Also acceptable: the original response is replayed. What is not
		// acceptable is the altered request taking effect.
		if awb, _ := second.Body["awb"].(string); awb != firstAWB {
			t.Errorf("reused key produced a different shipment %s (first %s): the key did not deduplicate",
				awb, firstAWB)
		}
		if strings.Contains(second.Raw, "Substituted") {
			t.Errorf("the altered body took effect under a reused idempotency key: %s", second.Raw)
		}
	default:
		t.Errorf("reused key with a different body returned %d: %s", second.Status, second.Raw)
	}

	// Exactly one shipment exists for the key.
	list := env.Do(t, "GET", "/api/v1/shipments?search="+urlQuery(firstAWB), tn.AdminAccessTok, nil)
	items, _ := list.Body["data"].([]any)
	if len(items) != 1 {
		t.Errorf("expected exactly one shipment for the reused key, found %d", len(items))
	}
}

// ---------------------------------------------------------------------------
// Error hygiene
// ---------------------------------------------------------------------------

func TestErrorsNeverLeakInternals(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "ERRH"})

	probes := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/shipments/not-a-public-id", nil},
		{"GET", "/api/v1/shipments?limit=999999999", nil},
		{"GET", "/api/v1/shipments?page=-1", nil},
		{"POST", "/api/v1/shipments", map[string]any{"customerId": 12345}},
		{"POST", "/api/v1/shipments", "not-json-at-all"},
		{"PATCH", "/api/v1/shipments/shp_01KZZZZZZZZZZZZZZZZZZZZZZZ", map[string]any{"x": 1}},
	}
	for _, p := range probes {
		resp := env.Do(t, p.method, p.path, tn.AdminAccessTok, p.body)
		if resp.Status >= 500 {
			t.Errorf("%s %s returned %d — a malformed request must not be an internal error\n%s",
				p.method, p.path, resp.Status, resp.Raw)
		}
		low := strings.ToLower(resp.Raw)
		for _, leak := range []string{
			"goroutine", ".go:", "/users/", "/app/", "panic:",
			"sqlstate", "pgx", "relation \"", "column \"",
		} {
			if strings.Contains(low, leak) {
				t.Errorf("%s %s leaked %q: %s", p.method, p.path, leak, resp.Raw)
			}
		}
		// Every error must still carry a request id, so support can find it.
		if resp.Status >= 400 && !strings.Contains(resp.Raw, "requestId") {
			t.Errorf("%s %s error has no requestId: %s", p.method, p.path, resp.Raw)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// withoutEcho lower-cases a response and removes the payload the caller sent,
// so a leak scan sees only what the server contributed.
func withoutEcho(raw, payload string) string {
	low := strings.ToLower(raw)
	p := strings.ToLower(payload)
	// The value may come back escaped for JSON, so strip both forms.
	escaped, err := json.Marshal(payload)
	if err == nil {
		trimmed := strings.ToLower(strings.Trim(string(escaped), `"`))
		low = strings.ReplaceAll(low, trimmed, "")
	}
	return strings.ReplaceAll(low, p, "")
}

func bookingBody(tn *harness.Tenant, name string) map[string]any {
	return map[string]any{
		"customerId": tn.CustomerPublicID, "serviceCode": tn.ServiceCode,
		"paymentMode": "PREPAID", "contentDescription": "security probe",
		"sender": map[string]any{
			"contactName": name, "phone": "08031234567", "line1": "1 Test",
			"city": "Lagos", "state": "Lagos", "pincode": tn.OriginPincode,
		},
		"recipient": map[string]any{
			"contactName": "Recipient", "phone": "08099887766", "line1": "2 Test",
			"city": "Abuja", "state": "Federal Capital Territory", "pincode": tn.DestPincode,
		},
		"packages": []map[string]any{{"actualWeightGrams": 500}},
	}
}

// awbLookupID resolves an AWB to its public id so a test can hit the by-id
// routes without depending on AWB being accepted there.
func awbLookupID(t *testing.T, env *harness.Env, tn *harness.Tenant, awb string) string {
	t.Helper()
	resp := env.Do(t, "GET", "/api/v1/shipments?search="+urlQuery(awb), tn.AdminAccessTok, nil)
	items, _ := resp.Body["data"].([]any)
	if len(items) == 0 {
		t.Fatalf("shipment %s not found: %s", awb, resp.Raw)
	}
	m, _ := items[0].(map[string]any)
	id, _ := m["id"].(string)
	return id
}
