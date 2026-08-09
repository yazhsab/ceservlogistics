-- Cash on delivery (M23).
--
-- COD is a custody chain before it is an accounting problem. Cash moves
-- consignee → agent → branch → franchise → head office, and at every hop
-- somebody is liable for it. The queries here are shaped around that: locks
-- where two people could claim the same cash, and explicit variance columns so
-- a shortfall is recorded rather than absorbed.

-- ---------------------------------------------------------------------------
-- Obligations
-- ---------------------------------------------------------------------------

-- name: CreateCODObligation :one
INSERT INTO cod_obligations (
    public_id, organization_id, shipment_id, awb, expected_minor, currency,
    consignor_customer_id, origin_franchise_id, destination_franchise_id,
    status, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE(sqlc.narg('status')::text,'EXPECTED'),$10)
RETURNING *;

-- name: GetObligationByShipment :one
SELECT * FROM cod_obligations WHERE organization_id = $1 AND shipment_id = $2;

-- name: GetObligationByPublicID :one
SELECT o.*, s.current_status AS shipment_status,
       f.code AS destination_franchise_code, f.name AS destination_franchise_name
FROM cod_obligations o
JOIN shipments s ON s.id = o.shipment_id
LEFT JOIN franchises f ON f.id = o.destination_franchise_id
WHERE o.organization_id = $1 AND o.public_id = $2;

-- name: GetObligationByID :one
SELECT * FROM cod_obligations WHERE organization_id = $1 AND id = $2;

-- name: LockObligation :one
-- Row lock before any custody move: two branches must not both be able to
-- declare the same cash.
SELECT * FROM cod_obligations
WHERE organization_id = $1 AND id = $2
FOR UPDATE;

-- name: LockObligationsByIDs :many
-- Locks a batch in a deterministic order, so two concurrent transfers over
-- overlapping sets deadlock-free serialise rather than interleave.
SELECT * FROM cod_obligations
WHERE organization_id = $1 AND id = ANY(sqlc.arg('ids')::bigint[])
ORDER BY id
FOR UPDATE;

-- name: MarkObligationCollected :one
-- Compare-and-swap on status: a replayed delivery cannot collect twice.
UPDATE cod_obligations
   SET status = 'AGENT_COLLECTED',
       collected_minor = collected_minor + sqlc.arg('amount_minor'),
       custodian_type = 'AGENT', custodian_id = sqlc.arg('agent_id'),
       collected_at = COALESCE(collected_at, sqlc.arg('collected_at')::timestamptz)
 WHERE organization_id = sqlc.arg('organization_id')
   AND id = sqlc.arg('id')
   AND status = 'EXPECTED'
RETURNING *;

-- name: MoveObligationCustody :one
-- The custody hop. Guarded on the expected current holder, so an out-of-order
-- acceptance is refused rather than silently rewriting the chain.
UPDATE cod_obligations
   SET status = sqlc.arg('to_status'),
       custodian_type = sqlc.arg('custodian_type'),
       custodian_id = sqlc.arg('custodian_id')
 WHERE organization_id = sqlc.arg('organization_id')
   AND id = sqlc.arg('id')
   AND status = sqlc.arg('from_status')
RETURNING *;

-- name: MarkObligationReconciled :one
UPDATE cod_obligations
   SET status = 'RECONCILED', reconciled_at = now(),
       adjusted_minor = adjusted_minor + sqlc.arg('adjusted_minor')
 WHERE organization_id = $1 AND id = $2
   AND status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED')
RETURNING *;

-- name: MarkObligationRemitted :one
UPDATE cod_obligations
   SET status = 'REMITTED', remitted_at = now(),
       remitted_minor = remitted_minor + sqlc.arg('amount_minor'),
       custodian_type = NULL, custodian_id = NULL
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('BRANCH_RECEIVED','FRANCHISE_CONFIRMED','RECONCILED')
RETURNING *;

-- name: CloseObligation :one
UPDATE cod_obligations
   SET status = 'CLOSED', closed_at = now()
 WHERE organization_id = $1 AND id = $2 AND status = 'REMITTED'
RETURNING *;

