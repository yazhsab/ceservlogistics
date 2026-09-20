#!/usr/bin/env python3
"""Validate the normalized UPS Nigeria 2026 data committed with the seed."""

from __future__ import annotations

import csv
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def rows(name: str) -> list[dict[str, str]]:
    with (ROOT / "data" / "carriers" / name).open(newline="", encoding="utf-8") as handle:
        return list(csv.DictReader(handle))


def main() -> None:
    rates = rows("ups-nigeria-2026-domestic-rates.csv")
    areas = rows("ups-nigeria-2026-onforwarding.csv")
    conflicts = rows("ups-nigeria-2026-onforwarding-conflicts.csv")

    assert len(rates) == 424
    assert len(areas) == 2597
    assert len(conflicts) == 41
    assert len({(r["service_code"], r["rate_zone_code"], r["from_weight_grams"]) for r in rates}) == 424
    assert len({(r["state_code"], r["normalized_city_name"]) for r in areas}) == 2597
    assert Counter((r["surcharge_type"], r["surcharge_amount_minor"]) for r in areas) == Counter(
        {("E", "500000"): 1559, ("R", "700000"): 1038}
    )
    assert len({r["conflict_key"] for r in conflicts}) == 20
    print("UPS Nigeria 2026 tariff data validation passed")


if __name__ == "__main__":
    main()
