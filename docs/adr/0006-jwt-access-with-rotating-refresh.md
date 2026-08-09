# ADR 0006 — Short JWT access tokens with rotating opaque refresh tokens

**Status:** accepted · Release 1

## Context

The Constitution requires secure authentication with session expiry, refresh
rotation, logout and revocation, plus audit of security events. The platform
runs on constrained hardware where a database round trip per request matters.

## Decision

- **Access token**: JWT, HS256, 15-minute lifetime, carrying the session public
  id. Verified by signature; the signing method is pinned.
- **Refresh token**: 256 bits of opaque entropy, stored only as a SHA-256
  digest, rotated on every use, with reuse detection that revokes the entire
  chain.
- **Revocation**: the session record in PostgreSQL is authoritative. The
  resolved session (identity, permissions, scope) is cached in Redis for 30
  seconds and deleted eagerly on logout, password change, deactivation and role
  change.

## Why

- **A pure JWT cannot be revoked**, which fails the Constitution's requirement.
  A pure database session costs several queries per request. The cache with
  eager invalidation gives revocation that is immediate in practice and bounded
  at 30 seconds in the worst case (a Redis outage), while removing four queries
  from the hot path.
- **Rotation with reuse detection** turns a stolen refresh token from a
  long-lived credential into a detectable, self-limiting event: the moment
  either party uses the old token, the whole chain dies and the incident is
  audited.
- **Only the digest is stored**, so a database disclosure yields no usable
  tokens. SHA-256 rather than a password KDF is correct because the input
  already carries full entropy — there is nothing to brute-force — and
  verification must stay cheap.

## Consequences

- Losing Redis degrades to a database lookup per request, not to a security
  failure. The pool is sized for that.
- Clients must serialise refresh calls. Two tabs refreshing concurrently will
  trip reuse detection and sign the user out — this is documented prominently in
  the frontend contract.
- HS256 requires the secret on every verifying instance. If verification ever
  needs to happen outside this deployment, move to RS256; the issuer is
  isolated in one file.
