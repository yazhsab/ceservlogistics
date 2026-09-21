# Booking observations: frontend delivery and backend gaps

Date: 2026-08-24

This audit compares the Odoo/UPS workflow reference, the August 2026 feedback
videos, and the current CServe booking contract. The frontend exposes every
observation supported by the published API. It does not create browser-only
shipment data or imply that unsupported corrections are persisted.

## Delivered through the existing contract

1. **General description of item** - the required
   `contentDescription` booking field is labelled explicitly and included in
   the shipment preview.
2. **Weight of shipment** - operators enter kilograms with gram-level
   precision; the frontend converts once to the contracted integer-gram field.
3. **States, capitals and local government** - sender and recipient forms use
   `GET /api/v1/geography/states`, `GET /api/v1/geography/districts`, and
   `GET /api/v1/geography/places`. Selecting a place fills the contracted city,
   state and postal-code fields.
4. **Preview** - the booking review now presents sender, recipient, item
   description, package count, per-package measurements, payment mode, route,
   weights, price lines and final total before submission.
5. **Number of packages** - the form exposes an explicit 1-50 package count.
   Every physical package remains a distinct request item and receives a piece
   barcode beneath the single shipment AWB.

The Nigeria reference data currently seeded by migrations is only a starter
set. All 37 states are present, but the full national postcode/LGA/locality
dataset and the supplied state/LGA cost schedule must be normalized and loaded
through the geography and pricing import/configuration contracts before every
workbook row can appear in booking and pricing.

## Backend contracts still required

### Commercial invoice and customs documentation

The booking API has shipment-level description and declared value plus
per-package description/value. It has no contracted commercial-invoice model
for commodities, harmonized tariff code, country of origin, quantity, unit of
measure, unit price, invoice currency, reason for export, terms of sale,
additional charges, or document preview/generation. The finance `Invoice`
resource is a customer billing invoice and must not be presented as a customs
commercial invoice.

Backend action: publish versioned commercial-invoice request/response schemas,
validation, immutable shipment linkage, read projection, and preview/PDF
generation endpoints. Define domestic-versus-international applicability and
permissions before the frontend form is added.

### Shipment modification window

Implemented in migration 0040 and `PATCH /api/v1/shipments/{shipmentId}`. A
shipment may be corrected while it remains `BOOKED`; the AWB, route, price,
service, package measurements, payment, declared value and customs facts stay
unchanged. Operators with `shipment.edit` may correct the customer reference,
contents, instructions, fragile flag, and non-routing sender/recipient contact
and street-address fields. A mandatory reason, optimistic concurrency version,
shipment event and audit record protect the workflow. Original booking address
snapshots remain immutable, while reprinted labels use the latest append-only
correction. Once pickup activity begins, route- or price-affecting mistakes
still require the existing controlled cancellation and rebooking workflow.
