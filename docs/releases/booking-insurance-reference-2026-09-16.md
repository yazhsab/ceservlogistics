# Commercial booking implementation — 16 September 2026

Status: backend and operations frontend implemented and verified locally. Not deployed. The user explicitly authorized backend implementation after the initial frontend-only review.

## Delivered behavior

- Insurance is calculated by the server from **configurable INSURANCE surcharge rules on the applicable rate-card version**, subject to service eligibility and existing value limits. A separate checkbox records acceptance of the exact current quote. The server rejects missing or stale acceptance and stores the applied rules, version, premium, actor and timestamp in an immutable commercial snapshot.
- Customs declarations contain invoice reference, export reason, terms, declaration text, goods description, quantity, unit, unit value, origin country and optional HS code. The server returns line totals, subtotal, discount, net declared goods value, freight, insurance, other charges and customs invoice total. Goods/customs values remain separate from transportation charges.
- Transportation and duty/tax each support shipper, receiver and third party independently. Third-party accounts are validated for tenant and customer scope. Credit reservation, cancellation release and customer invoice selection follow the transportation account.
- Origin/destination country and postal identifiers persist through saved addresses, country-aware lookup, preview, booking and detail. UK values such as `ss07jj` normalize to `SS0 7JJ`; Nigerian and Indian postal validation stays six-digit. Routes still require actual configured coverage.

## Routes, components and APIs

Existing web routes enhanced: `/shipments/new`, `/shipments/:shipmentId`, `/pricing/versions/:versionId`. No placeholder pages or new dependency added.

Reusable booking components: `BillingFields`, `CustomsFields`, and `CommercialSummary` in `web/src/pages/BookingCommercialFields.tsx`, using existing Field/Input/Select/Panel/Button/InlineNotice primitives. `CommercialSummary` is shared with shipment detail. Generated OpenAPI types cover requests and persisted commercial projections.

New API endpoints: `POST /api/v1/shipments/preview` and `POST /api/v1/partner/shipments/preview`. The active-country endpoint is now documented and consumed. Existing customer/address, geography/state/district/place, courier-service, booking/detail and pricing APIs are reused and extended. Internal preview requires `shipment.create`; partner preview requires `shipment:create`.

Backend additions include migration `0038_shipment_commercial`, generated sqlc queries, append-only and tenant-consistency triggers, commercial validation/snapshots, versioned insurance pricing, payer-aware credit/invoice queries and country-aware postal normalization. Existing records are not backfilled with consent. Pricing engine version is now 1.2.0.

## Insurance configuration

Use **Commercial → Rate cards → draft version → Add surcharge → INSURANCE**. Choose percentage, fixed or per-kilogram pricing, the basis, optional service scope, minimum/maximum charges and tax treatment. The percentage starts blank. For example, 2.5 becomes 250 basis points in the API. Save and activate the version; published versions cannot be edited.

The existing backend surcharge engine remains the single calculator. Insurance quote metadata is a projection of its applied rules. Booking and shipment detail show the actual premium and configuration; fixed/capped/composite premiums use an amount label. If no insurance rule applies, preview fails with `INSURANCE_RATE_NOT_CONFIGURED` instead of inventing a percentage. No live configuration was inserted.

## UX, keyboard, responsive and accessibility

The form uses server previews rather than browser financial calculations. Insurance acceptance is cleared on edits and changed quotes; duplicate submissions are blocked and a changed booking body obtains a fresh idempotency key synchronously. Forms preserve inputs on API failure. Billing account search identifies existing accounts by name/code. Country changes clear incompatible state/city/postal selections. Country loading and errors have visible feedback and retry.

Existing keyboard preview/booking shortcuts remain. Checkboxes support Tab/Space and full clickable labels; form errors use associated field feedback and focus. Dynamic goods fields have labels and fieldset legends. Shipment detail uses an accessible goods table with bounded horizontal scrolling. Scanner workflows were not changed.