-- name: CancelObligation :one
UPDATE cod_obligations
   SET status = 'CANCELLED', cancel_reason = $3, closed_at = now()
 WHERE organization_id = $1 AND id = $2
   AND status IN ('EXPECTED','AGENT_COLLECTED')
RETURNING *;

-- name: WriteOffObligation :one
UPDATE cod_obligations
   SET status = 'WRITTEN_OFF', closed_at = now(),
       waived_minor = waived_minor + sqlc.arg('waived_minor')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status NOT IN ('CLOSED','CANCELLED','WRITTEN_OFF')
RETURNING *;

-- name: ListCODObligations :many
SELECT o.*, s.awb AS shipment_awb, s.current_status AS shipment_status
FROM cod_obligations o
JOIN shipments s ON s.id = o.shipment_id
WHERE o.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR o.status = sqlc.narg('status')::text)
  AND (sqlc.narg('custodian_type')::text IS NULL OR o.custodian_type = sqlc.narg('custodian_type')::text)
  AND (sqlc.narg('custodian_id')::bigint IS NULL OR o.custodian_id = sqlc.narg('custodian_id')::bigint)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR o.destination_franchise_id = sqlc.narg('franchise_id')::bigint)
  AND (sqlc.narg('awb')::text IS NULL OR o.awb = sqlc.narg('awb')::text)
  AND (sqlc.narg('from_date')::timestamptz IS NULL OR o.created_at >= sqlc.narg('from_date')::timestamptz)
  AND (sqlc.narg('to_date')::timestamptz IS NULL OR o.created_at <= sqlc.narg('to_date')::timestamptz)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR o.id < sqlc.narg('cursor_id')::bigint)
ORDER BY o.id DESC
LIMIT $2;

-- name: CountCODObligations :one
SELECT count(*) FROM cod_obligations o
WHERE o.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR o.status = sqlc.narg('status')::text)
  AND (sqlc.narg('custodian_type')::text IS NULL OR o.custodian_type = sqlc.narg('custodian_type')::text)
  AND (sqlc.narg('custodian_id')::bigint IS NULL OR o.custodian_id = sqlc.narg('custodian_id')::bigint)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR o.destination_franchise_id = sqlc.narg('franchise_id')::bigint);

-- name: SumCODInCustody :one
-- What one party is holding right now. Derived from the obligations they are
-- custodian of, never from a stored balance (§25).
SELECT
    COALESCE(SUM(collected_minor - remitted_minor), 0)::bigint AS held_minor,
    COALESCE(SUM(expected_minor), 0)::bigint                   AS expected_minor,
    count(*)::bigint                                            AS obligation_count
FROM cod_obligations
WHERE organization_id = $1 AND custodian_type = $2 AND custodian_id = $3
  AND status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED');

-- name: ListObligationsForParty :many
-- The hand-in queue: everything a party is holding, oldest first, so the
-- longest-held cash is settled first.
SELECT o.*, s.awb AS shipment_awb
FROM cod_obligations o
JOIN shipments s ON s.id = o.shipment_id
WHERE o.organization_id = $1 AND o.custodian_type = $2 AND o.custodian_id = $3
  AND o.status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED')
ORDER BY o.collected_at NULLS FIRST, o.id
LIMIT $4;

-- ---------------------------------------------------------------------------
-- Collections
-- ---------------------------------------------------------------------------

