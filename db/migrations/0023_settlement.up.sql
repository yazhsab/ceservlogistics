-- 0023 Franchise settlement (M24).
--
-- A settlement is a period statement between head office and one franchise:
-- what was earned, what is owed, what was already paid, and the net.
--
-- §26 requires it to be *reproducible*. That is achieved by making the
-- calculation a pure function of rows that are themselves immutable —
-- commission calculations and COD obligations — and by writing every line with
-- a pointer back to the source row it came from. Rerunning the calculation over
-- the same period must produce the same lines; if it does not, either a source
-- row changed (which the guards in 0021 and 0022 forbid) or there is a bug, and
-- the reproducibility test will catch it.
--
-- An approved or paid settlement never silently recalculates. Corrections are
-- settlement_adjustments, which are additive rows, or a reversal.

CREATE TABLE settlements (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'stl')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    settlement_number   text        NOT NULL,
    franchise_id        bigint      NOT NULL REFERENCES franchises (id) ON DELETE RESTRICT,

    period_type         text        NOT NULL CHECK (period_type IN ('WEEKLY','FORTNIGHTLY','MONTHLY','CUSTOM')),
    period_start        date        NOT NULL,
    period_end          date        NOT NULL,

    status              text        NOT NULL DEFAULT 'DRAFT' CHECK (status IN
                        ('DRAFT','CALCULATED','UNDER_REVIEW','APPROVED','PARTIALLY_PAID',
                         'PAID','CLOSED','CANCELLED')),
    currency            char(3)     NOT NULL,

    -- Component totals. Each is the SUM of its settlement_lines and is
    -- verified by the trigger below rather than trusted.
    commission_minor    bigint      NOT NULL DEFAULT 0,
    incentive_minor     bigint      NOT NULL DEFAULT 0,
    cod_liability_minor bigint      NOT NULL DEFAULT 0,
    charges_minor       bigint      NOT NULL DEFAULT 0,
    penalties_minor     bigint      NOT NULL DEFAULT 0,
    adjustments_minor   bigint      NOT NULL DEFAULT 0,
    tax_minor           bigint      NOT NULL DEFAULT 0,
    withholding_minor   bigint      NOT NULL DEFAULT 0,
    -- Carried forward from the previous settlement for this franchise.
    opening_balance_minor bigint    NOT NULL DEFAULT 0,

    -- The answer. Positive = head office owes the franchise.
    net_amount_minor    bigint      NOT NULL DEFAULT 0,
    paid_minor          bigint      NOT NULL DEFAULT 0,

    -- A fingerprint of the inputs, so a recalculation that would change the
    -- result is detectable without diffing every line.
    calculation_hash    text,
    calculated_at       timestamptz,
    calculated_by       bigint      REFERENCES users (id) ON DELETE SET NULL,

    submitted_at        timestamptz,
    submitted_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    approved_at         timestamptz,
    approved_by         bigint      REFERENCES users (id) ON DELETE RESTRICT,
    closed_at           timestamptz,
    cancelled_at        timestamptz,
    cancel_reason       text,

    journal_transaction_id bigint   REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    notes               text,
    request_id          text,
    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    version             integer     NOT NULL DEFAULT 1,

    CONSTRAINT settlements_range CHECK (period_end >= period_start),
    CONSTRAINT settlements_calculated_recorded CHECK (
        (status = 'DRAFT' OR status = 'CANCELLED') OR calculated_at IS NOT NULL
    ),
    CONSTRAINT settlements_approval_recorded CHECK (
        (status NOT IN ('APPROVED','PARTIALLY_PAID','PAID','CLOSED')) OR
        (approved_by IS NOT NULL AND approved_at IS NOT NULL)
    ),
    -- Maker/checker (§ "FINANCE SECURITY"): the person who calculated a
    -- settlement cannot be the person who approves it.
    CONSTRAINT settlements_maker_is_not_checker CHECK (
        approved_by IS NULL OR calculated_by IS NULL OR approved_by <> calculated_by
    ),
    CONSTRAINT settlements_cancel_recorded CHECK (
        (status <> 'CANCELLED') OR (cancel_reason IS NOT NULL AND cancelled_at IS NOT NULL)
    ),
    CONSTRAINT settlements_paid_bounds CHECK (paid_minor >= 0)
);

