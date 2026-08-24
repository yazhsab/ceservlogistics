# Production Readiness — Release 5

Assessed 9 August 2026, against Hostinger KVM 4 (4 vCPU, 16 GB RAM, 200 GB NVMe,
Ubuntu 24.04). **Revised the same day** — see §0.

Every **PASS** below carries evidence that was executed, not designed. Where a
number was measured on a developer machine rather than on the target, it says
so — an unqualified figure from the wrong hardware is worse than no figure.

**This document does not say the system is 100% production ready**, and that is
deliberate (constitution §41). It says what was tested, what the results were,
and what remains genuinely unknown. §17 is the part to read before deciding.

---

## 0. Revision — a PASS that was wrong

The first version of this document marked **Financial Integrity: PASS**, on the
strength of module-level tests that were all genuinely passing.

It was wrong, and the error is worth recording rather than quietly editing.

While extending the restart drill to cover COD, an interrupted COD collection
needed an obligation to collect against — and after delivering a COD parcel
through the full operational journey there was **no obligation at all**. The
finance core was never connected to operations: `internal/delivery` held zero
references to commission or COD, only notifications and webhooks were registered
as transition observers, and `cod.OpenObligation` and `commission.Calculate`
were called from tests only.

What that meant in production terms:

```
COD parcel delivered, ₦2,500 collected by the agent
  → no cod_obligations row: no record of cash owed, no custody chain,
    nothing to reconcile. The amount existed only on the delivery attempt.

Settlement generated for a month of real deliveries
  → CommissionMinor: 0, CodLiabilityMinor: 0, NetAmountMinor: 0, lines: []
```

For a franchise courier network whose entire commercial model is commission and
COD settlement, that is the product not working. The gap was known and recorded
where nobody would find it — a comment in `cod_test.go` reading *"which is what
the booking path will do once M23 is wired to it"*.

**The lesson is about the shape of the test suite, not the code.** Every module
was tested and every module was right. Nothing tested the *seam*, so a
module-level PASS said nothing about whether the system worked. A readiness
assessment built from component evidence will keep making this mistake.

Now closed by `internal/financeops`, with tests written in the terms a franchise
owner would use: was the cash recorded, was the commission earned, does the
settlement have lines. Details in §6.

---

## Verdict

| | |
|---|---|
| Items assessed | 65 |
| PASS | 55 |
| RISK ACCEPTED | 7 |
| NOT YET VERIFIED | 2 |
| FAIL | 1 |

**The FAIL is that CI has never run** — the repository has no commits, so every
gate in `.github/workflows/ci.yml` is designed rather than demonstrated.
Everything here was executed by hand.

**NOT YET VERIFIED** is a category this document did not have before, and it
exists because §0 showed that "not tested" and "PASS" are easy to confuse. Two
gate items were reasoned about rather than executed: a clean deployment
simulation, and a saturation run to find the load ceiling. They are listed as
what they are.

Readiness in one sentence: **the software is ready for a controlled launch with
low COD volume, and is materially more trustworthy than it was this morning
because the one thing that would have made a franchise network unusable has been
found and fixed; what limits scaling is the operational envelope — a 24-hour
RPO, nothing measured on the real host, and no independent security review.**

---

## 1. Architecture

| Item | Status | Evidence |
|---|---|---|
| Modular monolith, one PostgreSQL | **PASS** | 33 migrations, one database, no service mesh (ADR-0001) |
| No prohibited infrastructure | **PASS** | No Kubernetes, Kafka, Elasticsearch, Mongo. Job queue is PostgreSQL-backed (ADR-0005) |
| Single-host operation | **PASS** | `deploy/docker-compose.prod.yml` — postgres, redis, api, worker, nginx, certbot |
| Redis holds no authoritative state | **PASS** | Cache, rate limits, OTP only. Losing it costs nothing durable (§29) |
| Horizontal scale is possible later | **PASS** | No process-local locks in correctness paths; AWB, invoice numbering and job claims are all database-arbitrated |

## 2. Build

