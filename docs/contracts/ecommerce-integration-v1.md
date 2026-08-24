# Ecommerce integration v1 contract

**Status:** frozen for Phase 1

**Canonical schema:**
[`CommerceFulfillmentReady/v1`](schemas/commerce-fulfillment-ready-v1.schema.json)

**Authoritative courier API:** [`docs/openapi.yaml`](../openapi.yaml)

**Architecture decision:**
[`ADR 0012`](../adr/0012-ecommerce-integration-boundary-and-fulfillment-ready-booking.md)

This profile defines the smallest stable seam between commerce platforms and
C-Serve Logistics. It covers only fulfillment-ready order/customer/parcel data
moving to logistics and shipment lifecycle events moving back. Affiliate
marketing, catalogue, promotions and other commerce features are outside this
contract.

## 1. Ownership boundary

| Concern | Owner |
|---|---|
| Commercial order, buyer, discounts, tax, checkout payment, inventory | Commerce platform |
| Packing workflow and measured package pieces | Commerce platform |
| Store-to-logistics merchant connection | Integration module |
| Canonical normalization and durable delivery | Integration module |
| Serviceability, route, price, AWB and shipment lifecycle | C-Serve Logistics |
| POD, COD custody and courier exceptions | C-Serve Logistics |
| Refund, order cancellation, payment settlement and restocking | Commerce platform policy, never an automatic carrier side effect |

The C-Serve `customerId` is the active logistics customer account representing
the merchant/store. It is provisioned during connection onboarding. The buyer
is the shipment recipient; a guest or buyer must not be created as a logistics
merchant customer for every order.

Native Ahiabekee integration belongs to a bounded Laravel
`LogisticsIntegration` module and calls the C-Serve partner API. Shopify and
WooCommerce adapters belong to the bounded provider-side commerce integration
area and must normalize into this same message. No adapter may write C-Serve
shipment tables or reproduce routing, pricing, AWB or state-machine logic.

## 2. When the message exists

`CommerceFulfillmentReady/v1` means all of the following are true:

1. The fulfillment's exact line-item scope has been chosen.
2. The sender warehouse/location and immutable delivery address are known.
3. Every physical piece has measured weight in integer grams and length, width
   and height in integer millimetres.
4. Payment mode, currency, order total and the outstanding collectable amount
   are frozen for this fulfillment attempt.
5. The store has an active C-Serve merchant customer mapping and service code.

Order creation alone does not satisfy this contract. For the native store the
transactional outbox record is written during `readyForDispatch`; network I/O
occurs after commit. Shopify uses the fulfillment-order/fulfillment-request
workflow. WooCommerce uses an explicit ready-for-courier action or configured
ready status carrying the structured packages.

## 3. Canonical envelope and identities

The machine-readable field rules are in the JSON Schema. Important semantics:

| Field | Rule |
|---|---|
| `schemaVersion` | Literal `CommerceFulfillmentReady/v1`. |
| `eventType` | Literal `commerce.fulfillment_ready`. |
| `eventId` | Stable outbox/business-event identity. A retry reuses it. |
| `occurredAt` | UTC RFC3339 time at which fulfillment readiness committed. |
| `platform` | `ahiabekee`, `shopify` or `woocommerce`. |
| `store.id` | Platform-stable store/tenant identity; not a display name. |
| `order.id` | Platform-stable commercial order identity. |
| `fulfillment.id` | Platform-stable fulfillment identity. Split/partial fulfillments have different IDs. |
| `fulfillment.version` | Starts at 1; increment only for a deliberate replacement booking after the previous carrier booking is cancelled. |
| `merchant.logisticsCustomerId` | Active C-Serve merchant customer public ID. Never the buyer ID. |
| `referenceNumber` | Stable support reference, maximum 64 characters, sent unchanged to C-Serve. |
| `serviceCode` | C-Serve service chosen/configured for this connection. |

The canonical uniqueness tuple is:

```text
platform + store.id + fulfillment.id + fulfillment.version
```

The default C-Serve idempotency key is the same tuple joined by `:`. The key and
exact booking body must be reused after timeouts. A changed body requires a new
fulfillment version and an explicit replacement workflow; silently changing a
body under the same key is forbidden.

