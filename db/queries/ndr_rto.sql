-- NDR (M17) and RTO (M18).

-- name: CreateNDRReason :one
INSERT INTO ndr_reasons (
    public_id, organization_id, code, name, description, category, default_action,
    max_attempts, is_customer_fault, requires_evidence, auto_rto_after_max, display_order
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: GetNDRReasonByCode :one
SELECT * FROM ndr_reasons
WHERE organization_id = sqlc.arg('organization_id') AND code = sqlc.arg('code');

-- name: ListNDRReasons :many
SELECT * FROM ndr_reasons
WHERE organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('is_active')::boolean IS NULL OR is_active = sqlc.narg('is_active'))
ORDER BY display_order, code;

-- name: UpdateNDRReason :one
UPDATE ndr_reasons
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    category = COALESCE(sqlc.narg('category'), category),
    default_action = COALESCE(sqlc.narg('default_action'), default_action),
    max_attempts = COALESCE(sqlc.narg('max_attempts'), max_attempts),
    is_customer_fault = COALESCE(sqlc.narg('is_customer_fault'), is_customer_fault),
    requires_evidence = COALESCE(sqlc.narg('requires_evidence'), requires_evidence),
    auto_rto_after_max = COALESCE(sqlc.narg('auto_rto_after_max'), auto_rto_after_max),
    display_order = COALESCE(sqlc.narg('display_order'), display_order),
    is_active = COALESCE(sqlc.narg('is_active'), is_active)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- NDR cases
-- ---------------------------------------------------------------------------

