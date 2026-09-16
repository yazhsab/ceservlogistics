# Shipment insurance, customs and billing instructions

Implemented 2026-09-16. The wire contract is `docs/openapi.yaml`; generated web types come from that contract. Migration `0038_shipment_commercial` must be applied before deploying this API and frontend together.

## Preview and book

1. Send the complete `BookingRequest` to `POST /api/v1/shipments/preview` (permission `shipment.create`). Partner integrations use `POST /api/v1/partner/shipments/preview` with an API key carrying `shipment:create`.
2. The server validates customer and billing accounts, destination countries/postcodes, product restrictions, route, price and customs inputs. Preview returns `quote`, `serviceability`, `commercial`, and `declaredValueMinor`. It allocates no AWB and writes no shipment, credit reservation or commercial snapshot.
3. When insurance is requested, display `quote.insurance.premiumMinor`, `declaredValueMinor`, `currency`, and the applied `rules`. A headline `rateBp` is optional and only describes a single uncapped percentage of declared value. Record the customer's decision explicitly.
4. Book with the same fields plus `insuranceAcceptance: { accepted: true, quoteFingerprint: "<preview fingerprint>" }`. Use an `Idempotency-Key`; partner bookings require it. The server recalculates the quote before accepting the fingerprint.
5. Read `Shipment.commercial` on the booking response or authorized shipment detail. The saved record cannot be edited or deleted. Historical shipments without this record retain their historical financial snapshots and do not acquire fabricated consent.

A fingerprint is a deterministic quote revision, not a bearer credential or customer signature. It binds the tenant, customer, service, financial inputs, rate-card version, price breakdown and policy version. Time alone does not invalidate an unchanged price; changes to the quoted values do. An authorized operator or integration records the decision, with actor identity/type and server time.

Relevant errors: `INSURANCE_NOT_AVAILABLE`, `INSURANCE_RATE_NOT_CONFIGURED`, `INSURANCE_ACCEPTANCE_REQUIRED`, `INSURANCE_QUOTE_CHANGED`, `SHIPMENT_NOT_SERVICEABLE`, and field validation errors. A missing or inaccessible billing customer returns 404. No failed validation allocates a shipment or reserves credit.

## Insurance calculation and compatibility

Pricing engine **1.2.0** uses the applicable rate-card version's existing `INSURANCE` surcharge rules. There is **no hardcoded rate or implicit fallback**.

- Configure insurance through **Rate Cards → draft version → Add surcharge → INSURANCE**, set the calculation and value, then activate the version. To charge 1%, explicitly enter 1 in Percentage (100 basis points in the API). Other rates work identically; the form has no default percentage.
- Existing `POST /api/v1/rate-cards/versions/{versionId}/surcharges` accepts percentage, fixed, and per-started-kilogram calculations, a calculation basis, service scope, conditions, priority, optional minimum/maximum charges, and the tax flag. Creation adds `conditions.requiresInsurance: true` and rejects attempts to disable it. Published versions remain immutable; change rates through a new version.
- All matching insurance rules apply once, in the existing surcharge priority order. Percentage rounding is half-up in integer minor units. The response includes each rule's ID, code, calculation, basis, percentage/amount, limits, tax setting, calculated premium, and explanation. `policyVersion` identifies the selected rate-card version. `premiumMinor` sums those charge lines; it is not an additional fee.
- `rateBp` is supplied only for a single uncapped percentage on declared value. For fixed, capped, or multiple rules, show the exact premium and rule breakdown instead of inventing a headline percentage.
- A positive declared value and an insurance-enabled courier service are required. Existing product maximum-value restrictions still apply. If no insurance rule matches, preview and booking return 409 `INSURANCE_RATE_NOT_CONFIGURED`; they never assume 1%. An explicitly configured zero premium is valid.
- The configured tax flag controls whether insurance enters the shipment tax base. Shipping discounts exclude insurance premiums from their basis.
- Without an insurance request, insurance rules are skipped, including older rules that lack a `requiresInsurance` condition. Omitted acceptance records `NOT_REQUESTED`; an explicit `{ accepted: false }` with `insuranceRequired: false` records `DECLINED`. An empty acceptance object is invalid. Preview uses `AWAITING_ACCEPTANCE`; successful insured bookings use `ACCEPTED`.
- Existing insured API clients must adopt preview plus acceptance before upgrading. Browser values never override the server premium or shipment total. Changes to the selected version, rule configuration, or calculated quote invalidate earlier acceptance.
- Saved shipments retain the accepted configuration and premium. Historical snapshots with a legacy `policyVersion` or no `rules` remain readable; they are never recalculated using today's configuration.

