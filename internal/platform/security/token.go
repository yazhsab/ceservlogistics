package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// OpaqueTokenBytes is the entropy of refresh and password-reset tokens.
// 32 bytes (256 bits) is far beyond brute-force reach and matches the SHA-256
// digest width used for storage.
const OpaqueTokenBytes = 32

// NewOpaqueToken returns a URL-safe random token and its storage digest.
//
// Only the digest is persisted. A database disclosure therefore does not yield
// usable refresh or reset tokens. SHA-256 (not a password KDF) is correct here
// because the input already carries full entropy — there is nothing to
// brute-force — and verification must stay cheap enough to run on every refresh.
func NewOpaqueToken() (token string, digest []byte, err error) {
	raw := make([]byte, OpaqueTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("security: read token entropy: %w", err)
	}
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:], nil
}

// HashToken returns the storage digest for a presented opaque token.
func HashToken(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != OpaqueTokenBytes {
		return nil, fmt.Errorf("security: malformed token")
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

// ConstantTimeEqual compares two digests without leaking timing information.
func ConstantTimeEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// RandomString returns n bytes of entropy as a URL-safe string. Used for
// generated codes and test fixtures, never for password material.
func RandomString(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("security: read entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
