-- Restore the generic append-only guard from 0028.
DROP INDEX IF EXISTS operational_snapshots_retention_idx;
DROP TRIGGER IF EXISTS operational_snapshots_guard ON operational_snapshots;
DROP FUNCTION IF EXISTS operational_snapshots_guard();

CREATE TRIGGER operational_snapshots_append_only
    BEFORE UPDATE OR DELETE ON operational_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
DROP INDEX IF EXISTS shipments_sla_breach_idx;
DROP INDEX IF EXISTS shipments_live_custody_idx;
DROP INDEX IF EXISTS shipments_live_backlog_idx;
