-- Pricing engine queries (M06).

-- ---------------------------------------------------------------------------
-- Rate cards
-- ---------------------------------------------------------------------------

-- name: CreateRateCard :one
INSERT INTO rate_cards (
    public_id, organization_id, code, name, description, scope,
    customer_id, franchise_id, currency, is_default, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetRateCardByPublicID :one
SELECT * FROM rate_cards WHERE public_id = $1 AND organization_id = $2;

-- name: GetRateCardByID :one
SELECT * FROM rate_cards WHERE id = $1 AND organization_id = $2;

-- name: ListRateCards :many
SELECT rc.*, c.code AS customer_code, c.name AS customer_name,
       f.code AS franchise_code, f.name AS franchise_name,
       (SELECT max(v.version) FROM rate_card_versions v WHERE v.rate_card_id = rc.id) AS latest_version,
       (SELECT v.version FROM rate_card_versions v WHERE v.rate_card_id = rc.id AND v.status = 'ACTIVE') AS active_version,
       count(*) OVER () AS total_count
FROM rate_cards rc
LEFT JOIN customers c ON c.id = rc.customer_id
LEFT JOIN franchises f ON f.id = rc.franchise_id
WHERE rc.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('scope')::text IS NULL OR rc.scope = sqlc.narg('scope'))
  AND (sqlc.narg('status')::text IS NULL OR rc.status = sqlc.narg('status'))
  AND (sqlc.narg('customer_id')::bigint IS NULL OR rc.customer_id = sqlc.narg('customer_id'))
ORDER BY rc.scope, rc.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateRateCard :one
UPDATE rate_cards
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ResolveRateCardForBooking picks the card that applies to a customer.
--
-- Precedence (Constitution §19): a card bound to this customer beats the
-- franchise card of the booking unit, which beats the tenant's default retail
-- card. Ties inside a level break on the lowest id so the choice is stable.
-- name: ResolveRateCardForBooking :one
SELECT rc.*, v.id AS version_id, v.public_id AS version_public_id, v.version AS version_number,
       v.effective_from AS version_effective_from, v.effective_to AS version_effective_to
FROM rate_cards rc
JOIN rate_card_versions v ON v.rate_card_id = rc.id
WHERE rc.organization_id = sqlc.arg('organization_id')
  AND rc.status = 'ACTIVE'
  AND v.status = 'ACTIVE'
  AND v.effective_from <= sqlc.arg('as_of')
  AND (v.effective_to IS NULL OR v.effective_to > sqlc.arg('as_of'))
  AND (
        (rc.scope = 'BUSINESS'  AND rc.customer_id = sqlc.narg('customer_id'))
     OR (rc.scope = 'FRANCHISE' AND rc.franchise_id = sqlc.narg('franchise_id'))
     OR (rc.scope = 'RETAIL'    AND rc.is_default)
  )
ORDER BY (CASE rc.scope WHEN 'BUSINESS' THEN 0 WHEN 'FRANCHISE' THEN 1 ELSE 2 END), rc.id
LIMIT 1;

-- name: GetRateCardVersionByID :one
SELECT v.*, rc.code AS rate_card_code, rc.currency, rc.scope
FROM rate_card_versions v
JOIN rate_cards rc ON rc.id = v.rate_card_id
WHERE v.id = sqlc.arg('id') AND v.organization_id = sqlc.arg('organization_id');

-- ---------------------------------------------------------------------------
-- Rate card versions
-- ---------------------------------------------------------------------------

-- name: CreateRateCardVersion :one
INSERT INTO rate_card_versions (
    public_id, organization_id, rate_card_id, version, effective_from, effective_to, notes, created_by
) VALUES (
    $1, $2, $3,
    COALESCE((SELECT max(v.version) + 1 FROM rate_card_versions v WHERE v.rate_card_id = $3), 1),
    $4, $5, $6, $7
)
RETURNING *;

-- name: GetRateCardVersionByPublicID :one
SELECT v.*, rc.code AS rate_card_code, rc.currency, rc.public_id AS rate_card_public_id
FROM rate_card_versions v
JOIN rate_cards rc ON rc.id = v.rate_card_id
WHERE v.public_id = sqlc.arg('public_id') AND v.organization_id = sqlc.arg('organization_id');

