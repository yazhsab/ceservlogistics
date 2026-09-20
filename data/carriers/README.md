# External carrier reference data

`university-of-calabar-surcharges-2026-09-20.csv` is a faithful structured
extraction of all 2,707 rows in the supplied *Tariff for University of Calabar*
PDF. It is reference data and is not activated in CESERVE pricing.

The file supplies locality, carrier centre, E/R classification and surcharge
amount only. It does not supply a base freight tariff, postcode mapping, weight
slabs, route/SLA, effective dates, tax treatment or a contract identifier.
There are also exact duplicates, locality/centre E/R conflicts, and one NGN
800 anomaly among the otherwise NGN 850 E rows.

Activation requires an external-carrier product and rate card plus written
resolution of those omissions. These values must remain separate from the
CESERVE internal rule of state base freight plus NGN 6,000 for a remote
destination.
