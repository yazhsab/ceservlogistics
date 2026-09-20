# Client reference PDF review

Reviewed 20 September 2026:

- `DOCUMENTATION (ODOO) (2).pdf`
- `Ceserv Demonstration - E-Commerce & Courier Management (1).pdf`
- `Tariff for University of Calabar (1).pdf`

These documents provide three different kinds of input. The Odoo document is
a booking-workflow reference. The demonstration document is a product scope
agenda. The tariff document is a locality-level carrier surcharge directory.
They must not be treated as one pricing contract.

## Odoo and UPS WorldShip booking reference

The six-page reference requests:

- sender and recipient name, company, phone, alternate phone, email, address,
  city, state, country and postal code;
- a general goods description;
- transportation billing to shipper, receiver or third party;
- a return service that creates outbound and return labels;
- total shipment weight, package count and numbered pieces;
- package types including envelope, PAK, tube and box;
- customs currency, reason for export, terms of sale, declaration statement,
  declared value, insurance, other charges and comments;
- commodity lines with description, harmonized tariff code, origin country,
  quantity, unit of measure and unit price;
- preview before label creation; and
- an auditable 20-30 minute correction period after label creation.

The current booking and commercial contract covers the address data, general
description, payer choices, multiple packages, package types, customs fields,
insurance consent and shipment preview. The following gaps remain:

1. Return service is not modeled as linked outbound and return shipments with
   separate labels.
2. Shipment correction has no server-owned `modifiableUntil`, editable-field
   policy, repricing, label invalidation or cancel-and-rebook relationship.
3. External payer instructions do not establish a carrier account or third
   party contractual authorization.

## Demonstration scope

The one-page agenda requests a wider product than shipment booking:

- ecommerce catalogue, cart, checkout and payment;
- sale-order creation after payment;
- a dropshipping vendor workflow for carrier and tracking information;
- promotions and checkout discounts;
- customer order, tracking, invoice and payment views;
- postcode masters, transit-hub mapping and parcel pricing by weight,
  distance and priority;
- parcel acceptance, payment capture, invoice creation and COD;
- internal and external courier handoffs; and
- external carrier AWB capture and barcode generation.

The repository contains courier operations, customer portal, pricing,
discount, COD, carrier, tracking and partner-integration capabilities. It does
not establish a complete ecommerce storefront or dropshipping vendor portal.
The published pricing contract supports zones, weight slabs and ordered rules;
it has no direct distance-pricing calculation. Carrier trips accept a partner
reference, but no shipment-level external-handoff contract was found that
creates a handoff number and stores an external courier AWB.

## University of Calabar tariff profile

The 58-page table was extracted and visually checked. It contains 2,707 data
rows with these fields:

```text
id, surcharge_code, surcharge_amount, city_name, centre, area_type
```

After repairing text that overflowed between PDF columns, the dominant rules
are:

| Code | Area type | Amount | Rows |
| --- | --- | ---: | ---: |
| 524 | E | NGN 850 | 1,620 |
| 629 | R | NGN 1,700 | 1,086 |

One `ZAKI / BAUCHI` row has code 524 with NGN 800 instead of NGN 850. The PDF
also contains 39 exact duplicate rows and 14 locality/centre pairs that appear
with both E and R classifications. The conflicting pairs include EDIBA,
EFFRAYA, MKPANI and UYANGA under CALABAR.

The table uses 28 carrier centres, including ABA, ABEOKUTA, ABUJA, AKURE,
BAUCHI, BENIN, CALABAR, ENUGU, IBADAN, ILORIN, JOS, KADUNA, KANO, LAGOS,
LOKOJA, MAIDUGURI, MAKURDI, MINNA, ONITSHA, OWERRI, PORT HARCOURT, SOKOTO,
UYO, WARRI, YOLA and ZARIA.

The table contains no postcode, state, LGA, weight slab, base freight, route,
SLA, effective date, expiry date, tax treatment or contract identifier. It
cannot be loaded through the postcode importer or used as a complete rate
card. The repeated and conflicting rows also require owner confirmation.

## Pricing interpretation

The supplied state/LGA workbook and client messages describe CESERVE domestic
pricing as a state base charge plus NGN 6,000 for a remote LGA. The tariff PDF
describes different E/R amounts attached to localities and carrier centres.
The Odoo reference is based on UPS WorldShip, and the demonstration agenda
separates internal CESERVE handoff from external UPS/DHL handoff.

The safe interpretation is therefore to keep two policies separate:

1. CESERVE internal service: state base freight plus the approved CESERVE
   remote-area rule.
2. External carrier service: carrier- and contract-specific E/R surcharge,
   effective-dated and linked to the external service.

The NGN 850 and NGN 1,700 values must not replace the CESERVE NGN 6,000 rule
without written confirmation that the PDF supersedes the earlier instruction.

## Data still required before production configuration

- approved legacy postcode-to-LGA/locality mappings;
- a mapping from every tariff locality to an approved postcode or service-area
  identifier;
- confirmation that E means extended area and R means remote area;
- confirmation of currency, tax treatment, contract owner and effective dates;
- resolution of the tariff duplicates, the 14 E/R conflicts and the NGN 800
  anomaly;
- the CESERVE route matrix, transit hubs and SLA hours; and
- the external-carrier base tariff or weight slabs, because this PDF provides
  surcharges only.
