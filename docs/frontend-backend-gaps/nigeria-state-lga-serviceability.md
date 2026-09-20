# Nigeria state/LGA serviceability and tariff contract gap

Updated 20 September 2026 from the supplied `AHIA BEKEE (STATES,L.G.A &
COST)` workbook, the client's written clarification and the three reference
PDFs reviewed in `client-reference-pdfs-2026-09-20.md`.

## Confirmed operating rules

- Nigeria is divided into the six geopolitical regions represented in the
  workbook.
- Each state capital is the state's CBD delivery point.
- The state cost in the workbook is the base freight amount for that
  destination state.
- A destination outside the state capital is remote. Its configured remote
  area charge is added to the state base cost; it does not replace the state
  cost.
- The state capital manages deliveries within its state.
- The client named Lagos, Benin, Abuja, Port Harcourt, Owerri and Enugu as
  regional or central routing hubs.

The intended destination calculation is therefore:

```text
CBD/capital destination = state base cost
remote LGA destination  = state base cost + LGA remote-area charge
```

The state base costs and six regional zones are already seeded by migration
`0037_courier_pricing_masters`. A `REMOTE_AREA` surcharge rule can represent
the additive LGA charge once the destination can be resolved as remote.

## Implemented production configuration

Shipment preview and booking resolve serviceability through active postcode,
service-area, operating-unit, route and rate-card records. The explicit
`ceserve_nigeria` configuration seed now supplies those records for CESERVE:

- 2,706 conflict-free researched postcode candidates plus state-capital CBD
  anchors, for 2,717 configured postcodes;
- six geopolitical regions, the six client-named hubs and 37 state/FCT
  branches;
- one pickup/delivery service area for every configured postcode;
- tenant postcode-to-zone mappings with server-owned CBD/remote status;
- 30 directed, low-priority fallback routes between the six hubs;
- a default NGN retail rate-card version; and
- separate REMOTE_AREA (NGN 6,000) and optional INSURANCE (1% of declared
  value) rules.

The existing state base rates remain authoritative for freight. The pricing
engine itemises the remote and insurance charges separately in preview.

## Postcode research completed on 20 September 2026

NIPOST provides an official postcode finder for individual legacy lookups, but
no public bulk API or machine-readable export was found for the current
six-digit system. The UPU Nigeria addressing guide confirms the six-digit
format and publishes a small set of examples. NIPOST's replacement site states
that an 11-character, building-level postcode system will launch nationally on
1 October 2026.

To measure the current gap, the supplied workbook was reconciled against two
secondary postcode directories that trace their records to the published
NIPOST allocation. This produced:

- 714 of 776 workbook LGA rows with one or more postcode candidates;
- 62 LGA rows requiring manual review;
- 2,706 unique, unambiguous six-digit postcode candidates;
- 269 postcodes assigned to more than one LGA by the reconciled sources, all
  excluded from the import CSV.

The review workbook is
`output/nigeria-postcode-research-2026-09-20.xlsx`. The restricted import file
is `output/nigeria-postcode-import-candidates-2026-09-20.csv`.

At the user's direction, only the conflict-free subset is loaded as provisional
serviceability data. Source URL and confidence remain attached to each postcode
for later audit. The 269 conflicting codes and 62 LGA rows without a reliable
candidate remain excluded rather than being guessed. The product also needs a
planned migration path for the new 11-character system because the current
postcode contract accepts six digits.

Research sources:

- NIPOST legacy finder: https://nipost.gov.ng/postcode-finder/
- NIPOST National Postcode System: https://www.postcode.gov.ng/
- UPU Nigeria addressing guide:
  https://www.upu.int/UPU/media/upu/PostalEntitiesFiles/addressingUnit/ngaEn.pdf
- ZipCodes.ng methodology: https://zipcodes.ng/about/
- archived NIPOST state-map index: https://www.postminer.com.ng/state-maps

## Configuration still requiring client confirmation

The six named locations are facilities; they are not an authoritative route
matrix. The following network data is still required:

- each state-to-hub assignment, especially the split between Owerri and Enugu
  and the roles of Benin and Port Harcourt;
- every permitted hub-to-hub lane, its direction, service and transit/SLA
  hours;
- whether the client's statement of "4 major routes" is authoritative, since
  six routing locations were listed;
- the UPS tariff/lane document the client said would be supplied.

Until that data arrives, the seed assigns states to the named hubs by
geopolitical region and uses a complete 48-hour fallback mesh. Every generated
route has `is_fallback=true` and priority 10; an approved service-specific or
non-fallback route wins automatically.

The supplied `Tariff for University of Calabar` PDF does not close this gap.
It contains locality-level E/R surcharge rows for carrier centres, with NGN
850 and NGN 1,700 as the dominant amounts. It contains no postcode, route,
weight slab, base freight or SLA. It therefore cannot establish postcode
serviceability or the route matrix.

The carrier surcharge data must remain separate from the client's CESERVE
domestic instruction of a state base charge plus NGN 6,000 for remote LGAs.
The PDF contains duplicate and conflicting E/R rows, so even a carrier-specific
import needs owner confirmation and an effective-dated contract.

## Workbook items requiring confirmation

- The workbook contains 776 listed LGA rows while the section headings total
  774. Niger, Adamawa, Kebbi, Cross River, Ekiti and Ogun have heading/list
  count differences.
- The client stated that the remote charge is NGN 6,000, but `Calabar
  Municipal` and `Calabar South` contain NGN 3,500 in the remote-charge column.
  The row-specific amount should be confirmed before import.
