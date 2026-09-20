#!/usr/bin/env python3
"""Build the reviewed CESERVE Nigeria configuration seed.

The input CSV is versioned so the generated SQL is reproducible. The seed is
deliberately scoped to the CESERVE tenant and is safe to run more than once.
"""

from __future__ import annotations

import argparse
import csv
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class StateConfig:
    code: str
    name: str
    capital: str
    capital_district: str
    capital_pincode: str
    zone: str
    hub: str


STATES = (
    StateConfig("AB", "Abia", "Umuahia", "Umuahia North", "440221", "NG-SE", "HUB_OWERRI"),
    StateConfig("AD", "Adamawa", "Yola", "Yola South", "640101", "NG-NE", "HUB_ABUJA"),
    StateConfig("AK", "Akwa Ibom", "Uyo", "Uyo", "520211", "NG-SS", "HUB_PORT_HARCOURT"),
    StateConfig("AN", "Anambra", "Awka", "Awka South", "420102", "NG-SE", "HUB_ENUGU"),
    StateConfig("BA", "Bauchi", "Bauchi", "Bauchi", "740101", "NG-NE", "HUB_ABUJA"),
    StateConfig("BY", "Bayelsa", "Yenagoa", "Yenagoa", "560212", "NG-SS", "HUB_PORT_HARCOURT"),
    StateConfig("BE", "Benue", "Makurdi", "Makurdi", "970101", "NG-NC", "HUB_ABUJA"),
    StateConfig("BO", "Borno", "Maiduguri", "Maiduguri Metropolitan", "600001", "NG-NE", "HUB_ABUJA"),
    StateConfig("CR", "Cross River", "Calabar", "Calabar Municipal", "540211", "NG-SS", "HUB_PORT_HARCOURT"),
    StateConfig("DE", "Delta", "Asaba", "Oshimili South", "320211", "NG-SS", "HUB_BENIN"),
    StateConfig("EB", "Ebonyi", "Abakaliki", "Abakaliki", "480211", "NG-SE", "HUB_ENUGU"),
    StateConfig("ED", "Edo", "Benin City", "Oredo", "300001", "NG-SS", "HUB_BENIN"),
    StateConfig("EK", "Ekiti", "Ado Ekiti", "Ado Ekiti", "360211", "NG-SW", "HUB_LAGOS"),
    StateConfig("EN", "Enugu", "Enugu", "Enugu North", "400001", "NG-SE", "HUB_ENUGU"),
    StateConfig("FC", "FCT", "Abuja", "Abuja Municipal", "900001", "NG-NC", "HUB_ABUJA"),
    StateConfig("GO", "Gombe", "Gombe", "Gombe", "760211", "NG-NE", "HUB_ABUJA"),
    StateConfig("IM", "Imo", "Owerri", "Owerri Municipal", "460211", "NG-SE", "HUB_OWERRI"),
    StateConfig("JI", "Jigawa", "Dutse", "Dutse", "720211", "NG-NW", "HUB_ABUJA"),
    StateConfig("KD", "Kaduna", "Kaduna", "Kaduna North", "800001", "NG-NW", "HUB_ABUJA"),
    StateConfig("KN", "Kano", "Kano", "Kano Municipal", "700001", "NG-NW", "HUB_ABUJA"),
    StateConfig("KT", "Katsina", "Katsina", "Katsina", "820001", "NG-NW", "HUB_ABUJA"),
    StateConfig("KE", "Kebbi", "Birnin Kebbi", "Birnin Kebbi", "860101", "NG-NW", "HUB_ABUJA"),
    StateConfig("KO", "Kogi", "Lokoja", "Lokoja", "260101", "NG-NC", "HUB_ABUJA"),
    StateConfig("KW", "Kwara", "Ilorin", "Ilorin West", "240005", "NG-NC", "HUB_ABUJA"),
    StateConfig("LA", "Lagos", "Ikeja", "Ikeja", "100001", "NG-SW", "HUB_LAGOS"),
    StateConfig("NA", "Nasarawa", "Lafia", "Lafia", "950101", "NG-NC", "HUB_ABUJA"),
    StateConfig("NI", "Niger", "Minna", "Chanchaga", "920211", "NG-NC", "HUB_ABUJA"),
    StateConfig("OG", "Ogun", "Abeokuta", "Abeokuta North", "110101", "NG-SW", "HUB_LAGOS"),
    StateConfig("ON", "Ondo", "Akure", "Akure South", "340211", "NG-SW", "HUB_LAGOS"),
    StateConfig("OS", "Osun", "Oshogbo", "Osogbo", "230211", "NG-SW", "HUB_LAGOS"),
    StateConfig("OY", "Oyo", "Ibadan", "Ibadan North", "200001", "NG-SW", "HUB_LAGOS"),
    StateConfig("PL", "Plateau", "Jos", "Jos North", "930105", "NG-NC", "HUB_ABUJA"),
    StateConfig("RI", "Rivers", "Port Harcourt", "Port Harcourt", "500001", "NG-SS", "HUB_PORT_HARCOURT"),
    StateConfig("SO", "Sokoto", "Sokoto", "Sokoto North", "840101", "NG-NW", "HUB_ABUJA"),
    StateConfig("TA", "Taraba", "Jalingo", "Jalingo", "660211", "NG-NE", "HUB_ABUJA"),
    StateConfig("YO", "Yobe", "Damaturu", "Damaturu", "620212", "NG-NE", "HUB_ABUJA"),
    StateConfig("ZA", "Zamfara", "Gusau", "Gusau", "860241", "NG-NW", "HUB_ABUJA"),
)


