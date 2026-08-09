# Release 2 — Physical Courier Operations

Date: 2026-08-08
Modules: M09 Pickup · M10 Operational Scanning · M11 Bagging · M12 Manifest ·
M13 Line Haul · M14 Hub Operations · M15 Destination Branch · M16 Delivery Run ·
M17 NDR · M18 RTO · M19 POD · M20 Public Tracking

Contract: [`docs/contracts/release-2.md`](../contracts/release-2.md)
API: [`docs/openapi.yaml`](../openapi.yaml)
Query plans: [`explain-analyze-operations.md`](explain-analyze-operations.md)

This release makes the backend the authoritative record of where a parcel
physically is, who is holding it, and what may legally happen to it next.
Release 1 could book a shipment and price it. Release 2 moves it.

---

## 1. What works

A parcel can now be carried end to end through the network under server-enforced
custody, with every hand-off recorded as an immutable event.

Verified against a running API (`cmd/api` on a seeded demo tenant), booking to
delivery-ready:

```
 1. booked          DMO260808000560  BOOKED
 2. scan RECEIVE    ACCEPTED -> ORIGIN_BRANCH_RECEIVED  (Bag for dispatch.)
 3. bagged          added=1 shipment=ORIGIN_BAGGED
 4. bag closed      CLOSED declared=1 shipment(s)
 5. manifest closed CLOSED bags=1 shipments=1
 6. depart w/o veh  409 TRIP_NO_VEHICLE  <- departure validation
 7. departed        DEPARTED carrying 1 shipment(s)
 8. received        manifest RECEIVED
 9. shipment now    DESTINATION_BRANCH_RECEIVED
10. delivery queue  1 ready at DEL-001
11. hub summary     custody=1 ready=1 inbound=0
12. public tracking IN_TRANSIT: Ready for delivery  (2 events, Bengaluru -> New Delhi)
13. leak check      clean — no internal ids, no finance, no remarks
```

Step 6 is the point of the exercise: the trip refused to depart without an
assigned vehicle, and the refusal came from the server, not from a UI guard.

### Delivered capability

| Module | Capability |
| --- | --- |
| M09 Pickup | Scheduled / on-demand / bulk requests, agent assignment, accept-reject, arrival, completion with piece counts, failure with reason, reschedule, dispatcher board, field-agent stop list |
| M10 Scanning | Eight scan types (RECEIVE, ARRIVAL, DEPARTURE, SORT, HOLD, RELEASE, DAMAGE, EXCEPTION), single and bulk, device-event dedupe, rejection recording, facility and custody validation |
| M11 Bagging | Full OPEN → CLOSED → DISPATCHED → RECEIVED → OPENED → RECONCILED lifecycle, seal capture and verification, duplicate and conflicting-membership rejection, post-close correction only via exception |
| M12 Manifest | DRAFT → CLOSED → DISPATCHED → RECEIVED → RECONCILED, bags and loose shipments, immutable content snapshot at closure, asynchronous document generation |
| M13 Line Haul | Carrier / vehicle / driver registry, multi-leg trips across ROAD, AIR, RAIL, PARTNER, assignment, departure gating, arrival propagation |
| M14 Hub Ops | Expected inbound, receipt, bag opening, scan-driven reconciliation, six exception types with auditable resolution, custody stocktake, throughput and workload summaries |
| M15 Destination | Branch receipt, delivery-ready queue, holds, address-issue handling, customer collection, assignment eligibility |
| M16 Delivery | Runs with ordered stops, agent assignment, OFD, OTP issue and verification, success, failure, COD hand-off capture, recipient capture |
| M17 NDR | Configurable reason catalogue, case lifecycle, attempt history, seven actions, scheduled retry, max-attempt policy with automatic RTO |
| M18 RTO | Reverse movement through the real network — destination branch, hub, line haul, origin, sender — reusing the forward routing graph |
| M19 POD | OTP / signature / photo / recipient / GPS / device capture, private object storage, MIME and size validation, checksum, authorized retrieval with access logging |
| M20 Tracking | Anonymous AWB lookup, milestone normalisation, IP rate limiting, customer-safe projection |

---

## 2. Files created

Hand-written Go, excluding generated code and tests:

```
internal/ops                 3 files     606 lines   shared operational primitives
internal/pickup              3 files   1,618 lines
internal/scanning            2 files   1,045 lines
internal/bagging             3 files   1,503 lines
internal/manifest            4 files   1,529 lines
internal/linehaul            4 files   1,842 lines
internal/hubops              3 files   1,230 lines
internal/delivery            3 files   1,486 lines
internal/ndr                 2 files     955 lines
internal/rto                 2 files     793 lines
internal/pod                 2 files     687 lines
internal/tracking            1 file      282 lines
internal/opsapi              5 files   3,228 lines   HTTP transport for all of R2
internal/platform/storage    2 files     573 lines   S3 + filesystem object store
internal/shipment/state.go              442 lines   state machine (rewritten)
internal/shipment/transition.go         528 lines   transition engine (new)
```

Tests:

```
tests/integration/operations_test.go           671 lines   six scenarios
tests/integration/operations_security_test.go  408 lines   isolation and custody
tests/perf/explain_ops_test.go                 420 lines   ten hot paths
tests/load/courier-operations.js               248 lines   four k6 scenarios
```

Documentation: `docs/contracts/release-2.md`, `docs/openapi.yaml` (extended),
`docs/releases/explain-analyze-operations.md`, and ADRs
[0008](../adr/0008-one-transition-engine-for-every-state-change.md),
[0009](../adr/0009-rto-as-reverse-movement-not-mirrored-statuses.md),
[0010](../adr/0010-hand-written-s3-client.md),
[0011](../adr/0011-one-transport-package-for-release-2.md).

Repository totals after Release 2: 45,450 hand-written Go lines, 28,669
generated, 19 migrations, 467 SQL queries, 108 test functions.

### What was reused rather than rebuilt

The instruction not to duplicate existing abstractions was treated as binding.
Release 2 adds no second router, no second auth path, no second tenancy
mechanism, no second audit writer, and no second routing engine.

- Tenancy and RBAC come from `internal/tenant` and `internal/auth` unchanged;
  Release 2 adds 56 permission rows, not a new authorisation mechanism.
- Routing for RTO calls the Release 1 serviceability engine in reverse rather
  than storing a second network graph — see ADR 0009.
- Audit writes go through the existing `internal/audit` writer.
- Shipment events extend the existing `shipment_events` table; the append-only
  trigger from Release 1 still guards it.
- `internal/ops` exists to stop twelve modules each growing their own copy of
  facility resolution, code allocation, device parsing and error mapping.

---

## 3. Physical lifecycle

```
                          BOOKED
                            │
                   ┌────────┴────────┐
                   │                 │
          PICKUP_SCHEDULED       (walk-in)
                   │                 │
          PICKUP_ASSIGNED            │
                   │                 │
              PICKED_UP ─────────────┤
                                     │
                     ORIGIN_BRANCH_RECEIVED ◄── RECEIVE scan
                                     │
                          ORIGIN_BAGGED ◄────── bag item add
                                     │
                       ORIGIN_DISPATCHED ◄───── manifest dispatch / trip depart
                                     │
                              IN_TRANSIT
                                     │
                   ┌─────────────────┼──────────────────┐
                   │                 │                  │
        TRANSIT_HUB_RECEIVED         │        DESTINATION_HUB_RECEIVED
                   │                 │                  │
        TRANSIT_HUB_DISPATCHED ──────┘                  │
                                                        │
                                    DESTINATION_BRANCH_RECEIVED
                                                        │
                                             OUT_FOR_DELIVERY
                                                        │
                                     ┌──────────────────┴───────────┐
                                     │                              │
                                 DELIVERED                  DELIVERY_FAILED
                                  (POD)                             │
                                                                   NDR
                                                                    │
                          ┌─────────────────────────────────────────┤
                          │                                         │
                   (reattempt)                              RTO_INITIATED
                   back to OFD                                      │
                                                          RTO_IN_TRANSIT
                                                                    │
                                                            RTO_DELIVERED

Terminal / exception states reachable under permission from most points:
CANCELLED · LOST · DAMAGED
```

The engine holds **63 legal transitions across 22 statuses**. Every one is
permission-gated; 33 additionally require custody; 32 require a written reason.
Six are forward-only, two reverse-only, and the remainder are legal in either
direction of travel. Any pair not in the table is refused — there is no
free-text status update anywhere in the API.

### The single chokepoint

Every status change in the system — from a scanner, a delivery app, an NDR
action, an RTO progression, a trip arrival — goes through
`shipment.Apply`, which performs eight checks in fixed order inside the
caller's transaction:

1. **Direction** — a reverse-moving parcel cannot re-enter the forward ladder.
2. **Permission** — the actor's RBAC grant for this specific transition.
3. **Reason** — required text for the 32 transitions that demand it.
4. **Hold** — a held shipment cannot move until released.
5. **Custody** — one of `NONE`, `HOLDER`, `ORIGIN_BRANCH`,
   `DESTINATION_BRANCH`, `AGENT`, `ANY_FACILITY`.
6. **Clock** — client `occurredAt` is clamped for ordering, kept verbatim for
   the record.
7. **Compare-and-swap** — `UPDATE … WHERE current_status = $expected`.
8. **Event append + audit**.

Adding a module cannot bypass this; adding a transition means adding a row to
the table. That is the whole of ADR 0008.

---

## 4. API surface

173 operations across 134 paths — 86 from Release 1, 87 added here:

| Tag | Ops | Tag | Ops |
| --- | --- | --- | --- |
| Line haul | 15 | Delivery | 9 |
| Hub operations | 13 | Manifest | 8 |
| Bagging | 12 | NDR | 6 |
| Pickup | 11 | RTO | 6 |
| Scanning | 3 | Proof of delivery | 3 |
| Public tracking | 1 | | |

Full request and response schemas, enums, error codes, permissions, legal
transitions, immutable fields and idempotency requirements are in
[`docs/contracts/release-2.md`](../contracts/release-2.md). The OpenAPI
document defines 214 schemas and is validated by
`tests/contract/openapi_test.go`, which fails if a documented enum drifts from
the Go constant that backs it.

Two contract-quality items surfaced while assembling this report:

- `POST /api/v1/shipments/{shipmentId}/label` was published as an empty
  operation (`post: {}`) describing a route the server does not serve — only
  `GET` is registered, at `internal/shipment/handlers.go:52`. A generated client
  would have emitted that method and received a 405. **Removed.**
  `TestEveryOperationIsDocumented` was skipping empty operations rather than
  rejecting them, which is why it went unnoticed; the check now fails on them.
- All 87 Release 2 operations carry an `operationId`; none of the 86 Release 1
  operations do. Client generators derive method names from `operationId` and
  fall back to synthesised names without it, so the frontend gets two different
  naming conventions across the same API. This is a Release 1 artefact, left
  alone here rather than rewritten unasked, but it is worth closing before the
  frontend generates a client.

### Frontend unblocked

`docs/releases/release-2-frontend.md` recorded the frontend as blocked with a
14-item contract list. All 14 are now published: pickup queues and assignment,
scan operation types and dedupe semantics, bag and manifest lifecycles with
scanner-assisted entry, trip and fleet contracts, hub console counters, branch
queues, delivery dispatcher and agent flows, NDR catalogue and actions, RTO
stages, POD authorisation and download, anonymous tracking fields, permission
identifiers per route, and pagination and filtering guidance.

---

## 5. Schema

Seven migrations, `0013`–`0019`, adding **41 tables**. Repository totals after
Release 2: 97 tables, 492 indexes, 85 triggers, 549 check constraints, 373
foreign keys, 112 permissions.

| Migration | Adds |
| --- | --- |
| 0013 operations core | `scan_events`, pickup tables, `operational_sequences`; extends `shipment_events` with source, device, GPS and `client_occurred_at`; extends `shipments` with custody and counters |
| 0014 bagging + manifest | `bags`, `bag_items`, `bag_seals`, `bag_events`, `manifests`, `manifest_bags`, `manifest_loose_shipments`, `manifest_events` |
| 0015 line haul | `carriers`, `vehicles`, `drivers`, `trips`, `trip_legs`, `trip_assignments`, `trip_events` |
| 0016 hub operations | `operational_exceptions`, `reconciliations`, `reconciliation_items`, `shipment_holds` |
| 0017 delivery / NDR / RTO | delivery runs, items, attempts, OTPs; NDR reasons, cases, attempts, actions; RTO cases and legs |
| 0018 POD + storage | `stored_objects`, `proof_of_delivery`, `pod_artifacts`, `object_access_log`, `tracking_milestones` + 22 default milestones |
| 0019 RBAC | 56 module permissions plus `shipment.declare_exception` and `shipment.override_custody`, granted across all 12 roles; default NDR reasons |

