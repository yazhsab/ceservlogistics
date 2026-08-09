-- Rollback 0016.

DROP TABLE IF EXISTS shipment_holds;
DROP TABLE IF EXISTS reconciliation_items;

ALTER TABLE operational_exceptions DROP CONSTRAINT IF EXISTS operational_exceptions_reconciliation_fk;
DROP TABLE IF EXISTS reconciliations;

ALTER TABLE bag_items DROP CONSTRAINT IF EXISTS bag_items_exception_fk;
DROP TABLE IF EXISTS operational_exceptions;
