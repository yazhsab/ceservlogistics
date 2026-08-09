# Release 1 — Foundation and Booking

**Modules:** M00 Platform · M01 Auth/RBAC · M02 Network · M03 Geography ·
M04 Serviceability/Routing · M05 Products · M06 Pricing · M07 Customers ·
M08 Booking/AWB

**Status:** complete and evidence-backed. Residual risks are listed in §11; per
Constitution §41 this document does not claim "100% production ready".

---

## 1. What works

Verified end to end against a real PostgreSQL and Redis, not mocks:

- Runs locally, under Docker Compose, and as a production image.
- Organization-based multi-tenancy with proven isolation.
- Authentication with Argon2id, rotating refresh tokens and reuse detection.
- RBAC: 54 permissions, 12 system roles, custom roles, operating-unit scope.
- Courier network: regions, hubs, branches, capabilities, franchises,
  agreements, hierarchy queries.
- Geography: global PIN code dataset, tenant zones, async bulk import with a
  per-row error report.
- Deterministic serviceability and routing with a full decision trace.
- Configurable courier products.
- Versioned, immutable-once-active rate cards with a line-by-line price
  breakdown.
- Retail and business customers with addresses, contacts, billing and credit.
- Shipment booking with concurrency-safe AWB allocation, three immutable
  snapshots, an append-only event log, and idempotent retries.
- An OpenAPI 3.1 contract that is validated in CI and asserted against the code.

---

## 2. Files created

| Area | Count | Notes |
|---|---|---|
| Go source (hand-written) | 69 files, 24,694 lines | `cmd/`, `internal/`, `tests/` |
| Go source (generated) | 12 files, 14,435 lines | `internal/dbgen`, from sqlc; CI fails on drift |
| SQL migrations | 12 up + 12 down, 4,756 lines total with queries | `db/migrations/` |
| sqlc queries | 9 files, **261** named queries | `db/queries/` |
| Tests | 67 top-level test functions | unit, integration, contract, perf |
| Documentation | OpenAPI (86 operations, 62 paths), frontend contract, architecture, deployment, 7 ADRs | `docs/` |
| Deployment | Dockerfile, 2 compose files, Nginx, PostgreSQL config, CI workflow | root, `deploy/`, `.github/` |

Layout:

```
cmd/{api,worker,migrate}          three binaries from one module
internal/api                      routing and middleware order
internal/{auth,tenant,network,geography,serviceability,product,
          pricing,customer,shipment,audit}
internal/platform/{config,database,cache,httpx,logging,apierr,telemetry,
                   idempotency,security,jobs,validate,publicid,money,
                   pagination,provision}
internal/dbgen                    generated
db/{migrations,queries,seeds}
docs/{openapi.yaml,contracts,adr,releases}
deploy/{nginx,postgres,docker-compose.prod.yml}
tests/{harness,integration,contract,perf,load}
```

---

## 3. Schema

**56 tables, 268 indexes, 1,238 constraints, 60 triggers.**

| Migration | Contents |
|---|---|
| `0001_platform_functions` | `btree_gist`, `set_updated_at`, `reject_mutation`, `bump_row_version`, `is_public_id`, `gen_seed_public_id` |
| `0002_tenancy_auth` | organizations, users, permissions, roles, role_permissions, user_roles, sessions, password_reset_tokens, audit_events |
| `0003_platform_runtime` | idempotency_keys, jobs |
| `0004_geography` | countries, states, districts, cities, pincodes, localities, zones, pincode_zone_mappings |
| `0005_network` | regions, operating_units, operating_unit_capabilities, franchises, franchise_agreements |
| `0006_products` | courier_services |
| `0007_customer` | customers, business_accounts, contacts, addresses, billing_profiles, credit_profiles, customer_credit_entries, customer_users |
| `0008_serviceability` | service_areas, serviceability_rules, route_definitions, route_legs, routing_overrides, temporary_closures |
| `0009_pricing` | rate_cards, rate_card_versions, zone_rates, weight_slabs, surcharge_rules, discount_rules, tax_rules |
| `0010_imports` | import_jobs, import_rows |
| `0011_shipment` | awb_sequences, shipments, shipment_packages, and the address / charge / route snapshots, shipment_events |
| `0012_rbac_catalog` | 54 permissions, 12 system roles, 237 grants |

Design decisions worth knowing:

