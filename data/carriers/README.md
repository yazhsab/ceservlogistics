# External carrier reference data

`university-of-calabar-surcharges-2026-09-20.csv` is a faithful structured
extraction of all 2,707 rows in the supplied *Tariff for University of Calabar*
PDF. It remains reference data and is not activated in CESERVE pricing.

The file supplies locality, carrier centre, E/R classification and surcharge
amount only. It does not supply a base freight tariff, postcode mapping, weight
slabs, route/SLA, effective dates, tax treatment or a contract identifier.
There are also exact duplicates, locality/centre E/R conflicts, and one NGN
800 anomaly among the otherwise NGN 850 E rows.

The later client-supplied workbooks are the active 2026 source:

- `ups-nigeria-2026-domestic-rates.csv` contains 424 exact domestic weight
  rows for Saver and Standard across zones 20–23, including the over-70 kg
  minimum and per-kilogram increment.
- `ups-nigeria-2026-onforwarding.csv` contains 2,597 normalized, conflict-free
  delivery cities. Extended-area rows charge NGN 5,000 and remote-area rows
  charge NGN 7,000.
- `ups-nigeria-2026-onforwarding-conflicts.csv` preserves the 41 source rows
  belonging to 20 contradictory state/city keys. They are deliberately not
  active until the tariff owner resolves each classification.
- `ups-nigeria-2026-source-sha256.txt` records the hashes of the two source
  workbooks used for the extraction.

The active domestic workbook is Lagos-origin. Other origins continue to use
the configured state-rate fallback until directional rate tables are supplied.
