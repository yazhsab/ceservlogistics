-- Reverse 0022.
DROP TRIGGER IF EXISTS cod_obligations_guard_trigger ON cod_obligations;
DROP FUNCTION IF EXISTS cod_obligations_guard();

DROP TABLE IF EXISTS cod_disputes;
DROP TABLE IF EXISTS cod_adjustments;
DROP TABLE IF EXISTS cod_remittance_items;
DROP TABLE IF EXISTS cod_remittances;
DROP TABLE IF EXISTS cod_reconciliation_items;
DROP TABLE IF EXISTS cod_reconciliations;
DROP TABLE IF EXISTS cod_custody_transfer_items;
DROP TABLE IF EXISTS cod_custody_transfers;
DROP TABLE IF EXISTS cod_collections;
DROP TABLE IF EXISTS cod_obligations;