HUBS = (
    ("HUB_LAGOS", "Lagos Regional Hub", "LA", "100001", "NG-SW"),
    ("HUB_BENIN", "Benin Central Hub", "ED", "300001", "NG-SS"),
    ("HUB_ABUJA", "Abuja Northern Hub", "FC", "900001", "NG-NC"),
    ("HUB_PORT_HARCOURT", "Port Harcourt South South Hub", "RI", "500001", "NG-SS"),
    ("HUB_OWERRI", "Owerri Eastern Hub", "IM", "460211", "NG-SE"),
    ("HUB_ENUGU", "Enugu Eastern Hub", "EN", "400001", "NG-SE"),
)


CBD_DISTRICTS = {
    "AB": {"umuahia north", "umuahia south", "ugwunagbo"},
    "AD": {"yola north", "yola south"},
    "AK": {"uyo"},
    "AN": {"awka north", "awka south"},
    "BA": {"bauchi"},
    "BY": {"yenagoa", "yenagoa lga"},
    "BE": {"makurdi"},
    "BO": {"maiduguri", "maiduguri metropolitan"},
    "CR": {"calabar municipal", "calabar south"},
    "DE": {"oshimili south"},
    "EB": {"abakaliki"},
    "ED": {"oredo"},
    "EK": {"ado ekiti"},
    "EN": {"enugu north", "enugu south", "enugu east"},
    "FC": {"abuja municipal"},
    "GO": {"gombe", "gombe lga"},
    "IM": {"owerri municipal", "owerri north", "owerri west"},
    "JI": {"dutse"},
    "KD": {"kaduna north", "kaduna south"},
    "KN": {"kano municipal"},
    "KT": {"katsina"},
    "KE": {"birnin kebbi"},
    "KO": {"lokoja"},
    "KW": {"ilorin east", "ilorin south", "ilorin west"},
    "LA": {"ikeja"},
    "NA": {"lafia"},
    "NI": {"chanchaga"},
    "OG": {"abeokuta north", "abeokuta south"},
    "ON": {"akure north", "akure south"},
    "OS": {"osogbo", "oshogbo"},
    "OY": {"ibadan north", "ibadan north east", "ibadan north west", "ibadan south east", "ibadan south west"},
    "PL": {"jos north", "jos south"},
    "RI": {"port harcourt", "port harcourt city", "obio-akpor"},
    "SO": {"sokoto north", "sokoto south"},
    "TA": {"jalingo"},
    "YO": {"damaturu"},
    "ZA": {"gusau"},
}


# The 2026 domestic workbook is a Lagos-origin tariff.  Its destination list
# assigns each state capital to one of four commercial zones.  Kaduna is zone
# 21 by default; Zaria is a city-level zone-20 override in the on-forwarding
# source.
STATE_RATE_ZONES = {
    "LA": "23",
    "OG": "22", "ED": "22", "OS": "22", "OY": "22",
    "AB": "21", "FC": "21", "EK": "21", "ON": "21", "CR": "21",
    "EB": "21", "EN": "21", "KW": "21", "PL": "21", "KD": "21",
    "KN": "21", "JI": "21", "YO": "21", "BO": "21", "AN": "21",
    "IM": "21", "BY": "21", "RI": "21", "AK": "21", "DE": "21",
    "BA": "20", "GO": "20", "KT": "20", "KO": "20", "NA": "20",
    "NI": "20", "BE": "20", "KE": "20", "ZA": "20", "SO": "20",
    "TA": "20", "AD": "20",
}


