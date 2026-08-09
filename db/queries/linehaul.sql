-- Line haul (M13).

-- name: CreateCarrier :one
INSERT INTO carriers (
    public_id, organization_id, code, name, carrier_type, modes,
    contact_name, contact_phone, contact_email, gst_number, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetCarrierByPublicID :one
SELECT * FROM carriers
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id');

-- name: ListCarriers :many
SELECT * FROM carriers
WHERE organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('is_active')::boolean IS NULL OR is_active = sqlc.narg('is_active'))
  AND (sqlc.narg('carrier_type')::text IS NULL OR carrier_type = sqlc.narg('carrier_type'))
ORDER BY name, id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('row_offset');

-- name: UpdateCarrier :one
UPDATE carriers
SET name = COALESCE(sqlc.narg('name'), name),
    carrier_type = COALESCE(sqlc.narg('carrier_type'), carrier_type),
    modes = COALESCE(sqlc.narg('modes'), modes),
    contact_name = COALESCE(sqlc.narg('contact_name'), contact_name),
    contact_phone = COALESCE(sqlc.narg('contact_phone'), contact_phone),
    contact_email = COALESCE(sqlc.narg('contact_email'), contact_email),
    gst_number = COALESCE(sqlc.narg('gst_number'), gst_number),
    is_active = COALESCE(sqlc.narg('is_active'), is_active)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: CreateVehicle :one
INSERT INTO vehicles (
    public_id, organization_id, carrier_id, registration_number, vehicle_type,
    capacity_weight_grams, capacity_volume_cc, base_unit_id,
    insurance_expiry, fitness_expiry, permit_expiry, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: GetVehicleByPublicID :one
SELECT v.*, c.code AS carrier_code, c.name AS carrier_name, ou.code AS base_unit_code
FROM vehicles v
LEFT JOIN carriers c ON c.id = v.carrier_id
LEFT JOIN operating_units ou ON ou.id = v.base_unit_id
WHERE v.public_id = sqlc.arg('public_id') AND v.organization_id = sqlc.arg('organization_id');

-- name: ListVehicles :many
SELECT v.*, c.code AS carrier_code, c.name AS carrier_name
FROM vehicles v
LEFT JOIN carriers c ON c.id = v.carrier_id
WHERE v.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('is_active')::boolean IS NULL OR v.is_active = sqlc.narg('is_active'))
  AND (sqlc.narg('carrier_id')::bigint IS NULL OR v.carrier_id = sqlc.narg('carrier_id'))
  AND (sqlc.narg('vehicle_type')::text IS NULL OR v.vehicle_type = sqlc.narg('vehicle_type'))
ORDER BY v.registration_number, v.id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('row_offset');

-- name: UpdateVehicle :one
UPDATE vehicles
SET vehicle_type = COALESCE(sqlc.narg('vehicle_type'), vehicle_type),
    carrier_id = COALESCE(sqlc.narg('carrier_id'), carrier_id),
    capacity_weight_grams = COALESCE(sqlc.narg('capacity_weight_grams'), capacity_weight_grams),
    capacity_volume_cc = COALESCE(sqlc.narg('capacity_volume_cc'), capacity_volume_cc),
    base_unit_id = COALESCE(sqlc.narg('base_unit_id'), base_unit_id),
    insurance_expiry = COALESCE(sqlc.narg('insurance_expiry'), insurance_expiry),
    fitness_expiry = COALESCE(sqlc.narg('fitness_expiry'), fitness_expiry),
    permit_expiry = COALESCE(sqlc.narg('permit_expiry'), permit_expiry),
    is_active = COALESCE(sqlc.narg('is_active'), is_active)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: CreateDriver :one
INSERT INTO drivers (
    public_id, organization_id, carrier_id, user_id, code, full_name, phone,
    licence_number, licence_expiry, base_unit_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetDriverByPublicID :one
SELECT d.*, c.code AS carrier_code, c.name AS carrier_name, ou.code AS base_unit_code
FROM drivers d
LEFT JOIN carriers c ON c.id = d.carrier_id
LEFT JOIN operating_units ou ON ou.id = d.base_unit_id
WHERE d.public_id = sqlc.arg('public_id') AND d.organization_id = sqlc.arg('organization_id');

-- name: ListDrivers :many
SELECT d.*, c.code AS carrier_code, c.name AS carrier_name
FROM drivers d
LEFT JOIN carriers c ON c.id = d.carrier_id
WHERE d.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('is_active')::boolean IS NULL OR d.is_active = sqlc.narg('is_active'))
  AND (sqlc.narg('carrier_id')::bigint IS NULL OR d.carrier_id = sqlc.narg('carrier_id'))
