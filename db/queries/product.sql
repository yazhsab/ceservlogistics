-- Courier product/service queries (M05).

-- name: CreateCourierService :one
INSERT INTO courier_services (
    public_id, organization_id, code, name, description, mode,
    min_weight_grams, max_weight_grams, max_length_mm, max_width_mm, max_height_mm,
    max_dimension_sum_mm, volumetric_divisor, weight_rounding_grams,
    cod_allowed, max_cod_amount_minor, insurance_allowed, max_declared_value_minor,
    sla_transit_hours, sla_rules, cutoff_time, effective_from, effective_to, sort_order, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
RETURNING *;

-- name: GetCourierServiceByPublicID :one
SELECT * FROM courier_services WHERE public_id = $1 AND organization_id = $2;

-- name: GetCourierServiceByCode :one
SELECT * FROM courier_services WHERE organization_id = $1 AND code = $2;

-- GetActiveCourierServiceByCode resolves a product for booking or quoting at a
-- point in time, honouring the effective-date window.
-- name: GetActiveCourierServiceByCode :one
SELECT * FROM courier_services
WHERE organization_id = sqlc.arg('organization_id')
  AND code = sqlc.arg('code')
  AND status = 'ACTIVE'
  AND effective_from <= sqlc.arg('as_of')::date
  AND (effective_to IS NULL OR effective_to >= sqlc.arg('as_of')::date);

-- name: ListCourierServices :many
SELECT s.*, count(*) OVER () AS total_count
FROM courier_services s
WHERE s.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status'))
  AND (sqlc.narg('mode')::text IS NULL OR s.mode = sqlc.narg('mode'))
  AND (sqlc.narg('cod_allowed')::boolean IS NULL OR s.cod_allowed = sqlc.narg('cod_allowed'))
  AND (sqlc.narg('search')::text IS NULL
       OR lower(s.name) LIKE '%' || lower(sqlc.narg('search')) || '%'
       OR s.code LIKE upper(sqlc.narg('search')) || '%')
ORDER BY s.sort_order, s.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateCourierService :one
UPDATE courier_services
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    mode = COALESCE(sqlc.narg('mode'), mode),
    min_weight_grams = COALESCE(sqlc.narg('min_weight_grams'), min_weight_grams),
    max_weight_grams = COALESCE(sqlc.narg('max_weight_grams'), max_weight_grams),
    max_length_mm = COALESCE(sqlc.narg('max_length_mm'), max_length_mm),
    max_width_mm = COALESCE(sqlc.narg('max_width_mm'), max_width_mm),
    max_height_mm = COALESCE(sqlc.narg('max_height_mm'), max_height_mm),
    max_dimension_sum_mm = COALESCE(sqlc.narg('max_dimension_sum_mm'), max_dimension_sum_mm),
    volumetric_divisor = COALESCE(sqlc.narg('volumetric_divisor'), volumetric_divisor),
    weight_rounding_grams = COALESCE(sqlc.narg('weight_rounding_grams'), weight_rounding_grams),
    cod_allowed = COALESCE(sqlc.narg('cod_allowed'), cod_allowed),
    max_cod_amount_minor = COALESCE(sqlc.narg('max_cod_amount_minor'), max_cod_amount_minor),
    insurance_allowed = COALESCE(sqlc.narg('insurance_allowed'), insurance_allowed),
    max_declared_value_minor = COALESCE(sqlc.narg('max_declared_value_minor'), max_declared_value_minor),
    sla_transit_hours = COALESCE(sqlc.narg('sla_transit_hours'), sla_transit_hours),
    sla_rules = COALESCE(sqlc.narg('sla_rules'), sla_rules),
    cutoff_time = COALESCE(sqlc.narg('cutoff_time'), cutoff_time),
    effective_to = COALESCE(sqlc.narg('effective_to'), effective_to),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id')
  AND organization_id = sqlc.arg('organization_id')
  AND version = sqlc.arg('expected_version')
RETURNING *;

-- name: DeactivateCourierService :one
UPDATE courier_services SET status = 'INACTIVE'
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND status = 'ACTIVE'
RETURNING *;

-- name: CountCourierServiceReferences :one
SELECT
    (SELECT count(*) FROM shipments sh WHERE sh.courier_service_id = sqlc.arg('service_id')) AS shipment_count,
    (SELECT count(*) FROM zone_rates zr WHERE zr.courier_service_id = sqlc.arg('service_id')) AS rate_count,
    (SELECT count(*) FROM route_definitions rd
      WHERE rd.courier_service_id = sqlc.arg('service_id') AND rd.status = 'ACTIVE') AS route_count;
