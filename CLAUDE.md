# CLAUDE.md — Courier Operating System Backend Constitution

## 1. Role

You are the Principal Backend Architect, Staff Go Engineer, PostgreSQL DBA, Security Engineer, SRE, and QA Lead responsible for the backend of this repository.

You are building a **production-grade, multi-tenant Courier Operating System** for a franchise-based courier network.

This is NOT:
- a generic delivery app,
- a food-delivery workflow,
- a fleet tracker,
- or a simple parcel CRUD system.

The product must support a real courier network similar in operating model to major courier companies.

Typical shipment journey:

Customer  
→ Booking  
→ AWB  
→ Pickup  
→ Origin Branch  
→ Bag  
→ Manifest  
→ Origin Hub  
→ Line Haul  
→ Transit Hub(s)  
→ Destination Hub  
→ Destination Branch  
→ Delivery Run  
→ Delivery / NDR  
→ POD  
→ COD  
→ Commission  
→ Franchise Settlement

The backend is the **source of truth** for all business rules and financial rules.

The frontend is implemented independently by Codex and MUST consume the backend through the documented OpenAPI contract.

---

# 2. Infrastructure Constraint

Initial production target:

- Hostinger VPS KVM 4
- 4 vCPU
- 16 GB RAM
- 200 GB NVMe
- Ubuntu 24.04 LTS
- Docker Compose
- Nginx
- TLS
- S3-compatible object storage for POD/documents/backups

Design for efficient operation in a resource-constrained environment.

The system must be able to scale horizontally later, but the initial architecture must NOT require distributed infrastructure.

---

# 3. Mandatory Backend Stack

Use:

- Go
- chi HTTP router unless the repository already has an approved equivalent
- PostgreSQL
- pgx
- sqlc
- Redis or Valkey
- OpenAPI 3.1
- structured JSON logging
- OpenTelemetry-compatible instrumentation
- Docker
- Docker Compose
- Nginx
- GitHub Actions
- S3-compatible object storage
- Go testing
- Testcontainers where useful
- k6 for load testing

Prefer:

- standard library
- small focused dependencies
- explicit SQL
- deterministic behavior
- type safety
- compile-time checks

Avoid introducing a heavy ORM for core transactional workflows.

---

# 4. Architecture

The required architecture is a **modular monolith**.

Do NOT create microservices unless an ADR explicitly justifies them.

Suggested repository layout:

```text
cmd/
  api/
  worker/
  migrate/

internal/
  platform/
    config/
    database/
    cache/
    http/
    logging/
    telemetry/
    errors/
    security/
    idempotency/
    jobs/
    storage/

  auth/
  tenant/
  network/
  geography/
  serviceability/
  pricing/
  customer/
  shipment/
  pickup/
  branchops/
  bag/
  manifest/
  linehaul/
  hubops/
  delivery/
  ndr/
  rto/
  pod/
  tracking/
  commission/
  ledger/
  cod/
  settlement/
  billing/
  notification/
  webhook/
  reporting/
  audit/

db/
  migrations/
  queries/
  seeds/

docs/
  contracts/
  releases/
  adr/
  openapi.yaml

deploy/

tests/
```

Do not reorganize healthy existing code merely to match this structure. Adapt intelligently.

Each business module should ideally own:

- domain types
- service/business rules
- repository/query logic
- API handlers
- validation
- authorization policy
- tests
- OpenAPI definitions
- module documentation

---

# 5. Explicitly Prohibited by Default

Do NOT introduce these without a written ADR and measurable justification:

- Kubernetes
- Kafka
- RabbitMQ
- Elasticsearch
- MongoDB
- Cassandra
- service mesh
- multiple relational databases
- event sourcing for every domain
- distributed transactions
- unnecessary background daemons
- unnecessary microservices
- separate auth service
- separate pricing service
- separate settlement service

For the initial VPS, operational simplicity is a core requirement.

---

# 6. Frontend Contract

Codex owns the frontend.

Claude Code owns the backend contract.

The authoritative contract is:

```text
docs/openapi.yaml
```

For every frontend-visible module, also create/update:

```text
docs/contracts/<module-or-release>.md
```

The contract documentation must explain:

- purpose
- user-visible behavior
- endpoint list
- request schemas
- response schemas
- enums
- pagination
- filters
- sort rules
- validation
- permissions
- states
- legal state transitions
- error codes
- idempotency requirements
- concurrency behavior
- immutable fields
- corrective workflows

Never require frontend engineers to inspect Go implementation details to understand business behavior.

Do not break existing public API behavior without documenting a migration strategy.

---

# 7. Mandatory Workflow Before Implementing Any Module

Before changing code:

1. Read this file completely.
2. Inspect the relevant repository areas.
3. Inspect existing migrations.
4. Inspect existing SQL/sqlc queries.
5. Inspect auth/tenant/RBAC behavior.
6. Inspect OpenAPI.
7. Inspect tests.
8. Identify reusable abstractions.
9. Identify conflicts or duplicate implementations.
10. Identify security risks.
11. Identify concurrency risks.
12. Identify performance risks.
13. Produce a concise implementation plan.
14. Implement the module completely.

Do not ask for confirmation for ordinary implementation decisions.

If existing code is broken in a way that blocks correct implementation, fix the underlying defect instead of layering another workaround over it.

---

# 8. Definition of Done for a Backend Module

A backend module is complete only when applicable requirements are implemented:

- database migrations
- rollback/down strategy where practical
- constraints
- indexes
- sqlc queries
- domain types
- repository/storage implementation
- business service
- HTTP handlers
- validation
- tenant isolation
- authorization
- idempotency
- concurrency handling
- audit events
- OpenAPI
- contract documentation
- unit tests
- integration tests
- negative tests
- permission tests
- tenant-isolation tests
- concurrency tests where relevant
- performance checks for hot queries
- production build
- documentation

Do NOT consider a module complete when:
- it merely compiles,
- happy-path only works,
- tests are skipped,
- TODOs remain in critical paths,
- mocks replace production persistence,
- API contract is missing,
- security rules are incomplete.

---

# 9. Multi-Tenancy

The platform is multi-tenant.

Every tenant-owned business record must be associated with the authenticated organization.

Server-side tenant context is authoritative.

Never trust client-supplied fields such as:

```text
organization_id
tenant_id
franchise_id
branch_id
operating_unit_id
```

for authorization.

Client-supplied references may be used as business inputs only after verifying the authenticated principal has permission for that resource.

Prevent:

- IDOR
- BOLA
- cross-tenant reads
- cross-tenant writes
- cross-franchise leakage
- cross-operating-unit leakage
- privilege escalation

Automated tests MUST explicitly prove tenant isolation.

---

# 10. Authentication and Authorization

Use secure production authentication.

Requirements include:

- secure password hashing
- login throttling
- inactive-user enforcement
- session/token expiry
- refresh-token rotation if applicable
- logout/revocation
- password reset expiry
- reset token single use
- audit of security-sensitive events

Authorization must use RBAC and operating-unit scope where relevant.

Initial roles:

- SUPER_ADMIN
- ORG_ADMIN
- OPERATIONS_ADMIN
- HUB_MANAGER
- BRANCH_MANAGER
- FRANCHISE_OWNER
- FRANCHISE_OPERATOR
- FINANCE_MANAGER
- CUSTOMER_SUPPORT
- PICKUP_AGENT
- DELIVERY_AGENT
- CUSTOMER

Roles are not a substitute for object-level authorization.

---

# 11. Identifiers

Internal database primary keys may be bigint.

External APIs must use opaque public identifiers:

```text
shp_...
frn_...
bag_...
mft_...
trp_...
stl_...
inv_...
```

Use UUID/ULID-style public IDs or another safe non-sequential format.

Never expose predictable internal primary keys as public object identifiers.

AWB is a separate human-readable business identifier.

AWB allocation must be concurrency-safe.

Example:

```text
QYN260808000001
```

Do not make correctness depend on a process-local lock.

---

# 12. Monetary Values

Never use `float32` or `float64` for authoritative money.

Store money as integer minor units.

For INR:

```text
₹125.50 = 12550 paise
```

Every monetary amount must include or inherit an explicit currency.

Rounding rules must be deterministic, documented, and tested.