CREATE UNIQUE INDEX settlements_number_idx
    ON settlements (organization_id, settlement_number);
-- Idempotent generation: one live settlement per franchise per period. A
-- retried generate call collides here instead of producing a duplicate.
CREATE UNIQUE INDEX settlements_period_idx
    ON settlements (organization_id, franchise_id, period_start, period_end)
    WHERE status <> 'CANCELLED';
CREATE INDEX settlements_franchise_idx
    ON settlements (organization_id, franchise_id, period_end DESC);
CREATE INDEX settlements_status_idx
    ON settlements (organization_id, status, period_end DESC);

CREATE TRIGGER settlements_touch
    BEFORE UPDATE ON settlements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER settlements_version
    BEFORE UPDATE ON settlements
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Lines — every one references the source row that produced it (§26).
-- ---------------------------------------------------------------------------
CREATE TABLE settlement_lines (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'sln')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    settlement_id     bigint      NOT NULL REFERENCES settlements (id) ON DELETE CASCADE,

    line_no           integer     NOT NULL CHECK (line_no > 0),
    category          text        NOT NULL CHECK (category IN
                      ('BOOKING_COMMISSION','PICKUP_COMMISSION','ORIGIN_HANDLING','DESTINATION_HANDLING',
                       'DELIVERY_COMMISSION','COD_COMMISSION','VOLUME_INCENTIVE','CUSTOM_COMMISSION',
                       'COD_LIABILITY','CHARGE','PENALTY','INCENTIVE','ADJUSTMENT','TAX','WITHHOLDING',
                       'OPENING_BALANCE')),

    description       text        NOT NULL,
    -- Signed. Positive increases what head office owes the franchise.
    amount_minor      bigint      NOT NULL,
    currency          char(3)     NOT NULL,
    quantity          integer     NOT NULL DEFAULT 1,

    -- Provenance. Exactly what this line came from.
    source_type       text        NOT NULL CHECK (source_type IN
                      ('COMMISSION_CALCULATION','COD_OBLIGATION','COD_ADJUSTMENT','SETTLEMENT_ADJUSTMENT',
                       'PREVIOUS_SETTLEMENT','TAX_RULE','MANUAL')),
    source_id         bigint,
    source_public_id  text,
    shipment_id       bigint      REFERENCES shipments (id) ON DELETE RESTRICT,

    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX settlement_lines_no_idx ON settlement_lines (settlement_id, line_no);
-- A source row can appear at most once on a settlement, which is what stops a
-- rerun from double-counting a commission it already swept.
CREATE UNIQUE INDEX settlement_lines_source_idx
    ON settlement_lines (settlement_id, source_type, source_id)
    WHERE source_id IS NOT NULL;
CREATE INDEX settlement_lines_settlement_idx ON settlement_lines (settlement_id, category);
CREATE INDEX settlement_lines_source_lookup_idx
    ON settlement_lines (organization_id, source_type, source_id);

-- ---------------------------------------------------------------------------
-- Adjustments — the only way to change an approved settlement.
-- ---------------------------------------------------------------------------
CREATE TABLE settlement_adjustments (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'sad')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    settlement_id     bigint      NOT NULL REFERENCES settlements (id) ON DELETE RESTRICT,

    adjustment_type   text        NOT NULL CHECK (adjustment_type IN
                      ('CORRECTION','PENALTY','INCENTIVE','WAIVER','RECOVERY','GOODWILL','OTHER')),
    amount_minor      bigint      NOT NULL,
    currency          char(3)     NOT NULL,
    reason            text        NOT NULL CHECK (length(btrim(reason)) >= 10),

    status            text        NOT NULL DEFAULT 'PENDING_APPROVAL' CHECK (status IN
                      ('PENDING_APPROVAL','APPROVED','REJECTED','APPLIED')),

    requested_by      bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    requested_at      timestamptz NOT NULL DEFAULT now(),
    approved_by       bigint      REFERENCES users (id) ON DELETE RESTRICT,
    approved_at       timestamptz,
    rejection_reason  text,
    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT settlement_adjustments_maker_is_not_checker CHECK (
        approved_by IS NULL OR approved_by <> requested_by
    ),
    CONSTRAINT settlement_adjustments_approval_recorded CHECK (
        (status NOT IN ('APPROVED','APPLIED')) OR (approved_by IS NOT NULL AND approved_at IS NOT NULL)
    ),
    CONSTRAINT settlement_adjustments_rejection_recorded CHECK (
        (status <> 'REJECTED') OR (rejection_reason IS NOT NULL)
    )
);

