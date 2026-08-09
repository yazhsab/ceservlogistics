-- Manifest (M12).

-- name: CreateManifest :one
INSERT INTO manifests (
    public_id, organization_id, manifest_code, origin_unit_id, destination_unit_id,
    trip_id, trip_leg_id, direction, remarks, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: GetManifestByPublicID :one
SELECT m.*,
       o.public_id AS origin_public_id, o.code AS origin_code, o.name AS origin_name,
       d.public_id AS destination_public_id, d.code AS destination_code, d.name AS destination_name,
       t.public_id AS trip_public_id, t.trip_code, t.status AS trip_status, t.mode AS trip_mode,
       cu.full_name AS closed_by_name, ru.full_name AS received_by_name
FROM manifests m
JOIN operating_units o ON o.id = m.origin_unit_id
JOIN operating_units d ON d.id = m.destination_unit_id
LEFT JOIN trips t ON t.id = m.trip_id
LEFT JOIN users cu ON cu.id = m.closed_by_user_id
LEFT JOIN users ru ON ru.id = m.received_by_user_id
WHERE m.public_id = sqlc.arg('public_id') AND m.organization_id = sqlc.arg('organization_id');

-- name: GetManifestByCode :one
SELECT * FROM manifests
WHERE organization_id = sqlc.arg('organization_id') AND manifest_code = sqlc.arg('manifest_code');

-- name: LockManifestForUpdate :one
SELECT * FROM manifests
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: LockManifestByIDForUpdate :one
SELECT * FROM manifests WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListManifests :many
SELECT m.id, m.public_id, m.manifest_code, m.status, m.direction,
       m.bag_count, m.loose_shipment_count, m.total_shipment_count, m.total_piece_count,
       m.total_weight_grams, m.closed_at, m.dispatched_at, m.received_at, m.created_at,
       m.document_object_key,
       o.public_id AS origin_public_id, o.code AS origin_code, o.name AS origin_name,
       d.public_id AS destination_public_id, d.code AS destination_code, d.name AS destination_name,
       t.public_id AS trip_public_id, t.trip_code
FROM manifests m
JOIN operating_units o ON o.id = m.origin_unit_id
JOIN operating_units d ON d.id = m.destination_unit_id
LEFT JOIN trips t ON t.id = m.trip_id
WHERE m.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR m.status = sqlc.narg('status'))
  AND (sqlc.narg('origin_unit_id')::bigint IS NULL OR m.origin_unit_id = sqlc.narg('origin_unit_id'))
  AND (sqlc.narg('destination_unit_id')::bigint IS NULL OR m.destination_unit_id = sqlc.narg('destination_unit_id'))
  AND (sqlc.narg('trip_id')::bigint IS NULL OR m.trip_id = sqlc.narg('trip_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL
       OR m.origin_unit_id = ANY(sqlc.narg('unit_ids')) OR m.destination_unit_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (m.created_at, m.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY m.created_at DESC, m.id DESC
LIMIT sqlc.arg('page_size');

-- name: AddManifestBag :one
INSERT INTO manifest_bags (
    organization_id, manifest_id, bag_id, declared_shipment_count, declared_piece_count,
    declared_weight_grams, added_by_user_id
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: AddManifestLooseShipment :one
INSERT INTO manifest_loose_shipments (
    organization_id, manifest_id, shipment_id, piece_count, weight_grams, added_by_user_id
) VALUES ($1,$2,$3,$4,$5,$6)
RETURNING *;

-- name: RemoveManifestBag :one
UPDATE manifest_bags
SET status = 'REMOVED', removed_at = now(), removal_reason = sqlc.arg('removal_reason')
WHERE manifest_id = sqlc.arg('manifest_id') AND bag_id = sqlc.arg('bag_id') AND status = 'LOADED'
RETURNING *;

-- name: RemoveManifestLooseShipment :one
UPDATE manifest_loose_shipments
SET status = 'REMOVED', removed_at = now(), removal_reason = sqlc.arg('removal_reason')
WHERE manifest_id = sqlc.arg('manifest_id') AND shipment_id = sqlc.arg('shipment_id') AND status = 'LOADED'
RETURNING *;

-- name: ListManifestBags :many
SELECT mb.id, mb.status, mb.declared_shipment_count, mb.declared_piece_count,
       mb.declared_weight_grams, mb.added_at, mb.received_at,
       b.id AS bag_id, b.public_id AS bag_public_id, b.bag_code, b.barcode,
       b.status AS bag_status,
       b.shipment_count, b.piece_count, b.total_weight_grams, b.gross_weight_grams,
       d.code AS bag_destination_code
FROM manifest_bags mb
JOIN bags b ON b.id = mb.bag_id
JOIN operating_units d ON d.id = b.destination_unit_id
WHERE mb.manifest_id = sqlc.arg('manifest_id')
  AND (sqlc.narg('only_active')::boolean IS NOT TRUE OR mb.status <> 'REMOVED')
ORDER BY mb.added_at, mb.id;

-- name: ListManifestLooseShipments :many
SELECT ml.id, ml.status, ml.piece_count, ml.weight_grams, ml.added_at, ml.received_at,
       s.public_id AS shipment_public_id, s.awb, s.current_status, s.destination_pincode,
       db.code AS destination_branch_code
FROM manifest_loose_shipments ml
JOIN shipments s ON s.id = ml.shipment_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
WHERE ml.manifest_id = sqlc.arg('manifest_id')
  AND (sqlc.narg('only_active')::boolean IS NOT TRUE OR ml.status <> 'REMOVED')
ORDER BY ml.added_at, ml.id;

-- name: ListManifestShipmentIDs :many
-- Every shipment travelling on this manifest, whether bagged or loose. Used to
-- drive the shipment state transitions at dispatch and receipt.
SELECT s.id, s.public_id, s.awb, s.current_status, s.event_sequence,
       s.movement_direction, s.destination_branch_id, s.destination_hub_id,
       b.id AS bag_id
FROM manifest_bags mb
JOIN bag_items bi ON bi.bag_id = mb.bag_id AND bi.status = 'IN_BAG'
JOIN shipments s ON s.id = bi.shipment_id
JOIN bags b ON b.id = mb.bag_id
WHERE mb.manifest_id = sqlc.arg('manifest_id') AND mb.status <> 'REMOVED'
UNION ALL
SELECT s.id, s.public_id, s.awb, s.current_status, s.event_sequence,
       s.movement_direction, s.destination_branch_id, s.destination_hub_id,
       NULL::bigint AS bag_id
FROM manifest_loose_shipments ml
JOIN shipments s ON s.id = ml.shipment_id
WHERE ml.manifest_id = sqlc.arg('manifest_id') AND ml.status <> 'REMOVED'
ORDER BY 1;

-- name: RecalculateManifestCounters :one
UPDATE manifests m
SET bag_count = c.bags,
    loose_shipment_count = c.loose,
    total_shipment_count = c.bagged_shipments + c.loose,
    total_piece_count = c.bagged_pieces + c.loose_pieces,
    total_weight_grams = c.bagged_weight + c.loose_weight
FROM (
    SELECT
        (SELECT count(*) FROM manifest_bags WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS bags,
        (SELECT COALESCE(sum(declared_shipment_count), 0)::int FROM manifest_bags
          WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS bagged_shipments,
        (SELECT COALESCE(sum(declared_piece_count), 0)::int FROM manifest_bags
          WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS bagged_pieces,
        (SELECT COALESCE(sum(declared_weight_grams), 0)::bigint FROM manifest_bags
          WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS bagged_weight,
        (SELECT count(*) FROM manifest_loose_shipments
          WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS loose,
        (SELECT COALESCE(sum(piece_count), 0)::int FROM manifest_loose_shipments
          WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS loose_pieces,
        (SELECT COALESCE(sum(weight_grams), 0)::bigint FROM manifest_loose_shipments
          WHERE manifest_id = sqlc.arg('manifest_id') AND status <> 'REMOVED') AS loose_weight
) c
WHERE m.id = sqlc.arg('manifest_id')
RETURNING m.*;

-- name: CloseManifest :one
UPDATE manifests
SET status = 'CLOSED', closed_at = now(),
    closed_by_user_id = sqlc.narg('closed_by_user_id'),
    closed_contents = sqlc.arg('closed_contents'),
    declared_weight_grams = sqlc.narg('declared_weight_grams'),
    event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id') AND status = 'DRAFT'
RETURNING *;

-- name: UpdateManifestStatus :one
UPDATE manifests
SET status = sqlc.arg('to_status'),
    trip_id = COALESCE(sqlc.narg('trip_id'), trip_id),
    trip_leg_id = COALESCE(sqlc.narg('trip_leg_id'), trip_leg_id),
    dispatched_at = CASE WHEN sqlc.arg('to_status')::text = 'DISPATCHED' THEN now() ELSE dispatched_at END,
    dispatched_by_user_id = COALESCE(sqlc.narg('dispatched_by_user_id'), dispatched_by_user_id),
    received_at = CASE WHEN sqlc.arg('to_status')::text = 'RECEIVED' THEN now() ELSE received_at END,
    received_by_user_id = COALESCE(sqlc.narg('received_by_user_id'), received_by_user_id),
    reconciled_at = CASE WHEN sqlc.arg('to_status')::text = 'RECONCILED' THEN now() ELSE reconciled_at END,
    event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: AttachManifestToTrip :one
UPDATE manifests SET trip_id = sqlc.arg('trip_id'), trip_leg_id = sqlc.narg('trip_leg_id')
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status IN ('DRAFT','CLOSED')
RETURNING *;

-- name: SetManifestDocument :one
UPDATE manifests
SET document_object_key = sqlc.arg('document_object_key'), document_generated_at = now()
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: MarkManifestBagReceived :one
UPDATE manifest_bags
SET status = sqlc.arg('status'), received_at = now(),
    received_by_user_id = sqlc.narg('received_by_user_id')
WHERE manifest_id = sqlc.arg('manifest_id') AND bag_id = sqlc.arg('bag_id')
RETURNING *;

-- name: MarkManifestLooseReceived :one
UPDATE manifest_loose_shipments
SET status = sqlc.arg('status'), received_at = now(),
    received_by_user_id = sqlc.narg('received_by_user_id')
WHERE manifest_id = sqlc.arg('manifest_id') AND shipment_id = sqlc.arg('shipment_id')
RETURNING *;

-- name: AppendManifestEvent :one
INSERT INTO manifest_events (
    public_id, organization_id, manifest_id, sequence, event_type, from_status, to_status,
    occurred_at, actor_user_id, operating_unit_id, description, reason_code, request_id, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('manifest_id'),
    sqlc.arg('sequence'), sqlc.arg('event_type'), sqlc.arg('from_status'),
    sqlc.arg('to_status'), COALESCE(sqlc.narg('occurred_at')::timestamptz, now()),
    sqlc.arg('actor_user_id'), sqlc.arg('operating_unit_id'), sqlc.arg('description'),
    sqlc.arg('reason_code'), sqlc.arg('request_id'), sqlc.arg('metadata')
)
RETURNING *;

-- name: ListManifestEvents :many
SELECT me.*, u.full_name AS actor_name, ou.code AS unit_code
FROM manifest_events me
LEFT JOIN users u ON u.id = me.actor_user_id
LEFT JOIN operating_units ou ON ou.id = me.operating_unit_id
WHERE me.manifest_id = sqlc.arg('manifest_id')
ORDER BY me.sequence DESC
LIMIT sqlc.arg('page_size');

-- name: BumpManifestEventSequence :one
UPDATE manifests SET event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING id, event_sequence, status;

-- name: ListExpectedInboundManifests :many
-- The hub's inbound board (M14): everything dispatched towards me that has not
-- yet been received, oldest first because that is what is most overdue.
SELECT m.public_id, m.manifest_code, m.status, m.bag_count, m.total_shipment_count,
       m.total_piece_count, m.total_weight_grams, m.dispatched_at,
       o.code AS origin_code, o.name AS origin_name,
       t.public_id AS trip_public_id, t.trip_code, t.status AS trip_status,
       t.scheduled_arrival, t.actual_departure, t.mode AS trip_mode,
       v.registration_number AS vehicle_registration
FROM manifests m
JOIN operating_units o ON o.id = m.origin_unit_id
LEFT JOIN trips t ON t.id = m.trip_id
LEFT JOIN vehicles v ON v.id = t.vehicle_id
WHERE m.organization_id = sqlc.arg('organization_id')
  AND m.destination_unit_id = sqlc.arg('destination_unit_id')
  AND m.status = 'DISPATCHED'
ORDER BY m.dispatched_at, m.id
LIMIT sqlc.arg('page_size');
