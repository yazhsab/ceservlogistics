-- Operational scanning (M10) and holds.

-- name: ResolveScanBarcode :one
-- The scanner's hot path: turn a barcode into the shipment plus everything the
-- custody check needs, in one round trip.
--
-- A barcode is either an AWB or a per-piece barcode. Both are UNIQUE, so the
-- lookup is two index probes at worst and returns at most one row.
SELECT s.id, s.public_id, s.awb, s.organization_id, s.current_status, s.movement_direction,
       s.current_custody_unit_id, s.current_custody_user_id, s.current_bag_id, s.current_trip_id,
       s.origin_branch_id, s.origin_hub_id, s.destination_hub_id, s.destination_branch_id,
       s.piece_count, s.chargeable_weight_grams, s.actual_weight_grams,
       s.event_sequence, s.is_held, s.customer_id, s.booking_unit_id,
       s.destination_pincode, s.origin_pincode, s.cod_amount_minor, s.payment_mode,
       p.id AS package_id, p.sequence AS package_sequence
FROM shipments s
LEFT JOIN shipment_packages p
       ON p.piece_barcode = sqlc.arg('barcode') AND p.shipment_id = s.id
WHERE s.organization_id = sqlc.arg('organization_id')
  AND (s.awb = sqlc.arg('barcode')
       OR s.id = (SELECT shipment_id FROM shipment_packages
                   WHERE piece_barcode = sqlc.arg('barcode') LIMIT 1));

-- name: LockShipmentByIDForUpdate :one
SELECT * FROM shipments
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: RecordScanEvent :one
INSERT INTO scan_events (
    public_id, organization_id, raw_barcode, shipment_id, package_id, scan_type,
    operating_unit_id, scanned_by_user_id, device_event_id, device_id, source,
    occurred_at, outcome, rejection_code, rejection_message, shipment_event_id,
    from_status, to_status, latitude, longitude, reference_type, reference_id, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('raw_barcode'),
    sqlc.arg('shipment_id'), sqlc.arg('package_id'), sqlc.arg('scan_type'),
    sqlc.arg('operating_unit_id'), sqlc.arg('scanned_by_user_id'),
    sqlc.arg('device_event_id'), sqlc.arg('device_id'), sqlc.arg('source'),
    COALESCE(sqlc.narg('occurred_at')::timestamptz, now()), sqlc.arg('outcome'),
    sqlc.arg('rejection_code'), sqlc.arg('rejection_message'),
    sqlc.arg('shipment_event_id'), sqlc.arg('from_status'), sqlc.arg('to_status'),
    sqlc.arg('latitude'), sqlc.arg('longitude'), sqlc.arg('reference_type'),
    sqlc.arg('reference_id'), sqlc.arg('metadata')
) RETURNING *;

-- name: GetScanEventByDeviceEvent :one
-- Device-level idempotency. A retried upload returns the original outcome.
SELECT se.*, s.public_id AS shipment_public_id, s.awb, s.current_status
FROM scan_events se
LEFT JOIN shipments s ON s.id = se.shipment_id
WHERE se.organization_id = sqlc.arg('organization_id')
  AND se.device_id = sqlc.arg('device_id')
  AND se.device_event_id = sqlc.arg('device_event_id');

-- name: ListScanEvents :many
SELECT se.id, se.public_id, se.raw_barcode, se.scan_type, se.outcome, se.rejection_code,
       se.rejection_message, se.occurred_at, se.recorded_at, se.from_status, se.to_status,
       se.source, se.device_id,
       s.public_id AS shipment_public_id, s.awb,
       ou.code AS unit_code, ou.name AS unit_name,
       u.public_id AS scanned_by_public_id, u.full_name AS scanned_by_name
