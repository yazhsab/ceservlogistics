-- 0030 Retention for operational_snapshots (M30).
--
-- 0028 put the generic append-only guard on operational_snapshots, which was
-- right about UPDATE and wrong about DELETE.
--
-- The table is written on a timer, per tenant, forever: at a 15-minute sweep
-- and fifty tenants that is roughly 1.75 million rows a year, and §27 does not
-- permit an unbounded table. So retention has to be possible. But "append-only"
-- must keep meaning something, or the guard is decorative: a trend chart whose
-- recent past can be quietly deleted is not evidence of anything.
--
-- The resolution is a purpose-built guard rather than the generic one:
--
--   * UPDATE is refused unconditionally. A reading is what it was.
--   * DELETE is refused for anything inside the retention floor, and permitted
--     outside it. Housekeeping can drop last year; nobody can drop last week.
--
-- The floor is deliberately a constant in the trigger rather than a setting.
-- A retention window that can be shortened at runtime is a retention window an
-- incident can be hidden behind.

CREATE OR REPLACE FUNCTION operational_snapshots_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    -- Older than this may be swept. Comfortably longer than any trend chart
    -- the command centre offers (90 days).
    retention_floor constant interval := interval '90 days';
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION
            'operational_snapshots is append-only; a captured reading cannot be changed'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF TG_OP = 'DELETE' THEN
        IF OLD.captured_at > now() - retention_floor THEN
            RAISE EXCEPTION
                'operational_snapshots rows younger than % cannot be deleted (captured_at %)',
                retention_floor, OLD.captured_at
                USING ERRCODE = 'restrict_violation';
        END IF;
        RETURN OLD;
    END IF;

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS operational_snapshots_append_only ON operational_snapshots;

CREATE TRIGGER operational_snapshots_guard
    BEFORE UPDATE OR DELETE ON operational_snapshots
    FOR EACH ROW EXECUTE FUNCTION operational_snapshots_guard();

-- Retention deletes by age across every tenant, so the sweep needs an index on
-- captured_at alone. The existing indexes all lead with organization_id, which
-- a global purge cannot use.
CREATE INDEX operational_snapshots_retention_idx ON operational_snapshots (captured_at);

-- ---------------------------------------------------------------------------
-- Live backlog indexes (M30)
--
-- The command centre's backlog gauges count "everything still in play". With a
-- plain (organization_id, current_status) index the planner correctly refuses
-- it whenever in-progress work is a large fraction of the table, and falls back
-- to a sequential scan — measured at a parallel seq scan over 200,000 rows,
-- which is precisely the cost this module exists to avoid, on the most-polled
-- endpoint in the platform.
--
-- A partial index fixes it structurally rather than statistically. Its size is
-- proportional to the *backlog*, not to history: a tenant with two million
-- delivered shipments and four thousand in play has a four-thousand-entry
-- index, and the count stays cheap no matter how the distribution shifts.
--
-- The predicate is written as NOT IN the terminal states rather than IN the
-- live ones on purpose: a status added later is live by default, so a new state
-- cannot silently fall out of the operator's backlog.
-- ---------------------------------------------------------------------------
CREATE INDEX shipments_live_backlog_idx
    ON shipments (organization_id, current_status)
    WHERE current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST');

-- The same set, keyed by who is physically holding it, for the per-facility
-- breakdown.
CREATE INDEX shipments_live_custody_idx
    ON shipments (organization_id, current_custody_unit_id)
    WHERE current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
      AND current_custody_unit_id IS NOT NULL;

-- The SLA-breach gauge asks a network-wide question: how many parcels were
-- promised before now and have not arrived. The Release 2 index that carries
-- promised_delivery_at leads with destination_branch_id, which serves a
-- per-branch question and cannot serve this one — measured as a parallel
-- sequential scan.
--
-- Partial again, and for the same reason: only live shipments that carry a
-- promise can breach one, so the index tracks outstanding commitments rather
-- than every delivery ever made.
CREATE INDEX shipments_sla_breach_idx
    ON shipments (organization_id, promised_delivery_at)
    WHERE promised_delivery_at IS NOT NULL
      AND current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST');