### Integrity enforced in the database

History is protected by triggers, not by application discipline. Release 2 adds
ten append-only triggers (`scan_events`, `bag_events`, `manifest_events`,
`trip_events`, `delivery_attempts`, `pickup_attempts`, `ndr_attempts`,
`proof_of_delivery`, `pod_artifacts`, `object_access_log`) and three custom
guards:

- `bag_items_guard` — rejects any DELETE, rejects INSERT unless the bag is
  `OPEN`, and rejects removing an item from a closed bag unless an exception
  record is attached.
- `manifest_bags_guard` and `manifest_loose_guard` — the same discipline for
  manifest contents after closure.

These fire against raw SQL, so a future module cannot quietly edit a closed bag
even with direct database access.

Twenty-three partial unique indexes act as concurrency control rather than mere
validation. The load-bearing ones:

```
delivery_attempts_success_idx      one DELIVERED attempt per shipment, ever
bag_items_active_membership_idx    a shipment can be in only one open bag
delivery_run_items_active_idx      a shipment can be on only one live run
trips_vehicle_active_idx           one live trip per vehicle
trips_driver_active_idx            one live trip per driver
scan_events_device_event_idx       device event id replay guard
delivery_attempts_device_event_idx  ditto for delivery
pickup_attempts_device_event_idx    ditto for pickup
ndr_cases_active_idx / rto_cases_active_idx / shipment_holds_active_idx
```

---

## 6. Business rules implemented

**Custody.** A scan is accepted only where the parcel actually is. Attempting
`OUT_FOR_DELIVERY` from a facility that does not hold the shipment fails on
rule 5 of the transition engine, which is the explicit requirement that OFD not
be reachable from an arbitrary facility. Overriding custody requires the
`shipment.override_custody` permission plus a reason, and the override is
audited.

**Bagging.** Contents are mutable only while `OPEN`. Closure freezes the
declared contents and records seal metadata. A shipment already in an open bag
is rejected from a second one. Removing an item post-closure requires an
exception record — enforced by trigger.

**Manifest.** Closure snapshots contents. A manifest cannot depart before
closure.

**Line haul.** Departure is gated on five conditions, and the assignment
requirement is mode-aware rather than uniform:

| Condition | Error |
| --- | --- |
| ROAD trip has a vehicle | `TRIP_NO_VEHICLE` |
| ROAD trip has a driver | `TRIP_NO_DRIVER` |
| AIR / RAIL / PARTNER trip has a carrier reference — a flight or train number, without which parcels cannot be traced if it goes missing | `TRIP_NO_REFERENCE` |
| Every manifest on the trip is closed | `TRIP_HAS_DRAFT_MANIFEST` |
| The trip carries at least one manifest | `TRIP_EMPTY` |

Origin custody is validated as well. Arrival propagates to shipments only
through verified manifest membership — never by blanket update.

**NDR.** Reasons are per-tenant configuration, seeded at organisation creation.
Each failed delivery opens or extends a case, records the attempt with evidence
and contact detail, and schedules the next action. Exceeding the configured
attempt maximum initiates RTO automatically.

**RTO.** Modelled as reverse movement through the real network, not as a status
flag. The shipment's `movement_direction` flips to `REVERSE`, the routing engine
is asked for the return path, and the parcel travels destination branch → hub →
line haul → origin → sender, generating the same bag, manifest and trip records
as a forward leg. RTO charges are captured; posting them is Release 3.

**Delivery.** Exactly one success is possible per shipment, guaranteed by
`delivery_attempts_success_idx` rather than by a read-then-write check.

---

## 7. Security controls

**Tenant isolation.** Every operational query filters on the authenticated
organisation; no client-supplied organisation, franchise or unit identifier is
trusted for authorisation. Cross-tenant probes on shipments, bags and manifests
return 404, and a cross-tenant AWB presented to a scanner resolves to
`UNKNOWN_BARCODE` — the same answer as a barcode that does not exist, so the
endpoint cannot be used to test whether an AWB belongs to another tenant.

**Operating-unit scope.** A branch user's queries are additionally narrowed to
their assigned units. Proven by test: seven listing endpoints return empty for a
user scoped elsewhere.

