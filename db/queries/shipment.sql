-- Shipment booking, AWB allocation and event history queries (M08).

-- ---------------------------------------------------------------------------
-- AWB allocation
-- ---------------------------------------------------------------------------

-- AllocateAWBNumber issues the next number for a (tenant, prefix, period).
--
-- The whole allocation is one statement. PostgreSQL serialises the DO UPDATE on
-- the conflicting row, so N concurrent callers receive N distinct values with no
-- application-level lock and no dependency on a single process. Gaps are
-- possible if the surrounding booking later fails, which is acceptable: AWBs
-- are identifiers, not a gapless accounting series.
-- name: AllocateAWBNumber :one
INSERT INTO awb_sequences (organization_id, prefix, period_key, current_value, max_value)
VALUES (sqlc.arg('organization_id'), sqlc.arg('prefix'), sqlc.arg('period_key'), 1, sqlc.arg('max_value'))
ON CONFLICT (organization_id, prefix, period_key) DO UPDATE
    SET current_value = awb_sequences.current_value + 1, updated_at = now()
RETURNING current_value, max_value;

-- ---------------------------------------------------------------------------
-- Shipments
-- ---------------------------------------------------------------------------

-- name: CreateShipment :one
INSERT INTO shipments (
    public_id, organization_id, awb, reference_number, customer_id, courier_service_id,
    booked_by_user_id, booking_unit_id, payment_mode, current_status, status_changed_at,
    event_sequence, origin_branch_id, origin_hub_id, destination_hub_id, destination_branch_id,
    route_definition_id, origin_pincode, destination_pincode, is_remote_origin, is_remote_destination,
    current_custody_unit_id, piece_count, actual_weight_grams, volumetric_weight_grams,
    chargeable_weight_grams, currency, declared_value_minor, cod_amount_minor, insurance_required,
    total_amount_minor, sla_hours, promised_delivery_at, booked_at,
    content_description, special_instructions, is_fragile, is_dangerous_goods, metadata
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,'BOOKED',now(),1,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
    $20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36
)
RETURNING *;