No production rate is automatically created by this feature. Provision or review the applicable insurance rules before accepting insured bookings. Tenant isolation, customer-specific rate-card selection, service restrictions, and existing activation permissions continue to apply.

Acceptance records the customer's agreement to the premium. It does not issue an insurer policy, certificate, claim or payment receipt.

## Customs values

`customs` is optional and includes invoice reference, export reason, terms of sale, declaration statement, currency, and 1–100 goods lines. Each line contains description, whole quantity, unit, value per unit, country of origin and optional HS/tariff code. Countries of origin must be active configured countries. Currency must match shipment currency; this version supports the existing NGN/INR/USD currency set and performs no FX conversion.

The server computes:

- Line total = quantity × value per unit.
- Goods subtotal = sum of line totals.
- Declared goods value = goods subtotal − goods discount.
- Customs invoice total = declared goods value + customs freight + insurance premium + other customs charges.

Money is nonnegative, quantities and totals are bounded, and overflow is rejected. Discount cannot exceed the goods subtotal. When customs are included, omit top-level `declaredValueMinor` or supply the exact derived value. If per-package declared values are supplied, their sum must agree too.

Customs freight and other charges describe commercial-invoice value; they do not override transport pricing or add a second charge to the shipping invoice. Customs declarations are stored and displayed, with no electronic customs submission, duty assessment or document attachment/issuance workflow.

## Independent billing parties

`billing.transportation` and `billing.dutyTax` each accept `{ party: SHIPPER | RECEIVER | THIRD_PARTY, customerId?: string }`. Defaults are shipper transportation and receiver duty/tax. Payment mode remains a separate field.

- Shipper uses the booking customer; no separate customer ID is allowed.
- Third party requires an active, authorized customer account within the same tenant.
- Receiver may omit an account for non-credit instructions. Transportation on credit requires an identified receiver account.
- Transportation credit reservation, release on cancellation, and billable-shipment selection use the selected transportation account. A receiver instruction with no account is excluded from automatic customer invoice runs, rather than silently charged to the shipper.
- Duty/tax instructions are preserved independently; this implementation does not assess, invoice or collect customs duties.

A platform-authorized operator chooses an existing account. Recording a payer instruction does not establish external carrier account authorization or third-party contractual consent.

## Country and postal identifiers

The booking form uses active countries from `GET /api/v1/geography/countries`. Migration 0038 makes GB available as reference data. Country availability is not a promise of serviceability: active postal records, zones, service areas, products and routes must already be configured. This migration does not invent UK postal coverage or carrier routes.

- Booking addresses and saved customer addresses carry `countryCode`.
- Serviceability and quote requests carry `originCountry` and `destinationCountry`; omission defaults to the organization country, falling back to NG.
- Nigeria and India require six digits with a non-zero first digit.
- UK postcodes are normalized to uppercase, with one space before the final three characters (`ss07jj` → `SS0 7JJ`).
- Other configured countries accept 3–12 letters, digits, spaces and hyphens. A postal identifier is always a string.
- Postal lookup, pricing, route resolution and immutable address snapshots use the same country. Zone mapping and service-area writes accept `countryCode`.

The existing bulk geography importer and other operational consoles remain oriented to the established domestic dataset. International reference data and lanes require deliberate geography/network provisioning. Countries without postal identifiers and country-specific customs mandates are not modeled by this change.
