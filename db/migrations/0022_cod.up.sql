-- 0022 Cash on delivery (M23).
--
-- COD is a custody problem before it is an accounting problem. Real money moves
-- from a consignee to an agent to a branch to a franchise to head office, and
-- at every hop somebody is liable for it. §25 forbids modelling that as an
-- editable balance, so the truth here is:
--
--   cod_obligations      one per COD shipment: what was owed and what state it is in
--   cod_collections      what was actually taken at the doorstep
--   cod_custody_transfers who handed what to whom, and who accepted
--   cod_reconciliations  a counted hand-in, with shortage/excess made explicit
--   cod_remittances      money leaving the network toward head office or the consignor
--   cod_adjustments      an approved correction, never an edit
--   cod_disputes         a contested amount, parked without blocking the rest
--
-- Every row that moves money carries its journal_transaction_id, so the
-- operational story and the accounting story are the same story.

-- ---------------------------------------------------------------------------
-- Obligation — created when a COD shipment is booked.
-- ---------------------------------------------------------------------------
CREATE TABLE cod_obligations (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cod')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    shipment_id         bigint      NOT NULL REFERENCES shipments (id) ON DELETE RESTRICT,
    awb                 text        NOT NULL,

    -- What the consignee is asked for. Frozen at booking from the shipment.
    expected_minor      bigint      NOT NULL CHECK (expected_minor > 0),
    currency            char(3)     NOT NULL,

    -- Where the money should end up.
    consignor_customer_id bigint    REFERENCES customers (id) ON DELETE RESTRICT,
    origin_franchise_id   bigint    REFERENCES franchises (id) ON DELETE RESTRICT,
    destination_franchise_id bigint REFERENCES franchises (id) ON DELETE RESTRICT,

    status              text        NOT NULL DEFAULT 'EXPECTED' CHECK (status IN
                        ('EXPECTED','AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED',
                         'RECONCILED','REMITTED','CLOSED','CANCELLED','WRITTEN_OFF')),

    -- Who is holding the cash right now. NULL before collection and after
    -- remittance. This is the custody pointer, not a balance.
    custodian_type      text        CHECK (custodian_type IN ('AGENT','OPERATING_UNIT','FRANCHISE','HEAD_OFFICE')),
    custodian_id        bigint,

    -- Denormalised for queue queries. Both are derived from the collection and
    -- adjustment rows and are re-derived, never hand-edited; the guard below
    -- refuses changes once the obligation is closed.
    collected_minor     bigint      NOT NULL DEFAULT 0 CHECK (collected_minor >= 0),
    remitted_minor      bigint      NOT NULL DEFAULT 0 CHECK (remitted_minor >= 0),
    adjusted_minor      bigint      NOT NULL DEFAULT 0,
    waived_minor        bigint      NOT NULL DEFAULT 0 CHECK (waived_minor >= 0),

    collected_at        timestamptz,
    reconciled_at       timestamptz,
    remitted_at         timestamptz,
    closed_at           timestamptz,

    cancel_reason       text,
    metadata            jsonb       NOT NULL DEFAULT '{}'::jsonb,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    version             integer     NOT NULL DEFAULT 1,

    CONSTRAINT cod_obligations_custodian_complete CHECK (
        (custodian_type IS NULL AND custodian_id IS NULL) OR
        (custodian_type IS NOT NULL AND custodian_id IS NOT NULL)
    ),
    CONSTRAINT cod_obligations_cancel_recorded CHECK (
        (status <> 'CANCELLED') OR (cancel_reason IS NOT NULL)
    )
);

-- One obligation per shipment. This is the first idempotency guard: a retried
-- booking cannot create a second liability for the same parcel.
CREATE UNIQUE INDEX cod_obligations_shipment_idx
    ON cod_obligations (shipment_id);
CREATE INDEX cod_obligations_status_idx
    ON cod_obligations (organization_id, status, created_at DESC);
CREATE INDEX cod_obligations_custodian_idx
    ON cod_obligations (organization_id, custodian_type, custodian_id)
    WHERE status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED');
CREATE INDEX cod_obligations_awb_idx ON cod_obligations (organization_id, awb);
CREATE INDEX cod_obligations_franchise_idx
    ON cod_obligations (organization_id, destination_franchise_id, status);

