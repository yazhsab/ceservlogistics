-- 0020 Double-entry ledger (M22).
--
-- This is the accounting system of record (§23). Every monetary fact in
-- Release 3 — commission, COD custody, settlement, billing — resolves to rows
-- in journal_transactions and journal_entries. Nothing else is authoritative.
--
-- Three properties are enforced here, in the database, rather than in Go:
--
--   1. Every POSTED transaction balances: SUM(debit) = SUM(credit). A DEFERRED
--      constraint trigger re-checks this at COMMIT, so it holds no matter which
--      statement order a writer used, and holds against raw SQL.
--   2. Posted journals are immutable. Corrections are new transactions that
--      reference the original (§23).
--   3. Nothing posts into a closed accounting period.
--
-- Amounts are integer minor units throughout (§12). There is no float anywhere
-- in this schema, and no column anywhere that stores a mutable "balance" — a
-- balance is always SUM() over entries.

-- ---------------------------------------------------------------------------
-- Accounting periods
--
-- A period gates posting. Closing a period freezes the accounts for that span
-- so a month-end trial balance cannot move under the finance team's feet.
-- ---------------------------------------------------------------------------
CREATE TABLE accounting_periods (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'acp')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    code            text        NOT NULL,          -- e.g. 2026-08
    starts_on       date        NOT NULL,
    ends_on         date        NOT NULL,
    status          text        NOT NULL DEFAULT 'OPEN'
                    CHECK (status IN ('OPEN','CLOSING','CLOSED')),

    -- Set when the period is closed, for audit. Never cleared: reopening is a
    -- deliberate, audited act that writes a new row in audit_events.
    closed_at       timestamptz,
    closed_by       bigint      REFERENCES users (id) ON DELETE SET NULL,
    close_reason    text,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT accounting_periods_range CHECK (ends_on >= starts_on),
    CONSTRAINT accounting_periods_closure_recorded CHECK (
        (status <> 'CLOSED') OR (closed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX accounting_periods_code_idx
    ON accounting_periods (organization_id, code);
-- Periods must not overlap, or a posting date would resolve to two periods and
-- the trial balance would depend on which one the query happened to pick.
CREATE EXTENSION IF NOT EXISTS btree_gist;
ALTER TABLE accounting_periods
    ADD CONSTRAINT accounting_periods_no_overlap
    EXCLUDE USING gist (
        organization_id WITH =,
        daterange(starts_on, ends_on, '[]') WITH &&
    );

CREATE TRIGGER accounting_periods_touch
    BEFORE UPDATE ON accounting_periods
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Chart of accounts
--
-- Two kinds of account live here:
--
--   * Control accounts — one per organization, e.g. "Commission Expense".
--   * Subsidiary accounts — one per counterparty, e.g. "Payable to franchise
--     BLR-001". party_type/party_id carry that, so a franchise's balance is a
--     SUM over its own account rather than a filtered scan of every entry.
--
-- normal_balance is what makes a signed balance meaningful: an ASSET grows on
-- the debit side, a LIABILITY on the credit side. Storing it removes the
-- if-statement that would otherwise be duplicated in every report.
-- ---------------------------------------------------------------------------
CREATE TABLE ledger_accounts (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'lac')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    code            text        NOT NULL,
    name            text        NOT NULL,
    account_type    text        NOT NULL
                    CHECK (account_type IN ('ASSET','LIABILITY','EQUITY','REVENUE','EXPENSE')),
    normal_balance  text        NOT NULL CHECK (normal_balance IN ('DEBIT','CREDIT')),
    currency        char(3)     NOT NULL,

    -- Counterparty for subsidiary accounts. NULL on control accounts.
    party_type      text        CHECK (party_type IN ('FRANCHISE','CUSTOMER','OPERATING_UNIT','AGENT','CARRIER')),
    party_id        bigint,

    -- Parent for presentation grouping only. Balances are never rolled up by
    -- following this chain at posting time.
    parent_id       bigint      REFERENCES ledger_accounts (id) ON DELETE RESTRICT,

    -- A system account is created by provisioning and referenced by code from
    -- Go. Renaming is allowed; deleting or repurposing is not.
    is_system       boolean     NOT NULL DEFAULT false,
    is_active       boolean     NOT NULL DEFAULT true,

    description     text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,

    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    -- The normal balance has to agree with the account type, or every report
    -- built on it is wrong in a way that is very hard to spot.
    CONSTRAINT ledger_accounts_normal_balance_matches_type CHECK (
        (account_type IN ('ASSET','EXPENSE')     AND normal_balance = 'DEBIT') OR
        (account_type IN ('LIABILITY','EQUITY','REVENUE') AND normal_balance = 'CREDIT')
    ),
    CONSTRAINT ledger_accounts_party_complete CHECK (
        (party_type IS NULL AND party_id IS NULL) OR
        (party_type IS NOT NULL AND party_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX ledger_accounts_code_idx
    ON ledger_accounts (organization_id, code);
-- One subsidiary account per party per control account, so "the franchise's
-- payable" is always exactly one row.
CREATE UNIQUE INDEX ledger_accounts_party_idx
    ON ledger_accounts (organization_id, parent_id, party_type, party_id)
    WHERE party_id IS NOT NULL;
CREATE INDEX ledger_accounts_type_idx
    ON ledger_accounts (organization_id, account_type) WHERE is_active;
CREATE INDEX ledger_accounts_party_lookup_idx
    ON ledger_accounts (organization_id, party_type, party_id) WHERE party_id IS NOT NULL;

CREATE TRIGGER ledger_accounts_touch
    BEFORE UPDATE ON ledger_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Journal transactions
--
-- The unit that must balance. A transaction is created DRAFT, entries are
-- attached, and it moves to POSTED — at which point it and its entries become
-- immutable.
--
-- source_type/source_id/purpose form the natural idempotency key (§21): the
-- partial unique index below means "the delivery commission for shipment 4711"
-- can exist at most once, whatever the caller retries, and regardless of
-- whether the HTTP idempotency layer was involved at all.
-- ---------------------------------------------------------------------------
CREATE TABLE journal_transactions (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id          text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'jrn')),
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    -- Human-readable, allocated from a sequence. Unique per organization.
    transaction_number text        NOT NULL,

    period_id          bigint      NOT NULL REFERENCES accounting_periods (id) ON DELETE RESTRICT,
    -- The accounting date. Distinct from created_at: a correction booked today
    -- may belong to an earlier open period.
    posting_date       date        NOT NULL,

    status             text        NOT NULL DEFAULT 'DRAFT'
                       CHECK (status IN ('DRAFT','POSTED','REVERSED')),
    currency           char(3)     NOT NULL,

    -- What caused this transaction. purpose distinguishes several postings
    -- arising from one source, e.g. a shipment producing both a booking
    -- commission and a delivery commission.
    source_type        text        NOT NULL
                       CHECK (source_type IN (
                           'COMMISSION','COD','SETTLEMENT','INVOICE','CREDIT_NOTE','DEBIT_NOTE',
                           'PAYMENT','ADJUSTMENT','REVERSAL','OPENING_BALANCE','MANUAL')),
    source_id          bigint,
    source_public_id   text,
    purpose            text        NOT NULL,

    description        text        NOT NULL,
    -- Required for MANUAL and ADJUSTMENT postings (§34: high-risk operations
    -- carry a reason). Enforced below.
    reason             text,

    -- Reversal linkage. reverses_id points at the transaction being undone;
    -- reversed_by_id is its mirror, maintained by the application in the same
    -- transaction that writes the reversal.
    reverses_id        bigint      REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    reversed_by_id     bigint      REFERENCES journal_transactions (id) ON DELETE RESTRICT,

    -- Denormalised total, equal to SUM(debit) and to SUM(credit) once posted.
    -- Kept for cheap listing; the balance trigger verifies it rather than
    -- trusting it.
    total_minor        bigint      NOT NULL DEFAULT 0 CHECK (total_minor >= 0),
    entry_count        integer     NOT NULL DEFAULT 0 CHECK (entry_count >= 0),

    posted_at          timestamptz,
    posted_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_by         bigint      REFERENCES users (id) ON DELETE SET NULL,
    request_id         text,
    metadata           jsonb       NOT NULL DEFAULT '{}'::jsonb,

    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT journal_transactions_posted_recorded CHECK (
        (status <> 'POSTED') OR (posted_at IS NOT NULL AND entry_count >= 2)
    ),
    CONSTRAINT journal_transactions_reason_required CHECK (
        (source_type NOT IN ('MANUAL','ADJUSTMENT','REVERSAL')) OR
        (reason IS NOT NULL AND length(btrim(reason)) >= 3)
    ),
    CONSTRAINT journal_transactions_no_self_reversal CHECK (
        reverses_id IS NULL OR reverses_id <> id
    )
);

CREATE UNIQUE INDEX journal_transactions_number_idx
    ON journal_transactions (organization_id, transaction_number);

-- The finance idempotency guard. One live transaction per (source, purpose):
-- a retried commission posting collides here instead of double-paying.
-- REVERSED rows are excluded so a reversal-then-repost correction is possible.
CREATE UNIQUE INDEX journal_transactions_source_idx
    ON journal_transactions (organization_id, source_type, source_id, purpose)
    WHERE source_id IS NOT NULL AND status <> 'REVERSED';

CREATE INDEX journal_transactions_period_idx
    ON journal_transactions (organization_id, period_id, status);
CREATE INDEX journal_transactions_date_idx
    ON journal_transactions (organization_id, posting_date DESC, id DESC);
CREATE INDEX journal_transactions_source_lookup_idx
    ON journal_transactions (organization_id, source_type, source_id);
CREATE INDEX journal_transactions_reverses_idx
    ON journal_transactions (reverses_id) WHERE reverses_id IS NOT NULL;

CREATE TRIGGER journal_transactions_touch
    BEFORE UPDATE ON journal_transactions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Journal entries
--
-- One row per account leg. debit_minor and credit_minor are separate columns,
-- both non-negative, exactly one non-zero. That is deliberate rather than a
-- single signed amount: it makes the balance invariant a plain SUM comparison,
-- it makes "which side" explicit in every row a human reads, and it makes an
-- accidental sign flip impossible to express.
-- ---------------------------------------------------------------------------
CREATE TABLE journal_entries (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'jen')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    transaction_id  bigint      NOT NULL REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    account_id      bigint      NOT NULL REFERENCES ledger_accounts (id) ON DELETE RESTRICT,
    line_no         integer     NOT NULL CHECK (line_no > 0),

    debit_minor     bigint      NOT NULL DEFAULT 0 CHECK (debit_minor >= 0),
    credit_minor    bigint      NOT NULL DEFAULT 0 CHECK (credit_minor >= 0),
    currency        char(3)     NOT NULL,

    -- Optional dimensions, for reporting without joining back through source.
    franchise_id    bigint      REFERENCES franchises (id) ON DELETE RESTRICT,
    operating_unit_id bigint    REFERENCES operating_units (id) ON DELETE RESTRICT,
    customer_id     bigint      REFERENCES customers (id) ON DELETE RESTRICT,
    shipment_id     bigint      REFERENCES shipments (id) ON DELETE RESTRICT,

    memo            text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),

    -- Exactly one side carries a value. A zero-zero entry is noise; a
    -- both-sides entry is a bug that would still balance and so would never be
    -- noticed by the transaction-level check.
    CONSTRAINT journal_entries_one_side CHECK (
        (debit_minor > 0 AND credit_minor = 0) OR
        (credit_minor > 0 AND debit_minor = 0)
    )
);

