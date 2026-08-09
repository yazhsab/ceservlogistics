# Release 2 frontend/backend contract gaps

Date: 2026-08-08

The Release 2 contract, schemas, permissions, and backend routes are now present
and were used for the frontend implementation. The following residual gaps are
non-blocking for the shipped workspaces but prevent a few requested views from
being fully server-driven.

## 1. OpenAPI path-prefix mismatch

`docs/contracts/release-2.md` and the mounted application router expose Release
2 at `/api/v1/...`, while the Release 2 path keys in `docs/openapi.yaml` start at
`/pickups`, `/scans`, `/bags`, and so on. The frontend uses the mounted
`/api/v1/...` routes and generated OpenAPI schema types, but the mismatch
prevents operation-level generated clients from addressing those URLs without
an adapter.

Requested backend action: make the OpenAPI `servers`/path convention match the
mounted router consistently across Release 1 and Release 2.

## 2. Pickup bulk assignment

The contract exposes `POST /api/v1/pickups/{pickupId}/assign`, but no bulk
assignment endpoint. The queue therefore submits one real assignment per
selected pickup and reports partial success explicitly. This is correct but
costs one request per row.

Requested backend action: add a bounded bulk assignment operation with
per-pickup outcomes and idempotency semantics.

## 3. Destination-branch categories

`GET /api/v1/deliveries/queue` supports ready/held/assigned behavior but does
not expose an "awaiting receipt" dataset or server filters for the requested
"Address Issue" and "Customer Pickup" categories. The frontend ships the
contract-backed Ready for Delivery, On Hold, Unassigned, and Assigned views; it
does not fabricate the other queues from a partial browser-side list.

Requested backend action: add explicit queue categories or server filters for
awaiting receipt, address issue, and customer pickup.

## 4. Scanner route/destination columns

`ScanHistoryEntry` includes the facility and state movement but not route or
destination. The scanner history therefore renders time, AWB/barcode,
operation, status movement, facility, and result, but cannot render the
requested route/destination column.

Requested backend action: add customer-safe operational route/destination
summaries to scan results/history.

## 5. RTO aging filter

The RTO list supports status but has no server-side age bucket or initiated
date range. The UI labels its seven-day counter as visible-page-only and does
not pretend it represents the full cursor-paginated population.

Requested backend action: add initiated date bounds or an aging bucket filter
and, ideally, aggregate counts.

## 6. POD discovery and print

POD supports create, get-by-POD-ID, and authorized artifact download. There is
no list/search or lookup-by-AWB endpoint and no printable POD endpoint. The
viewer is therefore reachable from returned POD identifiers and related
workflows, not through an invented AWB search.

Requested backend action: add permission-scoped POD lookup/listing and a print
representation only if those are intended product capabilities.

## 7. Public-tracking branding metadata

The tracking payload deliberately exposes only customer-safe shipment data and
does not include tenant branding. The page is visually white-label-ready but
uses the current Ceserve mark and copy.

Requested backend action: provide a public, allow-listed tracking-brand
configuration endpoint or add safe branding fields to the tracking response.