CREATE TRIGGER cod_obligations_touch
    BEFORE UPDATE ON cod_obligations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER cod_obligations_version
    BEFORE UPDATE ON cod_obligations
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Collection — what the agent actually took.
--
-- Fed from the Release 2 delivery attempt, which already records
-- cod_collected_minor, cod_payment_mode and cod_reference. This table is the
-- financial view of that same event, with its ledger linkage.
-- ---------------------------------------------------------------------------
CREATE TABLE cod_collections (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cdc')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    obligation_id       bigint      NOT NULL REFERENCES cod_obligations (id) ON DELETE RESTRICT,
    shipment_id         bigint      NOT NULL REFERENCES shipments (id) ON DELETE RESTRICT,
    delivery_attempt_id bigint      REFERENCES delivery_attempts (id) ON DELETE RESTRICT,

    amount_minor        bigint      NOT NULL CHECK (amount_minor > 0),
    currency            char(3)     NOT NULL,
    payment_mode        text        NOT NULL CHECK (payment_mode IN
                        ('CASH','UPI','CARD','WALLET','NET_BANKING','CHEQUE','DEMAND_DRAFT','OTHER')),
    -- UPI reference, cheque number, terminal id. Required for anything that is
    -- not cash, because a digital collection is only verifiable by reference.
    reference           text,

    collected_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    collected_at        timestamptz NOT NULL,
    operating_unit_id   bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,

    journal_transaction_id bigint   REFERENCES journal_transactions (id) ON DELETE RESTRICT,

    -- Offline-safe replay key, matching the Release 2 device-event scheme.
    device_id           text,
    device_event_id     text,
    request_id          text,
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cod_collections_reference_required CHECK (
        payment_mode = 'CASH' OR (reference IS NOT NULL AND length(btrim(reference)) > 0)
    )
);

-- One collection per delivery attempt: a replayed attempt cannot bank twice.
CREATE UNIQUE INDEX cod_collections_attempt_idx
    ON cod_collections (delivery_attempt_id) WHERE delivery_attempt_id IS NOT NULL;
CREATE UNIQUE INDEX cod_collections_device_event_idx
    ON cod_collections (organization_id, device_id, device_event_id)
    WHERE device_event_id IS NOT NULL;
CREATE INDEX cod_collections_obligation_idx ON cod_collections (obligation_id);
CREATE INDEX cod_collections_unit_idx
    ON cod_collections (organization_id, operating_unit_id, collected_at DESC);

CREATE TRIGGER cod_collections_append_only
    BEFORE UPDATE OR DELETE ON cod_collections
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Custody transfer — a hand-off of cash between two parties.
--
-- Two-sided on purpose: the giver declares, the receiver accepts. Until it is
-- accepted the money is still the giver's liability, which is exactly the
-- dispute an unaccepted hand-in causes in a real network.
-- ---------------------------------------------------------------------------
CREATE TABLE cod_custody_transfers (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cdt')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    transfer_code     text        NOT NULL,

    from_type         text        NOT NULL CHECK (from_type IN ('AGENT','OPERATING_UNIT','FRANCHISE','HEAD_OFFICE')),
    from_id           bigint      NOT NULL,
    to_type           text        NOT NULL CHECK (to_type IN ('AGENT','OPERATING_UNIT','FRANCHISE','HEAD_OFFICE')),
    to_id             bigint      NOT NULL,

    declared_minor    bigint      NOT NULL CHECK (declared_minor >= 0),
    accepted_minor    bigint      CHECK (accepted_minor IS NULL OR accepted_minor >= 0),
    currency          char(3)     NOT NULL,
    shipment_count    integer     NOT NULL DEFAULT 0 CHECK (shipment_count >= 0),

    status            text        NOT NULL DEFAULT 'DECLARED' CHECK (status IN
                      ('DECLARED','ACCEPTED','DISPUTED','CANCELLED')),

    declared_by       bigint      REFERENCES users (id) ON DELETE SET NULL,
    declared_at       timestamptz NOT NULL DEFAULT now(),
    accepted_by       bigint      REFERENCES users (id) ON DELETE SET NULL,
    accepted_at       timestamptz,

    -- Non-zero when the count disagrees with the declaration.
    variance_minor    bigint      NOT NULL DEFAULT 0,
    variance_reason   text,

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    notes             text,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cod_custody_transfers_accepted_recorded CHECK (
        (status <> 'ACCEPTED') OR (accepted_minor IS NOT NULL AND accepted_at IS NOT NULL)
    ),
    -- A variance has to be explained. Silent shortages are how cash goes
    -- missing without anyone being accountable.
    CONSTRAINT cod_custody_transfers_variance_explained CHECK (
        variance_minor = 0 OR variance_reason IS NOT NULL
    ),
    CONSTRAINT cod_custody_transfers_distinct_parties CHECK (
        NOT (from_type = to_type AND from_id = to_id)
    )
);