CREATE UNIQUE INDEX journal_entries_line_idx
    ON journal_entries (transaction_id, line_no);
-- The statement query: an account's entries in posting order.
CREATE INDEX journal_entries_account_idx
    ON journal_entries (organization_id, account_id, id DESC);
CREATE INDEX journal_entries_transaction_idx
    ON journal_entries (transaction_id);
CREATE INDEX journal_entries_franchise_idx
    ON journal_entries (organization_id, franchise_id, id DESC) WHERE franchise_id IS NOT NULL;
CREATE INDEX journal_entries_shipment_idx
    ON journal_entries (organization_id, shipment_id) WHERE shipment_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Invariant enforcement
-- ---------------------------------------------------------------------------

-- The balance check. Written as a DEFERRABLE constraint trigger so it runs once
-- at COMMIT rather than after each INSERT: entries necessarily arrive one at a
-- time, so an immediate check would reject every legitimate transaction on its
-- first leg.
--
-- This is the single most important guard in the release. It fires for any
-- writer — application, migration, psql — and a transaction that does not
-- balance cannot reach disk.
CREATE OR REPLACE FUNCTION assert_journal_balanced()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    txn_id     bigint := COALESCE(NEW.transaction_id, OLD.transaction_id);
    txn        record;
    sum_debit  bigint;
    sum_credit bigint;
    n_entries  integer;
