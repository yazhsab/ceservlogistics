-- Geography, zones and bulk import queries (M03).

-- ---------------------------------------------------------------------------
-- Countries / states / districts / cities
-- ---------------------------------------------------------------------------

-- name: ListCountries :many
SELECT * FROM countries WHERE status = 'ACTIVE' ORDER BY name;

-- name: GetCountryByISO2 :one
SELECT * FROM countries WHERE iso2 = $1;

-- name: UpsertCountry :one
INSERT INTO countries (public_id, iso2, iso3, name, phone_code, currency)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (iso2) DO UPDATE SET name = EXCLUDED.name, phone_code = EXCLUDED.phone_code
RETURNING *;

-- name: ListStates :many
SELECT s.* FROM states s
JOIN countries c ON c.id = s.country_id
WHERE c.iso2 = sqlc.arg('country_iso2') AND s.status = 'ACTIVE'
ORDER BY s.name;

-- name: GetStateByCode :one
SELECT * FROM states WHERE country_id = $1 AND code = $2;

-- name: GetStateByName :one
SELECT * FROM states WHERE country_id = $1 AND lower(name) = lower($2);

-- name: UpsertState :one
INSERT INTO states (public_id, country_id, code, name, gst_state_code)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (country_id, code) DO UPDATE
    SET name = EXCLUDED.name, gst_state_code = COALESCE(EXCLUDED.gst_state_code, states.gst_state_code)
RETURNING *;

-- name: ListDistricts :many
SELECT * FROM districts WHERE state_id = $1 AND status = 'ACTIVE' ORDER BY name;

-- name: UpsertDistrict :one
INSERT INTO districts (public_id, state_id, code, name)
VALUES ($1,$2,$3,$4)
ON CONFLICT (state_id, code) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: ListCities :many
SELECT c.*, s.name AS state_name, s.code AS state_code
FROM cities c
JOIN states s ON s.id = c.state_id
WHERE c.status = 'ACTIVE'
  AND (sqlc.narg('state_id')::bigint IS NULL OR c.state_id = sqlc.narg('state_id'))
  AND (sqlc.narg('search')::text IS NULL OR lower(c.name) LIKE lower(sqlc.narg('search')) || '%')
ORDER BY c.name
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpsertCity :one
INSERT INTO cities (public_id, state_id, district_id, name, tier, latitude, longitude)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (state_id, name) DO UPDATE
    SET district_id = COALESCE(EXCLUDED.district_id, cities.district_id),
        tier = EXCLUDED.tier
RETURNING *;

-- ---------------------------------------------------------------------------
-- PIN codes
-- ---------------------------------------------------------------------------

-- LookupPincode is the hottest read in the platform: it runs at least twice on
-- every booking and on every serviceability check. It is a single index lookup
-- on pincodes_code_unique plus three primary-key joins.
-- name: LookupPincode :one
SELECT p.id, p.public_id, p.code, p.is_remote, p.status,
       p.latitude, p.longitude, p.office_name,
       p.state_id, s.name AS state_name, s.code AS state_code, s.gst_state_code,
       p.district_id, d.name AS district_name,
       p.city_id, c.name AS city_name, c.tier AS city_tier,
       p.country_id, co.iso2 AS country_iso2
FROM pincodes p
JOIN states s ON s.id = p.state_id
JOIN countries co ON co.id = p.country_id
LEFT JOIN districts d ON d.id = p.district_id
LEFT JOIN cities c ON c.id = p.city_id
WHERE p.code = sqlc.arg('code') AND co.iso2 = sqlc.arg('country_iso2');

-- name: GetPincodeByPublicID :one
SELECT * FROM pincodes WHERE public_id = $1;

-- name: GetPincodeIDByCode :one
SELECT p.id FROM pincodes p
JOIN countries c ON c.id = p.country_id
WHERE p.code = sqlc.arg('code') AND c.iso2 = sqlc.arg('country_iso2');

