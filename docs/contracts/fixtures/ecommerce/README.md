# Ecommerce integration contract fixtures

These payloads are synthetic. Names, phone numbers, addresses, identifiers,
amounts and credentials are examples only and do not represent real customers.

Each scenario has four stages:

1. `canonical/<scenario>.json` — `CommerceFulfillmentReady/v1` from a commerce adapter.
2. `booking-requests/<scenario>.json` — exact deterministic C-Serve `BookingRequest`.
3. `booking-responses/<scenario>.json` — minimum successful partner response profile.
4. `webhooks/<scenario>.json` — a later C-Serve shipment lifecycle delivery;
   the three `*-captured/collected/completed-ng.json` files pin the operational
   `pickup.completed`, `pod.captured`, and `cod.collected` profiles.

`tests/contract/ecommerce_integration_test.go` validates the JSON Schema,
OpenAPI components, runtime booking validation, deterministic mappings, money,
packages and cross-file identifiers. Do not update one stage without updating
the paired scenario and contract intentionally.