| Item | Status | Evidence |
|---|---|---|
| Production image builds | **PASS** | `courier-os:gate`, **114 MB** |
| Static binaries, stripped | **PASS** | CGO off, `-trimpath -ldflags="-s -w"`; api 28 MB, worker 28 MB, migrate 12 MB |
| One image, three binaries | **PASS** | API and worker in a deployment are provably the same build |
| Non-root runtime | **PASS** | `uid=10001(courier) gid=10001(courier)` |
| Toolchain pinned | **PASS** | `go1.25.12` in `go.mod`, Dockerfile and CI. Was a floating `1.25` tag — see SECURITY_REVIEW M-1 |
| Base image pinned | **PASS** | `golang:1.25.12-alpine` build, `alpine:3.22` runtime |
| Reproducible | **PASS** | Version, commit and build time are build args, surfaced in `/version` and in `courier_app_build_info` |

## 3. Tests

| Item | Status | Evidence |
|---|---|---|
| Full suite green | **PASS** | All packages pass, `-count=1 -p 1` |
| Race detector | **PASS** | CI runs `go test -race` |
| Integration over real HTTP | **PASS** | Every test drives the real chi router against a real PostgreSQL |
| Tenant-isolation tests | **PASS** | §5 |
| Permission tests | **PASS** | §5 |
| Concurrency tests | **PASS** | Concurrent booking, delivery, invoice numbering, journal posting |
| Idempotency tests | **PASS** | Booking, COD, commission, settlement, partner API, webhooks |
| Migration up/down/reapply | **PASS** | 33 migrations apply, roll back and reapply on a clean database |
| No skipped critical tests | **PASS** | 3 `t.Skip` calls, all environment guards, none on a business path |
| **CI has ever executed** | **FAIL** | The repository has no commits. See Verdict |

## 4. Security

Full detail in `docs/SECURITY_REVIEW.md`.

| Item | Status | Evidence |
|---|---|---|
| Dependency vulnerabilities | **PASS** | `govulncheck`: **24 reachable → 0** |
| SQL injection | **PASS** | 56 probes across 7 searchable surfaces; all inert, no 5xx, no database detail leaked |
| Stored injection | **PASS** | `Robert'); DROP TABLE shipment_events; --` stored and returned verbatim; table intact |
| XSS reflection | **PASS** | JSON-only, `nosniff`, `default-src 'none'` CSP; payloads escaped |
| Security headers on 200/401/404/public | **PASS** | All four checked explicitly |
| CORS reflection | **PASS** | 4 hostile origins refused, including suffix-matching attacks |
| CSRF | **PASS** (N/A) | No cookie authentication anywhere in `internal/` |
| Path traversal | **PASS** | 4 encodings × 3 route families; two independent defences |
| Upload MIME spoofing | **PASS** | HTML/shell/PHP/ELF/SVG declaring `image/*` all refused; a real PNG accepted |
| Rate-limit bypass | **PASS** | Forged `X-Forwarded-For` from an untrusted peer ignored (11 unit cases); per-account budget stops a rotating-IP attack |
| Brute force | **PASS** | Throttled at attempt 11; throttle runs **before** Argon2, so it cannot be used as a CPU amplifier |
| Token forgery and replay | **PASS** | Replaced signature, empty signature, cross-tenant use, org-context pivot — all refused |
| Financial idempotency abuse | **PASS** | Reused key with altered body never applies the alteration |
| Error hygiene | **PASS** | No stack traces, SQL state, or paths in any of 6 malformed-request probes |
| Secrets in code | **PASS** | No literal secrets outside tests |
| Log redaction | **PASS** | 10 key patterns redacted; no bodies, no query strings, no personal data |
| Production config validation | **PASS** | Verified in-container: a bad production config **refuses to start** with 4 named errors |
| Independent penetration test | **RISK ACCEPTED** | Self-assessment only. See §17 |

## 5. Tenancy

