-- 0033 Nigerian postcodes, LGAs, and finding a postcode by name.
--
-- # The problem this solves
--
-- Serviceability, routing, zones and pricing are all keyed on postcode. The
-- format validates for Nigeria — six digits with a non-zero lead, the same
-- shape as an Indian PIN code — so nothing *rejected* a Nigerian booking. The
-- blocker was softer and worse: postcodes are poorly adopted in Nigerian
-- practice. Addresses are given by area, LGA and landmark. A sender who does
-- not know their code had no way to find it, because every lookup the platform
-- offered searched by code prefix and nothing else.
--
-- So the fix is not a format change. It is making the postcode *findable* from
-- what a sender actually knows, and loading the Nigerian data to find.
--
-- # What LGA maps onto
--
-- Nothing structural is added. Nigeria's Local Government Area sits at exactly
-- the level `districts` already occupies between state and city, and the area
-- names people actually say — Ikeja GRA, Wuse II — are what `localities`
-- already is. Both tables existed and were simply never populated or exposed
-- for Nigeria. The country-appropriate *label* is presentation and lives in Go.
--
-- # Provenance of the seeded data
--
-- This is a starter set, not the authoritative NIPOST table, and it is worth
-- being exact about which is which:
--
--   * The 37 states (migration 0031), the 20 Lagos LGAs and the 6 FCT area
--     councils are official administrative divisions and are complete.
--   * The nine postcodes are the NIPOST zone anchors — the state-capital codes
--     that are widely published and stable.
--   * The localities are genuine area names within their LGA.
--
-- It is deliberately small. Reference data that is wrong misroutes parcels, so
-- guessing at thousands of six-digit codes would be worse than seeding none.
-- The full NIPOST dataset loads through the existing importer
-- (POST /api/v1/geography/imports/pincodes) whose CSV now accepts a locality
-- column; see docs/localisation-nigeria.md §4.

-- ---------------------------------------------------------------------------
-- Finding a place by name
--
-- Name search is `ILIKE '%fragment%'`, which no btree index can serve. A GIN
-- trigram index can, and reference data is read-mostly, so the write cost is
-- paid on an import that already runs in the background.
--
-- These are the indexes behind GET /api/v1/geography/places.
-- ---------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX localities_name_trgm_idx ON localities USING gin (name gin_trgm_ops);
CREATE INDEX cities_name_trgm_idx     ON cities     USING gin (name gin_trgm_ops);
CREATE INDEX districts_name_trgm_idx  ON districts  USING gin (name gin_trgm_ops);
CREATE INDEX pincodes_office_trgm_idx ON pincodes   USING gin (office_name gin_trgm_ops)
    WHERE office_name IS NOT NULL;

-- Districts are joined from a postcode on every place-search result and were
-- reachable only by state.
CREATE INDEX pincodes_district_idx ON pincodes (district_id) WHERE district_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Local Government Areas
--
-- Lagos and the FCT in full: together they are the overwhelming majority of a
-- first-phase courier's volume, and they are the two whose LGA lists are
-- unambiguous. For the other seven anchor cities only the LGA containing the
-- capital is seeded, because a partial LGA list for a state reads as complete
-- and is not.
-- ---------------------------------------------------------------------------
INSERT INTO districts (public_id, state_id, code, name, status)
SELECT gen_seed_public_id('dst'), s.id,
       upper(replace(replace(d.name, ' ', '_'), '-', '_')), d.name, 'ACTIVE'
