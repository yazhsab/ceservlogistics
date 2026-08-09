-- Bagging (M11).

-- name: CreateBag :one
INSERT INTO bags (
    public_id, organization_id, bag_code, barcode, bag_type, origin_unit_id,
    destination_unit_id, courier_service_id, direction, max_weight_grams, max_shipments,
    opened_by_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING *;

-- name: GetBagByPublicID :one
SELECT b.*,
       o.public_id AS origin_public_id, o.code AS origin_code, o.name AS origin_name,
       d.public_id AS destination_public_id, d.code AS destination_code, d.name AS destination_name,
       sv.code AS service_code, sv.name AS service_name,
       cu.full_name AS closed_by_name, ru.full_name AS received_by_name
FROM bags b
JOIN operating_units o ON o.id = b.origin_unit_id
JOIN operating_units d ON d.id = b.destination_unit_id
LEFT JOIN courier_services sv ON sv.id = b.courier_service_id
LEFT JOIN users cu ON cu.id = b.closed_by_user_id
LEFT JOIN users ru ON ru.id = b.received_by_user_id
WHERE b.public_id = sqlc.arg('public_id') AND b.organization_id = sqlc.arg('organization_id');

-- name: GetBagByBarcode :one
SELECT * FROM bags
WHERE organization_id = sqlc.arg('organization_id')
  AND (barcode = sqlc.arg('barcode') OR bag_code = sqlc.arg('barcode'));

-- name: LockBagForUpdate :one
SELECT * FROM bags
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: LockBagByIDForUpdate :one
SELECT * FROM bags WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListBags :many
SELECT b.id, b.public_id, b.bag_code, b.barcode, b.status, b.bag_type, b.direction,
       b.shipment_count, b.piece_count, b.total_weight_grams, b.gross_weight_grams,
       b.closed_at, b.dispatched_at, b.received_at, b.created_at,
       o.public_id AS origin_public_id, o.code AS origin_code, o.name AS origin_name,
       d.public_id AS destination_public_id, d.code AS destination_code, d.name AS destination_name,
       sv.code AS service_code
FROM bags b
JOIN operating_units o ON o.id = b.origin_unit_id
JOIN operating_units d ON d.id = b.destination_unit_id
LEFT JOIN courier_services sv ON sv.id = b.courier_service_id
WHERE b.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR b.status = sqlc.narg('status'))
  AND (sqlc.narg('origin_unit_id')::bigint IS NULL OR b.origin_unit_id = sqlc.narg('origin_unit_id'))
  AND (sqlc.narg('destination_unit_id')::bigint IS NULL OR b.destination_unit_id = sqlc.narg('destination_unit_id'))
  AND (sqlc.narg('direction')::text IS NULL OR b.direction = sqlc.narg('direction'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL
       OR b.origin_unit_id = ANY(sqlc.narg('unit_ids')) OR b.destination_unit_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (b.created_at, b.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY b.created_at DESC, b.id DESC
LIMIT sqlc.arg('page_size');

-- name: AddBagItem :one
INSERT INTO bag_items (organization_id, bag_id, shipment_id, piece_count, weight_grams, added_by_user_id)
VALUES ($1,$2,$3,$4,$5,$6)
RETURNING *;

-- name: RemoveBagItem :one
UPDATE bag_items
SET status = 'REMOVED', removed_at = now(),
    removed_by_user_id = sqlc.narg('removed_by_user_id'),
    removal_reason = sqlc.arg('removal_reason'),
    exception_id = sqlc.narg('exception_id')
WHERE bag_id = sqlc.arg('bag_id') AND shipment_id = sqlc.arg('shipment_id') AND status = 'IN_BAG'
RETURNING *;

-- name: GetActiveBagItemForShipment :one
-- The "conflicting active bag membership" check from §15. The partial unique
-- index enforces it; this query gives a helpful error instead of a raw
-- constraint violation.
SELECT bi.*, b.public_id AS bag_public_id, b.bag_code, b.status AS bag_status
FROM bag_items bi
JOIN bags b ON b.id = bi.bag_id
WHERE bi.shipment_id = sqlc.arg('shipment_id') AND bi.status = 'IN_BAG';

-- name: ListBagItems :many
SELECT bi.id, bi.status, bi.piece_count, bi.weight_grams, bi.added_at, bi.removed_at,
       bi.removal_reason, bi.verified_at,
       s.public_id AS shipment_public_id, s.awb, s.current_status, s.destination_pincode,
       s.destination_branch_id, s.cod_amount_minor, s.payment_mode,
       db.code AS destination_branch_code,
       u.full_name AS added_by_name
FROM bag_items bi
JOIN shipments s ON s.id = bi.shipment_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
LEFT JOIN users u ON u.id = bi.added_by_user_id
WHERE bi.bag_id = sqlc.arg('bag_id')
  AND (sqlc.narg('only_active')::boolean IS NOT TRUE OR bi.status = 'IN_BAG')
ORDER BY bi.added_at, bi.id;

-- name: ListBagItemShipmentIDs :many
SELECT shipment_id FROM bag_items
WHERE bag_id = sqlc.arg('bag_id') AND status = 'IN_BAG'
ORDER BY shipment_id;

-- name: RecalculateBagCounters :one
UPDATE bags b
SET shipment_count = c.shipments, piece_count = c.pieces, total_weight_grams = c.weight
FROM (
    SELECT count(*) AS shipments,
           COALESCE(sum(piece_count), 0)::int AS pieces,
           COALESCE(sum(weight_grams), 0)::bigint AS weight
    FROM bag_items WHERE bag_id = sqlc.arg('bag_id') AND status = 'IN_BAG'
) c
WHERE b.id = sqlc.arg('bag_id')
RETURNING b.*;

-- name: CloseBag :one
-- Compare-and-swap on OPEN, so two clerks cannot both close the same bag and
-- write two different content snapshots.
UPDATE bags
SET status = 'CLOSED', closed_at = now(),
    closed_by_user_id = sqlc.narg('closed_by_user_id'),
    gross_weight_grams = sqlc.narg('gross_weight_grams'),
    closed_contents = sqlc.arg('closed_contents'),
    event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id') AND status = 'OPEN'
RETURNING *;

-- name: UpdateBagStatus :one
UPDATE bags
SET status = sqlc.arg('to_status'),
    dispatched_at = CASE WHEN sqlc.arg('to_status')::text = 'DISPATCHED' THEN now() ELSE dispatched_at END,
    received_at = CASE WHEN sqlc.arg('to_status')::text = 'RECEIVED' THEN now() ELSE received_at END,
    received_by_user_id = COALESCE(sqlc.narg('received_by_user_id'), received_by_user_id),
    received_unit_id = COALESCE(sqlc.narg('received_unit_id'), received_unit_id),
    opened_at_destination_at = CASE WHEN sqlc.arg('to_status')::text = 'OPENED' THEN now()
                                    ELSE opened_at_destination_at END,
    reconciled_at = CASE WHEN sqlc.arg('to_status')::text = 'RECONCILED' THEN now() ELSE reconciled_at END,
    event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: CreateBagSeal :one
INSERT INTO bag_seals (
    organization_id, bag_id, seal_number, seal_type, applied_by_user_id, applied_unit_id
) VALUES ($1,$2,$3,$4,$5,$6)
RETURNING *;

-- name: ListBagSeals :many
SELECT bs.*, au.full_name AS applied_by_name, vu.full_name AS verified_by_name
FROM bag_seals bs
LEFT JOIN users au ON au.id = bs.applied_by_user_id
LEFT JOIN users vu ON vu.id = bs.verified_by_user_id
WHERE bs.bag_id = sqlc.arg('bag_id')
ORDER BY bs.applied_at DESC;

-- name: VerifyBagSeal :one
UPDATE bag_seals
SET verified_at = now(), verified_by_user_id = sqlc.narg('verified_by_user_id'),
    verification_result = sqlc.arg('verification_result'),
    verification_remarks = sqlc.narg('verification_remarks'),
    broken_at = CASE WHEN sqlc.arg('verification_result')::text = 'BROKEN' THEN now() ELSE broken_at END
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: MarkBagSealBroken :exec
UPDATE bag_seals SET broken_at = now(), broken_by_user_id = sqlc.narg('broken_by_user_id')
WHERE bag_id = sqlc.arg('bag_id') AND broken_at IS NULL;

-- name: AppendBagEvent :one
INSERT INTO bag_events (
    public_id, organization_id, bag_id, sequence, event_type, from_status, to_status,
    occurred_at, actor_user_id, operating_unit_id, description, reason_code, request_id, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('bag_id'),
    sqlc.arg('sequence'), sqlc.arg('event_type'), sqlc.arg('from_status'),
    sqlc.arg('to_status'), COALESCE(sqlc.narg('occurred_at')::timestamptz, now()),
    sqlc.arg('actor_user_id'), sqlc.arg('operating_unit_id'), sqlc.arg('description'),
    sqlc.arg('reason_code'), sqlc.arg('request_id'), sqlc.arg('metadata')
)
RETURNING *;

-- name: ListBagEvents :many
SELECT be.*, u.full_name AS actor_name, ou.code AS unit_code
FROM bag_events be
LEFT JOIN users u ON u.id = be.actor_user_id
LEFT JOIN operating_units ou ON ou.id = be.operating_unit_id
WHERE be.bag_id = sqlc.arg('bag_id')
ORDER BY be.sequence DESC
LIMIT sqlc.arg('page_size');

-- name: BumpBagEventSequence :one
UPDATE bags SET event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING id, event_sequence, status;

-- name: UpdateBagItemStatus :one
UPDATE bag_items
SET status = sqlc.arg('status'),
    verified_at = CASE WHEN sqlc.arg('status')::text <> 'IN_BAG' THEN now() ELSE verified_at END,
    verified_by_user_id = COALESCE(sqlc.narg('verified_by_user_id'), verified_by_user_id),
    exception_id = COALESCE(sqlc.narg('exception_id'), exception_id)
WHERE bag_id = sqlc.arg('bag_id') AND shipment_id = sqlc.arg('shipment_id')
RETURNING *;

-- name: CountOpenBagsAtUnit :one
SELECT count(*) FROM bags
WHERE organization_id = sqlc.arg('organization_id')
  AND origin_unit_id = sqlc.arg('unit_id') AND status = 'OPEN';
