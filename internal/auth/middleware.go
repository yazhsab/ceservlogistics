package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/logging"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// sessionSnapshot is what the middleware caches per session.
//
// Caching the resolved authorization (not just the session row) removes four
// queries from every authenticated request. The TTL is deliberately short —
// 30 seconds by default — and the cache entry is deleted eagerly on logout,
// password change and role change, so a revocation takes effect immediately in
// practice and within one TTL in the worst case.
type sessionSnapshot struct {
	UserID                int64    `json:"uid"`
	UserPublicID          string   `json:"upid"`
	Email                 string   `json:"em"`
	FullName              string   `json:"fn"`
	OrganizationID        int64    `json:"oid"`
	OrganizationPublicID  string   `json:"opid"`
	OrganizationCode      string   `json:"ocode"`
	OrganizationCurrency  string   `json:"ocur"`
	OrganizationTimezone  string   `json:"otz"`
	OrganizationCountry   string   `json:"oco"`
	OrganizationAWBPrefix string   `json:"oawb"`
	IsPlatformOrg         bool     `json:"plat"`
	IsSuperAdmin          bool     `json:"sa"`
	SessionID             int64    `json:"sid"`
	SessionChainID        string   `json:"cid"`
	Permissions           []string `json:"perms"`
	Roles                 []string `json:"roles"`
	ScopedUnitIDs         []int64  `json:"units"`
	ScopedUnitPublicIDs   []string `json:"unitpids"`
	HasUnscopedRole       bool     `json:"unscoped"`
	CustomerIDs           []int64  `json:"customers"`
	IsPortalUser          bool     `json:"portal"`
}

const roleCustomer = "CUSTOMER"

// Authenticate validates the bearer token and builds the request principal.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := bearerToken(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		claims, err := s.tokens.Verify(raw)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		snap, err := s.resolveSession(r.Context(), claims.SessionID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		principal := snap.toPrincipal(claims.SessionID)

		// A platform operator may act inside a tenant by naming it explicitly.
		// Only is_super_admin unlocks this, the target is validated, and every
		// audit record written under it is flagged as impersonated.
		if orgCtx := strings.TrimSpace(r.Header.Get(httpx.HeaderOrgContext)); orgCtx != "" {
			if err := s.applyOrgContext(r.Context(), principal, orgCtx); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}

		ctx := tenant.WithPrincipal(r.Context(), principal)
		ctx = logging.WithContext(ctx, logging.FromContext(ctx).With(
			slog.String("organization_id", principal.OrganizationPublicID),
			slog.String("actor_id", principal.UserPublicID),
			slog.String("session_id", principal.SessionPublicID),
		))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuthenticate attaches a principal when a valid token is present but
// never rejects the request. Used by endpoints that behave differently for
// signed-in callers without requiring authentication.
func (s *Service) OptionalAuthenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := bearerToken(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := s.tokens.Verify(raw)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		snap, err := s.resolveSession(r.Context(), claims.SessionID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(tenant.WithPrincipal(r.Context(), snap.toPrincipal(claims.SessionID))))
	})
}

// RequirePermission gates a route on a permission code.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := tenant.FromContext(r.Context())
			if p == nil {
				httpx.WriteError(w, r, apierr.Unauthorized(apierr.CodeUnauthorized, "Authentication is required."))
				return
			}
			if err := p.Require(permission); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermission gates a route on holding at least one of the codes.
