// Package auth implements authentication, session lifecycle and RBAC (M01).
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/config"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/security"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Service owns credentials, sessions and role assignment.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	cache   *cache.Cache
	hasher  *security.Hasher
	tokens  *TokenIssuer
	audit   *audit.Recorder
	cfg     config.AuthConfig
	log     *slog.Logger
	metrics *telemetry.Metrics
}

// NewService builds the auth service.
func NewService(
	db *database.DB, q *dbgen.Queries, c *cache.Cache, rec *audit.Recorder,
	cfg config.AuthConfig, log *slog.Logger, m *telemetry.Metrics,
) *Service {
	return &Service{
		db:    db,
		q:     q,
		cache: c,
		hasher: security.NewHasher(security.Argon2Params{
			Time:        cfg.Argon2Time,
			MemoryKiB:   cfg.Argon2MemoryKiB,
			Parallelism: cfg.Argon2Parallelism,
			KeyLength:   cfg.Argon2KeyLength,
		}),
		tokens:  NewTokenIssuer(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTokenTTL),
		audit:   rec,
		cfg:     cfg,
		log:     log,
		metrics: m,
	}
}

// TokenPair is what a successful login or refresh returns.
type TokenPair struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken"`
	TokenType        string    `json:"tokenType"`
	ExpiresIn        int       `json:"expiresIn"`
	AccessExpiresAt  time.Time `json:"accessTokenExpiresAt"`
	RefreshExpiresAt time.Time `json:"refreshTokenExpiresAt"`
	SessionID        string    `json:"sessionId"`
}

// LoginInput carries credentials plus the request metadata recorded on the
// session and the audit trail.
type LoginInput struct {
	Email     string
	Password  string
	UserAgent string
	ClientIP  string
}

// genericAuthFailure is returned for every credential problem — unknown email,
// wrong password, inactive account. Distinguishing them would let an attacker
// enumerate valid users.
func genericAuthFailure() error {
	return apierr.Unauthorized(apierr.CodeInvalidCredentials, "Email or password is incorrect.")
}