def sql(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def canonical(value: str) -> str:
    value = " ".join(value.strip().split())
    return value.title() if value.isupper() else value


def read_candidates(path: Path) -> list[dict[str, str]]:
    name_to_code = {state.name.casefold(): state.code for state in STATES}
    name_to_code["fct"] = "FC"
    rows: list[dict[str, str]] = []
    with path.open(newline="", encoding="utf-8") as handle:
        for row in csv.DictReader(handle):
            state_code = row.get("state_code", "").strip().upper()
            if not state_code:
                state_code = name_to_code.get(row["state"].strip().casefold(), "")
            if state_code not in CBD_DISTRICTS:
                raise ValueError(f"Unknown Nigerian state in {path}: {row['state']!r}")
            district = canonical(row["district"])
            rows.append(
                {
                    "pincode": row["pincode"].strip(),
                    "state_code": state_code,
                    "district": district,
                    "city": canonical(row["city"]),
                    "office_name": canonical(row["office_name"]),
                    "is_remote": "false" if district.casefold() in CBD_DISTRICTS[state_code] else "true",
                    "confidence": row.get("confidence", "MEDIUM").strip().upper(),
                    "source_url": row.get("source_url", "").strip(),
                }
            )
    if len({row["pincode"] for row in rows}) != len(rows):
        raise ValueError("Candidate input must contain one row per postcode")
    return sorted(rows, key=lambda row: row["pincode"])


def read_csv(path: Path) -> list[dict[str, str]]:
    with path.open(newline="", encoding="utf-8") as handle:
        return list(csv.DictReader(handle))


def values(rows: list[tuple[str, ...]]) -> str:
    return ",\n".join("    (" + ", ".join(sql(v) for v in row) + ")" for row in rows)


def build_seed(
    candidates: list[dict[str, str]],
    domestic_rates: list[dict[str, str]],
    onforwarding: list[dict[str, str]],
) -> str:
    candidate_values = []
    for row in candidates:
        candidate_values.append(
            "    ("
            + ", ".join(
                [
                    sql(row["pincode"]),
                    sql(row["state_code"]),
                    sql(row["district"]),
                    sql(row["city"]),
                    sql(row["office_name"]),
                    row["is_remote"],
                    sql(row["confidence"]),
                    sql(row["source_url"]),
                    sql("SECONDARY_CANDIDATE"),
                ]
            )
            + ")"
        )

    capital_values = values(
        [
            (
                state.capital_pincode,
                state.code,
                state.capital_district,
                state.capital,
                state.capital,
                "false",
                "CAPITAL_OVERRIDE",
                "Client state/LGA workbook and NIPOST zone-anchor set",
                "CAPITAL_CBD",
            )
            for state in STATES
        ]
    ).replace("'false'", "false")
    state_values = values(
        [(s.code, s.capital, s.capital_pincode, s.zone, s.hub) for s in STATES]
    )
    hub_values = values(list(HUBS))
    route_values = []
    for origin, origin_name, *_ in HUBS:
        for destination, destination_name, *_ in HUBS:
            if origin == destination:
                continue
            route_values.append(
                (
                    f"FB_{origin.removeprefix('HUB_')}_{destination.removeprefix('HUB_')}",
                    f"Fallback {origin_name} to {destination_name}",
                    origin,
                    destination,
                )
            )

    domestic_rate_values = []
    for row in domestic_rates:
        domestic_rate_values.append(
            "    ("
            + ", ".join(
                [
                    sql(row["service_code"]),
                    sql(row["rate_zone_code"]),
                    row["from_weight_grams"],
                    row["to_weight_grams"] or "NULL",
                    row["price_minor"],
                    row.get("additional_step_grams", "") or "NULL",
                    row.get("additional_price_minor", "") or "NULL",
                    sql(row["source_sheet"]),
                    sql(row["source_cell"]),
                ]
            )
            + ")"
        )
    onforwarding_values = []
    for row in onforwarding:
        onforwarding_values.append(
            "    ("
            + ", ".join(
                [
                    row["source_row_id"],
                    sql(row["state_code"]),
                    sql(row["city_name"]),
                    sql(row["normalized_city_name"]),
                    sql(row["centre_area"]),
                    sql(row["rate_zone_code"]),
                    sql(row["surcharge_code"]),
                    sql(row["surcharge_type"]),
                    row["surcharge_amount_minor"],
                ]
            )
            + ")"
        )
    state_rate_zone_values = values(sorted(STATE_RATE_ZONES.items()))

    return f"""-- CESERVE Nigeria production configuration.
-- Generated by scripts/generate_ceserve_nigeria_seed.py.
--
-- The postcode set contains only the 2,706 unambiguous candidates retained
-- from the 20 September 2026 reconciliation, plus one CBD anchor for every
-- state/FCT. Conflicting postcode assignments are intentionally excluded.
--
-- Routes are low-priority, explicitly marked fallback routes. They provide a
-- safe nationwide preview path until the client supplies the authoritative
-- lane matrix; a configured non-fallback route always wins.
--
-- Version 2 uses the supplied UPS Nigeria 2026 domestic rates. The workbook
-- defines Lagos as the origin. Other origins retain the state-rate fallback
-- until the client supplies their directional tariff matrices.

BEGIN;

DO $$
BEGIN
    IF (SELECT count(*) FROM organizations WHERE code = 'CESERVE') <> 1 THEN
        RAISE EXCEPTION 'CESERVE tenant must exist exactly once';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM countries WHERE iso2 = 'NG' AND status = 'ACTIVE') THEN
        RAISE EXCEPTION 'Active NG country reference is missing';
    END IF;
    IF (SELECT count(*) FROM states s JOIN countries c ON c.id=s.country_id
        WHERE c.iso2='NG' AND s.status='ACTIVE') <> 37 THEN
        RAISE EXCEPTION 'Expected all 37 Nigeria state/FCT references';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM courier_services s JOIN organizations o ON o.id=s.organization_id
        WHERE o.code='CESERVE' AND s.code='DEMO_EXPRESS' AND s.status='ACTIVE') THEN
        RAISE EXCEPTION 'CESERVE DEMO_EXPRESS courier service is missing';
    END IF;
END $$;

-- Preserve the established service code used by booking and historical
-- shipments while presenting the commercial product name from the workbook.
UPDATE courier_services s
SET name='Domestic Standard',
    description='UPS Nigeria 2026 Domestic Standard tariff',
    max_weight_grams=1000000,
    sort_order=10,
    updated_at=now()
FROM organizations o
WHERE s.organization_id=o.id AND o.code='CESERVE' AND s.code='DEMO_EXPRESS';

INSERT INTO courier_services(public_id,organization_id,code,name,description,mode,
    min_weight_grams,max_weight_grams,max_length_mm,max_width_mm,max_height_mm,
    max_dimension_sum_mm,volumetric_divisor,weight_rounding_grams,cod_allowed,
    max_cod_amount_minor,insurance_allowed,max_declared_value_minor,sla_transit_hours,
    sla_rules,cutoff_time,status,effective_from,sort_order)
SELECT gen_seed_public_id('svc'),s.organization_id,'DOMESTIC_SAVER','Domestic Saver',
       'UPS Nigeria 2026 Domestic Saver tariff',s.mode,s.min_weight_grams,1000000,
       s.max_length_mm,s.max_width_mm,s.max_height_mm,s.max_dimension_sum_mm,
       s.volumetric_divisor,s.weight_rounding_grams,s.cod_allowed,s.max_cod_amount_minor,
       s.insurance_allowed,s.max_declared_value_minor,s.sla_transit_hours,s.sla_rules,
       s.cutoff_time,'ACTIVE','2026-09-20',20
FROM courier_services s JOIN organizations o ON o.id=s.organization_id AND o.code='CESERVE'
WHERE s.code='DEMO_EXPRESS'
ON CONFLICT (organization_id,code) DO UPDATE SET
    name=EXCLUDED.name,description=EXCLUDED.description,max_weight_grams=EXCLUDED.max_weight_grams,
    status='ACTIVE',sort_order=EXCLUDED.sort_order;

CREATE TEMP TABLE ceserve_ng_postcodes (
    pincode text PRIMARY KEY,
    state_code text NOT NULL,
    district text NOT NULL,
    city text NOT NULL,
    office_name text NOT NULL,
    is_remote boolean NOT NULL,
    confidence text NOT NULL,
    source_url text NOT NULL,
    source_kind text NOT NULL
) ON COMMIT DROP;

INSERT INTO ceserve_ng_postcodes VALUES
{',\n'.join(candidate_values)};

-- State-capital CBD anchors override a candidate classification when the same
-- postcode occurs in the reconciled data.
INSERT INTO ceserve_ng_postcodes VALUES
{capital_values}
ON CONFLICT (pincode) DO UPDATE SET
    state_code=EXCLUDED.state_code,
    district=EXCLUDED.district,
    city=EXCLUDED.city,
    office_name=EXCLUDED.office_name,
    is_remote=false,
    confidence=EXCLUDED.confidence,
    source_url=EXCLUDED.source_url,
    source_kind=EXCLUDED.source_kind;

-- Insert missing LGAs. Existing official rows are reused by case-insensitive
-- name, avoiding duplicate Ikeja/Abuja Municipal records.
WITH needed AS (
    SELECT DISTINCT s.id AS state_id, p.district AS name
    FROM ceserve_ng_postcodes p
    JOIN countries c ON c.iso2='NG'
    JOIN states s ON s.country_id=c.id AND s.code=p.state_code
)
INSERT INTO districts(public_id,state_id,code,name,status)
SELECT gen_seed_public_id('dst'), n.state_id,
       left(regexp_replace(upper(n.name),'[^A-Z0-9]+','_','g'),20) || '_' || upper(substr(md5(lower(n.name)),1,8)),
       n.name, 'ACTIVE'
FROM needed n
WHERE NOT EXISTS (
    SELECT 1 FROM districts d WHERE d.state_id=n.state_id AND lower(d.name)=lower(n.name)
);

WITH needed AS (
    SELECT DISTINCT ON (s.id, p.city) s.id AS state_id, d.id AS district_id, p.city AS name,
        CASE WHEN p.city IN ('Lagos','Abuja') THEN 'METRO'
             WHEN NOT p.is_remote THEN 'TIER_1' ELSE 'OTHER' END AS tier
    FROM ceserve_ng_postcodes p
    JOIN countries c ON c.iso2='NG'
    JOIN states s ON s.country_id=c.id AND s.code=p.state_code
    LEFT JOIN districts d ON d.state_id=s.id AND lower(d.name)=lower(p.district)
    ORDER BY s.id,p.city,d.id
)
INSERT INTO cities(public_id,state_id,district_id,name,tier,status)
SELECT gen_seed_public_id('cty'),n.state_id,n.district_id,n.name,n.tier,'ACTIVE'
FROM needed n
WHERE NOT EXISTS (
    SELECT 1 FROM cities c WHERE c.state_id=n.state_id AND lower(c.name)=lower(n.name)
);

INSERT INTO pincodes(public_id,country_id,code,state_id,district_id,city_id,office_name,is_remote,status,metadata)
SELECT gen_seed_public_id('pin'),c.id,p.pincode,s.id,d.id,ct.id,p.office_name,p.is_remote,'ACTIVE',
       jsonb_build_object('sourceKind',p.source_kind,'sourceConfidence',p.confidence,
                          'sourceUrl',p.source_url,'reviewedAt','2026-09-20')
FROM ceserve_ng_postcodes p
JOIN countries c ON c.iso2='NG'
JOIN states s ON s.country_id=c.id AND s.code=p.state_code
LEFT JOIN districts d ON d.state_id=s.id AND lower(d.name)=lower(p.district)
LEFT JOIN cities ct ON ct.state_id=s.id AND lower(ct.name)=lower(p.city)
ON CONFLICT (country_id,code) DO UPDATE SET
    state_id=EXCLUDED.state_id,
    district_id=COALESCE(EXCLUDED.district_id,pincodes.district_id),
    city_id=COALESCE(EXCLUDED.city_id,pincodes.city_id),
    office_name=EXCLUDED.office_name,
    is_remote=EXCLUDED.is_remote,
    status='ACTIVE',
    metadata=pincodes.metadata || EXCLUDED.metadata;

INSERT INTO localities(public_id,pincode_id,name,status)
SELECT gen_seed_public_id('loc'),p.id,v.name,'ACTIVE'
FROM ceserve_ng_postcodes src
JOIN countries c ON c.iso2='NG'
JOIN pincodes p ON p.country_id=c.id AND p.code=src.pincode
CROSS JOIN LATERAL (VALUES (src.city),(src.office_name)) v(name)
WHERE btrim(v.name) <> ''
ON CONFLICT (pincode_id,name) DO NOTHING;

CREATE TEMP TABLE ceserve_state_network(
    state_code text PRIMARY KEY,
    capital text NOT NULL,
    capital_pincode text NOT NULL,
    region_code text NOT NULL,
    hub_code text NOT NULL
) ON COMMIT DROP;
INSERT INTO ceserve_state_network VALUES
{state_values};

CREATE TEMP TABLE ceserve_hubs(
    code text PRIMARY KEY,
    name text NOT NULL,
    state_code text NOT NULL,
    pincode text NOT NULL,
    region_code text NOT NULL
) ON COMMIT DROP;
INSERT INTO ceserve_hubs VALUES
{hub_values};

INSERT INTO regions(public_id,organization_id,code,name,status)
SELECT gen_seed_public_id('rgn'),o.id,z.code,z.name,'ACTIVE'
FROM organizations o
JOIN zones z ON z.organization_id=o.id AND z.status='ACTIVE'
WHERE o.code='CESERVE' AND z.code LIKE 'NG-%'
ON CONFLICT (organization_id,code) DO NOTHING;

INSERT INTO operating_units(public_id,organization_id,code,name,unit_type,region_id,status,
    address_line1,pincode,pincode_id,city_id,state_id,operating_hours)
SELECT gen_seed_public_id('ou'),o.id,h.code,h.name,'REGIONAL_HUB',r.id,'ACTIVE',
       h.name,p.code,p.id,p.city_id,p.state_id,'{{}}'::jsonb
FROM ceserve_hubs h
JOIN organizations o ON o.code='CESERVE'
JOIN regions r ON r.organization_id=o.id AND r.code=h.region_code
JOIN countries c ON c.iso2='NG'
JOIN pincodes p ON p.country_id=c.id AND p.code=h.pincode
ON CONFLICT (organization_id,code) DO NOTHING;

INSERT INTO operating_units(public_id,organization_id,code,name,unit_type,parent_unit_id,region_id,status,
    address_line1,pincode,pincode_id,city_id,state_id,operating_hours)
SELECT gen_seed_public_id('ou'),o.id,'BR_'||n.state_code,n.capital||' State Branch','COMPANY_BRANCH',
       hub.id,r.id,'ACTIVE',n.capital||' CBD',p.code,p.id,p.city_id,p.state_id,'{{}}'::jsonb
FROM ceserve_state_network n
JOIN organizations o ON o.code='CESERVE'
JOIN operating_units hub ON hub.organization_id=o.id AND hub.code=n.hub_code
JOIN regions r ON r.organization_id=o.id AND r.code=n.region_code
JOIN countries c ON c.iso2='NG'
JOIN pincodes p ON p.country_id=c.id AND p.code=n.capital_pincode
ON CONFLICT (organization_id,code) DO NOTHING;

INSERT INTO operating_unit_capabilities(organization_id,operating_unit_id,capability,enabled)
SELECT o.id,u.id,c.capability,true
FROM organizations o
JOIN operating_units u ON u.organization_id=o.id
CROSS JOIN (VALUES ('BAGGING'),('MANIFEST'),('LINEHAUL_ORIGIN'),('LINEHAUL_DESTINATION'),
                   ('TRANSIT'),('WAREHOUSING')) c(capability)
WHERE o.code='CESERVE' AND u.code LIKE 'HUB_%'
ON CONFLICT (operating_unit_id,capability) DO UPDATE SET enabled=true;

INSERT INTO operating_unit_capabilities(organization_id,operating_unit_id,capability,enabled)
SELECT o.id,u.id,c.capability,true
FROM organizations o
JOIN operating_units u ON u.organization_id=o.id
CROSS JOIN (VALUES ('BOOKING'),('PICKUP'),('DELIVERY'),('CUSTOMER_WALKIN'),
                   ('COD_COLLECTION'),('RTO_PROCESSING')) c(capability)
WHERE o.code='CESERVE' AND u.code LIKE 'BR_%'
ON CONFLICT (operating_unit_id,capability) DO UPDATE SET enabled=true;

INSERT INTO pincode_zone_mappings(public_id,organization_id,pincode_id,zone_id,is_remote_override,
    effective_from,status)
SELECT gen_seed_public_id('zmp'),o.id,p.id,z.id,src.is_remote,'2026-09-20 00:00:00+00','ACTIVE'
FROM ceserve_ng_postcodes src
JOIN organizations o ON o.code='CESERVE'
JOIN countries c ON c.iso2='NG'
JOIN pincodes p ON p.country_id=c.id AND p.code=src.pincode
JOIN ceserve_state_network n ON n.state_code=src.state_code
JOIN zones z ON z.organization_id=o.id AND z.code=n.region_code
ON CONFLICT (organization_id,pincode_id) WHERE status='ACTIVE'
DO UPDATE SET zone_id=EXCLUDED.zone_id,is_remote_override=EXCLUDED.is_remote_override;

INSERT INTO service_areas(public_id,organization_id,operating_unit_id,pincode_id,area_type,
    priority,is_remote,effective_from,status)
SELECT gen_seed_public_id('sva'),o.id,b.id,p.id,'BOTH',100,src.is_remote,
       '2026-09-20 00:00:00+00','ACTIVE'
FROM ceserve_ng_postcodes src
JOIN organizations o ON o.code='CESERVE'
JOIN countries c ON c.iso2='NG'
JOIN pincodes p ON p.country_id=c.id AND p.code=src.pincode
JOIN operating_units b ON b.organization_id=o.id AND b.code='BR_'||src.state_code
ON CONFLICT (organization_id,operating_unit_id,pincode_id,area_type) WHERE status='ACTIVE'
DO UPDATE SET priority=EXCLUDED.priority,is_remote=EXCLUDED.is_remote;

CREATE TEMP TABLE ceserve_fallback_routes(
    code text PRIMARY KEY,
    name text NOT NULL,
    origin_code text NOT NULL,
    destination_code text NOT NULL
) ON COMMIT DROP;
INSERT INTO ceserve_fallback_routes VALUES
{values(route_values)};

INSERT INTO route_definitions(public_id,organization_id,code,name,origin_unit_id,destination_unit_id,
    courier_service_id,priority,transit_hours,is_fallback,effective_from,status)
SELECT gen_seed_public_id('rte'),o.id,r.code,r.name,orig.id,dest.id,svc.id,10,48,true,
       '2026-09-20 00:00:00+00','ACTIVE'
FROM ceserve_fallback_routes r
JOIN organizations o ON o.code='CESERVE'
JOIN operating_units orig ON orig.organization_id=o.id AND orig.code=r.origin_code
JOIN operating_units dest ON dest.organization_id=o.id AND dest.code=r.destination_code
JOIN courier_services svc ON svc.organization_id=o.id AND svc.code='DEMO_EXPRESS' AND svc.status='ACTIVE'
ON CONFLICT (organization_id,code) DO NOTHING;

INSERT INTO route_legs(public_id,organization_id,route_definition_id,sequence,from_unit_id,to_unit_id,
    mode,transit_hours)
SELECT gen_seed_public_id('rtl'),rd.organization_id,rd.id,1,rd.origin_unit_id,rd.destination_unit_id,
       'ROAD',rd.transit_hours
FROM route_definitions rd
JOIN organizations o ON o.id=rd.organization_id AND o.code='CESERVE'
JOIN ceserve_fallback_routes r ON r.code=rd.code
ON CONFLICT (route_definition_id,sequence) DO NOTHING;

INSERT INTO route_definitions(public_id,organization_id,code,name,origin_unit_id,destination_unit_id,
    courier_service_id,priority,transit_hours,is_fallback,effective_from,status)
SELECT gen_seed_public_id('rte'),o.id,
       'FB_SAVER_' || replace(orig.code,'HUB_','') || '_' || replace(dest.code,'HUB_',''),
       'Saver fallback ' || orig.name || ' to ' || dest.name,orig.id,dest.id,svc.id,10,48,true,
       '2026-09-20 00:00:00+00','ACTIVE'
FROM organizations o
JOIN operating_units orig ON orig.organization_id=o.id AND orig.unit_type='REGIONAL_HUB' AND orig.status='ACTIVE'
JOIN operating_units dest ON dest.organization_id=o.id AND dest.unit_type='REGIONAL_HUB' AND dest.status='ACTIVE' AND dest.id<>orig.id
JOIN courier_services svc ON svc.organization_id=o.id AND svc.code='DOMESTIC_SAVER' AND svc.status='ACTIVE'
WHERE o.code='CESERVE'
ON CONFLICT (organization_id,code) DO NOTHING;

INSERT INTO route_legs(public_id,organization_id,route_definition_id,sequence,from_unit_id,to_unit_id,
    mode,transit_hours)
SELECT gen_seed_public_id('rtl'),rd.organization_id,rd.id,1,rd.origin_unit_id,rd.destination_unit_id,
       'ROAD',rd.transit_hours
FROM route_definitions rd
JOIN organizations o ON o.id=rd.organization_id AND o.code='CESERVE'
WHERE rd.code LIKE 'FB_SAVER_%'
ON CONFLICT (route_definition_id,sequence) DO NOTHING;

INSERT INTO rate_cards(public_id,organization_id,code,name,description,scope,currency,is_default,status)
SELECT gen_seed_public_id('rc'),o.id,'CESERVE_RETAIL','CESERVE Retail Tariff',
       'State base freight with separately itemised remote-area and optional insurance charges',
       'RETAIL',o.currency,true,'ACTIVE'
FROM organizations o WHERE o.code='CESERVE'
ON CONFLICT (organization_id,code) DO NOTHING;

INSERT INTO rate_card_versions(public_id,organization_id,rate_card_id,version,status,effective_from,notes)
SELECT gen_seed_public_id('rcv'),rc.organization_id,rc.id,1,'DRAFT','2026-09-20 00:00:00+00',
       'Initial CESERVE state/LGA tariff. Remote NGN 6,000; insurance 1% when requested.'
FROM rate_cards rc JOIN organizations o ON o.id=rc.organization_id AND o.code='CESERVE'
WHERE rc.code='CESERVE_RETAIL'
  AND NOT EXISTS (SELECT 1 FROM rate_card_versions v WHERE v.rate_card_id=rc.id);

INSERT INTO surcharge_rules(public_id,organization_id,rate_card_version_id,code,name,surcharge_type,
    calc_type,value_minor,applies_to,courier_service_id,conditions,priority,is_taxable)
SELECT gen_seed_public_id('sur'),v.organization_id,v.id,'REMOTE_AREA_NG','Remote area charge',
       'REMOTE_AREA','FIXED',600000,'FREIGHT',svc.id,'{{"remoteDestination":true}}'::jsonb,100,true
FROM rate_card_versions v
JOIN rate_cards rc ON rc.id=v.rate_card_id AND rc.code='CESERVE_RETAIL'
JOIN organizations o ON o.id=v.organization_id AND o.code='CESERVE'
JOIN courier_services svc ON svc.organization_id=o.id AND svc.code='DEMO_EXPRESS'
WHERE v.status='DRAFT'
ON CONFLICT (rate_card_version_id,code) DO NOTHING;

INSERT INTO surcharge_rules(public_id,organization_id,rate_card_version_id,code,name,surcharge_type,
    calc_type,percentage_bp,applies_to,courier_service_id,conditions,priority,is_taxable)
SELECT gen_seed_public_id('sur'),v.organization_id,v.id,'SHIPMENT_INSURANCE','Shipment insurance',
       'INSURANCE','PERCENTAGE',100,'DECLARED_VALUE',svc.id,
       '{{"requiresInsurance":true}}'::jsonb,200,false
FROM rate_card_versions v
JOIN rate_cards rc ON rc.id=v.rate_card_id AND rc.code='CESERVE_RETAIL'
JOIN organizations o ON o.id=v.organization_id AND o.code='CESERVE'
JOIN courier_services svc ON svc.organization_id=o.id AND svc.code='DEMO_EXPRESS'
WHERE v.status='DRAFT'
ON CONFLICT (rate_card_version_id,code) DO NOTHING;

UPDATE rate_card_versions v
SET status='ACTIVE',activated_at=now()
FROM rate_cards rc, organizations o
WHERE v.rate_card_id=rc.id AND rc.organization_id=o.id
  AND o.code='CESERVE' AND rc.code='CESERVE_RETAIL' AND v.version=1 AND v.status='DRAFT';

-- UPS Nigeria 2026 tariff version. Active rate-card rows are immutable, so
-- updating prices always creates/supersedes a complete version.
INSERT INTO rate_card_versions(public_id,organization_id,rate_card_id,version,status,effective_from,notes)
SELECT gen_seed_public_id('rcv'),rc.organization_id,rc.id,2,'DRAFT','2026-09-20 15:30:00+00',
       'UPS Nigeria 2026 domestic Saver/Standard slabs; EAS NGN 5,000; RAS NGN 7,000; insurance 1%.'
FROM rate_cards rc JOIN organizations o ON o.id=rc.organization_id AND o.code='CESERVE'
WHERE rc.code='CESERVE_RETAIL'
ON CONFLICT (rate_card_id,version) DO NOTHING;

CREATE TEMP TABLE ceserve_domestic_state_zones (
    destination_state_code text PRIMARY KEY,
    rate_zone_code text NOT NULL
) ON COMMIT DROP;
INSERT INTO ceserve_domestic_state_zones VALUES
{state_rate_zone_values};

CREATE TEMP TABLE ceserve_domestic_rates (
    service_code text NOT NULL,
    rate_zone_code text NOT NULL,
    from_weight_grams integer NOT NULL,
    to_weight_grams integer,
    price_minor bigint NOT NULL,
    additional_step_grams integer,
    additional_price_minor bigint,
    source_sheet text NOT NULL,
    source_cell text NOT NULL
) ON COMMIT DROP;
INSERT INTO ceserve_domestic_rates VALUES
{',\n'.join(domestic_rate_values)};

CREATE TEMP TABLE ceserve_onforwarding (
    source_row_id integer NOT NULL,
    state_code text NOT NULL,
    city_name text NOT NULL,
    normalized_city_name text NOT NULL,
    centre_area text NOT NULL,
    rate_zone_code text NOT NULL,
    surcharge_code text NOT NULL,
    surcharge_type text NOT NULL,
    surcharge_amount_minor bigint NOT NULL
) ON COMMIT DROP;
INSERT INTO ceserve_onforwarding VALUES
{',\n'.join(onforwarding_values)};

INSERT INTO domestic_state_rate_zones(public_id,organization_id,rate_card_version_id,
    origin_state_id,destination_state_id,rate_zone_code)
SELECT gen_seed_public_id('dsz'),o.id,v.id,origin.id,dest.id,x.rate_zone_code
FROM organizations o
JOIN countries c ON c.iso2='NG'
JOIN states origin ON origin.country_id=c.id AND origin.code='LA'
JOIN ceserve_domestic_state_zones x ON true
JOIN states dest ON dest.country_id=c.id AND dest.code=x.destination_state_code
JOIN rate_cards rc ON rc.organization_id=o.id AND rc.code='CESERVE_RETAIL'
JOIN rate_card_versions v ON v.rate_card_id=rc.id AND v.version=2 AND v.status='DRAFT'
WHERE o.code='CESERVE'
ON CONFLICT (rate_card_version_id,origin_state_id,destination_state_id) DO NOTHING;

INSERT INTO domestic_weight_slabs(public_id,organization_id,rate_card_version_id,courier_service_id,
    origin_state_id,rate_zone_code,from_weight_grams,to_weight_grams,price_minor,
    additional_step_grams,additional_price_minor,source_sheet,source_cell)
SELECT gen_seed_public_id('dws'),o.id,v.id,svc.id,origin.id,r.rate_zone_code,
       r.from_weight_grams,r.to_weight_grams,r.price_minor,r.additional_step_grams,
       r.additional_price_minor,r.source_sheet,r.source_cell
FROM ceserve_domestic_rates r
JOIN organizations o ON o.code='CESERVE'
JOIN countries c ON c.iso2='NG'
JOIN states origin ON origin.country_id=c.id AND origin.code='LA'
JOIN courier_services svc ON svc.organization_id=o.id AND svc.code=r.service_code
JOIN rate_cards rc ON rc.organization_id=o.id AND rc.code='CESERVE_RETAIL'
JOIN rate_card_versions v ON v.rate_card_id=rc.id AND v.version=2 AND v.status='DRAFT'
ON CONFLICT (rate_card_version_id,courier_service_id,origin_state_id,rate_zone_code,from_weight_grams)
DO NOTHING;

INSERT INTO domestic_onforwarding_locations(public_id,organization_id,rate_card_version_id,
    destination_state_id,city_name,normalized_city_name,centre_area,rate_zone_code,
    surcharge_code,surcharge_type,surcharge_amount_minor,source_row_id)
SELECT gen_seed_public_id('dof'),o.id,v.id,s.id,x.city_name,x.normalized_city_name,x.centre_area,
       x.rate_zone_code,x.surcharge_code,x.surcharge_type,x.surcharge_amount_minor,x.source_row_id
FROM ceserve_onforwarding x
JOIN organizations o ON o.code='CESERVE'
JOIN countries c ON c.iso2='NG'
JOIN states s ON s.country_id=c.id AND s.code=x.state_code
JOIN rate_cards rc ON rc.organization_id=o.id AND rc.code='CESERVE_RETAIL'
JOIN rate_card_versions v ON v.rate_card_id=rc.id AND v.version=2 AND v.status='DRAFT'
ON CONFLICT (rate_card_version_id,destination_state_id,normalized_city_name) DO NOTHING;

INSERT INTO surcharge_rules(public_id,organization_id,rate_card_version_id,code,name,surcharge_type,
    calc_type,percentage_bp,applies_to,courier_service_id,conditions,priority,is_taxable)
SELECT gen_seed_public_id('sur'),v.organization_id,v.id,
       CASE WHEN svc.code='DOMESTIC_SAVER' THEN 'INSURANCE_SAVER' ELSE 'INSURANCE_STANDARD' END,
       'Shipment insurance','INSURANCE','PERCENTAGE',100,'DECLARED_VALUE',svc.id,
       '{{"requiresInsurance":true}}'::jsonb,200,false
FROM rate_card_versions v
JOIN rate_cards rc ON rc.id=v.rate_card_id AND rc.code='CESERVE_RETAIL'
JOIN organizations o ON o.id=v.organization_id AND o.code='CESERVE'
JOIN courier_services svc ON svc.organization_id=o.id AND svc.code IN ('DEMO_EXPRESS','DOMESTIC_SAVER')
WHERE v.version=2 AND v.status='DRAFT'
ON CONFLICT (rate_card_version_id,code) DO NOTHING;

UPDATE rate_card_versions v
SET status='SUPERSEDED',effective_to='2026-09-20 15:30:00+00'
FROM rate_cards rc, organizations o
WHERE v.rate_card_id=rc.id AND rc.organization_id=o.id AND o.code='CESERVE'
  AND rc.code='CESERVE_RETAIL' AND v.version<>2 AND v.status='ACTIVE';

UPDATE rate_card_versions v
SET status='ACTIVE',activated_at=COALESCE(activated_at,now())
FROM rate_cards rc, organizations o
WHERE v.rate_card_id=rc.id AND rc.organization_id=o.id AND o.code='CESERVE'
  AND rc.code='CESERVE_RETAIL' AND v.version=2 AND v.status='DRAFT';

DO $$
DECLARE
    postcode_count integer;
    service_area_count integer;
    branch_count integer;
    hub_count integer;
    route_count integer;
    surcharge_count integer;
    domestic_slab_count integer;
    onforwarding_count integer;
BEGIN
    SELECT count(*) INTO postcode_count FROM ceserve_ng_postcodes;
    IF postcode_count < 2706 THEN RAISE EXCEPTION 'Postcode staging set is incomplete: %',postcode_count; END IF;

    SELECT count(*) INTO service_area_count
    FROM service_areas sa JOIN organizations o ON o.id=sa.organization_id
    WHERE o.code='CESERVE' AND sa.status='ACTIVE';
    SELECT count(*) FILTER (WHERE unit_type='COMPANY_BRANCH'),
           count(*) FILTER (WHERE unit_type='REGIONAL_HUB')
      INTO branch_count,hub_count
      FROM operating_units u JOIN organizations o ON o.id=u.organization_id
     WHERE o.code='CESERVE' AND u.status='ACTIVE';
    SELECT count(*) INTO route_count
      FROM route_definitions r JOIN organizations o ON o.id=r.organization_id
     WHERE o.code='CESERVE' AND r.status='ACTIVE';
    SELECT count(*) INTO surcharge_count
      FROM surcharge_rules s
      JOIN rate_card_versions v ON v.id=s.rate_card_version_id AND v.status='ACTIVE'
      JOIN organizations o ON o.id=s.organization_id
     WHERE o.code='CESERVE' AND s.surcharge_type='INSURANCE';

    IF service_area_count < postcode_count THEN RAISE EXCEPTION 'CESERVE service areas incomplete: %/%',service_area_count,postcode_count; END IF;
    IF branch_count < 37 OR hub_count < 6 THEN RAISE EXCEPTION 'CESERVE network incomplete: % branches, % hubs',branch_count,hub_count; END IF;
    SELECT count(*) INTO domestic_slab_count
      FROM domestic_weight_slabs d JOIN organizations o ON o.id=d.organization_id
     WHERE o.code='CESERVE';
    SELECT count(*) INTO onforwarding_count
      FROM domestic_onforwarding_locations d JOIN organizations o ON o.id=d.organization_id
     WHERE o.code='CESERVE';
    IF route_count < 60 THEN RAISE EXCEPTION 'CESERVE fallback route matrix incomplete: %',route_count; END IF;
    IF surcharge_count <> 2 THEN RAISE EXCEPTION 'CESERVE insurance configuration incomplete: %',surcharge_count; END IF;
    IF domestic_slab_count <> 424 THEN RAISE EXCEPTION 'CESERVE domestic slabs incomplete: %',domestic_slab_count; END IF;
    IF onforwarding_count <> 2597 THEN RAISE EXCEPTION 'CESERVE on-forwarding locations incomplete: %',onforwarding_count; END IF;
END $$;

COMMIT;
"""


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--domestic-rates", type=Path, required=True)
    parser.add_argument("--onforwarding", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    candidates = read_candidates(args.input)
    domestic_rates = read_csv(args.domestic_rates)
    onforwarding = read_csv(args.onforwarding)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(build_seed(candidates, domestic_rates, onforwarding), encoding="utf-8")
    print(
        f"wrote {args.output} from {len(candidates)} postcode candidates, "
        f"{len(domestic_rates)} domestic rates and {len(onforwarding)} on-forwarding locations"
    )


if __name__ == "__main__":
    main()