| Item | Status | Evidence |
|---|---|---|
| Cross-tenant read/write | **PASS** | Refused across every resource family |
| Tenant derived server-side | **PASS** | From the principal; client-supplied `organization_id` never used for authorization |
| Operating-unit scope | **PASS** | Enforced on listings and mutations |
| IDOR / BOLA | **PASS** | Opaque ULID public ids; ownership checked before every read |
| Mass assignment | **PASS** | Unknown JSON fields rejected — confirmed live during load testing, when an invented field produced 422 |
| Privilege escalation | **PASS** | Role assignment cannot exceed the granter |
| Partner keys are scope-only | **PASS** | A partner principal holds **no permissions**, so a credential cannot acquire a person's rights |
| Custody enforcement | **PASS** | Wrong-facility transitions refused |
| Franchise lookup is tenant-scoped | **PASS** *(fixed)* | `GetFranchiseByOperatingUnit` was `WHERE operating_unit_id = $1` with no organization predicate. One of six callers compensated after the fact; five did not. Now enforced in the query |

## 6. Financial integrity

| Item | Status | Evidence |
|---|---|---|
| **Finance is connected to operations** | **PASS** *(was broken — §0)* | Delivering a COD parcel opens the obligation; delivering for a franchise raises and posts commission; a settlement for real work has lines and a non-zero total |
| Double-entry invariant | **PASS** | Enforced by constraint and re-verified in the restored database; **a positive control proves the check detects a violation**, not just that it returns 0 |
| Integer minor units | **PASS** | No float in any monetary path |
| Posted journals immutable | **PASS** | Trigger-enforced; corrections are reversals |
| Money is not duplicated by retries | **PASS** | Booking: 12 keys → exactly 12 shipments across a SIGKILL and two retry rounds |
| Interrupted delivery | **PASS** | 1 attempt, 1 DELIVERED event |
| Interrupted COD collection | **PASS** | 1 collection, obligation not over-collected |
| Interrupted commission | **PASS** | 1 calculation, ₦42.50 posted — not twice, not zero |
| Interrupted settlement generation | **PASS** | 1 settlement, no orphaned lines |
| Ledger survives every interruption | **PASS** | Journals balance and no orphaned entries after each of the four |
| Idempotency is database-backed | **PASS** | `idempotency_keys` with a unique constraint, not Redis (§21) |

The observer that closes the seam is deliberately **the one observer allowed to
fail a transition**. The alternative was tempting — swallow the error, mark the
parcel delivered, let finance catch up — and it produces a delivered parcel with
no record of the cash the agent is holding, which is the worst state this system
can reach. If the liability cannot be written, the delivery does not complete
and the agent retries.

## 7. Performance

Measured against **150,025 shipments / 167 MB**, mixed workload, 3 minutes.

| Item | Status | Evidence |
|---|---|---|
| Mixed workload, 14 scenarios | **PASS** | 15,675 requests, **1.16% failed** (all the expected 403 from a portal the admin correctly cannot see) |
| Latency | **PASS** | p50 **3.1 ms**, p95 **25.5 ms** |
| Booking throughput | **PASS** | 901 bookings in 3 minutes at 5/s arrival, p95 **31 ms** |
| Tracking (highest volume) | **PASS** | p95 **2.4 ms** at 30/s |
| Scanning (busiest write) | **PASS** | p95 **11.7 ms** at 15/s |
| Hub dashboard (heaviest read) | **PASS** | p95 **42.9 ms** |
| Settlement reads | **PASS** | p95 **10.4 ms** |
| Login | **PASS** | p95 **98 ms** — Argon2id, deliberately expensive |
| Memory | **PASS** | RSS **184–282 MB** across 50 samples, returning to 184 MB — no growth |
| CPU | **PASS** | mean **30.5%** of one core, peak 59.9% |
| Connection pool | **PASS** | Never saturated |
| Slow queries | **PASS** | Re-measured at 72,793 shipments / 60,000 COD obligations: every workload query **under 6 ms**, most sub-millisecond. `courier_db_slow_queries_total` stayed **0** across the whole saturation ramp. `docs/releases/explain-analyze-release-5.md` |
| Index hygiene | **PASS** | No index added. A candidate was measured, shown to make no difference, and dropped — see that document §3 for why the apparent 39 ms defect was a bad measurement |
| Unbounded queries | **PASS** | Every list endpoint paginates; place search bounded at 50 |
| Restart safety | **PASS** | `restart-drill.sh`: **11 checks, 0 failures** |
| Graceful shutdown | **PASS** | SIGTERM mid-burst: **10/10 in-flight requests completed with 201**, drained in **272 ms** |
| **Load ceiling** | **PASS** — *no knee found* | Ramped to **241.6 req/s** sustained: **1.15% failed**, p50 **1.9 ms**, p95 **16.6 ms**, max 94.8 ms. Nothing degraded, so the ceiling on this hardware is above 241 req/s — see §7a |
| Under-load Go heap, goroutines, FDs, pool, locks, Redis, queue, disk | **PASS** | 60 samples at 5 s through the ramp — §7a |
| **Measured on target hardware** | **RISK ACCEPTED** | All figures from an M-series Mac under OrbStack. Assume 3–5× slower on KVM 4 |