**Public tracking.** The projection is built field by field from a whitelist,
not by redacting a full object. It exposes milestone, human-readable status,
city names, and timestamps. It does not expose internal public IDs, employee
identities, facility codes, internal remarks, or any financial field — asserted
by the leak check in step 13 above and by
`TestPublicTrackingIsRateLimitedAndOpaque`. Rate limiting is per-IP.

**Object storage.** POD artifacts are stored private with generated keys.
Upload validates MIME against an allowlist and enforces a size cap; the SHA-256
checksum is stored. Retrieval requires authorisation and is recorded in
`object_access_log`, which is append-only.

**Client timestamps.** `client_occurred_at` is stored verbatim for the
operational record, but ordering and any decision use a server value clamped to
+5 minutes / −7 days. A device with a wrong clock cannot reorder history or
backdate a delivery.

**Tested attack shapes.** Cross-tenant read and write, operating-unit escape,
custody violation, forged state transition, hold bypass, frozen-bag mutation
(attempted directly against the database, not only through the API), history
mutation, and over-broad field-agent permissions.

---

## 8. Concurrency and idempotency

| Risk | Control | Evidence |
| --- | --- | --- |
| Double delivery | Partial unique index + CAS on status | 12 concurrent attempts → 1 success, 11 conflicts, 1 row |
| Replayed scan | `X-Device-Id` + `X-Device-Event-Id` partial unique index | Replay returns `DUPLICATE` with the original `scanId`; exactly one event written |
| Repeated scan without key | State check, rejection recorded | Returns `REJECTED`, recorded for audit |
| Two users closing one bag | Status CAS inside transaction | Second gets 409 |
| Bag in two manifests | `manifest_bags_active_idx` | Constraint violation → 409 |
| Vehicle on two trips | `trips_vehicle_active_idx` | Constraint violation → 409 |
| Duplicate NDR / RTO case | `ndr_cases_active_idx`, `rto_cases_active_idx` | Constraint violation → 409 |
| Concurrent bag-code allocation | Sequence table with row lock | No process-local locking anywhere |

Idempotency is anchored in database uniqueness, never in Redis alone. Offline
field operations — pickup completion, scans, bag scans, manifest receipt,
delivery attempts, POD, NDR — all accept a device event identifier and replay
safely.

---

## 9. Performance

### Query plans

Ten operational hot paths measured against a seeded database of 40,000
shipments, 240,000 shipment events, 60,000 scan events, 5,000 bags, 3,000
manifests, 8,000 exceptions and 4,000 NDR cases. Full plans in
[`explain-analyze-operations.md`](explain-analyze-operations.md).

| Query | Execution |
| --- | --- |
| Scanner barcode resolution | 0.030 ms |
| Delivery-ready queue at a branch | 1.841 ms |
| Facility custody stocktake | 2.661 ms |
| Bag contents | 0.020 ms |
| Expected inbound manifests | 0.110 ms |
| Scan history by facility (deep page) | 0.059 ms |
| Public tracking by AWB | 0.006 ms |
| Public tracking timeline | 0.054 ms |
| Open exceptions at a facility | 0.056 ms |
| NDR cases due for reattempt | 0.019 ms |

All ten are index-served; `tests/perf/explain_ops_test.go` fails the build on a
sequential scan over an operational table.

One caveat, since the raw plans show it: the barcode-resolution plan contains a
`Seq Scan on shipment_packages` because the fixture seeds no packages, and
PostgreSQL always sequential-scans a zero-page table. Measured separately with
196,560 package rows, the piece-barcode arm uses
`shipment_packages_piece_barcode_key` and the whole resolution costs 10 buffer
hits and 0.042 ms. The addendum in the plans document records this. The
standing fixture still does not cover the multi-piece path, so a regression
there would not be caught automatically.

### Load test

k6, four concurrent scenarios for two minutes, weighted the way a courier
network actually behaves — public tracking heaviest, scanner the heaviest
write:

```
requests            9,726
requests/s          80.84
failed rate         0.00%
scans accepted          60
scans rejected       2,341
scan p95            14.02 ms   (threshold 250)
tracking p95         3.98 ms   (threshold 150)
hub dashboard p95   10.18 ms   (threshold 400)
bag list p95         6.41 ms   (threshold 400)
delivery queue p95   4.10 ms   (threshold 500)
```

All thresholds passed. The pgx pool never exceeded 20 of its configured
connections, and process RSS stayed at 185 MB — which includes `go run`
overhead, so a compiled binary is lower.

