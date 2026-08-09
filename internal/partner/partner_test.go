package partner

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ceserve/courier-os/internal/platform/security"
)

// The signature scheme is the security boundary of the whole webhook feature:
// it is the only thing telling a consumer a request really came from us.

func TestSignatureVerifies(t *testing.T) {
	body := []byte(`{"id":"evt_1","type":"shipment.delivered"}`)
	ts := time.Now().Unix()
	sig := Sign("whsec_abc", ts, body)

	if !VerifySignature("whsec_abc", sig, ts, body) {
		t.Fatal("a signature we generated did not verify")
	}
}

func TestSignatureIsVersioned(t *testing.T) {
	// The prefix lets the algorithm change without silently breaking every
	// consumer: they can reject a version they do not understand.
	sig := Sign("s", 1, []byte("x"))
	if !strings.HasPrefix(sig, "v1=") {
		t.Fatalf("signature %q is not version-prefixed", sig)
	}
}

func TestSignatureRejectsATamperedBody(t *testing.T) {
	body := []byte(`{"amount":100}`)
	ts := time.Now().Unix()
	sig := Sign("whsec_abc", ts, body)

	tampered := []byte(`{"amount":900}`)
	if VerifySignature("whsec_abc", sig, ts, tampered) {
		t.Fatal("a tampered body verified against the original signature")
	}
}

func TestSignatureRejectsAChangedTimestamp(t *testing.T) {
	// The timestamp is inside the signed string, which is what stops a captured
	// request being replayed later with a fresh timestamp header.
	body := []byte(`{"id":"evt_1"}`)
	ts := time.Now().Unix()
	sig := Sign("whsec_abc", ts, body)

	if VerifySignature("whsec_abc", sig, ts+60, body) {
		t.Fatal("a signature verified against a different timestamp — replay is possible")
	}
}

func TestSignatureRejectsTheWrongSecret(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	ts := time.Now().Unix()
	sig := Sign("whsec_real", ts, body)

	if VerifySignature("whsec_attacker", sig, ts, body) {
		t.Fatal("a signature verified under the wrong secret")
	}
}

func TestSignatureIsStableForTheSameInputs(t *testing.T) {
	// A retry must send the same signature for the same payload and timestamp,
	// or a consumer caching by signature would see them as different events.
	body := []byte(`{"id":"evt_1"}`)
	a := Sign("s", 1700000000, body)
	b := Sign("s", 1700000000, body)
	if a != b {
		t.Fatalf("signing is not deterministic: %s vs %s", a, b)
	}
}

// ---------------------------------------------------------------------------
// Scopes
// ---------------------------------------------------------------------------

func TestScopeEnforcement(t *testing.T) {
	auth := &Authenticated{Scopes: []string{ScopeShipmentCreate, ScopeTrackingRead}}

	if !auth.Has(ScopeShipmentCreate) {
		t.Fatal("a granted scope was not recognised")
	}
	if auth.Has(ScopeShipmentCancel) {
		t.Fatal("an ungranted scope was reported as held")
	}
	if err := auth.Require(ScopeTrackingRead); err != nil {
		t.Fatalf("a granted scope was refused: %v", err)
	}
	err := auth.Require(ScopePODRead)
	if err == nil {
		t.Fatal("an ungranted scope was allowed")
	}
	// The error must name what was needed, or a partner cannot fix their
	// integration without contacting support.
	if !strings.Contains(err.Error(), ScopePODRead) {
		t.Fatalf("the error does not name the missing scope: %v", err)
	}
}

func TestAKeyWithNoScopesCanDoNothing(t *testing.T) {
	// The safe default for a key created by mistake.
	auth := &Authenticated{Scopes: nil}
	for _, scope := range AllScopes {
		if auth.Has(scope) {
			t.Fatalf("a key with no scopes reported holding %s", scope)
		}
	}
}

func TestValidateScopesRejectsATypo(t *testing.T) {
	// Caught at issue time. A typo that silently granted nothing would be found
	// only when the partner's integration mysteriously 403s in production.
	if err := ValidateScopes([]string{"shipment:creat"}); err == nil {
		t.Fatal("a misspelled scope was accepted")
	}
	if err := ValidateScopes([]string{ScopeShipmentCreate, ScopeTrackingRead}); err != nil {
		t.Fatalf("valid scopes were rejected: %v", err)
	}
	if err := ValidateScopes(nil); err != nil {
		t.Fatalf("an empty scope set should be valid: %v", err)
	}
}

func TestAllScopesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range AllScopes {
		if seen[s] {
			t.Fatalf("scope %s is listed twice", s)
		}
		seen[s] = true
	}
}