-- name: CreateShipmentPackage :one
INSERT INTO shipment_packages (
    public_id, organization_id, shipment_id, sequence, piece_barcode, reference,
    length_mm, width_mm, height_mm, actual_weight_grams, volumetric_weight_grams,
    chargeable_weight_grams, content_description, declared_value_minor
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: CreateAddressSnapshot :one
INSERT INTO shipment_address_snapshots (
    organization_id, shipment_id, role, source_address_id, contact_name, company_name,
    phone, alt_phone, email, line1, line2, landmark, city_name, state_name,
    pincode, country_code, latitude, longitude
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
RETURNING *;

-- name: CreateChargeSnapshot :one
INSERT INTO shipment_charge_snapshots (
    organization_id, shipment_id, rate_card_id, rate_card_version_id, rate_card_code,
    rate_card_version_no, pricing_engine_version, currency, origin_zone_code, destination_zone_code,
    chargeable_weight_grams, freight_minor, surcharge_total_minor, discount_total_minor,
    taxable_minor, tax_total_minor, rounding_minor, total_minor, line_items, calculation_inputs
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
RETURNING *;

-- name: CreateRouteSnapshot :one
INSERT INTO shipment_route_snapshots (
    organization_id, shipment_id, route_definition_id, route_code, resolution_source,
    origin_branch_code, origin_hub_code, destination_hub_code, destination_branch_code,
    legs, transit_hours, sla_hours, promised_delivery_at, explanation, explanation_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;

-- AppendShipmentEvent writes one row of append-only operational history.
--
-- The sequence is supplied by the caller from shipments.event_sequence, which is
-- bumped inside the same transaction; UNIQUE(shipment_id, sequence) turns any
-- concurrent double-append into a constraint violation rather than a silently
-- duplicated history entry.
-- name: AppendShipmentEvent :one
INSERT INTO shipment_events (
    public_id, organization_id, shipment_id, sequence, event_type, from_status, to_status,
    occurred_at, actor_user_id, actor_type, operating_unit_id, location_pincode, location_name,
    description, internal_remarks, reason_code, request_id, idempotency_key, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8, now()),$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
RETURNING *;

-- name: GetShipmentByPublicID :one
SELECT s.*,
       c.public_id AS customer_public_id, c.code AS customer_code, c.name AS customer_name,
       c.customer_type,
       sv.public_id AS service_public_id, sv.code AS service_code, sv.name AS service_name, sv.mode AS service_mode,
       ob.public_id AS origin_branch_public_id, ob.code AS origin_branch_code, ob.name AS origin_branch_name,
       db.public_id AS destination_branch_public_id, db.code AS destination_branch_code, db.name AS destination_branch_name,
       oh.code AS origin_hub_code, dh.code AS destination_hub_code,
       bu.public_id AS booking_unit_public_id, bu.code AS booking_unit_code,
       u.public_id AS booked_by_public_id, u.full_name AS booked_by_name
FROM shipments s
JOIN customers c ON c.id = s.customer_id
JOIN courier_services sv ON sv.id = s.courier_service_id
LEFT JOIN operating_units ob ON ob.id = s.origin_branch_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
LEFT JOIN operating_units oh ON oh.id = s.origin_hub_id
LEFT JOIN operating_units dh ON dh.id = s.destination_hub_id
LEFT JOIN operating_units bu ON bu.id = s.booking_unit_id
LEFT JOIN users u ON u.id = s.booked_by_user_id
WHERE s.public_id = sqlc.arg('public_id') AND s.organization_id = sqlc.arg('organization_id');

-- name: GetShipmentByAWB :one
SELECT s.* FROM shipments s
WHERE s.awb = sqlc.arg('awb') AND s.organization_id = sqlc.arg('organization_id');

-- LockShipmentForUpdate takes a row lock before a state transition, so two
-- concurrent transitions on the same shipment serialise instead of racing.
-- name: LockShipmentForUpdate :one
SELECT * FROM shipments
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- ApplyShipmentTransition moves a shipment to a new state and reserves the next
-- event sequence in the same statement.
--
-- `current_status = expected_status` makes the update a compare-and-swap: a
-- caller that read a stale state updates zero rows and is told to retry, rather
-- than overwriting a transition another actor already made.
-- name: ApplyShipmentTransition :one
UPDATE shipments
SET current_status = sqlc.arg('to_status'),
    status_changed_at = now(),
    event_sequence = event_sequence + 1,
    current_custody_unit_id = COALESCE(sqlc.narg('custody_unit_id'), current_custody_unit_id),
    current_custody_user_id = COALESCE(sqlc.narg('custody_user_id'), current_custody_user_id),
    cancelled_at = CASE WHEN sqlc.arg('to_status')::text = 'CANCELLED' THEN now() ELSE cancelled_at END,
    cancelled_by = CASE WHEN sqlc.arg('to_status')::text = 'CANCELLED'
                        THEN sqlc.narg('actor_user_id') ELSE cancelled_by END,
    cancellation_reason = CASE WHEN sqlc.arg('to_status')::text = 'CANCELLED'
                               THEN sqlc.narg('reason') ELSE cancellation_reason END
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
  AND current_status = sqlc.arg('expected_status')
RETURNING *;

-- ListShipments is the operations console's primary query.
--
-- It is keyset paginated on (created_at, id): the cursor predicate keeps the
-- plan on shipments_org_created_idx no matter how deep the caller pages, unlike
-- OFFSET which degrades linearly.
-- name: ListShipments :many
SELECT s.id, s.public_id, s.awb, s.reference_number, s.current_status, s.status_changed_at,
       s.payment_mode, s.piece_count, s.chargeable_weight_grams, s.currency,
       s.total_amount_minor, s.cod_amount_minor, s.origin_pincode, s.destination_pincode,
       s.promised_delivery_at, s.booked_at, s.created_at,
       c.public_id AS customer_public_id, c.code AS customer_code, c.name AS customer_name,
       sv.code AS service_code, sv.name AS service_name,
       ob.code AS origin_branch_code, db.code AS destination_branch_code,
       COALESCE(rac.contact_name, ras.contact_name) AS recipient_name,
       COALESCE(rac.city_name, ras.city_name) AS recipient_city
FROM shipments s
JOIN customers c ON c.id = s.customer_id
JOIN courier_services sv ON sv.id = s.courier_service_id
LEFT JOIN operating_units ob ON ob.id = s.origin_branch_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
LEFT JOIN shipment_address_snapshots ras ON ras.shipment_id = s.id AND ras.role = 'RECIPIENT'
LEFT JOIN LATERAL (
    SELECT contact_name, city_name
    FROM shipment_address_corrections
    WHERE shipment_id = s.id AND role = 'RECIPIENT'
    ORDER BY sequence DESC LIMIT 1
) rac ON true
WHERE s.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('statuses')::text[] IS NULL OR s.current_status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('customer_id')::bigint IS NULL OR s.customer_id = sqlc.narg('customer_id'))
  AND (sqlc.narg('courier_service_id')::bigint IS NULL OR s.courier_service_id = sqlc.narg('courier_service_id'))
  AND (sqlc.narg('payment_mode')::text IS NULL OR s.payment_mode = sqlc.narg('payment_mode'))
  AND (sqlc.narg('booked_from')::timestamptz IS NULL OR s.created_at >= sqlc.narg('booked_from'))
  AND (sqlc.narg('booked_to')::timestamptz IS NULL OR s.created_at < sqlc.narg('booked_to'))
  AND (sqlc.narg('origin_pincode')::text IS NULL OR s.origin_pincode = sqlc.narg('origin_pincode'))
  AND (sqlc.narg('destination_pincode')::text IS NULL OR s.destination_pincode = sqlc.narg('destination_pincode'))
  AND (sqlc.narg('search')::text IS NULL
       OR s.awb LIKE upper(sqlc.narg('search')) || '%'
       OR s.reference_number = sqlc.narg('search'))
  AND (sqlc.narg('scoped_unit_ids')::bigint[] IS NULL
       OR s.origin_branch_id = ANY(sqlc.narg('scoped_unit_ids')::bigint[])
       OR s.destination_branch_id = ANY(sqlc.narg('scoped_unit_ids')::bigint[])
       OR s.booking_unit_id = ANY(sqlc.narg('scoped_unit_ids')::bigint[]))
  AND (sqlc.narg('customer_ids')::bigint[] IS NULL OR s.customer_id = ANY(sqlc.narg('customer_ids')::bigint[]))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (s.created_at, s.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.arg('cursor_id')::bigint))
ORDER BY s.created_at DESC, s.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListShipmentPackages :many
SELECT * FROM shipment_packages WHERE shipment_id = $1 ORDER BY sequence;

-- name: ListShipmentAddressSnapshots :many
SELECT * FROM shipment_address_snapshots WHERE shipment_id = $1 ORDER BY role;

-- name: GetShipmentChargeSnapshot :one
SELECT * FROM shipment_charge_snapshots WHERE shipment_id = $1;

-- name: GetShipmentRouteSnapshot :one
SELECT * FROM shipment_route_snapshots WHERE shipment_id = $1;

-- name: ListShipmentEvents :many
SELECT e.*, u.full_name AS actor_name, ou.code AS operating_unit_code, ou.name AS operating_unit_name
FROM shipment_events e
LEFT JOIN users u ON u.id = e.actor_user_id
LEFT JOIN operating_units ou ON ou.id = e.operating_unit_id
WHERE e.shipment_id = sqlc.arg('shipment_id')
ORDER BY e.sequence
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- GetShipmentLabelData assembles everything a label needs in one round trip:
-- the shipment, both address snapshots and the resolved routing codes.
-- name: GetShipmentLabelData :one
SELECT s.public_id, s.awb, s.reference_number, s.current_status, s.payment_mode,
       s.piece_count, s.actual_weight_grams, s.chargeable_weight_grams, s.currency,
       s.cod_amount_minor, s.declared_value_minor, s.total_amount_minor,
       s.origin_pincode, s.destination_pincode, s.booked_at, s.promised_delivery_at,
       s.content_description, s.special_instructions, s.is_fragile,
       sv.code AS service_code, sv.name AS service_name, sv.mode AS service_mode,
       ob.code AS origin_branch_code, ob.name AS origin_branch_name,
       db.code AS destination_branch_code, db.name AS destination_branch_name,
       oh.code AS origin_hub_code, dh.code AS destination_hub_code,
       c.name AS customer_name, c.code AS customer_code,
       snd.contact_name AS sender_name, snd.company_name AS sender_company, snd.phone AS sender_phone,
       snd.line1 AS sender_line1, snd.line2 AS sender_line2, snd.city_name AS sender_city,
       snd.state_name AS sender_state, snd.pincode AS sender_pincode,
       rcp.contact_name AS recipient_name, rcp.company_name AS recipient_company, rcp.phone AS recipient_phone,
       rcp.line1 AS recipient_line1, rcp.line2 AS recipient_line2, rcp.landmark AS recipient_landmark,
       rcp.city_name AS recipient_city, rcp.state_name AS recipient_state, rcp.pincode AS recipient_pincode,
       COALESCE(sndc.id, 0) AS sender_correction_id,
       COALESCE(sndc.contact_name, '') AS corrected_sender_name,
       sndc.company_name AS corrected_sender_company,
       COALESCE(sndc.phone, '') AS corrected_sender_phone,
       COALESCE(sndc.line1, '') AS corrected_sender_line1,
       sndc.line2 AS corrected_sender_line2,
       COALESCE(rcpc.id, 0) AS recipient_correction_id,
       COALESCE(rcpc.contact_name, '') AS corrected_recipient_name,
       rcpc.company_name AS corrected_recipient_company,
       COALESCE(rcpc.phone, '') AS corrected_recipient_phone,
       COALESCE(rcpc.line1, '') AS corrected_recipient_line1,
       rcpc.line2 AS corrected_recipient_line2,
       rcpc.landmark AS corrected_recipient_landmark
FROM shipments s
JOIN courier_services sv ON sv.id = s.courier_service_id
JOIN customers c ON c.id = s.customer_id
LEFT JOIN operating_units ob ON ob.id = s.origin_branch_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
LEFT JOIN operating_units oh ON oh.id = s.origin_hub_id
LEFT JOIN operating_units dh ON dh.id = s.destination_hub_id
LEFT JOIN shipment_address_snapshots snd ON snd.shipment_id = s.id AND snd.role = 'SENDER'
LEFT JOIN shipment_address_snapshots rcp ON rcp.shipment_id = s.id AND rcp.role = 'RECIPIENT'
LEFT JOIN LATERAL (
    SELECT * FROM shipment_address_corrections
    WHERE shipment_id = s.id AND role = 'SENDER'
    ORDER BY sequence DESC LIMIT 1
) sndc ON true
LEFT JOIN LATERAL (
    SELECT * FROM shipment_address_corrections
    WHERE shipment_id = s.id AND role = 'RECIPIENT'
    ORDER BY sequence DESC LIMIT 1
) rcpc ON true
WHERE s.public_id = sqlc.arg('public_id') AND s.organization_id = sqlc.arg('organization_id');

-- name: CountShipmentsByStatus :many
SELECT current_status, count(*) AS total
FROM shipments
WHERE organization_id = sqlc.arg('organization_id')
  AND created_at >= sqlc.arg('booked_from')
GROUP BY current_status
ORDER BY current_status;

-- name: GetShipmentByID :one
-- Lookup by internal id, for modules that already hold a foreign key rather
-- than a public id — the finance services reach shipments this way from
-- obligations, commission calculations and invoice lines.
SELECT * FROM shipments WHERE organization_id = $1 AND id = $2;