Read the accepted-versus-rejected split honestly: 2,341 of 2,401 scans were
rejected on state, because the fixture's shipments are not all sitting at the
facility being scanned. That is deliberate and it does not deflate the
measurement — a rejection resolves the barcode, locks the row, evaluates
custody and writes a scan record, which is the same work as an acceptance plus
the rejection-recording path a happy-path benchmark would skip. It does mean
the figure is not a measure of sustained *state-advancing* throughput. A
profile that walks parcels forward would exercise more index maintenance on
`shipments`, and has not been run.

These figures come from development hardware, not from the 4 vCPU / 16 GB
Hostinger VPS in the Constitution. They demonstrate headroom and the absence of
pathological queries; they are not a capacity statement for the target host.
Re-running `tests/load/courier-operations.js` on the VPS before go-live is
required.

---

## 10. Test evidence

Full suite under `-race`:

```
ok  internal/platform/money      1.546s
ok  internal/platform/storage    2.045s
ok  internal/pricing             1.555s
ok  internal/shipment            2.142s
ok  tests/contract               4.013s
ok  tests/integration          108.012s
```

`gofmt` clean, `go vet` clean, no skipped tests.

### The six required scenarios

| # | Scenario | Result |
| --- | --- | --- |
| 1 | Booking → pickup → origin branch → bag → manifest → trip → destination → delivery → POD | Pass — full forward journey with POD artifact stored and retrievable |
| 2 | Delivery failed → NDR → reattempt → delivered | Pass — case opened, attempt recorded, reattempt scheduled, second attempt succeeds |
| 3 | Delivery failed → NDR → RTO → returned to sender | Pass — direction flips to REVERSE, parcel travels the reverse network, `RTO_DELIVERED` |
| 4 | Bag mismatch | Pass — reconciliation ends `COMPLETED_WITH_EXCEPTIONS`, 1 missing + 1 excess, both raise open exceptions, closure declaration unchanged |
| 5 | Duplicate scans and retries | Pass — replay returns `DUPLICATE` with original scan id; keyless repeat returns `REJECTED`; exactly one `ORIGIN_BRANCH_RECEIVED` event |
| 6 | Concurrent delivery completion | Pass — 12 goroutines, 1 success, 11 conflicts, 1 database row |

### Security tests

Eight tests in `tests/integration/operations_security_test.go`, all passing:

| Test | Asserts |
| --- | --- |
| `TestOperationalTenantIsolation` | Cross-tenant probes 404; foreign AWB scans as `UNKNOWN_BARCODE`; seven listings empty |
| `TestOperatingUnitScopeIsEnforced` | A user scoped to one branch sees nothing from another |
| `TestCustodyViolationsAreRefused` | OFD and dispatch refused where the parcel is not held |
| `TestHoldStopsTheParcel` | A held shipment cannot transition until released |
| `TestClosedBagContentsAreFrozen` | Trigger refuses mutation of a closed bag, tested through raw SQL as well as the API |
| `TestOperationalHistoryIsAppendOnly` | `shipment_events` and scan history reject UPDATE and DELETE |
| `TestFieldAgentPermissionsAreNarrow` | A delivery agent cannot reach dispatcher, bagging or finance operations |
| `TestPublicTrackingIsRateLimitedAndOpaque` | Rate limit fires; payload carries no internal identifier, employee or financial field |

### Migration round trip

```
clean apply      19 migrations applied
full rollback    19 rolled back, 1 table remaining (schema_migrations)
reapply          97 tables · 492 indexes · 85 triggers · 112 permissions
                 migration checksums validated
```

Seeding surfaced four real constraints doing their job, which is worth
recording as evidence they are live: `scan_events_rejection_recorded` (a
rejection must carry a code), `bag_items_guard` (bulk insert into a CLOSED bag
refused — the seed had to open, fill, then close),
`operational_exceptions_resolution_recorded`, and
`ndr_cases_resolution_recorded`.

---

## 11. Deployment notes

Apply migrations before starting the new binary:

```bash
./migrate up
```

`0013`–`0019` are additive. `0013` alters `shipment_events` and `shipments` with
nullable columns and widened CHECK constraints, so it does not rewrite either
table. `0019` seeds permissions and default NDR reasons for existing
organisations; new tenants get theirs from `provision.Bootstrap`.

Rollback: every migration has a tested `.down.sql`, and the full ladder has been
rolled back and reapplied on a clean database.