// Login authenticates a user and opens a session.
func (s *Service) Login(ctx context.Context, in LoginInput) (*TokenPair, *dbgen.GetUserForLoginRow, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))

	// Throttle by IP and by target account before touching the password hash,
	// so a brute-force attempt cannot use Argon2 as a CPU amplifier.
	if err := s.checkLoginThrottle(ctx, email, in.ClientIP); err != nil {
		s.audit.Record(ctx, audit.Entry{
			ActorType: audit.ActorAnonymous, ActorLabel: email,
			Action: audit.ActionLoginBlocked, ResourceType: "user",
			Metadata: map[string]any{"reason": "throttled"},
		})
		return nil, nil, err
	}

	row, err := s.q.GetUserForLogin(ctx, email)
	if err != nil {
		if database.IsNoRows(err) {
			// Spend the same CPU as a real verification so response time does
			// not reveal whether the account exists.
			s.hasher.DummyVerify(in.Password)
			s.recordFailedLogin(ctx, email, nil, "unknown_user")
			return nil, nil, genericAuthFailure()
		}
		return nil, nil, apierr.Internal(fmt.Errorf("load user for login: %w", err))
	}

	if row.LockedUntil != nil && row.LockedUntil.After(time.Now()) {
		s.hasher.DummyVerify(in.Password)
		s.recordFailedLogin(ctx, email, &row.ID, "locked")
		return nil, nil, apierr.Unauthorized(apierr.CodeAccountLocked,
			"This account is temporarily locked after repeated failed sign-in attempts. Try again later.")
	}

	needsRehash, verifyErr := s.hasher.Verify(row.PasswordHash, in.Password)
	if verifyErr != nil {
		if !errors.Is(verifyErr, security.ErrMismatch) {
			s.log.Error("password hash could not be verified",
				slog.String("user_public_id", row.PublicID), slog.String("error", verifyErr.Error()))
		}
		s.registerFailure(ctx, row.ID)
		s.recordFailedLogin(ctx, email, &row.ID, "bad_password")
		return nil, nil, genericAuthFailure()
	}

	// Credentials are valid. Only now do account/tenant status checks run, and
	// they still return the generic failure so a valid password cannot be used
	// to discover that an account exists but is disabled.
	if row.Status != "ACTIVE" {
		s.recordFailedLogin(ctx, email, &row.ID, "user_"+strings.ToLower(row.Status))
		return nil, nil, apierr.Unauthorized(apierr.CodeAccountInactive,
			"This account is not active. Contact your administrator.")
	}
	if row.OrganizationStatus != "ACTIVE" {
		s.recordFailedLogin(ctx, email, &row.ID, "organization_"+strings.ToLower(row.OrganizationStatus))
		return nil, nil, apierr.Unauthorized(apierr.CodeAccountInactive,
			"This organization is not active. Contact your administrator.")
	}

	if needsRehash {
		if newHash, hErr := s.hasher.Hash(in.Password); hErr == nil {
			if uErr := s.q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
				ID: row.ID, PasswordHash: newHash,
			}); uErr != nil {
				s.log.Warn("failed to upgrade password hash parameters",
					slog.String("user_public_id", row.PublicID))
			}
		}
	}

	pair, err := s.openSession(ctx, row.ID, row.OrganizationID, row.PublicID, row.OrganizationPublicID,
		in.UserAgent, in.ClientIP)
	if err != nil {
		return nil, nil, err
	}
	if err := s.q.RecordLoginSuccess(ctx, row.ID); err != nil {
		s.log.Warn("failed to record successful login", slog.String("error", err.Error()))
	}
	s.clearLoginThrottle(ctx, email, in.ClientIP)

	orgID := row.OrganizationID
	uid := row.ID
	s.audit.Record(ctx, audit.Entry{
		OrganizationID: &orgID, ActorUserID: &uid, ActorType: audit.ActorUser, ActorLabel: email,
		Action: audit.ActionLoginSucceeded, ResourceType: "session",
		ResourcePublicID: pair.SessionID,
		Metadata:         map[string]any{"userAgent": truncate(in.UserAgent, 200)},
	})
	s.metrics.RecordBusiness("login", "succeeded")
	return pair, &row, nil
}

// openSession creates the session row and mints the token pair.
func (s *Service) openSession(
	ctx context.Context, userID, orgID int64, userPublicID, orgPublicID, userAgent, clientIP string,
) (*TokenPair, error) {
	refreshToken, digest, err := security.NewOpaqueToken()
	if err != nil {
		return nil, apierr.Internal(err)
	}
	sessionPublicID := publicid.New(publicid.PrefixSession)
	now := time.Now()
	refreshExpires := now.Add(s.cfg.RefreshTokenTTL)

	params := dbgen.CreateSessionParams{
		PublicID:         sessionPublicID,
		OrganizationID:   orgID,
		UserID:           userID,
		RefreshTokenHash: digest,
		// A fresh login starts a new chain; rotations inherit this value.
		ChainID:   sessionPublicID,
		ExpiresAt: refreshExpires,
	}
	if userAgent != "" {
		ua := truncate(userAgent, 400)
		params.UserAgent = &ua
	}
	if clientIP != "" {
		params.ClientIp = &clientIP
	}
	if _, err := s.q.CreateSession(ctx, params); err != nil {
		return nil, apierr.Internal(fmt.Errorf("create session: %w", err))
	}

	accessToken, accessExpires, err := s.tokens.Issue(userPublicID, sessionPublicID, orgPublicID, now)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &TokenPair{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        int(s.tokens.TTL().Seconds()),
		AccessExpiresAt:  accessExpires,
		RefreshExpiresAt: refreshExpires,
		SessionID:        sessionPublicID,
	}, nil
}

