-- Courier network master queries (M02).

-- ---------------------------------------------------------------------------
-- Regions
-- ---------------------------------------------------------------------------

-- name: CreateRegion :one
INSERT INTO regions (public_id, organization_id, code, name, parent_region_id)
VALUES ($1,$2,$3,$4,$5)
RETURNING *;

-- name: GetRegionByPublicID :one
SELECT * FROM regions WHERE public_id = $1 AND organization_id = $2;

-- name: ListRegions :many
SELECT r.*, p.code AS parent_code, p.name AS parent_name, count(*) OVER () AS total_count
FROM regions r
LEFT JOIN regions p ON p.id = r.parent_region_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
ORDER BY r.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateRegion :one
UPDATE regions
SET name = COALESCE(sqlc.narg('name'), name),
    parent_region_id = COALESCE(sqlc.narg('parent_region_id'), parent_region_id),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Operating units
-- ---------------------------------------------------------------------------

-- name: CreateOperatingUnit :one
INSERT INTO operating_units (
    public_id, organization_id, code, name, unit_type, parent_unit_id, region_id,
    address_line1, address_line2, landmark, pincode, pincode_id, city_id, state_id,
    latitude, longitude, contact_name, contact_phone, contact_email,
    operating_hours, effective_from, effective_to, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
RETURNING *;

-- name: GetOperatingUnitByPublicID :one
SELECT ou.*, r.code AS region_code, r.name AS region_name,
       p.public_id AS parent_public_id, p.code AS parent_code, p.name AS parent_name,
       p.unit_type AS parent_unit_type,
       c.name AS city_name, s.name AS state_name
FROM operating_units ou
LEFT JOIN regions r ON r.id = ou.region_id
LEFT JOIN operating_units p ON p.id = ou.parent_unit_id
LEFT JOIN cities c ON c.id = ou.city_id
LEFT JOIN states s ON s.id = ou.state_id
WHERE ou.public_id = sqlc.arg('public_id') AND ou.organization_id = sqlc.arg('organization_id');

-- name: GetOperatingUnitByID :one
SELECT * FROM operating_units WHERE id = $1 AND organization_id = $2;

-- name: GetOperatingUnitByCode :one
SELECT * FROM operating_units WHERE organization_id = $1 AND code = $2;

-- name: ListOperatingUnits :many
SELECT ou.*, r.code AS region_code, p.code AS parent_code, p.name AS parent_name,
       c.name AS city_name, s.name AS state_name,
       count(*) OVER () AS total_count
FROM operating_units ou
LEFT JOIN regions r ON r.id = ou.region_id
LEFT JOIN operating_units p ON p.id = ou.parent_unit_id
LEFT JOIN cities c ON c.id = ou.city_id
LEFT JOIN states s ON s.id = ou.state_id
WHERE ou.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('unit_types')::text[] IS NULL OR ou.unit_type = ANY(sqlc.narg('unit_types')::text[]))
  AND (sqlc.narg('status')::text IS NULL OR ou.status = sqlc.narg('status'))
  AND (sqlc.narg('region_id')::bigint IS NULL OR ou.region_id = sqlc.narg('region_id'))
  AND (sqlc.narg('parent_unit_id')::bigint IS NULL OR ou.parent_unit_id = sqlc.narg('parent_unit_id'))
  AND (sqlc.narg('pincode')::text IS NULL OR ou.pincode = sqlc.narg('pincode'))
  AND (sqlc.narg('search')::text IS NULL
       OR lower(ou.name) LIKE '%' || lower(sqlc.narg('search')) || '%'
       OR ou.code LIKE upper(sqlc.narg('search')) || '%')
  AND (sqlc.narg('scoped_unit_ids')::bigint[] IS NULL OR ou.id = ANY(sqlc.narg('scoped_unit_ids')::bigint[]))