### 7a. The saturation run

Ramped to 6× the steady mix and **did not find a knee**. 72,948 requests at
241.6/s aggregate, 1.15% failed — and that 1.15% is entirely the franchise
portal correctly returning 403 to an operations admin who does not hold
`portal.franchise`.

Every resource, sampled every 5 s across the ramp:

| Measure | Min | Max | Final | Headroom |
|---|---|---|---|---|
| RSS | 182 MB | **185 MB** | 184 MB | 768 MB limit — 4× |
| CPU | 0% | **95.6%** of one core | 0% | 4 vCPU — the first real constraint |
| Goroutines | 14 | 183 | 179 | no leak: returns to baseline between ramps |
| Go heap | 69.8 MB | **74.6 MB** | 70.8 MB | flat under 6× load |
| Open FDs | 37 | 202 | 202 | far below any limit |
| **DB pool acquired** | 0 | **4** | 1 | **of 20 — 80% idle at peak** |
| Slow queries | 0 | **0** | 0 | none crossed 250 ms |
| Redis | 48 MB | 65 MB | 65 MB | 384 MB cap — 6× |
| Worker queue depth | 0 | 0 | 0 | never backed up |
| Ungranted PG locks | 0 | **0** | 0 | no contention |
| Disk | 12% | 12% | 12% | flat |

Two of these are worth reading twice.

**The pool peaked at 4 of 20.** The bounded pool was sized on the assumption it
would be the constraint on a 4 vCPU host. At six times the expected mix it is
80% idle, which says the queries are fast enough that connections are returned
before the next request needs one. **Raising `DATABASE_MAX_CONNS` would achieve
nothing** — and lowering it toward 10 would free PostgreSQL memory without
touching throughput. That is the opposite of what §16 assumed.

**Zero slow queries and zero lock contention** across the whole ramp. The
250 ms threshold was never crossed by any statement.

CPU is the first thing that will bind: 95.6% of one core at 241 req/s, with
three more cores available to a process that is single-instance by design. The
practical ceiling on this hardware is therefore well above what was measured,
and finding it would need a load generator and a host that are not the same
machine.

**Caveat, unchanged and important:** this is an M-series Mac under OrbStack. A
KVM 4 will be slower — assume 3–5× — which still leaves the measured mix
comfortable. The *shape* of the result transfers even if the number does not:
CPU binds first, the pool does not bind at all.

## 8. Database

| Item | Status | Evidence |
|---|---|---|
| Migrations reversible | **PASS** | 33 up/down/reapply verified |
| Bounded pool | **PASS** | `DATABASE_MAX_CONNS=20`, min 2. Measured peak under 6× load: **4 acquired**. Sized conservatively; §7a suggests 10–12 would do |
| Statement timeout | **PASS** | 15 s server-side; idle-in-transaction 30 s |
| Slow-query visibility | **PASS** | pgx tracer times **every** statement; histogram + log past 250 ms |
| Indexes based on access patterns | **PASS** | Partial indexes sized to the live backlog, not history |
| Keyset pagination on hot tables | **PASS** | |
| Uncalled queries | **RISK ACCEPTED** | ~132 of 792 sqlc queries have no caller. Dead weight, not a defect; needs per-module triage |

## 9. Redis

| Item | Status | Evidence |
|---|---|---|
| Not a system of record | **PASS** | §29 respected |
| Loss is survivable | **PASS** | DR §3.3 — restart, nothing to restore |
| Bounded pool | **PASS** | `REDIS_POOL_SIZE=16` |
| Health surfaced | **PASS** | `courier_app_dependency_up{dependency="redis"}` |
| maxmemory policy | **PASS** | Set in compose; see §16 |