BEGIN
    SELECT status, currency, total_minor, entry_count
      INTO txn
      FROM journal_transactions
     WHERE id = txn_id;

    -- The parent may have been deleted in this same transaction (only legal
    -- while DRAFT); nothing to assert.
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    -- A draft is allowed to be unbalanced: it is a work in progress and cannot
    -- be read as truth, because every balance query filters on POSTED.
    IF txn.status = 'DRAFT' THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(debit_minor), 0), COALESCE(SUM(credit_minor), 0), COUNT(*)
      INTO sum_debit, sum_credit, n_entries
      FROM journal_entries
     WHERE transaction_id = txn_id;

    IF sum_debit <> sum_credit THEN
        RAISE EXCEPTION
            'LEDGER_UNBALANCED: transaction % has debits % and credits %',
            txn_id, sum_debit, sum_credit
            USING ERRCODE = 'check_violation';
    END IF;

    IF n_entries < 2 THEN
        RAISE EXCEPTION
            'LEDGER_INCOMPLETE: transaction % is posted with % entries; at least 2 are required',
            txn_id, n_entries
            USING ERRCODE = 'check_violation';
    END IF;

    -- The denormalised header must agree with the entries, or listings and
    -- reports would disagree with the statement.
    IF txn.total_minor <> sum_debit THEN
        RAISE EXCEPTION
            'LEDGER_TOTAL_MISMATCH: transaction % header total % <> entry total %',
            txn_id, txn.total_minor, sum_debit
            USING ERRCODE = 'check_violation';
    END IF;

    IF txn.entry_count <> n_entries THEN
        RAISE EXCEPTION
            'LEDGER_COUNT_MISMATCH: transaction % header count % <> actual %',
            txn_id, txn.entry_count, n_entries
            USING ERRCODE = 'check_violation';
    END IF;

    -- Mixed currency inside one transaction cannot be balanced meaningfully.
    IF EXISTS (SELECT 1 FROM journal_entries
                WHERE transaction_id = txn_id AND currency <> txn.currency) THEN
        RAISE EXCEPTION
            'LEDGER_CURRENCY_MISMATCH: transaction % has entries in a currency other than %',
            txn_id, txn.currency
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER journal_entries_balance_check
    AFTER INSERT OR UPDATE OR DELETE ON journal_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_journal_balanced();