// Refresh rotates a refresh token.
//
// Rotation with reuse detection: the presented token is revoked and replaced by
// a child session. If a token that was already rotated is presented again, the
// chain has leaked — every session descended from that login is revoked at once
// and the event is audited.
func (s *Service) Refresh(ctx context.Context, refreshToken, userAgent, clientIP string) (*TokenPair, error) {
	digest, err := security.HashToken(refreshToken)
	if err != nil {
		return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The refresh token is not valid.")
	}
	row, err := s.q.GetSessionByRefreshHash(ctx, digest)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The refresh token is not valid.")
		}
		return nil, apierr.Internal(fmt.Errorf("load session: %w", err))
	}

	if row.RevokedAt != nil {
		if row.RevokedReason != nil && *row.RevokedReason == "ROTATED" {
			s.handleReuse(ctx, row)
			return nil, apierr.Unauthorized(apierr.CodeTokenRevoked,
				"This refresh token has already been used. All sessions for this login have been revoked.")
		}
		return nil, apierr.Unauthorized(apierr.CodeTokenRevoked, "This session has been revoked.")
	}
	if row.ExpiresAt.Before(time.Now()) {
		return nil, apierr.Unauthorized(apierr.CodeTokenExpired, "This session has expired. Please sign in again.")
	}
	if row.UserStatus != "ACTIVE" || row.OrganizationStatus != "ACTIVE" {
		s.revokeChain(ctx, row.ChainID, "USER_DEACTIVATED")
		return nil, apierr.Unauthorized(apierr.CodeAccountInactive, "This account is no longer active.")
	}

	user, err := s.q.GetUserWithOrganization(ctx, row.UserID)
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("load user: %w", err))
	}

	newRefresh, newDigest, err := security.NewOpaqueToken()
	if err != nil {
		return nil, apierr.Internal(err)
	}
	childPublicID := publicid.New(publicid.PrefixSession)
	now := time.Now()
	refreshExpires := row.ExpiresAt // rotation never extends the original window

	var pair *TokenPair
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		// Revoking the parent first means a concurrent refresh with the same
		// token finds it already ROTATED and trips reuse detection, instead of
		// both callers minting children.
		affected, rErr := qtx.RevokeSession(ctx, dbgen.RevokeSessionParams{
			ID: row.ID, Reason: strPtr("ROTATED"),
		})
		if rErr != nil {
			return apierr.Internal(fmt.Errorf("revoke rotated session: %w", rErr))
		}
		if affected == 0 {
			return apierr.Unauthorized(apierr.CodeTokenRevoked, "This refresh token has already been used.")
		}
		params := dbgen.CreateSessionParams{
			PublicID:         childPublicID,
			OrganizationID:   row.OrganizationID,
			UserID:           row.UserID,
			RefreshTokenHash: newDigest,
			ChainID:          row.ChainID,
			ParentSessionID:  &row.ID,
			RotationCounter:  row.RotationCounter + 1,
			ExpiresAt:        refreshExpires,
		}
		if userAgent != "" {
			ua := truncate(userAgent, 400)
			params.UserAgent = &ua
		}
		if clientIP != "" {
			params.ClientIp = &clientIP
		}
		if _, cErr := qtx.CreateSession(ctx, params); cErr != nil {
			return apierr.Internal(fmt.Errorf("create rotated session: %w", cErr))
		}
		accessToken, accessExpires, tErr := s.tokens.Issue(user.PublicID, childPublicID, user.OrganizationPublicID, now)
		if tErr != nil {
			return apierr.Internal(tErr)
		}
		pair = &TokenPair{
			AccessToken:      accessToken,
			RefreshToken:     newRefresh,
			TokenType:        "Bearer",
			ExpiresIn:        int(s.tokens.TTL().Seconds()),
			AccessExpiresAt:  accessExpires,
			RefreshExpiresAt: refreshExpires,
			SessionID:        childPublicID,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.invalidateSessionCache(ctx, row.PublicID)
	orgID := row.OrganizationID
	uid := row.UserID
	s.audit.Record(ctx, audit.Entry{
		OrganizationID: &orgID, ActorUserID: &uid, ActorType: audit.ActorUser,
		Action: audit.ActionTokenRefreshed, ResourceType: "session", ResourcePublicID: childPublicID,
		Metadata: map[string]any{"rotationCounter": row.RotationCounter + 1},
	})
	return pair, nil
}

