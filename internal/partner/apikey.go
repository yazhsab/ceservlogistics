// Package partner implements M32: the external API surface and webhooks.
//
// # Two credential systems, deliberately separate
//
// A user carries *permissions* checked against their roles. A partner carries
// *scopes* checked against their key. They are different systems and never mix:
// a partner integration must not be able to acquire a user's rights by holding
// a key, and a scope is not a permission that could accidentally be granted to
// a role.
//
// # Secret handling
//
// A key is two halves. The `key_id` identifies the organization and is safe to
// log. The secret is 32 bytes of cryptographic randomness, stored only as a
// SHA-256 digest; the plaintext exists exactly once, in the response to the
// create call. There is no recovery path — a lost key is replaced.
//
// SHA-256 rather than a password KDF, for the same reason refresh tokens use it
// (see security.NewOpaqueToken): the input already carries full entropy, so
// there is nothing to brute-force and stretching buys nothing. It also has to be
// cheap, because unlike a password this is verified on *every* request.
//
// That is not a theoretical concern. Argon2id at the platform's password
// settings costs ~85 ms of CPU and 64 MiB of transient allocation per
// verification; measured against the 4 vCPU / 16 GB target box that capped the
// partner API at roughly 45 requests per second and made every concurrent
// partner request a 64 MiB allocation. The identical query without key auth
// answered in ~1 ms.
//
// Keys issued before this change carry an Argon2id hash and are still accepted:
// Verify recognises the format and falls back. Nothing needs migrating, and a
// re-issued key picks up the fast path.
//
// Verification is constant-time and always pays a comparison, including for an
// unknown key id, so an attacker cannot distinguish "no such key" from "wrong
// secret" by timing.
package partner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/security"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Scopes a partner key can hold. A key with no scopes can do nothing, which is
// the safe default for one created by mistake.
const (
	ScopeServiceabilityRead = "serviceability:read"
	ScopePricingRead        = "pricing:read"
	ScopeShipmentCreate     = "shipment:create"
	ScopeShipmentRead       = "shipment:read"
	ScopeShipmentCancel     = "shipment:cancel"
	ScopeTrackingRead       = "tracking:read"
	ScopePickupCreate       = "pickup:create"
	ScopePickupRead         = "pickup:read"
	ScopeLabelRead          = "label:read"
	ScopePODRead            = "pod:read"
	ScopeWebhookManage      = "webhook:manage"
)

// AllScopes is the catalogue, and the source the database CHECK mirrors.
var AllScopes = []string{
	ScopeServiceabilityRead, ScopePricingRead,
	ScopeShipmentCreate, ScopeShipmentRead, ScopeShipmentCancel,
	ScopeTrackingRead, ScopePickupCreate, ScopePickupRead,
	ScopeLabelRead, ScopePODRead, ScopeWebhookManage,
}

// Key statuses.
const (
	KeyActive    = "ACTIVE"
	KeySuspended = "SUSPENDED"
	KeyRevoked   = "REVOKED"
	KeyExpired   = "EXPIRED"
)

// Error codes.
const (
	CodeInvalidKey   = "API_KEY_INVALID"
	CodeKeyRevoked   = "API_KEY_REVOKED"
	CodeKeyExpired   = "API_KEY_EXPIRED"
	CodeKeySuspended = "API_KEY_SUSPENDED"
	CodeScopeMissing = "API_SCOPE_MISSING"
	CodeIPNotAllowed = "API_KEY_IP_NOT_ALLOWED"
	CodeUnknownScope = "API_SCOPE_UNKNOWN"
)

// KeyService issues and verifies partner credentials.
type KeyService struct {
	db     *database.DB
	q      *dbgen.Queries
	hasher *security.Hasher
	audit  *audit.Recorder
	log    *slog.Logger
}

func NewKeyService(
	db *database.DB, q *dbgen.Queries, hasher *security.Hasher,
	rec *audit.Recorder, log *slog.Logger,
) *KeyService {
	return &KeyService{db: db, q: q, hasher: hasher, audit: rec, log: log}
}

// IssueRequest describes a new key.
type IssueRequest struct {
	Name         string
	Scopes       []string
	AllowedCIDRs []string
	ExpiresAt    *time.Time
	RateLimit    *int32
}

// IssuedKey is returned once, at creation.
type IssuedKey struct {
	Key dbgen.ApiKey
	// Secret is the only time the plaintext exists. It is not stored and
	// cannot be recovered; the caller must show it to the operator now.
	Secret string
	// Token is the convenience form the partner puts in a header:
	// "<keyId>.<secret>".
	Token string
}

