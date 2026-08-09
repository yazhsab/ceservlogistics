# ADR 0002 — Hubs and branches are one table

**Status:** accepted · Release 1

## Context

The network contains head offices, regional hubs, transit hubs, delivery hubs,
company branches and franchise branches. Shipments, bags, manifests, trips,
service areas, routes, role scopes and audit records all need to reference "a
facility".

## Decision

One `operating_units` table with a `unit_type` discriminator. `/network/hubs`
and `/network/branches` are filtered projections over it.

## Why

- **Every structural attribute is shared**: code, name, address, coordinates,
  contact, operating hours, hierarchy, effective dates, capabilities. Separate
  tables would duplicate all of it and then drift.
- **Polymorphic references become simple.** `shipments.origin_branch_id` and
  `route_definitions.origin_unit_id` are plain foreign keys. With separate
  tables each would need either a type column plus an unenforceable reference,
  or one nullable column per facility type.
- **The hierarchy is one recursive CTE.** A branch's parent is its hub; a hub's
  parent is its regional hub. Resolving "which hub serves this branch" — which
  every routing decision does — is a single query.
- **Behaviour is data, not type.** `operating_unit_capabilities` records what a
  facility may do, so enabling pickup at a transit hub is configuration rather
  than a code change.

## Consequences

- Type-specific validation lives in the service layer, not in the schema. It is
  tested there.
- Queries filtering by facility kind must say so; every such query in the
  repository carries an explicit `unit_type` predicate, and
  `operating_units_org_type_status_idx` serves them.
- The frontend must understand the discriminator. The contract document leads
  with it.
