-- Billing (M25).
--
-- Two properties decide the shape of everything here:
--
--   1. Numbering is concurrency-safe and gapless per series. AllocateInvoiceNumber
--      is one atomic statement, so N concurrent issuers get N distinct numbers
--      without a process-local lock (§22). Tax authorities care about gaps.
--   2. Lines are snapshots. An invoice line copies the amount, the description
--      and the tax at the moment of issue and never joins back to a rate card
--      at read time, so editing a rate card tomorrow cannot change an invoice
--      issued today (§19, §26).

-- ---------------------------------------------------------------------------
-- Numbering
-- ---------------------------------------------------------------------------

-- name: AllocateInvoiceNumber :one
-- The statutory counter. One atomic UPSERT: concurrent issuers serialise on the
-- row rather than on an application lock, and the WHERE guard refuses to wrap
-- around onto a number that already identifies a document.
INSERT INTO invoice_sequences (organization_id, series_code, document_type, prefix, scope_key, current_value)
VALUES (sqlc.arg('organization_id'), sqlc.arg('series_code'), sqlc.arg('document_type'),
        sqlc.arg('prefix'), sqlc.arg('scope_key'), 1)
ON CONFLICT (organization_id, series_code, document_type, scope_key)
DO UPDATE SET current_value = invoice_sequences.current_value + 1, updated_at = now()
WHERE invoice_sequences.current_value < invoice_sequences.max_value
RETURNING current_value, prefix, padding;

-- name: GetInvoiceSequence :one
SELECT * FROM invoice_sequences
WHERE organization_id = $1 AND series_code = $2 AND document_type = $3 AND scope_key = $4;

-- ---------------------------------------------------------------------------
-- Invoices
-- ---------------------------------------------------------------------------

-- name: CreateInvoice :one
INSERT INTO invoices (
    public_id, organization_id, invoice_number, series_code, document_type,
    customer_id, business_account_id, billing_run_id, billing_mode,
    period_start, period_end, issue_date, due_date, status, currency,
    bill_to, supplier, tax_metadata, notes, created_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'DRAFT',$14,$15,$16,$17,$18,$19,$20)
RETURNING *;

-- name: GetInvoiceByPublicID :one
SELECT i.*, c.code AS customer_code, c.name AS customer_name,
       u.full_name AS issued_by_name
FROM invoices i
JOIN customers c ON c.id = i.customer_id
LEFT JOIN users u ON u.id = i.issued_by
WHERE i.organization_id = $1 AND i.public_id = $2;

-- name: GetInvoiceByID :one
SELECT * FROM invoices WHERE organization_id = $1 AND id = $2;

-- name: LockInvoice :one
SELECT * FROM invoices WHERE organization_id = $1 AND id = $2 FOR UPDATE;

-- name: ApplyInvoiceTotals :one
-- Writes the computed header. Only while DRAFT; the invoices_guard trigger
-- refuses it once issued.
UPDATE invoices
   SET subtotal_minor = sqlc.arg('subtotal_minor'),
       discount_minor = sqlc.arg('discount_minor'),
       taxable_minor = sqlc.arg('taxable_minor'),
       tax_minor = sqlc.arg('tax_minor'),
       rounding_minor = sqlc.arg('rounding_minor'),
       total_minor = sqlc.arg('total_minor'),
       shipment_count = sqlc.arg('shipment_count')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'DRAFT'
RETURNING *;

-- name: IssueInvoice :one
-- CAS on DRAFT: two concurrent issuers cannot both stamp a number on one
-- invoice, and the loser sees it is already issued.
UPDATE invoices
   SET status = 'ISSUED', invoice_number = sqlc.arg('invoice_number'),
       issued_at = now(), issued_by = sqlc.arg('issued_by'),
       journal_transaction_id = sqlc.narg('journal_transaction_id')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'DRAFT'
RETURNING *;

-- name: RecordInvoicePayment :one
-- Payment progress follows the arithmetic rather than a caller's opinion.
UPDATE invoices
   SET paid_minor = paid_minor + sqlc.arg('amount_minor'),
       status = CASE
           WHEN paid_minor + credited_minor + sqlc.arg('amount_minor') >= total_minor THEN 'PAID'
           ELSE 'PARTIALLY_PAID'
       END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('ISSUED','PARTIALLY_PAID','OVERDUE')
RETURNING *;