// Issue creates a key and returns its plaintext secret exactly once.
func (s *KeyService) Issue(
	ctx context.Context, p *tenant.Principal, in IssueRequest,
) (*IssuedKey, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apierr.Validation("An API key needs a name.", nil)
	}
	if err := ValidateScopes(in.Scopes); err != nil {
		return nil, err
	}
	for _, c := range in.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(c); err != nil {
			return nil, apierr.Validation("Invalid CIDR.",
				map[string]any{"allowedCidrs": c})
		}
	}

	// Both array columns are NOT NULL with an empty default, and a nil Go slice
	// is sent as NULL rather than omitted — so normalise here. Empty scopes is a
	// legitimate state (a key that can do nothing); NULL is not a state at all.
	if in.Scopes == nil {
		in.Scopes = []string{}
	}
	if in.AllowedCIDRs == nil {
		in.AllowedCIDRs = []string{}
	}

	keyID, err := randomToken(12)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	keyID = "ck_" + keyID
	secret, err := randomToken(32)
	if err != nil {
		return nil, apierr.Internal(err)
	}

	hash, err := hashSecret(secret)
	if err != nil {
		return nil, apierr.Internal(err)
	}

	created, err := s.q.CreateAPIKey(ctx, dbgen.CreateAPIKeyParams{
		PublicID:       publicid.New("apk"),
		OrganizationID: p.OrganizationID,
		Name:           in.Name,
		KeyID:          keyID,
		SecretHash:     hash,
		// Enough to tell two keys apart in a list, not enough to be useful to
		// anyone who sees it.
		SecretHint:         secret[:6] + "…",
		Scopes:             in.Scopes,
		AllowedCidrs:       in.AllowedCIDRs,
		ExpiresAt:          in.ExpiresAt,
		RateLimitPerMinute: in.RateLimit,
		CreatedBy:          &p.UserID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	// Audited without the secret, obviously — but *with* the scopes, because
	// what a key was allowed to do is the security-relevant fact.
	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "apikey.issued", ResourceType: "api_key",
		ResourceID: &created.ID, ResourcePublicID: created.PublicID,
		Metadata: map[string]any{
			"name": in.Name, "keyId": keyID, "scopes": in.Scopes,
			"expiresAt": in.ExpiresAt,
		},
	}))

	return &IssuedKey{Key: created, Secret: secret, Token: keyID + "." + secret}, nil
}

// Authenticated is what a verified partner request carries.
type Authenticated struct {
	Principal *tenant.Principal
	KeyID     int64
	KeyName   string
	Scopes    []string
	RateLimit int32
}

// Has reports whether the key holds a scope.
func (a *Authenticated) Has(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Require returns a 403 unless the key holds the scope.
func (a *Authenticated) Require(scope string) error {
	if a.Has(scope) {
		return nil
	}
	return apierr.Forbidden(
		fmt.Sprintf("This API key does not hold the %s scope.", scope)).
		WithDetail("requiredScope", scope).
		WithDetail("grantedScopes", a.Scopes)
}

// Verify authenticates a partner token of the form "<keyId>.<secret>".
//
// Every failure path costs the same Argon2 verification, so an attacker cannot
// learn whether a key id exists by measuring response time.
func (s *KeyService) Verify(ctx context.Context, token string, clientIP net.IP) (*Authenticated, error) {
	keyID, secret, ok := strings.Cut(token, ".")
	if !ok || keyID == "" || secret == "" {
		return nil, apierr.New(http.StatusUnauthorized, CodeInvalidKey,
			"The API credential is not valid.")
	}

	row, err := s.q.FindAPIKeyByKeyID(ctx, keyID)
	if err != nil {
		// Unknown key id. Still perform a comparison against a fixed digest so
		// the timing is the same as a wrong secret for an existing key.
		_ = s.verifySecret(dummyDigest, secret)
		return nil, apierr.New(http.StatusUnauthorized, CodeInvalidKey,
			"The API credential is not valid.")
	}

	if !s.verifySecret(row.SecretHash, secret) {
		return nil, apierr.New(http.StatusUnauthorized, CodeInvalidKey,
			"The API credential is not valid.")
	}

	// The secret is right. Now the key's own state, with messages that name the
	// problem — the caller has proven they hold the credential, so telling them
	// it is revoked leaks nothing.
	switch row.Status {
	case KeyRevoked:
		return nil, apierr.New(http.StatusUnauthorized, CodeKeyRevoked,
			"This API key has been revoked.")
	case KeySuspended:
		return nil, apierr.New(http.StatusUnauthorized, CodeKeySuspended,
			"This API key is suspended.")
	case KeyExpired:
		return nil, apierr.New(http.StatusUnauthorized, CodeKeyExpired,
			"This API key has expired.")
	}
	if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) {
		return nil, apierr.New(http.StatusUnauthorized, CodeKeyExpired,
			"This API key has expired.")
	}

	if len(row.AllowedCidrs) > 0 && !ipAllowed(clientIP, row.AllowedCidrs) {
		return nil, apierr.New(http.StatusForbidden, CodeIPNotAllowed,
			"This API key is not permitted from this network.")
	}

	rateLimit := int32(0)
	if row.RateLimitPerMinute != nil {
		rateLimit = *row.RateLimitPerMinute
	}

	return &Authenticated{
		Principal: &tenant.Principal{
			OrganizationID:        row.OrganizationID,
			OrganizationPublicID:  row.OrganizationPublicID,
			OrganizationCode:      row.OrganizationCode,
			OrganizationCurrency:  row.OrganizationCurrency,
			OrganizationTimezone:  row.OrganizationTimezone,
			OrganizationCountry:   row.OrganizationCountry,
			OrganizationAWBPrefix: row.OrganizationAwbPrefix,
		},
		KeyID: row.ID, KeyName: row.Name, Scopes: row.Scopes, RateLimit: rateLimit,
	}, nil
}

