-- Reverse 0024.
DROP TRIGGER IF EXISTS invoices_total_check ON invoices;
DROP TRIGGER IF EXISTS invoice_lines_guard_trigger ON invoice_lines;
DROP TRIGGER IF EXISTS invoices_guard_trigger ON invoices;

DROP FUNCTION IF EXISTS assert_invoice_totals();
DROP FUNCTION IF EXISTS invoice_lines_guard();
DROP FUNCTION IF EXISTS invoices_guard();

ALTER TABLE tax_components DROP CONSTRAINT IF EXISTS tax_components_credit_note_fk;

DROP TABLE IF EXISTS invoice_payments;
DROP TABLE IF EXISTS credit_note_lines;
DROP TABLE IF EXISTS credit_notes;
DROP TABLE IF EXISTS tax_components;
DROP TABLE IF EXISTS invoice_shipments;
DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS billing_runs;
DROP TABLE IF EXISTS invoice_sequences;
