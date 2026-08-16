-- Franchise settlement (M24).
--
-- A settlement is a period statement between head office and one franchise.
-- §26 requires it to be *reproducible*: running the calculation twice over the
-- same period must produce the same lines and the same net.
--
-- That is achieved by making the calculation a pure function of rows that are
-- themselves immutable — posted commission calculations and COD obligations —
-- and by giving every line a pointer back to its source. The queries here are
-- shaped to make that possible: the sweeps lock their inputs, and the line
-- source index refuses the same source row twice on one statement.

-- ---------------------------------------------------------------------------
-- Settlements
-- ---------------------------------------------------------------------------

-- name: CreateSettlement :one
INSERT INTO settlements (
    public_id, organization_id, settlement_number, franchise_id,
    period_type, period_start, period_end, status, currency,
    opening_balance_minor, created_by, request_id, notes
) VALUES ($1,$2,$3,$4,$5,$6,$7,'DRAFT',$8,$9,$10,$11,$12)
RETURNING *;

-- name: FindSettlementForPeriod :one
-- The idempotency probe, mirroring settlements_period_idx: one live settlement
-- per franchise per period, so a retried generation returns the original.
SELECT * FROM settlements
WHERE organization_id = $1 AND franchise_id = $2
  AND period_start = $3 AND period_end = $4
  AND status <> 'CANCELLED';

-- name: GetSettlementByPublicID :one
SELECT s.*, f.code AS franchise_code, f.name AS franchise_name,
       cu.full_name AS calculated_by_name, au.full_name AS approved_by_name
FROM settlements s
JOIN franchises f ON f.id = s.franchise_id
LEFT JOIN users cu ON cu.id = s.calculated_by
LEFT JOIN users au ON au.id = s.approved_by
WHERE s.organization_id = $1 AND s.public_id = $2;

-- name: GetSettlementByID :one
SELECT * FROM settlements WHERE organization_id = $1 AND id = $2;

-- name: LockSettlement :one
-- Row lock before recalculation, approval or payment: two finance users acting
-- on one statement must serialise.
SELECT * FROM settlements WHERE organization_id = $1 AND id = $2 FOR UPDATE;

-- name: GetPreviousSettlementBalance :one
-- The closing position of the franchise's most recent settled statement, which
-- becomes this one's opening balance. Unpaid amounts carry forward rather than
-- being forgotten.
SELECT COALESCE(net_amount_minor - paid_minor, 0)::bigint AS carry_forward_minor,
       settlement_number, period_end
FROM settlements
WHERE organization_id = $1 AND franchise_id = $2
  AND period_end < sqlc.arg('before')::date
  AND status IN ('APPROVED','PARTIALLY_PAID','PAID','CLOSED')
ORDER BY period_end DESC, id DESC
LIMIT 1;

-- name: ApplySettlementTotals :one
-- Writes the calculated header. Only legal while the statement is still being
-- worked on; the settlements_guard trigger refuses it afterwards.
UPDATE settlements
   SET status = 'CALCULATED',
       commission_minor = sqlc.arg('commission_minor'),
       incentive_minor = sqlc.arg('incentive_minor'),
       cod_liability_minor = sqlc.arg('cod_liability_minor'),
       charges_minor = sqlc.arg('charges_minor'),
       penalties_minor = sqlc.arg('penalties_minor'),
       adjustments_minor = sqlc.arg('adjustments_minor'),
       tax_minor = sqlc.arg('tax_minor'),
       withholding_minor = sqlc.arg('withholding_minor'),
       collections_minor = sqlc.arg('collections_minor'),
       opening_balance_minor = sqlc.arg('opening_balance_minor'),
       net_amount_minor = sqlc.arg('net_amount_minor'),
       calculation_hash = sqlc.arg('calculation_hash'),
       calculated_at = now(), calculated_by = sqlc.arg('calculated_by')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('DRAFT','CALCULATED')
RETURNING *;

-- name: SubmitSettlement :one
UPDATE settlements
   SET status = 'UNDER_REVIEW', submitted_at = now(), submitted_by = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'CALCULATED'
RETURNING *;

-- name: ApproveSettlement :one
-- Maker/checker: the approver must differ from whoever calculated it. Enforced
-- here and again by the settlements_maker_is_not_checker CHECK constraint.
UPDATE settlements
   SET status = 'APPROVED', approved_at = now(), approved_by = sqlc.arg('approved_by'),
       journal_transaction_id = sqlc.narg('journal_transaction_id')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('CALCULATED','UNDER_REVIEW')
   AND (calculated_by IS NULL OR calculated_by <> sqlc.arg('approved_by'))
