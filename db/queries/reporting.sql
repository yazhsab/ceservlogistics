-- Report runs (M31).
--
-- A report is a job, not a request. The specification forbids building one in
-- RAM, so a run is queued, executed by a worker that reads with a cursor and
-- streams CSV into object storage, and downloaded through a signed URL that
-- expires. Nothing here returns report *rows* — the rows go to the file.

-- name: CreateReportRun :one
INSERT INTO report_runs (
    public_id, organization_id, report_type, format, parameters,
    period_start, period_end, requested_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetReportRunByPublicID :one
SELECT r.*, o.object_key, o.bucket, o.mime_type, o.size_bytes
FROM report_runs r
LEFT JOIN stored_objects o ON o.id = r.object_id
WHERE r.organization_id = $1 AND r.public_id = $2;

-- name: GetReportRunByID :one
SELECT * FROM report_runs WHERE id = $1;

-- name: ListReportRuns :many
SELECT r.public_id, r.report_type, r.format, r.status, r.period_start, r.period_end,
       r.row_count, r.byte_size, r.expires_at, r.started_at, r.completed_at,
       r.error_message, r.duration_ms, r.created_at,
       u.full_name AS requested_by_name
FROM report_runs r
LEFT JOIN users u ON u.id = r.requested_by
WHERE r.organization_id = $1
  AND (sqlc.narg('report_type')::text IS NULL OR r.report_type = sqlc.narg('report_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text)
  AND (sqlc.narg('requested_by')::bigint IS NULL OR r.requested_by = sqlc.narg('requested_by')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR r.id < sqlc.narg('cursor_id')::bigint)
ORDER BY r.id DESC
LIMIT sqlc.arg('row_limit');

-- name: StartReportRun :one
-- QUEUED -> RUNNING, and only that. A run already RUNNING is not restarted:
-- two workers generating the same file would both write, and the second would
-- overwrite the first's object while the first was still streaming into it.
UPDATE report_runs
   SET status = 'RUNNING', started_at = now()
 WHERE id = $1 AND status = 'QUEUED'
RETURNING *;

-- name: CompleteReportRun :one
UPDATE report_runs
   SET status = 'COMPLETED', completed_at = now(),
       object_id = sqlc.arg('object_id'), row_count = sqlc.arg('row_count'),
       byte_size = sqlc.arg('byte_size'),
       expires_at = now() + sqlc.arg('ttl')::interval,
       duration_ms = (EXTRACT(EPOCH FROM (now() - COALESCE(started_at, now()))) * 1000)::int
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: FailReportRun :one
UPDATE report_runs
   SET status = 'FAILED', completed_at = now(),
       error_message = sqlc.arg('error_message'),
       duration_ms = (EXTRACT(EPOCH FROM (now() - COALESCE(started_at, now()))) * 1000)::int
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: CancelReportRun :one
UPDATE report_runs
   SET status = 'CANCELLED', completed_at = now()
 WHERE organization_id = $1 AND id = $2 AND status IN ('QUEUED','RUNNING')
RETURNING *;

-- name: ExpireReportRuns :many
-- A completed report whose download window has passed. The rows are returned so
-- the sweep can delete the objects too: expiring the record while leaving the
-- file in the bucket would be a slow leak of exported customer data.
UPDATE report_runs
   SET status = 'EXPIRED'
 WHERE status = 'COMPLETED' AND expires_at IS NOT NULL AND expires_at < now()
RETURNING id, organization_id, public_id, object_id;

-- name: SetReportRunJob :exec
UPDATE report_runs SET job_id = $2 WHERE id = $1;

-- ---------------------------------------------------------------------------
-- The report data sources
--
-- Every one of these is a *cursor* read: ordered by a unique key, bounded by a
-- row limit, resumable from the last key seen. The worker walks them in chunks
-- and writes each chunk straight out, so peak memory is one chunk regardless of
-- how many rows the report covers.
--
-- None of them may use OFFSET. A million-row report paged by offset re-scans
-- the rows it skips on every chunk, which turns a linear export into a
-- quadratic one.
-- ---------------------------------------------------------------------------

-- name: ReportShipments :many
SELECT s.id, s.awb, s.reference_number, s.current_status, s.payment_mode,
       s.piece_count, s.actual_weight_grams, s.chargeable_weight_grams,
       s.origin_pincode, s.destination_pincode,
       s.total_amount_minor, s.cod_amount_minor, s.currency,
       s.booked_at, s.status_changed_at, s.promised_delivery_at,
       c.code AS customer_code, c.name AS customer_name,
       sv.code AS service_code,
       ob.code AS origin_branch_code, db.code AS destination_branch_code
FROM shipments s
JOIN customers c ON c.id = s.customer_id
JOIN courier_services sv ON sv.id = s.courier_service_id
LEFT JOIN operating_units ob ON ob.id = s.origin_branch_id
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
WHERE s.organization_id = $1
  AND s.booked_at >= sqlc.arg('period_start')::timestamptz
  AND s.booked_at < sqlc.arg('period_end')::timestamptz
  AND (sqlc.narg('unit_id')::bigint IS NULL
       OR s.booking_unit_id = sqlc.narg('unit_id')::bigint
       OR s.origin_branch_id = sqlc.narg('unit_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR s.current_status = sqlc.narg('status')::text)
  AND s.id > sqlc.arg('after_id')
ORDER BY s.id
LIMIT sqlc.arg('chunk_size');

-- name: ReportCODAging :many
SELECT o.id, o.awb, o.status, o.expected_minor, o.collected_minor, o.remitted_minor,
       o.currency, o.collected_at, o.remitted_at, o.custodian_type,
       f.code AS franchise_code, f.name AS franchise_name,
       EXTRACT(DAY FROM (now() - o.collected_at))::int AS days_held
FROM cod_obligations o
LEFT JOIN franchises f ON f.id = o.destination_franchise_id
WHERE o.organization_id = $1
  AND o.status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED','RECONCILED')
  AND o.id > sqlc.arg('after_id')
ORDER BY o.id
LIMIT sqlc.arg('chunk_size');

-- name: ReportCommissionEntries :many
SELECT e.id, e.public_id, e.entry_type, e.commission_type, e.amount_minor,
       e.currency, e.earned_on, e.memo,
       f.code AS franchise_code, f.name AS franchise_name,
       s.awb, st.settlement_number
FROM commission_entries e
JOIN franchises f ON f.id = e.franchise_id
LEFT JOIN shipments s ON s.id = e.shipment_id
LEFT JOIN settlements st ON st.id = e.settlement_id
WHERE e.organization_id = $1
  AND e.earned_on >= sqlc.arg('period_start')::date
  AND e.earned_on <= sqlc.arg('period_end')::date
  AND e.id > sqlc.arg('after_id')
ORDER BY e.id
LIMIT sqlc.arg('chunk_size');

-- name: ReportNDRCases :many
SELECT n.id, n.case_code, n.status, n.current_reason_code, n.attempt_count,
       n.opened_at, n.resolved_at, n.current_action,
       s.awb, s.destination_pincode,
       b.code AS branch_code
FROM ndr_cases n
JOIN shipments s ON s.id = n.shipment_id
LEFT JOIN operating_units b ON b.id = n.branch_id
WHERE n.organization_id = $1
  AND n.opened_at >= sqlc.arg('period_start')::timestamptz
  AND n.opened_at < sqlc.arg('period_end')::timestamptz
  AND n.id > sqlc.arg('after_id')
ORDER BY n.id
LIMIT sqlc.arg('chunk_size');

-- name: ReportBranchPerformance :many
-- Reads the rollup, not shipments: a branch-performance report over a year is
-- 365 rows per branch rather than every parcel they handled.
SELECT s.id, u.code AS unit_code, u.name AS unit_name, u.unit_type,
       s.stat_date, s.booked_count, s.delivered_count, s.ndr_count,
       s.rto_count, s.sla_breach_count, s.revenue_minor, s.cod_amount_minor
FROM shipment_daily_stats s
JOIN operating_units u ON u.id = s.operating_unit_id
WHERE s.organization_id = $1
  AND s.stat_date >= sqlc.arg('period_start')::date
  AND s.stat_date <= sqlc.arg('period_end')::date
  AND s.id > sqlc.arg('after_id')
ORDER BY s.id
LIMIT sqlc.arg('chunk_size');

-- name: GetStoredObjectKey :one
-- Just the key, for the expiry sweep: it deletes the file and does not need the
-- rest of the metadata row.
SELECT object_key FROM stored_objects WHERE id = $1;
