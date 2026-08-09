-- 0028 Command centre summaries (M30) and the report engine (M31).
--
-- # Why summary tables
--
-- The specification is explicit: "Do not run full-table aggregate scans on
-- every request." A command centre refreshing every thirty seconds across a
-- 40,000-shipment day would otherwise scan shipments and shipment_events on
-- every poll, from every operator's browser at once.
--
-- The approach here is an **incrementally maintained daily rollup**, one row per
-- (date, operating unit, service). It is updated by a trigger on shipments as
-- statuses change, so it is exact rather than eventually consistent, and a
-- dashboard query reads a few hundred small rows instead of aggregating
-- hundreds of thousands.
--
-- The trade is a write cost on every shipment status change. That is the right
-- trade for this workload: a shipment changes status perhaps a dozen times in
-- its life, while a dashboard is polled continuously by every supervisor in the
-- network.
--
-- # Consistency
--
-- The counters are updated in the same transaction as the status change, so
-- they are consistent, not eventually consistent. There is **no** staleness
-- window to document for the counters.
--
-- The one place staleness does exist is `operational_snapshots`, which captures
-- point-in-time backlog for trend charts and is written by a scheduled job. Its
-- `captured_at` is on every row so a chart can state its own age.

-- ---------------------------------------------------------------------------
-- Daily operational rollup
-- ---------------------------------------------------------------------------
CREATE TABLE shipment_daily_stats (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id   bigint  NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,

    stat_date         date    NOT NULL,
    -- The unit the shipment was booked at. NULL aggregates network-wide rows
    -- that cannot be attributed to a unit.
    operating_unit_id bigint  REFERENCES operating_units (id) ON DELETE CASCADE,
    service_id        bigint  REFERENCES courier_services (id) ON DELETE CASCADE,

    -- Counters, one per KPI the command centre reports. Maintained
    -- incrementally; every one is a count of shipments that *entered* that
    -- state on this date, except the backlog gauges below.
    booked_count          integer NOT NULL DEFAULT 0,
    pickup_pending_count  integer NOT NULL DEFAULT 0,
    picked_up_count       integer NOT NULL DEFAULT 0,
    in_transit_count      integer NOT NULL DEFAULT 0,
    out_for_delivery_count integer NOT NULL DEFAULT 0,
    delivered_count       integer NOT NULL DEFAULT 0,
    ndr_count             integer NOT NULL DEFAULT 0,
    rto_count             integer NOT NULL DEFAULT 0,
    cancelled_count       integer NOT NULL DEFAULT 0,
    damaged_count         integer NOT NULL DEFAULT 0,
    lost_count            integer NOT NULL DEFAULT 0,
    sla_breach_count      integer NOT NULL DEFAULT 0,

    -- Money and weight moved, for the revenue tiles.
    revenue_minor         bigint  NOT NULL DEFAULT 0,
    cod_amount_minor      bigint  NOT NULL DEFAULT 0,
    weight_grams          bigint  NOT NULL DEFAULT 0,

    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT shipment_daily_stats_non_negative CHECK (
        booked_count >= 0 AND delivered_count >= 0 AND ndr_count >= 0
    )
);

-- The upsert target. COALESCE on the nullable dimensions so a single unique
-- index covers rows with and without a unit or service.
CREATE UNIQUE INDEX shipment_daily_stats_key_idx
    ON shipment_daily_stats (organization_id, stat_date,
                             COALESCE(operating_unit_id, 0), COALESCE(service_id, 0));
-- The dashboard query: a date range for a tenant, optionally narrowed.
CREATE INDEX shipment_daily_stats_range_idx
    ON shipment_daily_stats (organization_id, stat_date DESC);
CREATE INDEX shipment_daily_stats_unit_idx
    ON shipment_daily_stats (organization_id, operating_unit_id, stat_date DESC)
    WHERE operating_unit_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- The incremental maintainer
--
-- Fires when a shipment's current_status changes. One UPSERT per transition,
-- touching one small row.
--
-- Deliberately counts *entries into* a state rather than recomputing a
-- population: an increment is O(1) and commutative, whereas a recount would
-- have to scan. The backlog gauges the command centre needs — "how many are
-- sitting in transit right now" — are answered by a separate live query over
-- the indexed current_status, which is cheap because it is a count over an
-- index rather than an aggregate over history.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION bump_shipment_daily_stats()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    d       date := (COALESCE(NEW.booked_at, NEW.created_at))::date;
    unit_id bigint := COALESCE(NEW.booking_unit_id, NEW.origin_branch_id);
    col     text;
