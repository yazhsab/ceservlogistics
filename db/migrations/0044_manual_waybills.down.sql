DROP INDEX IF EXISTS shipments_manual_waybill_uq;
ALTER TABLE shipments DROP COLUMN IF EXISTS manual_waybill_number;
