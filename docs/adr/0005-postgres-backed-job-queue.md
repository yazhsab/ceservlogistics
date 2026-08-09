# ADR 0005 — Background jobs live in PostgreSQL

**Status:** accepted · Release 1

## Context

Release 1 needs asynchronous bulk import; later releases add notifications,
webhooks, PDF rendering and report export. The Constitution forbids Kafka and
RabbitMQ without a written justification.

## Decision

A `jobs` table, claimed with `SELECT … FOR UPDATE SKIP LOCKED`, run by the
`worker` binary.

## Why

- **One durable store.** A job enqueued inside a business transaction becomes
  visible only if that transaction commits. With an external broker that is a
  two-phase commit or an outbox — more machinery for a smaller guarantee.
- **`SKIP LOCKED` is genuinely good at this.** Several workers claim disjoint
  batches without blocking each other, comfortably past the throughput four
  vCPUs can generate.
- **Operational simplicity.** No extra process to run, monitor, back up or
  patch on a 16 GB box.
- Retry with bounded exponential backoff, deduplication, error recording and
  dead-lettering are all straightforward columns.

## Consequences

- Job throughput is bounded by database write capacity. For this workload —
  imports and notifications, not a firehose — that is far from the constraint.
- Polling adds a small constant load: one indexed query per worker per poll
  interval, served by a partial index on pending rows.
- If a future release genuinely needs millions of events per hour, this ADR
  should be revisited with measurements. It is not a decision that would be
  expensive to change: the `Enqueuer` and `Handler` interfaces would stay.