FROM states s
JOIN countries c ON c.id = s.country_id
JOIN (VALUES
    -- Lagos State: all 20.
    ('LA','Agege'),('LA','Ajeromi-Ifelodun'),('LA','Alimosho'),
    ('LA','Amuwo-Odofin'),('LA','Apapa'),('LA','Badagry'),('LA','Epe'),
    ('LA','Eti-Osa'),('LA','Ibeju-Lekki'),('LA','Ifako-Ijaiye'),('LA','Ikeja'),
    ('LA','Ikorodu'),('LA','Kosofe'),('LA','Lagos Island'),
    ('LA','Lagos Mainland'),('LA','Mushin'),('LA','Ojo'),('LA','Oshodi-Isolo'),
    ('LA','Shomolu'),('LA','Surulere'),
    -- Federal Capital Territory: all 6 area councils.
    ('FC','Abaji'),('FC','Abuja Municipal'),('FC','Bwari'),('FC','Gwagwalada'),
    ('FC','Kuje'),('FC','Kwali'),
    -- The LGA holding each remaining anchor city.
    ('OY','Ibadan North'),('ED','Oredo'),('EN','Enugu North'),
    ('RI','Port Harcourt'),('BO','Maiduguri'),('KN','Kano Municipal'),
    ('KD','Kaduna North')
) AS d(state_code, name) ON d.state_code = s.code
WHERE c.iso2 = 'NG'
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Cities
-- ---------------------------------------------------------------------------
INSERT INTO cities (public_id, state_id, name, tier, status)
SELECT gen_seed_public_id('cty'), s.id, v.city, v.tier, 'ACTIVE'
FROM states s
JOIN countries c ON c.id = s.country_id
JOIN (VALUES
    ('LA','Lagos','METRO'),
    ('FC','Abuja','METRO'),
    ('OY','Ibadan','TIER_1'),
    ('ED','Benin City','TIER_1'),
    ('EN','Enugu','TIER_1'),
    ('RI','Port Harcourt','TIER_1'),
    ('KN','Kano','TIER_1'),
    ('KD','Kaduna','TIER_1'),
    ('BO','Maiduguri','TIER_2')
) AS v(state_code, city, tier) ON v.state_code = s.code
WHERE c.iso2 = 'NG'
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- The nine NIPOST zone anchors
--
-- Nigeria's scheme divides the country into nine postal zones, each anchored on
-- a major city, and the leading digit names the zone. These nine codes are the
-- best-attested Nigerian postcodes there are; everything finer belongs in the
-- imported file.
-- ---------------------------------------------------------------------------
INSERT INTO pincodes (public_id, country_id, code, state_id, district_id, city_id, office_name, status)
SELECT gen_seed_public_id('pin'), c.id, v.code, s.id, d.id, ct.id, v.office, 'ACTIVE'
FROM countries c
JOIN (VALUES
    ('100001','LA','Ikeja',          'Lagos',        'Ikeja'),
    ('200001','OY','Ibadan North',   'Ibadan',       'Ibadan'),
    ('300001','ED','Oredo',          'Benin City',   'Benin City'),
    ('400001','EN','Enugu North',    'Enugu',        'Enugu'),
    ('500001','RI','Port Harcourt',  'Port Harcourt','Port Harcourt'),
    ('600001','BO','Maiduguri',      'Maiduguri',    'Maiduguri'),
    ('700001','KN','Kano Municipal', 'Kano',         'Kano'),
    ('800001','KD','Kaduna North',   'Kaduna',       'Kaduna'),
    ('900001','FC','Abuja Municipal','Abuja',        'Garki')
) AS v(code, state_code, district, city, office) ON true
JOIN states s    ON s.country_id = c.id AND s.code = v.state_code
LEFT JOIN districts d ON d.state_id = s.id AND d.name = v.district
LEFT JOIN cities ct   ON ct.state_id = s.id AND ct.name = v.city
WHERE c.iso2 = 'NG'
ON CONFLICT (country_id, code) DO UPDATE
    SET district_id = COALESCE(EXCLUDED.district_id, pincodes.district_id),
        city_id     = COALESCE(EXCLUDED.city_id, pincodes.city_id),
        office_name = COALESCE(EXCLUDED.office_name, pincodes.office_name);

-- ---------------------------------------------------------------------------
-- Area names
--
-- What a Nigerian sender actually types. These are the search terms that have
-- to resolve to a postcode for a booking form to be completable at all.
-- ---------------------------------------------------------------------------
INSERT INTO localities (public_id, pincode_id, name, status)
SELECT gen_seed_public_id('loc'), p.id, v.name, 'ACTIVE'
FROM pincodes p
JOIN countries c ON c.id = p.country_id
JOIN (VALUES
    -- Ikeja, Lagos.
    ('100001','Ikeja GRA'),('100001','Alausa'),('100001','Oregun'),
    ('100001','Opebi'),('100001','Allen Avenue'),('100001','Computer Village'),
    -- Abuja Municipal, FCT.
    ('900001','Garki'),('900001','Wuse'),('900001','Wuse II'),
    ('900001','Maitama'),('900001','Asokoro'),
    ('900001','Central Business District'),
    -- The remaining anchors, named so a search for the city resolves.
    ('200001','Bodija'),('200001','Ibadan North'),
    ('300001','Benin City'),
    ('400001','Independence Layout'),('400001','Enugu'),
    ('500001','Port Harcourt'),('500001','Old GRA'),
    ('600001','Maiduguri'),
    ('700001','Kano'),('700001','Nassarawa'),
    ('800001','Kaduna')
) AS v(code, name) ON v.code = p.code
WHERE c.iso2 = 'NG'
ON CONFLICT DO NOTHING;
