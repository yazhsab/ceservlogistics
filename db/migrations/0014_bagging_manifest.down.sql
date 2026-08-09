-- Rollback 0014.

DROP TRIGGER IF EXISTS manifest_loose_guard_trigger ON manifest_loose_shipments;
DROP TRIGGER IF EXISTS manifest_bags_guard_trigger ON manifest_bags;
DROP FUNCTION IF EXISTS manifest_contents_guard();

DROP TABLE IF EXISTS manifest_events;
DROP TABLE IF EXISTS manifest_loose_shipments;
DROP TABLE IF EXISTS manifest_bags;
DROP TABLE IF EXISTS manifests;

DROP TRIGGER IF EXISTS bag_items_guard_trigger ON bag_items;
DROP FUNCTION IF EXISTS bag_items_guard();

DROP TABLE IF EXISTS bag_events;
DROP TABLE IF EXISTS bag_seals;
DROP TABLE IF EXISTS bag_items;

ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_current_bag_fk;
DROP TABLE IF EXISTS bags;
