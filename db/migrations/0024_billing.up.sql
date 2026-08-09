-- 0024 Billing (M25).
--
-- Invoices bill customers. Two properties matter most:
--
--   1. Numbering is concurrency-safe and gapless per series. Tax authorities
--      care about gaps, so the number comes from an atomic counter, allocated
--      inside the same transaction that writes the invoice.
--   2. Lines are snapshots. An invoice line copies the amount, the tax rate and
--      the description at the moment of issue; it never joins back to a rate
--      card at read time. Editing a rate card tomorrow cannot change an invoice
--      issued today (§19, §26).
--
-- The tax model is deliberately generic: tax_components holds named components
-- with rates in basis points, and India GST is expressed through that rather
-- than hardcoded. The metadata column carries HSN/SAC, place of supply and the
-- CGST/SGST/IGST split without the schema having to know what those mean.

-- ---------------------------------------------------------------------------
-- Invoice number series.
--
-- Separate from operational_sequences because an invoice series has a
-- statutory shape — prefix, financial year, reset policy — that operational
-- codes do not.
-- ---------------------------------------------------------------------------
CREATE TABLE invoice_sequences (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    series_code     text        NOT NULL,
    document_type   text        NOT NULL CHECK (document_type IN ('INVOICE','CREDIT_NOTE','DEBIT_NOTE')),
    prefix          text        NOT NULL DEFAULT '',
    -- Financial year or other reset scope, e.g. '2026-27'. Part of the key, so
    -- a new year starts a new counter without touching the old one.
    scope_key       text        NOT NULL DEFAULT '',

    current_value   bigint      NOT NULL DEFAULT 0 CHECK (current_value >= 0),
    max_value       bigint      NOT NULL DEFAULT 999999999,
    padding         integer     NOT NULL DEFAULT 6 CHECK (padding BETWEEN 1 AND 12),

    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invoice_sequences_key_idx
    ON invoice_sequences (organization_id, series_code, document_type, scope_key);

-- ---------------------------------------------------------------------------
-- Billing runs — the periodic sweep that produces invoices in bulk.
-- ---------------------------------------------------------------------------
CREATE TABLE billing_runs (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'brn')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    run_code          text        NOT NULL,
    billing_cycle     text        NOT NULL CHECK (billing_cycle IN
                      ('WEEKLY','FORTNIGHTLY','MONTHLY','CUSTOM','ON_DEMAND')),
    period_start      date        NOT NULL,
    period_end        date        NOT NULL,

    status            text        NOT NULL DEFAULT 'PENDING' CHECK (status IN
                      ('PENDING','RUNNING','COMPLETED','COMPLETED_WITH_ERRORS','FAILED','CANCELLED')),

    customer_filter   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    invoice_count     integer     NOT NULL DEFAULT 0,
    shipment_count    integer     NOT NULL DEFAULT 0,
    total_minor       bigint      NOT NULL DEFAULT 0,
    currency          char(3)     NOT NULL,
    error_count       integer     NOT NULL DEFAULT 0,
    errors            jsonb       NOT NULL DEFAULT '[]'::jsonb,

    started_at        timestamptz,
    completed_at      timestamptz,
    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT billing_runs_range CHECK (period_end >= period_start)
);

CREATE UNIQUE INDEX billing_runs_code_idx ON billing_runs (organization_id, run_code);
-- One live run per cycle window: two concurrent runs would each invoice the
-- same shipments.
CREATE UNIQUE INDEX billing_runs_active_idx
    ON billing_runs (organization_id, period_start, period_end)
    WHERE status IN ('PENDING','RUNNING');
CREATE INDEX billing_runs_status_idx ON billing_runs (organization_id, status, created_at DESC);

CREATE TRIGGER billing_runs_touch
    BEFORE UPDATE ON billing_runs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Invoices
