-- Double-entry ledger (M22).
--
-- Balances are never stored. Every balance in this file is a SUM over
-- journal_entries restricted to POSTED transactions, which is what makes the
-- ledger self-consistent by construction: there is no cached number that can
-- drift from the entries.

-- ---------------------------------------------------------------------------
-- Accounting periods
-- ---------------------------------------------------------------------------

-- name: CreateAccountingPeriod :one
INSERT INTO accounting_periods (public_id, organization_id, code, starts_on, ends_on, status)
VALUES ($1, $2, $3, $4, $5, COALESCE(sqlc.narg('status')::text, 'OPEN'))
RETURNING *;

-- name: GetPeriodForDate :one
-- Resolves a posting date to its period. The exclusion constraint in 0020
-- guarantees at most one row can match.
SELECT * FROM accounting_periods
WHERE organization_id = $1 AND $2::date BETWEEN starts_on AND ends_on;

-- name: GetPeriodByPublicID :one
SELECT * FROM accounting_periods WHERE organization_id = $1 AND public_id = $2;

-- name: ListAccountingPeriods :many
SELECT * FROM accounting_periods
WHERE organization_id = $1
ORDER BY starts_on DESC
LIMIT $2;

-- name: CloseAccountingPeriod :one
-- Compare-and-swap on status: two concurrent closers cannot both succeed.
UPDATE accounting_periods
   SET status = 'CLOSED', closed_at = now(), closed_by = $3, close_reason = $4
 WHERE organization_id = $1 AND id = $2 AND status IN ('OPEN','CLOSING')
RETURNING *;

-- name: ReopenAccountingPeriod :one
UPDATE accounting_periods
   SET status = 'OPEN', closed_at = NULL, closed_by = NULL, close_reason = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'CLOSED'
RETURNING *;

-- ---------------------------------------------------------------------------
-- Chart of accounts
-- ---------------------------------------------------------------------------