## 10. Storage

| Item | Status | Evidence |
|---|---|---|
| Objects outside PostgreSQL | **PASS** | S3-compatible; database holds metadata and checksum |
| Production refuses filesystem storage | **PASS** | Startup error, verified in-container |
| Upload validation | **PASS** | Content sniffing, allowlist, size cap, generated keys |
| Path traversal | **PASS** | `..` refused outright, then `filepath.Rel` confirms containment |
| Object storage backup | **RISK ACCEPTED** | Relies on provider durability. Enable versioning + a no-delete credential |

## 11. Backup

| Item | Status | Evidence |
|---|---|---|
| Scheduled | **PASS** | `backup.sh`, cron 02:00 UTC |
| Compressed | **PASS** | `pg_dump -Fc --compress=6`: 167 MB → **4.8 MB** |
| Encrypted | **PASS** | age (or gpg); **warns loudly when unconfigured** |
| Off-server | **PASS** | rclone, with `lsf` confirmation rather than trusting an exit code |
| Retention | **PASS** | 7 days local, 30 remote |
| Verified at creation | **PASS** | `pg_restore --list` on every dump; refuses under 40 tables or 4 KB |
| Checksummed | **PASS** | SHA-256 over the plaintext, stored alongside |
| Alerts if it stops running | **PASS** | Heartbeat pinged **only on success**, so a missing ping is the alert |

## 12. Restore

| Item | Status | Evidence |
|---|---|---|
| Restore script exists | **PASS** | `restore.sh`, refuses the live database without `--force` and while the API runs |
| **Restore actually executed** | **PASS** | 150,025 shipments restored in **6 s**; 144 tables, schema 33, 0 orphans, journals balance |
| Automated drill | **PASS** | `restore-test.sh`, monthly, fails on a stale backup as well as a bad one |
| Drill found real defects | **PASS** | Two: `pg_restore --jobs` is incompatible with stdin, and the verification query named a column that does not exist. Both invisible until executed |
| Restore tested on target hardware | **RISK ACCEPTED** | Run the drill once on the real VPS and record the number |

## 13. Monitoring

| Item | Status | Evidence |
|---|---|---|
| Structured JSON logs | **PASS** | |
| Request IDs end to end | **PASS** | In logs, in `X-Request-Id`, in every error envelope |
| Route latency, status, 5xx | **PASS** | Histogram by route pattern; status as a class |
| DB pool stats | **PASS** | acquired/idle/total/max, sampled every 15 s |
| Slow-query logging | **PASS** | Verified live |
| Redis availability | **PASS** | `dependency_up`, 2 s bounded probe |
| Worker queue depth and failures | **PASS** | |
| Application version | **PASS** | `courier_app_build_info`, set before any dependency opens |
| Business metrics | **PASS** | bookings, scans, deliveries, NDR, COD, commission, settlement — failures instrumented on a **named error result**, so a new return path is counted automatically |
| Log rotation | **PASS** | 50 MB × 5 per container |
| Metrics endpoint not public | **PASS** | Loopback-bound, not proxied |
| Monitoring stack | **RISK ACCEPTED** | Deliberately not on this host. Options in OBSERVABILITY §6 |

## 14. Deployment

| Item | Status | Evidence |
|---|---|---|
| Health endpoint | **PASS** | `/livez` — process only, so a database blip cannot cause a restart loop |
| Readiness endpoint | **PASS** | `/readyz` — PostgreSQL and Redis |
| Container healthcheck | **PASS** | 30 s interval, 10 s start period |
| Graceful shutdown | **PASS** | 272 ms, 10/10 in-flight completed |
| Migrations are an explicit step | **PASS** | Never implicit on boot, so a rolling restart cannot apply DDL |
| Schema verified at startup | **PASS** | Refuses to serve against an unexpected schema |
| Env validation | **PASS** | 4 production misconfigurations refused at boot |
| Resource limits | **PASS** | Per-service CPU and memory in compose |
| TLS | **PASS** | Nginx + certbot; HSTS behind a flag |
| **Clean deployment simulation** | **PASS** | Full stack from nothing in **14 s**, all seven services healthy. See §14a — it found two defects that would each have broken production |
| Zero-downtime rolling restart | **PASS** | 120 consecutive probes through nginx during `--force-recreate` of the API: **120/120 returned 200** |
| Request id spans proxy and app | **PASS** | The same id appears in the nginx access log and the API log for one request |
| Backup and restore against the live stack | **PASS** | `backup.sh` → 144 tables verified; `restore-test.sh` → PASS |
| Disk monitoring | **RISK ACCEPTED** | Documented in the runbook; no automated alert on this host |