// Revoke permanently disables a key.
func (s *KeyService) Revoke(
	ctx context.Context, p *tenant.Principal, publicID, reason string,
) (*dbgen.ApiKey, error) {
	if len([]rune(reason)) < 3 {
		return nil, apierr.Validation("Revoking a key requires a reason.",
			map[string]any{"reason": "at least 3 characters"})
	}
	key, err := s.q.GetAPIKeyByPublicID(ctx, dbgen.GetAPIKeyByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "API key")
	}
	revoked, err := s.q.RevokeAPIKey(ctx, dbgen.RevokeAPIKeyParams{
		OrganizationID: p.OrganizationID, ID: key.ID,
		RevokedBy: &p.UserID, RevokeReason: ops.Optional(reason),
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(CodeKeyRevoked, "This key is already revoked.")
		}
		return nil, apierr.Internal(err)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "apikey.revoked", ResourceType: "api_key",
		ResourceID: &key.ID, ResourcePublicID: key.PublicID, Reason: reason,
		Metadata: map[string]any{"name": key.Name, "keyId": key.KeyID},
	}))
	return &revoked, nil
}

// Suspend pauses a key, or resumes a suspended one.
//
// Distinct from revocation on purpose. A suspected leak that turns out to be a
// misconfigured proxy should not force a partner through a key rotation, and a
// revoked key can never come back — the database trigger sees to that.
func (s *KeyService) Suspend(
	ctx context.Context, p *tenant.Principal, publicID string, suspend bool, reason string,
) (*dbgen.ApiKey, error) {
	key, err := s.q.GetAPIKeyByPublicID(ctx, dbgen.GetAPIKeyByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "API key")
	}
	updated, err := s.q.SuspendAPIKey(ctx, dbgen.SuspendAPIKeyParams{
		OrganizationID: p.OrganizationID, ID: key.ID, Suspend: suspend,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			// The only statuses the statement will not move are REVOKED and
			// EXPIRED, both of which are permanent.
			return nil, apierr.Conflict(apierr.CodeConflict,
				"This key is "+strings.ToLower(key.Status)+" and cannot be suspended or resumed.")
		}
		return nil, apierr.Internal(err)
	}

	action := "apikey.resumed"
	if suspend {
		action = "apikey.suspended"
	}
	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: action, ResourceType: "api_key",
		ResourceID: &key.ID, ResourcePublicID: key.PublicID, Reason: reason,
		Before: map[string]any{"status": key.Status},
		After:  map[string]any{"status": updated.Status},
	}))
	return &updated, nil
}

// UsageSummary aggregates a key's recent traffic.
type UsageSummary struct {
	RequestCount  int64
	ErrorCount    int64
	AvgDurationMs int32
}