-- name: ListRateCardVersions :many
SELECT v.*, u.full_name AS activated_by_name, count(*) OVER () AS total_count
FROM rate_card_versions v
LEFT JOIN users u ON u.id = v.activated_by
WHERE v.rate_card_id = sqlc.arg('rate_card_id') AND v.organization_id = sqlc.arg('organization_id')
ORDER BY v.version DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- UpdateDraftRateCardVersion only touches DRAFT rows; the database trigger
-- rejects the update anyway, but restricting the WHERE clause gives the service
-- layer a clean "0 rows updated" signal instead of an exception.
-- name: UpdateDraftRateCardVersion :one
UPDATE rate_card_versions
SET effective_from = COALESCE(sqlc.narg('effective_from'), effective_from),
    effective_to = COALESCE(sqlc.narg('effective_to'), effective_to),
    notes = COALESCE(sqlc.narg('notes'), notes)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND status = 'DRAFT'
RETURNING *;

-- SupersedeActiveVersion closes the currently active version of a card.
-- name: SupersedeActiveVersion :execrows
UPDATE rate_card_versions
SET status = 'SUPERSEDED', effective_to = sqlc.arg('effective_to')
WHERE rate_card_id = sqlc.arg('rate_card_id') AND status = 'ACTIVE';

-- name: ActivateRateCardVersion :one
UPDATE rate_card_versions
SET status = 'ACTIVE', activated_at = now(), activated_by = sqlc.narg('activated_by')
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND status = 'DRAFT'
RETURNING *;

-- name: ArchiveRateCardVersion :one
UPDATE rate_card_versions SET status = 'ARCHIVED'
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND status IN ('DRAFT','SUPERSEDED')
RETURNING *;

-- name: CountVersionPricingRows :one
SELECT
    (SELECT count(*) FROM zone_rates z WHERE z.rate_card_version_id = sqlc.arg('version_id')) AS zone_rate_count,
    (SELECT count(*) FROM weight_slabs w WHERE w.rate_card_version_id = sqlc.arg('version_id')) AS slab_count,
    (SELECT count(*) FROM surcharge_rules s WHERE s.rate_card_version_id = sqlc.arg('version_id')) AS surcharge_count,
    (SELECT count(*) FROM discount_rules d WHERE d.rate_card_version_id = sqlc.arg('version_id')) AS discount_count;

-- ---------------------------------------------------------------------------
-- Zone rates and weight slabs
-- ---------------------------------------------------------------------------