-- ---------------------------------------------------------------------------
CREATE TABLE invoices (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'inv')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    invoice_number      text        NOT NULL,
    series_code         text        NOT NULL,
    document_type       text        NOT NULL DEFAULT 'INVOICE'
                        CHECK (document_type IN ('INVOICE','PROFORMA')),

    customer_id         bigint      NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    business_account_id bigint      REFERENCES business_accounts (id) ON DELETE RESTRICT,
    billing_run_id      bigint      REFERENCES billing_runs (id) ON DELETE RESTRICT,

    -- Billing mode. Retail is billed per shipment at booking; business accounts
    -- are billed periodically.
    billing_mode        text        NOT NULL CHECK (billing_mode IN ('IMMEDIATE','PERIODIC')),
    period_start        date,
    period_end          date,

    issue_date          date        NOT NULL,
    due_date            date        NOT NULL,

    status              text        NOT NULL DEFAULT 'DRAFT' CHECK (status IN
                        ('DRAFT','ISSUED','PARTIALLY_PAID','PAID','OVERDUE','CANCELLED','WRITTEN_OFF')),

    currency            char(3)     NOT NULL,
    -- All derived from the lines and verified by the deferred trigger below.
    subtotal_minor      bigint      NOT NULL DEFAULT 0,
    discount_minor      bigint      NOT NULL DEFAULT 0,
    taxable_minor       bigint      NOT NULL DEFAULT 0,
    tax_minor           bigint      NOT NULL DEFAULT 0,
    rounding_minor      bigint      NOT NULL DEFAULT 0,
    total_minor         bigint      NOT NULL DEFAULT 0,
    paid_minor          bigint      NOT NULL DEFAULT 0 CHECK (paid_minor >= 0),
    -- Credit notes reduce what is owed without touching the invoice.
    credited_minor      bigint      NOT NULL DEFAULT 0 CHECK (credited_minor >= 0),

    shipment_count      integer     NOT NULL DEFAULT 0,

    -- Snapshot of the parties at issue time. A customer moving office next
    -- month must not change last month's invoice.
    bill_to             jsonb       NOT NULL DEFAULT '{}'::jsonb,
    supplier            jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Statutory extras: HSN/SAC, place of supply, GSTIN, reverse charge.
    tax_metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,

    journal_transaction_id bigint   REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    -- The rendered PDF, once the background job has produced it.
    document_object_id  bigint      REFERENCES stored_objects (id) ON DELETE SET NULL,

    notes               text,
    cancel_reason       text,
    issued_at           timestamptz,
    issued_by           bigint      REFERENCES users (id) ON DELETE SET NULL,
    cancelled_at        timestamptz,
    request_id          text,
    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    version             integer     NOT NULL DEFAULT 1,

    CONSTRAINT invoices_due_after_issue CHECK (due_date >= issue_date),
    CONSTRAINT invoices_period_complete CHECK (
        billing_mode <> 'PERIODIC' OR (period_start IS NOT NULL AND period_end IS NOT NULL)
    ),
    CONSTRAINT invoices_issued_recorded CHECK (
        (status = 'DRAFT' OR status = 'CANCELLED') OR issued_at IS NOT NULL
    ),
    CONSTRAINT invoices_cancel_recorded CHECK (
        (status <> 'CANCELLED') OR (cancel_reason IS NOT NULL)
    ),
    CONSTRAINT invoices_settled_within_total CHECK (paid_minor + credited_minor <= total_minor + 100)
);

CREATE UNIQUE INDEX invoices_number_idx ON invoices (organization_id, invoice_number);
CREATE INDEX invoices_customer_idx
    ON invoices (organization_id, customer_id, issue_date DESC, id DESC);
CREATE INDEX invoices_status_idx ON invoices (organization_id, status, due_date);
CREATE INDEX invoices_run_idx ON invoices (billing_run_id) WHERE billing_run_id IS NOT NULL;
-- The outstanding-receivables query.
CREATE INDEX invoices_outstanding_idx
    ON invoices (organization_id, customer_id, due_date)
    WHERE status IN ('ISSUED','PARTIALLY_PAID','OVERDUE');