-- GetPincodeIDByCodeInCountry resolves a code within an already-known country.
--
-- Callers that have resolved the country must use this rather than the ISO
-- variant with a default: India and Nigeria both use six-digit codes with a
-- non-zero lead, so the same digits can name a real place in either, and
-- resolving against the wrong country silently maps a tenant's zone onto
-- somebody else's postcode instead of failing.
--
-- name: GetPincodeIDByCodeInCountry :one
SELECT id FROM pincodes
WHERE code = sqlc.arg('code') AND country_id = sqlc.arg('country_id');

-- name: SearchPincodes :many
SELECT p.id, p.public_id, p.code, p.is_remote, p.status, p.office_name,
       s.name AS state_name, c.name AS city_name, c.tier AS city_tier,
       count(*) OVER () AS total_count
FROM pincodes p
JOIN states s ON s.id = p.state_id
LEFT JOIN cities c ON c.id = p.city_id
WHERE p.status = 'ACTIVE'
  AND (sqlc.narg('code_prefix')::text IS NULL OR p.code LIKE sqlc.narg('code_prefix') || '%')
  AND (sqlc.narg('state_id')::bigint IS NULL OR p.state_id = sqlc.narg('state_id'))
  AND (sqlc.narg('city_id')::bigint IS NULL OR p.city_id = sqlc.narg('city_id'))
  AND (sqlc.narg('is_remote')::boolean IS NULL OR p.is_remote = sqlc.narg('is_remote'))
ORDER BY p.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpsertPincode :one
INSERT INTO pincodes (
    public_id, country_id, code, state_id, district_id, city_id,
    office_name, latitude, longitude, is_remote
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (country_id, code) DO UPDATE
    SET state_id = EXCLUDED.state_id,
        district_id = COALESCE(EXCLUDED.district_id, pincodes.district_id),
        city_id = COALESCE(EXCLUDED.city_id, pincodes.city_id),
        office_name = COALESCE(EXCLUDED.office_name, pincodes.office_name),
        latitude = COALESCE(EXCLUDED.latitude, pincodes.latitude),
        longitude = COALESCE(EXCLUDED.longitude, pincodes.longitude),
        is_remote = EXCLUDED.is_remote,
        status = 'ACTIVE'
RETURNING id, public_id, (xmax = 0) AS inserted;

-- name: SetPincodeStatus :one
UPDATE pincodes SET status = $2 WHERE public_id = $1 RETURNING *;

-- name: ListLocalities :many
SELECT * FROM localities WHERE pincode_id = $1 AND status = 'ACTIVE' ORDER BY name;

-- name: UpsertLocality :one
INSERT INTO localities (public_id, pincode_id, name)
VALUES ($1,$2,$3)
ON CONFLICT (pincode_id, name) DO UPDATE SET status = 'ACTIVE'
RETURNING *;

-- ---------------------------------------------------------------------------
-- Zones
-- ---------------------------------------------------------------------------

-- name: CreateZone :one
INSERT INTO zones (public_id, organization_id, code, name, zone_type, description, sort_order)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: GetZoneByPublicID :one
SELECT * FROM zones WHERE public_id = $1 AND organization_id = $2;

-- name: GetZoneByCode :one
SELECT * FROM zones WHERE organization_id = $1 AND code = $2;

-- name: ListZones :many
SELECT z.*, count(*) OVER () AS total_count
FROM zones z
WHERE z.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR z.status = sqlc.narg('status'))
  AND (sqlc.narg('zone_type')::text IS NULL OR z.zone_type = sqlc.narg('zone_type'))
ORDER BY z.sort_order, z.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateZone :one
UPDATE zones
SET name = COALESCE(sqlc.narg('name'), name),
    zone_type = COALESCE(sqlc.narg('zone_type'), zone_type),
    description = COALESCE(sqlc.narg('description'), description),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: CountZoneReferences :one
SELECT
    (SELECT count(*) FROM pincode_zone_mappings WHERE zone_id = $1 AND status = 'ACTIVE') AS mapping_count,
    (SELECT count(*) FROM zone_rates WHERE origin_zone_id = $1 OR destination_zone_id = $1) AS rate_count;