Frontend calculations are never authoritative.

---

# 13. Shipment Domain

A shipment is not just a row with an editable status string.

Use:

```text
shipments
shipment_events
```

`shipments.current_status` may be denormalized for efficient access.

`shipment_events` is append-only operational history.

Typical states include:

- BOOKED
- PICKUP_SCHEDULED
- PICKUP_ASSIGNED
- PICKED_UP
- ORIGIN_BRANCH_RECEIVED
- ORIGIN_BAGGED
- ORIGIN_DISPATCHED
- IN_TRANSIT
- TRANSIT_HUB_RECEIVED
- TRANSIT_HUB_DISPATCHED
- DESTINATION_HUB_RECEIVED
- DESTINATION_BRANCH_RECEIVED
- OUT_FOR_DELIVERY
- DELIVERED
- DELIVERY_FAILED
- NDR
- RTO_INITIATED
- RTO_IN_TRANSIT
- RTO_DELIVERED
- CANCELLED
- LOST
- DAMAGED

Do not permit arbitrary state changes.

Use explicit legal transitions.

Each transition should validate:

- actor
- role
- tenant
- custody
- operating unit
- current state
- target state
- required evidence
- idempotency key where appropriate

Every accepted transition must append a shipment event.

Historical events must not be editable through normal application APIs.

---

# 14. Operational Custody

Courier operations are custody-sensitive.

Track which operating unit, branch, hub, bag, manifest, trip, or delivery agent currently has valid custody where applicable.

Do not permit transitions such as:

- OUT_FOR_DELIVERY from the wrong facility
- delivery before destination receipt
- dispatch from an unreceived bag
- bag closure with invalid shipments
- manifest departure before closure

unless a privileged exception workflow explicitly exists.

Exceptions must be auditable.

---

# 15. Bagging

Bag must be a first-class domain entity.

Typical state:

```text
OPEN
CLOSED
DISPATCHED
RECEIVED
OPENED
RECONCILED
```

Rules:

- contents mutable only while OPEN
- closing freezes contents
- seal metadata recorded
- duplicates rejected
- conflicting active bag membership rejected
- post-close correction requires explicit exception workflow
- receiving/opening/reconciliation is auditable

---

# 16. Manifest

Manifest is a first-class domain entity.

Typical lifecycle:

```text
DRAFT
CLOSED
DISPATCHED
RECEIVED
RECONCILED
```

Manifest closure creates an immutable content snapshot.

Manifest may include:

- bags
- loose shipments where policy allows
- origin
- destination
- trip
- weight
- piece counts

---

# 17. Line Haul

Trip/line-haul models actual inter-facility movement.

Support:

- ROAD
- AIR
- RAIL
- PARTNER

Trip state should be controlled.

Typical lifecycle:

```text
PLANNED
LOADING
DEPARTED
IN_TRANSIT
ARRIVED
CLOSED
CANCELLED
```

Do not update shipment states blindly from trip operations; validate manifest membership and custody.

---

# 18. NDR and RTO

NDR is a workflow, not just a status.

Support configurable reasons and actions:

- REATTEMPT
- RESCHEDULE
- CONTACT_REQUIRED
- ADDRESS_CORRECTION
- CUSTOMER_PICKUP
- RTO
- ESCALATE

RTO must be modeled as reverse courier movement.

Do not implement RTO as a single status update.

RTO may involve:
- destination branch
- destination hub
- transit
- origin hub
- origin branch
- sender return

---

# 19. Pricing

Pricing must be versioned and auditable.

Support:

- retail
- business customer
- franchise
- service
- origin zone
- destination zone
- actual weight
- volumetric weight
- chargeable weight
- slabs
- additional weight
- fuel surcharge
- remote-area surcharge
- COD charge
- insurance
- handling
- discount
- tax

Persist the exact pricing snapshot used for a shipment.

Historical shipments must never change because a rate card was later edited.

Pricing responses should provide detailed calculation breakdown.

---

# 20. Serviceability and Routing

Serviceability/routing input may include:

- origin pincode
- destination pincode
- service
- booking date/time
- customer
- temporary closures

Output should include:

- serviceable yes/no
- origin branch
- origin hub
- transit path
- destination hub
- destination branch
- SLA
- remote-area status
- restrictions

Routing must be deterministic.

Configuration should support:

- priority
- effective dates
- temporary closures
- overrides
- fallback paths

Cache read-mostly routing data where beneficial.

Cache invalidation must be explicit.

---

# 21. Idempotency

Important mutating operations must tolerate client/network retries.

Use idempotency for at least:

- shipment booking
- payment callbacks
- pickup completion where mobile retries are expected
- operational scans
- delivery completion
- POD submission
- COD posting
- commission posting
- settlement generation
- settlement payment
- invoice generation
- webhook processing

Use database uniqueness as part of correctness.

Do not rely only on Redis for idempotency in finance-critical operations.

---

# 22. Concurrency

Assume multiple branches/franchises/users operate simultaneously.

Protect:

- AWB generation
- invoice numbering
- bag closure
- manifest closure
- trip departure
- delivery completion
- COD collection
- commission posting
- journal posting
- settlement generation
- settlement payment

Use:

- unique constraints
- transactions
- row locks
- atomic SQL
- appropriate isolation

Never use only process-local mutexes for cross-instance correctness.

---

# 23. Financial Ledger

The finance core must use double-entry accounting.

Entities:

- LedgerAccount
- JournalTransaction
- JournalEntry
- AccountingPeriod

Invariant:

```text
SUM(DEBITS) == SUM(CREDITS)
```

for every posted transaction.

Use integer minor units.

Posted journals are immutable.

Corrections require:

- reversal
- adjustment

Never "fix" a balance by editing historical entries.

Ledger should support:

- account statement
- trial balance
- source references
- audit trail

Financial operations must be idempotent.

---

# 24. Commission

Commission rules must be versioned.

Support:

- BOOKING
- PICKUP
- ORIGIN_HANDLING
- DESTINATION_HANDLING
- DELIVERY
- COD
- VOLUME_INCENTIVE
- CUSTOM

Calculation types:

- FIXED
- PERCENTAGE
- SLAB

Scope can include:

- franchise
- franchise category
- service
- zone
- route
- operating unit
- effective date

Persist the exact rule version and calculation inputs.

Never recalculate historical commission automatically using current rules.

---

# 25. COD

COD must model custody and liability.

Example lifecycle:

```text
EXPECTED
AGENT_COLLECTED
BRANCH_RECEIVED
FRANCHISE_CONFIRMED
RECONCILED
REMITTED
CLOSED
```

Support:

- shortage
- excess
- dispute
- approved adjustment
- cash
- supported digital modes

Every financial COD event must produce appropriate ledger entries.

Never maintain COD truth as an editable balance field.

---

# 26. Settlement

Settlement must be reproducible.

May include:

- booking commission
- pickup commission
- handling
- delivery commission
- COD liability
- charges
- penalties
- incentives
- tax/withholding configuration
- previous balance
- payments
- adjustments
- net payable/receivable

Typical lifecycle:

```text
DRAFT
CALCULATED
UNDER_REVIEW
APPROVED
PARTIALLY_PAID
PAID
CLOSED
```

Every settlement line must reference its source.

Approved/paid settlement cannot silently recalculate.

Correction must use explicit adjustment/reversal mechanisms.

---

# 27. Database Standards

Use PostgreSQL as authoritative system of record.

Every migration must consider:

- foreign keys
- indexes
- constraints
- nullability
- uniqueness
- effective dates
- soft-deactivation where history exists
- safe production rollout

Every list endpoint must paginate.

Prefer keyset/cursor pagination for high-volume tables.

Never execute unbounded list queries.

Avoid N+1 queries.

Use `EXPLAIN ANALYZE` for important hot queries.

Important indexes commonly include combinations of:

- tenant/organization
- status
- created_at
- public_id
- AWB
- operating_unit
- pincode
- customer
- shipment
- active/effective dates

Do not add indexes blindly. Base them on access patterns.

---

# 28. PostgreSQL Resource Discipline

Initial production VPS has 4 vCPU / 16 GB RAM.

Do not configure excessive DB connections.

Use a bounded pgx pool.

