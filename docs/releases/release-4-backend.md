# Release 4 — Productization, Portals, Reporting and Integrations

Completion report for M26–M32. Written 9 August 2026.

Release 4 turns a working courier backend into something several different
audiences can use: customers, franchises, hub and branch staff, operations
supervisors, and other companies' software. Every module in it is a *scope rule
plus a projection* over machinery Releases 1–3 already built — which is why the
tests here are overwhelmingly about isolation rather than about features.

---

## 1. Status

| Module | State |
|---|---|
| M26 Notifications | complete — service, worker, observers, admin API |
| M27 Customer portal | complete |
| M28 Franchise portal | complete |
| M29 Hub/branch console | complete |
| M30 Operations command centre | complete |
| M31 Reporting engine | complete |
| M32 Partner API and webhooks | complete |

Evidence: 296 documented API operations, 30 migrations that apply, roll back and
re-apply on a clean database, and a full test suite green under `-p 1`
(`go test ./...`), including the load profile below run against a live server.

---

## 2. Files created and changed

### New packages

| Path | Purpose | Lines |
|---|---|---|
| `internal/notification/` | M26: adapters, delivery, observers, admin | 5 files |
| `internal/partner/` | M32: API keys, webhooks, event catalogue | 4 files |
| `internal/partnerapi/` | M32 transport: external + management surfaces | 4 files |
| `internal/analytics/` | M30: command centre service and transport | 2 files |
| `internal/portal/` | M27/M28/M29: scope rules and projections | 2 files |
| `internal/portalapi/` | M27/M28/M29 transport | 1 file |
| `internal/reporting/` | M31: engine, chunked sources, transport | 3 files |

### Schema

| Migration | Contents |
|---|---|
| `0026_notifications` | templates, preferences, subscriptions, notifications, attempts, 12 default templates |
| `0027_partner_webhooks` | api_keys, api_key_requests, webhook_endpoints, subscriptions, deliveries, attempts |
| `0028_analytics_reporting` | shipment_daily_stats + incremental trigger, operational_snapshots, report_runs, backfill |
| `0029_rbac_release4` | 17 permissions across notification, portal, analytics, reporting and partner |
| `0030_snapshot_retention` | snapshot retention guard, three partial indexes (see §7) |

### Queries

`db/queries/notification.sql`, `partner.sql`, `analytics.sql`, `portal.sql`,
`reporting.sql`.

### Cross-cutting changes to existing code

- **`shipment.Transitioner` gained observers** (`transition.go`). This is the
  outbox point of the platform: notifications and webhooks both register on it,
  so they fire for every state change without any module remembering to call
  them. Booking and cancellation do not go through the engine and invoke the
  observers explicitly.
- **`shipment.Booker` gained `Get`, `Cancel` and `Label`**, moved out of the
  HTTP handler so the partner and portal transports share them. Two transports
  calling one service is the only arrangement where "may this be cancelled"
  cannot drift between them.
- **`tenant.Principal` gained `ActorUserID()`** and partner fields. A partner
  request has no user row, and `&p.UserID` pointing at zero violated four
  foreign keys.
- **`jobs.RetryAfter`** lets a handler dictate its own retry interval (§5).
- **`pricing.QuoteRequest` / `QuoteFor`** and `shipment.ValidateBooking` /
  `ReadBody` / `DecodeStrict` exported for reuse by the partner transport.

---

## 3. Business rules implemented

### M26 Notifications

Provider-neutral: the domain raises a notification, an adapter sends it, and no
provider SDK appears anywhere in the shipment, delivery or finance modules.

The rule that shapes everything else — **raising is never a failure path**. A
message that cannot be sent becomes a `SUPPRESSED` row carrying the reason
(`NO_ADDRESS`, `NO_TEMPLATE`, `NO_SENDER`, `OPTED_OUT`), not an error. Failing a
delivery scan because a text message could not be composed would be absurd.

The corollary is that the module is a black box unless somebody can look inside
it, which is what the admin surface is for: the outbox with suppression reasons,
per-attempt provider history, template preview, and a channels endpoint that
says which channels actually have an adapter — the most common cause of
"notifications are not working", and otherwise invisible.

