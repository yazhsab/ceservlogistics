# Ecommerce merchant onboarding for C-Serve

This checklist creates one store-to-C-Serve connection. It applies to the
native Ahiabekee adapter and, later, to Shopify and WooCommerce adapters that
use `CommerceFulfillmentReady/v1`.

## 1. Merchant mapping

- Create or select one **active C-Serve customer** for the merchant/store. Save
  its public `cus_...` ID in the ecommerce connection. The shopper is the
  shipment recipient and must not be created as the logistics customer.
- Select one **active courier service code** supported for the store's lanes,
  weights, dimensions, declared values, and COD policy.
- Confirm the organization country, currency, timezone, and AWB prefix. A
  Nigerian connection uses `NG`, `NGN`, and an Africa/Lagos timezone. The
  ecommerce adapter still sends `countryCode` explicitly on both addresses.
- Decide which sender warehouse snapshot the store will use. Do not map an
  editable shopper or catalogue record into the logistics merchant identity.

## 2. Issue a least-privilege API key

Minimum scopes for booking and status synchronization:

| Scope | Why |
|---|---|
| `shipment:create` | Create a shipment at fulfillment readiness. |
| `shipment:read` | Resolve an idempotent replay and inspect the booked shipment. |
| `tracking:read` | Reconcile progress if a webhook is missed. |
| `webhook:manage` | Register and inspect this connection's webhook endpoint. |

Add `label:read` or `pod:read` only when the adapter actually downloads those
resources. Add `pickup:create`/`pickup:read` only when it manages pickups.

The API key token is returned once. Store it immediately in the ecommerce
platform's encrypted credential storage. Do not put it in source control,
ordinary environment samples, queue payloads, logs, analytics, support tickets,
or browser storage. A lost token is revoked and replaced; it is not recovered.

## 3. Register the webhook endpoint

Use `POST /api/v1/partner/webhooks/endpoints` with the API key and subscribe to
the events listed in `ecommerce-integration-v1.md`. The destination must be:

- an absolute public `https://` URL;
- on port 443 (an omitted port means 443);
- free of URL userinfo and fragments;
- resolvable only to public addresses.

Private, loopback, link-local, multicast, unspecified, ULA, special-use, cloud
metadata, mixed public/private DNS answers, and redirects are refused. C-Serve
rechecks DNS immediately before delivery and again in the validating dialer.

The signing secret is also returned once. Store it encrypted and bind it to this
specific store connection and endpoint ID. Verify `Webhook-Signature` over the
exact raw body and enforce the five-minute `Webhook-Timestamp` tolerance before
decoding JSON. Deduplicate on `Webhook-Id`.

## 4. Verify before enabling bookings

1. Confirm the API key can read only its own organization and customer scope.
2. Register the endpoint and retain its `whe_...` ID and one-time signing
   secret.
3. Send a sanitized PREPAID Nigerian contract fixture and verify one shipment,
   AWB, customer mapping, service code, packages, and `NG` snapshots.
4. Repeat with the COD fixture and verify the collectable amount in integer NGN
   minor units.
5. Verify a signed `shipment.booked` delivery, duplicate delivery handling, and
   the documented retry response policy.
6. Enable the ecommerce connection only after all checks pass.

## 5. Rotation and disablement

- Issue a replacement API key, deploy it to the store, verify traffic, then
  revoke the old key. Never reactivate a revoked credential.
- Create a replacement webhook endpoint to rotate a signing secret. Accept the
  old and new endpoint identities during the bounded cutover, then disable the
  old endpoint after its outbox is drained.
- Disabling a store connection stops new bookings. Keep shipment mappings and
  continue accepting valid webhooks for shipments already in flight until the
  explicit cutover policy says otherwise.