ORDER BY ou.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateOperatingUnit :one
UPDATE operating_units
SET name = COALESCE(sqlc.narg('name'), name),
    parent_unit_id = CASE WHEN sqlc.arg('clear_parent')::boolean THEN NULL
                          ELSE COALESCE(sqlc.narg('parent_unit_id'), parent_unit_id) END,
    region_id = COALESCE(sqlc.narg('region_id'), region_id),
    address_line1 = COALESCE(sqlc.narg('address_line1'), address_line1),
    address_line2 = COALESCE(sqlc.narg('address_line2'), address_line2),
    landmark = COALESCE(sqlc.narg('landmark'), landmark),
    pincode = COALESCE(sqlc.narg('pincode'), pincode),
    pincode_id = COALESCE(sqlc.narg('pincode_id'), pincode_id),
    city_id = COALESCE(sqlc.narg('city_id'), city_id),
    state_id = COALESCE(sqlc.narg('state_id'), state_id),
    latitude = COALESCE(sqlc.narg('latitude'), latitude),
    longitude = COALESCE(sqlc.narg('longitude'), longitude),
    contact_name = COALESCE(sqlc.narg('contact_name'), contact_name),
    contact_phone = COALESCE(sqlc.narg('contact_phone'), contact_phone),
    contact_email = COALESCE(sqlc.narg('contact_email'), contact_email),
    operating_hours = COALESCE(sqlc.narg('operating_hours'), operating_hours),
    effective_to = COALESCE(sqlc.narg('effective_to'), effective_to),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id')
  AND organization_id = sqlc.arg('organization_id')
  AND version = sqlc.arg('expected_version')
RETURNING *;

-- name: DeactivateOperatingUnit :one
UPDATE operating_units
SET status = 'INACTIVE', deactivated_at = now(), deactivated_by = sqlc.narg('deactivated_by')
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND status <> 'INACTIVE'
RETURNING *;

-- CountOperatingUnitReferences reports whether a unit is still referenced by
-- transactional or configuration data. Units with references are deactivated,
-- never deleted (Constitution §M02).
-- name: CountOperatingUnitReferences :one
SELECT
    (SELECT count(*) FROM shipments WHERE origin_branch_id = $1 OR destination_branch_id = $1
        OR origin_hub_id = $1 OR destination_hub_id = $1 OR booking_unit_id = $1) AS shipment_count,
    (SELECT count(*) FROM operating_units WHERE parent_unit_id = $1) AS child_count,
    (SELECT count(*) FROM service_areas WHERE operating_unit_id = $1 AND status = 'ACTIVE') AS service_area_count,
    (SELECT count(*) FROM route_definitions WHERE (origin_unit_id = $1 OR destination_unit_id = $1) AND status = 'ACTIVE') AS route_count,
    (SELECT count(*) FROM franchises WHERE operating_unit_id = $1) AS franchise_count,
    (SELECT count(*) FROM user_roles WHERE operating_unit_id = $1) AS role_assignment_count;

-- GetOperatingUnitHierarchy walks down from a root unit.
--
-- The recursive CTE is depth-limited so a cycle introduced by bad data cannot
-- spin forever; the not-self-parent CHECK prevents the trivial case and this
-- bound covers the rest.
-- name: GetOperatingUnitHierarchy :many
WITH RECURSIVE tree AS (
    SELECT ou.id, ou.public_id, ou.code, ou.name, ou.unit_type, ou.status,
           ou.parent_unit_id, ou.pincode, ou.city_id, 0 AS depth,
           ARRAY[ou.code]::text[] AS path
    FROM operating_units ou
    WHERE ou.organization_id = sqlc.arg('organization_id')
      AND (CASE WHEN sqlc.narg('root_public_id')::text IS NULL
                THEN ou.parent_unit_id IS NULL
                ELSE ou.public_id = sqlc.narg('root_public_id') END)
    UNION ALL
    SELECT c.id, c.public_id, c.code, c.name, c.unit_type, c.status,
           c.parent_unit_id, c.pincode, c.city_id, t.depth + 1,
           t.path || c.code
    FROM operating_units c
    JOIN tree t ON c.parent_unit_id = t.id
    WHERE t.depth < sqlc.arg('max_depth')::int
      AND c.organization_id = sqlc.arg('organization_id')
      AND NOT c.code = ANY(t.path)
)
SELECT * FROM tree ORDER BY path;

