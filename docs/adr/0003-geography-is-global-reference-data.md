# ADR 0003 — Geography is global; zones are per tenant

**Status:** accepted · Release 1

## Context

Every courier tenant on the platform delivers to the same ~19,300 India Post PIN
codes. Each tenant has its own commercial view of them: which pricing zone a PIN
code falls into, and whether it treats that PIN code as a remote area.

## Decision

`countries`, `states`, `districts`, `cities`, `pincodes` and `localities` are
global reference data with no `organization_id`, maintained by the platform
operator. The tenant-specific overlay lives in `pincode_zone_mappings`, which
carries the zone and an optional `is_remote_override`.

## Why

- **PIN codes are objective facts**, not tenant opinions. Per-tenant copies
  would let two tenants disagree about which state a PIN code is in.
- **One dataset, one cache.** The hottest read in the platform is an exact PIN
  code lookup; a shared cache entry serves every tenant.
- **Import happens once**, not once per tenant. A postal data refresh is a
  single platform operation.
- **The tenant-specific parts genuinely are tenant-specific**, and they are
  small: a zone mapping row per PIN code the tenant actually serves.

## Consequences

- PIN code maintenance requires the platform-level `pincode.manage` permission.
  A tenant administrator cannot rewrite shared data — which is the point.
- Tenant isolation tests target the overlay tables, service areas and routes.
  Geography reads are deliberately shared and are not a leak.
- A tenant needing a PIN code the platform lacks must ask the operator to add
  it. Booking fails with `PINCODE_NOT_FOUND`, which is a clear, actionable
  message rather than a silent misroute.