RETURNING *;

-- name: RejectSettlement :one
-- Back to CALCULATED so it can be corrected and resubmitted.
UPDATE settlements
   SET status = 'CALCULATED', submitted_at = NULL, submitted_by = NULL
 WHERE organization_id = $1 AND id = $2 AND status = 'UNDER_REVIEW'
RETURNING *;

-- name: RecordSettlementPaid :one
-- Advances payment progress. The status follows the arithmetic rather than a
-- caller's opinion, so a part payment cannot be marked PAID.
UPDATE settlements
   SET paid_minor = paid_minor + sqlc.arg('amount_minor'),
       status = CASE
           WHEN paid_minor + sqlc.arg('amount_minor') >= abs(net_amount_minor) THEN 'PAID'
           ELSE 'PARTIALLY_PAID'
       END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('APPROVED','PARTIALLY_PAID')
RETURNING *;

-- name: CloseSettlement :one
UPDATE settlements
   SET status = 'CLOSED', closed_at = now()
 WHERE organization_id = $1 AND id = $2 AND status = 'PAID'
RETURNING *;

-- name: CancelSettlement :one
-- Only before approval. Afterwards a correction is an adjustment (§26).
UPDATE settlements
   SET status = 'CANCELLED', cancelled_at = now(), cancel_reason = $3
 WHERE organization_id = $1 AND id = $2
   AND status IN ('DRAFT','CALCULATED','UNDER_REVIEW')
RETURNING *;

-- name: ListSettlements :many
SELECT s.*, f.code AS franchise_code, f.name AS franchise_name
FROM settlements s
JOIN franchises f ON f.id = s.franchise_id
WHERE s.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR s.franchise_id = sqlc.narg('franchise_id')::bigint)
  AND (sqlc.narg('from_date')::date IS NULL OR s.period_end >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR s.period_end <= sqlc.narg('to_date')::date)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR s.id < sqlc.narg('cursor_id')::bigint)
ORDER BY s.id DESC
LIMIT $2;

-- name: CountSettlements :one
SELECT count(*) FROM settlements s
WHERE s.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR s.franchise_id = sqlc.narg('franchise_id')::bigint);

-- ---------------------------------------------------------------------------
-- Lines
-- ---------------------------------------------------------------------------