-- ResolveAncestorHub walks up from a branch to the nearest hub.
--
-- Routing is defined between hubs, so every branch must resolve to one; the
-- walk stops at the first ancestor whose unit_type is a hub type.
-- name: ResolveAncestorHub :one
WITH RECURSIVE up AS (
    SELECT ou.id, ou.public_id, ou.code, ou.name, ou.unit_type, ou.parent_unit_id, ou.status, 0 AS depth
    FROM operating_units ou
    WHERE ou.id = sqlc.arg('unit_id') AND ou.organization_id = sqlc.arg('organization_id')
    UNION ALL
    SELECT p.id, p.public_id, p.code, p.name, p.unit_type, p.parent_unit_id, p.status, u.depth + 1
    FROM operating_units p
    JOIN up u ON p.id = u.parent_unit_id
    WHERE u.depth < 10
)
SELECT id, public_id, code, name, unit_type, status, depth FROM up
WHERE unit_type IN ('DELIVERY_HUB','TRANSIT_HUB','REGIONAL_HUB')
ORDER BY depth
LIMIT 1;

-- ---------------------------------------------------------------------------
-- Capabilities
-- ---------------------------------------------------------------------------

-- name: SetOperatingUnitCapability :one
INSERT INTO operating_unit_capabilities (organization_id, operating_unit_id, capability, enabled, metadata)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (operating_unit_id, capability) DO UPDATE
    SET enabled = EXCLUDED.enabled, metadata = EXCLUDED.metadata
RETURNING *;

-- name: ListOperatingUnitCapabilities :many
SELECT * FROM operating_unit_capabilities WHERE operating_unit_id = $1 ORDER BY capability;

-- name: HasOperatingUnitCapability :one
SELECT EXISTS (
    SELECT 1 FROM operating_unit_capabilities
    WHERE operating_unit_id = $1 AND capability = $2 AND enabled
) AS has_capability;

-- ---------------------------------------------------------------------------
-- Franchises
-- ---------------------------------------------------------------------------

-- name: CreateFranchise :one
INSERT INTO franchises (
    public_id, organization_id, code, name, operating_unit_id, category,
    owner_name, owner_user_id, owner_phone, owner_email, gst_number, pan_number,
    status, onboarded_at, metadata, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
RETURNING *;

-- name: GetFranchiseByPublicID :one
SELECT f.*, ou.code AS unit_code, ou.name AS unit_name, ou.unit_type, ou.pincode AS unit_pincode,
       u.public_id AS owner_user_public_id, u.email AS owner_user_email
FROM franchises f
JOIN operating_units ou ON ou.id = f.operating_unit_id
LEFT JOIN users u ON u.id = f.owner_user_id
WHERE f.public_id = sqlc.arg('public_id') AND f.organization_id = sqlc.arg('organization_id');

-- GetFranchiseByOperatingUnit resolves the franchise operating a unit.
--
-- Scoped on organization as well as unit. A unit belongs to exactly one tenant
-- so the unit id alone happens to be sufficient today, but §9 requires the
-- tenant to be *enforced* rather than implied: one caller already compensated
-- for the missing scope with an `f.OrganizationID == orgID` check after the
-- fact, and the other five did not. A predicate in the query cannot be
-- forgotten by the next caller.
--
-- name: GetFranchiseByOperatingUnit :one
SELECT * FROM franchises
WHERE operating_unit_id = sqlc.arg('operating_unit_id')
  AND organization_id = sqlc.arg('organization_id');

-- name: ListFranchises :many
SELECT f.*, ou.code AS unit_code, ou.name AS unit_name, ou.pincode AS unit_pincode,
       count(*) OVER () AS total_count
FROM franchises f
JOIN operating_units ou ON ou.id = f.operating_unit_id
WHERE f.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR f.status = sqlc.narg('status'))
  AND (sqlc.narg('category')::text IS NULL OR f.category = sqlc.narg('category'))
  AND (sqlc.narg('search')::text IS NULL
       OR lower(f.name) LIKE '%' || lower(sqlc.narg('search')) || '%'
       OR f.code LIKE upper(sqlc.narg('search')) || '%')
  AND (sqlc.narg('scoped_unit_ids')::bigint[] IS NULL
       OR f.operating_unit_id = ANY(sqlc.narg('scoped_unit_ids')::bigint[]))
