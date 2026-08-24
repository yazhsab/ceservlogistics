# Mobile multi-piece delivery verification gap

**Status:** Open

**Surface:** CServe Driver (Flutter)

**Contract owner:** Backend/OpenAPI

## Operational scenario

A single consignment may be split across multiple cartons because it cannot fit
in one box. Each carton has its own carrier barcode, while the labels share a
master shipment identity, recipient, route and shipment-level weight. The
example reviewed for this requirement was explicitly labelled `1 OF 2` and
`2 OF 2`.

The correct driver workflow is:

1. show one delivery stop for the consignee;
2. show the shipment's expected piece count;
3. scan every distinct carton;
4. reject a barcode belonging to another shipment;
5. refuse shipment completion while any expected carton is absent;
6. preserve a per-piece audit trail across retries and devices.

## What the current contract supports

- `Shipment.packages[]` exposes `id`, `sequence`, `pieceBarcode` and
  `reference`.
- `DeliveryStop` exposes `shipmentId`, `awb` and `pieceCount`.
- `GET /api/v1/shipments/{shipmentId}` lets the app fetch package details.
- Scanner writes support device and event IDs for idempotent retries.

## Missing authoritative behavior

`POST /api/v1/deliveries/attempts` completes the shipment from one `barcode`.
It does not accept or verify the full set of package IDs/barcodes. The server
therefore cannot currently guarantee that all cartons were physically present
before the shipment became `DELIVERED`. A client-only set of scanned pieces is
useful error prevention, but is not authoritative and does not survive another
device taking over the stop.

External-carrier piece identity is also ambiguous. `pieceBarcode` is CServe's
generated value and `reference` is generic; neither is explicitly documented
as the carrier's scannable tracking number.

## Required contract extension

Add an idempotent, server-recorded delivery-verification operation, for example:

```text
POST /api/v1/delivery-runs/{runId}/stops/{stopId}/pieces/scan
X-Device-Id
X-Device-Event-Id

{ "barcode": "<piece or external-carrier barcode>", "occurredAt": "…" }
```

The response should identify the matched piece and return authoritative stop
progress:

```json
{
  "outcome": "ACCEPTED",
  "shipmentId": "shp_…",
  "pieceId": "pkg_…",
  "pieceSequence": 1,
  "expectedPieces": 2,
  "verifiedPieces": 1,
  "remainingPieces": 1,
  "verificationId": "dv_…"
}
```

Delivery completion should require the resulting `verificationId` (or enforce
the recorded run/stop verification directly) and return a domain conflict such
as `DELIVERY_PIECES_INCOMPLETE` with expected, verified and missing piece
details. The database transition to `DELIVERED` must perform this check.

Package identity should gain explicit fields for integrated labels, such as
`carrierCode`, `carrierTrackingNumber` and `carrierPieceBarcode`, with uniqueness
scoped appropriately. A barcode lookup must resolve both native CServe and
carrier piece barcodes without relying on a generic reference field.

## Current Flutter behavior

Until the backend extension exists, CServe Driver:

- fetches the shipment packages for the selected stop;
- matches each scan against `pieceBarcode` and `reference`;
- accepts an AWB as a piece scan only for a one-piece shipment;
- de-duplicates package scans;
- blocks the delivered action until all returned packages are scanned;
- labels this behavior in documentation as a client safety gate.

No OpenAPI or backend behavior has been invented by the Flutter clients.