Templates derive their variable list from the body rather than accepting one:
a declared list that disagrees with the text is a lie the first recipient
discovers. Only one active template may serve a given (event, channel, locale).

### M27 Customer portal

Scoped by the `customer_users` link, not by the tenant. The distinction matters:
`Principal.CustomerScope()` returns nil for a staff user meaning *unrestricted*,
and passing that into a portal query would hand one customer the tenant's entire
book. `portal.CustomerScope` therefore refuses a staff principal outright.

Another account's shipment is **404**, not 403.

### M28 Franchise portal

The franchise is derived from the caller's operating-unit grants, never from a
parameter. A principal scoped across two franchises is a configuration error and
is reported rather than silently resolved to whichever came first.

Commission "earned" is the **net of the signed entry types** — EARNED positive,
REVERSAL negative — matching `SumCommissionByFranchise` exactly, so the portal
and the settlement engine cannot disagree about what a franchise is owed.

### M29 Hub and branch console

The only surface where response *size* is a design constraint. A lookup returns
ten fields where the internal API returns forty; `amountDueMinor` is present
only for COD, so a prepaid parcel cannot be misread as owing money. A load-test
check fails if the payload exceeds 800 bytes.

### M30 Operations command centre

Two kinds of figure, and the response says which is which:

| Section | Guarantee |
|---|---|
| `period` | exact — an incrementally maintained rollup updated in the same transaction as each status change |
| `live` | live — counted at request time over a partial index |
| `snapshots` | sampled — every point carries its own `capturedAt` |

Operating-unit scope applies whether or not the client asks.

### M31 Reporting

A report is a job, not a request. `POST` returns **202** with a poll URL; a
worker reads by keyset cursor, formats each chunk, and writes it straight into
an upload stream through an `io.Pipe`. Peak memory is one chunk (2,000 rows),
not one report — there is no slice of results anywhere in the engine, which is
the property most worth preserving if it is ever edited.

Offset paging is banned in the sources: a million-row export paged by OFFSET
re-scans the rows it skips on every chunk, turning a linear export into a
quadratic one.

Finance report types (commission, settlement, revenue, COD aging) need
`report.finance` on top of `report.run`.

### M32 Partner API and webhooks

Two credential systems that never mix, tested in both directions. Full detail in
[`docs/contracts/release-4.md`](../contracts/release-4.md).

---

## 4. Authorization and tenant controls

All 17 Release 4 permissions are enforced in code — verified by grepping each
against the source, which is how the gap reported mid-release (12 of 17 seeded
with nothing checking them) was found and closed.

Isolation proven by test:

| Boundary | Test |
|---|---|
| API key → internal API | `TestPartnerCannotReachTheInternalAPI` (401 with every scope) |
| Session → partner API | `TestAUserSessionCannotUseThePartnerSurface` |
| Cross-tenant partner reads | `TestPartnerKeyCannotSeeAnotherTenant` (404, no AWB echo) |
| Customer → another account | `TestCustomerPortalShowsOnlyItsOwnAccount` |
| Staff → customer portal | `TestCustomerPortalRefusesAStaffLogin` |
| Franchise → another franchise | `TestFranchisePortalCannotReadAnotherFranchise` |
| Scoped manager → network | `TestScopedOperatorCannotReadTheWholeNetwork` |
| Scoped manager → another branch queue | `TestConsoleCannotReadAnotherBranchesQueue` |
| Cross-tenant console scan | `TestConsoleIsTenantScoped` |
| Cross-tenant report download | `TestReportsAreTenantScoped` |
| Cross-tenant notifications | `TestNotificationsAreTenantScoped` |

---

## 5. Concurrency and idempotency

| Control | Where |
|---|---|
| `Idempotency-Key` **required** on partner booking | a retry loop cannot see a duplicate the way a clerk can |
| Claim is `PENDING → SENDING` only | notifications and webhook deliveries; a second worker cannot re-send |
| Stuck-row recovery by staleness window | `ReclaimStuckNotifications` / `ReclaimStuckDeliveries` |
| Report claim is `QUEUED → RUNNING` only | two workers streaming into one object key would both write |
| Dedupe index `(endpoint_id, event_id) WHERE replay_of_id IS NULL` | one delivery per subscriber per business event |
| Revocation is one-way | `api_keys_guard` refuses reinstatement even by direct SQL |

