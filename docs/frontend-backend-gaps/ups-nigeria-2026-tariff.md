# UPS Nigeria 2026 tariff implementation

Reviewed source workbooks:

- `2026 RATES.xlsx`
- `2026 Updated Onforwarding Charges.xlsx`

## Activated configuration

The domestic workbook defines Lagos as the origin and four destination tariff
zones:

| Zone | Description in workbook |
| --- | --- |
| 23 | City Wide |
| 22 | Area Wide |
| 21 | Region Wide |
| 20 | Nation Wide |

Both supplied tariff variants are configured. The established
`DEMO_EXPRESS` code is retained for historical/API compatibility and displayed
as **Domestic Standard**. **Domestic Saver** uses service code
`DOMESTIC_SAVER`.

The rate card contains 424 weight rows. It uses the workbook's exact values up
to 70 kg. Above 70 kg it applies the zone's 70 kg minimum plus the supplied
price for each started additional kilogram.

The on-forwarding workbook contains 2,707 rows:

| Classification | Source code | Charge | Source rows |
| --- | ---: | ---: | ---: |
| Extended area (`E`) | 524 | NGN 5,000 | 1,621 |
| Remote area (`R`) | 629 | NGN 7,000 | 1,086 |

After normalizing punctuation and whitespace, 2,597 unique state/city entries
are conflict-free and active. Identical duplicate rows are collapsed. The 41
rows belonging to 20 contradictory state/city keys are retained in the
conflict CSV and are not used for charging.

The pricing engine chooses the domestic base zone from the exact recipient
city when it is in the on-forwarding table. The same match adds the EAS or RAS
line item. When a city has no configured on-forwarding entry, the destination
state's capital zone determines base freight and no city surcharge is
invented.

## Remaining commercial inputs

- The workbook supplies only Lagos-origin domestic rates. Directional rates
  from other origin states are not available.
- The `EXPORT` and `IMPORT` sheets contain international prices, but production
  does not yet have the corresponding country serviceability, international
  routes, customs service products and zone-to-country mappings. Those sheets
  remain inactive.
- The tariff owner must resolve the 20 contradictory on-forwarding city keys.
- Tax treatment is not stated in the workbook. On-forwarding charges follow
  the existing taxable surcharge treatment; configured tenant tax rules remain
  authoritative.
