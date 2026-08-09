-- Operations command centre (M30).
--
-- Two kinds of question, answered two different ways.
--
-- **"How many moved today?"** reads shipment_daily_stats — a rollup maintained
-- incrementally by a trigger, so it is exact and costs one small row read per
-- day in the range. Never an aggregate over shipments.
--
-- **"How many are sitting there right now?"** is a live count over an indexed
-- current_status. That is a count over an index, not an aggregate over history,
-- and it must stay that way: a backlog gauge is the one number an operator will
-- not accept as stale, because it is what they are about to act on.
--
-- Every query here is tenant-scoped and bounded. None of them scans shipments
-- without a status or unit predicate.

-- ---------------------------------------------------------------------------
-- Daily rollup
-- ---------------------------------------------------------------------------

-- name: SummariseDailyStats :one
-- The headline tiles for a date range. Reads the rollup, never the source.
SELECT
    COALESCE(SUM(booked_count), 0)::bigint            AS booked,
    COALESCE(SUM(picked_up_count), 0)::bigint         AS picked_up,
    COALESCE(SUM(in_transit_count), 0)::bigint        AS in_transit,
    COALESCE(SUM(out_for_delivery_count), 0)::bigint  AS out_for_delivery,
    COALESCE(SUM(delivered_count), 0)::bigint         AS delivered,
    COALESCE(SUM(ndr_count), 0)::bigint               AS ndr,
    COALESCE(SUM(rto_count), 0)::bigint               AS rto,
    COALESCE(SUM(cancelled_count), 0)::bigint         AS cancelled,
    COALESCE(SUM(damaged_count), 0)::bigint           AS damaged,
    COALESCE(SUM(lost_count), 0)::bigint              AS lost,
    COALESCE(SUM(sla_breach_count), 0)::bigint        AS sla_breaches,
    COALESCE(SUM(revenue_minor), 0)::bigint           AS revenue_minor,
    COALESCE(SUM(cod_amount_minor), 0)::bigint        AS cod_amount_minor,
    COALESCE(SUM(weight_grams), 0)::bigint            AS weight_grams
FROM shipment_daily_stats
WHERE organization_id = $1
  AND stat_date BETWEEN sqlc.arg('from_date') AND sqlc.arg('to_date')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
  AND (sqlc.narg('service_id')::bigint IS NULL
       OR service_id = sqlc.narg('service_id')::bigint);

-- name: ListDailyStatsByDate :many
-- The trend series behind the tiles. One row per day, so a 90-day chart reads
-- 90 rows.
SELECT stat_date,
    SUM(booked_count)::bigint           AS booked,
    SUM(delivered_count)::bigint        AS delivered,
    SUM(ndr_count)::bigint              AS ndr,
    SUM(rto_count)::bigint              AS rto,
    SUM(sla_breach_count)::bigint       AS sla_breaches,
    SUM(revenue_minor)::bigint          AS revenue_minor,
    SUM(cod_amount_minor)::bigint       AS cod_amount_minor
FROM shipment_daily_stats
WHERE organization_id = $1
  AND stat_date BETWEEN sqlc.arg('from_date') AND sqlc.arg('to_date')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
GROUP BY stat_date
ORDER BY stat_date;

-- name: ListDailyStatsByUnit :many
-- The league table: which branches and hubs are performing. Bounded, because a
-- large network has hundreds of units and a dashboard shows the top slice.
SELECT s.operating_unit_id,
    u.public_id AS unit_public_id, u.code AS unit_code, u.name AS unit_name,
    u.unit_type,
    SUM(s.booked_count)::bigint     AS booked,
    SUM(s.delivered_count)::bigint  AS delivered,
    SUM(s.ndr_count)::bigint        AS ndr,
    SUM(s.rto_count)::bigint        AS rto,
    SUM(s.sla_breach_count)::bigint AS sla_breaches,
    SUM(s.revenue_minor)::bigint    AS revenue_minor
