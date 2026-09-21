-- name: CorrectBookedShipment :one
UPDATE shipments
SET reference_number = sqlc.narg('reference_number'),
    content_description = sqlc.arg('content_description'),
    special_instructions = sqlc.narg('special_instructions'),
    is_fragile = sqlc.arg('is_fragile'),
    event_sequence = event_sequence + 1
WHERE id = sqlc.arg('id')
  AND organization_id = sqlc.arg('organization_id')
  AND current_status = 'BOOKED'
  AND version = sqlc.arg('expected_version')
RETURNING *;

-- name: NextShipmentAddressCorrectionSequence :one
SELECT COALESCE(MAX(sequence), 0)::integer + 1
FROM shipment_address_corrections
WHERE shipment_id = sqlc.arg('shipment_id') AND role = sqlc.arg('role');

-- name: CreateShipmentAddressCorrection :one
INSERT INTO shipment_address_corrections (
    public_id, organization_id, shipment_id, role, sequence,
    contact_name, company_name, phone, alt_phone, email, line1, line2, landmark,
    city_name, state_name, pincode, country_code, reason,
    corrected_by_user_id, request_id
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('shipment_id'),
    sqlc.arg('role'), sqlc.arg('sequence'), sqlc.arg('contact_name'),
    sqlc.narg('company_name'), sqlc.arg('phone'), sqlc.narg('alt_phone'),
    sqlc.narg('email'), sqlc.arg('line1'), sqlc.narg('line2'),
    sqlc.narg('landmark'), sqlc.arg('city_name'), sqlc.arg('state_name'),
    sqlc.arg('pincode'), sqlc.arg('country_code'), sqlc.arg('reason'),
    sqlc.narg('corrected_by_user_id'), sqlc.narg('request_id')
)
RETURNING *;

-- name: ListLatestShipmentAddressCorrections :many
SELECT DISTINCT ON (role) *
FROM shipment_address_corrections
WHERE shipment_id = sqlc.arg('shipment_id')
ORDER BY role, sequence DESC;