## 4. Addresses and customer information

Both sender and recipient are immutable shipment snapshots. Full addresses are
sent rather than commerce database identifiers because C-Serve cannot resolve
another application's primary keys.

- `countryCode` is always explicit ISO-3166 alpha-2. Nigerian fixtures use
  `NG`; an adapter must not rely on a server default.
- The current C-Serve serviceability key is a six-digit postal code. Nigerian
  examples use Lagos `100001` and Abuja `900001`.
- The recipient may carry `externalCustomerId` for correlation. It is metadata
  for the commerce integration and is not mapped to C-Serve `customerId`.
- Only contact and address fields needed for delivery may cross the boundary.
  Passwords, access tokens, full customer profiles, marketing consent, payment
  instrument data and identity documents are forbidden.

## 5. Package-per-piece policy

V1 supports **one to fifty physical pieces**. Every element of `packages`
represents one labelable piece and requires:

- a stable piece `reference` within the fulfillment;
- `actualWeightGrams` as a positive integer;
- `lengthMm`, `widthMm` and `heightMm` as positive integers;
- a customer-safe `contentDescription`;
- `declaredValueMinor` in the order currency.

Total shipment `declaredValueMinor` must equal the sum of all package declared
values. Missing dimensions, ambiguous units, a single total weight spread over
several pieces, or free-text dimensions do not satisfy v1 and must be rejected
before an outbox event is created.

## 6. Money and COD policy

All monetary fields are integer minor units and use `payment.currency`. For NGN
the minor unit is kobo. Decimal-to-minor conversion must use decimal arithmetic,
never binary floating point.

The commerce platform freezes these non-negative values:

```text
orderTotalMinor =
    merchandiseSubtotalMinor
  - discountMinor
  + taxMinor
  + shippingChargeMinor

outstandingAtDoorMinor = max(
    0,
    orderTotalMinor
  - prepaidAmountMinor
  - storeCreditAppliedMinor
)
```

Rules:

- `payment.mode = COD` requires `codAmountMinor` and it must equal the positive
  `outstandingAtDoorMinor` exactly.
- `payment.mode = PREPAID` forbids `codAmountMinor` and requires
  `outstandingAtDoorMinor = 0`.
- Discounts reduce the amount collected. Tax and a buyer-payable shipping
  charge are included. Captured online payment and applied store credit are
  deducted.
- `declaredValueMinor` is the goods claim/insurance value after item discounts.
  It excludes tax, buyer shipping, courier charge and COD fee unless a separate
  merchant contract explicitly changes the claim basis.
- C-Serve response `totalAmountMinor` is the courier charge billed under the
  logistics rate card. It is not the ecommerce `orderTotalMinor`.
- `cod.collected` records cash custody only. It may set
  `collected_pending_reconciliation`; it is **not payment settlement** and must
  not mark the commerce order paid. Payment becomes settled only through the
  later finance/reconciliation workflow.

## 7. Exact mapping to C-Serve `BookingRequest`

The mapper is deterministic. Fields not listed are not sent.

| `BookingRequest` field | Canonical source/default |
|---|---|
| `customerId` | `merchant.logisticsCustomerId` |
| `referenceNumber` | `referenceNumber` unchanged |
| `serviceCode` | `serviceCode` |
| `paymentMode` | `payment.mode` (`PREPAID` or `COD` in v1) |
| `sender` | `sender`, excluding `externalCustomerId` |
| `recipient` | `recipient`, excluding `externalCustomerId` |
| `packages[]` | Each canonical package, field for field |
| `declaredValueMinor` | `declaredValueMinor` |
| `codAmountMinor` | `payment.codAmountMinor` for COD; omitted for PREPAID |
| `insuranceRequired` | `insuranceRequired` |
| `contentDescription` | `contentDescription` |
| `specialInstructions` | `specialInstructions` when present |
| `isFragile` | `isFragile` |
| `isDangerousGoods` | `isDangerousGoods` |
| `bookingUnitId` | Canonical `bookingUnitId` when connection onboarding pins one; otherwise omitted so C-Serve resolves the origin branch |
| `metadata` | Generated allow-list below |