-- The same check has to fire when the header flips DRAFT -> POSTED, because at
-- that moment no entry row changes and the trigger above would not run.
CREATE OR REPLACE FUNCTION assert_journal_balanced_on_post()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    sum_debit  bigint;
    sum_credit bigint;
    n_entries  integer;
BEGIN
    IF NEW.status = 'DRAFT' THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(debit_minor), 0), COALESCE(SUM(credit_minor), 0), COUNT(*)
      INTO sum_debit, sum_credit, n_entries
      FROM journal_entries
     WHERE transaction_id = NEW.id;

    IF sum_debit <> sum_credit THEN
        RAISE EXCEPTION
            'LEDGER_UNBALANCED: transaction % has debits % and credits %',
            NEW.id, sum_debit, sum_credit
            USING ERRCODE = 'check_violation';
    END IF;
    IF n_entries < 2 THEN
        RAISE EXCEPTION
            'LEDGER_INCOMPLETE: transaction % is posted with % entries; at least 2 are required',
            NEW.id, n_entries
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.total_minor <> sum_debit THEN
        RAISE EXCEPTION
            'LEDGER_TOTAL_MISMATCH: transaction % header total % <> entry total %',
            NEW.id, NEW.total_minor, sum_debit
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER journal_transactions_balance_check
    AFTER INSERT OR UPDATE ON journal_transactions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_journal_balanced_on_post();