### 14a. What the deployment simulation found

The stack had never been started as a stack. Building the image and checking its
runtime properties — which is what the first pass did — proved the image and
nothing about the deployment. Two defects were waiting, and both would have been
discovered on the VPS at the worst moment.

**1. Nginx could never start.** `nginx.conf` includes
`/etc/nginx/proxy_params_courier`. The file exists in `deploy/nginx/` and the
compose file never mounted it:

```
[emerg] open() "/etc/nginx/proxy_params_courier" failed (2: No such file or directory)
```

Nginx crash-looped. **The entire ingress was down** — not one route, all of them,
including TLS termination. Fixed by mounting the file.

**2. The worker reported unhealthy forever.** It inherited the image's
`HEALTHCHECK`, which probes `:8080/livez` — the API's port. The worker serves no
HTTP, so a worker that had started cleanly, connected to PostgreSQL and Redis
and was polling five queues was permanently red.

Chasing that turned up something worse: **every `courier_jobs_*` metric was
invisible.** Those metrics are recorded by whichever process runs the job, which
is the worker, and the worker had no scrape endpoint. Queue depth, worker
failures, in-flight count — all recorded into a registry nobody could reach. The
observability document claimed them as available.

Fixed by giving the worker its own listener on `WORKER_HEALTH_ADDR`
(default `:8090`) serving `/livez`, `/readyz` and `/metrics`, with a compose
healthcheck that probes it.

**3. `/version` was documented but unreachable.** Nginx serves `/livez`,
`/readyz` and `/api/`, blocks `/metrics`, and 404s everything else. `/version`
fell into "everything else". Blocking it is the *right* posture — publishing the
running build to the internet only helps somebody matching CVEs — so the fix was
to remove it from the public contract and document it as internal-only, not to
proxy it.

The pattern is the same one as §0: **`TestEveryDocumentedPathIsRoutable` tests
against the application, not through the proxy**, so it could not have caught
this. Every layer was individually correct.

---

## 15. Rollback

| Item | Status | Evidence |
|---|---|---|
| Image rollback | **PASS** | `IMAGE_TAG=<previous> docker compose up -d api worker` |
| Running version identifiable | **PASS** | `courier_app_build_info` |
| Backup before migration | **PASS** | In `deploy.sh` |
| Migration rollback strategy documented | **PASS** | DR §3.6 — additive vs destructive, and why forward-fix is usually safer |

## 16. Capacity — the KVM 4 profile

16 GB total. The allocation below leaves **~4 GB of OS page cache**, which is
not slack: PostgreSQL's `shared_buffers` is only 2 GB, and the page cache is
what keeps the working set off disk.

| Component | Memory | Setting | Why |
|---|---|---|---|
| PostgreSQL | 4 GB limit | `shared_buffers=2GB`, `effective_cache_size=6GB`, `work_mem=8MB` | 25% of RAM in shared buffers is the standard starting point; `effective_cache_size` tells the planner what the OS is caching |
| API | 768 MB | `DATABASE_MAX_CONNS=20`, `REDIS_POOL_SIZE=16` | Measured peak RSS **282 MB** under mixed load — 2.7× headroom |
| Worker | 512 MB | `WORKER_CONCURRENCY=4` | One per vCPU |
| Redis | 512 MB | `maxmemory 384mb`, `allkeys-lru` | Cache only; eviction is correct behaviour here |
| Nginx | 128 MB | | |
| OS + page cache | ~4 GB | | **Do not consume this** |

**On the connection pool.** 20 API + a worker share `max_connections=100`. The
pool is deliberately far below what PostgreSQL would accept: 4 vCPU cannot
usefully execute more than a handful of queries at once, and a larger pool
converts a slow query into a thundering herd instead of a queue. Pool saturation
is the leading indicator to alert on (OBSERVABILITY §4).