FROM scan_events se
LEFT JOIN shipments s ON s.id = se.shipment_id
JOIN operating_units ou ON ou.id = se.operating_unit_id
LEFT JOIN users u ON u.id = se.scanned_by_user_id
WHERE se.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('unit_id')::bigint IS NULL OR se.operating_unit_id = sqlc.narg('unit_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR se.operating_unit_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('shipment_id')::bigint IS NULL OR se.shipment_id = sqlc.narg('shipment_id'))
  AND (sqlc.narg('scan_type')::text IS NULL OR se.scan_type = sqlc.narg('scan_type'))
  AND (sqlc.narg('outcome')::text IS NULL OR se.outcome = sqlc.narg('outcome'))
  AND (sqlc.narg('cursor_occurred_at')::timestamptz IS NULL
       OR (se.occurred_at, se.id) < (sqlc.narg('cursor_occurred_at'), sqlc.narg('cursor_id')::bigint))
ORDER BY se.occurred_at DESC, se.id DESC
LIMIT sqlc.arg('page_size');

-- name: ListShipmentScans :many
SELECT se.public_id, se.raw_barcode, se.scan_type, se.outcome, se.occurred_at,
       se.from_status, se.to_status, ou.code AS unit_code, ou.name AS unit_name,
       u.full_name AS scanned_by_name
FROM scan_events se
JOIN operating_units ou ON ou.id = se.operating_unit_id
LEFT JOIN users u ON u.id = se.scanned_by_user_id
WHERE se.shipment_id = sqlc.arg('shipment_id')
ORDER BY se.occurred_at DESC, se.id DESC
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Holds
-- ---------------------------------------------------------------------------

-- name: CreateShipmentHold :one
INSERT INTO shipment_holds (
    public_id, organization_id, shipment_id, operating_unit_id,
    hold_reason_code, reason, held_by_user_id, expected_release_at, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetActiveHold :one
SELECT h.*, ou.code AS unit_code, ou.name AS unit_name, u.full_name AS held_by_name
FROM shipment_holds h
JOIN operating_units ou ON ou.id = h.operating_unit_id
LEFT JOIN users u ON u.id = h.held_by_user_id
WHERE h.shipment_id = sqlc.arg('shipment_id') AND h.released_at IS NULL;

-- name: ReleaseShipmentHold :one
UPDATE shipment_holds
SET released_at = now(), released_by_user_id = sqlc.narg('released_by_user_id'),
    release_notes = sqlc.narg('release_notes')
WHERE shipment_id = sqlc.arg('shipment_id') AND released_at IS NULL
RETURNING *;

-- name: SetShipmentHoldFlag :one
UPDATE shipments
SET is_held = sqlc.arg('is_held'), hold_reason = sqlc.narg('hold_reason')
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING id, is_held, hold_reason;

-- name: ListActiveHolds :many
SELECT h.public_id, h.hold_reason_code, h.reason, h.held_at, h.expected_release_at,
       s.public_id AS shipment_public_id, s.awb, s.current_status,
       ou.code AS unit_code, ou.name AS unit_name,
       u.full_name AS held_by_name
FROM shipment_holds h
JOIN shipments s ON s.id = h.shipment_id
JOIN operating_units ou ON ou.id = h.operating_unit_id
LEFT JOIN users u ON u.id = h.held_by_user_id
WHERE h.organization_id = sqlc.arg('organization_id')
  AND h.released_at IS NULL
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR h.operating_unit_id = ANY(sqlc.narg('unit_ids')))
ORDER BY h.held_at DESC, h.id DESC
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Shipment mutation helpers used by every operational module
-- ---------------------------------------------------------------------------

-- name: ApplyOperationalTransition :one
-- The single write behind every Release 2 state change.
--
-- `current_status = expected_status` makes it a compare-and-swap, so two
-- concurrent scans on the same parcel cannot both succeed: the loser updates
-- zero rows and is told to retry (§22). It also reserves the next event
-- sequence in the same statement, which is what makes the
-- UNIQUE(shipment_id, sequence) on shipment_events an effective guard.
UPDATE shipments
SET current_status = sqlc.arg('to_status'),
    status_changed_at = now(),
    event_sequence = event_sequence + 1,
    current_custody_unit_id = CASE WHEN sqlc.arg('set_custody_unit')::boolean
                                   THEN sqlc.narg('custody_unit_id') ELSE current_custody_unit_id END,
    current_custody_user_id = CASE WHEN sqlc.arg('set_custody_user')::boolean
                                   THEN sqlc.narg('custody_user_id') ELSE current_custody_user_id END,
    current_bag_id = CASE WHEN sqlc.arg('set_bag')::boolean
                          THEN sqlc.narg('bag_id') ELSE current_bag_id END,
    current_trip_id = CASE WHEN sqlc.arg('set_trip')::boolean
                           THEN sqlc.narg('trip_id') ELSE current_trip_id END,
    movement_direction = COALESCE(sqlc.narg('movement_direction'), movement_direction),
    picked_up_at = CASE WHEN sqlc.arg('to_status')::text = 'PICKED_UP'
                        THEN COALESCE(picked_up_at, now()) ELSE picked_up_at END,
    first_ofd_at = CASE WHEN sqlc.arg('to_status')::text = 'OUT_FOR_DELIVERY'
                        THEN COALESCE(first_ofd_at, now()) ELSE first_ofd_at END,
    delivered_at = CASE WHEN sqlc.arg('to_status')::text IN ('DELIVERED','RTO_DELIVERED')
                        THEN now() ELSE delivered_at END,
    delivery_attempt_count = delivery_attempt_count + sqlc.arg('delivery_attempt_increment')::int,
    pickup_attempt_count = pickup_attempt_count + sqlc.arg('pickup_attempt_increment')::int
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
  AND current_status = sqlc.arg('expected_status')
RETURNING *;

-- name: AppendShipmentEventFull :one
-- Append-only history with the full operational envelope (§Operational Event
-- Integrity): occurrence versus record time, actor, facility, device, source.
INSERT INTO shipment_events (
    public_id, organization_id, shipment_id, sequence, event_type, from_status, to_status,
    occurred_at, client_occurred_at, actor_user_id, actor_type, operating_unit_id,
    location_pincode, location_name, description, internal_remarks, reason_code,
    request_id, idempotency_key, source, device_id, device_model, latitude, longitude, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('shipment_id'),
    sqlc.arg('sequence'), sqlc.arg('event_type'), sqlc.arg('from_status'),
    sqlc.arg('to_status'), COALESCE(sqlc.narg('occurred_at')::timestamptz, now()),
    sqlc.arg('client_occurred_at'), sqlc.arg('actor_user_id'), sqlc.arg('actor_type'),
    sqlc.arg('operating_unit_id'), sqlc.arg('location_pincode'), sqlc.arg('location_name'),
    sqlc.arg('description'), sqlc.arg('internal_remarks'), sqlc.arg('reason_code'),
    sqlc.arg('request_id'), sqlc.arg('idempotency_key'), sqlc.arg('source'),
    sqlc.arg('device_id'), sqlc.arg('device_model'), sqlc.arg('latitude'),
    sqlc.arg('longitude'), sqlc.arg('metadata')
)
RETURNING *;

-- name: ReserveShipmentEventSequence :one
-- For events that record something without changing status (a location update
-- during reverse movement, a remark). Still bumps the counter so the event gets
-- a unique, gap-free sequence.
UPDATE shipments SET event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING id, event_sequence, current_status;

-- name: SetShipmentCustody :one
-- Custody-only move: the parcel changed hands without changing lifecycle state,
-- which happens when a bag is loaded onto a trip.
UPDATE shipments
SET current_custody_unit_id = CASE WHEN sqlc.arg('set_custody_unit')::boolean
                                   THEN sqlc.narg('custody_unit_id') ELSE current_custody_unit_id END,
    current_custody_user_id = CASE WHEN sqlc.arg('set_custody_user')::boolean
                                   THEN sqlc.narg('custody_user_id') ELSE current_custody_user_id END,
    current_bag_id = CASE WHEN sqlc.arg('set_bag')::boolean
                          THEN sqlc.narg('bag_id') ELSE current_bag_id END,
    current_trip_id = CASE WHEN sqlc.arg('set_trip')::boolean
                           THEN sqlc.narg('trip_id') ELSE current_trip_id END
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;