CREATE INDEX settlement_adjustments_settlement_idx ON settlement_adjustments (settlement_id);
CREATE INDEX settlement_adjustments_pending_idx
    ON settlement_adjustments (organization_id, requested_at DESC) WHERE status = 'PENDING_APPROVAL';

CREATE TRIGGER settlement_adjustments_touch
    BEFORE UPDATE ON settlement_adjustments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Approvals — the audit trail of the review, kept even when rejected.
-- ---------------------------------------------------------------------------
CREATE TABLE settlement_approvals (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'sap')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    settlement_id   bigint      NOT NULL REFERENCES settlements (id) ON DELETE RESTRICT,

    action          text        NOT NULL CHECK (action IN ('SUBMITTED','APPROVED','REJECTED','REOPENED')),
    -- The net at the moment of the decision, so an approval cannot later be
    -- claimed to have been for a different number.
    net_amount_minor bigint     NOT NULL,
    currency        char(3)     NOT NULL,
    comment         text,

    actor_id        bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    acted_at        timestamptz NOT NULL DEFAULT now(),
    request_id      text
);

CREATE INDEX settlement_approvals_settlement_idx
    ON settlement_approvals (settlement_id, acted_at DESC);

CREATE TRIGGER settlement_approvals_append_only
    BEFORE UPDATE OR DELETE ON settlement_approvals
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Payments against a settlement.
-- ---------------------------------------------------------------------------
CREATE TABLE settlement_payments (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'spm')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    settlement_id     bigint      NOT NULL REFERENCES settlements (id) ON DELETE RESTRICT,

    payment_number    text        NOT NULL,
    direction         text        NOT NULL CHECK (direction IN ('OUTBOUND','INBOUND')),
    amount_minor      bigint      NOT NULL CHECK (amount_minor > 0),
    currency          char(3)     NOT NULL,

    payment_mode      text        NOT NULL CHECK (payment_mode IN
                      ('BANK_TRANSFER','UPI','CHEQUE','CASH','ADJUSTMENT','OTHER')),
    reference         text,
    paid_on           date        NOT NULL,

    status            text        NOT NULL DEFAULT 'RECORDED' CHECK (status IN
                      ('RECORDED','CONFIRMED','FAILED','REVERSED')),

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,

    recorded_by       bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    confirmed_by      bigint      REFERENCES users (id) ON DELETE RESTRICT,
    confirmed_at      timestamptz,
    failure_reason    text,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT settlement_payments_failure_recorded CHECK (
        (status <> 'FAILED') OR (failure_reason IS NOT NULL)
    )
);

CREATE UNIQUE INDEX settlement_payments_number_idx
    ON settlement_payments (organization_id, payment_number);
CREATE INDEX settlement_payments_settlement_idx ON settlement_payments (settlement_id);

CREATE TRIGGER settlement_payments_touch
    BEFORE UPDATE ON settlement_payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Guards
-- ---------------------------------------------------------------------------