**On processes.** One API process, not several. Go serves concurrent requests on
one process, and multiple replicas on one host would multiply the pool against
the same database for no gain.

**On the rate limit.** The default is 600/min per IP. **This is a real
consideration for a franchise branch behind one NAT**: ten scanners at a busy
hub will exceed it. Load testing hit exactly this — a single-IP generator was
throttled at 10 req/s and produced a 90% failure rate until the limit was
raised. Either raise `RATE_LIMIT_PER_MINUTE` for known branch addresses at
Nginx, or accept that a busy branch needs a higher budget.

### Observed headroom

At the measured mix (~87 req/s aggregate) the API used **30.5% of one core** and
282 MB. Extrapolating at 3–5× slower on the target still leaves the CPU
comfortable. **The first constraint you will meet is PostgreSQL I/O on
dashboard aggregates, not the Go process.**

---

## 17. Known risks

Ordered by how much they should influence the launch decision.

**1. The test suite proves modules, not seams.** §0 is the reason this is now
first. Every module was tested, every module was right, and the system did not
work — because nothing exercised the joins between modules. One such seam has
been found and closed. **There is no evidence about the others**, and the ones
worth checking next are: NDR → RTO, POD → delivery completion, billing →
invoicing from shipments, and pickup → booking. Each is a place where two
correct modules could be sitting side by side, unconnected.

**2. The 24-hour RPO.** A failure at 01:59 loses a full day of shipments and COD
custody records. Parcels physically exist so operations can be reconstructed,
but it is a genuinely bad day. **Enable WAL archiving before COD volume becomes
material.**

**3. No independent security review.** Everything in `SECURITY_REVIEW.md` is a
self-assessment by the engineer who wrote the code. The tests are real; a review
shares its author's blind spots — as §0 demonstrated.

**4. Nothing has been measured on the target hardware.** Every latency, memory
and restore figure comes from a developer machine. Directionally sound, wide
margins, but **run the load profile and the restore drill once on the real VPS**
and replace these numbers.

**5. Two gate items were never executed** — a clean deployment simulation and a
saturation run. Listed as NOT YET VERIFIED rather than folded into a PASS.

**6. CI has never run.** The pipeline is designed, not demonstrated.

**7. Two API surfaces accept input they ignore.** `webhook_subscriptions.filter`
is stored and never read; report `format: JSON` always produces CSV. Phase 2
closed the former event-catalogue gap by adding transactional emitters for
`pickup.completed`, `pod.captured`, and `cod.collected`.

**8. No real email provider.** Termii covers SMS and WhatsApp. Email — the
cheapest channel and the bulk of the intended mix — has only a logging sender.

**9. Single host, no failover.** By design (§2). A host failure is a restore,
not a failover; RTO ~1 hour.

**10. Host hardening is out of scope.** SSH, firewall, kernel updates and
fail2ban are the operator's responsibility and are not verified here.

## 18. Commands

### Deploy

```bash
ssh root@your-vps && cd /opt/courier-os
git pull --ff-only
./deploy/scripts/backup.sh                      # before anything else
export IMAGE_TAG=$(git rev-parse --short HEAD)
docker compose -f deploy/docker-compose.prod.yml build \
  --build-arg VERSION="${IMAGE_TAG}" \
  --build-arg GIT_COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
docker compose -f deploy/docker-compose.prod.yml run --rm migrate up
docker compose -f deploy/docker-compose.prod.yml up -d api worker nginx
```

### Verify

```bash
curl -sf http://localhost:8080/livez  && echo LIVE
curl -sf http://localhost:8080/readyz && echo READY
curl -s  http://localhost:9090/metrics | grep courier_app_build_info
curl -s  http://localhost:9090/metrics | grep courier_app_dependency_up
docker compose -f deploy/docker-compose.prod.yml ps
```

### Smoke test

