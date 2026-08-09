# ADR 0001 — A modular monolith on one PostgreSQL

**Status:** accepted · Release 1

## Context

The initial production target is a single Hostinger VPS KVM 4: 4 vCPU, 16 GB
RAM, 200 GB NVMe. The domain spans nine modules in Release 1 and grows to
roughly thirty by Release 5, covering operations and double-entry finance.

## Decision

One Go binary set (`api`, `worker`, `migrate`) built from one module graph,
against one PostgreSQL database. Modules are Go packages with their own domain
types, queries, services, handlers and tests; they depend on the platform layer
and on each other's *services*, never on each other's storage.

## Why

- **Transactions are the point.** A booking writes a shipment, packages, three
  snapshots, a credit reservation, an event and an audit record. Across services
  that is a distributed transaction or a saga with compensations. In one
  database it is `BEGIN … COMMIT`, and the atomicity the Constitution demands is
  free.
- **The hardware forbids the alternative.** Nine services with their own pools,
  sidecars and inter-service TLS would spend most of four cores on
  serialisation and network I/O.
- **Modularity is a code-structure property, not a deployment one.** The module
  boundaries here are the same ones a service split would follow later; nothing
  in the design prevents extracting one when there is a measured reason.

## Consequences

- A bug in one module can affect the whole process, so panic recovery, bounded
  timeouts and a bounded pool are mandatory (they are in `internal/platform`).
- Scaling is vertical first, then horizontal by running more API replicas
  against the same database. Pool sizing is budgeted for that in
  `deploy/postgres/postgresql.prod.conf`.
- Extracting a service later means giving it its own database and accepting
  eventual consistency at that seam. That is a real cost, deliberately deferred
  until there is evidence it is needed.