-- name: CreateCODCollection :one
INSERT INTO cod_collections (
    public_id, organization_id, obligation_id, shipment_id, delivery_attempt_id,
    amount_minor, currency, payment_mode, reference, collected_by, collected_at,
    operating_unit_id, journal_transaction_id, device_id, device_event_id, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
RETURNING *;

-- name: FindCollectionByAttempt :one
-- Idempotency probe: one collection per delivery attempt, mirroring the index.
SELECT * FROM cod_collections
WHERE organization_id = $1 AND delivery_attempt_id = $2;

-- name: FindCollectionByDeviceEvent :one
-- Offline replay probe, matching the Release 2 device-event scheme.
SELECT * FROM cod_collections
WHERE organization_id = $1 AND device_id = $2 AND device_event_id = $3;

-- name: ListCollectionsForObligation :many
SELECT * FROM cod_collections WHERE obligation_id = $1 ORDER BY collected_at, id;

-- name: ListCODCollections :many
SELECT c.*, s.awb, u.full_name AS collected_by_name
FROM cod_collections c
JOIN shipments s ON s.id = c.shipment_id
LEFT JOIN users u ON u.id = c.collected_by
WHERE c.organization_id = $1
  AND (sqlc.narg('operating_unit_id')::bigint IS NULL OR c.operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
  AND (sqlc.narg('collected_by')::bigint IS NULL OR c.collected_by = sqlc.narg('collected_by')::bigint)
  AND (sqlc.narg('payment_mode')::text IS NULL OR c.payment_mode = sqlc.narg('payment_mode')::text)
  AND (sqlc.narg('from_date')::timestamptz IS NULL OR c.collected_at >= sqlc.narg('from_date')::timestamptz)
  AND (sqlc.narg('to_date')::timestamptz IS NULL OR c.collected_at <= sqlc.narg('to_date')::timestamptz)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR c.id < sqlc.narg('cursor_id')::bigint)
ORDER BY c.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Custody transfers
-- ---------------------------------------------------------------------------

-- name: CreateCustodyTransfer :one
INSERT INTO cod_custody_transfers (
    public_id, organization_id, transfer_code, from_type, from_id, to_type, to_id,
    declared_minor, currency, shipment_count, status, declared_by, notes, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'DECLARED',$11,$12,$13)
RETURNING *;

-- name: AddTransferItem :one
INSERT INTO cod_custody_transfer_items (transfer_id, obligation_id, amount_minor, status)
VALUES ($1,$2,$3,COALESCE(sqlc.narg('status')::text,'INCLUDED'))
RETURNING *;

-- name: GetTransferByPublicID :one
SELECT t.*, du.full_name AS declared_by_name, au.full_name AS accepted_by_name
FROM cod_custody_transfers t
LEFT JOIN users du ON du.id = t.declared_by
LEFT JOIN users au ON au.id = t.accepted_by
WHERE t.organization_id = $1 AND t.public_id = $2;

-- name: LockTransfer :one
SELECT * FROM cod_custody_transfers
WHERE organization_id = $1 AND id = $2
FOR UPDATE;

-- name: AcceptCustodyTransfer :one
-- CAS on DECLARED: two receivers cannot both accept the same hand-in.
UPDATE cod_custody_transfers
   SET status = 'ACCEPTED', accepted_minor = sqlc.arg('accepted_minor'),
       accepted_by = sqlc.arg('accepted_by'), accepted_at = now(),
       variance_minor = sqlc.arg('variance_minor'),
       variance_reason = sqlc.narg('variance_reason'),
       journal_transaction_id = sqlc.narg('journal_transaction_id')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'DECLARED'
RETURNING *;

-- name: DisputeCustodyTransfer :one
UPDATE cod_custody_transfers
   SET status = 'DISPUTED', variance_minor = sqlc.arg('variance_minor'),
       variance_reason = sqlc.arg('variance_reason')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'DECLARED'
RETURNING *;

-- name: ListTransferItems :many
SELECT i.*, o.public_id AS obligation_public_id, o.awb, o.expected_minor
FROM cod_custody_transfer_items i
JOIN cod_obligations o ON o.id = i.obligation_id
WHERE i.transfer_id = $1
ORDER BY i.id;

-- name: MarkTransferItemsOutcome :exec
UPDATE cod_custody_transfer_items
   SET status = sqlc.arg('status')
 WHERE transfer_id = sqlc.arg('transfer_id')
   AND obligation_id = ANY(sqlc.arg('obligation_ids')::bigint[]);

-- name: ListCustodyTransfers :many
SELECT t.* FROM cod_custody_transfers t
WHERE t.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status')::text)
  AND (sqlc.narg('from_type')::text IS NULL OR t.from_type = sqlc.narg('from_type')::text)
  AND (sqlc.narg('from_id')::bigint IS NULL OR t.from_id = sqlc.narg('from_id')::bigint)
  AND (sqlc.narg('to_type')::text IS NULL OR t.to_type = sqlc.narg('to_type')::text)
  AND (sqlc.narg('to_id')::bigint IS NULL OR t.to_id = sqlc.narg('to_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR t.id < sqlc.narg('cursor_id')::bigint)
ORDER BY t.id DESC
LIMIT $2;

-- name: AllocateCODSequence :one
-- Shared atomic counter for transfer, reconciliation, remittance and dispute
-- codes. Same shape as every other sequence in the platform (§22).
INSERT INTO operational_sequences (organization_id, kind, scope_key, current_value)
VALUES (sqlc.arg('organization_id'), sqlc.arg('kind'), sqlc.arg('scope_key'), 1)
ON CONFLICT (organization_id, kind, scope_key)
DO UPDATE SET current_value = operational_sequences.current_value + 1, updated_at = now()
WHERE operational_sequences.current_value < operational_sequences.max_value
RETURNING current_value;

-- ---------------------------------------------------------------------------
-- Reconciliation
-- ---------------------------------------------------------------------------

-- name: CreateCODReconciliation :one
INSERT INTO cod_reconciliations (
    public_id, organization_id, reconciliation_code, party_type, party_id,
    period_start, period_end, expected_minor, currency, shipment_count,
    status, opened_by, notes
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'OPEN',$11,$12)
RETURNING *;

-- name: GetCODReconciliationByPublicID :one
SELECT r.*, ou.full_name AS opened_by_name, cu.full_name AS completed_by_name
FROM cod_reconciliations r
LEFT JOIN users ou ON ou.id = r.opened_by
LEFT JOIN users cu ON cu.id = r.completed_by
WHERE r.organization_id = $1 AND r.public_id = $2;

-- name: LockCODReconciliation :one
SELECT * FROM cod_reconciliations
WHERE organization_id = $1 AND id = $2
FOR UPDATE;

-- name: AddCODReconciliationItem :one
INSERT INTO cod_reconciliation_items (
    reconciliation_id, obligation_id, expected_minor, counted_minor, outcome, notes
) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (reconciliation_id, obligation_id)
DO UPDATE SET counted_minor = EXCLUDED.counted_minor,
              outcome = EXCLUDED.outcome,
              notes = EXCLUDED.notes
RETURNING *;

-- name: ListCODReconciliationItems :many
SELECT i.*, o.public_id AS obligation_public_id, o.awb
FROM cod_reconciliation_items i
JOIN cod_obligations o ON o.id = i.obligation_id
WHERE i.reconciliation_id = $1
ORDER BY i.id;

-- name: SumCODReconciliationItems :one
SELECT
    COALESCE(SUM(expected_minor), 0)::bigint AS expected_minor,
    COALESCE(SUM(counted_minor), 0)::bigint  AS counted_minor,
    COALESCE(SUM(GREATEST(expected_minor - counted_minor, 0)), 0)::bigint AS shortage_minor,
    COALESCE(SUM(GREATEST(counted_minor - expected_minor, 0)), 0)::bigint AS excess_minor,
    count(*)::bigint AS item_count
FROM cod_reconciliation_items
WHERE reconciliation_id = $1;

-- name: CompleteCODReconciliation :one
UPDATE cod_reconciliations
   SET status = sqlc.arg('status'),
       counted_minor = sqlc.arg('counted_minor'),
       shortage_minor = sqlc.arg('shortage_minor'),
       excess_minor = sqlc.arg('excess_minor'),
       expected_minor = sqlc.arg('expected_minor'),
       shipment_count = sqlc.arg('shipment_count'),
       variance_reason = sqlc.narg('variance_reason'),
       journal_transaction_id = sqlc.narg('journal_transaction_id'),
       completed_by = sqlc.arg('completed_by'), completed_at = now()
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('OPEN','COUNTED')
RETURNING *;

-- name: ListCODReconciliations :many
SELECT r.* FROM cod_reconciliations r
WHERE r.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text)
  AND (sqlc.narg('party_type')::text IS NULL OR r.party_type = sqlc.narg('party_type')::text)
  AND (sqlc.narg('party_id')::bigint IS NULL OR r.party_id = sqlc.narg('party_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR r.id < sqlc.narg('cursor_id')::bigint)
ORDER BY r.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Remittance
-- ---------------------------------------------------------------------------

-- name: CreateCODRemittance :one
INSERT INTO cod_remittances (
    public_id, organization_id, remittance_code, from_type, from_id,
    beneficiary_type, beneficiary_id, amount_minor, currency, shipment_count,
    payment_mode, reference, paid_on, status, created_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'INITIATED',$14,$15)
RETURNING *;

-- name: AddRemittanceItem :one
INSERT INTO cod_remittance_items (remittance_id, obligation_id, amount_minor)
VALUES ($1,$2,$3)
RETURNING *;

-- name: GetRemittanceByPublicID :one
SELECT r.*, cu.full_name AS created_by_name, fu.full_name AS confirmed_by_name
FROM cod_remittances r
LEFT JOIN users cu ON cu.id = r.created_by
LEFT JOIN users fu ON fu.id = r.confirmed_by
WHERE r.organization_id = $1 AND r.public_id = $2;

-- name: LockRemittance :one
SELECT * FROM cod_remittances WHERE organization_id = $1 AND id = $2 FOR UPDATE;

-- name: ConfirmCODRemittance :one
UPDATE cod_remittances
   SET status = 'CONFIRMED', confirmed_by = sqlc.arg('confirmed_by'), confirmed_at = now(),
       journal_transaction_id = sqlc.narg('journal_transaction_id')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'INITIATED'
RETURNING *;

-- name: FailCODRemittance :one
UPDATE cod_remittances
   SET status = 'FAILED', failure_reason = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'INITIATED'
RETURNING *;

-- name: ListRemittanceItems :many
SELECT i.*, o.public_id AS obligation_public_id, o.awb
FROM cod_remittance_items i
JOIN cod_obligations o ON o.id = i.obligation_id
WHERE i.remittance_id = $1
ORDER BY i.id;

-- name: ListCODRemittances :many
SELECT r.* FROM cod_remittances r
WHERE r.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text)
  AND (sqlc.narg('from_type')::text IS NULL OR r.from_type = sqlc.narg('from_type')::text)
  AND (sqlc.narg('from_id')::bigint IS NULL OR r.from_id = sqlc.narg('from_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR r.id < sqlc.narg('cursor_id')::bigint)
ORDER BY r.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Adjustments (maker/checker)
-- ---------------------------------------------------------------------------

-- name: CreateCODAdjustment :one
INSERT INTO cod_adjustments (
    public_id, organization_id, obligation_id, reconciliation_id, adjustment_type,
    amount_minor, currency, liable_party_type, liable_party_id, reason,
    status, requested_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'PENDING_APPROVAL',$11,$12)
RETURNING *;

-- name: GetAdjustmentByPublicID :one
SELECT a.*, ru.full_name AS requested_by_name, au.full_name AS approved_by_name
FROM cod_adjustments a
LEFT JOIN users ru ON ru.id = a.requested_by
LEFT JOIN users au ON au.id = a.approved_by
WHERE a.organization_id = $1 AND a.public_id = $2;

-- name: LockAdjustment :one
SELECT * FROM cod_adjustments WHERE organization_id = $1 AND id = $2 FOR UPDATE;

-- name: ApproveCODAdjustment :one
-- The approver must differ from the requester. Enforced here in the WHERE, and
-- again by the cod_adjustments_maker_is_not_checker CHECK constraint, so a
-- caller that bypasses the service still cannot self-approve.
UPDATE cod_adjustments
   SET status = 'APPROVED', approved_by = sqlc.arg('approved_by'), approved_at = now()
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'PENDING_APPROVAL'
   AND requested_by <> sqlc.arg('approved_by')
RETURNING *;

-- name: RejectCODAdjustment :one
UPDATE cod_adjustments
   SET status = 'REJECTED', rejection_reason = sqlc.arg('rejection_reason'),
       approved_by = sqlc.arg('approved_by'), approved_at = now()
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status = 'PENDING_APPROVAL'
   AND requested_by <> sqlc.arg('approved_by')
RETURNING *;

-- name: MarkAdjustmentPosted :one
UPDATE cod_adjustments
   SET status = 'POSTED', journal_transaction_id = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'APPROVED'
RETURNING *;

-- name: ListCODAdjustments :many
SELECT a.* FROM cod_adjustments a
WHERE a.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR a.status = sqlc.narg('status')::text)
  AND (sqlc.narg('obligation_id')::bigint IS NULL OR a.obligation_id = sqlc.narg('obligation_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR a.id < sqlc.narg('cursor_id')::bigint)
ORDER BY a.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Disputes
-- ---------------------------------------------------------------------------

-- name: CreateCODDispute :one
INSERT INTO cod_disputes (
    public_id, organization_id, obligation_id, dispute_code, raised_by_type,
    raised_by_id, disputed_minor, currency, category, description, status, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'OPEN',$11)
RETURNING *;

-- name: GetDisputeByPublicID :one
SELECT d.*, o.awb, u.full_name AS resolved_by_name
FROM cod_disputes d
JOIN cod_obligations o ON o.id = d.obligation_id
LEFT JOIN users u ON u.id = d.resolved_by
WHERE d.organization_id = $1 AND d.public_id = $2;

-- name: ResolveCODDispute :one
UPDATE cod_disputes
   SET status = sqlc.arg('status'), resolution = sqlc.arg('resolution'),
       resolved_minor = sqlc.narg('resolved_minor'),
       resolved_by = sqlc.arg('resolved_by'), resolved_at = now(),
       adjustment_id = sqlc.narg('adjustment_id')
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('OPEN','UNDER_REVIEW')
RETURNING *;

-- name: ListCODDisputes :many
SELECT d.*, o.awb FROM cod_disputes d
JOIN cod_obligations o ON o.id = d.obligation_id
WHERE d.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR d.status = sqlc.narg('status')::text)
  AND (sqlc.narg('obligation_id')::bigint IS NULL OR d.obligation_id = sqlc.narg('obligation_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR d.id < sqlc.narg('cursor_id')::bigint)
ORDER BY d.id DESC
LIMIT $2;

-- ---------------------------------------------------------------------------
-- Settlement sweep and dashboards
-- ---------------------------------------------------------------------------

-- name: SweepCODForSettlement :many
-- COD a franchise is liable for in a period, for the settlement statement.
-- FOR UPDATE so a concurrent settlement generation blocks rather than sweeping
-- the same obligations onto two statements.
SELECT * FROM cod_obligations
WHERE organization_id = $1
  AND destination_franchise_id = $2
  AND status IN ('FRANCHISE_CONFIRMED','RECONCILED')
  AND collected_at >= sqlc.arg('period_start')::timestamptz
  AND collected_at < sqlc.arg('period_end')::timestamptz
ORDER BY collected_at, id
FOR UPDATE;

-- name: GetCODSummary :one
-- The COD dashboard: one pass over obligations rather than a query per tile.
SELECT
    count(*) FILTER (WHERE status = 'EXPECTED')::bigint            AS expected_count,
    count(*) FILTER (WHERE status = 'AGENT_COLLECTED')::bigint     AS with_agent_count,
    count(*) FILTER (WHERE status = 'BRANCH_RECEIVED')::bigint     AS with_branch_count,
    count(*) FILTER (WHERE status = 'FRANCHISE_CONFIRMED')::bigint AS with_franchise_count,
    count(*) FILTER (WHERE status = 'REMITTED')::bigint            AS remitted_count,
    COALESCE(SUM(expected_minor) FILTER (WHERE status = 'EXPECTED'), 0)::bigint AS expected_minor,
    COALESCE(SUM(collected_minor - remitted_minor)
             FILTER (WHERE status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED')), 0)::bigint
        AS in_custody_minor,
    COALESCE(SUM(remitted_minor), 0)::bigint AS remitted_minor
FROM cod_obligations
WHERE organization_id = $1
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR destination_franchise_id = sqlc.narg('franchise_id')::bigint);