`payment.currency` must equal `store.currency` and the connected C-Serve
organization currency. It is a preflight guard but is not sent in
`BookingRequest`, because C-Serve derives currency from the authenticated
organization and rate card.

The only C-Serve booking metadata keys produced by v1 are:

```text
canonicalSchemaVersion
commerceEventId
commercePlatform
commerceStoreId
commerceOrderId
commerceFulfillmentId
commerceFulfillmentVersion
sourceChannel       (only when present)
salesChannel        (only when present)
customerNoteReference (only when present; an opaque reference, never note text)
```

Secrets, tokens, raw platform payloads and arbitrary metadata keys are
forbidden. The paired canonical and booking fixtures are executable mapping
examples.

## 8. Booking response profile

The partner call is:

```http
POST /api/v1/partner/shipments
Authorization: Bearer <partner-key-id>.<secret>
Idempotency-Key: <platform>:<store.id>:<fulfillment.id>:<version>
Content-Type: application/json
```

A `201` response, including `Idempotent-Replay: true`, is successful. The
integration persists at minimum:

- C-Serve shipment `id`;
- `awb`;
- the canonical tuple and `referenceNumber`;
- `status` (`BOOKED` at creation);
- `paymentMode`, currency and relevant amounts;
- package identities returned by C-Serve;
- booking response time and replay indicator.

The OpenAPI component `EcommerceBookingResponseV1` pins the minimum response
fields relied on by connectors. Unknown, malformed or mismatched identifiers
must not be treated as success.

## 9. Webhook envelope and verification

C-Serve sends the exact JSON bytes described by `PartnerWebhookEnvelope` and,
for shipment lifecycle events, `PartnerShipmentStatusWebhookEnvelope` in the
OpenAPI document.

Headers:

| Header | Contract |
|---|---|
| `Webhook-Signature` | `v1=<lowercase hex HMAC-SHA256>` |
| `Webhook-Id` | Equals body `id`; the sole business-event dedupe key |
| `Webhook-Timestamp` | Unix seconds used in the signed string |
| `Webhook-Event` | Equals body `type` |
| `Webhook-Attempt` | One-based delivery attempt; not a dedupe key |

Verification order:

1. Identify an active connection without revealing whether its secret exists.
2. Read the exact raw body with a configured size limit; do not reserialize it.
3. Parse `Webhook-Timestamp` and reject values outside ±300 seconds of the
   receiver clock.
4. Compute
   `v1=` + hex(HMAC-SHA256(secret, `timestamp + "." + rawBody`)).
5. Compare the complete signature in constant time and reject unknown schemes.
6. Require header ID/type to equal body ID/type.
7. Insert `(provider, Webhook-Id)` into the durable inbox under a unique
   constraint, then return 2xx. A valid duplicate returns the same 2xx without
   creating another timeline event.

Response policy:

- Return 2xx only after the verified event is durably recorded, or when that
  exact verified event was already recorded.
- Return non-2xx for invalid signature/timestamp/body and transient inability to
  persist, so C-Serve retries.
- An unknown but authentic future event type may be durably quarantined and
  acknowledged only if the receiver can retain/reprocess it safely. Otherwise
  return non-2xx and alert. Never silently map it to an order state.
- Delivery retries and manual replays carry the same `Webhook-Id`. Dedupe on ID, not type/status. Distinct `shipment.in_transit` IDs are distinct timeline facts.

## 10. Carrier-to-commerce projection policy

