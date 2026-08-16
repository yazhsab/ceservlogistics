-- Franchise counter payment collection.

-- name: CreateFranchiseCollection :one
INSERT INTO franchise_collections (
    public_id, organization_id, shipment_id, franchise_id, operating_unit_id,
    customer_id, amount_minor, currency, payment_mode, reference,
    collected_at, collected_by, notes, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: GetFranchiseCollectionByPublicID :one
SELECT c.*, s.awb, f.code AS franchise_code, f.name AS franchise_name,
       ou.code AS unit_code, u.full_name AS collected_by_name
FROM franchise_collections c
JOIN shipments s ON s.id = c.shipment_id
JOIN franchises f ON f.id = c.franchise_id
JOIN operating_units ou ON ou.id = c.operating_unit_id
JOIN users u ON u.id = c.collected_by
WHERE c.organization_id = $1 AND c.public_id = $2;

-- name: GetFranchiseCollectionByShipment :one
SELECT * FROM franchise_collections WHERE organization_id = $1 AND shipment_id = $2;

-- name: ListFranchiseCollections :many
SELECT c.*, s.awb, f.code AS franchise_code, f.name AS franchise_name,
       ou.code AS unit_code, u.full_name AS collected_by_name,
       count(*) OVER () AS total_count
FROM franchise_collections c
JOIN shipments s ON s.id = c.shipment_id
JOIN franchises f ON f.id = c.franchise_id
JOIN operating_units ou ON ou.id = c.operating_unit_id
JOIN users u ON u.id = c.collected_by
WHERE c.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR c.franchise_id = sqlc.narg('franchise_id'))
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status'))
  AND (sqlc.narg('scoped_unit_ids')::bigint[] IS NULL OR c.operating_unit_id = ANY(sqlc.narg('scoped_unit_ids')))
ORDER BY c.collected_at DESC, c.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: SweepFranchiseCollectionsForSettlement :many
SELECT c.*, s.awb
FROM franchise_collections c JOIN shipments s ON s.id = c.shipment_id
WHERE c.organization_id = sqlc.arg('organization_id')
  AND c.franchise_id = sqlc.arg('franchise_id')
  AND c.status = 'COLLECTED' AND c.settlement_id IS NULL
  AND c.collected_at >= sqlc.arg('period_start')::date
  AND c.collected_at < (sqlc.arg('period_end')::date + 1)
ORDER BY c.collected_at, c.id
FOR UPDATE OF c;

-- name: AttachFranchiseCollectionsToSettlement :exec
UPDATE franchise_collections
SET settlement_id = sqlc.arg('settlement_id'), status = 'IN_SETTLEMENT'
WHERE organization_id = sqlc.arg('organization_id') AND id = ANY(sqlc.arg('ids')::bigint[])
  AND settlement_id IS NULL AND status = 'COLLECTED';

-- name: ReleaseFranchiseCollectionsFromSettlement :exec
UPDATE franchise_collections SET settlement_id = NULL, status = 'COLLECTED'
WHERE organization_id = $1 AND settlement_id = $2 AND status = 'IN_SETTLEMENT';

-- name: MarkFranchiseCollectionsRemitted :exec
UPDATE franchise_collections SET status = 'REMITTED', remitted_at = now()
WHERE organization_id = $1 AND settlement_id = $2 AND status = 'IN_SETTLEMENT';