-- name: RecordInvoiceCredit :one
UPDATE invoices
   SET credited_minor = credited_minor + sqlc.arg('amount_minor'),
       status = CASE
           WHEN paid_minor + credited_minor + sqlc.arg('amount_minor') >= total_minor THEN 'PAID'
           ELSE status
       END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('ISSUED','PARTIALLY_PAID','OVERDUE')
RETURNING *;

-- name: CancelInvoice :one
UPDATE invoices
   SET status = 'CANCELLED', cancel_reason = $3, cancelled_at = now()
 WHERE organization_id = $1 AND id = $2
   AND status IN ('DRAFT','ISSUED')
   AND paid_minor = 0
RETURNING *;

-- name: AttachInvoiceDocument :one
UPDATE invoices SET document_object_id = $3
 WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: ListInvoices :many
SELECT i.*, c.code AS customer_code, c.name AS customer_name
FROM invoices i
JOIN customers c ON c.id = i.customer_id
WHERE i.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status')::text)
  AND (sqlc.narg('customer_id')::bigint IS NULL OR i.customer_id = sqlc.narg('customer_id')::bigint)
  AND (sqlc.narg('billing_run_id')::bigint IS NULL OR i.billing_run_id = sqlc.narg('billing_run_id')::bigint)
  AND (sqlc.narg('from_date')::date IS NULL OR i.issue_date >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR i.issue_date <= sqlc.narg('to_date')::date)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR i.id < sqlc.narg('cursor_id')::bigint)
ORDER BY i.id DESC
LIMIT $2;

-- name: CountInvoices :one
SELECT count(*) FROM invoices i
WHERE i.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status')::text)
  AND (sqlc.narg('customer_id')::bigint IS NULL OR i.customer_id = sqlc.narg('customer_id')::bigint);

-- name: GetCustomerOutstanding :one
-- What a customer owes across every open invoice. A SUM, never a stored
-- balance (§25 applied to receivables as well as COD).
SELECT
    COALESCE(SUM(total_minor - paid_minor - credited_minor), 0)::bigint AS outstanding_minor,
    COALESCE(SUM(total_minor - paid_minor - credited_minor)
             FILTER (WHERE due_date < current_date), 0)::bigint AS overdue_minor,
    count(*)::bigint AS invoice_count
FROM invoices
WHERE organization_id = $1 AND customer_id = $2
  AND status IN ('ISSUED','PARTIALLY_PAID','OVERDUE');

-- name: MarkOverdueInvoices :exec
-- Run by the worker. Moves past-due invoices into OVERDUE so the collections
-- queue is a query rather than a date comparison in every caller.
UPDATE invoices
   SET status = 'OVERDUE'
 WHERE organization_id = $1
   AND status IN ('ISSUED','PARTIALLY_PAID')
   AND due_date < current_date;

-- ---------------------------------------------------------------------------
-- Invoice lines — snapshots
-- ---------------------------------------------------------------------------

