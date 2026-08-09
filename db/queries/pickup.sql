-- Pickup (M09).

-- name: AllocateOperationalSequence :one
-- Shared counter for every human-readable operational code. Identical shape to
-- AWB allocation: one atomic statement, so N concurrent callers get N distinct
-- values without a process-local lock (§22).
INSERT INTO operational_sequences (organization_id, kind, scope_key, current_value)
VALUES (sqlc.arg('organization_id'), sqlc.arg('kind'), sqlc.arg('scope_key'), 1)
ON CONFLICT (organization_id, kind, scope_key)
DO UPDATE SET current_value = operational_sequences.current_value + 1,
              updated_at = now()
WHERE operational_sequences.current_value < operational_sequences.max_value
RETURNING current_value, max_value;

-- name: CreatePickupRequest :one
INSERT INTO pickup_requests (
    public_id, organization_id, reference_code, customer_id, branch_id, pickup_type, status,
    contact_name, contact_phone, alt_phone, line1, line2, landmark, city_name, state_name,
    pincode, latitude, longitude, source_address_id,
    scheduled_date, window_start, window_end,
    expected_piece_count, expected_weight_grams, special_instructions, max_attempts,
    requested_by_user_id, metadata
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28
) RETURNING *;

-- name: GetPickupRequestByPublicID :one
SELECT pr.*,
       c.public_id AS customer_public_id, c.code AS customer_code, c.name AS customer_name,
       b.public_id AS branch_public_id, b.code AS branch_code, b.name AS branch_name
FROM pickup_requests pr
JOIN customers c ON c.id = pr.customer_id
JOIN operating_units b ON b.id = pr.branch_id
WHERE pr.public_id = sqlc.arg('public_id') AND pr.organization_id = sqlc.arg('organization_id');

-- name: LockPickupRequest :one
SELECT * FROM pickup_requests
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListPickupRequests :many
-- Dispatcher queue. Keyset paginated on (created_at, id) so paging depth does
-- not change the plan.
SELECT pr.id, pr.public_id, pr.reference_code, pr.status, pr.pickup_type,
       pr.scheduled_date, pr.window_start, pr.window_end,
       pr.contact_name, pr.contact_phone, pr.line1, pr.city_name, pr.pincode,
       pr.expected_piece_count, pr.actual_piece_count, pr.attempt_count, pr.max_attempts,
       pr.created_at, pr.completed_at,
       c.public_id AS customer_public_id, c.code AS customer_code, c.name AS customer_name,
       b.public_id AS branch_public_id, b.code AS branch_code, b.name AS branch_name,
       a.public_id AS assignment_public_id, a.status AS assignment_status,
       u.public_id AS agent_public_id, u.full_name AS agent_name
FROM pickup_requests pr
JOIN customers c ON c.id = pr.customer_id
JOIN operating_units b ON b.id = pr.branch_id
LEFT JOIN pickup_assignments a ON a.pickup_request_id = pr.id
     AND a.status IN ('ASSIGNED','ACCEPTED','ARRIVED')
