-- Serviceability and routing queries (M04).
--
-- Every resolution query below carries a total ORDER BY ending in `id`, so the
-- winner is a deterministic function of the stored configuration and never
-- depends on physical row order or plan choice.

-- ---------------------------------------------------------------------------
-- Service areas
-- ---------------------------------------------------------------------------

-- name: CreateServiceArea :one
INSERT INTO service_areas (
    public_id, organization_id, operating_unit_id, pincode_id, area_type,
    priority, is_remote, cutoff_time, effective_from, effective_to, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- ResolveServingUnits returns candidate facilities for a PIN code, best first.
--
-- Precedence: higher priority wins; ties break on a more specific area_type
-- (an explicit PICKUP entry beats a catch-all BOTH), then on the lowest id so
-- the result is stable across restarts and replicas.
-- name: ResolveServingUnits :many
SELECT sa.id, sa.public_id, sa.priority, sa.is_remote, sa.area_type, sa.cutoff_time,
       ou.id AS unit_id, ou.public_id AS unit_public_id, ou.code AS unit_code,
       ou.name AS unit_name, ou.unit_type, ou.parent_unit_id, ou.status AS unit_status
FROM service_areas sa
JOIN operating_units ou ON ou.id = sa.operating_unit_id
WHERE sa.organization_id = sqlc.arg('organization_id')
  AND sa.pincode_id = sqlc.arg('pincode_id')
  AND sa.status = 'ACTIVE'
  AND ou.status = 'ACTIVE'
  AND sa.area_type IN (sqlc.arg('area_type'), 'BOTH')
  AND sa.effective_from <= sqlc.arg('as_of')
  AND (sa.effective_to IS NULL OR sa.effective_to > sqlc.arg('as_of'))
ORDER BY sa.priority DESC,
         (CASE WHEN sa.area_type = sqlc.arg('area_type') THEN 0 ELSE 1 END),
         sa.id
LIMIT sqlc.arg('row_limit');

-- name: ListServiceAreas :many
SELECT sa.*, ou.code AS unit_code, ou.name AS unit_name, ou.unit_type,
       p.code AS pincode, s.name AS state_name, c.name AS city_name,
       count(*) OVER () AS total_count
FROM service_areas sa
JOIN operating_units ou ON ou.id = sa.operating_unit_id
JOIN pincodes p ON p.id = sa.pincode_id
JOIN states s ON s.id = p.state_id
LEFT JOIN cities c ON c.id = p.city_id
WHERE sa.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL OR sa.operating_unit_id = sqlc.narg('operating_unit_id'))
  AND (sqlc.narg('pincode')::text IS NULL OR p.code = sqlc.narg('pincode'))
  AND (sqlc.narg('area_type')::text IS NULL OR sa.area_type = sqlc.narg('area_type'))
  AND (sqlc.narg('status')::text IS NULL OR sa.status = sqlc.narg('status'))
ORDER BY p.code, sa.priority DESC, sa.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateServiceArea :one
UPDATE service_areas
SET priority = COALESCE(sqlc.narg('priority'), priority),
    is_remote = COALESCE(sqlc.narg('is_remote'), is_remote),
    cutoff_time = COALESCE(sqlc.narg('cutoff_time'), cutoff_time),
    effective_to = COALESCE(sqlc.narg('effective_to'), effective_to),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Serviceability rules
-- ---------------------------------------------------------------------------