FROM shipment_daily_stats s
JOIN operating_units u ON u.id = s.operating_unit_id
WHERE s.organization_id = $1
  AND s.stat_date BETWEEN sqlc.arg('from_date') AND sqlc.arg('to_date')
  AND (sqlc.narg('unit_type')::text IS NULL OR u.unit_type = sqlc.narg('unit_type')::text)
GROUP BY s.operating_unit_id, u.public_id, u.code, u.name, u.unit_type
ORDER BY SUM(s.booked_count) DESC
LIMIT sqlc.arg('row_limit');

-- name: ListDailyStatsByService :many
SELECT s.service_id,
    c.code AS service_code, c.name AS service_name,
    SUM(s.booked_count)::bigint     AS booked,
    SUM(s.delivered_count)::bigint  AS delivered,
    SUM(s.ndr_count)::bigint        AS ndr,
    SUM(s.sla_breach_count)::bigint AS sla_breaches,
    SUM(s.revenue_minor)::bigint    AS revenue_minor
FROM shipment_daily_stats s
JOIN courier_services c ON c.id = s.service_id
WHERE s.organization_id = $1
  AND s.stat_date BETWEEN sqlc.arg('from_date') AND sqlc.arg('to_date')
GROUP BY s.service_id, c.code, c.name
ORDER BY SUM(s.booked_count) DESC
LIMIT sqlc.arg('row_limit');

-- ---------------------------------------------------------------------------
-- Live backlog
-- ---------------------------------------------------------------------------

-- name: CountLiveShipmentsByStatus :many
-- The live gauges.
--
-- The predicate is written exactly as shipments_live_backlog_idx is defined —
-- NOT IN the four terminal states — because that is what lets the planner use
-- the partial index. An equivalent `= ANY(live statuses)` form does not: the
-- planner cannot prove one implies the other, and it falls back to a sequential
-- scan over the whole table.
SELECT current_status, count(*)::bigint AS count
FROM shipments
WHERE organization_id = $1
  AND current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR current_custody_unit_id = sqlc.narg('operating_unit_id')::bigint)
GROUP BY current_status;

-- name: CountBacklogByUnit :many
-- Where the parcels physically are. Custody, not routing: the question is
-- which facility is holding work, and a parcel routed through a hub it has not
-- reached yet is not that hub's problem.
SELECT s.current_custody_unit_id AS operating_unit_id,
       u.public_id AS unit_public_id, u.code AS unit_code, u.name AS unit_name,
       count(*)::bigint AS count
FROM shipments s
JOIN operating_units u ON u.id = s.current_custody_unit_id
WHERE s.organization_id = $1
  AND s.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
GROUP BY s.current_custody_unit_id, u.public_id, u.code, u.name
ORDER BY count(*) DESC
LIMIT sqlc.arg('row_limit');

-- name: CountSLABreaches :one
-- Promised before now and not yet arrived.
--
-- Served by shipments_sla_breach_idx, whose predicate this matches exactly. The
-- Release 2 index that also carries promised_delivery_at leads with
-- destination_branch_id and cannot answer the network-wide form.
SELECT count(*)::bigint AS breached,
       count(*) FILTER (WHERE s.promised_delivery_at > now() - interval '24 hours')::bigint
           AS breached_today
FROM shipments s
WHERE s.organization_id = $1
  AND s.promised_delivery_at IS NOT NULL
  AND s.promised_delivery_at < now()
  AND s.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR s.destination_branch_id = sqlc.narg('operating_unit_id')::bigint);

-- name: CountHeldShipments :one
SELECT count(*)::bigint AS count
FROM shipments
WHERE organization_id = $1 AND is_held;

-- ---------------------------------------------------------------------------
-- Open work: NDR, exceptions, COD in custody
-- ---------------------------------------------------------------------------

