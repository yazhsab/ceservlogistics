package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ceserve/courier-os/internal/platform/apierr"
)

// Claims is the access-token payload.
//
// The access token is a short-lived bearer credential (15 minutes by default).
// It carries the session public id so the server can check revocation, and the
// organization public id purely for logging and cache keying — authorization
// always re-reads the tenant from the database or the session cache, never from
// the token (Constitution §9).
type Claims struct {
	jwt.RegisteredClaims
	SessionID      string `json:"sid"`
	OrganizationID string `json:"org"`
	TokenType      string `json:"typ"`
}

const (
	tokenTypeAccess = "access"
	tokenAudience   = "courier-os-api"
)

// TokenIssuer mints and verifies access tokens.
type TokenIssuer struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewTokenIssuer builds a TokenIssuer.
func NewTokenIssuer(secret []byte, issuer string, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: secret, issuer: issuer, ttl: ttl}
}

// TTL returns the access-token lifetime.
func (t *TokenIssuer) TTL() time.Duration { return t.ttl }

// Issue mints an access token for a session.
func (t *TokenIssuer) Issue(userPublicID, sessionPublicID, orgPublicID string, now time.Time) (string, time.Time, error) {
	expires := now.Add(t.ttl)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    t.issuer,
			Subject:   userPublicID,
			Audience:  jwt.ClaimStrings{tokenAudience},
			ExpiresAt: jwt.NewNumericDate(expires),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ID:        sessionPublicID + ":" + fmt.Sprint(now.UnixNano()),
		},
		SessionID:      sessionPublicID,
		OrganizationID: orgPublicID,
		TokenType:      tokenTypeAccess,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expires, nil
}

// Verify parses and validates an access token.
//
// The signing method is pinned to HS256: without that check a caller could
// present an "alg":"none" token, or an RS256 token whose "public key" is our
// HMAC secret, and be trusted.
func (t *TokenIssuer) Verify(raw string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", token.Header["alg"])
		}
		return t.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(t.issuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, apierr.Unauthorized(apierr.CodeTokenExpired, "The access token has expired.")
		default:
			return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The access token is not valid.")
		}
	}
	if claims.TokenType != tokenTypeAccess {
		return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The access token is not valid.")
	}
	if claims.SessionID == "" || claims.Subject == "" {
		return nil, apierr.Unauthorized(apierr.CodeTokenInvalid, "The access token is not valid.")
	}
	return &claims, nil
}
