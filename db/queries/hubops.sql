-- Hub operations (M14): exceptions, reconciliation and workload summaries.

-- name: CreateOperationalException :one
INSERT INTO operational_exceptions (
    public_id, organization_id, exception_code, exception_type, severity,
    operating_unit_id, shipment_id, bag_id, manifest_id, trip_id, reconciliation_id,
    scan_event_id, raw_barcode, description,
    expected_count, actual_count, expected_weight_grams, actual_weight_grams,
    raised_by_user_id, assigned_to_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
RETURNING *;

-- name: GetExceptionByPublicID :one
SELECT e.*,
       ou.public_id AS unit_public_id, ou.code AS unit_code, ou.name AS unit_name,
       s.public_id AS shipment_public_id, s.awb,
       b.public_id AS bag_public_id, b.bag_code,
       m.public_id AS manifest_public_id, m.manifest_code,
       ru.full_name AS raised_by_name, au.full_name AS assigned_to_name,
       vu.full_name AS resolved_by_name
FROM operational_exceptions e
JOIN operating_units ou ON ou.id = e.operating_unit_id
LEFT JOIN shipments s ON s.id = e.shipment_id
LEFT JOIN bags b ON b.id = e.bag_id
LEFT JOIN manifests m ON m.id = e.manifest_id
LEFT JOIN users ru ON ru.id = e.raised_by_user_id
LEFT JOIN users au ON au.id = e.assigned_to_user_id
LEFT JOIN users vu ON vu.id = e.resolved_by_user_id
WHERE e.public_id = sqlc.arg('public_id') AND e.organization_id = sqlc.arg('organization_id');

-- name: ListExceptions :many
SELECT e.id, e.public_id, e.exception_code, e.exception_type, e.severity, e.status,
       e.description, e.raw_barcode, e.expected_count, e.actual_count,
       e.raised_at, e.resolved_at, e.resolution_action, e.created_at,
       ou.code AS unit_code, ou.name AS unit_name,
       s.public_id AS shipment_public_id, s.awb,
       b.public_id AS bag_public_id, b.bag_code,
       ru.full_name AS raised_by_name, au.full_name AS assigned_to_name
FROM operational_exceptions e
JOIN operating_units ou ON ou.id = e.operating_unit_id
LEFT JOIN shipments s ON s.id = e.shipment_id
LEFT JOIN bags b ON b.id = e.bag_id
LEFT JOIN users ru ON ru.id = e.raised_by_user_id
LEFT JOIN users au ON au.id = e.assigned_to_user_id
WHERE e.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR e.status = sqlc.narg('status'))
  AND (sqlc.narg('exception_type')::text IS NULL OR e.exception_type = sqlc.narg('exception_type'))
  AND (sqlc.narg('severity')::text IS NULL OR e.severity = sqlc.narg('severity'))
  AND (sqlc.narg('unit_id')::bigint IS NULL OR e.operating_unit_id = sqlc.narg('unit_id'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR e.operating_unit_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('shipment_id')::bigint IS NULL OR e.shipment_id = sqlc.narg('shipment_id'))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (e.created_at, e.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg('page_size');

-- name: UpdateExceptionStatus :one
UPDATE operational_exceptions
SET status = sqlc.arg('to_status'),
    assigned_to_user_id = COALESCE(sqlc.narg('assigned_to_user_id'), assigned_to_user_id),
    resolution_action = COALESCE(sqlc.narg('resolution_action'), resolution_action),
    resolution_notes = COALESCE(sqlc.narg('resolution_notes'), resolution_notes),
    resolved_at = CASE WHEN sqlc.arg('to_status')::text IN ('RESOLVED','WRITTEN_OFF')
                       THEN now() ELSE resolved_at END,
    resolved_by_user_id = CASE WHEN sqlc.arg('to_status')::text IN ('RESOLVED','WRITTEN_OFF')
                               THEN sqlc.narg('resolved_by_user_id') ELSE resolved_by_user_id END
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = sqlc.arg('expected_status')
RETURNING *;

-- name: LockExceptionForUpdate :one
SELECT * FROM operational_exceptions
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- ---------------------------------------------------------------------------
-- Reconciliation
-- ---------------------------------------------------------------------------

-- name: CreateReconciliation :one
INSERT INTO reconciliations (
    public_id, organization_id, reconciliation_code, subject_type, bag_id, manifest_id,
    trip_id, operating_unit_id, expected_count, started_by_user_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetReconciliationByPublicID :one
SELECT r.*, ou.code AS unit_code, ou.name AS unit_name,
       b.public_id AS bag_public_id, b.bag_code,
       m.public_id AS manifest_public_id, m.manifest_code,
       su.full_name AS started_by_name, cu.full_name AS completed_by_name
FROM reconciliations r
JOIN operating_units ou ON ou.id = r.operating_unit_id
LEFT JOIN bags b ON b.id = r.bag_id
LEFT JOIN manifests m ON m.id = r.manifest_id
LEFT JOIN users su ON su.id = r.started_by_user_id
LEFT JOIN users cu ON cu.id = r.completed_by_user_id
WHERE r.public_id = sqlc.arg('public_id') AND r.organization_id = sqlc.arg('organization_id');

-- name: LockReconciliationForUpdate :one
SELECT * FROM reconciliations
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: GetActiveReconciliationForBag :one
SELECT * FROM reconciliations
WHERE bag_id = sqlc.arg('bag_id') AND status = 'IN_PROGRESS';

-- name: CreateReconciliationItem :one
INSERT INTO reconciliation_items (
    organization_id, reconciliation_id, item_type, shipment_id, bag_id, raw_barcode,
    outcome, was_declared
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: MarkReconciliationItemScanned :one
UPDATE reconciliation_items
SET outcome = sqlc.arg('outcome'), scanned_at = now(),
    scanned_by_user_id = sqlc.narg('scanned_by_user_id'),
    scan_event_id = sqlc.narg('scan_event_id'),
    remarks = COALESCE(sqlc.narg('remarks'), remarks)
WHERE reconciliation_id = sqlc.arg('reconciliation_id')
  AND raw_barcode = sqlc.arg('raw_barcode')
RETURNING *;

-- name: ListReconciliationItems :many
SELECT ri.*, s.public_id AS shipment_public_id, s.awb, s.current_status,
       b.bag_code, e.public_id AS exception_public_id, e.exception_type
FROM reconciliation_items ri
LEFT JOIN shipments s ON s.id = ri.shipment_id
LEFT JOIN bags b ON b.id = ri.bag_id
LEFT JOIN operational_exceptions e ON e.id = ri.exception_id
WHERE ri.reconciliation_id = sqlc.arg('reconciliation_id')
  AND (sqlc.narg('outcome')::text IS NULL OR ri.outcome = sqlc.narg('outcome'))
ORDER BY ri.outcome, ri.id;

-- name: RecalculateReconciliationCounters :one
UPDATE reconciliations r
SET scanned_count = c.scanned, matched_count = c.matched,
    missing_count = c.missing, excess_count = c.excess, damaged_count = c.damaged
FROM (
    SELECT count(*) FILTER (WHERE scanned_at IS NOT NULL) AS scanned,
           count(*) FILTER (WHERE outcome = 'MATCHED') AS matched,
           count(*) FILTER (WHERE outcome = 'MISSING') AS missing,
           count(*) FILTER (WHERE outcome = 'EXCESS') AS excess,
           count(*) FILTER (WHERE outcome = 'DAMAGED') AS damaged
    FROM reconciliation_items WHERE reconciliation_id = sqlc.arg('reconciliation_id')
) c
WHERE r.id = sqlc.arg('reconciliation_id')
RETURNING r.*;

-- name: MarkUnscannedItemsMissing :exec
-- Completing a reconciliation converts everything still unscanned into MISSING.
UPDATE reconciliation_items
SET outcome = 'MISSING'
WHERE reconciliation_id = sqlc.arg('reconciliation_id')
  AND scanned_at IS NULL AND outcome = 'EXPECTED';

-- name: CompleteReconciliation :one
UPDATE reconciliations
SET status = sqlc.arg('to_status'), completed_at = now(),
    completed_by_user_id = sqlc.narg('completed_by_user_id'),
    remarks = COALESCE(sqlc.narg('remarks'), remarks)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
  AND status = 'IN_PROGRESS'
RETURNING *;

-- name: ListReconciliations :many
SELECT r.id, r.public_id, r.reconciliation_code, r.subject_type, r.status,
       r.expected_count, r.scanned_count, r.matched_count, r.missing_count,
       r.excess_count, r.damaged_count, r.started_at, r.completed_at,
       ou.code AS unit_code, b.bag_code, m.manifest_code
FROM reconciliations r
JOIN operating_units ou ON ou.id = r.operating_unit_id
LEFT JOIN bags b ON b.id = r.bag_id
LEFT JOIN manifests m ON m.id = r.manifest_id
WHERE r.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status'))
  AND (sqlc.narg('unit_ids')::bigint[] IS NULL OR r.operating_unit_id = ANY(sqlc.narg('unit_ids')))
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (r.created_at, r.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Workload summaries
-- ---------------------------------------------------------------------------

-- name: GetHubWorkloadSummary :one
-- One round trip for the facility dashboard. Every subquery is index-served on
-- a partial or composite index, so this stays cheap even on a busy hub.
--
-- Columns are alias-qualified throughout: the named parameter and the column
-- share the name organization_id, and without the alias the analyzer cannot
-- tell which one a bare reference means.
SELECT
    (SELECT count(*) FROM shipments sh
      WHERE sh.organization_id = sqlc.arg('organization_id')
        AND sh.current_custody_unit_id = sqlc.arg('unit_id')::bigint
        AND sh.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST'))::bigint AS in_custody,
    (SELECT count(*) FROM bags b
      WHERE b.organization_id = sqlc.arg('organization_id')
        AND b.origin_unit_id = sqlc.arg('unit_id')::bigint AND b.status = 'OPEN')::bigint AS open_bags,
    (SELECT count(*) FROM bags b
      WHERE b.organization_id = sqlc.arg('organization_id')
        AND b.origin_unit_id = sqlc.arg('unit_id')::bigint AND b.status = 'CLOSED')::bigint AS closed_bags,
    (SELECT count(*) FROM bags b
      WHERE b.organization_id = sqlc.arg('organization_id')
        AND b.destination_unit_id = sqlc.arg('unit_id')::bigint AND b.status = 'DISPATCHED')::bigint AS inbound_bags,
    (SELECT count(*) FROM manifests m
      WHERE m.organization_id = sqlc.arg('organization_id')
        AND m.origin_unit_id = sqlc.arg('unit_id')::bigint AND m.status = 'DRAFT')::bigint AS draft_manifests,
    (SELECT count(*) FROM manifests m
      WHERE m.organization_id = sqlc.arg('organization_id')
        AND m.destination_unit_id = sqlc.arg('unit_id')::bigint AND m.status = 'DISPATCHED')::bigint AS inbound_manifests,
    (SELECT count(*) FROM trips t
      WHERE t.organization_id = sqlc.arg('organization_id')
        AND t.destination_unit_id = sqlc.arg('unit_id')::bigint
        AND t.status IN ('DEPARTED','IN_TRANSIT'))::bigint AS inbound_trips,
    (SELECT count(*) FROM trips t
      WHERE t.organization_id = sqlc.arg('organization_id')
        AND t.origin_unit_id = sqlc.arg('unit_id')::bigint
        AND t.status IN ('PLANNED','LOADING'))::bigint AS outbound_trips,
    (SELECT count(*) FROM shipments sh
      WHERE sh.organization_id = sqlc.arg('organization_id')
        AND sh.destination_branch_id = sqlc.arg('unit_id')::bigint
        AND sh.current_status = 'DESTINATION_BRANCH_RECEIVED')::bigint AS ready_for_delivery,
    (SELECT count(*) FROM shipments sh
      WHERE sh.organization_id = sqlc.arg('organization_id')
        AND sh.destination_branch_id = sqlc.arg('unit_id')::bigint
        AND sh.current_status = 'OUT_FOR_DELIVERY')::bigint AS out_for_delivery,
    (SELECT count(*) FROM shipments sh
      WHERE sh.organization_id = sqlc.arg('organization_id')
        AND sh.destination_branch_id = sqlc.arg('unit_id')::bigint
        AND sh.current_status IN ('NDR','DELIVERY_FAILED'))::bigint AS ndr_pending,
    (SELECT count(*) FROM operational_exceptions e
      WHERE e.organization_id = sqlc.arg('organization_id')
        AND e.operating_unit_id = sqlc.arg('unit_id')::bigint
        AND e.status IN ('OPEN','INVESTIGATING'))::bigint AS open_exceptions,
    (SELECT count(*) FROM shipment_holds h
      WHERE h.organization_id = sqlc.arg('organization_id')
        AND h.operating_unit_id = sqlc.arg('unit_id')::bigint AND h.released_at IS NULL)::bigint AS on_hold,
    (SELECT count(*) FROM pickup_requests pr
      WHERE pr.organization_id = sqlc.arg('organization_id')
        AND pr.branch_id = sqlc.arg('unit_id')::bigint
        AND pr.status IN ('REQUESTED','SCHEDULED','ASSIGNED','ACCEPTED','IN_PROGRESS'))::bigint AS pending_pickups;

-- name: GetUnitScanThroughput :many
-- Scan volume by hour for the last day, for the operations console chart.
SELECT date_trunc('hour', occurred_at)::timestamptz AS bucket,
       scan_type,
       count(*)::bigint AS total
FROM scan_events
WHERE organization_id = sqlc.arg('organization_id')
  AND operating_unit_id = sqlc.arg('unit_id')::bigint
  AND occurred_at >= sqlc.arg('since')
GROUP BY 1, 2
ORDER BY 1 DESC, 2;

-- name: ListUnitCustodyShipments :many
-- What is physically here right now, for a stocktake.
SELECT s.id, s.public_id, s.awb, s.current_status, s.status_changed_at, s.piece_count,
       s.chargeable_weight_grams, s.destination_pincode, s.promised_delivery_at,
       s.is_held, s.movement_direction,
       b.bag_code, db.code AS destination_branch_code
FROM shipments s
LEFT JOIN bags b ON b.id = s.current_bag_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
WHERE s.organization_id = sqlc.arg('organization_id')
  AND s.current_custody_unit_id = sqlc.arg('unit_id')::bigint
  AND s.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
  AND (sqlc.narg('status')::text IS NULL OR s.current_status = sqlc.narg('status'))
  AND (sqlc.narg('cursor_changed_at')::timestamptz IS NULL
       OR (s.status_changed_at, s.id) < (sqlc.narg('cursor_changed_at'), sqlc.narg('cursor_id')::bigint))
ORDER BY s.status_changed_at DESC, s.id DESC
LIMIT sqlc.arg('page_size');