| C-Serve event | Carrier projection | Permitted commerce projection |
|---|---|---|
| `shipment.booked` | Save shipment ID, AWB and `BOOKED` | Keep dispatch requested |
| `shipment.picked_up` | `PICKED_UP` | Fulfillment dispatched; order shipped |
| `shipment.in_transit` | Append `IN_TRANSIT` timeline event | Keep order shipped |
| `shipment.out_for_delivery` | `OUT_FOR_DELIVERY` | Fulfillment/order out for delivery |
| `shipment.delivered` | `DELIVERED` | Fulfillment/order delivered |
| `shipment.delivery_failed` | Delivery exception | No terminal commercial change |
| `shipment.ndr` | NDR/retry exception | No terminal commercial change |
| `shipment.rto_initiated` | Return in transit | No inventory restoration |
| `shipment.rto_delivered` | Returned to merchant, awaiting warehouse receipt | Restock only after warehouse reconciliation |
| `shipment.cancelled` | Carrier booking cancelled | Do not cancel ecommerce order automatically |
| `shipment.lost` | Loss/claim exception | Do not cancel or refund automatically |
| `shipment.damaged` | Damage/claim exception | Do not cancel or refund automatically |
| `pickup.completed` | Pickup timeline/operation fact | No commercial order change by itself |
| `pod.captured` | Attach/link POD evidence | No status regression |
| `cod.collected` | Collected pending reconciliation | Do not mark paid or settled |

Commercial projections are monotonic. An older event arriving late cannot
regress `DELIVERED` or another terminal commerce state. Carrier state retains
the full ordered timeline and exception information even when the commercial
projection does not change.

### Operational event payloads

The OpenAPI profiles `PartnerPickupCompletedWebhookEnvelope`,
`PartnerPODCapturedWebhookEnvelope`, and
`PartnerCODCollectedWebhookEnvelope` freeze the three non-`shipment.*` shapes.
Their stable event IDs are respectively the pickup attempt ID, POD ID, and COD
collection ID.

- `pickup.completed` is emitted for `COMPLETED` and
  `PARTIALLY_COMPLETED` visits and carries collected shipment public IDs/AWBs.
- `pod.captured` carries evidence flags and counts, never private object keys,
  identity-document values, coordinates, or artifact bytes.
- `cod.collected` carries cash-custody facts and an explicit
  `reconciliationStatus: PENDING`. It is not payment settlement.

All producers write the webhook delivery and worker job inside the domain
transaction. No producer performs HTTP in that transaction; delivery remains
asynchronous through the webhook worker.

## 11. Compatibility and versioning

- The schema name and literal version are part of the contract.
- Additive optional fields and new optional metadata keys may be added to v1
  only after all strict consumers are updated to accept them.
- Required-field changes, meaning/unit changes, enum removals/renames, identity
  changes and status-policy changes are breaking and require
  `CommerceFulfillmentReady/v2`.
- Producers must emit one version at a time per connection. During migration,
  consumers support the old and new versions in parallel until the old outbox
  and inbox are drained.
- Unknown fields are rejected by v1. This is deliberate protection against
  accidental platform payload leakage and silent semantic drift.
- C-Serve OpenAPI remains authoritative for `BookingRequest`, booking response
  and webhook wire shapes. The canonical schema is authoritative only before
  deterministic mapping to that API.

## 12. Fixtures and operational prerequisites

All files under [`fixtures/ecommerce`](fixtures/ecommerce) are synthetic and
contain no real customer data or credentials. Contract tests validate:

- Nigerian COD and PREPAID canonical messages against the JSON Schema;
- their exact deterministic mapping to runtime-validated `BookingRequest`;
- response fixtures against `EcommerceBookingResponseV1`;
- shipment webhook fixtures against `PartnerShipmentStatusWebhookEnvelope`;
- cross-fixture shipment/reference/AWB/payment/package consistency;
- declared-value sums, checkout/COD arithmetic and metadata allow-lists.

The logistics partner boundary implements these prerequisites:

1. API-key self-service endpoint registration records the creating API key and
   leaves the nullable user actor empty.
2. Webhook targets are public HTTPS/443 only; registration, pre-delivery, and
   connection-time DNS validation reject internal/special-use targets and
   redirects are disabled. The deployment runbook requires egress filtering as
   a second layer.
3. Booking resolves an explicit address country, otherwise the authenticated
   organization country, otherwise the configured `NG` platform default. The
   same value drives postal-code resolution and immutable snapshots.
4. Every published event has a transactional producer, including
   `pickup.completed`, `pod.captured`, and `cod.collected`.

Merchant/store connection prerequisites and the one-time secret procedure are
in [`ecommerce-merchant-onboarding.md`](ecommerce-merchant-onboarding.md).