// ---------------------------------------------------------------------------
// Network restriction
// ---------------------------------------------------------------------------

func TestIPAllowList(t *testing.T) {
	cidrs := []string{"203.0.113.0/24", "198.51.100.7/32"}

	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"203.0.113.45", true},
		{"203.0.113.0", true},
		{"198.51.100.7", true},
		{"198.51.100.8", false},
		{"192.0.2.1", false},
	} {
		got := ipAllowed(parseIP(t, tc.ip), cidrs)
		if got != tc.want {
			t.Errorf("ipAllowed(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}

	// A nil address must never pass an allow-list: failing open would defeat
	// the restriction entirely when the client IP cannot be determined.
	if ipAllowed(nil, cidrs) {
		t.Fatal("a nil client address passed the allow-list")
	}
}

// ---------------------------------------------------------------------------
// Backoff
// ---------------------------------------------------------------------------

func TestWebhookBackoffGrowsAndIsCapped(t *testing.T) {
	if webhookBackoff(1) >= webhookBackoff(2) {
		t.Fatal("backoff does not grow")
	}
	// Capped, so a long partner outage does not push a retry past the point
	// the event is still useful to them.
	if got := webhookBackoff(30); got != 30*time.Minute {
		t.Fatalf("webhookBackoff(30) = %v, want the 30m cap", got)
	}
	// Starts fast: a transient 502 usually clears in seconds.
	if got := webhookBackoff(1); got != 10*time.Second {
		t.Fatalf("first retry after %v, want 10s", got)
	}
}

func TestRandomTokenIsUniqueAndURLSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tok, err := randomToken(32)
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatal("randomToken produced a duplicate")
		}
		seen[tok] = true
		// Must survive being put in a header and a URL without escaping.
		if strings.ContainsAny(tok, "+/= .") {
			t.Fatalf("token %q is not URL-safe", tok)
		}
	}
}

func parseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("could not parse %s", s)
	}
	return ip
}

// ---------------------------------------------------------------------------
// Secret digests
// ---------------------------------------------------------------------------

// The stored form of an API key secret is the security boundary at rest, and
// its cost is the throughput ceiling of the whole partner surface. Both matter.

func TestSecretDigestIsDeterministicAndNotThePlaintext(t *testing.T) {
	secret, err := randomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	a, err := hashSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("hashing is not deterministic")
	}
	if strings.Contains(a, secret) {
		t.Fatal("the digest contains the plaintext secret")
	}
	if !strings.HasPrefix(a, digestPrefix) {
		t.Fatalf("the digest is not self-describing: %q", a)
	}
}

func TestDifferentSecretsProduceDifferentDigests(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		secret, err := randomToken(32)
		if err != nil {
			t.Fatal(err)
		}
		d, err := hashSecret(secret)
		if err != nil {
			t.Fatal(err)
		}
		if seen[d] {
			t.Fatal("two secrets produced the same digest")
		}
		seen[d] = true
	}
}

func TestVerifyAcceptsTheFastDigestAndRejectsAWrongSecret(t *testing.T) {
	svc := &KeyService{hasher: security.NewHasher(security.DefaultArgon2Params())}

	secret, err := randomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := hashSecret(secret)
	if err != nil {
		t.Fatal(err)
	}

	if !svc.verifySecret(stored, secret) {
		t.Fatal("the correct secret did not verify")
	}

	wrong, _ := randomToken(32)
	if svc.verifySecret(stored, wrong) {
		t.Fatal("a wrong secret verified")
	}
	// A malformed presentation must fail rather than panic or pass.
	if svc.verifySecret(stored, "not base64 !!!") {
		t.Fatal("a malformed secret verified")
	}
}

func TestVerifyStillAcceptsALegacyArgon2Key(t *testing.T) {
	// Keys issued before the digest change carry an Argon2id hash. They must
	// keep working — there is no migration, and breaking them would silently
	// take every existing partner integration offline.
	hasher := security.NewHasher(security.Argon2Params{Time: 1, MemoryKiB: 8192, Parallelism: 1})
	svc := &KeyService{hasher: hasher}

	secret, err := randomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := hasher.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(legacy, "$argon2") {
		t.Fatalf("the fixture is not an Argon2 hash: %q", legacy)
	}

	if !svc.verifySecret(legacy, secret) {
		t.Fatal("a legacy Argon2 key stopped working")
	}
	wrong, _ := randomToken(32)
	if svc.verifySecret(legacy, wrong) {
		t.Fatal("a wrong secret verified against a legacy key")
	}
}