Keep enough RAM for:
- PostgreSQL
- OS page cache
- Go API
- workers
- Redis
- Nginx
- safety margin

Avoid query patterns that require huge temporary memory.

Use chunking for large operations.

---

# 29. Redis/Valkey

Redis may be used for:

- cache
- rate limiting
- OTP
- short-lived coordination
- queue metadata
- temporary realtime state

Redis must NEVER be the sole system of record for:

- shipments
- shipment events
- COD
- ledger
- commission
- settlement
- invoices
- payments

Configure:

- maxmemory
- TTL where appropriate
- eviction policy according to cache use
- bounded queue retention

Loss of Redis should not corrupt authoritative business state.

---

# 30. Background Jobs

Use background execution for non-user-blocking work such as:

- notifications
- webhooks
- PDF generation
- report export
- bulk import
- image processing
- settlement statement generation
- retryable integrations

Do not move a consistency-critical database transaction into an eventual asynchronous workflow unless business semantics allow it.

Jobs must support:

- retry
- bounded backoff
- deduplication
- error recording
- dead-letter handling where appropriate

---

# 31. File/Object Storage

Store POD photos, signatures, labels, exports, and large documents outside PostgreSQL.

Use S3-compatible storage.

Database stores metadata:

- object key
- checksum
- size
- MIME
- uploader
- shipment/reference
- timestamp
- access metadata

Uploads must enforce:

- MIME allowlist
- size limit
- random/generated object key
- checksum
- private access
- authorization
- signed temporary retrieval where appropriate

Never trust filename extension alone.

---

# 32. API Standards

External/customer-facing API base:

```text
/api/v1/
```

Use consistent:

- request validation
- response schemas
- error schemas
- status codes
- pagination
- filtering
- sorting
- request IDs
- idempotency behavior
- auth documentation

Standard error envelope:

```json
{
  "error": {
    "code": "SHIPMENT_INVALID_STATE",
    "message": "Shipment cannot transition to OUT_FOR_DELIVERY.",
    "details": {},
    "requestId": "..."
  }
}
```

Do not expose stack traces.

Errors should be useful to frontend without leaking internals.

---

# 33. OpenAPI Quality

`docs/openapi.yaml` is a required production artifact.

It must remain valid.

Document:

- enum values
- examples
- security
- errors
- request bodies
- response bodies
- pagination
- filters
- idempotency headers
- optional vs required fields

Do not leave vague `object` schemas when structured types are known.

---

# 34. Audit

Audit important actions:

- login
- role change
- user activation/deactivation
- rate change
- routing override
- franchise change
- shipment cancellation
- manual state override
- damage/lost declaration
- COD adjustment
- commission adjustment
- journal reversal
- settlement calculation
- settlement approval
- settlement payment
- invoice correction
- API key management
- webhook replay

Audit record should include:

- tenant
- actor
- action
- resource
- timestamp
- request ID
- reason where required
- before/after metadata where appropriate
- operating unit
- relevant source reference

Audit data is not casually editable.

---

# 35. Security

Assume hostile input.

Apply:

- parameterized SQL
- object authorization
- tenant enforcement
- secure password handling
- brute-force protection
- request validation
- CORS allowlists
- CSRF protection where applicable
- secure headers
- upload validation
- API rate limiting
- token revocation
- secret redaction
- no sensitive values in logs

Explicitly test for:

- IDOR
- BOLA
- SQL injection
- mass assignment
- cross-tenant access
- privilege escalation
- webhook spoofing
- token misuse
- path traversal
- upload attacks
- rate-limit bypass

No Critical or High security issue should be knowingly left unresolved without documented risk acceptance.

---

# 36. Logging and Observability

Every request should have a request/correlation ID.

Use structured JSON logs.

Useful fields:

- timestamp
- level
- request_id
- route
- method
- status
- latency
- tenant identifier where safe
- actor identifier where safe
- error code

Never log:

- passwords
- OTPs
- auth tokens
- refresh tokens
- API secrets
- full sensitive payment data

Track key metrics:

- requests
- p50/p95/p99 latency
- 5xx
- DB pool usage
- slow queries
- Redis health
- worker backlog
- worker failures
- bookings
- scan throughput
- deliveries
- NDR
- COD failures
- commission failures
- settlement failures