New configuration for object storage:

```
STORAGE_DRIVER            s3 | filesystem   (default filesystem)
STORAGE_ENDPOINT          required when driver is s3
STORAGE_ACCESS_KEY        required when driver is s3
STORAGE_SECRET_KEY        required when driver is s3
STORAGE_BUCKET            default courier-os
STORAGE_REGION            default us-east-1
STORAGE_USE_PATH_STYLE    default true
STORAGE_DIR               filesystem driver only
STORAGE_MAX_OBJECT_BYTES  default 8 MiB, accepted range 1 KiB–64 MiB
STORAGE_SIGNED_URL_TTL    default 15m
```

The `filesystem` driver exists for development. Startup **fails** if it is
selected in production — POD evidence written to a container filesystem does
not survive the next deploy. That check is in `config.validate`, so a
misconfigured production deploy refuses to boot rather than silently losing
evidence.

Public tracking mounts outside the authenticated router group with its own
per-IP rate limiter. If Nginx sits in front, it must pass a trustworthy client
address or the limiter will bucket the whole internet together.

### Production build

All three binaries build clean with the Dockerfile's exact flags
(`CGO_ENABLED=0 GOOS=linux -trimpath -ldflags="-s -w"`):

```
api      28,602,552 bytes   ELF 64-bit LSB executable, x86-64, statically linked, stripped
worker   28,233,912 bytes
migrate  10,227,896 bytes
```

Static and stripped, so the Alpine runtime stage needs no libc and carries no
debug symbols.

The image builds and runs. `courier-os:r2`, 106 MB, verified end to end:

| Check | Result |
| --- | --- |
| Image built | `courier-os:r2`, 106 MB |
| Contents | `/app/api`, `/app/worker`, `/app/migrate` |
| Runs as | `uid=10001(courier)`, non-root |
| Migrator against a fresh database | 19 migrations applied → 97 tables, 492 indexes, 112 permissions |
| API boots read-only (`--read-only --tmpfs /tmp`) | container reports `healthy` |
| `GET /livez` | `200 {"status":"alive"}` |
| `GET /readyz` | `200` with database and cache `up` |
| `GET /api/v1/track/{awb}` | `404` with a correct error envelope and `requestId` |
| Config validation | refuses to start on missing `DATABASE_URL` / `JWT_SECRET`, and on `STORAGE_DRIVER=filesystem` in production |

The `HEALTHCHECK` and the read-only root filesystem both work as the compose
file assumes.

### Fixed: the production stack could not start

Booting the image against the real `deploy/docker-compose.prod.yml` environment
surfaced a defect that no unit or integration test could have caught, because it
lives in the deployment manifest rather than in the code:

```
migrate: invalid configuration:
  - STORAGE_DRIVER must be s3 in production; filesystem storage does not survive a container restart
```

Release 2 made object storage a required subsystem — Release 1 had none, per
ADR 0007 — but `x-app-env` in the compose file was never given any `STORAGE_*`
variables. `STORAGE_DRIVER` defaults to `filesystem`, and `config.validate`
refuses that under `APP_ENV=production`. The `migrate` service therefore exits
non-zero, and because `api` and `worker` both declare
`depends_on: migrate: service_completed_successfully`, **the entire production
stack would have failed to start on first deploy.**

Fixed in `deploy/docker-compose.prod.yml` by adding the storage block to
`x-app-env`, with endpoint, bucket and both keys as required variables rather
than defaulted ones — a stack that cannot store proof of delivery should fail at
`compose up`, not at the first delivery. `.env.example` gained the matching
documented block. The failure now surfaces during interpolation:

```
error while interpolating x-app-env.STORAGE_ENDPOINT:
  required variable STORAGE_ENDPOINT is missing a value: STORAGE_ENDPOINT is required
```

### A note on building on macOS with OrbStack

Worth recording, because it cost real time. This host runs OrbStack, whose
Docker CLI config carries two things that matter:

```json
{ "credsStore": "osxkeychain", "currentContext": "orbstack" }
```

Setting `DOCKER_CONFIG` to a scratch directory to avoid the keychain credential
helper — which blocks on a GUI prompt in a non-interactive shell — also discards
`currentContext`, so the CLI falls back to the default context and waits on a
daemon socket that does not exist. The build then hangs with no output at all,
which looks exactly like a wedged daemon and is not one.