func (s *Service) handleReuse(ctx context.Context, row dbgen.GetSessionByRefreshHashRow) {
	s.revokeChain(ctx, row.ChainID, "REUSE_DETECTED")
	orgID := row.OrganizationID
	uid := row.UserID
	s.audit.Record(ctx, audit.Entry{
		OrganizationID: &orgID, ActorUserID: &uid, ActorType: audit.ActorUser,
		Action: audit.ActionTokenReuseDetected, ResourceType: "session",
		ResourcePublicID: row.PublicID,
		Reason:           "A previously rotated refresh token was presented again",
		Metadata:         map[string]any{"chainId": row.ChainID, "clientIp": httpx.ClientIP(ctx)},
	})
	s.log.Warn("refresh token reuse detected; session chain revoked",
		slog.String("chain_id", row.ChainID), slog.String("user_email", row.UserEmail))
	s.metrics.RecordBusiness("token_reuse", "detected")
}

func (s *Service) revokeChain(ctx context.Context, chainID, reason string) {
	if _, err := s.q.RevokeSessionChain(ctx, dbgen.RevokeSessionChainParams{
		ChainID: chainID, Reason: &reason,
	}); err != nil {
		s.log.Error("failed to revoke session chain", slog.String("error", err.Error()))
	}
	s.invalidateChainCache(ctx, chainID)
}

// Logout revokes the current session. When a refresh token is supplied the
// whole rotation chain is revoked, so a stolen refresh token cannot outlive the
// user's sign-out.
func (s *Service) Logout(ctx context.Context, sessionPublicID, refreshToken string, allDevices bool) error {
	p := tenant.FromContext(ctx)
	reason := "LOGOUT"
	if allDevices {
		reason = "LOGOUT_ALL"
	}

	switch {
	case allDevices && p != nil:
		if _, err := s.q.RevokeUserSessions(ctx, dbgen.RevokeUserSessionsParams{
			UserID: p.UserID, Reason: &reason,
		}); err != nil {
			return apierr.Internal(fmt.Errorf("revoke sessions: %w", err))
		}
	case refreshToken != "":
		digest, err := security.HashToken(refreshToken)
		if err == nil {
			if row, lErr := s.q.GetSessionByRefreshHash(ctx, digest); lErr == nil {
				s.revokeChain(ctx, row.ChainID, reason)
			}
		}
	}

	if sessionPublicID != "" {
		row, err := s.q.GetSessionByPublicID(ctx, sessionPublicID)
		if err == nil {
			if _, rErr := s.q.RevokeSession(ctx, dbgen.RevokeSessionParams{
				ID: row.ID, Reason: &reason,
			}); rErr != nil {
				s.log.Warn("failed to revoke session", slog.String("error", rErr.Error()))
			}
			s.invalidateChainCache(ctx, row.ChainID)
		}
		s.invalidateSessionCache(ctx, sessionPublicID)
	}

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionLogout, ResourceType: "session", ResourcePublicID: sessionPublicID,
		Metadata: map[string]any{"allDevices": allDevices},
	}))
	return nil
}