-- name: UpsertZoneRate :one
INSERT INTO zone_rates (
    public_id, organization_id, rate_card_version_id, courier_service_id,
    origin_zone_id, destination_zone_id, base_weight_grams, base_price_minor,
    additional_step_grams, additional_price_minor, min_chargeable_weight_grams
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (rate_card_version_id, courier_service_id, origin_zone_id, destination_zone_id)
DO UPDATE SET base_weight_grams = EXCLUDED.base_weight_grams,
              base_price_minor = EXCLUDED.base_price_minor,
              additional_step_grams = EXCLUDED.additional_step_grams,
              additional_price_minor = EXCLUDED.additional_price_minor,
              min_chargeable_weight_grams = EXCLUDED.min_chargeable_weight_grams
RETURNING *;

-- FindZoneRate is the pricing engine's primary lookup: one index probe on
-- zone_rates_unique.
-- name: FindZoneRate :one
SELECT * FROM zone_rates
WHERE rate_card_version_id = sqlc.arg('rate_card_version_id')
  AND courier_service_id = sqlc.arg('courier_service_id')
  AND origin_zone_id = sqlc.arg('origin_zone_id')
  AND destination_zone_id = sqlc.arg('destination_zone_id');

-- name: ListZoneRates :many
SELECT zr.*, s.code AS service_code, oz.code AS origin_zone_code, dz.code AS destination_zone_code,
       count(*) OVER () AS total_count
FROM zone_rates zr
JOIN courier_services s ON s.id = zr.courier_service_id
JOIN zones oz ON oz.id = zr.origin_zone_id
JOIN zones dz ON dz.id = zr.destination_zone_id
WHERE zr.rate_card_version_id = sqlc.arg('rate_card_version_id')
  AND (sqlc.narg('courier_service_id')::bigint IS NULL OR zr.courier_service_id = sqlc.narg('courier_service_id'))
ORDER BY s.code, oz.code, dz.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: DeleteZoneRate :execrows
DELETE FROM zone_rates WHERE public_id = $1 AND organization_id = $2;

-- name: CreateWeightSlab :one
INSERT INTO weight_slabs (
    public_id, organization_id, rate_card_version_id, courier_service_id,
    origin_zone_id, destination_zone_id, from_weight_grams, to_weight_grams,
    price_minor, additional_step_grams, additional_price_minor, sequence
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- FindWeightSlab locates the slab covering a chargeable weight, if the lane
-- uses explicit slabs. The half-open interval and the exclusion constraint
-- together guarantee at most one match.
-- name: FindWeightSlab :one
SELECT * FROM weight_slabs
WHERE rate_card_version_id = sqlc.arg('rate_card_version_id')
  AND courier_service_id = sqlc.arg('courier_service_id')
  AND origin_zone_id = sqlc.arg('origin_zone_id')
  AND destination_zone_id = sqlc.arg('destination_zone_id')
  AND from_weight_grams <= sqlc.arg('chargeable_weight_grams')
  AND (to_weight_grams IS NULL OR to_weight_grams > sqlc.arg('chargeable_weight_grams'))
ORDER BY from_weight_grams DESC
LIMIT 1;

-- name: ListWeightSlabs :many
SELECT ws.*, s.code AS service_code, oz.code AS origin_zone_code, dz.code AS destination_zone_code,
       count(*) OVER () AS total_count
FROM weight_slabs ws
JOIN courier_services s ON s.id = ws.courier_service_id
JOIN zones oz ON oz.id = ws.origin_zone_id
JOIN zones dz ON dz.id = ws.destination_zone_id
WHERE ws.rate_card_version_id = sqlc.arg('rate_card_version_id')
ORDER BY s.code, oz.code, dz.code, ws.from_weight_grams
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: DeleteWeightSlab :execrows
DELETE FROM weight_slabs WHERE public_id = $1 AND organization_id = $2;

-- ---------------------------------------------------------------------------
-- Surcharges, discounts, tax
-- ---------------------------------------------------------------------------

-- name: CreateSurchargeRule :one
INSERT INTO surcharge_rules (
    public_id, organization_id, rate_card_version_id, code, name, surcharge_type,
    calc_type, value_minor, percentage_bp, applies_to, min_amount_minor, max_amount_minor,
    courier_service_id, conditions, priority, is_taxable
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
RETURNING *;

-- name: ListSurchargeRulesForVersion :many
SELECT * FROM surcharge_rules
WHERE rate_card_version_id = sqlc.arg('rate_card_version_id')
  AND (courier_service_id IS NULL OR courier_service_id = sqlc.arg('courier_service_id'))
ORDER BY priority, id;

-- name: ListAllSurchargeRules :many
SELECT sr.*, s.code AS service_code, count(*) OVER () AS total_count
FROM surcharge_rules sr
LEFT JOIN courier_services s ON s.id = sr.courier_service_id
WHERE sr.rate_card_version_id = sqlc.arg('rate_card_version_id')
ORDER BY sr.priority, sr.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: DeleteSurchargeRule :execrows
DELETE FROM surcharge_rules WHERE public_id = $1 AND organization_id = $2;

-- name: CreateDiscountRule :one
INSERT INTO discount_rules (
    public_id, organization_id, rate_card_version_id, code, name, discount_type,
    value_minor, percentage_bp, applies_to, courier_service_id,
    min_subtotal_minor, max_discount_minor, conditions, priority, is_stackable
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;

-- name: ListDiscountRulesForVersion :many
SELECT * FROM discount_rules
WHERE rate_card_version_id = sqlc.arg('rate_card_version_id')
  AND (courier_service_id IS NULL OR courier_service_id = sqlc.arg('courier_service_id'))
ORDER BY priority, id;

-- name: ListAllDiscountRules :many
SELECT dr.*, s.code AS service_code, count(*) OVER () AS total_count
FROM discount_rules dr
LEFT JOIN courier_services s ON s.id = dr.courier_service_id
WHERE dr.rate_card_version_id = sqlc.arg('rate_card_version_id')
ORDER BY dr.priority, dr.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: DeleteDiscountRule :execrows
DELETE FROM discount_rules WHERE public_id = $1 AND organization_id = $2;

-- name: CreateTaxRule :one
INSERT INTO tax_rules (
    public_id, organization_id, code, name, tax_type, percentage_bp,
    intra_state_only, hsn_sac_code, priority, effective_from, effective_to, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- ListApplicableTaxRules returns the tax rules in force for a lane.
-- intra_state_only NULL means the rule applies either way.
-- name: ListApplicableTaxRules :many
SELECT * FROM tax_rules
WHERE organization_id = sqlc.arg('organization_id')
  AND status = 'ACTIVE'
  AND effective_from <= sqlc.arg('as_of')
  AND (effective_to IS NULL OR effective_to > sqlc.arg('as_of'))
  AND (intra_state_only IS NULL OR intra_state_only = sqlc.arg('is_intra_state'))
ORDER BY priority, id;

-- name: ListTaxRules :many
SELECT t.*, count(*) OVER () AS total_count
FROM tax_rules t
WHERE t.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status'))
ORDER BY t.priority, t.code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: SetTaxRuleStatus :one
UPDATE tax_rules
SET status = sqlc.arg('status'), effective_to = COALESCE(sqlc.narg('effective_to'), effective_to)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;
