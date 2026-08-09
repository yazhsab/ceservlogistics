# Security Review — M33

Assessment date: 9 August 2026
Scope: the Go backend in this repository — 304 documented API operations across
33 migrations. The frontend, the VPS host and the Nginx TLS termination are
reviewed only where the backend depends on them.

Method: code review of every authenticated surface, plus adversarial tests
executed against the real router. **Findings are only recorded here as closed
when a test proves it**, because a review that asserts safety without executing
anything is a document, not evidence.

---

## 1. Summary

| Severity | Found | Fixed | Risk accepted |
|---|---|---|---|
| Critical | 0 | — | 0 |
| High | 0 | — | 0 |
| Medium | 4 | 4 | 0 |
| Low | 2 | 1 | 1 |

No Critical or High finding was identified, and nothing Medium or above is left
open.

There were **no authentication, authorization or tenant-isolation defects in the
application code**. That part of the system holds up, and the executed tests in
§3 are the reason to believe it rather than take my word. The four Medium
findings are two dependency issues, one missing test on security-critical code,
and two logic bugs in code paths that are adjacent to security rather than in
the authorization core.

One process finding sits above all of them and is recorded separately in §2.0.

---

## 2. Findings

### 2.0 — CI has never run · **PROCESS**

The repository has **no commits**. Every quality gate described in
`.github/workflows/ci.yml` — including an existing `security` job that runs
`govulncheck` — has therefore never executed against this code.

This is not a vulnerability, but it changes how every other statement in this
document should be read: **the guarantees in CI are designed, not demonstrated.**
Everything reported below was executed locally by hand for this review. The
first push will be the first time the pipeline has ever run, and it should be
treated as a real gate rather than a formality.

### M-1 — Two shipping dependency vulnerabilities, and unpinned toolchains · **FIXED**

`govulncheck` reported 24 reachable vulnerabilities. Being precise about which
of those were actually *shipping* matters, because the raw count overstates it:

- **22 were Go standard library** on the local toolchain, 1.25.4. Both the
  Dockerfile (`golang:1.25-alpine`) and CI (`GO_VERSION: "1.25"`) used *floating*
  tags that resolve to the newest 1.25.x, so an image built today would have
  picked up a patched toolchain regardless. The local development environment
  was stale; the build pipeline was not. **Real exposure here was low.**
- **2 were modules pinned at vulnerable versions in `go.mod`** —
  `google.golang.org/grpc@v1.73.0` (GO-2026-6061) and
  `otlptracehttp@v1.37.0` (GO-2026-4985). A pin ships exactly what it says, so
  **these two would have gone to production.** This is the substance of the
  finding.

**Fixed** by upgrading both modules, and by pinning the toolchain to `go1.25.12`
in all three places so it cannot drift: `go.mod` (`toolchain`), `Dockerfile`
(`golang:1.25.12-alpine`) and `.github/workflows/ci.yml` (`GO_VERSION`). The
Alpine runtime base moved from 3.20 to 3.22.

The pin is worth doing on its own terms even though the floating tags happened
to be safe: a floating base tag means **the build is not reproducible and you
cannot say what you shipped.** "It probably picked up the patched one" is not a
sentence that belongs in an incident review.

Evidence:

```
before:  Your code is affected by 24 vulnerabilities from 2 modules and the Go standard library.
after:   No vulnerabilities found.
```

Residual: 4 vulnerabilities remain in imported packages and 2 in required
modules, none reachable from any code path this repository executes (§6).

### M-2 — `X-Forwarded-For` handling had no test · **FIXED**

The implementation was correct — `clientIP` trusts the header only when the
immediate peer is inside `TRUSTED_PROXY_CIDRS`, and takes the right-most
non-trusted entry — but `internal/platform/httpx` had **no test file at all**,
and this function is what rate limiting and login throttling budget against. A
one-line regression turns both controls into decoration.

**Fixed** by adding `internal/platform/httpx/clientip_test.go`: 11 cases
covering a forged header from a direct connection, a forged chain, a client
forging a loopback address to look trusted, multiple trusted hops, garbage
entries, no configured proxies, and a malformed peer address.

### M-3 — Zone-mapping import resolved postcodes against the wrong country · **FIXED**

`applyZoneMappingRow` received the import job's resolved country and then
ignored it, looking postcodes up by `geography.DefaultCountry` instead. Harmless
while the default was `IN` and India was the only market. Once the default
became `NG` it became a cross-tenant data-integrity bug: **India and Nigeria
both use six-digit postcodes with a non-zero lead**, so the same digits name a
real place in either, and an Indian tenant's import would map its zones onto
Nigerian postcodes rather than failing.