- **Money** is `bigint` minor units with an explicit currency; percentages are
  integer basis points. No `numeric`, no float, anywhere in a monetary path.
- **Enums** are `text` with `CHECK` constraints, not native enum types
  ([ADR 0004](../adr/0004-text-and-check-instead-of-postgres-enums.md)).
  Shipment status declares all 22 Release 2 values now, so no migration is
  needed when operations ship.
- **Append-only tables** — `shipment_events`, `audit_events`,
  `customer_credit_entries`, all three snapshot tables — carry a trigger that
  rejects `UPDATE` and `DELETE`.
- **Weight slabs** cannot overlap: a GiST exclusion constraint makes ambiguous
  pricing impossible at the storage layer.
- **Activated rate card versions** are frozen by trigger, not by convention.

---

## 4. API surface

86 documented operations across 62 paths, all under `/api/v1` except the three
unauthenticated probes.

| Group | Endpoints |
|---|---|
| Health | `/livez` `/readyz` `/version` |
| Authentication | login, refresh, logout, forgot/reset/change password, me, sessions |
| Users & roles | user CRUD, status, role grant/revoke, session revocation, role CRUD, permission catalogue |
| Organization | profile read/update, audit trail |
| Network | regions, operating units (+hierarchy, capabilities, deactivate), hubs, branches, franchises, agreements |
| Geography | countries, states, cities, PIN code lookup/search, localities, zones, zone mappings, imports (+status, errors, CSV) |
| Products | courier service CRUD, deactivate |
| Serviceability | check (GET/POST), debug, explanations, service areas, rules, closures, routes, route overrides |
| Pricing | quote, rate cards, versions, zone rates, weight slabs, surcharges, discounts, activate/archive, tax rules |
| Customers | CRUD, status, addresses, contacts, credit, billing profile |
| Shipments | book, list, get, events, cancel, label (JSON/ZPL) |

The frontend contract is `docs/contracts/release-1.md`; the machine-readable
contract is `docs/openapi.yaml`.

---

## 5. Business rules implemented

**Booking** — one transaction covering: authenticate actor → validate
tenant/customer → validate sender → validate recipient → validate packages
against the product envelope → check serviceability → resolve route → calculate
price → check credit → allocate AWB → persist shipment, packages, address
snapshot, charge snapshot, route snapshot → append `BOOKED` event → append audit
event → commit.

**AWB** — `<prefix><YYMMDD><000001>`, e.g. `CSV260808000001`. Allocated by one
atomic UPSERT, globally unique, with `UNIQUE(awb)` as the backstop.

**Pricing** — chargeable weight is `max(actual, volumetric, lane minimum,
product minimum)` rounded up to the product's slab granularity. Freight comes
from an explicit weight slab where one covers the weight, otherwise from the
base-plus-additional zone rate. Surcharges apply in priority order with
condition evaluation; discounts respect stackability and caps; tax selects
CGST+SGST for intra-state and IGST for inter-state. Rounding is HALF_UP, once
per line. Every line carries a customer-safe explanation.

**Routing** — precedence: override → service-specific route → generic route →
fallback → local delivery. Every candidate query ends its `ORDER BY` with `id`,
so ties are stable. Temporary closures block the affected end. Cut-off pushes
the promise to the next despatch. Remote-area SLA uplift applies.

**State machine** — 22 states declared, transitions table-driven. Release 1
permits `BOOKED → CANCELLED` only; Release 2 transitions are declared and
refused with a message naming the release. Terminal states admit nothing.

**Credit** — the profile row is locked `FOR UPDATE`, the limit is checked, and
every movement writes an append-only ledger entry beside the counter, so the
balance is reproducible from history. Cancellation releases it.

---

## 6. Security controls

