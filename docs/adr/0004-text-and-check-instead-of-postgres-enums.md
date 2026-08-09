# ADR 0004 — Enum-like columns are text with a CHECK constraint

**Status:** accepted · Release 1

## Context

The schema has roughly forty enumerated columns: shipment status, unit type,
payment mode, calculation type, and so on. Release 2 adds values to several of
them.

## Decision

Every such column is `text` with a `CHECK (col IN (...))` constraint, not a
PostgreSQL `ENUM` type.

## Why

- **`ALTER TYPE … ADD VALUE` cannot run inside a transaction** in the versions
  we target, which makes a native enum hostile to an atomic migration.
- **A value can never be removed** from a native enum, only orphaned.
- **A CHECK constraint is replaceable** in one transactional migration:
  `DROP CONSTRAINT` + `ADD CONSTRAINT`.
- **sqlc maps text to `string`** directly, whereas a native enum produces a
  generated type per column that then has to be converted at every boundary.
- The safety property that matters — an invalid value cannot be stored — is
  identical.

## Consequences

- The Go constant, the CHECK constraint and the OpenAPI enum must agree. That is
  enforced by tests: `TestShipmentStatusEnumMatchesCode` in `tests/contract`
  fails the build if the published enum drifts from the implementation.
- Shipment status declares all twenty-two Release 2 values from day one, so no
  migration is needed when operations ship. The state machine refuses the
  not-yet-available transitions in the meantime, and says which release enables
  them.