-- name: CreateInvoiceLine :one
INSERT INTO invoice_lines (
    public_id, organization_id, invoice_id, line_no, line_type, description,
    hsn_sac_code, quantity, unit_price_minor, amount_minor, discount_minor,
    taxable_minor, tax_minor, total_minor, currency, shipment_id,
    charge_snapshot_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
RETURNING *;

-- name: DeleteInvoiceLines :exec
DELETE FROM invoice_lines WHERE invoice_id = $1;

-- name: ListInvoiceLines :many
SELECT l.*, s.awb
FROM invoice_lines l
LEFT JOIN shipments s ON s.id = l.shipment_id
WHERE l.invoice_id = $1
ORDER BY l.line_no;

-- name: SumInvoiceLines :one
SELECT
    COALESCE(SUM(amount_minor), 0)::bigint   AS amount_minor,
    COALESCE(SUM(discount_minor), 0)::bigint AS discount_minor,
    COALESCE(SUM(taxable_minor), 0)::bigint  AS taxable_minor,
    COALESCE(SUM(tax_minor), 0)::bigint      AS tax_minor,
    COALESCE(SUM(total_minor), 0)::bigint    AS total_minor,
    count(*)::bigint                          AS line_count
FROM invoice_lines
WHERE invoice_id = $1;

-- ---------------------------------------------------------------------------
-- Billable shipments
-- ---------------------------------------------------------------------------

-- name: ListBillableShipments :many
-- Delivered shipments for a customer in a window that have not been invoiced.
--
-- The charge snapshot is joined here rather than the rate card: the snapshot is
-- what the shipment was actually priced at, and it is immutable. Billing from a
-- live rate card would let a rate change alter an old invoice.
--
-- FOR UPDATE OF s so a concurrent billing run for the same customer blocks
-- rather than invoicing the same shipments twice.
SELECT s.id, s.public_id, s.awb, s.booked_at, s.chargeable_weight_grams,
       s.origin_pincode, s.destination_pincode, s.currency,
       cs.id AS charge_snapshot_id,
       cs.freight_minor, cs.surcharge_total_minor, cs.discount_total_minor,
       cs.taxable_minor, cs.tax_total_minor, cs.rounding_minor, cs.total_minor,
       cs.line_items
FROM shipments s
JOIN shipment_charge_snapshots cs ON cs.shipment_id = s.id
LEFT JOIN shipment_commercial_snapshots commercial ON commercial.shipment_id = s.id AND commercial.organization_id = s.organization_id
LEFT JOIN invoice_shipments inv ON inv.shipment_id = s.id
WHERE s.organization_id = sqlc.arg('organization_id')
  AND (CASE WHEN commercial.shipment_id IS NULL THEN s.customer_id ELSE commercial.transport_customer_id END) = sqlc.arg('customer_id')
  AND s.payment_mode IN ('CREDIT','PREPAID')
  AND s.current_status NOT IN ('CANCELLED')
  AND s.booked_at >= sqlc.arg('period_start')::timestamptz
  AND s.booked_at < sqlc.arg('period_end')::timestamptz
  AND inv.id IS NULL
ORDER BY s.booked_at, s.id
LIMIT sqlc.arg('limit')
FOR UPDATE OF s;

-- name: AddInvoiceShipment :one
INSERT INTO invoice_shipments (
    organization_id, invoice_id, shipment_id, awb, amount_minor, tax_minor
) VALUES ($1,$2,$3,$4,$5,$6)
RETURNING *;

-- name: ListInvoiceShipments :many
SELECT * FROM invoice_shipments WHERE invoice_id = $1 ORDER BY id;

-- name: DeleteInvoiceShipments :exec
DELETE FROM invoice_shipments WHERE invoice_id = $1;

-- ---------------------------------------------------------------------------
-- Tax components
-- ---------------------------------------------------------------------------

-- name: CreateTaxComponent :one
INSERT INTO tax_components (
    public_id, organization_id, invoice_id, invoice_line_id, credit_note_id,
    component_code, component_name, rate_bp, taxable_minor, tax_minor,
    currency, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: ListInvoiceTaxComponents :many
SELECT component_code, component_name, rate_bp,
       SUM(taxable_minor)::bigint AS taxable_minor,
       SUM(tax_minor)::bigint     AS tax_minor,
       currency
FROM tax_components
WHERE invoice_id = $1
GROUP BY component_code, component_name, rate_bp, currency
ORDER BY component_code;

-- name: DeleteInvoiceTaxComponents :exec
DELETE FROM tax_components WHERE invoice_id = $1;

-- ---------------------------------------------------------------------------
-- Billing runs
-- ---------------------------------------------------------------------------

-- name: CreateBillingRun :one
INSERT INTO billing_runs (
    public_id, organization_id, run_code, billing_cycle, period_start, period_end,
    status, customer_filter, currency, created_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,'PENDING',$7,$8,$9,$10)
RETURNING *;

-- name: GetBillingRunByPublicID :one
SELECT * FROM billing_runs WHERE organization_id = $1 AND public_id = $2;

-- name: StartBillingRun :one
UPDATE billing_runs
   SET status = 'RUNNING', started_at = now()
 WHERE organization_id = $1 AND id = $2 AND status = 'PENDING'
RETURNING *;

-- name: CompleteBillingRun :one
UPDATE billing_runs
   SET status = sqlc.arg('status'), completed_at = now(),
       invoice_count = sqlc.arg('invoice_count'),
       shipment_count = sqlc.arg('shipment_count'),
       total_minor = sqlc.arg('total_minor'),
       error_count = sqlc.arg('error_count'),
       errors = sqlc.arg('errors')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'RUNNING'
RETURNING *;

-- name: ListBillingRuns :many
SELECT * FROM billing_runs
WHERE organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT $2;

-- name: ListCustomersForBilling :many
-- Business accounts due for periodic billing on a cycle.
SELECT c.id, c.public_id, c.code, c.name, c.customer_type,
       b.id AS business_account_id, b.billing_cycle, b.payment_terms_days,
       b.currency, b.tax_metadata
FROM customers c
JOIN business_accounts b ON b.customer_id = c.id
WHERE c.organization_id = $1
  AND c.status = 'ACTIVE'
  AND b.status = 'ACTIVE'
  AND (sqlc.narg('billing_cycle')::text IS NULL
       OR b.billing_cycle = sqlc.narg('billing_cycle')::text)
  AND (sqlc.narg('customer_id')::bigint IS NULL OR c.id = sqlc.narg('customer_id')::bigint)
ORDER BY c.code;

-- ---------------------------------------------------------------------------
-- Credit and debit notes
-- ---------------------------------------------------------------------------

-- name: CreateCreditNote :one
INSERT INTO credit_notes (
    public_id, organization_id, note_number, note_type, invoice_id, customer_id,
    reason_code, reason, issue_date, currency, subtotal_minor, tax_minor,
    total_minor, status, tax_metadata, created_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'DRAFT',$14,$15,$16)
RETURNING *;

-- name: CreateCreditNoteLine :one
INSERT INTO credit_note_lines (
    organization_id, credit_note_id, line_no, description,
    amount_minor, tax_minor, total_minor, currency, invoice_line_id, shipment_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: GetCreditNoteByPublicID :one
SELECT n.*, c.code AS customer_code, c.name AS customer_name,
       i.invoice_number
FROM credit_notes n
JOIN customers c ON c.id = n.customer_id
LEFT JOIN invoices i ON i.id = n.invoice_id
WHERE n.organization_id = $1 AND n.public_id = $2;

-- name: IssueCreditNote :one
-- Maker/checker: whoever raised the note cannot be the one who issues it.
-- Enforced here and by the credit_notes_maker_is_not_checker CHECK.
UPDATE credit_notes
   SET status = 'ISSUED', note_number = sqlc.arg('note_number'),
       issued_at = now(), issued_by = sqlc.arg('issued_by'),
       approved_by = sqlc.arg('approved_by'), approved_at = now(),
       journal_transaction_id = sqlc.narg('journal_transaction_id')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'DRAFT'
   AND (created_by IS NULL OR created_by <> sqlc.arg('approved_by'))
RETURNING *;

-- name: ListCreditNoteLines :many
SELECT * FROM credit_note_lines WHERE credit_note_id = $1 ORDER BY line_no;

-- name: ListCreditNotes :many
SELECT n.*, c.code AS customer_code
FROM credit_notes n
JOIN customers c ON c.id = n.customer_id
WHERE n.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR n.status = sqlc.narg('status')::text)
  AND (sqlc.narg('note_type')::text IS NULL OR n.note_type = sqlc.narg('note_type')::text)
  AND (sqlc.narg('customer_id')::bigint IS NULL OR n.customer_id = sqlc.narg('customer_id')::bigint)
  AND (sqlc.narg('invoice_id')::bigint IS NULL OR n.invoice_id = sqlc.narg('invoice_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR n.id < sqlc.narg('cursor_id')::bigint)
ORDER BY n.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Invoice payments
-- ---------------------------------------------------------------------------

-- name: CreateInvoicePayment :one
INSERT INTO invoice_payments (
    public_id, organization_id, invoice_id, customer_id, payment_number,
    amount_minor, currency, payment_mode, reference, received_on,
    status, journal_transaction_id, recorded_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'RECORDED',$11,$12,$13)
RETURNING *;

-- name: ListInvoicePayments :many
SELECT p.*, u.full_name AS recorded_by_name
FROM invoice_payments p
LEFT JOIN users u ON u.id = p.recorded_by
WHERE p.invoice_id = $1
ORDER BY p.received_on DESC, p.id DESC;

-- name: SumInvoicePayments :one
SELECT COALESCE(SUM(amount_minor) FILTER (WHERE status IN ('RECORDED','CONFIRMED')), 0)::bigint
           AS paid_minor,
       count(*)::bigint AS payment_count
FROM invoice_payments
WHERE invoice_id = $1;

-- name: FindPaymentByReference :one
-- Idempotency probe for payment callbacks: a gateway retrying a webhook must
-- not bank the same payment twice.
SELECT * FROM invoice_payments
WHERE organization_id = $1 AND invoice_id = $2 AND reference = $3;