-- name: CreateNDRCase :one
INSERT INTO ndr_cases (
    public_id, organization_id, case_code, shipment_id, ndr_reason_id,
    current_reason_code, max_attempts, branch_id, current_action, next_attempt_at, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetActiveNDRCase :one
SELECT * FROM ndr_cases
WHERE shipment_id = sqlc.arg('shipment_id')
  AND status IN ('OPEN','PENDING_CUSTOMER','SCHEDULED','ESCALATED')
FOR UPDATE;

-- name: GetNDRCaseByPublicID :one
SELECT n.*, s.public_id AS shipment_public_id, s.awb, s.current_status AS shipment_status,
       s.customer_id, s.destination_branch_id, s.promised_delivery_at,
       b.code AS branch_code, b.name AS branch_name,
       r.name AS reason_name, r.category AS reason_category, r.is_customer_fault,
       u.full_name AS assigned_to_name
FROM ndr_cases n
JOIN shipments s ON s.id = n.shipment_id
LEFT JOIN operating_units b ON b.id = n.branch_id
LEFT JOIN ndr_reasons r ON r.id = n.ndr_reason_id
LEFT JOIN users u ON u.id = n.assigned_to_user_id
WHERE n.public_id = sqlc.arg('public_id') AND n.organization_id = sqlc.arg('organization_id');

-- name: LockNDRCaseForUpdate :one
SELECT * FROM ndr_cases
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListNDRCases :many
SELECT n.id, n.public_id, n.case_code, n.status, n.current_reason_code, n.current_action,
       n.attempt_count, n.max_attempts, n.next_attempt_at, n.opened_at, n.resolved_at,
       n.created_at,
       s.public_id AS shipment_public_id, s.awb, s.current_status AS shipment_status,
       s.promised_delivery_at, s.cod_amount_minor, s.payment_mode,
       b.code AS branch_code, b.name AS branch_name,
       c.name AS customer_name, c.code AS customer_code,
       r.name AS reason_name, r.category AS reason_category,
       sa.contact_name AS recipient_name, sa.phone AS recipient_phone, sa.pincode
FROM ndr_cases n
JOIN shipments s ON s.id = n.shipment_id
JOIN customers c ON c.id = s.customer_id
LEFT JOIN operating_units b ON b.id = n.branch_id
LEFT JOIN ndr_reasons r ON r.id = n.ndr_reason_id
LEFT JOIN shipment_address_snapshots sa ON sa.shipment_id = s.id AND sa.role = 'RECIPIENT'
WHERE n.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR n.status = sqlc.narg('status'))
  AND (sqlc.narg('reason_code')::text IS NULL OR n.current_reason_code = sqlc.narg('reason_code'))
  AND (sqlc.narg('action')::text IS NULL OR n.current_action = sqlc.narg('action'))
  AND (sqlc.narg('branch_id')::bigint IS NULL OR n.branch_id = sqlc.narg('branch_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR n.branch_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('customer_ids')::bigint[] IS NULL OR s.customer_id = ANY(sqlc.narg('customer_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (n.created_at, n.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY n.created_at DESC, n.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdateNDRCase :one
UPDATE ndr_cases
SET status = sqlc.arg('to_status'),
    current_reason_code = COALESCE(sqlc.narg('current_reason_code'), current_reason_code),
    ndr_reason_id = COALESCE(sqlc.narg('ndr_reason_id'), ndr_reason_id),
    current_action = COALESCE(sqlc.narg('current_action'), current_action),
    next_attempt_at = CASE WHEN sqlc.arg('clear_next_attempt')::boolean THEN NULL
                           ELSE COALESCE(sqlc.narg('next_attempt_at'), next_attempt_at) END,
    attempt_count = attempt_count + sqlc.arg('attempt_increment')::int,
    assigned_to_user_id = COALESCE(sqlc.narg('assigned_to_user_id'), assigned_to_user_id),
    resolved_at = CASE WHEN sqlc.arg('to_status')::text IN
                       ('RESOLVED_DELIVERED','RESOLVED_RTO','RESOLVED_CANCELLED','CLOSED')
                       THEN now() ELSE resolved_at END,
    resolution_notes = COALESCE(sqlc.narg('resolution_notes'), resolution_notes)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: ApplyNDRAddressCorrection :one
UPDATE ndr_cases
SET corrected_address_line1 = sqlc.narg('line1'),
    corrected_address_line2 = sqlc.narg('line2'),
    corrected_landmark = sqlc.narg('landmark'),
    corrected_pincode = sqlc.narg('pincode'),
    corrected_phone = sqlc.narg('phone'),
    corrected_at = now(),
    corrected_by_user_id = sqlc.narg('corrected_by_user_id')
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: CreateNDRAttempt :one
INSERT INTO ndr_attempts (
    public_id, organization_id, ndr_case_id, delivery_attempt_id, attempt_number,
    reason_code, remarks, evidence_object_keys, occurred_at, recorded_by_user_id, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('ndr_case_id'),
    sqlc.arg('delivery_attempt_id'), sqlc.arg('attempt_number'), sqlc.arg('reason_code'),
    sqlc.arg('remarks'), sqlc.arg('evidence_object_keys'),
    COALESCE(sqlc.narg('occurred_at')::timestamptz, now()),
    sqlc.arg('recorded_by_user_id'), sqlc.arg('metadata')
)
RETURNING *;

-- name: ListNDRAttempts :many
SELECT na.*, u.full_name AS recorded_by_name
FROM ndr_attempts na
LEFT JOIN users u ON u.id = na.recorded_by_user_id
WHERE na.ndr_case_id = sqlc.arg('ndr_case_id')
ORDER BY na.attempt_number DESC;

-- name: CreateNDRAction :one
INSERT INTO ndr_actions (
    public_id, organization_id, ndr_case_id, sequence, action, requested_by,
    instructions, scheduled_for, contact_name, contact_phone, contact_notes,
    created_by_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING *;

-- name: SupersedePendingNDRActions :execrows
UPDATE ndr_actions SET status = 'SUPERSEDED'
WHERE ndr_case_id = sqlc.arg('ndr_case_id') AND status = 'PENDING';

-- name: MarkNDRActionApplied :one
UPDATE ndr_actions SET status = 'APPLIED', applied_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListNDRActions :many
SELECT na.*, u.full_name AS created_by_name
FROM ndr_actions na
LEFT JOIN users u ON u.id = na.created_by_user_id
WHERE na.ndr_case_id = sqlc.arg('ndr_case_id')
ORDER BY na.sequence DESC;

-- name: NextNDRActionSequence :one
SELECT COALESCE(max(sequence), 0)::int + 1 AS next_sequence FROM ndr_actions
WHERE ndr_case_id = sqlc.arg('ndr_case_id');

-- name: ListDueNDRCases :many
-- Worker sweep: cases whose scheduled reattempt time has arrived.
SELECT n.id, n.public_id, n.case_code, n.shipment_id, n.current_action, n.next_attempt_at,
       n.attempt_count, n.max_attempts, s.awb, s.current_status
FROM ndr_cases n
JOIN shipments s ON s.id = n.shipment_id
WHERE n.organization_id = sqlc.arg('organization_id')
  AND n.status IN ('OPEN','SCHEDULED')
  AND n.next_attempt_at IS NOT NULL
  AND n.next_attempt_at <= now()
ORDER BY n.next_attempt_at
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- RTO
-- ---------------------------------------------------------------------------

-- name: CreateRTOCase :one
INSERT INTO rto_cases (
    public_id, organization_id, case_code, shipment_id, ndr_case_id, reason_code,
    reason_notes, initiated_by_user_id, return_branch_id, return_hub_id, origin_hub_id,
    route_resolution_source, route_legs, route_explanation,
    return_contact_name, return_phone, return_line1, return_line2,
    return_city, return_state, return_pincode,
    rto_charge_minor, currency, charge_bearer, charge_rule_notes, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
RETURNING *;

-- name: GetRTOCaseByPublicID :one
SELECT r.*, s.public_id AS shipment_public_id, s.awb, s.current_status AS shipment_status,
       s.customer_id, s.movement_direction,
       rb.code AS return_branch_code, rb.name AS return_branch_name,
       u.full_name AS initiated_by_name
FROM rto_cases r
JOIN shipments s ON s.id = r.shipment_id
LEFT JOIN operating_units rb ON rb.id = r.return_branch_id
LEFT JOIN users u ON u.id = r.initiated_by_user_id
WHERE r.public_id = sqlc.arg('public_id') AND r.organization_id = sqlc.arg('organization_id');

-- name: GetActiveRTOCase :one
SELECT * FROM rto_cases
WHERE shipment_id = sqlc.arg('shipment_id')
  AND status NOT IN ('RETURNED','DISPOSED','CANCELLED')
FOR UPDATE;

-- name: LockRTOCaseForUpdate :one
SELECT * FROM rto_cases
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListRTOCases :many
SELECT r.id, r.public_id, r.case_code, r.status, r.reason_code, r.initiated_at, r.returned_at,
       r.rto_charge_minor, r.currency, r.charge_bearer, r.created_at,
       s.public_id AS shipment_public_id, s.awb, s.current_status AS shipment_status,
       rb.code AS return_branch_code, rb.name AS return_branch_name,
       c.name AS customer_name, c.code AS customer_code
FROM rto_cases r
JOIN shipments s ON s.id = r.shipment_id
JOIN customers c ON c.id = s.customer_id
LEFT JOIN operating_units rb ON rb.id = r.return_branch_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
  AND (sqlc.narg('return_branch_id')::bigint IS NULL OR r.return_branch_id = sqlc.narg('return_branch_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR r.return_branch_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('customer_ids')::bigint[] IS NULL OR s.customer_id = ANY(sqlc.narg('customer_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (r.created_at, r.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdateRTOCaseStatus :one
UPDATE rto_cases
SET status = sqlc.arg('to_status'),
    returned_at = CASE WHEN sqlc.arg('to_status')::text = 'RETURNED' THEN now() ELSE returned_at END,
    returned_to_name = COALESCE(sqlc.narg('returned_to_name'), returned_to_name),
    closed_at = CASE WHEN sqlc.arg('to_status')::text IN ('RETURNED','DISPOSED','CANCELLED')
                     THEN now() ELSE closed_at END
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: CreateRTOLeg :one
INSERT INTO rto_legs (
    organization_id, rto_case_id, sequence, from_unit_id, to_unit_id, leg_type, status
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: ListRTOLegs :many
SELECT rl.*, f.code AS from_code, f.name AS from_name,
       t.code AS to_code, t.name AS to_name
FROM rto_legs rl
LEFT JOIN operating_units f ON f.id = rl.from_unit_id
LEFT JOIN operating_units t ON t.id = rl.to_unit_id
WHERE rl.rto_case_id = sqlc.arg('rto_case_id')
ORDER BY rl.sequence;

-- name: AdvanceRTOLeg :one
UPDATE rto_legs
SET status = sqlc.arg('to_status'),
    started_at = CASE WHEN sqlc.arg('to_status')::text = 'IN_PROGRESS'
                      THEN COALESCE(started_at, now()) ELSE started_at END,
    completed_at = CASE WHEN sqlc.arg('to_status')::text = 'COMPLETED' THEN now() ELSE completed_at END,
    trip_id = COALESCE(sqlc.narg('trip_id'), trip_id),
    manifest_id = COALESCE(sqlc.narg('manifest_id'), manifest_id)
WHERE rto_case_id = sqlc.arg('rto_case_id') AND sequence = sqlc.arg('sequence')
RETURNING *;

-- name: MarkRTOLegForUnit :one
-- Advance the reverse journey when the parcel is received at a facility on the
-- planned path: complete the leg that ends here and start the next one.
UPDATE rto_legs
SET status = 'COMPLETED', completed_at = now()
WHERE rto_case_id = sqlc.arg('rto_case_id')
  AND to_unit_id = sqlc.arg('unit_id')
  AND status IN ('PENDING','IN_PROGRESS')
RETURNING *;

-- name: SeedDefaultNDRReasons :exec
-- Give a newly created organization a working NDR catalogue.
--
-- Without this a tenant's first failed delivery would be refused for an unknown
-- reason code, which is a bad first day. The list matches the defaults migration
-- 0019 applied to organizations that already existed; keeping both in SQL means
-- there is one definition of "a sensible starting catalogue".
INSERT INTO ndr_reasons (public_id, organization_id, code, name, description, category,
                         default_action, max_attempts, is_customer_fault, requires_evidence,
                         auto_rto_after_max, display_order)
SELECT gen_seed_public_id('ndrs'), sqlc.arg('organization_id'), d.code, d.name, d.description,
       d.category, d.default_action, d.max_attempts, d.is_customer_fault, d.requires_evidence,
       d.auto_rto_after_max, d.display_order
FROM (VALUES
    ('CUSTOMER_NOT_AVAILABLE','Customer not available','Nobody was present at the delivery address.','CUSTOMER_UNAVAILABLE','REATTEMPT',3,true,false,true,10),
    ('PREMISES_CLOSED','Premises closed','The business or residence was closed at the time of the attempt.','ACCESS','RESCHEDULE',3,false,false,true,20),
    ('ADDRESS_INCOMPLETE','Address incomplete','The address is missing details needed to locate it.','ADDRESS_PROBLEM','ADDRESS_CORRECTION',2,true,false,true,30),
    ('ADDRESS_NOT_FOUND','Address not found','The address could not be located.','ADDRESS_PROBLEM','ADDRESS_CORRECTION',2,true,true,true,40),
    ('CUSTOMER_REFUSED','Customer refused delivery','The consignee declined to accept the shipment.','REFUSED','RTO',1,true,true,true,50),
    ('COD_NOT_READY','COD amount not ready','The consignee could not pay the COD amount.','PAYMENT','REATTEMPT',2,true,false,true,60),
    ('CUSTOMER_RESCHEDULED','Customer asked to reschedule','The consignee requested delivery on another day.','CUSTOMER_UNAVAILABLE','RESCHEDULE',3,true,false,false,70),
    ('PHONE_UNREACHABLE','Consignee unreachable','The consignee could not be contacted by phone.','CUSTOMER_UNAVAILABLE','CONTACT_REQUIRED',3,true,false,true,80),
    ('RESTRICTED_ACCESS','Restricted access','The agent could not enter the premises or area.','ACCESS','CONTACT_REQUIRED',3,false,false,true,90),
    ('WEATHER_DISRUPTION','Weather disruption','Delivery could not be attempted because of weather.','WEATHER','REATTEMPT',5,false,false,false,100),
    ('VEHICLE_BREAKDOWN','Operational delay','The delivery could not be completed for operational reasons.','OPERATIONAL','REATTEMPT',5,false,false,false,110),
    ('SHIPMENT_DAMAGED','Shipment damaged','The shipment was found damaged before delivery.','DAMAGE','ESCALATE',1,false,true,false,120)
) AS d(code, name, description, category, default_action, max_attempts, is_customer_fault,
       requires_evidence, auto_rto_after_max, display_order)
ON CONFLICT DO NOTHING;