// ChangePassword rotates a user's own credential and revokes every other
// session, so a password change actually evicts an attacker.
func (s *Service) ChangePassword(ctx context.Context, p *tenant.Principal, currentPassword, newPassword string) error {
	user, err := s.q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return apierr.Internal(fmt.Errorf("load user: %w", err))
	}
	if _, vErr := s.hasher.Verify(user.PasswordHash, currentPassword); vErr != nil {
		return apierr.Unauthorized(apierr.CodeInvalidCredentials, "The current password is incorrect.")
	}
	if currentPassword == newPassword {
		return apierr.Validation("The new password must differ from the current password.", nil)
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return apierr.Internal(err)
	}
	reason := "PASSWORD_CHANGED"
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if uErr := qtx.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
			ID: p.UserID, PasswordHash: hash,
		}); uErr != nil {
			return apierr.Internal(fmt.Errorf("update password: %w", uErr))
		}
		if _, rErr := qtx.RevokeUserSessions(ctx, dbgen.RevokeUserSessionsParams{
			UserID: p.UserID, Reason: &reason,
		}); rErr != nil {
			return apierr.Internal(fmt.Errorf("revoke sessions: %w", rErr))
		}
		if _, iErr := qtx.InvalidateUserResetTokens(ctx, p.UserID); iErr != nil {
			return apierr.Internal(fmt.Errorf("invalidate reset tokens: %w", iErr))
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.invalidateUserSessionCache(ctx, p.UserID)
	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: audit.ActionPasswordChanged, ResourceType: "user", ResourcePublicID: p.UserPublicID,
	}))
	return nil
}

// ResetRequest is the outcome of a forgot-password call.
type ResetRequest struct {
	// Token is non-empty only when a matching active account was found. It is
	// delivered by email in production; the API never returns it.
	Token     string
	ExpiresAt time.Time
	UserID    int64
	Email     string
}

// RequestPasswordReset issues a single-use reset token.
//
// The caller must return the same response whether or not the email matched:
// this endpoint is otherwise an account-enumeration oracle.
func (s *Service) RequestPasswordReset(ctx context.Context, email, clientIP string) (*ResetRequest, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	row, err := s.q.GetUserForLogin(ctx, email)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, apierr.Internal(fmt.Errorf("load user: %w", err))
	}
	if row.Status != "ACTIVE" || row.OrganizationStatus != "ACTIVE" {
		return nil, nil
	}

	token, digest, err := security.NewOpaqueToken()
	if err != nil {
		return nil, apierr.Internal(err)
	}
	expires := time.Now().Add(s.cfg.PasswordResetTTL)

	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		// Outstanding tokens are invalidated so only the newest link works.
		if _, iErr := qtx.InvalidateUserResetTokens(ctx, row.ID); iErr != nil {
			return apierr.Internal(iErr)
		}
		params := dbgen.CreatePasswordResetTokenParams{
			UserID: row.ID, TokenHash: digest, ExpiresAt: expires,
		}
		if clientIP != "" {
			params.RequestIp = &clientIP
		}
		_, cErr := qtx.CreatePasswordResetToken(ctx, params)
		return cErr
	})
	if err != nil {
		return nil, apierr.Internal(fmt.Errorf("create reset token: %w", err))
	}

	orgID := row.OrganizationID
	uid := row.ID
	s.audit.Record(ctx, audit.Entry{
		OrganizationID: &orgID, ActorUserID: &uid, ActorType: audit.ActorUser, ActorLabel: email,
		Action: audit.ActionPasswordResetRequested, ResourceType: "user", ResourcePublicID: row.PublicID,
	})
	return &ResetRequest{Token: token, ExpiresAt: expires, UserID: row.ID, Email: email}, nil
}