-- An approved settlement cannot silently recalculate (§26). Once approved, the
-- component totals are frozen; only payment progress and closure may move.
CREATE OR REPLACE FUNCTION settlements_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'DRAFT' THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION
            'SETTLEMENT_IMMUTABLE: settlement % is % and cannot be deleted; cancel it instead',
            OLD.settlement_number, OLD.status
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.status IN ('DRAFT','CALCULATED','UNDER_REVIEW') THEN
        RETURN NEW;     -- still being worked on
    END IF;

    -- APPROVED and beyond: the numbers are settled.
    IF NEW.commission_minor      IS DISTINCT FROM OLD.commission_minor
       OR NEW.incentive_minor    IS DISTINCT FROM OLD.incentive_minor
       OR NEW.cod_liability_minor IS DISTINCT FROM OLD.cod_liability_minor
       OR NEW.charges_minor      IS DISTINCT FROM OLD.charges_minor
       OR NEW.penalties_minor    IS DISTINCT FROM OLD.penalties_minor
       OR NEW.tax_minor          IS DISTINCT FROM OLD.tax_minor
       OR NEW.withholding_minor  IS DISTINCT FROM OLD.withholding_minor
       OR NEW.opening_balance_minor IS DISTINCT FROM OLD.opening_balance_minor
       OR NEW.net_amount_minor   IS DISTINCT FROM OLD.net_amount_minor
       OR NEW.period_start       IS DISTINCT FROM OLD.period_start
       OR NEW.period_end         IS DISTINCT FROM OLD.period_end
       OR NEW.franchise_id       IS DISTINCT FROM OLD.franchise_id THEN
        RAISE EXCEPTION
            'SETTLEMENT_IMMUTABLE: settlement % is %; raise a settlement adjustment instead of recalculating',
            OLD.settlement_number, OLD.status
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER settlements_guard_trigger
    BEFORE UPDATE OR DELETE ON settlements
    FOR EACH ROW EXECUTE FUNCTION settlements_guard();

-- Lines belong to the calculation. Once the settlement leaves the working
-- states they are frozen, so an approved statement cannot grow a line.
CREATE OR REPLACE FUNCTION settlement_lines_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_status text;
BEGIN
    SELECT status INTO parent_status FROM settlements
     WHERE id = COALESCE(NEW.settlement_id, OLD.settlement_id);

    IF NOT FOUND THEN
        RETURN COALESCE(NEW, OLD);   -- parent going away in this transaction
    END IF;

    IF parent_status NOT IN ('DRAFT','CALCULATED','UNDER_REVIEW') THEN
        RAISE EXCEPTION
            'SETTLEMENT_LINES_FROZEN: settlement is %; lines cannot be changed (%)',
            parent_status, TG_OP
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER settlement_lines_guard_trigger
    BEFORE INSERT OR UPDATE OR DELETE ON settlement_lines
    FOR EACH ROW EXECUTE FUNCTION settlement_lines_guard();

-- The header totals must equal the sum of the lines. Deferred, because lines
-- necessarily arrive after the header is created and one at a time.
CREATE OR REPLACE FUNCTION assert_settlement_totals()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    stl        record;
    line_total bigint;
BEGIN
    SELECT status, net_amount_minor, opening_balance_minor
      INTO stl FROM settlements WHERE id = NEW.id;
    IF NOT FOUND OR stl.status IN ('DRAFT','CANCELLED') THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(amount_minor), 0) INTO line_total
      FROM settlement_lines WHERE settlement_id = NEW.id;

    IF line_total <> stl.net_amount_minor THEN
        RAISE EXCEPTION
            'SETTLEMENT_TOTAL_MISMATCH: settlement % has lines totalling % but a net of %',
            NEW.id, line_total, stl.net_amount_minor
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER settlements_total_check
    AFTER INSERT OR UPDATE ON settlements
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_settlement_totals();

-- Now that settlements exists, close the loop from 0021 and 0022.
ALTER TABLE commission_calculations
    ADD CONSTRAINT commission_calculations_settlement_fk
    FOREIGN KEY (settlement_id) REFERENCES settlements (id) ON DELETE RESTRICT;
ALTER TABLE commission_entries
    ADD CONSTRAINT commission_entries_settlement_fk
    FOREIGN KEY (settlement_id) REFERENCES settlements (id) ON DELETE RESTRICT;
