-- Rollback 0015.

DROP TABLE IF EXISTS trip_events;
DROP TABLE IF EXISTS trip_assignments;

DROP INDEX IF EXISTS manifests_trip_leg_idx;
ALTER TABLE manifests DROP COLUMN IF EXISTS trip_leg_id;

DROP TABLE IF EXISTS trip_legs;

ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_current_trip_fk;
ALTER TABLE manifests DROP CONSTRAINT IF EXISTS manifests_trip_fk;

DROP TABLE IF EXISTS trips;
DROP TABLE IF EXISTS drivers;
DROP TABLE IF EXISTS vehicles;
DROP TABLE IF EXISTS carriers;
