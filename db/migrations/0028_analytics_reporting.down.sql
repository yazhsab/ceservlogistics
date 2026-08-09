-- Reverse 0028.
DROP TRIGGER IF EXISTS report_runs_touch ON report_runs;
DROP TRIGGER IF EXISTS operational_snapshots_append_only ON operational_snapshots;
DROP TRIGGER IF EXISTS shipments_daily_stats_trigger ON shipments;
DROP FUNCTION IF EXISTS bump_shipment_daily_stats();

DROP TABLE IF EXISTS report_runs;
DROP TABLE IF EXISTS operational_snapshots;
DROP TABLE IF EXISTS shipment_daily_stats;
