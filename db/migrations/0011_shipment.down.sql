DROP TABLE IF EXISTS shipment_events;
DROP TABLE IF EXISTS shipment_route_snapshots;
DROP TABLE IF EXISTS shipment_charge_snapshots;
DROP TABLE IF EXISTS shipment_address_snapshots;
DROP TABLE IF EXISTS shipment_packages;
DROP INDEX IF EXISTS customer_credit_entries_shipment_idx;
ALTER TABLE customer_credit_entries DROP CONSTRAINT IF EXISTS customer_credit_entries_shipment_fk;
DROP TABLE IF EXISTS shipments;
DROP TABLE IF EXISTS awb_sequences;
