// Package security holds credential hashing, opaque token handling and request
// rate limiting.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Defaults (t=3, m=64 MiB, p=2) target roughly 50-70 ms per
// hash on the 4 vCPU production box: slow enough to make offline cracking
// expensive, fast enough that a login burst does not exhaust CPU. They are
// configurable so the cost can be raised as hardware improves, and the cost is
// encoded in every hash so existing credentials keep verifying after a change.
type Argon2Params struct {
	Time        uint32
	MemoryKiB   uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params returns the recommended parameters.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{Time: 3, MemoryKiB: 64 * 1024, Parallelism: 2, SaltLength: 16, KeyLength: 32}
}

// Hasher hashes and verifies passwords.
type Hasher struct{ params Argon2Params }

// NewHasher builds a Hasher, falling back to safe defaults for zero values.
func NewHasher(p Argon2Params) *Hasher {
	d := DefaultArgon2Params()
	if p.Time == 0 {
		p.Time = d.Time
	}
	if p.MemoryKiB == 0 {
		p.MemoryKiB = d.MemoryKiB
	}
	if p.Parallelism == 0 {
		p.Parallelism = d.Parallelism
	}
	if p.SaltLength == 0 {
		p.SaltLength = d.SaltLength
	}
	if p.KeyLength == 0 {
		p.KeyLength = d.KeyLength
	}
	return &Hasher{params: p}
}

// ErrInvalidHash indicates a stored hash that cannot be parsed.
var ErrInvalidHash = errors.New("security: invalid password hash encoding")

// ErrMismatch indicates the supplied password does not match.
var ErrMismatch = errors.New("security: password mismatch")

// Hash produces a PHC-formatted Argon2id hash:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt-b64>$<hash-b64>
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("security: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify checks a password against an encoded hash in constant time.
// It also reports whether the stored hash uses outdated parameters, so callers
// can transparently re-hash on successful login.
func (h *Hasher) Verify(encoded, password string) (needsRehash bool, err error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, ErrMismatch
	}
	outdated := p.Time < h.params.Time || p.MemoryKiB < h.params.MemoryKiB ||
		p.Parallelism != h.params.Parallelism || uint32(len(want)) < h.params.KeyLength
	return outdated, nil
}

// DummyVerify performs a hash computation against a fixed encoded hash.
//
// Login handlers call this when no user matches, so that a request for an
// unknown account costs the same as one for a known account. Without it, the
// response-time difference enumerates valid usernames.
func (h *Hasher) DummyVerify(password string) {
	_, _ = h.Verify(dummyHash, password)
}

// dummyHash is a well-formed Argon2id record carrying the default cost
// parameters. Verifying against it performs the same key derivation work as a
// real credential check. No password produces this digest, so it can never
// authenticate anyone.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$Y291cmllcm9zZHVtbXlzYWx0$Zm9yY29uc3RhbnR0aW1lbG9naW5jb21wYXJpc29u"

func decodeHash(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	var p Argon2Params
	kv := strings.Split(parts[3], ",")
	if len(kv) != 3 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	for _, item := range kv {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return Argon2Params{}, nil, nil, ErrInvalidHash
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return Argon2Params{}, nil, nil, ErrInvalidHash
		}
		switch k {
		case "m":
			p.MemoryKiB = uint32(n)
		case "t":
			p.Time = uint32(n)
		case "p":
			if n > 255 {
				return Argon2Params{}, nil, nil, ErrInvalidHash
			}
			p.Parallelism = uint8(n)
		default:
			return Argon2Params{}, nil, nil, ErrInvalidHash
		}
	}
	if p.Time == 0 || p.MemoryKiB == 0 || p.Parallelism == 0 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