**Fixed** with `GetPincodeIDByCodeInCountry`, keyed on the resolved country id.

### M-4 — Franchise agreement currency was a hardcoded allowlist · **FIXED**

`{INR, USD}` as a literal, which both excluded NGN entirely and could drift from
what the ledger accepts. **Fixed** to default to the organization's currency and
validate against `money.SupportedCodes()` — the same list the ledger enforces.

### L-1 — Two configuration keys were undocumented · **FIXED**

`PARTNER_RATE_LIMIT_PER_MINUTE` and `PUBLIC_TRACKING_URL` were absent from
`.env.example`. An undocumented key with a permissive default is how a control
gets silently left off. **Fixed**; `.env.example` now covers 97 of 99 keys, the
remaining two being `GIT_COMMIT` and `BUILT_AT`, which are Docker build args.

### L-2 — Validation errors echo the rejected input · **RISK ACCEPTED**

A `422` quotes the value it rejected, e.g.
`"value": "'; SELECT PG_SLEEP(5); --"`. This is deliberate: a form cannot show a
user what was wrong without naming it.

It is not an injection or XSS vector — the response is `application/json` with
`X-Content-Type-Options: nosniff` and a `default-src 'none'` CSP, and Go's JSON
encoder escapes the payload. The residual risk is a client that renders an error
value as raw HTML.

**Accepted.** Mitigation is on the consumer: `docs/contracts/release-1.md` §3
already states that error `details` are untrusted input. Removing the echo would
make validation errors unusable to fix a real defect that does not exist in the
first-party client.