-- name: CreateServiceabilityRule :one
INSERT INTO serviceability_rules (
    public_id, organization_id, name, rule_type, courier_service_id,
    origin_zone_id, destination_zone_id, origin_pincode_id, destination_pincode_id,
    origin_state_id, destination_state_id, priority, restrictions, reason_code, message,
    effective_from, effective_to, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
RETURNING *;

-- MatchServiceabilityRules returns every rule matching a lane, most specific
-- first. A NULL scope column matches anything; the caller applies DENY before
-- ALLOW at equal priority.
-- name: MatchServiceabilityRules :many
SELECT r.* FROM serviceability_rules r
WHERE r.organization_id = sqlc.arg('organization_id')
  AND r.status = 'ACTIVE'
  AND r.effective_from <= sqlc.arg('as_of')
  AND (r.effective_to IS NULL OR r.effective_to > sqlc.arg('as_of'))
  AND (r.courier_service_id IS NULL OR r.courier_service_id = sqlc.arg('courier_service_id'))
  AND (r.origin_zone_id IS NULL OR r.origin_zone_id = sqlc.narg('origin_zone_id'))
  AND (r.destination_zone_id IS NULL OR r.destination_zone_id = sqlc.narg('destination_zone_id'))
  AND (r.origin_pincode_id IS NULL OR r.origin_pincode_id = sqlc.arg('origin_pincode_id'))
  AND (r.destination_pincode_id IS NULL OR r.destination_pincode_id = sqlc.arg('destination_pincode_id'))
  AND (r.origin_state_id IS NULL OR r.origin_state_id = sqlc.arg('origin_state_id'))
  AND (r.destination_state_id IS NULL OR r.destination_state_id = sqlc.arg('destination_state_id'))
ORDER BY (CASE WHEN r.rule_type = 'DENY' THEN 0 ELSE 1 END), r.priority DESC, r.id
LIMIT sqlc.arg('row_limit');

-- name: ListServiceabilityRules :many
SELECT r.*, s.code AS service_code, oz.code AS origin_zone_code, dz.code AS destination_zone_code,
       op.code AS origin_pincode, dp.code AS destination_pincode,
       count(*) OVER () AS total_count
FROM serviceability_rules r
LEFT JOIN courier_services s ON s.id = r.courier_service_id
LEFT JOIN zones oz ON oz.id = r.origin_zone_id
LEFT JOIN zones dz ON dz.id = r.destination_zone_id
LEFT JOIN pincodes op ON op.id = r.origin_pincode_id
LEFT JOIN pincodes dp ON dp.id = r.destination_pincode_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('rule_type')::text IS NULL OR r.rule_type = sqlc.narg('rule_type'))
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
ORDER BY r.priority DESC, r.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: SetServiceabilityRuleStatus :one
UPDATE serviceability_rules
SET status = sqlc.arg('status'), effective_to = COALESCE(sqlc.narg('effective_to'), effective_to)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Routes
-- ---------------------------------------------------------------------------

