# ADR 0012 — Ecommerce adapters book at fulfillment readiness

**Status:** accepted · Ecommerce integration v1

## Context

C-Serve must receive orders from the evolving Ahiabekee store and, later,
Shopify and WooCommerce. The commerce platforms know buyers, order totals and
inventory intent. C-Serve knows serviceability, routing, pricing, shipment
lifecycle, AWBs, POD and COD custody.

An order-created event is too early to create a courier shipment. It commonly
lacks the final origin location, the exact items in a partial fulfillment, the
number of physical pieces, and each piece's measured weight and dimensions.
Creating a shipment at that point either invents parcel data or forces later
mutation of facts used for routing and pricing. Both choices make duplicate and
incorrect bookings likely.

Calling C-Serve synchronously inside the commerce order or packing transaction
is also unsafe. A timeout after C-Serve commits can cause the commerce
transaction to roll back and retry, while a C-Serve outage can hold locks and
prevent the merchant from completing packing.

## Decision

The versioned boundary message is `CommerceFulfillmentReady/v1`. It is emitted
only after the commerce platform has an immutable recipient snapshot, an
authoritative sender location, payment/COD totals and package-per-piece facts in
grams and millimetres.

The native Ahiabekee implementation will own a bounded
`LogisticsIntegration` module. The fulfillment transaction will write the
canonical message to a transactional outbox; a worker will map it to the
published C-Serve `BookingRequest` and call the partner API with a stable
idempotency key. It will not import C-Serve Go packages or write logistics data.

Provider-hosted Shopify and WooCommerce adapters will live behind a bounded
commerce-integration module in the C-Serve modular monolith. They will produce
the same canonical message and the same `BookingRequest`. Their booking port
must use the existing booking/idempotency application boundary; adapters may
not write shipment tables, allocate AWBs, price parcels or implement shipment
state rules. A separate microservice is deferred until measured scaling or
security isolation needs justify the operational cost.

The three platform triggers are:

- Ahiabekee: the packed fulfillment's `readyForDispatch` transition.
- Shopify: a fulfillment-order/fulfillment-request workflow with complete
  packaging data, not `orders/create` alone.
- WooCommerce: an explicit ready-for-courier action or configured ready status
  carrying complete package data, not `order.created` alone.

The stable canonical identity is
`platform + store.id + fulfillment.id + fulfillment.version`. The shipment
idempotency key is derived from the same tuple. `referenceNumber` is a stable,
support-readable value carried by the canonical message and C-Serve shipment.

Carrier state is not commercial order state. Webhook consumers may project the
limited transitions frozen in the integration contract, but carrier
cancellation, NDR, RTO, loss, damage and COD collection cannot directly cancel,
refund, restock or settle a commerce order.

## Consequences

- A packed fulfillment can complete while C-Serve is unavailable; the outbox
  retries later without losing the booking intent.
- C-Serve receives measured parcels rather than guesses, and partial/split
  fulfillments become separate stable shipment identities.
- A timeout or duplicate delivery is safe because the same canonical identity,
  idempotency key and body are reused.
- Order creation does not immediately produce an AWB. The merchant sees the AWB
  after packing and asynchronous booking succeeds.
- Platforms must provide a packing workflow or packaging profile when their
  order APIs do not contain dimensions. Missing package facts are a validation
  error, not a reason to invent defaults.
- The canonical JSON Schema and contract fixtures become compatibility assets.
  Additive optional fields may evolve v1; breaking changes require a new schema
  name and parallel consumer support.