CREATE TRIGGER invoices_touch
    BEFORE UPDATE ON invoices
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER invoices_version
    BEFORE UPDATE ON invoices
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Invoice lines — snapshots, never references to live configuration.
-- ---------------------------------------------------------------------------
CREATE TABLE invoice_lines (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ivl')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    invoice_id        bigint      NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,

    line_no           integer     NOT NULL CHECK (line_no > 0),
    line_type         text        NOT NULL CHECK (line_type IN
                      ('FREIGHT','SURCHARGE','COD_FEE','INSURANCE','HANDLING','DISCOUNT',
                       'ADJUSTMENT','SUMMARY','OTHER')),

    description       text        NOT NULL,
    -- Copied from the charge snapshot at issue time.
    hsn_sac_code      text,
    quantity          integer     NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit_price_minor  bigint      NOT NULL DEFAULT 0,
    amount_minor      bigint      NOT NULL,
    discount_minor    bigint      NOT NULL DEFAULT 0,
    taxable_minor     bigint      NOT NULL DEFAULT 0,
    tax_minor         bigint      NOT NULL DEFAULT 0,
    total_minor       bigint      NOT NULL DEFAULT 0,
    currency          char(3)     NOT NULL,

    shipment_id       bigint      REFERENCES shipments (id) ON DELETE RESTRICT,
    -- The pricing snapshot this line was copied from, for traceability.
    charge_snapshot_id bigint     REFERENCES shipment_charge_snapshots (id) ON DELETE RESTRICT,

    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invoice_lines_no_idx ON invoice_lines (invoice_id, line_no);
CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id);
CREATE INDEX invoice_lines_shipment_idx
    ON invoice_lines (organization_id, shipment_id) WHERE shipment_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Which shipments an invoice covers. Separate from lines because a summary
-- invoice has few lines but many shipments.
-- ---------------------------------------------------------------------------
CREATE TABLE invoice_shipments (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint  NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    invoice_id      bigint  NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    shipment_id     bigint  NOT NULL REFERENCES shipments (id) ON DELETE RESTRICT,
    awb             text    NOT NULL,
    amount_minor    bigint  NOT NULL DEFAULT 0,
    tax_minor       bigint  NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invoice_shipments_unique_idx ON invoice_shipments (invoice_id, shipment_id);
-- A shipment is billed once. This is the guard that stops a re-run of a
-- billing cycle from invoicing the same parcel twice.
CREATE UNIQUE INDEX invoice_shipments_billed_once_idx
    ON invoice_shipments (organization_id, shipment_id);
CREATE INDEX invoice_shipments_shipment_idx ON invoice_shipments (shipment_id);

-- ---------------------------------------------------------------------------
-- Tax components — the configurable tax engine.
--
-- Rows here belong either to an invoice line (the applied tax) or to a
-- template (the configured rate). Keeping both in one shape means an invoice
-- can always explain its tax without consulting current configuration.
-- ---------------------------------------------------------------------------
CREATE TABLE tax_components (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'txc')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    invoice_id      bigint      REFERENCES invoices (id) ON DELETE CASCADE,
    invoice_line_id bigint      REFERENCES invoice_lines (id) ON DELETE CASCADE,
    credit_note_id  bigint,

    -- CGST, SGST, IGST, VAT, or anything else. Not an enum: the tax regime is
    -- configuration, not a code change.
    component_code  text        NOT NULL,
    component_name  text        NOT NULL,
    rate_bp         integer     NOT NULL CHECK (rate_bp >= 0 AND rate_bp <= 1000000),
    taxable_minor   bigint      NOT NULL DEFAULT 0,
    tax_minor       bigint      NOT NULL DEFAULT 0,
    currency        char(3)     NOT NULL,

    -- Jurisdiction and statutory attributes, e.g. place of supply.
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tax_components_target CHECK (
        invoice_id IS NOT NULL OR invoice_line_id IS NOT NULL OR credit_note_id IS NOT NULL
    )
);