BEGIN
    -- Only act on a real status change, or on insert.
    IF TG_OP = 'UPDATE' AND NEW.current_status IS NOT DISTINCT FROM OLD.current_status THEN
        RETURN NULL;
    END IF;

    col := CASE NEW.current_status
        WHEN 'BOOKED'                     THEN 'booked_count'
        WHEN 'PICKUP_SCHEDULED'           THEN 'pickup_pending_count'
        WHEN 'PICKED_UP'                  THEN 'picked_up_count'
        WHEN 'IN_TRANSIT'                 THEN 'in_transit_count'
        WHEN 'OUT_FOR_DELIVERY'           THEN 'out_for_delivery_count'
        WHEN 'DELIVERED'                  THEN 'delivered_count'
        WHEN 'NDR'                        THEN 'ndr_count'
        WHEN 'RTO_INITIATED'              THEN 'rto_count'
        WHEN 'CANCELLED'                  THEN 'cancelled_count'
        WHEN 'DAMAGED'                    THEN 'damaged_count'
        WHEN 'LOST'                       THEN 'lost_count'
        ELSE NULL
    END;

    -- On insert, also record the money and weight this shipment represents.
    IF TG_OP = 'INSERT' THEN
        INSERT INTO shipment_daily_stats (
            organization_id, stat_date, operating_unit_id, service_id,
            revenue_minor, cod_amount_minor, weight_grams, updated_at)
        VALUES (NEW.organization_id, d, unit_id, NEW.courier_service_id,
                COALESCE(NEW.total_amount_minor, 0), COALESCE(NEW.cod_amount_minor, 0),
                COALESCE(NEW.chargeable_weight_grams, 0), now())
        ON CONFLICT (organization_id, stat_date,
                     COALESCE(operating_unit_id, 0), COALESCE(service_id, 0))
        DO UPDATE SET
            revenue_minor    = shipment_daily_stats.revenue_minor + EXCLUDED.revenue_minor,
            cod_amount_minor = shipment_daily_stats.cod_amount_minor + EXCLUDED.cod_amount_minor,
            weight_grams     = shipment_daily_stats.weight_grams + EXCLUDED.weight_grams,
            updated_at       = now();
    END IF;

    IF col IS NULL THEN
        RETURN NULL;   -- a transit state the dashboard does not tile
    END IF;

    -- Ensure the row exists, then increment the one counter. Two statements
    -- rather than dynamic SQL in the upsert, because the column name is
    -- computed and EXECUTE on the increment keeps it readable.
    INSERT INTO shipment_daily_stats (organization_id, stat_date, operating_unit_id, service_id)
    VALUES (NEW.organization_id, d, unit_id, NEW.courier_service_id)
    ON CONFLICT (organization_id, stat_date,
                 COALESCE(operating_unit_id, 0), COALESCE(service_id, 0))
    DO NOTHING;

    EXECUTE format(
        'UPDATE shipment_daily_stats SET %I = %I + 1, updated_at = now()
          WHERE organization_id = $1 AND stat_date = $2
            AND COALESCE(operating_unit_id, 0) = COALESCE($3::bigint, 0)
            AND COALESCE(service_id, 0) = COALESCE($4::bigint, 0)', col, col)
    USING NEW.organization_id, d, unit_id, NEW.courier_service_id;

    RETURN NULL;
END;
$$;

CREATE TRIGGER shipments_daily_stats_trigger
    AFTER INSERT OR UPDATE OF current_status ON shipments
    FOR EACH ROW EXECUTE FUNCTION bump_shipment_daily_stats();

-- ---------------------------------------------------------------------------
-- Point-in-time backlog snapshots, for trend charts.
--
-- Unlike the counters above, this IS eventually consistent: it is written by a
-- scheduled job, and every row carries the moment it was taken so a chart can
-- state its own age rather than implying it is live.
-- ---------------------------------------------------------------------------
CREATE TABLE operational_snapshots (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id   bigint  NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    operating_unit_id bigint  REFERENCES operating_units (id) ON DELETE CASCADE,

    captured_at       timestamptz NOT NULL DEFAULT now(),

    in_custody_count      integer NOT NULL DEFAULT 0,
    awaiting_pickup_count integer NOT NULL DEFAULT 0,
    inbound_count         integer NOT NULL DEFAULT 0,
    outbound_count        integer NOT NULL DEFAULT 0,
    ready_for_delivery_count integer NOT NULL DEFAULT 0,
    ndr_open_count        integer NOT NULL DEFAULT 0,
    exception_open_count  integer NOT NULL DEFAULT 0,
    cod_in_custody_minor  bigint  NOT NULL DEFAULT 0
);

CREATE INDEX operational_snapshots_time_idx
    ON operational_snapshots (organization_id, captured_at DESC);
CREATE INDEX operational_snapshots_unit_idx
    ON operational_snapshots (organization_id, operating_unit_id, captured_at DESC)
    WHERE operating_unit_id IS NOT NULL;

CREATE TRIGGER operational_snapshots_append_only
    BEFORE UPDATE OR DELETE ON operational_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Report runs (M31)
--
-- A report is a job, not a request. The specification forbids building one in
-- RAM, so the engine reads with a cursor, writes CSV to object storage in
-- chunks, and hands back a signed URL that expires.
-- ---------------------------------------------------------------------------
CREATE TABLE report_runs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rpt')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    report_type     text        NOT NULL CHECK (report_type IN (
                        'SHIPMENT_VOLUME','BRANCH_PERFORMANCE','FRANCHISE_PERFORMANCE',
                        'SERVICE_PERFORMANCE','SLA','NDR','RTO','COD_AGING',
                        'COMMISSION','SETTLEMENT','REVENUE','EXCEPTIONS')),
    format          text        NOT NULL DEFAULT 'CSV' CHECK (format IN ('CSV','JSON')),

    -- The filters this run was produced with, kept so a downloaded file can
    -- always be traced back to the question it answered.
    parameters      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    period_start    date,
    period_end      date,

    status          text        NOT NULL DEFAULT 'QUEUED' CHECK (status IN
                    ('QUEUED','RUNNING','COMPLETED','FAILED','EXPIRED','CANCELLED')),

    row_count       bigint      NOT NULL DEFAULT 0,
    byte_size       bigint      NOT NULL DEFAULT 0,
    -- The generated file. Null until the run completes.
    object_id       bigint      REFERENCES stored_objects (id) ON DELETE SET NULL,
    -- Downloads are signed and time-limited; after this the file is swept.
    expires_at      timestamptz,

    started_at      timestamptz,
    completed_at    timestamptz,
    error_message   text,
    duration_ms     integer,

    job_id          bigint      REFERENCES jobs (id) ON DELETE SET NULL,
    requested_by    bigint      REFERENCES users (id) ON DELETE SET NULL,
    request_id      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT report_runs_range CHECK (period_end IS NULL OR period_start IS NULL
                                        OR period_end >= period_start),
    CONSTRAINT report_runs_failure_recorded CHECK (
        status <> 'FAILED' OR error_message IS NOT NULL
    ),
    CONSTRAINT report_runs_completion_recorded CHECK (
        status <> 'COMPLETED' OR (object_id IS NOT NULL AND completed_at IS NOT NULL)
    )
);