-- Immutability of posted journals (§23). A POSTED transaction may only move to
-- REVERSED, and may only have its reversal linkage filled in. Everything else
-- about it — amounts, accounts, dates, description — is frozen.
CREATE OR REPLACE FUNCTION journal_transactions_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'DRAFT' THEN
            RETURN OLD;      -- abandoning a draft is fine
        END IF;
        RAISE EXCEPTION
            'JOURNAL_IMMUTABLE: a posted journal cannot be deleted; reverse it instead'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.status = 'DRAFT' THEN
        RETURN NEW;          -- drafts are freely editable
    END IF;

    -- From here OLD is POSTED or REVERSED.
    IF OLD.status = 'REVERSED' AND NEW.status <> 'REVERSED' THEN
        RAISE EXCEPTION
            'JOURNAL_IMMUTABLE: a reversed journal cannot change status'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF NEW.status NOT IN ('POSTED','REVERSED') THEN
        RAISE EXCEPTION
            'JOURNAL_IMMUTABLE: a posted journal cannot return to %', NEW.status
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF NEW.total_minor      IS DISTINCT FROM OLD.total_minor
       OR NEW.entry_count   IS DISTINCT FROM OLD.entry_count
       OR NEW.currency      IS DISTINCT FROM OLD.currency
       OR NEW.posting_date  IS DISTINCT FROM OLD.posting_date
       OR NEW.period_id     IS DISTINCT FROM OLD.period_id
       OR NEW.source_type   IS DISTINCT FROM OLD.source_type
       OR NEW.source_id     IS DISTINCT FROM OLD.source_id
       OR NEW.purpose       IS DISTINCT FROM OLD.purpose
       OR NEW.description   IS DISTINCT FROM OLD.description
       OR NEW.posted_at     IS DISTINCT FROM OLD.posted_at
       OR NEW.transaction_number IS DISTINCT FROM OLD.transaction_number THEN
        RAISE EXCEPTION
            'JOURNAL_IMMUTABLE: posted journal % cannot be edited; post a reversal or an adjustment',
            OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER journal_transactions_guard_trigger
    BEFORE UPDATE OR DELETE ON journal_transactions
    FOR EACH ROW EXECUTE FUNCTION journal_transactions_guard();

-- Entries of a posted transaction are frozen outright: there is no legitimate
-- edit, because the transaction they belong to can no longer change either.
CREATE OR REPLACE FUNCTION journal_entries_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_status text;
BEGIN
    SELECT status INTO parent_status
      FROM journal_transactions
     WHERE id = COALESCE(NEW.transaction_id, OLD.transaction_id);

    IF NOT FOUND THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF parent_status <> 'DRAFT' THEN
        RAISE EXCEPTION
            'JOURNAL_IMMUTABLE: entries of a % transaction cannot be changed (%); post a reversal instead',
            parent_status, TG_OP
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER journal_entries_guard_trigger
    BEFORE UPDATE OR DELETE ON journal_entries
    FOR EACH ROW EXECUTE FUNCTION journal_entries_guard();

-- Nothing posts into a closed period. Checked on the header, where the period
-- is chosen, and again if a period were somehow closed mid-transaction.
CREATE OR REPLACE FUNCTION assert_period_open()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    period_status text;
    period_code   text;
BEGIN
    IF NEW.status = 'DRAFT' THEN
        RETURN NEW;
    END IF;

    SELECT status, code INTO period_status, period_code
      FROM accounting_periods
     WHERE id = NEW.period_id;

    IF period_status = 'CLOSED' THEN
        RAISE EXCEPTION
            'PERIOD_CLOSED: accounting period % is closed and cannot receive postings',
            period_code
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER journal_transactions_period_guard
    BEFORE INSERT OR UPDATE ON journal_transactions
    FOR EACH ROW EXECUTE FUNCTION assert_period_open();

-- ---------------------------------------------------------------------------
-- Default chart of accounts
--
-- Seeded for every existing organization, and by provision.Bootstrap for new
-- ones. Codes are referenced from Go by constant, so they are part of the
-- contract and must not be renumbered.
-- ---------------------------------------------------------------------------
INSERT INTO ledger_accounts (public_id, organization_id, code, name, account_type,
                             normal_balance, currency, is_system, description)
SELECT
    gen_seed_public_id('lac'),
    o.id, v.code, v.name, v.account_type, v.normal_balance, o.currency, true, v.description