func RequireAnyPermission(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := tenant.FromContext(r.Context())
			if p == nil {
				httpx.WriteError(w, r, apierr.Unauthorized(apierr.CodeUnauthorized, "Authentication is required."))
				return
			}
			if !p.CanAny(permissions...) {
				httpx.WriteError(w, r, apierr.Forbidden("You do not have permission to perform this action.").
					WithDetail("requiredAnyOf", permissions))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSuperAdmin gates platform-operator-only routes.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := tenant.FromContext(r.Context())
		if p == nil || !p.IsSuperAdmin {
			httpx.WriteError(w, r, apierr.Forbidden("This operation is restricted to platform operators."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimitKey keys the request limiter on the session when authenticated, so
// one noisy tenant cannot exhaust another's budget through a shared NAT.
func RateLimitKey(r *http.Request) string {
	if p := tenant.FromContext(r.Context()); p != nil {
		return "usr:" + p.UserPublicID
	}
	return ""
}

// resolveSession loads and validates a session, using the cache when warm.
func (s *Service) resolveSession(ctx context.Context, sessionPublicID string) (*sessionSnapshot, error) {
	if !publicid.Valid(publicid.PrefixSession, sessionPublicID) {
		return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The access token is not valid.")
	}
	cacheKey := s.sessionCacheKey(sessionPublicID)

	var snap sessionSnapshot
	if err := s.cache.GetJSON(ctx, cacheKey, &snap); err == nil {
		s.metrics.RecordCache("session", "hit")
		return &snap, nil
	}
	s.metrics.RecordCache("session", "miss")

	session, err := s.q.GetSessionByPublicID(ctx, sessionPublicID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The session is no longer valid.")
		}
		return nil, apierr.Internal(fmt.Errorf("load session: %w", err))
	}
	if session.RevokedAt != nil {
		return nil, apierr.Unauthorized(apierr.CodeTokenRevoked, "This session has been revoked.")
	}
	if session.ExpiresAt.Before(time.Now()) {
		return nil, apierr.Unauthorized(apierr.CodeTokenExpired, "This session has expired. Please sign in again.")
	}

	user, err := s.q.GetUserWithOrganization(ctx, session.UserID)
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("load user: %w", err))
	}
	if user.Status != "ACTIVE" {
		return nil, apierr.Unauthorized(apierr.CodeAccountInactive, "This account is not active.")
	}
	if user.OrganizationStatus != "ACTIVE" {
		return nil, apierr.Unauthorized(apierr.CodeAccountInactive, "This organization is not active.")
	}

	authz, err := s.q.GetUserAuthorization(ctx, session.UserID)
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("load authorization: %w", err))
	}

	snap = sessionSnapshot{
		UserID:                user.ID,
		UserPublicID:          user.PublicID,
		Email:                 user.Email,
		FullName:              user.FullName,
		OrganizationID:        user.OrganizationID,
		OrganizationPublicID:  user.OrganizationPublicID,
		OrganizationCode:      user.OrganizationCode,
		OrganizationCurrency:  user.OrganizationCurrency,
		OrganizationTimezone:  user.OrganizationTimezone,
		OrganizationCountry:   user.OrganizationCountry,
		OrganizationAWBPrefix: user.OrganizationAwbPrefix,
		IsPlatformOrg:         user.OrganizationIsPlatform,
		IsSuperAdmin:          user.IsSuperAdmin,
		SessionID:             session.ID,
		SessionChainID:        session.ChainID,
		Permissions:           authz.PermissionCodes,
		Roles:                 authz.RoleCodes,
		ScopedUnitIDs:         authz.ScopedUnitIds,
		ScopedUnitPublicIDs:   authz.ScopedUnitPublicIds,
		HasUnscopedRole:       authz.HasUnscopedRole,
	}

	// Portal principals are additionally restricted to the customers they are
	// linked to; that list is part of the snapshot so object-level checks stay
	// a pure in-memory comparison.
	for _, role := range snap.Roles {
		if role == roleCustomer {
			snap.IsPortalUser = true
			break
		}
	}
	if snap.IsPortalUser {
		ids, cErr := s.q.ListCustomerIDsForUser(ctx, user.ID)
		if cErr != nil {
			return nil, apierr.Internal(fmt.Errorf("load customer links: %w", cErr))
		}
		snap.CustomerIDs = ids
	}

	s.cache.SetJSON(ctx, cacheKey, snap, s.cfg.SessionCacheTTL)

	// last_used_at is informational; a failure must not fail the request.
	if err := s.q.TouchSession(ctx, session.ID); err != nil {
		s.log.Debug("failed to touch session", slog.String("error", err.Error()))
	}
	return &snap, nil
}

// applyOrgContext switches a platform operator into a tenant.
func (s *Service) applyOrgContext(ctx context.Context, p *tenant.Principal, orgPublicID string) error {
	if !p.IsSuperAdmin {
		return apierr.Forbidden("Only platform operators may select an organization context.")
	}
	if !publicid.Valid(publicid.PrefixOrganization, orgPublicID) {
		return apierr.Validation("The organization context header is not a valid organization identifier.", nil)
	}
	if orgPublicID == p.OrganizationPublicID {
		return nil
	}
	org, err := s.q.GetOrganizationByPublicID(ctx, orgPublicID)
	if err != nil {
		if database.IsNoRows(err) {
			return apierr.NotFound("Organization")
		}
		return apierr.Internal(fmt.Errorf("load organization context: %w", err))
	}
	if org.Status != "ACTIVE" {
		return apierr.Forbidden("The selected organization is not active.")
	}
	p.OrganizationID = org.ID
	p.OrganizationPublicID = org.PublicID
	p.OrganizationCode = org.Code
	p.OrganizationCurrency = org.Currency
	p.OrganizationTimezone = org.Timezone
	p.OrganizationCountry = org.Country
	p.OrganizationAWBPrefix = org.AwbPrefix
	p.IsPlatformOrg = org.IsPlatform
	p.ImpersonatedOrg = true
	return nil
}