-- name: CreateSettlementLine :one
INSERT INTO settlement_lines (
    public_id, organization_id, settlement_id, line_no, category, description,
    amount_minor, currency, quantity, source_type, source_id, source_public_id,
    shipment_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: DeleteSettlementLines :exec
-- Clears the working lines before a recalculation. Only possible while the
-- settlement is DRAFT/CALCULATED/UNDER_REVIEW; the settlement_lines_guard
-- trigger refuses it once approved.
DELETE FROM settlement_lines
WHERE settlement_id = $1;

-- name: ListSettlementLines :many
SELECT l.*, s.awb
FROM settlement_lines l
LEFT JOIN shipments s ON s.id = l.shipment_id
WHERE l.settlement_id = $1
ORDER BY l.line_no;

-- name: SumSettlementLinesByCategory :many
SELECT category, COALESCE(SUM(amount_minor), 0)::bigint AS amount_minor, count(*)::bigint AS line_count
FROM settlement_lines
WHERE settlement_id = $1
GROUP BY category
ORDER BY category;

-- name: SumSettlementLines :one
SELECT COALESCE(SUM(amount_minor), 0)::bigint AS total_minor, count(*)::bigint AS line_count
FROM settlement_lines
WHERE settlement_id = $1;

-- ---------------------------------------------------------------------------
-- Adjustments — maker/checker
-- ---------------------------------------------------------------------------

-- name: CreateSettlementAdjustment :one
INSERT INTO settlement_adjustments (
    public_id, organization_id, settlement_id, adjustment_type,
    amount_minor, currency, reason, status, requested_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,'PENDING_APPROVAL',$8)
RETURNING *;

-- name: GetSettlementAdjustmentByPublicID :one
SELECT a.*, ru.full_name AS requested_by_name, au.full_name AS approved_by_name
FROM settlement_adjustments a
LEFT JOIN users ru ON ru.id = a.requested_by
LEFT JOIN users au ON au.id = a.approved_by
WHERE a.organization_id = $1 AND a.public_id = $2;

-- name: ApproveSettlementAdjustment :one
UPDATE settlement_adjustments
   SET status = 'APPROVED', approved_by = sqlc.arg('approved_by'), approved_at = now()
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'PENDING_APPROVAL'
   AND requested_by <> sqlc.arg('approved_by')
RETURNING *;

-- name: RejectSettlementAdjustment :one
-- The other half of the maker/checker pair. A rejection is as much a decision
-- as an approval and carries its own reason, so "why was this refused" has an
-- answer six months later.
--
-- The rejecter must also differ from the requester: somebody quietly withdrawing
-- their own correction leaves the same hole in the record as approving it.
UPDATE settlement_adjustments
   SET status = 'REJECTED', approved_by = sqlc.arg('rejected_by'), approved_at = now(),
       rejection_reason = sqlc.arg('rejection_reason')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'PENDING_APPROVAL'
   AND requested_by <> sqlc.arg('rejected_by')
RETURNING *;

-- name: MarkSettlementAdjustmentApplied :one
UPDATE settlement_adjustments
   SET status = 'APPLIED', journal_transaction_id = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'APPROVED'
RETURNING *;

-- name: ListPendingAdjustmentsForSettlement :many
SELECT * FROM settlement_adjustments
WHERE settlement_id = $1 AND status IN ('APPROVED','APPLIED')
ORDER BY id;

-- name: ListSettlementAdjustments :many
SELECT a.*, s.settlement_number, s.status AS settlement_status,
       ru.full_name AS requested_by_name, au.full_name AS approved_by_name
FROM settlement_adjustments a
JOIN settlements s ON s.id = a.settlement_id
LEFT JOIN users ru ON ru.id = a.requested_by
LEFT JOIN users au ON au.id = a.approved_by
WHERE a.organization_id = $1
  AND (sqlc.narg('settlement_id')::bigint IS NULL OR a.settlement_id = sqlc.narg('settlement_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR a.status = sqlc.narg('status')::text)
ORDER BY a.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Approvals — the review trail
-- ---------------------------------------------------------------------------

-- name: RecordSettlementApproval :one
INSERT INTO settlement_approvals (
    public_id, organization_id, settlement_id, action,
    net_amount_minor, currency, comment, actor_id, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: ListSettlementApprovals :many
SELECT a.*, u.full_name AS actor_name
FROM settlement_approvals a
LEFT JOIN users u ON u.id = a.actor_id
WHERE a.settlement_id = $1
ORDER BY a.acted_at DESC, a.id DESC;

-- ---------------------------------------------------------------------------
-- Payments
-- ---------------------------------------------------------------------------

-- name: CreateSettlementPayment :one
INSERT INTO settlement_payments (
    public_id, organization_id, settlement_id, payment_number, direction,
    amount_minor, currency, payment_mode, reference, paid_on, status,
    journal_transaction_id, recorded_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'RECORDED',$11,$12,$13)
RETURNING *;

-- name: ListSettlementPayments :many
SELECT p.*, u.full_name AS recorded_by_name
FROM settlement_payments p
LEFT JOIN users u ON u.id = p.recorded_by
WHERE p.settlement_id = $1
ORDER BY p.paid_on DESC, p.id DESC;

-- name: SumSettlementPayments :one
SELECT COALESCE(SUM(amount_minor) FILTER (WHERE status IN ('RECORDED','CONFIRMED')), 0)::bigint
           AS paid_minor,
       count(*)::bigint AS payment_count
FROM settlement_payments
WHERE settlement_id = $1;

-- name: AllocateSettlementNumber :one
INSERT INTO operational_sequences (organization_id, kind, scope_key, current_value)
VALUES (sqlc.arg('organization_id'), sqlc.arg('kind'), sqlc.arg('scope_key'), 1)
ON CONFLICT (organization_id, kind, scope_key)
DO UPDATE SET current_value = operational_sequences.current_value + 1, updated_at = now()
WHERE operational_sequences.current_value < operational_sequences.max_value
RETURNING current_value;

-- ---------------------------------------------------------------------------
-- Franchise lookup for the settlement run
-- ---------------------------------------------------------------------------

-- name: ListFranchisesForSettlement :many
-- Franchises with an active agreement, for a bulk settlement run. The
-- agreement supplies the settlement cycle and currency, so a franchise on a
-- monthly cycle is not swept into a weekly run.
SELECT f.id, f.public_id, f.code, f.name, f.category,
       a.settlement_cycle, a.currency, a.commission_plan_code
FROM franchises f
JOIN franchise_agreements a ON a.franchise_id = f.id AND a.status = 'ACTIVE'
WHERE f.organization_id = $1
  AND f.status = 'ACTIVE'
  AND (sqlc.narg('settlement_cycle')::text IS NULL
       OR a.settlement_cycle = sqlc.narg('settlement_cycle')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR f.id = sqlc.narg('franchise_id')::bigint)
ORDER BY f.code;
