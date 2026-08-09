-- Delivery runs and attempts (M15, M16).

-- name: CreateDeliveryRun :one
INSERT INTO delivery_runs (
    public_id, organization_id, run_code, branch_id, agent_user_id, run_date,
    vehicle_reference, currency, created_by_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: GetDeliveryRunByPublicID :one
SELECT r.*, b.public_id AS branch_public_id, b.code AS branch_code, b.name AS branch_name,
       u.public_id AS agent_public_id, u.full_name AS agent_name, u.phone AS agent_phone
FROM delivery_runs r
JOIN operating_units b ON b.id = r.branch_id
JOIN users u ON u.id = r.agent_user_id
WHERE r.public_id = sqlc.arg('public_id') AND r.organization_id = sqlc.arg('organization_id');

-- name: LockDeliveryRunForUpdate :one
SELECT * FROM delivery_runs
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListDeliveryRuns :many
SELECT r.id, r.public_id, r.run_code, r.status, r.run_date, r.planned_stops, r.completed_stops,
       r.delivered_count, r.failed_count, r.cod_expected_minor, r.cod_collected_minor,
       r.currency, r.dispatched_at, r.completed_at, r.created_at,
       b.public_id AS branch_public_id, b.code AS branch_code, b.name AS branch_name,
       u.public_id AS agent_public_id, u.full_name AS agent_name
FROM delivery_runs r
JOIN operating_units b ON b.id = r.branch_id
JOIN users u ON u.id = r.agent_user_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
  AND (sqlc.narg('run_date')::date IS NULL OR r.run_date = sqlc.narg('run_date'))
  AND (sqlc.narg('agent_user_id')::bigint IS NULL OR r.agent_user_id = sqlc.narg('agent_user_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR r.branch_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (r.created_at, r.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdateDeliveryRunStatus :one
UPDATE delivery_runs
SET status = sqlc.arg('to_status'),
    agent_user_id = COALESCE(sqlc.narg('agent_user_id'), agent_user_id),
    dispatched_at = CASE WHEN sqlc.arg('to_status')::text = 'DISPATCHED' THEN now() ELSE dispatched_at END,
    dispatched_by_user_id = COALESCE(sqlc.narg('dispatched_by_user_id'), dispatched_by_user_id),
    started_at = CASE WHEN sqlc.arg('to_status')::text = 'IN_PROGRESS'
                      THEN COALESCE(started_at, now()) ELSE started_at END,
    completed_at = CASE WHEN sqlc.arg('to_status')::text = 'COMPLETED' THEN now() ELSE completed_at END,
    closed_at = CASE WHEN sqlc.arg('to_status')::text = 'CLOSED' THEN now() ELSE closed_at END,
    closed_by_user_id = COALESCE(sqlc.narg('closed_by_user_id'), closed_by_user_id),
    cancelled_at = CASE WHEN sqlc.arg('to_status')::text = 'CANCELLED' THEN now() ELSE cancelled_at END,
    cancellation_reason = COALESCE(sqlc.narg('cancellation_reason'), cancellation_reason)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: AddDeliveryRunItem :one
INSERT INTO delivery_run_items (
    public_id, organization_id, delivery_run_id, shipment_id, stop_sequence,
    delivery_type, cod_amount_minor, added_by_user_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: GetActiveDeliveryRunItem :one
-- The structural guard against two agents holding the same parcel. Backed by
-- delivery_run_items_active_idx.
SELECT dri.*, dr.public_id AS run_public_id, dr.run_code, dr.status AS run_status,
       dr.agent_user_id, u.full_name AS agent_name
FROM delivery_run_items dri
JOIN delivery_runs dr ON dr.id = dri.delivery_run_id
JOIN users u ON u.id = dr.agent_user_id
WHERE dri.shipment_id = sqlc.arg('shipment_id')
  AND dri.status IN ('PENDING','OUT_FOR_DELIVERY');

-- name: GetDeliveryRunItemForShipment :one
SELECT * FROM delivery_run_items
WHERE delivery_run_id = sqlc.arg('delivery_run_id') AND shipment_id = sqlc.arg('shipment_id')
FOR UPDATE;

-- name: ListDeliveryRunItems :many
SELECT dri.public_id, dri.stop_sequence, dri.status, dri.delivery_type,
       dri.cod_amount_minor, dri.cod_collected_minor, dri.attempt_count,
       dri.dispatched_at, dri.completed_at,
       s.public_id AS shipment_public_id, s.awb, s.current_status, s.piece_count,
       s.payment_mode, s.promised_delivery_at, s.is_held, s.currency,
       sa.contact_name AS recipient_name, sa.phone AS recipient_phone,
       sa.line1, sa.line2, sa.landmark, sa.city_name, sa.pincode,
       sa.latitude, sa.longitude
FROM delivery_run_items dri
JOIN shipments s ON s.id = dri.shipment_id
LEFT JOIN shipment_address_snapshots sa ON sa.shipment_id = s.id AND sa.role = 'RECIPIENT'
WHERE dri.delivery_run_id = sqlc.arg('delivery_run_id')
  AND (sqlc.narg('only_active')::boolean IS NOT TRUE OR dri.status <> 'REMOVED')
ORDER BY dri.stop_sequence, dri.id;

-- name: ListDeliveryRunShipmentIDs :many
SELECT dri.id AS item_id, dri.shipment_id, dri.status, dri.delivery_type, dri.stop_sequence,
       s.current_status, s.event_sequence, s.is_held, s.destination_branch_id
FROM delivery_run_items dri
JOIN shipments s ON s.id = dri.shipment_id
WHERE dri.delivery_run_id = sqlc.arg('delivery_run_id') AND dri.status = 'PENDING'
ORDER BY dri.stop_sequence, dri.id;

-- name: UpdateDeliveryRunItemStatus :one
UPDATE delivery_run_items
SET status = sqlc.arg('to_status'),
    dispatched_at = CASE WHEN sqlc.arg('to_status')::text = 'OUT_FOR_DELIVERY'
                         THEN COALESCE(dispatched_at, now()) ELSE dispatched_at END,
    completed_at = CASE WHEN sqlc.arg('to_status')::text IN ('DELIVERED','FAILED','RETURNED_TO_BRANCH')
                        THEN now() ELSE completed_at END,
    cod_collected_minor = COALESCE(sqlc.narg('cod_collected_minor'), cod_collected_minor),
    attempt_count = attempt_count + sqlc.arg('attempt_increment')::int,
    removed_at = CASE WHEN sqlc.arg('to_status')::text = 'REMOVED' THEN now() ELSE removed_at END,
    removal_reason = COALESCE(sqlc.narg('removal_reason'), removal_reason)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: RecalculateDeliveryRunCounters :one
UPDATE delivery_runs r
SET planned_stops = c.total, completed_stops = c.completed,
    delivered_count = c.delivered, failed_count = c.failed,
    cod_expected_minor = c.cod_expected, cod_collected_minor = c.cod_collected
FROM (
    SELECT count(*) FILTER (WHERE status <> 'REMOVED') AS total,
           count(*) FILTER (WHERE status IN ('DELIVERED','FAILED','RETURNED_TO_BRANCH')) AS completed,
           count(*) FILTER (WHERE status = 'DELIVERED') AS delivered,
           count(*) FILTER (WHERE status = 'FAILED') AS failed,
           COALESCE(sum(cod_amount_minor) FILTER (WHERE status <> 'REMOVED'), 0)::bigint AS cod_expected,
           COALESCE(sum(cod_collected_minor), 0)::bigint AS cod_collected
    FROM delivery_run_items WHERE delivery_run_id = sqlc.arg('run_id')
) c
WHERE r.id = sqlc.arg('run_id')
RETURNING r.*;

-- ---------------------------------------------------------------------------
-- Attempts
-- ---------------------------------------------------------------------------

-- name: CreateDeliveryAttempt :one
INSERT INTO delivery_attempts (
    public_id, organization_id, shipment_id, delivery_run_id, delivery_run_item_id,
    attempt_number, outcome, failure_reason_code, remarks,
    recipient_name, recipient_relationship, recipient_phone,
    otp_required, otp_verified, otp_verified_at,
    cod_amount_minor, cod_collected_minor, cod_payment_mode, cod_reference, currency,
    agent_user_id, operating_unit_id, occurred_at, latitude, longitude,
    location_accuracy_m, device_id, device_event_id, next_attempt_at, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('shipment_id'),
    sqlc.arg('delivery_run_id'), sqlc.arg('delivery_run_item_id'),
    sqlc.arg('attempt_number'), sqlc.arg('outcome'), sqlc.arg('failure_reason_code'),
    sqlc.arg('remarks'), sqlc.arg('recipient_name'), sqlc.arg('recipient_relationship'),
    sqlc.arg('recipient_phone'), sqlc.arg('otp_required'), sqlc.arg('otp_verified'),
    sqlc.arg('otp_verified_at'), sqlc.arg('cod_amount_minor'),
    sqlc.arg('cod_collected_minor'), sqlc.arg('cod_payment_mode'),
    sqlc.arg('cod_reference'), sqlc.arg('currency'), sqlc.arg('agent_user_id'),
    sqlc.arg('operating_unit_id'), COALESCE(sqlc.narg('occurred_at')::timestamptz, now()),
    sqlc.arg('latitude'), sqlc.arg('longitude'), sqlc.arg('location_accuracy_m'),
    sqlc.arg('device_id'), sqlc.arg('device_event_id'), sqlc.arg('next_attempt_at'),
    sqlc.arg('metadata')
)
RETURNING *;

-- name: GetDeliveryAttemptByDeviceEvent :one
-- Offline retry: the field app resends the same device_event_id and gets the
-- original attempt back instead of delivering twice.
SELECT da.*, s.public_id AS shipment_public_id, s.awb, s.current_status
FROM delivery_attempts da
JOIN shipments s ON s.id = da.shipment_id
WHERE da.organization_id = sqlc.arg('organization_id')
  AND da.device_id = sqlc.arg('device_id')
  AND da.device_event_id = sqlc.arg('device_event_id');

-- name: ListDeliveryAttempts :many
SELECT da.public_id, da.attempt_number, da.outcome, da.failure_reason_code, da.remarks,
       da.recipient_name, da.recipient_relationship, da.otp_verified,
       da.cod_collected_minor, da.cod_payment_mode, da.currency,
       da.occurred_at, da.recorded_at, da.next_attempt_at,
       u.public_id AS agent_public_id, u.full_name AS agent_name,
       ou.code AS unit_code
FROM delivery_attempts da
LEFT JOIN users u ON u.id = da.agent_user_id
LEFT JOIN operating_units ou ON ou.id = da.operating_unit_id
WHERE da.shipment_id = sqlc.arg('shipment_id')
ORDER BY da.attempt_number DESC;

-- name: CountDeliveryAttempts :one
SELECT COALESCE(max(attempt_number), 0)::int AS last_attempt FROM delivery_attempts
WHERE shipment_id = sqlc.arg('shipment_id');

-- ---------------------------------------------------------------------------
-- Delivery queue at the destination branch (M15)
-- ---------------------------------------------------------------------------

-- name: ListDeliveryQueue :many
-- Everything at this branch that could go out today, most urgent first.
-- Served by shipments_delivery_queue_idx.
SELECT s.public_id, s.awb, s.current_status, s.status_changed_at, s.piece_count,
       s.chargeable_weight_grams, s.payment_mode, s.cod_amount_minor, s.currency,
       s.promised_delivery_at, s.delivery_attempt_count, s.is_held, s.hold_reason,
       s.movement_direction,
       sa.contact_name AS recipient_name, sa.phone AS recipient_phone,
       sa.line1, sa.line2, sa.landmark, sa.city_name, sa.pincode,
       c.name AS customer_name, c.code AS customer_code,
       n.public_id AS ndr_case_public_id, n.current_reason_code AS ndr_reason,
       n.current_action AS ndr_action, n.next_attempt_at AS ndr_next_attempt_at,
       (dri.id IS NOT NULL)::boolean AS on_active_run
FROM shipments s
JOIN customers c ON c.id = s.customer_id
LEFT JOIN shipment_address_snapshots sa ON sa.shipment_id = s.id AND sa.role = 'RECIPIENT'
LEFT JOIN ndr_cases n ON n.shipment_id = s.id
     AND n.status IN ('OPEN','PENDING_CUSTOMER','SCHEDULED','ESCALATED')
LEFT JOIN delivery_run_items dri ON dri.shipment_id = s.id
     AND dri.status IN ('PENDING','OUT_FOR_DELIVERY')
WHERE s.organization_id = sqlc.arg('organization_id')
  AND s.destination_branch_id = sqlc.arg('branch_id')::bigint
  AND s.current_status IN ('DESTINATION_BRANCH_RECEIVED','NDR','DELIVERY_FAILED')
  AND (sqlc.narg('include_held')::boolean IS TRUE OR s.is_held = false)
  AND (sqlc.narg('status')::text IS NULL OR s.current_status = sqlc.narg('status'))
  AND (sqlc.narg('exclude_assigned')::boolean IS NOT TRUE OR dri.id IS NULL)
ORDER BY s.promised_delivery_at NULLS LAST, s.status_changed_at, s.id
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Delivery OTP
-- ---------------------------------------------------------------------------

-- name: CreateDeliveryOTP :one
INSERT INTO delivery_otps (
    organization_id, shipment_id, code_hash, sent_to_masked, expires_at,
    max_attempts, issued_by_user_id
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: GetActiveDeliveryOTP :one
SELECT * FROM delivery_otps
WHERE shipment_id = sqlc.arg('shipment_id') AND consumed_at IS NULL
FOR UPDATE;

-- name: ConsumeDeliveryOTP :one
UPDATE delivery_otps SET consumed_at = now()
WHERE id = sqlc.arg('id') AND consumed_at IS NULL
RETURNING *;

-- name: IncrementDeliveryOTPAttempts :one
UPDATE delivery_otps SET attempt_count = attempt_count + 1
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ExpireStaleDeliveryOTPs :execrows
UPDATE delivery_otps SET consumed_at = now()
WHERE consumed_at IS NULL AND expires_at < now();

-- name: GetShipmentAddressSnapshot :one
-- The sender or recipient details frozen at booking. Used to reach the
-- consignee for an OTP without re-reading the customer's current address.
SELECT * FROM shipment_address_snapshots
WHERE shipment_id = sqlc.arg('shipment_id') AND role = sqlc.arg('role');
