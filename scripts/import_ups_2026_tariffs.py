#!/usr/bin/env python3
"""Extract the supplied UPS Nigeria 2026 workbooks into reviewable CSV data.

The source workbooks remain the commercial authority.  This importer makes the
subset used by CESERVE deterministic and records conflicts instead of silently
choosing between contradictory rows.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import re
import unicodedata
from collections import defaultdict
from decimal import Decimal, ROUND_HALF_UP
from pathlib import Path

from openpyxl import load_workbook


CENTRE_STATE = {
    "ABA": "AB",
    "ABUJA": "FC",
    "ABEOKUTA": "OG",
    "AKURE": "ON",
    "BAUCHI": "BA",
    "BENIN": "ED",
    "CALABAR": "CR",
    "ENUGU": "EN",
    "GOMBE": "GO",
    "IBADAN": "OY",
    "ILORIN": "KW",
    "JOS": "PL",
    "KADUNA": "KD",
    "KANO": "KN",
    "KATSINA": "KT",
    "LAGOS": "LA",
    "LOKOJA": "KO",
    "MAIDUGURI": "BO",
    "MAKURDI": "BE",
    "MINNA": "NI",
    "ONITSHA": "AN",
    "ONTISHA": "AN",
    "OWERRI": "IM",
    "PORTHARCOURT": "RI",
    "SOKOTO": "SO",
    "UYO": "AK",
    "WARRI": "DE",
    "YOLA": "AD",
    "ZARIA": "KD",
}

CENTRE_ZONE = {
    "LAGOS": "23",
    "ABEOKUTA": "22",
    "BENIN": "22",
    "IBADAN": "22",
    "ABA": "21",
    "ABUJA": "21",
    "AKURE": "21",
    "CALABAR": "21",
    "ENUGU": "21",
    "ILORIN": "21",
    "JOS": "21",
    "KADUNA": "21",
    "KANO": "21",
    "MAIDUGURI": "21",
    "ONITSHA": "21",
    "ONTISHA": "21",
    "OWERRI": "21",
    "PORTHARCOURT": "21",
    "UYO": "21",
    "WARRI": "21",
    "BAUCHI": "20",
    "GOMBE": "20",
    "KATSINA": "20",
    "LOKOJA": "20",
    "MAKURDI": "20",
    "MINNA": "20",
    "SOKOTO": "20",
    "YOLA": "20",
    "ZARIA": "20",
}


def clean(value: object) -> str:
    return " ".join(str(value or "").strip().split())


def normalized(value: object) -> str:
    ascii_value = unicodedata.normalize("NFKD", clean(value)).encode("ascii", "ignore").decode()
    return re.sub(r"[^A-Z0-9]+", "", ascii_value.upper())


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def minor_units(value: object) -> int:
    amount = Decimal(str(value)) * 100
    return int(amount.quantize(Decimal("1"), rounding=ROUND_HALF_UP))


def write_csv(path: Path, fieldnames: list[str], rows: list[dict[str, object]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames, lineterminator="\n")
        writer.writeheader()
        writer.writerows(rows)


def extract_domestic(source: Path) -> list[dict[str, object]]:
    workbook = load_workbook(source, data_only=True, read_only=True)
    result: list[dict[str, object]] = []
    sheet_services = (("Domestic saver", "DOMESTIC_SAVER"), ("Domestic Standard", "DEMO_EXPRESS"))
    zone_columns = ((10, "23"), (11, "22"), (12, "21"), (13, "20"))
    for sheet_name, service_code in sheet_services:
        sheet = workbook[sheet_name]
        weights: list[tuple[int, int]] = []
        previous_limit = -1
        for row_number in range(27, 79):
            label = clean(sheet.cell(row_number, 9).value)
            match = re.match(r"([0-9]+(?:\.[0-9]+)?)\s*kg$", label, re.IGNORECASE)
            if not match:
                continue
            upper_grams = int(Decimal(match.group(1)) * 1000)
            from_grams = 0 if previous_limit < 0 else previous_limit + 1
            to_grams = upper_grams + 1
            weights.append((from_grams, to_grams))
            for column, zone_code in zone_columns:
                value = sheet.cell(row_number, column).value
                if value is None:
                    raise ValueError(f"Missing {sheet_name} rate at {sheet.cell(row_number, column).coordinate}")
                result.append(
                    {
                        "service_code": service_code,
                        "rate_zone_code": zone_code,
                        "from_weight_grams": from_grams,
                        "to_weight_grams": to_grams,
                        "price_minor": minor_units(value),
                        "source_sheet": sheet_name,
                        "source_cell": sheet.cell(row_number, column).coordinate,
                    }
                )
            previous_limit = upper_grams

        if not weights or previous_limit != 70_000:
            raise ValueError(f"{sheet_name} did not end at the expected 70 kg slab")
        for column, zone_code in zone_columns:
            per_kg = sheet.cell(80, column).value
            minimum = sheet.cell(81, column).value
            if per_kg is None or minimum is None:
                raise ValueError(f"Missing over-70 kg values for {sheet_name} zone {zone_code}")
            result.append(
                {
                    "service_code": service_code,
                    "rate_zone_code": zone_code,
                    "from_weight_grams": 70_001,
                    "to_weight_grams": "",
                    "price_minor": minor_units(minimum),
                    "additional_step_grams": 1000,
                    "additional_price_minor": minor_units(per_kg),
                    "source_sheet": sheet_name,
                    "source_cell": f"{sheet.cell(81, column).coordinate}/{sheet.cell(80, column).coordinate}",
                }
            )
    workbook.close()
    return result


def extract_onforwarding(source: Path) -> tuple[list[dict[str, object]], list[dict[str, object]]]:
    workbook = load_workbook(source, data_only=True, read_only=True)
    sheet = workbook.active
    grouped: dict[tuple[str, str], list[dict[str, object]]] = defaultdict(list)
    for values in sheet.iter_rows(min_row=2, values_only=True):
        row_id, surcharge_code, amount, city, centre, area_type = values
        centre_normalized = normalized(centre)
        if centre_normalized not in CENTRE_STATE or centre_normalized not in CENTRE_ZONE:
            raise ValueError(f"Unknown centre area {centre!r} at source row {row_id}")
        row = {
            "source_row_id": int(row_id),
            "state_code": CENTRE_STATE[centre_normalized],
            "city_name": clean(city),
            "normalized_city_name": normalized(city),
            "centre_area": clean(centre),
            "rate_zone_code": CENTRE_ZONE[centre_normalized],
            "surcharge_code": str(int(surcharge_code)),
            "surcharge_type": clean(area_type).upper(),
            "surcharge_amount_minor": int(amount) * 100,
        }
        grouped[(row["state_code"], row["normalized_city_name"])].append(row)

    clean_rows: list[dict[str, object]] = []
    conflicts: list[dict[str, object]] = []
    for key, rows in sorted(grouped.items()):
        variants = {
            (row["surcharge_code"], row["surcharge_type"], row["surcharge_amount_minor"], row["rate_zone_code"])
            for row in rows
        }
        if len(variants) > 1:
            for row in rows:
                conflicts.append({**row, "conflict_key": f"{key[0]}:{key[1]}"})
            continue
        # Identical source duplicates do not create duplicate pricing records.
        clean_rows.append(min(rows, key=lambda row: int(row["source_row_id"])))
    workbook.close()
    return clean_rows, conflicts


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--rates", type=Path, required=True)
    parser.add_argument("--onforwarding", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()

    domestic = extract_domestic(args.rates)
    onforwarding, conflicts = extract_onforwarding(args.onforwarding)
    fields = [
        "service_code",
        "rate_zone_code",
        "from_weight_grams",
        "to_weight_grams",
        "price_minor",
        "additional_step_grams",
        "additional_price_minor",
        "source_sheet",
        "source_cell",
    ]
    for row in domestic:
        row.setdefault("additional_step_grams", "")
        row.setdefault("additional_price_minor", "")
    write_csv(args.output_dir / "ups-nigeria-2026-domestic-rates.csv", fields, domestic)
    onforwarding_fields = [
        "source_row_id",
        "state_code",
        "city_name",
        "normalized_city_name",
        "centre_area",
        "rate_zone_code",
        "surcharge_code",
        "surcharge_type",
        "surcharge_amount_minor",
    ]
    write_csv(
        args.output_dir / "ups-nigeria-2026-onforwarding.csv",
        onforwarding_fields,
        onforwarding,
    )
    write_csv(
        args.output_dir / "ups-nigeria-2026-onforwarding-conflicts.csv",
        onforwarding_fields + ["conflict_key"],
        conflicts,
    )
    manifest = args.output_dir / "ups-nigeria-2026-source-sha256.txt"
    manifest.write_text(
        f"{sha256(args.rates)}  {args.rates.name}\n{sha256(args.onforwarding)}  {args.onforwarding.name}\n",
        encoding="utf-8",
    )
    print(
        f"wrote {len(domestic)} domestic rates, {len(onforwarding)} active on-forwarding locations, "
        f"and {len(conflicts)} conflicting source rows"
    )


if __name__ == "__main__":
    main()