Desktop keeps the review beside the form. Tablet/mobile stack the form and retain sticky booking actions. Goods lines reflow rather than forcing horizontal form scrolling.

## Verification and evidence

- OpenAPI types regenerated; strict TypeScript check passed.
- Production Vite build passed; output remains route-split. Booking chunk approximately 37 kB (10 kB gzip); shared application chunk approximately 542 kB (166 kB gzip). No dependencies added.
- Vitest: **27 tests passed**.
- Playwright: **48 cases passed** across existing booking/auth/routing/pricing/customer/detail regressions and new insurance/customs/billing/country cases. The first run passed 46 cases; the two configuration cases passed on a focused rerun after correcting a test selector to include the required-field marker. Coverage includes creating a 2.5% draft insurance rule, limits/tax validation, missing configuration, fixed-premium labels, keyboard acceptance, invalid values, service ineligibility, changed quote re-acceptance, duplicate-submit protection, unsigned legacy insurance requests, persisted commercial detail, UK postcode/country, independent payer payloads and all seven target viewport sizes (1366×768, 1440×900, 1920×1080, 768×1024, 1024×768, 390×844, 430×932). Responsive cases assert no document-level horizontal overflow.
- Visual self-review completed using screenshots of the customs form, mobile customs value breakdown, mobile and desktop 2.5% acceptance controls, the insurance configuration dialog, and desktop saved commercial detail. Screenshots are under the ignored `web/test-results/booking-insurance-*` and `web/test-results/configuration-only/` directories. Tall element screenshots include the existing sticky app header/actions; viewport captures were also reviewed.
- Ten new PostgreSQL/Redis integration tests pass (including eight amount/tax subcases), including all nine payer combinations, integer customs totals, immutable records, tenant isolation, replay, invalid/empty/stale acceptance, ineligible service, saved-address country updates, GB postcode normalization, partner scope/actor identity, credit reservation/release, invoice account selection, unrequested historical surcharge suppression and exclusion of insurance from shipping discounts. Additional coverage verifies no implicit default, 2.5% and 3.75% rates, separate tenant configurations, real version activation and stale-consent rejection, immutable historical pricing, fixed/per-kilogram premiums, half-up rounding, configured floors/caps, tax exemption, explicit zero rates, matching conditions and multiple rules. A regression assertion rejects package values that conflict with fully discounted customs goods.
- Focused booking/pricing/validation tests pass. `go vet ./internal/... ./tests/...` passed.
- Full `go test ./...` was run: the only failures are `TestASettlementForRealDeliveryWorkIsNotEmpty` (hardcoded August settlement period versus today's delivered work) and `TestPickupCompletedEventIsQueuedAtomically` (pickup serviceability fixture). Both failures were reproduced from an isolated archive of unchanged HEAD. They were not skipped or relaxed.
- Changed frontend files pass ESLint. Repository-wide lint is blocked by two unchanged findings: missing hook dependencies in `web/src/components/operations.tsx:243` and a promise handler in `web/src/pages/CollectionPages.tsx:127`.
- `git diff --check` passes.

## Deployment and remaining limits

Apply migration 0038 and deploy API/frontend together. Insured API clients must switch to preview plus explicit acceptance. Configure insurance on a draft rate-card version and activate it. Existing applicable `INSURANCE` rules are reused, with percentage/fixed/per-kilogram calculation, scope, limits and tax settings. No percentage is assumed when rules are missing. These are intentional compatibility changes described in [the commercial booking contract](../contracts/shipment-commercial.md). No live rate cards, production data or deployment were changed.

Country availability does not create a serviceable lane. International states/postcodes and network/pricing coverage require provisioning; the existing bulk geography importer remains domestic. The current money model supports NGN/INR/USD without FX conversion. Countries without postal identifiers are not modeled.

This implementation records insurance acceptance and commercial/customs instructions. It does not issue insurer policies, perform customs filing, assess/collect duties, obtain third-party contractual consent, or generate/upload customs documents. These remain separate integrations/workflows, rather than claims made by the new UI.
