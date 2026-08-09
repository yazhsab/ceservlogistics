-- Reverse 0023.
ALTER TABLE commission_entries DROP CONSTRAINT IF EXISTS commission_entries_settlement_fk;
ALTER TABLE commission_calculations DROP CONSTRAINT IF EXISTS commission_calculations_settlement_fk;

DROP TRIGGER IF EXISTS settlements_total_check ON settlements;
DROP TRIGGER IF EXISTS settlement_lines_guard_trigger ON settlement_lines;
DROP TRIGGER IF EXISTS settlements_guard_trigger ON settlements;

DROP FUNCTION IF EXISTS assert_settlement_totals();
DROP FUNCTION IF EXISTS settlement_lines_guard();
DROP FUNCTION IF EXISTS settlements_guard();

DROP TABLE IF EXISTS settlement_payments;
DROP TABLE IF EXISTS settlement_approvals;
DROP TABLE IF EXISTS settlement_adjustments;
DROP TABLE IF EXISTS settlement_lines;
DROP TABLE IF EXISTS settlements;