| Control | Implementation |
|---|---|
| Tenant isolation | `organization_id` predicate on every tenant-owned query; the tenant comes from the session, never the request |
| Object-level authorization | Operating-unit scope and customer-portal scope, checked per object |
| Enumeration resistance | Out-of-scope objects return 404, not 403; login failures are byte-identical for unknown and wrong-password |
| Password storage | Argon2id, t=3 m=64MiB p=2, per-hash cost recorded, transparent re-hash on login |
| Timing attacks | Constant-cost dummy verification when no user matches |
| Brute force | Per-IP and per-account throttling plus a 5-failure account lockout |
| Token theft | 15-minute access tokens; refresh rotation with chain-wide revocation on reuse |
| Privilege escalation | `isSuperAdmin` not settable via API; `SUPER_ADMIN` grantable only by a platform operator; a role cannot carry a permission its creator lacks |
| Mass assignment | Unknown JSON fields rejected with the field named |
| SQL injection | 100% parameterised; sort and filter values come from allowlists |
| IDOR / BOLA | Public IDs are 128-bit and unguessable; every lookup is tenant-scoped |
| Rate limiting | Redis sliding window keyed by session, degrading to in-process on outage |
| Transport | TLS 1.2+, HSTS, CSP, `X-Frame-Options: DENY`, `nosniff`, no-store |
| CORS | Strict allowlist; wildcard refused in production |
| Header spoofing | `X-Forwarded-For` honoured only from configured proxy CIDRs |
| Secret hygiene | Automatic redaction of sensitive keys in logs; no secret in any response |
| Audit | 60+ audited actions with before/after, actor, reason and request id, append-only |
| Upload safety | Extension and MIME check, 25 MB cap, streamed parse, row cap |
| ZPL injection | Label text escaped before it reaches printer control codes |

---

## 7. Concurrency and idempotency

| Risk | Control | Proven by |
|---|---|---|
| Duplicate AWB | Atomic UPSERT + `UNIQUE(awb)` | `TestConcurrentBookingsProduceUniqueAWBs` — 120 concurrent, 120 distinct |
| Duplicate booking on retry | `UNIQUE(org, endpoint, key)` + completion inside the business transaction | `TestIdempotentBookingReplays`, `TestConcurrentIdempotentBookingsCreateOne` — 20 parallel, 1 shipment |
| Key reused with a different body | SHA-256 request digest comparison | `TestIdempotencyKeyReuseWithDifferentBodyIsRejected` |
| Lost update on config | `version` column + `expectedVersion` | Update paths return `CONCURRENT_MODIFICATION` |
| Double state transition | Compare-and-swap on `current_status` | `TestCancellationIsGuardedByTheStateMachine` |
| Credit overdraw | `SELECT … FOR UPDATE` on the credit profile | `ReserveCredit`, exercised by the booking tests |
| Duplicate shipment event | `UNIQUE(shipment_id, sequence)` | Schema constraint |
| Concurrent migration | PostgreSQL advisory lock | `TestMigrationsApplyRollBackAndReapply` |
| Overlapping rate slabs | GiST exclusion constraint | `TestOverlappingWeightSlabsAreRejected` |