func (snap *sessionSnapshot) toPrincipal(sessionPublicID string) *tenant.Principal {
	return tenant.NewPrincipal(tenant.Principal{
		UserID:                snap.UserID,
		UserPublicID:          snap.UserPublicID,
		Email:                 snap.Email,
		FullName:              snap.FullName,
		OrganizationID:        snap.OrganizationID,
		OrganizationPublicID:  snap.OrganizationPublicID,
		OrganizationCode:      snap.OrganizationCode,
		OrganizationCurrency:  snap.OrganizationCurrency,
		OrganizationTimezone:  snap.OrganizationTimezone,
		OrganizationCountry:   snap.OrganizationCountry,
		OrganizationAWBPrefix: snap.OrganizationAWBPrefix,
		IsPlatformOrg:         snap.IsPlatformOrg,
		IsSuperAdmin:          snap.IsSuperAdmin,
		SessionID:             snap.SessionID,
		SessionPublicID:       sessionPublicID,
		SessionChainID:        snap.SessionChainID,
		Roles:                 snap.Roles,
		ScopedUnitIDs:         snap.ScopedUnitIDs,
		ScopedUnitPublicIDs:   snap.ScopedUnitPublicIDs,
		HasUnscopedRole:       snap.HasUnscopedRole,
		CustomerIDs:           snap.CustomerIDs,
		IsPortalUser:          snap.IsPortalUser,
	}, snap.Permissions)
}

func (s *Service) sessionCacheKey(sessionPublicID string) string {
	return s.cache.Key("session", sessionPublicID)
}

func (s *Service) invalidateSessionCache(ctx context.Context, sessionPublicID string) {
	if sessionPublicID == "" {
		return
	}
	s.cache.Delete(ctx, s.sessionCacheKey(sessionPublicID))
}

// invalidateChainCache drops every cached session in one rotation chain, so a
// revoked login stops authenticating immediately rather than after the TTL.
func (s *Service) invalidateChainCache(ctx context.Context, chainID string) {
	ids, err := s.q.ListSessionPublicIDsByChain(ctx, chainID)
	if err != nil {
		s.log.Warn("failed to enumerate session chain for cache invalidation",
			slog.String("error", err.Error()))
		return
	}
	s.dropSessionCache(ctx, ids)
}

// invalidateUserSessionCache drops every cached session for a user. Called on
// password change, deactivation and role changes so authorization changes take
// effect immediately rather than after the cache TTL.
func (s *Service) invalidateUserSessionCache(ctx context.Context, userID int64) {
	ids, err := s.q.ListSessionPublicIDsForUser(ctx, userID)
	if err != nil {
		// Fall back to letting entries expire naturally; the TTL bounds the
		// staleness to SESSION_CACHE_TTL.
		s.log.Warn("failed to enumerate sessions for cache invalidation",
			slog.String("error", err.Error()))
		return
	}
	s.dropSessionCache(ctx, ids)
}

func (s *Service) dropSessionCache(ctx context.Context, sessionPublicIDs []string) {
	if len(sessionPublicIDs) == 0 {
		return
	}
	keys := make([]string, 0, len(sessionPublicIDs))
	for _, id := range sessionPublicIDs {
		keys = append(keys, s.sessionCacheKey(id))
	}
	s.cache.Delete(ctx, keys...)
}

// InvalidateUserCache is the exported hook other modules use after changing a
// user's roles or status.
func (s *Service) InvalidateUserCache(ctx context.Context, userID int64) {
	s.invalidateUserSessionCache(ctx, userID)
}

func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", apierr.Unauthorized(apierr.CodeUnauthorized, "Authentication is required.")
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", apierr.Unauthorized(apierr.CodeTokenInvalid,
			"The Authorization header must use the Bearer scheme.")
	}
	return strings.TrimSpace(token), nil
}
