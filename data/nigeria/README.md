# CESERVE Nigeria postcode configuration

`postcode-candidates-2026-09-20.csv` contains 2,706 unique six-digit
postcode candidates reconciled against the client-supplied state/LGA workbook.
It excludes 269 codes that the secondary sources assigned to more than one
LGA. The seed adds a reviewed CBD anchor for every state and the FCT, producing
2,717 configured postcodes in total.

The candidate sources are ZipCodes.ng and NigeriaPostal.com. Both describe
their records as based on the published NIPOST allocation, but neither is an
authoritative NIPOST bulk feed. Source confidence and URL are retained on every
row and in `pincodes.metadata` after import.

CBD/remote classification follows the client's rule:

- state-capital LGAs are CBD and do not receive the remote surcharge;
- other retained LGAs are remote and receive the configured remote surcharge.

Regenerate the explicit CESERVE seed with:

```bash
python3 scripts/generate_ceserve_nigeria_seed.py \
  --input data/nigeria/postcode-candidates-2026-09-20.csv \
  --output db/seeds/ceserve_nigeria.sql
```

The generated seed is tenant-scoped, transactional and idempotent. It creates
the postcode overlays, service areas, state branches, hubs, fallback routes and
initial retail pricing rules needed by shipment preview. The fallback routes
are deliberately low priority so an approved lane matrix supersedes them.

The new NIPOST 11-character building postcode system is outside this dataset.
Its announced national launch date is 1 October 2026, so the product still
needs an effective-dated transition plan.