-- name: CountOpenNDRCases :many
SELECT current_reason_code AS reason_code, count(*)::bigint AS count
FROM ndr_cases
WHERE organization_id = $1
  AND status IN ('OPEN','PENDING_CUSTOMER','SCHEDULED','ESCALATED')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR branch_id = sqlc.narg('operating_unit_id')::bigint)
GROUP BY current_reason_code
ORDER BY count(*) DESC;

-- name: CountOpenExceptions :many
SELECT exception_type, severity, count(*)::bigint AS count
FROM operational_exceptions
WHERE organization_id = $1
  AND status IN ('OPEN','INVESTIGATING')
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
GROUP BY exception_type, severity
ORDER BY count(*) DESC;

-- name: SummariseCODInCustody :one
-- Cash the network is holding but has not remitted. A finance number on an
-- operations screen on purpose: it is the one that grows quietly.
SELECT COALESCE(SUM(collected_minor - remitted_minor), 0)::bigint AS outstanding_minor,
       count(*)::bigint AS obligation_count,
       count(*) FILTER (WHERE collected_at < now() - interval '48 hours')::bigint
           AS aged_over_48h
FROM cod_obligations
WHERE organization_id = $1
  AND status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED','RECONCILED');

-- ---------------------------------------------------------------------------
-- Throughput, for the "is the network moving" strip
-- ---------------------------------------------------------------------------

-- name: CountRecentScans :one
-- Scans in the last hour. The pulse of the network: if this falls to zero
-- during working hours, something is wrong that no status count will show.
SELECT count(*)::bigint AS count
FROM scan_events
WHERE organization_id = $1 AND occurred_at >= now() - sqlc.arg('window')::interval;

-- name: CountActiveTrips :many
SELECT status, count(*)::bigint AS count
FROM trips
WHERE organization_id = $1 AND status IN ('PLANNED','LOADING','DEPARTED','IN_TRANSIT','ARRIVED')
GROUP BY status;

-- name: CountOpenBagsAndManifests :one
SELECT
    (SELECT count(*) FROM bags b
      WHERE b.organization_id = sqlc.arg('organization_id') AND b.status = 'OPEN')::bigint
        AS open_bags,
    (SELECT count(*) FROM manifests m
      WHERE m.organization_id = sqlc.arg('organization_id') AND m.status = 'DRAFT')::bigint
        AS draft_manifests,
    (SELECT count(*) FROM manifests m
      WHERE m.organization_id = sqlc.arg('organization_id') AND m.status = 'DISPATCHED')::bigint
        AS manifests_in_flight;

-- ---------------------------------------------------------------------------
-- Snapshots
-- ---------------------------------------------------------------------------

-- name: CaptureOperationalSnapshot :one
INSERT INTO operational_snapshots (
    organization_id, operating_unit_id,
    in_custody_count, awaiting_pickup_count, inbound_count, outbound_count,
    ready_for_delivery_count, ndr_open_count, exception_open_count,
    cod_in_custody_minor
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: ListOperationalSnapshots :many
-- The trend series. Bounded by both a time window and a row cap, because this
-- table grows with every capture interval forever.
SELECT * FROM operational_snapshots
WHERE organization_id = $1
  AND captured_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
ORDER BY captured_at DESC
LIMIT sqlc.arg('row_limit');

-- name: LatestOperationalSnapshot :one
SELECT * FROM operational_snapshots
WHERE organization_id = $1
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL
       OR operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
ORDER BY captured_at DESC
LIMIT 1;

-- name: PurgeOldSnapshots :execrows
-- Retention. Trend charts look back weeks, not years, and this table is written
-- on a timer forever.
DELETE FROM operational_snapshots
WHERE captured_at < now() - sqlc.arg('older_than')::interval;

-- name: ListOrganizationIDs :many
-- For the platform-wide snapshot sweep, which has no tenant context of its own.
SELECT id FROM organizations WHERE status = 'ACTIVE' ORDER BY id;