CREATE INDEX tax_components_invoice_idx ON tax_components (invoice_id) WHERE invoice_id IS NOT NULL;
CREATE INDEX tax_components_line_idx ON tax_components (invoice_line_id) WHERE invoice_line_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Credit and debit notes.
-- ---------------------------------------------------------------------------
CREATE TABLE credit_notes (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'crn')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    note_number       text        NOT NULL,
    note_type         text        NOT NULL CHECK (note_type IN ('CREDIT','DEBIT')),
    invoice_id        bigint      REFERENCES invoices (id) ON DELETE RESTRICT,
    customer_id       bigint      NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,

    reason_code       text        NOT NULL CHECK (reason_code IN
                      ('BILLING_ERROR','SERVICE_FAILURE','RATE_CORRECTION','GOODWILL','RETURN',
                       'TAX_CORRECTION','SHORT_SHIPMENT','OTHER')),
    reason            text        NOT NULL CHECK (length(btrim(reason)) >= 5),

    issue_date        date        NOT NULL,
    currency          char(3)     NOT NULL,
    subtotal_minor    bigint      NOT NULL DEFAULT 0,
    tax_minor         bigint      NOT NULL DEFAULT 0,
    total_minor       bigint      NOT NULL CHECK (total_minor > 0),

    status            text        NOT NULL DEFAULT 'DRAFT' CHECK (status IN
                      ('DRAFT','ISSUED','APPLIED','CANCELLED')),

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    document_object_id bigint     REFERENCES stored_objects (id) ON DELETE SET NULL,
    tax_metadata      jsonb       NOT NULL DEFAULT '{}'::jsonb,

    issued_at         timestamptz,
    issued_by         bigint      REFERENCES users (id) ON DELETE SET NULL,
    -- Maker/checker on anything that reduces a receivable.
    approved_by       bigint      REFERENCES users (id) ON DELETE RESTRICT,
    approved_at       timestamptz,
    request_id        text,
    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT credit_notes_issued_recorded CHECK (
        (status IN ('DRAFT','CANCELLED')) OR issued_at IS NOT NULL
    ),
    CONSTRAINT credit_notes_maker_is_not_checker CHECK (
        approved_by IS NULL OR created_by IS NULL OR approved_by <> created_by
    )
);

CREATE UNIQUE INDEX credit_notes_number_idx ON credit_notes (organization_id, note_number);
CREATE INDEX credit_notes_invoice_idx ON credit_notes (invoice_id) WHERE invoice_id IS NOT NULL;
CREATE INDEX credit_notes_customer_idx
    ON credit_notes (organization_id, customer_id, issue_date DESC);

CREATE TRIGGER credit_notes_touch
    BEFORE UPDATE ON credit_notes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE tax_components
    ADD CONSTRAINT tax_components_credit_note_fk
    FOREIGN KEY (credit_note_id) REFERENCES credit_notes (id) ON DELETE CASCADE;

