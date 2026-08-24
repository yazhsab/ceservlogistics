# Release 4 frontend/backend contract gaps

Date: 2026-08-09

The original gap report was answered in
[`release-4-response.md`](release-4-response.md), and the Release 4 contract is
now final. This file records only the capability boundaries that remain after
the completed frontend implementation. The UI does not invent substitutes for
them.

## 1. Notification versioning and providers

- Template list/detail/create/update/preview and delivery evidence are callable.
- Template version history is not present in the schema or API. The frontend
  therefore does not show fictitious versions or rollback.
- Channels are compiled adapters and are intentionally read-only. The frontend
  shows configured/unconfigured state and health rather than an editor.
- The contract currently uses logging senders, not real SMS, email, WhatsApp, or
  push provider adapters.

Backend/product action if required: contract template versions/rollback and
register production provider adapters.

## 2. Customer self-service workflows

Customer-bound summary, accounts, shipments, safe tracking, and invoices are
available. The contract does not expose:

- customer portal booking;
- price estimate;
- pickup request/management;
- saved-address management;
- customer-scoped POD retrieval.

The portal explicitly explains that these actions are not enabled and directs
the customer to their account team. It does not call internal organization-wide
APIs or the partner API using session credentials.

Backend/product action if required: approve the customer self-service product
scope and publish caller-bound operations/projections.

## 3. Franchise dashboard and operational routes

Franchise summary, ORIGIN/DESTINATION/ANY shipments, and settlements are
available. The summary does not expose distinct pickup-pending, inbound,
outbound, delivery-pending, bag, or manifest counts.

Franchise operators perform booking, pickup, scanning, bag, manifest, delivery,
COD, commission, ledger, and settlement actions through the ordinary APIs with
the backend's operating-unit scope. The frontend links to those workspaces and
does not create a second authorization model.

Backend action if required: add those queue aggregates to the franchise summary;
thin `/portal/franchise` wrappers are unnecessary unless routing conventions
change.

## 4. Command-centre exception precision

The command contract provides exact period totals, live status/backlog,
timestamped snapshots, SLA breach totals, NDR by reason, open exceptions, and
COD aging over 48 hours. It does not provide:

- a dedicated late-pickup count/queue separate from uncollected bookings;
- NDR aging buckets or a server-ranked NDR-aging queue.

The UI labels pickup pending as uncollected bookings and NDR as reason groups.
It does not present either number as the missing late/aging measure.

Backend action if required: add explicit late-pickup and NDR-aging fields plus
drill-down filter contracts.

## 5. Report preview and progress

Queued report lifecycle, completion evidence, failure, expiry, cancellation, and
download are callable. The backend intentionally does not expose a preview or
progress percentage because export row count is unknown until the cursor stream
finishes.

The frontend shows an indeterminate running state and states that no
browser-side preview exists. It never calculates a guessed percentage.

Backend/product action if required: provide a bounded, real report artifact for
preview rather than running a separate query that may disagree with the export.

## 6. Seeded end-to-end fixtures

The backend supplies callable operations but no production fixture endpoint or
default demo customer/franchise subjects. The repository frontend Playwright
suite uses contract-shaped request interception and exercises identity and
permission denial.

Backend/test action if full-stack E2E is required: add test-environment setup SQL
for customer and franchise users, operating-unit bindings, shipments, invoices,
COD, settlements, notifications, reports, credentials, and webhooks. Do not add
a fixture API to production.

## 7. Known integration limitations

The Phase 2 ecommerce/logistics hardening added transactional producers for
`pickup.completed`, `pod.captured`, and `cod.collected`. Remaining limitations:

- webhook subscription filters are stored but not applied;
- circuit breaking is limited to the existing 20-failure pause.

The integration UI shows configured events and actual delivery evidence but does
not claim filter enforcement.

## 8. Cross-release Nigeria contract terminology

The final OpenAPI document still uses legacy `pincode`, `gstNumber`, and
`panNumber` property names, plus India-specific GSTIN/PAN patterns and examples.
The database/runtime has begun generalizing these concepts per organization
country, but the machine-readable contract does not yet expose neutral
`postalCode`, `taxRegistrationNumber`, or `businessRegistrationNumber` fields.

The frontend keeps the contracted JSON property names while presenting Nigerian
labels: postal code, TIN, and CAC/RC number. It does not rename request fields or
hand-maintain replacement DTOs.

Backend action: publish country-neutral aliases and per-country validation in
OpenAPI, deprecate the old names, and update remaining customer/organization
handlers that still validate specifically as Indian GSTIN/PAN.