```bash
BASE=https://api.example.com
TOKEN=$(curl -s -X POST $BASE/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@yourorg.com","password":"..."}' | jq -r .tokens.accessToken)

curl -s "$BASE/api/v1/geography/places?q=Ikeja" -H "Authorization: Bearer $TOKEN" | jq '.data[0]'
curl -s "$BASE/api/v1/serviceability/check?originPincode=100001&destinationPincode=900001&serviceCode=EXPRESS" \
  -H "Authorization: Bearer $TOKEN" | jq '{serviceable, slaHours}'
curl -s -X POST "$BASE/api/v1/pricing/quote" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"originPincode":"100001","destinationPincode":"900001","serviceCode":"EXPRESS",
       "paymentMode":"PREPAID","packages":[{"actualWeightGrams":1000}]}' | jq '.totalMinor'
curl -s "$BASE/api/v1/shipments?limit=1" -H "Authorization: Bearer $TOKEN" | jq '.data | length'
curl -si "$BASE/api/v1/shipments" -H "Authorization: Bearer $TOKEN" | grep -i x-request-id
```

### Backup and restore

```bash
./deploy/scripts/backup.sh
./deploy/scripts/restore.sh /opt/courier-os/backups/courier_os_YYYY.dump.age courier_os_check
./deploy/scripts/restore-test.sh
AGE_IDENTITY=/root/age-key.txt ./deploy/scripts/restore.sh <backup> courier_os --force  # live, API stopped
```

### Rollback

```bash
export IMAGE_TAG=<previous>
docker compose -f deploy/docker-compose.prod.yml up -d api worker
curl -s http://localhost:9090/metrics | grep courier_app_build_info
```

### Re-run the gate

```bash
govulncheck ./...
go test ./... -count=1 -p 1
./deploy/scripts/restart-drill.sh ./api "$DATABASE_URL"
k6 run -e BASE_URL=... -e EMAIL=... -e PASSWORD='...' \
       -e CUSTOMER_ID=cus_... -e UNIT_ID=ou_... \
       tests/load/courier-production-gate.js
```

---

## 19. Environment variables

97 of 99 documented in `.env.example` (`GIT_COMMIT` and `BUILT_AT` are build
args). Required in production:

```bash
APP_ENV=production
DATABASE_URL=postgres://courier:PASSWORD@postgres:5432/courier_os?sslmode=disable
REDIS_URL=redis://redis:6379/0
JWT_SECRET=                       # >= 32 bytes, from a password manager
CORS_ALLOWED_ORIGINS=https://app.example.com    # never '*'
RATE_LIMIT_ENABLED=true                          # refuses to start if false
STORAGE_DRIVER=s3                                # refuses filesystem
STORAGE_BUCKET= STORAGE_ENDPOINT= STORAGE_ACCESS_KEY= STORAGE_SECRET_KEY=
MARKET_COUNTRY=NG MARKET_CURRENCY=NGN MARKET_TIMEZONE=Africa/Lagos
TERMII_API_KEY= TERMII_SENDER_ID=
PUBLIC_TRACKING_URL=https://track.example.com
TRUSTED_PROXY_CIDRS=172.16.0.0/12               # the Nginx container network
DATABASE_MAX_CONNS=20 WORKER_CONCURRENCY=4
DATABASE_SLOW_QUERY_THRESHOLD=250ms
ENABLE_METRICS=true METRICS_ADDR=127.0.0.1:9090
```

**`TRUSTED_PROXY_CIDRS` is a security control, not a convenience.** Set it to
the Nginx container network and nothing wider: anything inside it can choose its
own rate-limit identity.

---

## 20. Recommendation

Deploy to production **for a controlled launch** — a small number of branches,
low COD volume, close monitoring for the first weeks.

The system is materially more trustworthy than it was before §0, because the one
defect that would have made a franchise network unusable has been found and
fixed. It is also, for the same reason, a system whose readiness evidence should
be read with more suspicion than before: a module-level PASS was wrong once
today.

Before scaling beyond a controlled launch, in order:

1. **Audit the remaining module seams** (§17.1). This is now the highest-value
   item on the list, above anything infrastructural. One seam was broken; the
   others have never been checked.
2. Enable WAL archiving. Take the RPO from 24 hours to minutes.
3. Run the load profile, the saturation run and the restore drill on the real
   VPS; replace §7 and §12 with those numbers.
4. Run the clean deployment simulation.
5. Push and let CI run.
6. Commission an independent security review.
7. Close the three accept-but-ignore surfaces in §17.7, or remove them from the
   contract.
