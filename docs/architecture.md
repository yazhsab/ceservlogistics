# Architecture

## What this is

A multi-tenant Courier Operating System for a franchise-based courier network.
The backend is the source of truth for every business and financial rule; the
frontend consumes it through `docs/openapi.yaml`.

## Shape

A **modular monolith**: one Go module producing three binaries against one
PostgreSQL database, with Redis as a cache. See
[ADR 0001](adr/0001-modular-monolith-on-one-postgres.md) for why.

```
                    ┌───────── Nginx (TLS, edge limits) ─────────┐
                    │                                            │
              ┌─────▼─────┐  ┌───────────┐              ┌────────▼────────┐
              │  api x2   │  │  worker   │              │  /metrics       │
              │  :8080    │  │           │              │  (loopback)     │
              └─────┬─────┘  └─────┬─────┘              └─────────────────┘
                    │              │
        ┌───────────┴──────────────┴───────────┐
        │                                      │
  ┌─────▼──────┐                        ┌──────▼──────┐
  │ PostgreSQL │  system of record      │ Redis       │  cache only
  │            │  every business fact   │             │  rebuildable
  └────────────┘                        └─────────────┘
```

`api` serves HTTP. `worker` runs background jobs and periodic maintenance.
`migrate` applies schema changes and performs first-run provisioning. All three
are built from the same source, so a deployment is provably consistent.

## Layers

```
cmd/                     process entry points: wiring, signals, shutdown
  api/ worker/ migrate/

internal/api/            the only place that knows the full module graph
                         routing, middleware order, operational endpoints

internal/<module>/       business modules: auth, tenant, network, geography,
                         serviceability, product, pricing, customer, shipment,
                         audit. Each owns its domain types, service, handlers,
                         validation and authorization policy.

internal/dbgen/          sqlc-generated query code. Never edited by hand; CI
                         regenerates and fails on a diff.

internal/platform/       infrastructure with no business knowledge:
                         config database cache httpx logging apierr telemetry
                         idempotency security jobs validate publicid money
                         pagination provision

db/migrations/           versioned SQL, embedded into the binary
db/queries/              sqlc source of truth for all static SQL
```

Dependencies point one way: `cmd` → `api` → modules → platform. A module may
call another module's *service* (booking calls pricing and routing) but never
its storage. Nothing depends on the router, so a module is testable on its own.

## The four platform rules everything obeys

### 1. The tenant comes from the token, never the request

`internal/tenant.Principal` is built only by the authentication middleware from
the authenticated session. Every tenant-owned query carries
`organization_id = $principal`. A client-supplied `organizationId` is rejected
as an unknown field.

The single exception is a platform operator (`is_super_admin`) supplying
`X-Organization-Context`. It is validated, restricted to super admins, and every
audit record written under it is flagged `impersonatedByPlatformOperator`.

Out-of-scope objects return **404, not 403**, so the API cannot be used to probe
which identifiers exist elsewhere.

### 2. Money is integer minor units

`internal/platform/money` is the only arithmetic. No float ever touches an
authoritative amount. Percentages are integer basis points. Rounding is HALF_UP
away from zero, applied once per derived line item, and overflow panics rather
than wrapping — inside a transaction that will roll back.

### 3. History is append-only, enforced by the database

`shipment_events`, `audit_events`, `customer_credit_entries` and the three
shipment snapshot tables carry a trigger that rejects `UPDATE` and `DELETE`.
Not a convention — a constraint. An activated rate card version is immutable the
same way.

### 4. Correctness comes from PostgreSQL, not from application locks

- AWB allocation: one atomic `INSERT … ON CONFLICT DO UPDATE … RETURNING`, with
  `UNIQUE(awb)` as the backstop.
- Idempotency: `UNIQUE(organization_id, endpoint, key)` plus a completion write
  inside the business transaction.
- State transitions: a compare-and-swap on `current_status`.
- Credit: `SELECT … FOR UPDATE` on the credit profile.
- Overlapping weight slabs: a GiST exclusion constraint.
- Concurrent edits: a `version` column and `expectedVersion` on updates.

Nothing depends on a process-local mutex, so a second API replica changes
nothing.

## Request lifecycle

```
Nginx → RequestContext (correlation id, client IP against the proxy allowlist)
      → Recoverer (panic → redacted 500, stack to logs)
      → SecurityHeaders → CORS allowlist
      → AccessLog + metrics
      → BodyLimit → Timeout
      → Authenticate (JWT → session cache → Principal)
      → RateLimit (keyed by session, or by IP when anonymous)
      → RequirePermission
      → handler
```

Handlers return `error`; one place renders it through the error envelope. No
handler can invent its own error shape.

## The booking transaction

The most important path in Release 1, and the shape every later mutation
follows.

**Phase 1 — prepare (no transaction, pooled connections):** resolve customer,
sender and recipient, validate packages against the product envelope, check
serviceability, resolve the route, price it, allocate the AWB.

**Phase 2 — commit (one transaction):** insert the shipment, its packages, the
address, charge and route snapshots, reserve credit, append the `BOOKED` event,
write the audit record, and complete the idempotency key.

The split is deliberate and load-bearing. Acquiring a pooled connection while
already holding a transaction deadlocks the pool once concurrency exceeds pool
size — that failure was found by the 120-concurrent-booking test and is what
`idempotency.Preparer` exists to prevent. It also keeps the transaction short,
so contention stays low.

## Determinism in routing

Two calls with the same inputs against the same configuration must produce the
same answer, on any replica. That is achieved by:

- every candidate query ending its `ORDER BY` with `id`, so ties never depend on
  physical row order;
- one fixed precedence, encoded once: override → service route → generic route →
  fallback → local delivery;
- a decision trace recorded for every resolution and stored permanently on the
  shipment, so a past answer can be defended months later.

## Caching

Redis holds only rebuildable state: resolved sessions (30 s), PIN code lookups
and zone assignments (5 min), routing explanations (1 h), rate-limit counters and
login throttles.

Every read degrades to PostgreSQL on a miss or an outage. Invalidation is
explicit — changing a zone mapping, a service area, a route or a user's roles
drops the affected keys immediately rather than waiting for a TTL.

Redis is never the system of record for anything.

## Resource budget (4 vCPU / 16 GB)

| Component | Memory | Notes |
|---|---|---|
| PostgreSQL | 4 GB | 2 GB shared_buffers, `max_connections=100` |
| api x2 | 768 MB each | 20 pool connections each |
| worker | 512 MB | 8 pool connections, concurrency 4 |
| Redis | 512 MB | capped, `allkeys-lru`, no persistence |
| Nginx | 128 MB | |
| **Total** | **~6.6 GB** | ~9 GB left for the OS, page cache and backups |

48 of 100 database connections are used at full deployment, leaving headroom for
migrations, backups and an operator session.

## Observability

Structured JSON logs with an automatic redaction pass over sensitive keys.
Every request carries a correlation id echoed as `X-Request-Id` and recorded on
every audit row. Prometheus metrics on a loopback-bound port: request rate and
latency by route, database pool utilisation, job queue depth, cache hit rate and
business counters. OpenTelemetry tracing is available and off by default,
sampled at 5% when enabled.

## What Release 1 deliberately does not have

Object storage ([ADR 0007](adr/0007-object-storage-deferred-to-release-2.md)),
notifications, webhooks, PDF rendering, the double-entry ledger, COD custody,
commission and settlement. The interfaces those need — the job queue, the audit
recorder, the snapshot tables, the state machine — are in place, and the
shipment status enum already declares every Release 2 state.