CREATE TABLE credit_note_lines (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint  NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    credit_note_id  bigint  NOT NULL REFERENCES credit_notes (id) ON DELETE CASCADE,
    line_no         integer NOT NULL CHECK (line_no > 0),
    description     text    NOT NULL,
    amount_minor    bigint  NOT NULL,
    tax_minor       bigint  NOT NULL DEFAULT 0,
    total_minor     bigint  NOT NULL,
    currency        char(3) NOT NULL,
    invoice_line_id bigint  REFERENCES invoice_lines (id) ON DELETE RESTRICT,
    shipment_id     bigint  REFERENCES shipments (id) ON DELETE RESTRICT,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX credit_note_lines_no_idx ON credit_note_lines (credit_note_id, line_no);

-- ---------------------------------------------------------------------------
-- Invoice payments
-- ---------------------------------------------------------------------------
CREATE TABLE invoice_payments (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ipm')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    invoice_id        bigint      NOT NULL REFERENCES invoices (id) ON DELETE RESTRICT,
    customer_id       bigint      NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,

    payment_number    text        NOT NULL,
    amount_minor      bigint      NOT NULL CHECK (amount_minor > 0),
    currency          char(3)     NOT NULL,
    payment_mode      text        NOT NULL CHECK (payment_mode IN
                      ('BANK_TRANSFER','UPI','CARD','CHEQUE','CASH','WALLET','ADJUSTMENT','OTHER')),
    reference         text,
    received_on       date        NOT NULL,

    status            text        NOT NULL DEFAULT 'RECORDED' CHECK (status IN
                      ('RECORDED','CONFIRMED','BOUNCED','REVERSED')),

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    recorded_by       bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX invoice_payments_number_idx
    ON invoice_payments (organization_id, payment_number);
CREATE INDEX invoice_payments_invoice_idx ON invoice_payments (invoice_id);

CREATE TRIGGER invoice_payments_touch
    BEFORE UPDATE ON invoice_payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Guards
-- ---------------------------------------------------------------------------

-- An issued invoice is a statutory document. Only settlement progress moves.
CREATE OR REPLACE FUNCTION invoices_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'DRAFT' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION
            'INVOICE_IMMUTABLE: invoice % is issued and cannot be deleted; cancel it or raise a credit note',
            OLD.invoice_number
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.status = 'DRAFT' THEN
        RETURN NEW;
    END IF;

    IF NEW.subtotal_minor  IS DISTINCT FROM OLD.subtotal_minor
       OR NEW.tax_minor    IS DISTINCT FROM OLD.tax_minor
       OR NEW.total_minor  IS DISTINCT FROM OLD.total_minor
       OR NEW.taxable_minor IS DISTINCT FROM OLD.taxable_minor
       OR NEW.discount_minor IS DISTINCT FROM OLD.discount_minor
       OR NEW.currency     IS DISTINCT FROM OLD.currency
       OR NEW.customer_id  IS DISTINCT FROM OLD.customer_id
       OR NEW.invoice_number IS DISTINCT FROM OLD.invoice_number
       OR NEW.issue_date   IS DISTINCT FROM OLD.issue_date
       OR NEW.bill_to      IS DISTINCT FROM OLD.bill_to THEN
        RAISE EXCEPTION
            'INVOICE_IMMUTABLE: invoice % is issued; raise a credit or debit note instead of editing it',
            OLD.invoice_number
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER invoices_guard_trigger
    BEFORE UPDATE OR DELETE ON invoices
    FOR EACH ROW EXECUTE FUNCTION invoices_guard();

-- Lines of an issued invoice are frozen outright.
CREATE OR REPLACE FUNCTION invoice_lines_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_status text;
BEGIN
    SELECT status INTO parent_status FROM invoices
     WHERE id = COALESCE(NEW.invoice_id, OLD.invoice_id);
    IF NOT FOUND THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    IF parent_status <> 'DRAFT' THEN
        RAISE EXCEPTION
            'INVOICE_IMMUTABLE: lines of an issued invoice cannot be changed (%); raise a credit note',
            TG_OP
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER invoice_lines_guard_trigger
    BEFORE INSERT OR UPDATE OR DELETE ON invoice_lines
    FOR EACH ROW EXECUTE FUNCTION invoice_lines_guard();

-- The header must equal the sum of the lines on an issued invoice.
CREATE OR REPLACE FUNCTION assert_invoice_totals()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    inv        record;
    line_total bigint;
BEGIN
    SELECT status, total_minor, rounding_minor INTO inv FROM invoices WHERE id = NEW.id;
    IF NOT FOUND OR inv.status IN ('DRAFT','CANCELLED') THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(total_minor), 0) INTO line_total
      FROM invoice_lines WHERE invoice_id = NEW.id;

    IF line_total + inv.rounding_minor <> inv.total_minor THEN
        RAISE EXCEPTION
            'INVOICE_TOTAL_MISMATCH: invoice % has lines totalling % (rounding %) but a total of %',
            NEW.id, line_total, inv.rounding_minor, inv.total_minor
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER invoices_total_check
    AFTER INSERT OR UPDATE ON invoices
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_invoice_totals();

-- ---------------------------------------------------------------------------
-- Default invoice series per organization.
-- ---------------------------------------------------------------------------
INSERT INTO invoice_sequences (organization_id, series_code, document_type, prefix, scope_key, padding)
SELECT o.id, 'DEFAULT', d.doc, d.prefix, '', 6
FROM organizations o
CROSS JOIN (VALUES ('INVOICE','INV'), ('CREDIT_NOTE','CRN'), ('DEBIT_NOTE','DBN')) AS d(doc, prefix)
ON CONFLICT DO NOTHING;