-- ---------------------------------------------------------------------------
-- PIN code -> zone mappings
-- ---------------------------------------------------------------------------

-- ResolveZoneForPincode returns the tenant's zone for a PIN code together with
-- the effective remote flag (tenant override falling back to the platform
-- default). Serviceability and pricing both depend on this being a single
-- indexed read.
-- name: ResolveZoneForPincode :one
SELECT z.id AS zone_id, z.public_id AS zone_public_id, z.code AS zone_code,
       z.name AS zone_name, z.zone_type,
       COALESCE(m.is_remote_override, p.is_remote) AS is_remote,
       m.public_id AS mapping_public_id
FROM pincode_zone_mappings m
JOIN zones z ON z.id = m.zone_id
JOIN pincodes p ON p.id = m.pincode_id
WHERE m.organization_id = sqlc.arg('organization_id')
  AND m.pincode_id = sqlc.arg('pincode_id')
  AND m.status = 'ACTIVE'
  AND m.effective_from <= sqlc.arg('as_of')
  AND (m.effective_to IS NULL OR m.effective_to > sqlc.arg('as_of'));

-- name: UpsertPincodeZoneMapping :one
INSERT INTO pincode_zone_mappings (
    public_id, organization_id, pincode_id, zone_id, is_remote_override, effective_from, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (organization_id, pincode_id) WHERE status = 'ACTIVE' DO UPDATE
    SET zone_id = EXCLUDED.zone_id,
        is_remote_override = EXCLUDED.is_remote_override,
        effective_from = EXCLUDED.effective_from
RETURNING *;

-- name: ListPincodeZoneMappings :many
SELECT m.public_id, m.is_remote_override, m.effective_from, m.effective_to, m.status,
       p.code AS pincode, p.is_remote AS pincode_is_remote,
       z.code AS zone_code, z.name AS zone_name,
       s.name AS state_name, c.name AS city_name,
       count(*) OVER () AS total_count
FROM pincode_zone_mappings m
JOIN pincodes p ON p.id = m.pincode_id
JOIN zones z ON z.id = m.zone_id
JOIN states s ON s.id = p.state_id
LEFT JOIN cities c ON c.id = p.city_id
WHERE m.organization_id = sqlc.arg('organization_id')
  AND m.status = 'ACTIVE'
  AND (sqlc.narg('zone_id')::bigint IS NULL OR m.zone_id = sqlc.narg('zone_id'))
  AND (sqlc.narg('pincode_prefix')::text IS NULL OR p.code LIKE sqlc.narg('pincode_prefix') || '%')
ORDER BY p.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: DeactivatePincodeZoneMapping :execrows
UPDATE pincode_zone_mappings SET status = 'INACTIVE', effective_to = now()
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id') AND status = 'ACTIVE';

-- ---------------------------------------------------------------------------
-- Bulk import
-- ---------------------------------------------------------------------------

-- name: CreateImportJob :one
INSERT INTO import_jobs (
    public_id, organization_id, import_type, file_name, file_size_bytes, file_checksum, options, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: GetImportJobByPublicID :one
SELECT * FROM import_jobs
WHERE public_id = sqlc.arg('public_id')
  AND (organization_id IS NOT DISTINCT FROM sqlc.narg('organization_id'));

-- name: ListImportJobs :many
SELECT i.*, count(*) OVER () AS total_count
FROM import_jobs i
WHERE (i.organization_id IS NOT DISTINCT FROM sqlc.narg('organization_id'))
  AND (sqlc.narg('import_type')::text IS NULL OR i.import_type = sqlc.narg('import_type'))
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
ORDER BY i.created_at DESC, i.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: SetImportJobTotals :exec
UPDATE import_jobs SET total_rows = $2 WHERE id = $1;

-- name: LinkImportJobToJob :exec
UPDATE import_jobs SET job_id = $2 WHERE id = $1;

-- name: StartImportJob :exec
UPDATE import_jobs SET status = 'PROCESSING', started_at = now() WHERE id = $1;

-- name: RecordImportProgress :exec
UPDATE import_jobs
SET processed_rows = processed_rows + sqlc.arg('processed_delta')::int,
    success_rows = success_rows + sqlc.arg('success_delta')::int,
    failed_rows = failed_rows + sqlc.arg('failed_delta')::int
WHERE id = sqlc.arg('id');

-- name: FinishImportJob :exec
UPDATE import_jobs
SET status = sqlc.arg('status'),
    error_summary = sqlc.arg('error_summary'),
    failure_reason = sqlc.narg('failure_reason'),
    completed_at = now()
WHERE id = sqlc.arg('id');

-- InsertImportRows stages a batch of parsed CSV records using COPY.
--
-- COPY is an order of magnitude cheaper than individual INSERTs for a 19,000
-- row PIN code file, and combined with the streaming CSV reader it keeps peak
-- memory at one batch regardless of file size.
-- name: InsertImportRows :copyfrom
INSERT INTO import_rows (import_job_id, row_number, raw) VALUES ($1, $2, $3);

-- name: ClaimImportRowBatch :many
SELECT id, row_number, raw FROM import_rows
WHERE import_job_id = sqlc.arg('import_job_id')
  AND status = 'PENDING'
  AND id > sqlc.arg('cursor_id')
ORDER BY id
LIMIT sqlc.arg('row_limit');

-- name: MarkImportRowsSucceeded :execrows
UPDATE import_rows SET status = 'SUCCEEDED', processed_at = now()
WHERE id = ANY(sqlc.arg('row_ids')::bigint[]);

-- name: MarkImportRowFailed :exec
UPDATE import_rows
SET status = 'FAILED', error_code = sqlc.arg('error_code'),
    error_message = sqlc.arg('error_message'), processed_at = now()
WHERE id = sqlc.arg('id');

-- name: ListImportRowErrors :many
SELECT id, row_number, raw, error_code, error_message
FROM import_rows
WHERE import_job_id = sqlc.arg('import_job_id') AND status = 'FAILED' AND id > sqlc.arg('cursor_id')
ORDER BY id
LIMIT sqlc.arg('row_limit');

-- name: CountImportRowErrors :one
SELECT count(*) FROM import_rows WHERE import_job_id = $1 AND status = 'FAILED';

-- name: GetImportJobByID :one
SELECT * FROM import_jobs WHERE id = $1;

-- name: FinishImportJobFileMeta :exec
UPDATE import_jobs SET file_size_bytes = $2, file_checksum = $3 WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Finding a postcode from what a sender knows
-- ---------------------------------------------------------------------------

-- SearchPlaces resolves a free-text fragment to postcode candidates.
--
-- This exists because postcode adoption in Nigeria is poor: senders give an
-- area, an LGA and a landmark, not six digits. Every other lookup in this file
-- searches by code, which is exactly backwards for a booking form.
--
-- One branch per source rather than a single OR: an OR across four tables
-- cannot use any of the trigram indexes, and the planner falls back to
-- sequential scans of all four. Each branch here is independently indexable.
--
-- rank_class orders the branches by how strong the evidence is that this is the
-- place the sender meant — an exact code beats a locality name, which beats a
-- city, which beats an LGA covering hundreds of streets. DISTINCT ON collapses
-- a postcode matched by several branches to its strongest one, so "Ikeja"
-- appears once as an area rather than three times as area, office and LGA.
--
-- Two things here are deliberate and were measured, not assumed:
--
--   * The code branch uses ~>=~ and ~<~ rather than LIKE. A LIKE pattern built
--     at runtime cannot be turned into index bounds during planning, so it cost
--     a full scan of pincodes on every keystroke, including for a term like
--     "maitama" that can never prefix-match a numeric code. The byte-ordered
--     range is the same set — every code character is ASCII below chr(127), so
--     [term, term||chr(127)) is exactly "starts with term" — and it uses
--     pincodes_prefix_idx with bounds evaluated at execution time.
--
--   * The limit is applied in `top`, before joining back for display columns.
--     Without it the planner estimates the union at its 200-row default, decides
--     a hash join is cheaper, and scans all of pincodes to build it. Limiting
--     first makes the outer side provably tiny and the join an index lookup.
--
-- `term` is the raw fragment and `pattern` is the same fragment with LIKE
-- wildcards escaped. They are separate parameters because they are consumed by
-- different operators: escaping "10_" for ILIKE gives "10\_", which is correct
-- as a pattern and wrong as a range bound.
--
-- name: SearchPlaces :many
WITH q AS (
    SELECT sqlc.arg('term')::text AS term,
           sqlc.arg('pattern')::text AS pattern,
           (SELECT id FROM countries WHERE iso2 = sqlc.arg('country')::text) AS country_id
), matched AS (
    SELECT p.id, p.code, 0 AS rank_class, p.code AS matched_name, 'CODE' AS matched_kind
    FROM pincodes p, q
    WHERE p.country_id = q.country_id AND p.status = 'ACTIVE'
      AND p.code ~>=~ q.term AND p.code ~<~ (q.term || chr(127))
    UNION ALL
    SELECT p.id, p.code, 1, l.name, 'AREA'
    FROM localities l JOIN pincodes p ON p.id = l.pincode_id, q
    WHERE p.country_id = q.country_id AND p.status = 'ACTIVE'
      AND l.status = 'ACTIVE' AND l.name ILIKE '%' || q.pattern || '%'
    UNION ALL
    SELECT p.id, p.code, 2, c.name, 'CITY'
    FROM pincodes p JOIN cities c ON c.id = p.city_id, q
    WHERE p.country_id = q.country_id AND p.status = 'ACTIVE'
      AND c.status = 'ACTIVE' AND c.name ILIKE '%' || q.pattern || '%'
    UNION ALL
    SELECT p.id, p.code, 3, p.office_name, 'OFFICE'
    FROM pincodes p, q
    WHERE p.country_id = q.country_id AND p.status = 'ACTIVE'
      AND p.office_name IS NOT NULL AND p.office_name ILIKE '%' || q.pattern || '%'
    UNION ALL
    SELECT p.id, p.code, 4, d.name, 'DISTRICT'
    FROM pincodes p JOIN districts d ON d.id = p.district_id, q
    WHERE p.country_id = q.country_id AND p.status = 'ACTIVE'
      AND d.status = 'ACTIVE' AND d.name ILIKE '%' || q.pattern || '%'
), best AS (
    SELECT DISTINCT ON (id) id, code, rank_class, matched_name, matched_kind
    FROM matched
    ORDER BY id, rank_class
), top AS (
    SELECT id, code, rank_class, matched_name, matched_kind
    FROM best
    ORDER BY rank_class, length(matched_name), matched_name, code
    LIMIT sqlc.arg('row_limit')
)
SELECT p.public_id, p.code, p.is_remote,
       t.matched_name::text AS matched_name,
       t.matched_kind::text AS matched_kind,
       s.name AS state_name, s.code AS state_code,
       c.name AS city_name, d.name AS district_name,
       p.office_name
FROM top t
JOIN pincodes p ON p.id = t.id
JOIN states s ON s.id = p.state_id
LEFT JOIN cities c ON c.id = p.city_id
LEFT JOIN districts d ON d.id = p.district_id
ORDER BY t.rank_class, length(t.matched_name), t.matched_name, p.code;

-- ListDistrictsByState lists a state's districts — Local Government Areas in
-- Nigeria — by state code rather than internal id, so a caller holding only
-- what a user picked does not need a second round trip.
--
-- name: ListDistrictsByState :many
SELECT d.public_id, d.code, d.name, s.code AS state_code, s.name AS state_name
FROM districts d
JOIN states s ON s.id = d.state_id
JOIN countries c ON c.id = s.country_id
WHERE c.iso2 = sqlc.arg('country')::text
  AND s.code = sqlc.arg('state_code')::text
  AND d.status = 'ACTIVE'
ORDER BY d.name;