ORDER BY d.full_name, d.id
LIMIT sqlc.arg('page_size') OFFSET sqlc.arg('row_offset');

-- name: UpdateDriver :one
UPDATE drivers
SET full_name = COALESCE(sqlc.narg('full_name'), full_name),
    phone = COALESCE(sqlc.narg('phone'), phone),
    carrier_id = COALESCE(sqlc.narg('carrier_id'), carrier_id),
    user_id = COALESCE(sqlc.narg('user_id'), user_id),
    licence_number = COALESCE(sqlc.narg('licence_number'), licence_number),
    licence_expiry = COALESCE(sqlc.narg('licence_expiry'), licence_expiry),
    base_unit_id = COALESCE(sqlc.narg('base_unit_id'), base_unit_id),
    is_active = COALESCE(sqlc.narg('is_active'), is_active)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Trips
-- ---------------------------------------------------------------------------

-- name: CreateTrip :one
INSERT INTO trips (
    public_id, organization_id, trip_code, mode, origin_unit_id, destination_unit_id,
    route_definition_id, direction, carrier_id, vehicle_id, primary_driver_id,
    external_reference, scheduled_departure, scheduled_arrival, leg_count,
    created_by_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
RETURNING *;

-- name: GetTripByPublicID :one
SELECT t.*,
       o.public_id AS origin_public_id, o.code AS origin_code, o.name AS origin_name,
       d.public_id AS destination_public_id, d.code AS destination_code, d.name AS destination_name,
       c.code AS carrier_code, c.name AS carrier_name,
       v.public_id AS vehicle_public_id, v.registration_number,
       dr.public_id AS driver_public_id, dr.full_name AS driver_name, dr.phone AS driver_phone
FROM trips t
JOIN operating_units o ON o.id = t.origin_unit_id
JOIN operating_units d ON d.id = t.destination_unit_id
LEFT JOIN carriers c ON c.id = t.carrier_id
LEFT JOIN vehicles v ON v.id = t.vehicle_id
LEFT JOIN drivers dr ON dr.id = t.primary_driver_id
WHERE t.public_id = sqlc.arg('public_id') AND t.organization_id = sqlc.arg('organization_id');

-- name: LockTripForUpdate :one
SELECT * FROM trips
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: ListTrips :many
SELECT t.id, t.public_id, t.trip_code, t.mode, t.status, t.direction,
       t.scheduled_departure, t.scheduled_arrival, t.actual_departure, t.actual_arrival,
       t.manifest_count, t.bag_count, t.shipment_count, t.total_weight_grams,
       t.external_reference, t.leg_count, t.current_leg_sequence, t.created_at,
       o.public_id AS origin_public_id, o.code AS origin_code, o.name AS origin_name,
       d.public_id AS destination_public_id, d.code AS destination_code, d.name AS destination_name,
       v.registration_number, dr.full_name AS driver_name, c.name AS carrier_name
FROM trips t
JOIN operating_units o ON o.id = t.origin_unit_id
JOIN operating_units d ON d.id = t.destination_unit_id
LEFT JOIN vehicles v ON v.id = t.vehicle_id
LEFT JOIN drivers dr ON dr.id = t.primary_driver_id
LEFT JOIN carriers c ON c.id = t.carrier_id
WHERE t.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status'))
  AND (sqlc.narg('mode')::text IS NULL OR t.mode = sqlc.narg('mode'))
  AND (sqlc.narg('origin_unit_id')::bigint IS NULL OR t.origin_unit_id = sqlc.narg('origin_unit_id'))
  AND (sqlc.narg('destination_unit_id')::bigint IS NULL OR t.destination_unit_id = sqlc.narg('destination_unit_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL
       OR t.origin_unit_id = ANY(sqlc.narg('unit_ids')) OR t.destination_unit_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (t.created_at, t.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY t.created_at DESC, t.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdateTripStatus :one
-- Compare-and-swap, so a second dispatcher pressing "depart" gets a conflict
-- rather than a second departure event.
UPDATE trips
SET status = sqlc.arg('to_status'),
    actual_departure = CASE WHEN sqlc.arg('to_status')::text = 'DEPARTED'
                            THEN COALESCE(sqlc.narg('occurred_at')::timestamptz, now()) ELSE actual_departure END,
    actual_arrival = CASE WHEN sqlc.arg('to_status')::text = 'ARRIVED'
                          THEN COALESCE(sqlc.narg('occurred_at')::timestamptz, now()) ELSE actual_arrival END,
    closed_at = CASE WHEN sqlc.arg('to_status')::text = 'CLOSED' THEN now() ELSE closed_at END,
    cancelled_at = CASE WHEN sqlc.arg('to_status')::text = 'CANCELLED' THEN now() ELSE cancelled_at END,
    cancellation_reason = COALESCE(sqlc.narg('cancellation_reason'), cancellation_reason),
    departure_odometer_km = COALESCE(sqlc.narg('departure_odometer_km'), departure_odometer_km),
    arrival_odometer_km = COALESCE(sqlc.narg('arrival_odometer_km'), arrival_odometer_km),
    seal_number = COALESCE(sqlc.narg('seal_number'), seal_number),
    current_leg_sequence = COALESCE(sqlc.narg('current_leg_sequence'), current_leg_sequence),
    event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: UpdateTripAssets :one
UPDATE trips
SET vehicle_id = COALESCE(sqlc.narg('vehicle_id'), vehicle_id),
    primary_driver_id = COALESCE(sqlc.narg('primary_driver_id'), primary_driver_id),
    carrier_id = COALESCE(sqlc.narg('carrier_id'), carrier_id),
    external_reference = COALESCE(sqlc.narg('external_reference'), external_reference),
    scheduled_departure = COALESCE(sqlc.narg('scheduled_departure'), scheduled_departure),
    scheduled_arrival = COALESCE(sqlc.narg('scheduled_arrival'), scheduled_arrival)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status IN ('PLANNED','LOADING')
RETURNING *;

-- name: RecalculateTripCounters :one
UPDATE trips t
SET manifest_count = c.manifests, bag_count = c.bags,
    shipment_count = c.shipments, total_weight_grams = c.weight
FROM (
    SELECT count(*) AS manifests,
           COALESCE(sum(bag_count), 0)::int AS bags,
           COALESCE(sum(total_shipment_count), 0)::int AS shipments,
           COALESCE(sum(total_weight_grams), 0)::bigint AS weight
    FROM manifests
    WHERE trip_id = sqlc.arg('trip_id') AND status <> 'CANCELLED'
) c
WHERE t.id = sqlc.arg('trip_id')
RETURNING t.*;

-- name: CreateTripLeg :one
INSERT INTO trip_legs (
    public_id, organization_id, trip_id, sequence, origin_unit_id, destination_unit_id,
    scheduled_departure, scheduled_arrival, distance_km
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: ListTripLegs :many
SELECT tl.*, o.code AS origin_code, o.name AS origin_name,
       d.code AS destination_code, d.name AS destination_name,
       o.public_id AS origin_public_id, d.public_id AS destination_public_id
FROM trip_legs tl
JOIN operating_units o ON o.id = tl.origin_unit_id
JOIN operating_units d ON d.id = tl.destination_unit_id
WHERE tl.trip_id = sqlc.arg('trip_id')
ORDER BY tl.sequence;

-- name: GetTripLegBySequence :one
SELECT * FROM trip_legs
WHERE trip_id = sqlc.arg('trip_id') AND sequence = sqlc.arg('sequence')
FOR UPDATE;

-- name: UpdateTripLegStatus :one
UPDATE trip_legs
SET status = sqlc.arg('to_status'),
    actual_departure = CASE WHEN sqlc.arg('to_status')::text = 'DEPARTED'
                            THEN COALESCE(sqlc.narg('occurred_at')::timestamptz, now()) ELSE actual_departure END,
    actual_arrival = CASE WHEN sqlc.arg('to_status')::text = 'ARRIVED'
                          THEN COALESCE(sqlc.narg('occurred_at')::timestamptz, now()) ELSE actual_arrival END,
    remarks = COALESCE(sqlc.narg('remarks'), remarks)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: CreateTripAssignment :one
INSERT INTO trip_assignments (
    public_id, organization_id, trip_id, trip_leg_id, role, driver_id, user_id,
    assigned_by_user_id, remarks
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: ListTripAssignments :many
SELECT ta.*, d.full_name AS driver_name, d.phone AS driver_phone, d.public_id AS driver_public_id,
       u.full_name AS user_name, u.public_id AS user_public_id
FROM trip_assignments ta
LEFT JOIN drivers d ON d.id = ta.driver_id
LEFT JOIN users u ON u.id = ta.user_id
WHERE ta.trip_id = sqlc.arg('trip_id') AND ta.status IN ('ASSIGNED','ACCEPTED')
ORDER BY ta.role, ta.id;

-- name: ReleaseTripAssignment :one
UPDATE trip_assignments SET status = 'RELEASED', released_at = now()
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: ListTripManifests :many
SELECT m.id, m.public_id, m.manifest_code, m.status, m.bag_count, m.total_shipment_count,
       m.total_piece_count, m.total_weight_grams, m.trip_leg_id,
       o.code AS origin_code, d.code AS destination_code, d.id AS destination_unit_id
FROM manifests m
JOIN operating_units o ON o.id = m.origin_unit_id
JOIN operating_units d ON d.id = m.destination_unit_id
WHERE m.trip_id = sqlc.arg('trip_id') AND m.status <> 'CANCELLED'
ORDER BY m.id;

-- name: CountOpenTripManifests :one
-- Departure validation: every manifest on the trip must be CLOSED (§13).
SELECT count(*) FROM manifests
WHERE trip_id = sqlc.arg('trip_id') AND status = 'DRAFT';

-- name: AppendTripEvent :one
INSERT INTO trip_events (
    public_id, organization_id, trip_id, trip_leg_id, sequence, event_type,
    from_status, to_status, occurred_at, actor_user_id, operating_unit_id,
    latitude, longitude, description, reason_code, request_id, metadata
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('trip_id'),
    sqlc.arg('trip_leg_id'), sqlc.arg('sequence'), sqlc.arg('event_type'),
    sqlc.arg('from_status'), sqlc.arg('to_status'),
    COALESCE(sqlc.narg('occurred_at')::timestamptz, now()), sqlc.arg('actor_user_id'),
    sqlc.arg('operating_unit_id'), sqlc.arg('latitude'), sqlc.arg('longitude'),
    sqlc.arg('description'), sqlc.arg('reason_code'), sqlc.arg('request_id'),
    sqlc.arg('metadata')
)
RETURNING *;

-- name: ListTripEvents :many
SELECT te.*, u.full_name AS actor_name, ou.code AS unit_code
FROM trip_events te
LEFT JOIN users u ON u.id = te.actor_user_id
LEFT JOIN operating_units ou ON ou.id = te.operating_unit_id
WHERE te.trip_id = sqlc.arg('trip_id')
ORDER BY te.sequence DESC
LIMIT sqlc.arg('page_size');

-- name: BumpTripEventSequence :one
UPDATE trips SET event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING id, event_sequence, status;

-- name: ListInboundTrips :many
SELECT t.public_id, t.trip_code, t.mode, t.status, t.scheduled_arrival, t.actual_departure,
       t.manifest_count, t.bag_count, t.shipment_count,
       o.code AS origin_code, o.name AS origin_name,
       v.registration_number, dr.full_name AS driver_name, dr.phone AS driver_phone
FROM trips t
JOIN operating_units o ON o.id = t.origin_unit_id
LEFT JOIN vehicles v ON v.id = t.vehicle_id
LEFT JOIN drivers dr ON dr.id = t.primary_driver_id
WHERE t.organization_id = sqlc.arg('organization_id')
  AND t.destination_unit_id = sqlc.arg('destination_unit_id')
  AND t.status IN ('DEPARTED','IN_TRANSIT')
ORDER BY t.scheduled_arrival, t.id
LIMIT sqlc.arg('page_size');