### One retry schedule, not two

`next_attempt_at` was written by the services and read by nothing: the claim
queries filtered on status alone, so retry timing was entirely the job queue's
generic `2^n × 5s`. The contract published "capped at 30 minutes"; the system
did 5. Fixed by adding `jobs.RetryAfter(d, err)` — the domain computes its delay
once, stores it, and hands the same value to the queue.

### Orphaned outbox rows

`Raise()` and `Emit()` log and continue when their send job cannot be enqueued —
correct, because a queue problem must not roll back a shipment — but that leaves
a row nobody will pick up. `SweepDue` on both services re-enqueues them every 15
minutes; the job dedupe key makes it a no-op for rows that already have a job.

---

## 6. Defects found and fixed during the release

Listed because they are the substance of the work, not incidental to it.

1. **Retry schedule published but not honoured** (above).
2. **App clock vs database clock.** `next_attempt_at` was computed from
   `time.Now()` and compared against `now()` in the claim query. Sub-second skew
   made a webhook that should go out immediately wait for the next poll — it
   surfaced as an intermittently failing replay test. Retries now pass a *delay*
   and the database schedules from its own clock.
3. **Nil array → NULL.** A nil `scopes` slice was sent as NULL against a NOT
   NULL column; key creation 500'd.
4. **Partner principal had no user row.** `&p.UserID` at zero violated four
   foreign keys (`shipments.booked_by_user_id`, shipment events, pickups, POD
   access).
5. **Two actor vocabularies.** `audit_events` allows `API_KEY`;
   `shipment_events` allows `PARTNER`. Neither CHECK was widened.
6. **Command centre sequential scans.** Two of its queries scanned the whole
   shipments table (§7).
7. **Snapshot retention was impossible.** A purge was written against a table
   whose trigger refused DELETE. Migration 0030 replaces it with a guard that
   refuses UPDATE unconditionally and DELETE inside a 90-day floor.
8. **Franchise portal contradicted itself.** The summary counted bookings while
   the shipment list counted anything the branch touched — 0 above a list of 3.
   The listing now takes an explicit `role` (ORIGIN/DESTINATION/ANY), defaults
   to ORIGIN, and every row says which it is.
9. **Duplicate template returned 500.** The service checked the wrong
   constraint names, so a legitimate mistake surfaced as an internal error.
10. **Template preview reported no missing variables, ever.** It computed them
    from the *rendered* text, by which point `Render` had already substituted
    them away — the endpoint's entire purpose, silently broken.
11. **Argon2id on every partner request** (§7).
12. **Harness AWB prefix collision.** `randomPrefix` took the ULID's *timestamp*
    half, so two tenants created in the same window got identical AWB prefixes
    and collided across tenants — presenting as a booking bug.

---

## 7. Performance

### Command centre — two sequential scans found and fixed

Measured at 200,000 shipments; full plans in
[`explain-analyze-command-centre.md`](explain-analyze-command-centre.md).

| Query | Before | After | Fix |
|---|---|---|---|
| Live backlog by status | 19.45 ms, parallel seq scan | **8.55 ms**, index-only | partial index, 848 kB |
| SLA breaches | 85.12 ms, parallel seq scan | **0.40 ms**, index-only | partial index, 872 kB |
| Period totals (30 days) | — | **0.077 ms** | rollup, 30 rows read |
| Backlog by facility | — | **13.57 ms** | partial index |

The three partial indexes total under 2 MB against a 63 MB table, and none grows
with delivered history — they cover only work still in play, so a tenant with
two million delivered shipments and four thousand in progress gets the same
response time.

The query predicates had to be rewritten as `NOT IN (terminal states)` to match
the index definitions: the planner cannot prove that `= ANY(17 live statuses)`
implies `NOT IN (4 terminal ones)`.

### Partner API — Argon2id per request

The load profile showed the partner surface at **p95 89.6 ms** against 2–6 ms
for every other authenticated surface. Isolated by comparing the identical
tracking query with and without key auth:

```
partner tracking (API key auth):  84–94 ms
public tracking (no auth):        ~1 ms
console lookup (session auth):    ~1 ms
```

The whole difference was Argon2id (t=3, 64 MiB, p=2) run on every request. On
the 4 vCPU / 16 GB target box that caps partner throughput at roughly 45 req/s
with all cores saturated, and makes every concurrent partner request a 64 MiB
transient allocation.

Argon2 exists to make *low-entropy* secrets expensive to brute-force. An API key
secret is 32 bytes of cryptographic randomness — there is nothing to
brute-force, and the codebase already documents exactly this reasoning for
refresh tokens in `security.NewOpaqueToken`. API key secrets now store a
self-describing SHA-256 digest (`sha256$…`).

**Keys issued before the change keep working**: `verifySecret` recognises an
Argon2 hash and falls back, so there is no migration and no partner outage. A
re-issued key picks up the fast path.

| | Before | After |
|---|---|---|
| Partner tracking | 84–94 ms | **4–5 ms** |
| Legacy Argon2 key | — | 58–65 ms (fallback, still works) |
| Load profile p95 | 89.6 ms | **11.8 ms** |

### Load profile

`tests/load/courier-productization.js`, run for 2 minutes against a live server
on the development machine with a 300-shipment demo tenant. All five surfaces
exercised with their own credentials.

```
console_lookup_latency:   p95=4.0ms   avg=1.9ms    (30 req/s)
console_queue_latency:    p95=2.8ms   avg=1.7ms
command_centre_latency:   p95=5.9ms   avg=3.0ms    (6 req/s)
customer_portal_latency:  p95=4.1ms   avg=2.3ms    (ramp to 10 req/s)
franchise_portal_latency: p95=6.0ms   avg=3.3ms    (2 req/s)
partner_api_latency:      p95=11.8ms  avg=6.8ms    (5 req/s)

console_lookups: 3601      http_reqs: 8218      http_req_failed: 0.00%
```

Every threshold passed. **This is not a production capacity measurement** — it
is a development machine with 300 shipments, not a 4 vCPU VPS with a year of
history. Its value is as a regression gate: the thresholds are tight enough that
losing an index or widening the console projection would fail the run.

---

## 8. Tests

| Suite | Coverage added |
|---|---|
| `internal/partner` | signature scheme, scopes, IP allow-list, backoff, secret digests, legacy fallback |
| `internal/platform/jobs` | handler-specified retry interval, permanent-failure precedence |
| `tests/integration/partner_test.go` | credentials, scope enforcement on 10 endpoints, both isolation directions, idempotency, rate limiting, webhook signature/retry/dead-letter/replay/dedupe |
| `tests/integration/command_centre_test.go` | rollup accuracy vs source, scope leakage, window bounds, snapshot append-only and retention floor |
| `tests/integration/portal_test.go` | customer/franchise/console isolation, console payload budget, franchise role semantics |
| `tests/integration/reporting_test.go` | queued-not-computed, multi-chunk streaming correctness, tenant scoping, finance gating, expiry deleting the file, single-claim generation |
| `tests/integration/notification_admin_test.go` | outbox suppression reasons, template preview, duplicate conflict, retry/cancel, permissions |
| `tests/contract` | partner scopes, webhook events, report types, suppression reasons, partner security scheme — each pinned against the Go constants |

Full suite: green. Migration ladder: 30 apply → 30 roll back → 30 re-apply with
checksums validated. Partner tests also pass under `-race`.

---

## 9. Deployment notes

New configuration:

| Variable | Default | Meaning |
|---|---|---|
| `PARTNER_RATE_LIMIT_PER_MINUTE` | `120` | fallback limit for a key without its own |
| `PUBLIC_TRACKING_URL` | `https://track.example.com/` | goes into notification bodies; must be the page a recipient can open |
| `WORKER_QUEUES` | `default,import,notifications,webhooks,reports` | **the three new queues must be listed or nothing is delivered or generated** |

Worker maintenance, every 15 minutes:

- `ExpireAPIKeys`
- `ReclaimStuckNotifications` / `ReclaimStuckDeliveries` (15-minute staleness)
- `SweepDue` on notifications and webhooks (orphaned outbox rows)
- `Analytics.CaptureAll` (one snapshot per tenant) and `PurgeSnapshots` (90 days)
- `Reporting.ExpireRuns` (deletes the object as well as the record)

Migration 0030 creates three indexes on `shipments`. On a large production table
they should be created `CONCURRENTLY` — the migration as written takes a lock,
which is fine on the current data volume and will not be at scale.

---

## 10. Known risks and deferred work

**Risks**

- **No real provider adapters.** Every notification channel is wired to a
  logging sender. Adding one means implementing `notification.Sender` and
  registering it; nothing else changes. Until then every message is composed,
  recorded and never actually delivered.
- **Autovacuum on `shipments` is untuned.** The new partial indexes are
  index-only scans, which depend on the visibility map. Immediately after a bulk
  update the SLA query measured 58 ms with 124,000 heap fetches, and 0.4 ms
  after `VACUUM ANALYZE`. This should be settled before the command centre goes
  under production load.
- **The rollup trigger adds a write to every status change.** That is the
  deliberate trade, but it has not been measured under the scan throughput of a
  peak hub shift.
- **Load numbers are from a development machine**, not the target VPS (§7).
- **Legacy Argon2 API keys stay slow** until re-issued. Correct, but a tenant
  with many old keys keeps the old ceiling on those keys.

**Closed after the frontend gap review** (see
[`release-4-response.md`](../frontend-backend-gaps/release-4-response.md))

- **Session portal subject.** `GET /auth/me` now carries a `portal` object
  naming the customer accounts or the franchise a login represents. Without it a
  portal client had to infer "which franchise am I" from a list of operating
  unit ids, which is backend authorization logic running in a browser. Neither
  the portal endpoints nor the backend tests would have caught this — it took
  someone building the client to notice.
- **Offset page metadata** on the API key and webhook endpoint registers. A
  plain omission: the frontend was requesting the maximum page and stating when
  the cap was hit rather than guessing.
- **Settlement adjustments.** The correction path `Generate` had been pointing
  at since Release 3 without it existing. See §6a.

**Deferred at the Release 4 cutoff**

- `pickup.completed`, `pod.captured`, and `cod.collected` had no emitters at the
  Release 4 cutoff. Phase 2 later added transactional producers and focused
  integration tests for all three.
- `webhook_subscriptions.filter` is stored but not applied: every subscriber to
  an event receives every instance of it.
- Report `format: JSON` is accepted and validated but always produces CSV.
- Per-unit operational snapshots: one network-wide row per tenant per interval,
  not one per facility.
- No per-endpoint circuit breaker beyond the 20-failure auto-pause; a slow
  endpoint consumes a worker slot for its timeout on every attempt.

---

## 10a. Settlement adjustments — a Release 3 gap closed here

`Generate` refused to recalculate a frozen settlement with the message "Raise an
adjustment instead", and there was no way to raise one. The table, the queries
and both permissions existed; nothing connected them, so the one documented
correction path for an approved statement dead-ended.

`internal/settlement/adjustment.go` closes it. The subtle part is where the money
lands: against an open statement the approved adjustment is folded into the next
recalculation and must **not** post its own journal, because the settlement's
approval will post the net including it; against a frozen one the net is already
in the ledger, so the adjustment posts its own balanced journal and becomes
`APPLIED`. Doing both double-counts, and
`TestAdjustmentOnAnOpenSettlementDoesNotPostTwice` is what keeps them exclusive.

Maker/checker applies to rejection as well as approval — quietly withdrawing
your own correction leaves the same hole in the record as approving it.

Two defects were found while writing it: `postAdjustment` omitted the reason
that `SourceAdjustment` postings require, and a test straddled the
`PartyBalance` sign convention (it normalises by each account's normal balance,
so a negative net posts to the franchise receivable and reads back positive).

---

## 11. What Release 5 should pick up

The production gate (M33–M36) now has a concrete list to work from: the
autovacuum question, a load run on real hardware with real data volume, the
`CONCURRENTLY` index rollout, and at least one real notification provider so the
suppression paths are exercised against something that can genuinely fail.