Point `DOCKER_HOST` at the socket directly instead of overriding the context:

```bash
export DOCKER_HOST=unix://$HOME/.orbstack/run/docker.sock
docker build -t courier-os:r2 .
```

### Build context

The repository had no `.dockerignore`, and the Dockerfile does `COPY . .`. Every
backend build was therefore shipping the full 292 MB working tree to the Docker
daemon, of which 280 MB is `web/node_modules` — a frontend dependency tree the
Go build has no use for. CI paid that transfer on every run.

Added `.dockerignore` excluding `web/`, `.git/`, `docs/`, test output, and the
`.env*` / key / credential family. The secret exclusions are preventative rather
than a fix: the only env-shaped file present is `.env.example`, a committed
template with no real values, and the image is multi-stage so nothing from the
build stage reaches the runtime layer regardless. The point is that a future
`.env` left in a working tree now cannot be baked into a build layer by
accident.

---

## 12. Known risks and limitations

**Not implemented in this release.**

- *Notifications.* Delivery OTPs and NDR updates are returned to the API
  caller; nothing is sent to a consignee. Until M26, an OTP reaches the customer
  only if an operator relays it. This is the most operationally visible gap.
- *Financial posting.* COD collection and RTO charges are captured as
  operational facts, but no ledger entry, commission or settlement is produced.
  That is Release 3 by design, and the data captured is sufficient to post
  retrospectively.
- *Webhooks and partner API.* Release 4.
- *Route optimisation.* Delivery run stops are ordered as given. There is no
  sequencing or distance optimisation.
- *Manifest documents* render as fixed-width text, not PDF. Generation is
  already asynchronous and stored as an object, so replacing the renderer does
  not change the contract.

**Risks to watch.**

- *Load figures are from development hardware.* Re-run on the VPS before
  go-live. The scan endpoint is the one to watch: it is the highest-volume
  write and the one an operator physically feels.
- *The transition table is the security boundary.* Sixty-three transitions are
  correct today because each was reviewed. A carelessly added row with
  `Custody: CustodyNone` would silently widen what a role can do from anywhere
  in the network. The duplicate-pair panic at init catches collisions but
  cannot judge whether a new rule is safe.
- *Custody override is powerful.* `shipment.override_custody` bypasses physical
  reality by design, for genuine operational exceptions. It is audited and
  requires a reason, but it is granted to operations admins, and its use should
  be reviewed periodically rather than assumed rare.
- *Reconciliation is scan-driven.* A hub that does not scan will show clean
  reconciliations. The system detects mismatches between what was declared and
  what was scanned; it cannot detect parcels nobody looked at.
- *Multi-piece perf coverage.* As noted in §9, the piece-barcode path is
  verified but not in the standing fixture.
- *Redis is optional but assumed.* Losing it degrades rate limiting and
  caching; it holds no authoritative state, and every idempotency guarantee in
  §8 is a database constraint.
- *Object storage has never been exercised against a real S3 endpoint.* The
  `S3Store` is unit-tested and the filesystem driver is proven end to end in the
  image, but no POD artifact has been written to Backblaze, MinIO or AWS from
  this build. The hand-written SigV4 implementation (ADR 0010) is the part to
  watch. Upload one POD against the production bucket as a deployment smoke
  test before taking traffic.

**Explicitly deferred, with the data to support it already captured.** COD
custody events, commission triggers, and settlement-relevant operational facts
are all recorded now — Release 3 reads them rather than needing them
backfilled.

---

## 13. Readiness

Release 2 is functionally complete against its specification and evidenced by
the tests, plans and measurements above. The production image builds, boots
read-only as a non-root user, migrates a clean database and serves traffic.

It is not certified production-ready. Four things stand between this and a
production gate:

1. **Notifications** (M26) — until they exist, a delivery OTP reaches a
   consignee only if an operator relays it by hand. This is the largest gap.
2. **Load profile on the target VPS** — the figures in §9 are from development
   hardware and are not a capacity statement for 4 vCPU / 16 GB.
3. **One POD written to the real object store** — the SigV4 client has never
   spoken to a live S3 endpoint.
4. **Security review against the widened API surface** — 87 new operations have
   module-level tests but have not been through a dedicated review pass.

None of the four is a defect in what was built; each is work not yet done. The
one deployment defect that *was* found — a production stack that could not
start — is fixed and recorded in §11.