*(This was also the one false positive in the first test run: my own leak
detector matched `pg_` inside the echoed payload. The test now strips the echo
before scanning, which is the correct assertion — a validation error quoting the
attacker's own string is not the server leaking anything.)*

---

## 3. What was tested, and the result

All tests run against the real chi router over HTTP, not against handlers in
isolation. `tests/integration/security_attack_test.go` is the adversarial file
added for this review; the rest predate it.

### Injection

| Attack | Result |
|---|---|
| SQL injection across 7 searchable surfaces × 8 payloads (56 probes) | **PASS** — every payload treated as data. No 5xx, no `syntax error`, no `sqlstate`, no relation name in any response |
| Injection stored in a body field | **PASS** — `Robert'); DROP TABLE shipment_events; --` stored verbatim and returned verbatim; `shipment_events` intact afterwards |

Surfaces covered: place search, postcode prefix, district state, shipment
search, shipment status filter, customer search, audit action filter.

The structural reason this holds is that every query is generated by sqlc and
executed through pgx with bind parameters. There is no string-built SQL in the
repository — that is a property of the architecture, and the tests confirm it
rather than establish it.

### Reflection and browser-facing headers

| Attack | Result |
|---|---|
| `<script>`, `<img onerror>`, `javascript:`, `<svg/onload>` reflected in search | **PASS** — `application/json` with `nosniff`; JSON encoder escapes the payload |
| Security headers on 200, 401, 404 and public tracking | **PASS** — `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `default-src 'none'` CSP on all four |

Error responses matter as much as success responses here, which is why the test
covers 401 and 404 explicitly.

### CORS

| Attack | Result |
|---|---|
| Reflection of `https://evil.example.com`, `null`, `localhost.evil.example.com`, `app.example.com.evil.net` | **PASS** — no `Access-Control-Allow-Origin` for any unlisted origin |
| Credentialed access to an unlisted origin | **PASS** — never granted |

The allowlist is exact-match after lower-casing and trailing-slash trimming;
there is no prefix or suffix matching, which is what defeats
`app.example.com.evil.net`. `CORS_ALLOWED_ORIGINS` containing `*` is a **startup
error** in staging and production, and `*` combined with
`CORS_ALLOW_CREDENTIALS` is a startup error in every environment.

### CSRF

**Not applicable, by design.** The API has no cookie authentication: there is no
`http.Cookie`, `SetCookie` or `r.Cookie(` anywhere in `internal/`. Credentials
are a `Authorization: Bearer` header or an API key, neither of which a browser
attaches automatically to a cross-site request. Adding cookie auth later would
reintroduce CSRF as a live concern.

### Path traversal

| Attack | Result |
|---|---|
| `../`, percent-encoded, doubled-up and prefix-disguised traversal on 3 route families | **PASS** — 400/404, no filesystem detail in any response |

Two independent defences: public identifiers are validated against their prefix
and ULID shape before any lookup, and `FSStore.pathFor` rejects any key
containing `..` outright and then confirms with `filepath.Rel` that the result
stays under the root. The S3 driver never touches a local path.

### Uploads

| Attack | Result |
|---|---|
| HTML, shell script, PHP, ELF and script-bearing SVG each declaring `image/jpeg` or `image/png` | **PASS** — all refused |
| A genuine PNG | **PASS** — accepted, so the refusals above are about content, not a blanket deny |

Classification is by `http.DetectContentType` on the leading 512 bytes against
an allowlist, never by filename or by the client's declared `Content-Type`.
Object keys are generated server-side, so a filename never reaches the
filesystem or the bucket. Size is bounded before the read.

### Authentication, tokens and sessions

| Attack | Result |
|---|---|
| Replaced signature | **PASS** — 401 |
| Empty signature (`alg: none` shape) | **PASS** — 401 |
| Tenant A's token against tenant B's facility | **PASS** — 404 |
| `X-Organization-Context` pivot by a non-super-admin | **PASS** — 403 |
| Refresh reuse after rotation | **PASS** — detected, chain revoked *(pre-existing test)* |
| Access token after logout | **PASS** — immediate revocation *(pre-existing)* |
| Login does not reveal whether an account exists | **PASS** *(pre-existing)* |
| Password reset is single-use and expires | **PASS** *(pre-existing)* |
| Deactivated user loses access immediately | **PASS** *(pre-existing)* |

### Brute force and rate limiting

| Attack | Result |
|---|---|
| 40 failed logins from ever-changing forged client IPs | **PASS** — throttled at attempt 11 |
| Forged `X-Forwarded-For` from an untrusted peer | **PASS** — header ignored, peer address used |
| Partner rate limit is per key, not per address | **PASS** *(pre-existing)* |

Worth being precise about why the first one holds, because the naive reading is
wrong. In the harness the peer is loopback, which *is* a trusted proxy, so the
forged header **is** honoured and each request genuinely gets a fresh IP bucket.
What stops the run is that `checkLoginThrottle` budgets **per account as well as
per IP**. That is the control that matters: an attacker with a botnet has as
many source addresses as it wants, so an IP-only budget protects nothing.

The other half — that an untrusted peer cannot forge the header at all — is
proven by the unit tests in `internal/platform/httpx`, because it cannot be
exercised through a harness whose peer is always loopback.

Throttling runs **before** the password hash is computed, so a brute-force
attempt cannot use Argon2id as a CPU amplifier against a 4-vCPU host.

### Tenancy and authorization

Pre-existing and re-run for this review:

| Property | Result |
|---|---|
| Cross-tenant read/write on every resource family | **PASS** |
| Booking rejects a foreign customer id | **PASS** |
| Mass assignment of server-owned fields | **PASS** — rejected |
| Operating-unit scope limits visibility | **PASS** |
| Custody violations refused | **PASS** |
| Privilege escalation via role assignment | **PASS** |
| System roles immutable | **PASS** |
| Last administrator cannot be removed | **PASS** |
| Field-agent permissions are narrow | **PASS** |
| Partner key cannot see another tenant | **PASS** |
| Partner key cannot reach the internal API | **PASS** |
| A user session cannot use the partner surface | **PASS** |

The two credential systems are deliberately disjoint: a partner principal
carries scopes and **no permissions**, so a partner integration cannot acquire a
person's rights by holding a credential.

### Webhooks

| Property | Result |
|---|---|
| Endpoint must be HTTPS | **PASS** |
| Signature is HMAC-SHA256 over `timestamp.body` with a `v1=` prefix | **PASS** — verified by a consumer implementation in the test |
| Replay window bounded by `SignatureTolerance()` | **PASS** |
| Deliveries are tenant-scoped | **PASS** |
| Unknown event type rejected at subscription | **PASS** |
| Replay is a new delivery, not a counter reset | **PASS** |

The signing secret is in the log redaction list, so it cannot reach a log line.

### Financial integrity

| Property | Result |
|---|---|
| Idempotency key reused with a different body | **PASS** — the altered body never takes effect and exactly one shipment exists |
| Booking replays on retry rather than double-booking | **PASS** |
| Double-entry invariant `SUM(debits) = SUM(credits)` | **PASS** *(pre-existing, enforced by CHECK and by test)* |
| Posted journals immutable | **PASS** |
| Concurrent delivery completion | **PASS** |

### Error hygiene

| Property | Result |
|---|---|
| 6 malformed-request probes produce no 5xx | **PASS** |
| No `goroutine`, `.go:`, `panic:`, `sqlstate`, `pgx`, `relation "` in any error | **PASS** |
| Every 4xx carries a `requestId` | **PASS** |

---

## 4. Secrets and logging

**No literal secrets in non-test code.** A pattern scan for assigned
password/secret/token/key literals of 12+ characters returns nothing outside
tests.

Every secret is read from the environment at startup and validated there.
Production refuses to start when: `JWT_SECRET` is short or default,
`STORAGE_DRIVER` is not `s3`, `ENABLE_DEBUG_ROUTES` is true,
`RATE_LIMIT_ENABLED` is false, or `CORS_ALLOWED_ORIGINS` contains `*`. **A
misconfiguration is a crash at boot, not a quiet weakening in production.**

Logging redacts by key, so a sensitive value cannot be logged by accident even
by new code: `authorization`, `password`, `token`, `refresh_token`, `secret`,
`api_key`, `otp`, `idempotency-key`, `proxy-authorization`,
`webhook_signing_secret`.

The request logger records method, path, route pattern, status, bytes,
duration, request id and client IP. It does **not** record the query string,
request bodies or response bodies, so a search term or an address never lands in
a log file.

Two things worth noting for whoever operates this:

- **`path` is logged, `query` is not.** Public identifiers are opaque, so a path
  carries no personal data. Do not add query logging without revisiting this.
- **Personal data is deliberately absent from request logs.** `LogAttrs` returns
  organization, user and session public ids only — never email or name.

---

## 5. Structural properties this review relies on

Stated explicitly because they are what makes the test results generalise beyond
the specific endpoints probed:

1. **All SQL is generated and parameterised.** sqlc + pgx, no string
   concatenation. CI regenerates `internal/dbgen` and fails on any diff, so a
   hand-edited query cannot slip in.
2. **Tenant comes from the principal, never the request.** The organization id
   is a server-side value on `tenant.Principal`; client-supplied
   `organization_id` is never read for authorization.
3. **One transition engine.** Every shipment state change validates actor, role,
   tenant, custody, unit and legality in one place, so an operational endpoint
   cannot accidentally bypass custody rules.
4. **Public identifiers are opaque.** ULID-based with a type prefix; internal
   bigint keys are never exposed, so enumeration is not available.
5. **Money is integer minor units, and posted journals are immutable.**
   Corrections are reversals, enforced by trigger.

---

## 6. Residual risk

| Risk | Severity | Why accepted |
|---|---|---|
| 4 vulnerabilities in imported packages, 2 in required modules, none reachable | Low | `govulncheck` symbol analysis shows no call path. Re-checked on every dependency change; CI runs the scan. |
| Validation errors echo rejected input (L-2) | Low | JSON-only responses with `nosniff` and a deny-all CSP. Consumer-side concern, documented in the frontend contract. |
| TLS terminates at Nginx, outside this repository | Medium | Configuration is in `deploy/nginx/`, verified in `docs/PRODUCTION_READINESS.md`. Certificate expiry is an operational control, not a code one — runbook covers it. |
| Host-level hardening (SSH, firewall, kernel) is out of scope | Medium | Not this repository's responsibility. `docs/DISASTER_RECOVERY.md` names it as an operator precondition. |
| No third-party penetration test | Medium | This is a self-assessment by the engineer who wrote the code, which is a real limitation: I am poorly placed to find what I failed to imagine. An independent test is recommended before handling significant COD volume. |

The last one deserves emphasis rather than a table row. **Everything above is a
self-assessment.** It is evidence-backed and the tests are real, but a review
performed by the author of the code shares the author's blind spots. It should
raise confidence, not settle the question.

---

## 7. Re-running this review

```bash
govulncheck ./...
go test ./internal/platform/httpx/ -count=1
go test ./tests/integration/ -run 'TestSQLInjection|TestInjection|TestScriptPayloads|TestSecurityHeaders|TestCORS|TestPathTraversal|TestUploadMIME|TestRateLimit|TestTokens|TestIdempotencyKey|TestErrorsNever' -count=1
go test ./tests/integration/ -run 'TestCrossTenant|TestOperational|TestPermissions|TestPrivilege|TestPartner' -count=1
```

Any failure is a finding. Re-run on every dependency bump and before each
release.