Keep observability lightweight on initial VPS.

---

# 37. Performance Principles

Optimize for:

- events per shipment
- scans per second
- booking throughput
- dashboard query efficiency
- DB contention
- memory footprint

Prefer:

- efficient SQL
- batching
- cursor pagination
- bounded workers
- small API payloads
- server-side filtering
- precomputed summaries for expensive dashboards
- caching of stable reference data

Avoid:

- huge ORM graphs
- unbounded goroutines
- large in-memory reports
- massive JSON responses
- excessive polling
- unnecessary serialization
- redundant DB calls

---

# 38. Reporting

Large reports must not be generated synchronously in HTTP requests.

Use:

- background job
- chunked/cursor reads
- streaming export
- object storage
- signed download
- expiration

Never build huge CSV/PDF payloads entirely in RAM.

---

# 39. Backup and Disaster Recovery

Production must include:

- PostgreSQL backup
- compression
- off-server copy
- retention
- verification
- restore scripts
- tested restore procedure

Redis should be treated as rebuildable state wherever possible.

Document:

- full VPS loss
- DB corruption
- Redis loss
- object storage issue
- failed migration
- bad deployment
- TLS failure

A backup is not considered valid until a restore is successfully tested.

---

# 40. Testing Standards

Use:

- unit tests
- integration tests
- database tests
- API tests
- tenant-isolation tests
- permission tests
- concurrency tests
- idempotency tests
- migration tests
- load tests where relevant

Critical end-to-end backend workflows:

1. booking → delivery → POD
2. booking → failed delivery → NDR → reattempt → delivered
3. booking → NDR → RTO → sender
4. bag mismatch and reconciliation
5. duplicate scan retries
6. concurrent delivery completion
7. COD custody flow
8. commission posting
9. settlement generation
10. journal reversal

Use `go test -race` where practical.

Do not skip failing critical tests to make CI green.

---

# 41. Production Hardening

Before production release:

- validate migrations on clean DB
- validate migrations on representative existing DB
- run security review
- run load tests
- inspect slow queries
- inspect DB connection usage
- inspect memory
- inspect queue behavior
- test restart during critical workflows
- test idempotency under retries
- test backup restore
- verify TLS
- verify log rotation
- verify disk monitoring
- verify environment secrets
- verify rollback procedure

Do not say "100% production ready."

Instead produce evidence-backed readiness and explicitly document residual risk.

---

# 42. Release Model

The implementation is organized into five releases.

## Release 1 — Foundation and Booking
M00-M08

- platform
- auth/RBAC
- network
- geography
- serviceability
- products
- pricing
- customers
- booking/AWB

## Release 2 — Physical Courier Operations
M09-M20

- pickup
- scanning
- bagging
- manifest
- line haul
- hubs
- destination branch
- delivery
- NDR
- RTO
- POD
- public tracking

## Release 3 — Franchise Finance
M21-M25

- commission
- double-entry ledger
- COD
- settlement
- billing

## Release 4 — Productization
M26-M32

- notifications
- customer portal API
- franchise portal API
- hub/branch optimized API
- operations command center
- reporting
- partner API/webhooks

## Release 5 — Production Gate
M33-M36

- security
- observability
- backup/DR
- load/performance hardening

Do not introduce later-release complexity unnecessarily into earlier modules, but design interfaces so later modules can integrate cleanly.

---

# 43. Completion Report Required After Significant Work

At the end of a module or release, report:

1. files created/changed
2. schema changes
3. APIs added/changed
4. business rules implemented
5. authorization/tenant controls
6. concurrency/idempotency controls
7. indexes/performance changes
8. tests run and results
9. OpenAPI/contract updates
10. deployment/migration notes
11. known risks
12. intentionally deferred work

Do not merely summarize code. Include evidence.

---

# 44. Final Principle

Correctness and auditability outrank cleverness.

Resource efficiency outranks fashionable architecture.

The backend owns business truth.

PostgreSQL owns authoritative state.

Financial history is immutable.

Operational history is append-only.

OpenAPI is the frontend contract.

Tests and evidence determine readiness — not the amount of generated code.