// Usage returns a traffic summary plus the most recent calls.
//
// The recent list is bounded rather than paginated: it exists to answer "what
// is this integration doing right now", and an operator who needs the full
// history has the audit trail.
func (s *KeyService) Usage(
	ctx context.Context, p *tenant.Principal, publicID string, window time.Duration,
) (UsageSummary, []dbgen.ApiKeyRequest, error) {
	key, err := s.q.GetAPIKeyByPublicID(ctx, dbgen.GetAPIKeyByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return UsageSummary{}, nil, ops.NotFoundOr(err, "API key")
	}
	row, err := s.q.SummariseAPIKeyUsage(ctx, dbgen.SummariseAPIKeyUsageParams{
		ApiKeyID: key.ID, Since: time.Now().Add(-window),
	})
	if err != nil {
		return UsageSummary{}, nil, apierr.Internal(err)
	}
	recent, err := s.q.ListAPIKeyRequests(ctx, dbgen.ListAPIKeyRequestsParams{
		OrganizationID: p.OrganizationID, ApiKeyID: key.ID, Limit: 50,
	})
	if err != nil {
		return UsageSummary{}, nil, apierr.Internal(err)
	}
	return UsageSummary{
		RequestCount: row.RequestCount, ErrorCount: row.ErrorCount,
		AvgDurationMs: row.AvgDurationMs,
	}, recent, nil
}

// List returns the tenant's keys, never their hashes.
func (s *KeyService) List(
	ctx context.Context, p *tenant.Principal, status string, limit, offset int32,
) ([]dbgen.ListAPIKeysRow, error) {
	rows, err := s.q.ListAPIKeys(ctx, dbgen.ListAPIKeysParams{
		OrganizationID: p.OrganizationID, Status: ops.Optional(status),
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// RecordUsage logs a partner request. Fire-and-forget: a logging failure must
// never fail the request it describes.
func (s *KeyService) RecordUsage(
	ctx context.Context, orgID, keyID int64,
	method, route string, statusCode, durationMs int32,
	requestID, errorCode string, clientIP net.IP,
) {
	// sqlc maps inet to netip.Addr; convert once here rather than threading a
	// second address type through every caller.
	var ip *netip.Addr
	if clientIP != nil {
		if addr, ok := netip.AddrFromSlice(clientIP); ok {
			unmapped := addr.Unmap()
			ip = &unmapped
		}
	}
	if err := s.q.RecordAPIKeyRequest(ctx, dbgen.RecordAPIKeyRequestParams{
		OrganizationID: orgID, ApiKeyID: keyID,
		Method: method, Route: route, StatusCode: statusCode, DurationMs: durationMs,
		RequestID: ops.Optional(requestID), ErrorCode: ops.Optional(errorCode),
		ClientIp: ip,
	}); err != nil {
		s.log.Warn("could not record partner API usage", "error", err)
	}
	if err := s.q.TouchAPIKey(ctx, dbgen.TouchAPIKeyParams{ID: keyID, ClientIp: ip}); err != nil {
		s.log.Warn("could not update API key usage", "error", err)
	}
}

// ValidateScopes rejects an unknown scope at issue time rather than letting a
// typo silently grant nothing.
func ValidateScopes(scopes []string) error {
	known := map[string]bool{}
	for _, s := range AllScopes {
		known[s] = true
	}
	for _, s := range scopes {
		if !known[s] {
			return apierr.Validation("Unknown API scope.",
				map[string]any{"scope": s, "allowed": AllScopes})
		}
	}
	return nil
}

func ipAllowed(ip net.IP, cidrs []string) bool {
	if ip == nil {
		return false
	}
	for _, c := range cidrs {
		if _, network, err := net.ParseCIDR(c); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// hashSecret returns the stored digest for an API key secret.
func hashSecret(secret string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("partner: malformed secret")
	}
	sum := sha256.Sum256(raw)
	return digestPrefix + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

// digestPrefix marks the fast digest format, so a stored value's scheme is
// self-describing and an Argon2id hash from before the change is recognisable.
const digestPrefix = "sha256$"

// dummyDigest is compared against when the key id is unknown, so the failure
// costs the same as a wrong secret and the two are not distinguishable by
// timing.
var dummyDigest = digestPrefix + base64.RawStdEncoding.EncodeToString(make([]byte, 32))

// verifySecret checks a presented secret against a stored digest.
//
// Accepts both formats: keys issued before the switch carry an Argon2id hash
// and keep working, which is why there is no migration.
func (s *KeyService) verifySecret(stored, secret string) bool {
	if !strings.HasPrefix(stored, digestPrefix) {
		// Legacy Argon2id key. Slow, but only for keys issued before the
		// change, and re-issuing one moves it to the fast path.
		_, err := s.hasher.Verify(stored, secret)
		return err == nil
	}
	expected, err := hashSecret(secret)
	if err != nil {
		// A malformed presented secret still pays a comparison, so it costs the
		// same as a well-formed wrong one, then fails.
		_ = security.ConstantTimeEqual([]byte(stored), []byte(dummyDigest))
		return false
	}
	return security.ConstantTimeEqual([]byte(stored), []byte(expected))
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