LEFT JOIN users u ON u.id = a.agent_user_id
WHERE pr.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR pr.status = sqlc.narg('status'))
  AND (sqlc.narg('pickup_type')::text IS NULL OR pr.pickup_type = sqlc.narg('pickup_type'))
  AND (sqlc.narg('customer_id')::bigint IS NULL OR pr.customer_id = sqlc.narg('customer_id'))
  AND (sqlc.narg('scheduled_date')::date IS NULL OR pr.scheduled_date = sqlc.narg('scheduled_date'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR pr.branch_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (pr.created_at, pr.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY pr.created_at DESC, pr.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdatePickupRequestStatus :one
UPDATE pickup_requests
SET status = sqlc.arg('to_status'),
    completed_at = CASE WHEN sqlc.arg('to_status')::text IN ('COMPLETED','PARTIALLY_COMPLETED')
                        THEN now() ELSE completed_at END,
    cancelled_at = CASE WHEN sqlc.arg('to_status')::text = 'CANCELLED' THEN now() ELSE cancelled_at END,
    cancellation_reason = COALESCE(sqlc.narg('cancellation_reason'), cancellation_reason),
    failure_reason = COALESCE(sqlc.narg('failure_reason'), failure_reason),
    actual_piece_count = COALESCE(sqlc.narg('actual_piece_count'), actual_piece_count),
    attempt_count = attempt_count + sqlc.arg('attempt_increment')::int,
    scheduled_date = COALESCE(sqlc.narg('scheduled_date'), scheduled_date),
    window_start = COALESCE(sqlc.narg('window_start'), window_start),
    window_end = COALESCE(sqlc.narg('window_end'), window_end)
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: AddShipmentToPickupRequest :one
INSERT INTO pickup_request_shipments (organization_id, pickup_request_id, shipment_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListPickupRequestShipments :many
SELECT prs.*, s.public_id AS shipment_public_id, s.awb, s.current_status,
       s.piece_count, s.actual_weight_grams, s.payment_mode, s.cod_amount_minor
FROM pickup_request_shipments prs
JOIN shipments s ON s.id = prs.shipment_id
WHERE prs.pickup_request_id = sqlc.arg('pickup_request_id')
ORDER BY prs.added_at, prs.id;

-- name: UpdatePickupRequestShipmentStatus :one
UPDATE pickup_request_shipments
SET status = sqlc.arg('status'), resolved_at = now(), remarks = sqlc.narg('remarks')
WHERE pickup_request_id = sqlc.arg('pickup_request_id')
  AND shipment_id = sqlc.arg('shipment_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Assignments
-- ---------------------------------------------------------------------------

-- name: CreatePickupAssignment :one
INSERT INTO pickup_assignments (
    public_id, organization_id, pickup_request_id, pickup_run_id, agent_user_id,
    assigned_by_user_id, stop_sequence, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: GetPickupAssignmentByPublicID :one
SELECT a.*, pr.public_id AS request_public_id, pr.reference_code, pr.status AS request_status,
       pr.branch_id, pr.pincode, pr.contact_name, pr.contact_phone,
       u.public_id AS agent_public_id, u.full_name AS agent_name
FROM pickup_assignments a
JOIN pickup_requests pr ON pr.id = a.pickup_request_id
JOIN users u ON u.id = a.agent_user_id
WHERE a.public_id = sqlc.arg('public_id') AND a.organization_id = sqlc.arg('organization_id');

-- name: GetActivePickupAssignment :one
SELECT * FROM pickup_assignments
WHERE pickup_request_id = sqlc.arg('pickup_request_id')
  AND status IN ('ASSIGNED','ACCEPTED','ARRIVED')
FOR UPDATE;

-- name: UpdatePickupAssignmentStatus :one
UPDATE pickup_assignments
SET status = sqlc.arg('to_status'),
    accepted_at = CASE WHEN sqlc.arg('to_status')::text = 'ACCEPTED' THEN now() ELSE accepted_at END,
    rejected_at = CASE WHEN sqlc.arg('to_status')::text = 'REJECTED' THEN now() ELSE rejected_at END,
    rejection_reason = COALESCE(sqlc.narg('rejection_reason'), rejection_reason),
    arrived_at = CASE WHEN sqlc.arg('to_status')::text = 'ARRIVED' THEN now() ELSE arrived_at END,
    completed_at = CASE WHEN sqlc.arg('to_status')::text IN ('COMPLETED','FAILED') THEN now() ELSE completed_at END
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: ListAgentPickupAssignments :many
-- The field agent's work list for a day. Small, ordered, and index-served.
SELECT a.public_id, a.status, a.stop_sequence, a.assigned_at, a.accepted_at, a.arrived_at,
       pr.public_id AS request_public_id, pr.reference_code, pr.pickup_type,
       pr.contact_name, pr.contact_phone, pr.line1, pr.line2, pr.landmark,
       pr.city_name, pr.pincode, pr.latitude, pr.longitude,
       pr.window_start, pr.window_end, pr.expected_piece_count, pr.special_instructions,
       pr.status AS request_status,
       c.name AS customer_name, c.code AS customer_code
FROM pickup_assignments a
JOIN pickup_requests pr ON pr.id = a.pickup_request_id
JOIN customers c ON c.id = pr.customer_id
WHERE a.organization_id = sqlc.arg('organization_id')
  AND a.agent_user_id = sqlc.arg('agent_user_id')
  AND (sqlc.narg('run_date')::date IS NULL OR pr.scheduled_date = sqlc.narg('run_date'))
  AND (sqlc.narg('only_open')::boolean IS NOT TRUE
       OR a.status IN ('ASSIGNED','ACCEPTED','ARRIVED'))
ORDER BY a.stop_sequence NULLS LAST, pr.window_start, a.id
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Runs
-- ---------------------------------------------------------------------------

-- name: CreatePickupRun :one
INSERT INTO pickup_runs (
    public_id, organization_id, run_code, branch_id, agent_user_id, run_date,
    vehicle_reference, created_by_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetPickupRunByPublicID :one
SELECT r.*, b.code AS branch_code, b.name AS branch_name,
       u.public_id AS agent_public_id, u.full_name AS agent_name
FROM pickup_runs r
JOIN operating_units b ON b.id = r.branch_id
JOIN users u ON u.id = r.agent_user_id
WHERE r.public_id = sqlc.arg('public_id') AND r.organization_id = sqlc.arg('organization_id');

-- name: UpdatePickupRunStatus :one
UPDATE pickup_runs
SET status = sqlc.arg('to_status'),
    started_at = CASE WHEN sqlc.arg('to_status')::text = 'STARTED' THEN now() ELSE started_at END,
    completed_at = CASE WHEN sqlc.arg('to_status')::text = 'COMPLETED' THEN now() ELSE completed_at END
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: RecalculatePickupRunCounters :exec
UPDATE pickup_runs r
SET planned_stops = c.total, completed_stops = c.completed
FROM (
    SELECT count(*) FILTER (WHERE status NOT IN ('CANCELLED','REASSIGNED','REJECTED')) AS total,
           count(*) FILTER (WHERE status = 'COMPLETED') AS completed
    FROM pickup_assignments WHERE pickup_run_id = sqlc.arg('run_id')
) c
WHERE r.id = sqlc.arg('run_id');

-- name: ListPickupRuns :many
SELECT r.public_id, r.run_code, r.status, r.run_date, r.planned_stops, r.completed_stops,
       r.collected_pieces, r.started_at, r.completed_at, r.created_at,
       b.code AS branch_code, b.name AS branch_name,
       u.public_id AS agent_public_id, u.full_name AS agent_name
FROM pickup_runs r
JOIN operating_units b ON b.id = r.branch_id
JOIN users u ON u.id = r.agent_user_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
  AND (sqlc.narg('run_date')::date IS NULL OR r.run_date = sqlc.narg('run_date'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR r.branch_id = ANY(sqlc.narg('unit_ids')))
ORDER BY r.run_date DESC, r.id DESC
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('row_offset');

-- ---------------------------------------------------------------------------
-- Attempts
-- ---------------------------------------------------------------------------

-- name: CreatePickupAttempt :one
INSERT INTO pickup_attempts (
    public_id, organization_id, pickup_request_id, pickup_assignment_id, attempt_number,
    outcome, failure_reason_code, remarks, pieces_collected, weight_grams,
    agent_user_id, occurred_at, latitude, longitude, device_id, device_event_id,
    next_attempt_at, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('pickup_request_id'),
    sqlc.arg('pickup_assignment_id'), sqlc.arg('attempt_number'), sqlc.arg('outcome'),
    sqlc.arg('failure_reason_code'), sqlc.arg('remarks'), sqlc.arg('pieces_collected'),
    sqlc.arg('weight_grams'), sqlc.arg('agent_user_id'),
    COALESCE(sqlc.narg('occurred_at')::timestamptz, now()), sqlc.arg('latitude'),
    sqlc.arg('longitude'), sqlc.arg('device_id'), sqlc.arg('device_event_id'),
    sqlc.arg('next_attempt_at'), sqlc.arg('metadata')
)
RETURNING *;

-- name: GetPickupAttemptByDeviceEvent :one
-- Offline retry: the field app resends the same device_event_id and gets the
-- original attempt back instead of creating a second one.
SELECT * FROM pickup_attempts
WHERE organization_id = sqlc.arg('organization_id')
  AND device_id = sqlc.arg('device_id')
  AND device_event_id = sqlc.arg('device_event_id');

-- name: ListPickupAttempts :many
SELECT a.*, u.full_name AS agent_name
FROM pickup_attempts a
LEFT JOIN users u ON u.id = a.agent_user_id
WHERE a.pickup_request_id = sqlc.arg('pickup_request_id')
ORDER BY a.attempt_number DESC;

-- name: CountPickupRequestsByStatus :many
SELECT status, count(*) AS total
FROM pickup_requests
WHERE organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR branch_id = ANY(sqlc.narg('unit_ids')))
  AND scheduled_date = sqlc.arg('scheduled_date')
GROUP BY status;