CREATE UNIQUE INDEX cod_custody_transfers_code_idx
    ON cod_custody_transfers (organization_id, transfer_code);
CREATE INDEX cod_custody_transfers_from_idx
    ON cod_custody_transfers (organization_id, from_type, from_id, status);
CREATE INDEX cod_custody_transfers_to_idx
    ON cod_custody_transfers (organization_id, to_type, to_id, status);

CREATE TRIGGER cod_custody_transfers_touch
    BEFORE UPDATE ON cod_custody_transfers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Which obligations travelled in a transfer.
CREATE TABLE cod_custody_transfer_items (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id   bigint NOT NULL REFERENCES cod_custody_transfers (id) ON DELETE CASCADE,
    obligation_id bigint NOT NULL REFERENCES cod_obligations (id) ON DELETE RESTRICT,
    amount_minor  bigint NOT NULL CHECK (amount_minor >= 0),
    status        text   NOT NULL DEFAULT 'INCLUDED'
                  CHECK (status IN ('INCLUDED','MISSING','EXCESS','DISPUTED')),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX cod_custody_transfer_items_unique_idx
    ON cod_custody_transfer_items (transfer_id, obligation_id);
CREATE INDEX cod_custody_transfer_items_obligation_idx
    ON cod_custody_transfer_items (obligation_id);
-- An obligation can only be in one undecided transfer at a time, or two
-- branches could both claim to be handing in the same cash.
CREATE UNIQUE INDEX cod_custody_transfer_items_active_idx
    ON cod_custody_transfer_items (obligation_id)
    WHERE status IN ('INCLUDED','DISPUTED');

-- ---------------------------------------------------------------------------
-- Reconciliation — a counted hand-in.
-- ---------------------------------------------------------------------------
CREATE TABLE cod_reconciliations (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cdr')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    reconciliation_code text      NOT NULL,
    party_type        text        NOT NULL CHECK (party_type IN ('AGENT','OPERATING_UNIT','FRANCHISE')),
    party_id          bigint      NOT NULL,

    period_start      date        NOT NULL,
    period_end        date        NOT NULL,

    expected_minor    bigint      NOT NULL DEFAULT 0,
    counted_minor     bigint      NOT NULL DEFAULT 0,
    shortage_minor    bigint      NOT NULL DEFAULT 0 CHECK (shortage_minor >= 0),
    excess_minor      bigint      NOT NULL DEFAULT 0 CHECK (excess_minor >= 0),
    currency          char(3)     NOT NULL,
    shipment_count    integer     NOT NULL DEFAULT 0,

    status            text        NOT NULL DEFAULT 'OPEN' CHECK (status IN
                      ('OPEN','COUNTED','COMPLETED','COMPLETED_WITH_VARIANCE','CANCELLED')),

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,

    opened_by         bigint      REFERENCES users (id) ON DELETE SET NULL,
    completed_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    completed_at      timestamptz,
    variance_reason   text,
    notes             text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cod_reconciliations_range CHECK (period_end >= period_start),
    CONSTRAINT cod_reconciliations_variance_explained CHECK (
        (shortage_minor = 0 AND excess_minor = 0) OR
        status <> 'COMPLETED_WITH_VARIANCE' OR variance_reason IS NOT NULL
    ),
    CONSTRAINT cod_reconciliations_completion_recorded CHECK (
        (status NOT IN ('COMPLETED','COMPLETED_WITH_VARIANCE')) OR completed_at IS NOT NULL
    )
);

CREATE UNIQUE INDEX cod_reconciliations_code_idx
    ON cod_reconciliations (organization_id, reconciliation_code);
-- One open reconciliation per party: two concurrent counts of the same cash
-- would each see the other's uncounted rows.
CREATE UNIQUE INDEX cod_reconciliations_active_idx
    ON cod_reconciliations (organization_id, party_type, party_id)
    WHERE status IN ('OPEN','COUNTED');
CREATE INDEX cod_reconciliations_party_idx
    ON cod_reconciliations (organization_id, party_type, party_id, period_end DESC);

CREATE TRIGGER cod_reconciliations_touch
    BEFORE UPDATE ON cod_reconciliations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE cod_reconciliation_items (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reconciliation_id bigint NOT NULL REFERENCES cod_reconciliations (id) ON DELETE CASCADE,
    obligation_id     bigint NOT NULL REFERENCES cod_obligations (id) ON DELETE RESTRICT,
    expected_minor    bigint NOT NULL DEFAULT 0,
    counted_minor     bigint NOT NULL DEFAULT 0,
    outcome           text   NOT NULL DEFAULT 'MATCHED'
                      CHECK (outcome IN ('MATCHED','SHORT','EXCESS','MISSING','DISPUTED')),
    notes             text,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX cod_reconciliation_items_unique_idx
    ON cod_reconciliation_items (reconciliation_id, obligation_id);
CREATE INDEX cod_reconciliation_items_obligation_idx
    ON cod_reconciliation_items (obligation_id);

-- ---------------------------------------------------------------------------
-- Remittance — money leaving toward head office or the consignor.
-- ---------------------------------------------------------------------------
CREATE TABLE cod_remittances (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cdm')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    remittance_code   text        NOT NULL,
    from_type         text        NOT NULL CHECK (from_type IN ('OPERATING_UNIT','FRANCHISE','HEAD_OFFICE')),
    from_id           bigint      NOT NULL,
    -- Where it went. A consignor remittance closes the liability to the sender.
    beneficiary_type  text        NOT NULL CHECK (beneficiary_type IN ('HEAD_OFFICE','CONSIGNOR','CUSTOMER')),
    beneficiary_id    bigint,

    amount_minor      bigint      NOT NULL CHECK (amount_minor > 0),
    currency          char(3)     NOT NULL,
    shipment_count    integer     NOT NULL DEFAULT 0,

    payment_mode      text        NOT NULL CHECK (payment_mode IN
                      ('BANK_TRANSFER','UPI','CHEQUE','CASH_DEPOSIT','ADJUSTMENT','OTHER')),
    reference         text,
    paid_on           date        NOT NULL,

    status            text        NOT NULL DEFAULT 'INITIATED' CHECK (status IN
                      ('INITIATED','CONFIRMED','FAILED','CANCELLED')),

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,

    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    confirmed_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    confirmed_at      timestamptz,
    failure_reason    text,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cod_remittances_confirmed_recorded CHECK (
        (status <> 'CONFIRMED') OR (confirmed_at IS NOT NULL)
    ),
    CONSTRAINT cod_remittances_failure_recorded CHECK (
        (status <> 'FAILED') OR (failure_reason IS NOT NULL)
    )
);

CREATE UNIQUE INDEX cod_remittances_code_idx
    ON cod_remittances (organization_id, remittance_code);
CREATE INDEX cod_remittances_from_idx
    ON cod_remittances (organization_id, from_type, from_id, paid_on DESC);

CREATE TRIGGER cod_remittances_touch
    BEFORE UPDATE ON cod_remittances
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE cod_remittance_items (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    remittance_id bigint NOT NULL REFERENCES cod_remittances (id) ON DELETE CASCADE,
    obligation_id bigint NOT NULL REFERENCES cod_obligations (id) ON DELETE RESTRICT,
    amount_minor  bigint NOT NULL CHECK (amount_minor > 0),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX cod_remittance_items_unique_idx
    ON cod_remittance_items (remittance_id, obligation_id);
CREATE INDEX cod_remittance_items_obligation_idx ON cod_remittance_items (obligation_id);

-- ---------------------------------------------------------------------------
-- Adjustment — an approved correction. Maker/checker: whoever raises it cannot
-- be the one who approves it (enforced in the service and tested).
-- ---------------------------------------------------------------------------
CREATE TABLE cod_adjustments (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cda')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    obligation_id     bigint      REFERENCES cod_obligations (id) ON DELETE RESTRICT,
    reconciliation_id bigint      REFERENCES cod_reconciliations (id) ON DELETE RESTRICT,

    adjustment_type   text        NOT NULL CHECK (adjustment_type IN
                      ('SHORTAGE_RECOVERY','SHORTAGE_WRITE_OFF','EXCESS_REFUND','EXCESS_RETAINED',
                       'WAIVER','CORRECTION','DISPUTE_RESOLUTION')),
    -- Signed: what it does to the amount owed.
    amount_minor      bigint      NOT NULL,
    currency          char(3)     NOT NULL,

    -- Who bears it.
    liable_party_type text        CHECK (liable_party_type IN ('AGENT','OPERATING_UNIT','FRANCHISE','HEAD_OFFICE','CUSTOMER')),
    liable_party_id   bigint,

    reason            text        NOT NULL CHECK (length(btrim(reason)) >= 10),

    status            text        NOT NULL DEFAULT 'PENDING_APPROVAL' CHECK (status IN
                      ('PENDING_APPROVAL','APPROVED','REJECTED','POSTED')),

    requested_by      bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    requested_at      timestamptz NOT NULL DEFAULT now(),
    approved_by       bigint      REFERENCES users (id) ON DELETE RESTRICT,
    approved_at       timestamptz,
    rejection_reason  text,

    journal_transaction_id bigint REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cod_adjustments_approval_recorded CHECK (
        (status NOT IN ('APPROVED','POSTED')) OR (approved_by IS NOT NULL AND approved_at IS NOT NULL)
    ),
    CONSTRAINT cod_adjustments_rejection_recorded CHECK (
        (status <> 'REJECTED') OR (rejection_reason IS NOT NULL)
    ),
    -- Maker/checker, enforced in the database as well as the service: the
    -- approver must be a different person.
    CONSTRAINT cod_adjustments_maker_is_not_checker CHECK (
        approved_by IS NULL OR approved_by <> requested_by
    ),
    CONSTRAINT cod_adjustments_target CHECK (
        obligation_id IS NOT NULL OR reconciliation_id IS NOT NULL
    )
);

CREATE INDEX cod_adjustments_obligation_idx ON cod_adjustments (obligation_id);
CREATE INDEX cod_adjustments_pending_idx
    ON cod_adjustments (organization_id, requested_at DESC) WHERE status = 'PENDING_APPROVAL';

CREATE TRIGGER cod_adjustments_touch
    BEFORE UPDATE ON cod_adjustments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Dispute — a contested amount, parked so it does not block the rest.
-- ---------------------------------------------------------------------------
CREATE TABLE cod_disputes (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cdd')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    obligation_id     bigint      NOT NULL REFERENCES cod_obligations (id) ON DELETE RESTRICT,
    dispute_code      text        NOT NULL,

    raised_by_type    text        NOT NULL CHECK (raised_by_type IN ('AGENT','OPERATING_UNIT','FRANCHISE','CUSTOMER','HEAD_OFFICE')),
    raised_by_id      bigint,
    disputed_minor    bigint      NOT NULL CHECK (disputed_minor > 0),
    currency          char(3)     NOT NULL,

    category          text        NOT NULL CHECK (category IN
                      ('AMOUNT_MISMATCH','NOT_COLLECTED','ALREADY_REMITTED','FAKE_ENTRY','CUSTOMER_CLAIM','OTHER')),
    description       text        NOT NULL,

    status            text        NOT NULL DEFAULT 'OPEN' CHECK (status IN
                      ('OPEN','UNDER_REVIEW','RESOLVED','REJECTED','WITHDRAWN')),
    resolution        text,
    resolved_minor    bigint,
    resolved_by       bigint      REFERENCES users (id) ON DELETE SET NULL,
    resolved_at       timestamptz,
    adjustment_id     bigint      REFERENCES cod_adjustments (id) ON DELETE RESTRICT,

    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT cod_disputes_resolution_recorded CHECK (
        (status NOT IN ('RESOLVED','REJECTED')) OR (resolution IS NOT NULL AND resolved_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX cod_disputes_code_idx ON cod_disputes (organization_id, dispute_code);
CREATE UNIQUE INDEX cod_disputes_active_idx
    ON cod_disputes (obligation_id) WHERE status IN ('OPEN','UNDER_REVIEW');
CREATE INDEX cod_disputes_open_idx
    ON cod_disputes (organization_id, status, created_at DESC);

CREATE TRIGGER cod_disputes_touch
    BEFORE UPDATE ON cod_disputes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- A closed obligation is history.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION cod_obligations_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION
            'COD_IMMUTABLE: a COD obligation cannot be deleted; cancel or write it off instead'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.status IN ('CLOSED','WRITTEN_OFF','CANCELLED')
       AND (NEW.collected_minor IS DISTINCT FROM OLD.collected_minor
            OR NEW.remitted_minor IS DISTINCT FROM OLD.remitted_minor
            OR NEW.adjusted_minor IS DISTINCT FROM OLD.adjusted_minor
            OR NEW.expected_minor IS DISTINCT FROM OLD.expected_minor) THEN
        RAISE EXCEPTION
            'COD_IMMUTABLE: obligation % is % and its amounts cannot change; raise an adjustment',
            OLD.id, OLD.status
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- The expected amount is set at booking and never moves. Changing what the
    -- consignee owed after the fact is how COD fraud is hidden.
    IF NEW.expected_minor IS DISTINCT FROM OLD.expected_minor THEN
        RAISE EXCEPTION
            'COD_IMMUTABLE: the expected COD amount is fixed at booking; raise an adjustment instead'
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER cod_obligations_guard_trigger
    BEFORE UPDATE OR DELETE ON cod_obligations
    FOR EACH ROW EXECUTE FUNCTION cod_obligations_guard();