-- name: CreateLedgerAccount :one
INSERT INTO ledger_accounts (
    public_id, organization_id, code, name, account_type, normal_balance, currency,
    party_type, party_id, parent_id, is_system, description, metadata, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: GetAccountByCode :one
SELECT * FROM ledger_accounts WHERE organization_id = $1 AND code = $2;

-- name: GetAccountByPublicID :one
SELECT * FROM ledger_accounts WHERE organization_id = $1 AND public_id = $2;

-- name: GetAccountByID :one
SELECT * FROM ledger_accounts WHERE organization_id = $1 AND id = $2;

-- name: FindPartyAccount :one
-- The subsidiary account for one counterparty under a control account.
SELECT * FROM ledger_accounts
WHERE organization_id = $1 AND parent_id = $2 AND party_type = $3 AND party_id = $4;

-- name: ListLedgerAccounts :many
SELECT * FROM ledger_accounts
WHERE organization_id = $1
  AND (sqlc.narg('account_type')::text IS NULL OR account_type = sqlc.narg('account_type')::text)
  AND (sqlc.narg('party_type')::text IS NULL OR party_type = sqlc.narg('party_type')::text)
  AND (sqlc.narg('is_active')::boolean IS NULL OR is_active = sqlc.narg('is_active')::boolean)
  AND (sqlc.narg('search')::text IS NULL
       OR code ILIKE '%' || sqlc.narg('search')::text || '%'
       OR name ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY code
LIMIT $2 OFFSET $3;

-- name: CountLedgerAccounts :one
SELECT count(*) FROM ledger_accounts
WHERE organization_id = $1
  AND (sqlc.narg('account_type')::text IS NULL OR account_type = sqlc.narg('account_type')::text)
  AND (sqlc.narg('party_type')::text IS NULL OR party_type = sqlc.narg('party_type')::text)
  AND (sqlc.narg('is_active')::boolean IS NULL OR is_active = sqlc.narg('is_active')::boolean)
  AND (sqlc.narg('search')::text IS NULL
       OR code ILIKE '%' || sqlc.narg('search')::text || '%'
       OR name ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: UpdateLedgerAccount :one
UPDATE ledger_accounts
   SET name = COALESCE(sqlc.narg('name')::text, name),
       description = COALESCE(sqlc.narg('description')::text, description),
       is_active = COALESCE(sqlc.narg('is_active')::boolean, is_active)
 WHERE organization_id = $1 AND id = $2
RETURNING *;

-- ---------------------------------------------------------------------------
-- Journal posting
-- ---------------------------------------------------------------------------

-- name: AllocateJournalNumber :one
-- Same atomic-UPSERT shape as AWB and operational codes: N concurrent callers
-- receive N distinct values without a process-local lock (§22).
INSERT INTO operational_sequences (organization_id, kind, scope_key, current_value)
VALUES (sqlc.arg('organization_id'), 'JRN', sqlc.arg('scope_key'), 1)
ON CONFLICT (organization_id, kind, scope_key)
DO UPDATE SET current_value = operational_sequences.current_value + 1, updated_at = now()
WHERE operational_sequences.current_value < operational_sequences.max_value
RETURNING current_value;

-- name: CreateJournalTransaction :one
INSERT INTO journal_transactions (
    public_id, organization_id, transaction_number, period_id, posting_date, status,
    currency, source_type, source_id, source_public_id, purpose, description, reason,
    reverses_id, total_minor, entry_count, posted_at, posted_by, created_by, request_id, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
RETURNING *;

-- name: CreateJournalEntry :one
INSERT INTO journal_entries (
    public_id, organization_id, transaction_id, account_id, line_no,
    debit_minor, credit_minor, currency,
    franchise_id, operating_unit_id, customer_id, shipment_id, memo, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: FindJournalBySource :one
-- The idempotency probe. Mirrors journal_transactions_source_idx exactly, so a
-- caller can check before inserting and get the same answer the index enforces.
SELECT * FROM journal_transactions
WHERE organization_id = $1 AND source_type = $2 AND source_id = $3
  AND purpose = $4 AND status <> 'REVERSED';

-- name: GetJournalByPublicID :one
SELECT t.*, p.code AS period_code, p.status AS period_status,
       u.full_name AS posted_by_name
FROM journal_transactions t
JOIN accounting_periods p ON p.id = t.period_id
LEFT JOIN users u ON u.id = t.posted_by
WHERE t.organization_id = $1 AND t.public_id = $2;

-- name: GetJournalByID :one
SELECT * FROM journal_transactions WHERE organization_id = $1 AND id = $2;

-- name: LockJournalForReversal :one
-- Row lock so two concurrent reversals of the same journal serialise; the
-- second sees status = 'REVERSED' and stops.
SELECT * FROM journal_transactions
WHERE organization_id = $1 AND id = $2
FOR UPDATE;

-- name: MarkJournalReversed :one
UPDATE journal_transactions
   SET status = 'REVERSED', reversed_by_id = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'POSTED'
RETURNING *;

-- name: ListJournalEntries :many
SELECT e.*, a.code AS account_code, a.name AS account_name,
       a.account_type, a.normal_balance
FROM journal_entries e
JOIN ledger_accounts a ON a.id = e.account_id
WHERE e.transaction_id = $1
ORDER BY e.line_no;

-- name: ListJournalTransactions :many
SELECT t.*, p.code AS period_code
FROM journal_transactions t
JOIN accounting_periods p ON p.id = t.period_id
WHERE t.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status')::text)
  AND (sqlc.narg('source_type')::text IS NULL OR t.source_type = sqlc.narg('source_type')::text)
  AND (sqlc.narg('period_id')::bigint IS NULL OR t.period_id = sqlc.narg('period_id')::bigint)
  AND (sqlc.narg('from_date')::date IS NULL OR t.posting_date >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR t.posting_date <= sqlc.narg('to_date')::date)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR t.id < sqlc.narg('cursor_id')::bigint)
ORDER BY t.id DESC
LIMIT $2;

-- name: CountJournalTransactions :one
SELECT count(*) FROM journal_transactions t
WHERE t.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status')::text)
  AND (sqlc.narg('source_type')::text IS NULL OR t.source_type = sqlc.narg('source_type')::text)
  AND (sqlc.narg('period_id')::bigint IS NULL OR t.period_id = sqlc.narg('period_id')::bigint)
  AND (sqlc.narg('from_date')::date IS NULL OR t.posting_date >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR t.posting_date <= sqlc.narg('to_date')::date);

-- ---------------------------------------------------------------------------
-- Balances and reports
--
-- Every query below counts transactions with status IN ('POSTED','REVERSED'),
-- not POSTED alone. That is deliberate and it matters.
--
-- A reversal does not delete the original: it posts an equal and opposite
-- transaction, and the original is flagged REVERSED so a reader knows it was
-- undone. Both sets of entries are real history and both belong in the books —
-- they cancel each other arithmetically.
--
-- Excluding REVERSED would remove the original's amount *and* count the
-- reversal's mirror, subtracting the amount twice and leaving the account at
-- the negative of where it started. The integration test
-- TestReversalLeavesTheOriginalIntactAndNetsToZero pins this down.
--
-- Only DRAFT is out of the books.
-- ---------------------------------------------------------------------------

-- name: GetAccountBalance :one
-- Signed by the account's normal balance, so an ASSET with more debits than
-- credits reports a positive number and so does a LIABILITY with more credits.
SELECT
    COALESCE(SUM(e.debit_minor), 0)::bigint  AS total_debit,
    COALESCE(SUM(e.credit_minor), 0)::bigint AS total_credit,
    CASE WHEN a.normal_balance = 'DEBIT'
         THEN COALESCE(SUM(e.debit_minor), 0) - COALESCE(SUM(e.credit_minor), 0)
         ELSE COALESCE(SUM(e.credit_minor), 0) - COALESCE(SUM(e.debit_minor), 0)
    END::bigint AS balance_minor,
    count(e.id)::bigint AS entry_count
FROM ledger_accounts a
LEFT JOIN journal_entries e ON e.account_id = a.id
LEFT JOIN journal_transactions t ON t.id = e.transaction_id AND t.status IN ('POSTED','REVERSED')
WHERE a.organization_id = $1 AND a.id = $2
  AND (e.id IS NULL OR t.id IS NOT NULL)
  AND (sqlc.narg('as_of')::date IS NULL OR t.posting_date <= sqlc.narg('as_of')::date)
GROUP BY a.id, a.normal_balance;

-- name: GetAccountStatement :many
-- Ledger statement with a running balance computed in the database, so the
-- caller cannot introduce a rounding or ordering error of its own.
SELECT
    e.id, e.public_id, e.debit_minor, e.credit_minor, e.currency, e.memo,
    e.shipment_id, e.franchise_id, e.customer_id,
    t.public_id AS transaction_public_id, t.transaction_number, t.posting_date,
    t.description, t.source_type, t.source_public_id, t.purpose,
    SUM(CASE WHEN a.normal_balance = 'DEBIT'
             THEN e.debit_minor - e.credit_minor
             ELSE e.credit_minor - e.debit_minor END)
        OVER (ORDER BY t.posting_date, e.id
              ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)::bigint AS running_balance_minor
FROM journal_entries e
JOIN journal_transactions t ON t.id = e.transaction_id
JOIN ledger_accounts a ON a.id = e.account_id
WHERE e.organization_id = $1 AND e.account_id = $2 AND t.status IN ('POSTED','REVERSED')
  AND (sqlc.narg('from_date')::date IS NULL OR t.posting_date >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR t.posting_date <= sqlc.narg('to_date')::date)
ORDER BY t.posting_date, e.id
LIMIT $3 OFFSET $4;

-- name: CountAccountStatement :one
SELECT count(*) FROM journal_entries e
JOIN journal_transactions t ON t.id = e.transaction_id
WHERE e.organization_id = $1 AND e.account_id = $2 AND t.status IN ('POSTED','REVERSED')
  AND (sqlc.narg('from_date')::date IS NULL OR t.posting_date >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR t.posting_date <= sqlc.narg('to_date')::date);

-- name: GetTrialBalance :many
-- Every account with its totals. The sum of the debit column must equal the sum
-- of the credit column across the whole result; if it does not, the ledger is
-- broken, which is exactly what the invariant test asserts.
SELECT
    a.id, a.public_id, a.code, a.name, a.account_type, a.normal_balance, a.currency,
    a.party_type, a.party_id,
    COALESCE(SUM(e.debit_minor), 0)::bigint  AS total_debit,
    COALESCE(SUM(e.credit_minor), 0)::bigint AS total_credit,
    CASE WHEN a.normal_balance = 'DEBIT'
         THEN COALESCE(SUM(e.debit_minor), 0) - COALESCE(SUM(e.credit_minor), 0)
         ELSE COALESCE(SUM(e.credit_minor), 0) - COALESCE(SUM(e.debit_minor), 0)
    END::bigint AS balance_minor
FROM ledger_accounts a
LEFT JOIN journal_entries e ON e.account_id = a.id
LEFT JOIN journal_transactions t ON t.id = e.transaction_id AND t.status IN ('POSTED','REVERSED')
    AND (sqlc.narg('as_of')::date IS NULL OR t.posting_date <= sqlc.narg('as_of')::date)
WHERE a.organization_id = $1
  AND (e.id IS NULL OR t.id IS NOT NULL)
  AND (sqlc.narg('include_zero')::boolean IS TRUE OR e.id IS NOT NULL)
GROUP BY a.id
ORDER BY a.code;

-- name: AssertLedgerBalanced :one
-- The whole-ledger invariant, as a single query: total debits must equal total
-- credits across every posted transaction in the organization. Used by the
-- invariant tests and available as an operational health check.
SELECT
    COALESCE(SUM(e.debit_minor), 0)::bigint  AS total_debit,
    COALESCE(SUM(e.credit_minor), 0)::bigint AS total_credit,
    (COALESCE(SUM(e.debit_minor), 0) - COALESCE(SUM(e.credit_minor), 0))::bigint AS difference,
    count(DISTINCT t.id)::bigint AS transaction_count
FROM journal_entries e
JOIN journal_transactions t ON t.id = e.transaction_id
WHERE e.organization_id = $1 AND t.status IN ('POSTED','REVERSED');

-- name: FindUnbalancedTransactions :many
-- Diagnostic: any posted transaction whose legs do not agree. The deferred
-- constraint trigger makes this impossible, so a non-empty result is a bug
-- report, and the invariant test asserts it stays empty.
SELECT t.id, t.public_id, t.transaction_number, t.posting_date,
       COALESCE(SUM(e.debit_minor), 0)::bigint  AS total_debit,
       COALESCE(SUM(e.credit_minor), 0)::bigint AS total_credit
FROM journal_transactions t
LEFT JOIN journal_entries e ON e.transaction_id = t.id
WHERE t.organization_id = $1 AND t.status IN ('POSTED','REVERSED')
GROUP BY t.id
HAVING COALESCE(SUM(e.debit_minor), 0) <> COALESCE(SUM(e.credit_minor), 0)
LIMIT 100;

-- name: GetPartyBalance :one
-- What one counterparty owes or is owed, across every account bound to it.
SELECT
    COALESCE(SUM(CASE WHEN a.normal_balance = 'DEBIT'
                      THEN e.debit_minor - e.credit_minor
                      ELSE e.credit_minor - e.debit_minor END), 0)::bigint AS balance_minor,
    count(e.id)::bigint AS entry_count
FROM ledger_accounts a
JOIN journal_entries e ON e.account_id = a.id
JOIN journal_transactions t ON t.id = e.transaction_id AND t.status IN ('POSTED','REVERSED')
WHERE a.organization_id = $1 AND a.party_type = $2 AND a.party_id = $3
  AND (sqlc.narg('as_of')::date IS NULL OR t.posting_date <= sqlc.narg('as_of')::date);

-- ---------------------------------------------------------------------------
-- Provisioning
-- ---------------------------------------------------------------------------

-- name: SeedChartOfAccounts :exec
-- Give a newly created organization its chart of accounts.
--
-- Without this the first posting fails with LEDGER_ACCOUNT_MISSING, which is a
-- bad first day for a tenant. The list matches what migration 0020 applied to
-- organizations that already existed; keeping both in SQL means there is one
-- definition of "the default chart", and the contract test asserts they agree.
INSERT INTO ledger_accounts (public_id, organization_id, code, name, account_type,
                             normal_balance, currency, is_system, description, metadata)
SELECT gen_seed_public_id('lac'), o.id, v.code, v.name, v.account_type,
       v.normal_balance, o.currency, true, v.description, v.metadata::jsonb
FROM organizations o
CROSS JOIN (VALUES
    ('1000', 'Cash and Bank',                 'ASSET',     'DEBIT',  'Head-office cash and bank balances.', '{}'),
    ('1100', 'COD Receivable from Agents',    'ASSET',     'DEBIT',  'Cash collected at the doorstep and not yet handed in.', '{"subsidiaryParty":"AGENT"}'),
    ('1110', 'COD Receivable from Branches',  'ASSET',     'DEBIT',  'Cash held at a branch and not yet remitted.', '{"subsidiaryParty":"OPERATING_UNIT"}'),
    ('1120', 'COD Receivable from Franchises','ASSET',     'DEBIT',  'Cash confirmed by a franchise and not yet remitted.', '{"subsidiaryParty":"FRANCHISE"}'),
    ('1200', 'Trade Receivable from Customers','ASSET',    'DEBIT',  'Invoiced amounts owed by billed customers.', '{"subsidiaryParty":"CUSTOMER"}'),
    ('1300', 'Franchise Receivable',          'ASSET',     'DEBIT',  'Net amounts owed to head office by a franchise.', '{"subsidiaryParty":"FRANCHISE"}'),
    ('1400', 'COD Shortage Recoverable',      'ASSET',     'DEBIT',  'Shortfalls under recovery from the responsible party.', '{}'),
    ('2000', 'COD Payable to Consignors',     'LIABILITY', 'CREDIT', 'COD collected and owed to the sender.', '{}'),
    ('2100', 'Commission Payable',            'LIABILITY', 'CREDIT', 'Commission earned by franchises and not yet settled.', '{}'),
    ('2200', 'Franchise Payable',             'LIABILITY', 'CREDIT', 'Net amounts owed by head office to a franchise.', '{"subsidiaryParty":"FRANCHISE"}'),
    ('2300', 'Tax Payable',                   'LIABILITY', 'CREDIT', 'Output tax collected and owed to the authority.', '{}'),
    ('2310', 'Withholding Tax Payable',       'LIABILITY', 'CREDIT', 'Tax withheld from franchise settlements.', '{}'),
    ('2400', 'Customer Advances',             'LIABILITY', 'CREDIT', 'Prepaid balances held on account.', '{}'),
    ('2500', 'COD Excess Payable',            'LIABILITY', 'CREDIT', 'Over-collections owed back.', '{}'),
    ('3000', 'Retained Earnings',             'EQUITY',    'CREDIT', 'Accumulated result.', '{}'),
    ('3100', 'Opening Balance Equity',        'EQUITY',    'CREDIT', 'Counterpart for opening balances at go-live.', '{}'),
    ('4000', 'Freight Revenue',               'REVENUE',   'CREDIT', 'Transportation charges earned.', '{}'),
    ('4100', 'Surcharge Revenue',             'REVENUE',   'CREDIT', 'Fuel, remote-area and handling surcharges.', '{}'),
    ('4200', 'COD Service Fee Revenue',       'REVENUE',   'CREDIT', 'Fees charged for collecting COD.', '{}'),
    ('4300', 'Other Operating Revenue',       'REVENUE',   'CREDIT', 'Everything not captured above.', '{}'),
    ('5000', 'Commission Expense',            'EXPENSE',   'DEBIT',  'Commission earned by the network.', '{}'),
    ('5100', 'Incentive Expense',             'EXPENSE',   'DEBIT',  'Volume and performance incentives.', '{}'),
    ('5200', 'COD Shortage Written Off',      'EXPENSE',   'DEBIT',  'Irrecoverable COD shortfalls.', '{}'),
    ('5300', 'Discount Allowed',              'EXPENSE',   'DEBIT',  'Discounts and credit notes issued.', '{}'),
    ('5400', 'Penalty Income Offset',         'EXPENSE',   'DEBIT',  'Penalties refunded or waived.', '{}')
) AS v(code, name, account_type, normal_balance, description, metadata)
WHERE o.id = sqlc.arg('organization_id')
ON CONFLICT DO NOTHING;

-- name: SeedCurrentAccountingPeriod :exec
-- An open period for the month containing the given date, so a fresh tenant can
-- post immediately. Later periods are created on demand by EnsurePeriod.
INSERT INTO accounting_periods (public_id, organization_id, code, starts_on, ends_on, status)
SELECT gen_seed_public_id('acp'), sqlc.arg('organization_id'),
       to_char(sqlc.arg('as_of')::date, 'YYYY-MM'),
       date_trunc('month', sqlc.arg('as_of')::date)::date,
       (date_trunc('month', sqlc.arg('as_of')::date) + interval '1 month - 1 day')::date,
       'OPEN'
ON CONFLICT DO NOTHING;

-- name: SeedDefaultCommissionScheme :exec
-- A default scheme so commission rules have somewhere to live on day one.
INSERT INTO commission_schemes (public_id, organization_id, code, name, description,
                                currency, is_default, status)
SELECT gen_seed_public_id('csm'), o.id, 'STANDARD', 'Standard franchise scheme',
       'Default commission scheme created at provisioning.', o.currency, true, 'ACTIVE'
FROM organizations o
WHERE o.id = sqlc.arg('organization_id')
ON CONFLICT DO NOTHING;

-- name: SeedInvoiceSequences :exec
-- The statutory numbering series. Without these the first invoice cannot be
-- numbered, and numbering is not something to improvise at issue time.
INSERT INTO invoice_sequences (organization_id, series_code, document_type, prefix, scope_key, padding)
SELECT sqlc.arg('organization_id'), 'DEFAULT', d.doc, d.prefix, '', 6
FROM (VALUES ('INVOICE','INV'), ('CREDIT_NOTE','CRN'), ('DEBIT_NOTE','DBN')) AS d(doc, prefix)
ON CONFLICT DO NOTHING;

-- name: GetAccountRollupBalance :one
-- A control account's balance including its subsidiary accounts.
--
-- Postings never roll up: a leg lands on exactly one account, and a subsidiary
-- account's balance is its own. That is correct double-entry — the subsidiary
-- ledger carries the per-party detail — but it means the control account itself
-- reads zero once subsidiaries exist.
--
-- Finance still needs "what is our total COD exposure?", which is the control
-- account *and* everything beneath it. This query answers that without
-- introducing a stored rollup that could drift.
SELECT
    COALESCE(SUM(e.debit_minor), 0)::bigint  AS total_debit,
    COALESCE(SUM(e.credit_minor), 0)::bigint AS total_credit,
    CASE WHEN parent.normal_balance = 'DEBIT'
         THEN COALESCE(SUM(e.debit_minor), 0) - COALESCE(SUM(e.credit_minor), 0)
         ELSE COALESCE(SUM(e.credit_minor), 0) - COALESCE(SUM(e.debit_minor), 0)
    END::bigint AS balance_minor,
    count(DISTINCT a.id)::bigint AS account_count
FROM ledger_accounts parent
JOIN ledger_accounts a
  ON a.organization_id = parent.organization_id
 AND (a.id = parent.id OR a.parent_id = parent.id)
LEFT JOIN journal_entries e ON e.account_id = a.id
LEFT JOIN journal_transactions t
  ON t.id = e.transaction_id AND t.status IN ('POSTED','REVERSED')
WHERE parent.organization_id = $1 AND parent.code = $2
  AND (e.id IS NULL OR t.id IS NOT NULL)
  AND (sqlc.narg('as_of')::date IS NULL OR t.posting_date <= sqlc.narg('as_of')::date)
GROUP BY parent.id, parent.normal_balance;