CREATE INDEX report_runs_org_idx ON report_runs (organization_id, created_at DESC);
CREATE INDEX report_runs_status_idx ON report_runs (organization_id, status, created_at DESC);
CREATE INDEX report_runs_expiring_idx
    ON report_runs (expires_at) WHERE status = 'COMPLETED' AND expires_at IS NOT NULL;
-- A user's own reports, which is what the download screen lists.
CREATE INDEX report_runs_requester_idx
    ON report_runs (organization_id, requested_by, created_at DESC);

CREATE TRIGGER report_runs_touch
    BEFORE UPDATE ON report_runs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Backfill the rollup from existing shipments.
--
-- Without this a tenant with history would show an empty command centre until
-- new shipments arrived. Runs once, at migration time, and is bounded by the
-- data that already exists.
-- ---------------------------------------------------------------------------
INSERT INTO shipment_daily_stats (
    organization_id, stat_date, operating_unit_id, service_id,
    booked_count, delivered_count, ndr_count, rto_count, cancelled_count,
    damaged_count, lost_count, out_for_delivery_count, in_transit_count,
    picked_up_count, revenue_minor, cod_amount_minor, weight_grams)
SELECT
    s.organization_id,
    COALESCE(s.booked_at, s.created_at)::date,
    COALESCE(s.booking_unit_id, s.origin_branch_id),
    s.courier_service_id,
    count(*),
    count(*) FILTER (WHERE s.current_status = 'DELIVERED'),
    count(*) FILTER (WHERE s.current_status = 'NDR'),
    count(*) FILTER (WHERE s.current_status IN ('RTO_INITIATED','RTO_IN_TRANSIT','RTO_DELIVERED')),
    count(*) FILTER (WHERE s.current_status = 'CANCELLED'),
    count(*) FILTER (WHERE s.current_status = 'DAMAGED'),
    count(*) FILTER (WHERE s.current_status = 'LOST'),
    count(*) FILTER (WHERE s.current_status = 'OUT_FOR_DELIVERY'),
    count(*) FILTER (WHERE s.current_status = 'IN_TRANSIT'),
    count(*) FILTER (WHERE s.current_status = 'PICKED_UP'),
    COALESCE(SUM(s.total_amount_minor), 0),
    COALESCE(SUM(s.cod_amount_minor), 0),
    COALESCE(SUM(s.chargeable_weight_grams), 0)
FROM shipments s
GROUP BY 1, 2, 3, 4
ON CONFLICT DO NOTHING;