-- name: CreateRouteDefinition :one
INSERT INTO route_definitions (
    public_id, organization_id, code, name, origin_unit_id, destination_unit_id,
    courier_service_id, priority, transit_hours, cutoff_time, is_fallback,
    effective_from, effective_to, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: CreateRouteLeg :one
INSERT INTO route_legs (
    public_id, organization_id, route_definition_id, sequence,
    from_unit_id, to_unit_id, mode, transit_hours, partner_name
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: DeleteRouteLegs :execrows
DELETE FROM route_legs WHERE route_definition_id = $1;

-- MatchRoutes returns candidate routes between two facilities, best first.
--
-- Precedence encoded in the ORDER BY:
--   1. a route bound to the requested product beats a generic route;
--   2. a non-fallback route beats a fallback;
--   3. higher priority wins;
--   4. the later effective_from wins;
--   5. the lower id breaks any remaining tie.
-- name: MatchRoutes :many
SELECT r.*, s.code AS service_code
FROM route_definitions r
LEFT JOIN courier_services s ON s.id = r.courier_service_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND r.origin_unit_id = sqlc.arg('origin_unit_id')
  AND r.destination_unit_id = sqlc.arg('destination_unit_id')
  AND r.status = 'ACTIVE'
  AND r.effective_from <= sqlc.arg('as_of')
  AND (r.effective_to IS NULL OR r.effective_to > sqlc.arg('as_of'))
  AND (r.courier_service_id IS NULL OR r.courier_service_id = sqlc.arg('courier_service_id'))
ORDER BY (CASE WHEN r.courier_service_id IS NOT NULL THEN 0 ELSE 1 END),
         (CASE WHEN r.is_fallback THEN 1 ELSE 0 END),
         r.priority DESC,
         r.effective_from DESC,
         r.id
LIMIT sqlc.arg('row_limit');

-- name: GetRouteDefinitionByPublicID :one
SELECT r.*, o.code AS origin_code, o.name AS origin_name,
       d.code AS destination_code, d.name AS destination_name, s.code AS service_code
FROM route_definitions r
JOIN operating_units o ON o.id = r.origin_unit_id
JOIN operating_units d ON d.id = r.destination_unit_id
LEFT JOIN courier_services s ON s.id = r.courier_service_id
WHERE r.public_id = sqlc.arg('public_id') AND r.organization_id = sqlc.arg('organization_id');

-- name: GetRouteDefinitionByID :one
SELECT * FROM route_definitions WHERE id = $1 AND organization_id = $2;

-- name: ListRouteLegs :many
SELECT rl.*, f.code AS from_code, f.name AS from_name, t.code AS to_code, t.name AS to_name
FROM route_legs rl
JOIN operating_units f ON f.id = rl.from_unit_id
JOIN operating_units t ON t.id = rl.to_unit_id
WHERE rl.route_definition_id = $1
ORDER BY rl.sequence;

-- name: ListRouteLegsForRoutes :many
SELECT rl.route_definition_id, rl.sequence, rl.mode, rl.transit_hours, rl.partner_name,
       f.code AS from_code, f.name AS from_name, t.code AS to_code, t.name AS to_name
FROM route_legs rl
JOIN operating_units f ON f.id = rl.from_unit_id
JOIN operating_units t ON t.id = rl.to_unit_id
WHERE rl.route_definition_id = ANY(sqlc.arg('route_ids')::bigint[])
ORDER BY rl.route_definition_id, rl.sequence;

-- name: ListRouteDefinitions :many
SELECT r.*, o.code AS origin_code, d.code AS destination_code, s.code AS service_code,
       count(*) OVER () AS total_count
FROM route_definitions r
JOIN operating_units o ON o.id = r.origin_unit_id
JOIN operating_units d ON d.id = r.destination_unit_id
LEFT JOIN courier_services s ON s.id = r.courier_service_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('origin_unit_id')::bigint IS NULL OR r.origin_unit_id = sqlc.narg('origin_unit_id'))
  AND (sqlc.narg('destination_unit_id')::bigint IS NULL OR r.destination_unit_id = sqlc.narg('destination_unit_id'))
  AND (sqlc.narg('courier_service_id')::bigint IS NULL OR r.courier_service_id = sqlc.narg('courier_service_id'))
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
ORDER BY o.code, d.code, r.priority DESC, r.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateRouteDefinition :one
UPDATE route_definitions
SET name = COALESCE(sqlc.narg('name'), name),
    priority = COALESCE(sqlc.narg('priority'), priority),
    transit_hours = COALESCE(sqlc.narg('transit_hours'), transit_hours),
    cutoff_time = COALESCE(sqlc.narg('cutoff_time'), cutoff_time),
    is_fallback = COALESCE(sqlc.narg('is_fallback'), is_fallback),
    effective_to = COALESCE(sqlc.narg('effective_to'), effective_to),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Routing overrides
-- ---------------------------------------------------------------------------

-- name: CreateRoutingOverride :one
INSERT INTO routing_overrides (
    public_id, organization_id, origin_pincode_id, destination_pincode_id,
    courier_service_id, route_definition_id, priority, reason,
    effective_from, effective_to, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- MatchRoutingOverride returns the winning manual override for a lane, if any.
-- Overrides sit above every other routing source.
-- name: MatchRoutingOverride :one
SELECT o.*, r.code AS route_code, r.transit_hours AS route_transit_hours,
       r.origin_unit_id, r.destination_unit_id, r.status AS route_status
FROM routing_overrides o
JOIN route_definitions r ON r.id = o.route_definition_id
WHERE o.organization_id = sqlc.arg('organization_id')
  AND o.origin_pincode_id = sqlc.arg('origin_pincode_id')
  AND o.destination_pincode_id = sqlc.arg('destination_pincode_id')
  AND o.status = 'ACTIVE'
  AND r.status = 'ACTIVE'
  AND o.effective_from <= sqlc.arg('as_of')
  AND (o.effective_to IS NULL OR o.effective_to > sqlc.arg('as_of'))
  AND (o.courier_service_id IS NULL OR o.courier_service_id = sqlc.arg('courier_service_id'))
ORDER BY (CASE WHEN o.courier_service_id IS NOT NULL THEN 0 ELSE 1 END),
         o.priority DESC, o.effective_from DESC, o.id
LIMIT 1;

-- name: ListRoutingOverrides :many
SELECT o.*, op.code AS origin_pincode, dp.code AS destination_pincode,
       r.code AS route_code, s.code AS service_code, u.full_name AS created_by_name,
       count(*) OVER () AS total_count
FROM routing_overrides o
JOIN pincodes op ON op.id = o.origin_pincode_id
JOIN pincodes dp ON dp.id = o.destination_pincode_id
JOIN route_definitions r ON r.id = o.route_definition_id
LEFT JOIN courier_services s ON s.id = o.courier_service_id
LEFT JOIN users u ON u.id = o.created_by
WHERE o.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR o.status = sqlc.narg('status'))
  AND (sqlc.narg('origin_pincode')::text IS NULL OR op.code = sqlc.narg('origin_pincode'))
  AND (sqlc.narg('destination_pincode')::text IS NULL OR dp.code = sqlc.narg('destination_pincode'))
ORDER BY o.priority DESC, o.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: SetRoutingOverrideStatus :one
UPDATE routing_overrides
SET status = sqlc.arg('status'), effective_to = COALESCE(sqlc.narg('effective_to'), effective_to)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Temporary closures
-- ---------------------------------------------------------------------------

-- name: CreateTemporaryClosure :one
INSERT INTO temporary_closures (
    public_id, organization_id, operating_unit_id, pincode_id, closure_type,
    reason_code, reason, starts_at, ends_at, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- FindActiveClosures returns closures affecting either endpoint of a lane at
-- the booking timestamp.
-- name: FindActiveClosures :many
SELECT c.*, ou.code AS unit_code, p.code AS pincode
FROM temporary_closures c
LEFT JOIN operating_units ou ON ou.id = c.operating_unit_id
LEFT JOIN pincodes p ON p.id = c.pincode_id
WHERE c.organization_id = sqlc.arg('organization_id')
  AND c.status = 'ACTIVE'
  AND c.starts_at <= sqlc.arg('as_of')
  AND c.ends_at > sqlc.arg('as_of')
  AND (c.operating_unit_id = ANY(sqlc.arg('unit_ids')::bigint[])
       OR c.pincode_id = ANY(sqlc.arg('pincode_ids')::bigint[]))
ORDER BY c.id;

-- name: ListTemporaryClosures :many
SELECT c.*, ou.code AS unit_code, ou.name AS unit_name, p.code AS pincode,
       count(*) OVER () AS total_count
FROM temporary_closures c
LEFT JOIN operating_units ou ON ou.id = c.operating_unit_id
LEFT JOIN pincodes p ON p.id = c.pincode_id
WHERE c.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status'))
  AND (sqlc.narg('active_only')::boolean IS NOT TRUE
       OR (c.starts_at <= now() AND c.ends_at > now()))
ORDER BY c.starts_at DESC, c.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CancelTemporaryClosure :one
UPDATE temporary_closures SET status = 'CANCELLED'
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND status = 'ACTIVE'
RETURNING *;