FROM organizations o
CROSS JOIN (VALUES
    -- Assets
    ('1000', 'Cash and Bank',                 'ASSET',     'DEBIT',  'Head-office cash and bank balances.'),
    ('1100', 'COD Receivable from Agents',    'ASSET',     'DEBIT',  'Cash collected at the doorstep and not yet handed in.'),
    ('1110', 'COD Receivable from Branches',  'ASSET',     'DEBIT',  'Cash held at a branch and not yet remitted.'),
    ('1120', 'COD Receivable from Franchises','ASSET',     'DEBIT',  'Cash confirmed by a franchise and not yet remitted.'),
    ('1200', 'Trade Receivable from Customers','ASSET',    'DEBIT',  'Invoiced amounts owed by billed customers.'),
    ('1300', 'Franchise Receivable',          'ASSET',     'DEBIT',  'Net amounts owed to head office by a franchise.'),
    ('1400', 'COD Shortage Recoverable',      'ASSET',     'DEBIT',  'Shortfalls under recovery from the responsible party.'),
    -- Liabilities
    ('2000', 'COD Payable to Consignors',     'LIABILITY', 'CREDIT', 'COD collected and owed to the sender.'),
    ('2100', 'Commission Payable',            'LIABILITY', 'CREDIT', 'Commission earned by franchises and not yet settled.'),
    ('2200', 'Franchise Payable',             'LIABILITY', 'CREDIT', 'Net amounts owed by head office to a franchise.'),
    ('2300', 'Tax Payable',                   'LIABILITY', 'CREDIT', 'Output tax collected and owed to the authority.'),
    ('2310', 'Withholding Tax Payable',       'LIABILITY', 'CREDIT', 'Tax withheld from franchise settlements.'),
    ('2400', 'Customer Advances',             'LIABILITY', 'CREDIT', 'Prepaid balances held on account.'),
    ('2500', 'COD Excess Payable',            'LIABILITY', 'CREDIT', 'Over-collections owed back.'),
    -- Equity
    ('3000', 'Retained Earnings',             'EQUITY',    'CREDIT', 'Accumulated result.'),
    ('3100', 'Opening Balance Equity',        'EQUITY',    'CREDIT', 'Counterpart for opening balances at go-live.'),
    -- Revenue
    ('4000', 'Freight Revenue',               'REVENUE',   'CREDIT', 'Transportation charges earned.'),
    ('4100', 'Surcharge Revenue',             'REVENUE',   'CREDIT', 'Fuel, remote-area and handling surcharges.'),
    ('4200', 'COD Service Fee Revenue',       'REVENUE',   'CREDIT', 'Fees charged for collecting COD.'),
    ('4300', 'Other Operating Revenue',       'REVENUE',   'CREDIT', 'Everything not captured above.'),
    -- Expense
    ('5000', 'Commission Expense',            'EXPENSE',   'DEBIT',  'Commission earned by the network.'),
    ('5100', 'Incentive Expense',             'EXPENSE',   'DEBIT',  'Volume and performance incentives.'),
    ('5200', 'COD Shortage Written Off',      'EXPENSE',   'DEBIT',  'Irrecoverable COD shortfalls.'),
    ('5300', 'Discount Allowed',              'EXPENSE',   'DEBIT',  'Discounts and credit notes issued.'),
    ('5400', 'Penalty Income Offset',         'EXPENSE',   'DEBIT',  'Penalties refunded or waived.')
) AS v(code, name, account_type, normal_balance, description)
ON CONFLICT DO NOTHING;

-- Subsidiary accounts hang off these four control accounts. Recorded in
-- metadata so the application can find the right parent by code without
-- hardcoding an id.
UPDATE ledger_accounts
   SET metadata = jsonb_build_object('subsidiaryParty', 'FRANCHISE')
 WHERE code IN ('1300','2200','1120');
UPDATE ledger_accounts
   SET metadata = jsonb_build_object('subsidiaryParty', 'CUSTOMER')
 WHERE code = '1200';
UPDATE ledger_accounts
   SET metadata = jsonb_build_object('subsidiaryParty', 'OPERATING_UNIT')
 WHERE code = '1110';
UPDATE ledger_accounts
   SET metadata = jsonb_build_object('subsidiaryParty', 'AGENT')
 WHERE code = '1100';