**A real defect this found.** The first 120-concurrent run failed with every
request timing out. The cause was that booking acquired pooled connections
*inside* its transaction: once concurrency exceeded the pool size, every
transaction held one connection and waited for a second that would never come.
The fix split booking into `Prepare` (reads, pooled, no transaction) and
`Commit` (writes only, on the transaction's connection), and added
`idempotency.Preparer` so the pattern is structural rather than a convention.
This would have been a production outage under load.

A second defect the tests caught: cached DTOs used `json:"-"` on internal row
ids, so a cache *hit* returned zeroed ids and every downstream lookup failed.
Fixed by making the cached structs fully serialisable and projecting a separate
API view.

---

## 8. Performance

### Query plans

`docs/releases/explain-analyze.md` holds the full `EXPLAIN (ANALYZE, BUFFERS)`
output, captured against 19,000 PIN codes, 40,000 shipments and 40,000 audit
events. The test fails the build if any of these falls back to a sequential
scan on a high-volume table.

| Query | Plan | Actual |
|---|---|---|
| PIN code lookup | Nested loop over `pincodes_code_unique` | 0.015 ms |
| Serviceability: serving units | Index scan on `service_areas_resolve_idx` | 0.008 ms |
| Shipment by AWB | Index scan | 0.012 ms |
| Shipment list, page 1 | Index scan on `shipments_org_created_idx` | 0.017 ms |
| Shipment list, 20,000 rows deep | Same index, same cost | 0.014 ms |
| Shipment list filtered by status | `shipments_org_status_idx` | 0.020 ms |
| Rate card resolution | Index scan | 0.007 ms |
| Audit trail page | `audit_events_org_time_idx` | 0.011 ms |

The deep-page result is the point of keyset pagination: page 800 costs the same
as page 1, where `OFFSET` would degrade linearly.

### Load test

`k6` against the API on the development machine (PostgreSQL 16 and Redis 7 in
containers), 3 minutes, four concurrent scenarios: 20 VUs of lookups, 8 of
bookings, 5 of console browsing, 2 logins per second.

```
iterations       2,639        http_reqs  6,156 (33.3/s)      http_req_failed  0.00%

                        avg      p50      p90      p95      max
pincode lookup         1.45     1.20     2.35     2.83    13.09  ms
quote                  2.95     2.81     3.88     4.29    14.44  ms
shipment detail        2.85     2.60     4.33     5.67     8.44  ms
shipment list          4.10     3.49     7.29     8.84    19.07  ms
serviceability         5.29     4.98     7.46     8.11    39.05  ms
booking               21.94    21.55    28.02    30.11    50.20  ms
login                 83.95    83.96    89.68    91.64   104.20  ms

497 shipments booked, 497 distinct AWBs, 0 duplicates
DB pool peak 7 of 20 · heap 73 MB · every threshold passed
```

Login is deliberately the slowest path: Argon2id at production cost (t=3,
64 MiB) is ~84 ms of intentional work.

Reproduce:

```bash
go run ./cmd/migrate up && go run ./cmd/migrate demo
k6 run -e BASE_URL=http://localhost:8080 -e EMAIL=admin@demo.test \
       -e PASSWORD='DemoPassw0rd!2026' -e CUSTOMER_ID=cus_... \
       tests/load/courier-load.js
```

---

## 9. Test evidence

```
$ go test -race -count=1 ./...
ok  github.com/ceserve/courier-os/internal/platform/money   1.471s
ok  github.com/ceserve/courier-os/internal/pricing          1.662s
ok  github.com/ceserve/courier-os/internal/shipment         2.205s
ok  github.com/ceserve/courier-os/tests/contract            3.284s
ok  github.com/ceserve/courier-os/tests/integration        50.993s
```

67 test functions, all passing under the race detector. Nothing is skipped.

| Category | What it proves |
|---|---|
| **Tenant isolation** | Organization B gets 404 on every one of A's shipments, events, labels, cancels, customers and addresses; lists and AWB search return nothing; booking against a foreign customer is refused; the impersonation header is refused to a tenant admin. |
| **Authentication** | Unknown email and wrong password are indistinguishable; lockout after 5 failures; refresh rotates and reuse revokes the chain; logout is immediate despite the cache; reset tokens are single-use; malformed and `alg:none` tokens rejected. |
| **Authorization** | A franchise operator is refused 8 privileged mutations and allowed 4 permitted ones; branch scope hides other branches' shipments (404); privilege escalation blocked three ways; system roles immutable; deactivation is immediate; the last administrator cannot be removed. |
| **Booking** | AWB format, snapshots, event log, audit record and price arithmetic all asserted on one booking; append-only enforced by attempting UPDATE and DELETE directly in SQL. |
| **Concurrency** | 120 concurrent bookings → 120 unique AWBs, 0 failures. 20 concurrent identical idempotent requests → exactly 1 shipment. |
| **Pricing** | 10 weight/rounding/volumetric boundary cases; HALF_UP rounding pinned on the exact figures that appear on an invoice; overflow panics; conditional COD and remote surcharges; CGST/SGST vs IGST; activated versions immutable in SQL *and* through the API; a booked shipment's price survives a rate change while a new booking picks up the new rate; overlapping slabs rejected. |
| **Routing** | 25 identical resolutions produce identical answers; a higher-priority service area wins deterministically; effective dating is honoured; overrides take precedence and are audited; closures block and un-block booking; refusals carry an actionable reason and a decision trace. |
| **Migrations** | On a clean database: 12 apply, re-running is a no-op, checksums validate, all 12 roll back leaving zero tables, and all 12 re-apply. Editing an applied migration is detected. |
| **Contract** | The OpenAPI document parses, every `$ref` resolves, the published shipment-status enum matches the code exactly, every operation is documented, and no mutation lacks a failure response. |

Coverage is 38.5% of `internal/` overall, concentrated where correctness
matters: shipment 80.5%, audit 71.2%, auth 57.0%, pricing 55.8%, tenant 54.7%,
serviceability 50.6%. Configuration CRUD is lower (network 15%, geography 19%) —
see §11.

---

## 10. Deployment notes

Migrations are a separate, explicit step; the API refuses to start against an
unmigrated database. Applying them is safe from multiple instances at once
(advisory lock) and re-running is a no-op.

```bash
docker compose -f deploy/docker-compose.prod.yml run --rm migrate up
docker compose -f deploy/docker-compose.prod.yml run --rm migrate status
BOOTSTRAP_ADMIN_EMAIL=... BOOTSTRAP_ADMIN_PASSWORD=... \
  docker compose -f deploy/docker-compose.prod.yml run --rm migrate bootstrap
docker compose -f deploy/docker-compose.prod.yml up -d
```

`migrate bootstrap` is idempotent and creates the platform tenant plus a
`SUPER_ADMIN` with `mustChangePassword`. Remove the bootstrap variables
afterwards.

PostgreSQL requires the `btree_gist` extension (in the official image and in
`postgresql-contrib`); migration 0001 creates it.

Rollback: roll the image back first. Every migration has a `down`, but rolling
back a schema discards data written since the deploy — prefer a forward fix.

Full runbook, backup/restore procedure and disaster scenarios:
[`docs/deployment.md`](../deployment.md).

---

## 11. Known risks and limitations

1. **Test coverage is uneven.** Business-critical paths are well covered;
   configuration CRUD (network 15%, geography 19%, product 32%) is exercised
   mainly through the fixtures that other tests depend on. The gap is real:
   an untested update path could accept bad input. Mitigated by shared
   validation and database constraints, but worth closing in Release 2.

2. **No production restore has been rehearsed.** The procedure is written and
   the backup script self-verifies with `pg_restore --list`, but per
   Constitution §39 a backup is not valid until a restore has been tested. Do
   this before go-live and record the measured restore time.

3. **Load tested on development hardware, not the VPS.** Figures in §8 come from
   an Apple Silicon laptop with containerised PostgreSQL. The 4 vCPU target will
   be slower; the *shape* (which paths are expensive, where the pool sits)
   transfers, the absolute numbers do not. Re-run `tests/load/courier-load.js`
   on the VPS before committing to an SLA.

4. **Session revocation is bounded by the cache TTL in one case.** Logout,
   password change, deactivation and role changes invalidate eagerly and take
   effect immediately. If Redis is unreachable at that moment the eager delete
   fails silently and revocation is delayed by up to `SESSION_CACHE_TTL`
   (30 s). Accepted: the window is short and the alternative is a database read
   per request.

5. **AWB series is capped at 999,999 per tenant per day.** Exhaustion returns a
   clear 409 rather than wrapping. Ample for the target scale; a tenant
   approaching it needs a wider numeric segment, which is a schema change.

6. **Credit exposure is a maintained counter, not a ledger.** Every movement
   writes an append-only entry so the balance is reproducible, but the
   authoritative double-entry ledger arrives in Release 3. Until then credit is
   correct but not independently auditable against a chart of accounts.

7. **No email transport.** `forgot-password` generates and stores a token but
   nothing delivers it; outside production the token is returned in the response
   for testing. Release 4 adds notifications. **This means password reset is not
   usable by real users yet** — administrators must reset passwords directly.

8. **Rate limiting degrades to per-process on a Redis outage.** With two API
   replicas the effective limit doubles during an outage. Deliberate: failing
   open entirely would remove brute-force protection exactly when the platform
   is already degraded.

9. **`govulncheck` runs in CI but has not been run against a pinned release
   build.** Dependencies were fetched at build time; run it and record the
   result as part of the release gate.

10. **PIN code dataset is not loaded.** The import machinery is complete and
    tested, but no real India Post dataset ships with the repository. An
    operator must import one before the system can book anything outside the
    demo tenant.

---

## 12. Deferred to later releases

| Deferred | Release | Why now is wrong |
|---|---|---|
| Object storage (S3) | 2 | No Release 1 consumer; see [ADR 0007](../adr/0007-object-storage-deferred-to-release-2.md) |
| Pickup, scanning, bagging, manifest, line haul, delivery, NDR, RTO, POD | 2 | The state machine, custody columns and event log are in place for them |
| Public tracking | 2 | Depends on operational events existing |
| Commission, double-entry ledger, COD custody, settlement, billing | 3 | Depends on delivery and POD |
| Notifications, webhooks, customer/franchise portals, reporting, partner API | 4 | Job queue and audit foundation are in place |
| Security review, observability hardening, backup/DR verification, load hardening | 5 | This release provides the evidence base they build on |

Interfaces that later releases will use — the job queue, the audit recorder, the
snapshot tables, the transition table, the capability model — are already in
place, and the shipment status enum already declares every Release 2 state, so
no migration is required to start using them.
