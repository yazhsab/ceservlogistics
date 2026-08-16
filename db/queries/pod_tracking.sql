-- Proof of delivery (M19), object storage and public tracking (M20).

-- name: CreateStoredObject :one
INSERT INTO stored_objects (
    public_id, organization_id, object_key, bucket, purpose, mime_type, size_bytes,
    checksum_sha256, original_filename, uploaded_by_user_id, retention_until, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: GetStoredObjectByPublicID :one
SELECT * FROM stored_objects
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
  AND deleted_at IS NULL;

-- name: GetStoredObjectByChecksum :one
-- Re-uploading identical bytes reuses the stored object instead of paying for
-- a second copy. Scoped to the tenant so no cross-tenant inference is possible.
SELECT * FROM stored_objects
WHERE organization_id = sqlc.arg('organization_id')
  AND checksum_sha256 = sqlc.arg('checksum_sha256')
  AND purpose = sqlc.arg('purpose')
  AND deleted_at IS NULL
LIMIT 1;

-- name: SoftDeleteStoredObject :one
UPDATE stored_objects SET deleted_at = now()
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: RecordObjectAccess :exec
INSERT INTO object_access_log (
    organization_id, stored_object_id, accessed_by_user_id, access_type,
    request_id, ip_address, expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7);

-- name: ListExpiredObjects :many
SELECT id, public_id, bucket, object_key FROM stored_objects
WHERE retention_until IS NOT NULL AND retention_until < now() AND deleted_at IS NULL
ORDER BY retention_until
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Proof of delivery
-- ---------------------------------------------------------------------------

-- name: CreateProofOfDelivery :one
INSERT INTO proof_of_delivery (
    public_id, organization_id, shipment_id, delivery_attempt_id, pod_type,
    recipient_name, recipient_relationship, recipient_phone,
    recipient_id_type, recipient_id_masked,
    otp_verified, signature_captured, photo_captured,
    delivered_at, delivered_by_user_id, operating_unit_id,
    latitude, longitude, location_accuracy_m, device_id, device_model, remarks, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
RETURNING *;

-- name: GetProofOfDelivery :one
SELECT p.*, s.public_id AS shipment_public_id, s.awb, s.current_status,
       s.customer_id, s.destination_branch_id, s.origin_branch_id, s.booking_unit_id,
       u.public_id AS delivered_by_public_id, u.full_name AS delivered_by_name,
       ou.code AS unit_code, ou.name AS unit_name
FROM proof_of_delivery p
JOIN shipments s ON s.id = p.shipment_id
LEFT JOIN users u ON u.id = p.delivered_by_user_id
LEFT JOIN operating_units ou ON ou.id = p.operating_unit_id
WHERE p.public_id = sqlc.arg('public_id') AND p.organization_id = sqlc.arg('organization_id');

-- name: GetProofOfDeliveryForShipment :one
SELECT p.*, u.full_name AS delivered_by_name, ou.code AS unit_code
FROM proof_of_delivery p
LEFT JOIN users u ON u.id = p.delivered_by_user_id
LEFT JOIN operating_units ou ON ou.id = p.operating_unit_id
WHERE p.shipment_id = sqlc.arg('shipment_id') AND p.pod_type = sqlc.arg('pod_type');

-- name: CreatePODArtifact :one
INSERT INTO pod_artifacts (
    public_id, organization_id, proof_of_delivery_id, stored_object_id,
    artifact_type, sequence, caption, captured_at
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('proof_of_delivery_id'),
    sqlc.arg('stored_object_id'), sqlc.arg('artifact_type'), sqlc.arg('sequence'),
    sqlc.arg('caption'), COALESCE(sqlc.narg('captured_at')::timestamptz, now())
)
RETURNING *;

-- name: ListPODArtifacts :many
SELECT pa.public_id, pa.artifact_type, pa.sequence, pa.caption, pa.captured_at,
       so.id AS object_id, so.public_id AS object_public_id, so.mime_type, so.size_bytes,
       so.checksum_sha256, so.object_key, so.bucket
FROM pod_artifacts pa
JOIN stored_objects so ON so.id = pa.stored_object_id
WHERE pa.proof_of_delivery_id = sqlc.arg('proof_of_delivery_id')
  AND so.deleted_at IS NULL
ORDER BY pa.artifact_type, pa.sequence;

-- name: NextPODArtifactSequence :one
SELECT COALESCE(max(sequence), 0)::int + 1 AS next_sequence FROM pod_artifacts
WHERE proof_of_delivery_id = sqlc.arg('proof_of_delivery_id')
  AND artifact_type = sqlc.arg('artifact_type');

-- ---------------------------------------------------------------------------
-- Public tracking (M20)
-- ---------------------------------------------------------------------------

-- name: GetPublicTrackingShipment :one
-- Deliberately narrow. No internal public ids, no customer identity, no
-- financial detail beyond the COD amount the recipient must have ready, no
-- operating unit ids. Everything selected here is safe to show anonymously.
SELECT s.awb,
       s.current_status,
       s.status_changed_at,
       s.piece_count,
       s.promised_delivery_at,
       s.booked_at,
       s.delivered_at,
       s.movement_direction,
       s.cod_amount_minor,
       s.currency,
       s.payment_mode,
       s.route_definition_id,
       sv.name AS service_name,
       origin_unit.name AS origin_unit_name,
       dest_unit.name AS destination_unit_name,
       -- City-level only: a street address must not be readable from an AWB.
       origin_city.name AS origin_city,
       dest_city.name AS destination_city,
       s.origin_pincode,
       s.destination_pincode,
       s.id AS shipment_id,
       s.organization_id
FROM shipments s
JOIN courier_services sv ON sv.id = s.courier_service_id
LEFT JOIN operating_units origin_unit ON origin_unit.id = s.origin_branch_id
LEFT JOIN operating_units dest_unit ON dest_unit.id = s.destination_branch_id
-- City names come through the PIN code, which is keyed by country. The
-- platform is single-country per deployment (§M03), so the join resolves the
-- country by its ISO code rather than assuming an id.
LEFT JOIN countries ctry ON ctry.iso2 = 'NG'
LEFT JOIN pincodes op ON op.code = s.origin_pincode AND op.country_id = ctry.id
LEFT JOIN cities origin_city ON origin_city.id = op.city_id
LEFT JOIN pincodes dp ON dp.code = s.destination_pincode AND dp.country_id = ctry.id
LEFT JOIN cities dest_city ON dest_city.id = dp.city_id
WHERE s.awb = sqlc.arg('awb');

-- name: ListPublicTrackingEvents :many
-- The customer-facing timeline. internal_remarks, actor identity and reason
-- codes are never selected; the facility name is joined but the caller decides
-- whether to show it based on the milestone's show_location flag.
SELECT e.event_type,
       e.to_status,
       e.from_status,
       e.occurred_at,
       e.description,
       COALESCE(e.location_pincode, ou.pincode) AS location_pincode,
       ou.name AS location_name,
       ou.unit_type AS location_type,
       city.name AS location_city
FROM shipment_events e
LEFT JOIN operating_units ou ON ou.id = e.operating_unit_id
LEFT JOIN countries ctry ON ctry.iso2 = 'NG'
LEFT JOIN pincodes p ON p.code = COALESCE(e.location_pincode, ou.pincode) AND p.country_id = ctry.id
LEFT JOIN cities city ON city.id = p.city_id
WHERE e.shipment_id = sqlc.arg('shipment_id')
  -- Status changes and milestones only; internal remarks never surface.
  AND e.event_type <> 'REMARK'
ORDER BY e.occurred_at ASC, e.sequence ASC
LIMIT sqlc.arg('page_size');

-- name: ListTrackingMilestones :many
-- Tenant overrides win over the platform defaults. DISTINCT ON picks the tenant
-- row when both exist, which keeps the resolution in SQL rather than in Go.
SELECT DISTINCT ON (shipment_status)
       shipment_status, milestone_code, public_title, public_description,
       show_location, display_order
FROM tracking_milestones
WHERE is_active = true
  AND (organization_id = sqlc.arg('organization_id') OR organization_id IS NULL)
ORDER BY shipment_status, organization_id NULLS LAST;

-- name: UpsertTrackingMilestone :one
INSERT INTO tracking_milestones (
    organization_id, shipment_status, milestone_code, public_title,
    public_description, show_location, display_order, is_active
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (organization_id, shipment_status)
DO UPDATE SET milestone_code = EXCLUDED.milestone_code,
              public_title = EXCLUDED.public_title,
              public_description = EXCLUDED.public_description,
              show_location = EXCLUDED.show_location,
              display_order = EXCLUDED.display_order,
              is_active = EXCLUDED.is_active
RETURNING *;

-- name: GetShipmentDeliveryEstimate :one
-- Promised date plus whatever the NDR workflow has rescheduled to, so public
-- tracking can show a realistic date rather than a stale promise.
SELECT s.promised_delivery_at,
       n.next_attempt_at AS ndr_next_attempt_at,
       n.current_action AS ndr_action
FROM shipments s
LEFT JOIN ndr_cases n ON n.shipment_id = s.id
     AND n.status IN ('OPEN','PENDING_CUSTOMER','SCHEDULED','ESCALATED')
WHERE s.id = sqlc.arg('shipment_id');