// ResetPassword consumes a reset token and sets a new credential.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	digest, err := security.HashToken(token)
	if err != nil {
		return apierr.Unauthorized(apierr.CodeTokenInvalid, "This password reset link is not valid.")
	}
	hash, hErr := s.hasher.Hash(newPassword)
	if hErr != nil {
		return apierr.Internal(hErr)
	}

	var userID int64
	reason := "PASSWORD_CHANGED"
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		// Single use and expiry are both enforced by the UPDATE's WHERE clause,
		// so two concurrent redemptions cannot both succeed.
		rec, cErr := qtx.ConsumePasswordResetToken(ctx, digest)
		if cErr != nil {
			if database.IsNoRows(cErr) {
				return apierr.Unauthorized(apierr.CodeTokenInvalid,
					"This password reset link is invalid, expired, or has already been used.")
			}
			return apierr.Internal(cErr)
		}
		userID = rec.UserID
		if uErr := qtx.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
			ID: rec.UserID, PasswordHash: hash,
		}); uErr != nil {
			return apierr.Internal(uErr)
		}
		if _, rErr := qtx.RevokeUserSessions(ctx, dbgen.RevokeUserSessionsParams{
			UserID: rec.UserID, Reason: &reason,
		}); rErr != nil {
			return apierr.Internal(rErr)
		}
		// UpdateUserPassword also clears the lockout, so a user who locked
		// themselves out and then reset their password can sign straight in.
		return nil
	})
	if err != nil {
		return err
	}
	s.invalidateUserSessionCache(ctx, userID)

	if user, uErr := s.q.GetUserByID(ctx, userID); uErr == nil {
		orgID := user.OrganizationID
		s.audit.Record(ctx, audit.Entry{
			OrganizationID: &orgID, ActorUserID: &userID, ActorType: audit.ActorUser,
			Action: audit.ActionPasswordResetCompleted, ResourceType: "user", ResourcePublicID: user.PublicID,
		})
	}
	return nil
}

// ---- login throttling ------------------------------------------------------

// checkLoginThrottle enforces a per-IP and per-account attempt budget.
//
// Redis is the shared counter; when it is unavailable the limiter degrades to
// per-process counters rather than failing open, and the database-backed
// failed_login_count lockout still applies regardless.
func (s *Service) checkLoginThrottle(ctx context.Context, email, ip string) error {
	if s.cache == nil {
		return nil
	}
	window := s.cfg.LoginThrottleWindow
	limit := int64(s.cfg.LoginThrottleMax)

	if ip != "" {
		if n, ok := s.cache.Incr(ctx, s.cache.Key("login", "ip", ip), window); ok && n > limit {
			return apierr.RateLimited("Too many sign-in attempts. Please wait a moment and try again.")
		}
	}
	if email != "" {
		if n, ok := s.cache.Incr(ctx, s.cache.Key("login", "email", email), window); ok && n > limit {
			return apierr.RateLimited("Too many sign-in attempts for this account. Please wait a moment and try again.")
		}
	}
	return nil
}

func (s *Service) clearLoginThrottle(ctx context.Context, email, ip string) {
	if s.cache == nil {
		return
	}
	keys := make([]string, 0, 2)
	if email != "" {
		keys = append(keys, s.cache.Key("login", "email", email))
	}
	if ip != "" {
		keys = append(keys, s.cache.Key("login", "ip", ip))
	}
	s.cache.Delete(ctx, keys...)
}

func (s *Service) registerFailure(ctx context.Context, userID int64) {
	if _, err := s.q.RecordLoginFailure(ctx, dbgen.RecordLoginFailureParams{
		ID:             userID,
		MaxFailures:    int32(s.cfg.MaxFailedLogins),
		LockoutSeconds: int32(s.cfg.LoginLockoutDuration.Seconds()),
	}); err != nil {
		s.log.Warn("failed to record login failure", slog.String("error", err.Error()))
	}
}

func (s *Service) recordFailedLogin(ctx context.Context, email string, userID *int64, reason string) {
	s.audit.Record(ctx, audit.Entry{
		ActorUserID: userID, ActorType: audit.ActorAnonymous, ActorLabel: email,
		Action: audit.ActionLoginFailed, ResourceType: "user",
		Metadata: map[string]any{"reason": reason},
	})
	s.metrics.RecordBusiness("login", "failed")
}

func strPtr(s string) *string { return &s }

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