ORDER BY f.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateFranchise :one
UPDATE franchises
SET name = COALESCE(sqlc.narg('name'), name),
    category = COALESCE(sqlc.narg('category'), category),
    owner_name = COALESCE(sqlc.narg('owner_name'), owner_name),
    owner_user_id = COALESCE(sqlc.narg('owner_user_id'), owner_user_id),
    owner_phone = COALESCE(sqlc.narg('owner_phone'), owner_phone),
    owner_email = COALESCE(sqlc.narg('owner_email'), owner_email),
    gst_number = COALESCE(sqlc.narg('gst_number'), gst_number),
    pan_number = COALESCE(sqlc.narg('pan_number'), pan_number),
    status = COALESCE(sqlc.narg('status'), status),
    terminated_at = CASE WHEN sqlc.narg('status')::text = 'TERMINATED' THEN now() ELSE terminated_at END,
    metadata = COALESCE(sqlc.narg('metadata'), metadata)
WHERE public_id = sqlc.arg('public_id')
  AND organization_id = sqlc.arg('organization_id')
  AND version = sqlc.arg('expected_version')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Franchise agreements
-- ---------------------------------------------------------------------------

-- name: CreateFranchiseAgreement :one
INSERT INTO franchise_agreements (
    public_id, organization_id, franchise_id, agreement_number, status,
    effective_from, effective_to, currency, security_deposit_minor, credit_limit_minor,
    commission_plan_code, settlement_cycle, terms, signed_at, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;

-- name: GetFranchiseAgreementByPublicID :one
SELECT * FROM franchise_agreements WHERE public_id = $1 AND organization_id = $2;

-- name: ListFranchiseAgreements :many
SELECT a.*, count(*) OVER () AS total_count
FROM franchise_agreements a
WHERE a.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR a.franchise_id = sqlc.narg('franchise_id'))
  AND (sqlc.narg('status')::text IS NULL OR a.status = sqlc.narg('status'))
ORDER BY a.effective_from DESC, a.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateFranchiseAgreementStatus :one
UPDATE franchise_agreements
SET status = sqlc.arg('status'),
    effective_to = COALESCE(sqlc.narg('effective_to'), effective_to),
    signed_at = COALESCE(sqlc.narg('signed_at'), signed_at)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: ListFranchisesForUnits :many
-- The franchises owning any of a principal's operating units.
--
-- Used to put a franchise subject on the session. A portal client otherwise has
-- to infer "which franchise am I" from a unit list, which is backend
-- authorization logic running in a browser.
SELECT f.id, f.public_id, f.code, f.name, f.operating_unit_id,
       u.code AS unit_code
FROM franchises f
JOIN operating_units u ON u.id = f.operating_unit_id
WHERE f.organization_id = $1
  AND f.operating_unit_id = ANY(sqlc.arg('unit_ids')::bigint[])
  AND f.status = 'ACTIVE'
ORDER BY f.code;

-- name: ListCustomersForPortalUser :many
-- The customer accounts a portal login may act for, for the session subject.
SELECT c.id, c.public_id, c.code, c.name
FROM customers c
JOIN customer_users cu ON cu.customer_id = c.id
WHERE cu.organization_id = $1 AND cu.user_id = $2 AND cu.status = 'ACTIVE'
ORDER BY c.name;
