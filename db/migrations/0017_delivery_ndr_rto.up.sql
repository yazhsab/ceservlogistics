-- 0017 Delivery runs (M16), NDR (M17) and RTO (M18).
--
-- Delivery is where a courier network earns or loses its reputation, and where
-- the concurrency risk is sharpest: two agents can hold pieces of the same
-- consignment, a customer can pay COD twice, an app can retry a completion over
-- a flaky connection. The controls here are:
--
--   * one live delivery-run item per shipment (partial unique index)
--   * one successful attempt per shipment      (partial unique index)
--   * device_event_id idempotency on attempts  (partial unique index)
--   * compare-and-swap on shipment status      (0011 ApplyShipmentTransition)
--
-- NDR is modelled as a case with attempts and actions, not as a status. RTO is
-- modelled as reverse movement over the same physical network, not as a status
-- flip: see ADR 0009.

-- ---------------------------------------------------------------------------
-- Delivery runs
-- ---------------------------------------------------------------------------
CREATE TABLE delivery_runs (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'drn')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    run_code          text        NOT NULL,

    branch_id         bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    agent_user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    run_date          date        NOT NULL,

    status            text        NOT NULL DEFAULT 'PLANNED' CHECK (status IN
                      ('PLANNED','ASSIGNED','DISPATCHED','IN_PROGRESS','COMPLETED','CLOSED','CANCELLED')),

    vehicle_reference text,
    planned_stops     integer     NOT NULL DEFAULT 0 CHECK (planned_stops >= 0),
    completed_stops   integer     NOT NULL DEFAULT 0 CHECK (completed_stops >= 0),
    delivered_count   integer     NOT NULL DEFAULT 0 CHECK (delivered_count >= 0),
    failed_count      integer     NOT NULL DEFAULT 0 CHECK (failed_count >= 0),

    -- COD the agent is expected to bring back. The authoritative COD custody
    -- ledger arrives in Release 3; this is the operational expectation, in
    -- integer minor units per §12.
    cod_expected_minor bigint     NOT NULL DEFAULT 0 CHECK (cod_expected_minor >= 0),
    cod_collected_minor bigint    NOT NULL DEFAULT 0 CHECK (cod_collected_minor >= 0),
    currency          char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),

    dispatched_at     timestamptz,
    dispatched_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    started_at        timestamptz,
    completed_at      timestamptz,
    closed_at         timestamptz,
    closed_by_user_id bigint      REFERENCES users (id) ON DELETE SET NULL,
    cancelled_at      timestamptz,
    cancellation_reason text,

    created_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT delivery_runs_code_unique UNIQUE (organization_id, run_code),
    CONSTRAINT delivery_runs_dispatch_recorded
        CHECK (status NOT IN ('DISPATCHED','IN_PROGRESS','COMPLETED','CLOSED') OR dispatched_at IS NOT NULL),
    CONSTRAINT delivery_runs_cancellation_recorded
        CHECK (status <> 'CANCELLED' OR (cancelled_at IS NOT NULL AND cancellation_reason IS NOT NULL))
);

CREATE INDEX delivery_runs_agent_date_idx ON delivery_runs (agent_user_id, run_date DESC, status);
CREATE INDEX delivery_runs_branch_date_idx ON delivery_runs (branch_id, run_date DESC, status);
CREATE INDEX delivery_runs_org_created_idx ON delivery_runs (organization_id, created_at DESC, id DESC);
CREATE INDEX delivery_runs_open_idx ON delivery_runs (branch_id, run_date)
    WHERE status IN ('PLANNED','ASSIGNED','DISPATCHED','IN_PROGRESS');

CREATE TRIGGER delivery_runs_touch BEFORE UPDATE ON delivery_runs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER delivery_runs_bump_version BEFORE UPDATE ON delivery_runs
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Delivery run items — the ordered stops
-- ---------------------------------------------------------------------------
CREATE TABLE delivery_run_items (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dri')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    delivery_run_id   bigint      NOT NULL REFERENCES delivery_runs (id) ON DELETE CASCADE,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE RESTRICT,

    stop_sequence     integer     NOT NULL CHECK (stop_sequence >= 1),
    status            text        NOT NULL DEFAULT 'PENDING' CHECK (status IN
                      ('PENDING','OUT_FOR_DELIVERY','DELIVERED','FAILED','RETURNED_TO_BRANCH','REMOVED')),

    -- Direction matters: the same run can carry forward deliveries and RTO
    -- returns to senders.
    delivery_type     text        NOT NULL DEFAULT 'FORWARD'
                                  CHECK (delivery_type IN ('FORWARD','RTO')),

    cod_amount_minor  bigint      NOT NULL DEFAULT 0 CHECK (cod_amount_minor >= 0),
    cod_collected_minor bigint    NOT NULL DEFAULT 0 CHECK (cod_collected_minor >= 0),

    added_at          timestamptz NOT NULL DEFAULT now(),
    added_by_user_id  bigint      REFERENCES users (id) ON DELETE SET NULL,
    dispatched_at     timestamptz,
    completed_at      timestamptz,
    removed_at        timestamptz,
    removal_reason    text,
    attempt_count     integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),

    CONSTRAINT delivery_run_items_unique UNIQUE (delivery_run_id, shipment_id),
    CONSTRAINT delivery_run_items_removal_recorded
        CHECK (status <> 'REMOVED' OR (removed_at IS NOT NULL AND removal_reason IS NOT NULL))
);

-- A shipment can be out with only one agent at a time. This is the structural
-- guard against two agents both delivering the same parcel.
CREATE UNIQUE INDEX delivery_run_items_active_idx
    ON delivery_run_items (shipment_id)
    WHERE status IN ('PENDING','OUT_FOR_DELIVERY');
CREATE UNIQUE INDEX delivery_run_items_stop_idx
    ON delivery_run_items (delivery_run_id, stop_sequence)
    WHERE status <> 'REMOVED';
CREATE INDEX delivery_run_items_run_idx ON delivery_run_items (delivery_run_id, stop_sequence);
CREATE INDEX delivery_run_items_shipment_idx ON delivery_run_items (shipment_id, added_at DESC);

-- ---------------------------------------------------------------------------
-- Delivery attempts (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE delivery_attempts (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dat')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    delivery_run_id   bigint      REFERENCES delivery_runs (id) ON DELETE SET NULL,
    delivery_run_item_id bigint   REFERENCES delivery_run_items (id) ON DELETE SET NULL,
    attempt_number    integer     NOT NULL CHECK (attempt_number >= 1),

    outcome           text        NOT NULL CHECK (outcome IN ('DELIVERED','FAILED','RESCHEDULED','CANCELLED')),
    failure_reason_code text,
    remarks           text,

    -- Recipient details, captured at the door. The POD record holds the
    -- evidence; this holds the facts the agent typed.
    recipient_name    text,
    recipient_relationship text    CHECK (recipient_relationship IS NULL OR recipient_relationship IN
                      ('SELF','FAMILY','NEIGHBOUR','SECURITY','RECEPTION','COLLEAGUE','OTHER')),
    recipient_phone   text,

    -- OTP verification. The code itself is never stored: only its digest, the
    -- same discipline as passwords (§35). A delivery agent must not be able to
    -- read the OTP out of a debug endpoint or a log.
    otp_required      boolean     NOT NULL DEFAULT false,
    otp_verified      boolean     NOT NULL DEFAULT false,
    otp_verified_at   timestamptz,

    cod_amount_minor  bigint      NOT NULL DEFAULT 0 CHECK (cod_amount_minor >= 0),
    cod_collected_minor bigint    NOT NULL DEFAULT 0 CHECK (cod_collected_minor >= 0),
    cod_payment_mode  text        CHECK (cod_payment_mode IS NULL OR cod_payment_mode IN
                      ('CASH','UPI','CARD','WALLET','BANK_TRANSFER','CHEQUE')),
    cod_reference     text,
    currency          char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),

    agent_user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    operating_unit_id bigint      REFERENCES operating_units (id) ON DELETE SET NULL,
    occurred_at       timestamptz NOT NULL DEFAULT now(),
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    latitude          double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude         double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    location_accuracy_m integer   CHECK (location_accuracy_m IS NULL OR location_accuracy_m >= 0),
    device_id         text,
    device_event_id   text,
    next_attempt_at   timestamptz,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT delivery_attempts_number_unique UNIQUE (shipment_id, attempt_number),
    CONSTRAINT delivery_attempts_failure_reason
        CHECK (outcome NOT IN ('FAILED','RESCHEDULED') OR failure_reason_code IS NOT NULL),
    CONSTRAINT delivery_attempts_delivery_recipient
        CHECK (outcome <> 'DELIVERED' OR recipient_name IS NOT NULL),
    CONSTRAINT delivery_attempts_cod_collected
        CHECK (cod_collected_minor = 0 OR cod_payment_mode IS NOT NULL)
);

-- Exactly one successful delivery per shipment, ever. This is the last line of
-- defence against double delivery: even if two transactions somehow both pass
-- the state check, only one can insert.
CREATE UNIQUE INDEX delivery_attempts_success_idx
    ON delivery_attempts (shipment_id) WHERE outcome = 'DELIVERED';
-- Offline retry safety for the field app.
CREATE UNIQUE INDEX delivery_attempts_device_event_idx
    ON delivery_attempts (organization_id, device_id, device_event_id)
    WHERE device_event_id IS NOT NULL AND device_id IS NOT NULL;
CREATE INDEX delivery_attempts_shipment_idx ON delivery_attempts (shipment_id, attempt_number DESC);
CREATE INDEX delivery_attempts_run_idx ON delivery_attempts (delivery_run_id, occurred_at DESC)
    WHERE delivery_run_id IS NOT NULL;
CREATE INDEX delivery_attempts_agent_idx ON delivery_attempts (agent_user_id, occurred_at DESC)
    WHERE agent_user_id IS NOT NULL;

CREATE TRIGGER delivery_attempts_append_only
    BEFORE UPDATE OR DELETE ON delivery_attempts
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Delivery OTP
-- ---------------------------------------------------------------------------
-- Short-lived, single-use, digest-only. Kept in PostgreSQL rather than Redis
-- because a lost Redis must not let a parcel be delivered without the code.
CREATE TABLE delivery_otps (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id     bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    code_hash       bytea       NOT NULL,
    -- Last 4 digits of the recipient phone the code was sent to, for support
    -- to confirm "we sent it to the number ending 4821" without exposing it.
    sent_to_masked  text,
    issued_at       timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    consumed_at     timestamptz,
    attempt_count   integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts    integer     NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 10),
    issued_by_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT delivery_otps_expiry CHECK (expires_at > issued_at)
);

CREATE UNIQUE INDEX delivery_otps_active_idx
    ON delivery_otps (shipment_id) WHERE consumed_at IS NULL;
CREATE INDEX delivery_otps_expiry_idx ON delivery_otps (expires_at) WHERE consumed_at IS NULL;

-- ---------------------------------------------------------------------------
-- NDR reason catalogue (configurable, per §18)
-- ---------------------------------------------------------------------------
CREATE TABLE ndr_reasons (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ndrs')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code            text        NOT NULL CHECK (code ~ '^[A-Z0-9_]{2,40}$'),
    name            text        NOT NULL,
    description     text,
    category        text        NOT NULL CHECK (category IN
                    ('CUSTOMER_UNAVAILABLE','ADDRESS_PROBLEM','REFUSED','PAYMENT','ACCESS',
                     'OPERATIONAL','WEATHER','DAMAGE','OTHER')),
    -- What operations should normally do next. The operator can override.
    default_action  text        NOT NULL DEFAULT 'REATTEMPT' CHECK (default_action IN
                    ('REATTEMPT','RESCHEDULE','CONTACT_REQUIRED','ADDRESS_CORRECTION',
                     'CUSTOMER_PICKUP','RTO','ESCALATE')),
    -- Attempts allowed before the case is forced to a terminal decision.
    max_attempts    integer     NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
    -- True when the failure is the customer's doing, which matters for who
    -- bears the RTO charge in Release 3.
    is_customer_fault boolean   NOT NULL DEFAULT false,
    requires_evidence boolean   NOT NULL DEFAULT false,
    auto_rto_after_max boolean  NOT NULL DEFAULT true,
    is_active       boolean     NOT NULL DEFAULT true,
    display_order   integer     NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ndr_reasons_code_unique UNIQUE (organization_id, code)
);

CREATE INDEX ndr_reasons_org_active_idx ON ndr_reasons (organization_id, is_active, display_order);
CREATE TRIGGER ndr_reasons_touch BEFORE UPDATE ON ndr_reasons
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- NDR cases
-- ---------------------------------------------------------------------------
CREATE TABLE ndr_cases (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ndr')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    case_code         text        NOT NULL,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,

    status            text        NOT NULL DEFAULT 'OPEN' CHECK (status IN
                      ('OPEN','PENDING_CUSTOMER','SCHEDULED','ESCALATED','RESOLVED_DELIVERED',
                       'RESOLVED_RTO','RESOLVED_CANCELLED','CLOSED')),

    ndr_reason_id     bigint      REFERENCES ndr_reasons (id) ON DELETE SET NULL,
    current_reason_code text      NOT NULL,
    attempt_count     integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts      integer     NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),

    branch_id         bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    assigned_to_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,

    -- The decision currently in force, and when it should be acted on.
    current_action    text        CHECK (current_action IS NULL OR current_action IN
                      ('REATTEMPT','RESCHEDULE','CONTACT_REQUIRED','ADDRESS_CORRECTION',
                       'CUSTOMER_PICKUP','RTO','ESCALATE')),
    next_attempt_at   timestamptz,

    -- Corrected delivery details, when the resolution was an address fix. The
    -- original shipment address snapshot is never rewritten (§13).
    corrected_address_line1 text,
    corrected_address_line2 text,
    corrected_landmark text,
    corrected_pincode text,
    corrected_phone   text,
    corrected_at      timestamptz,
    corrected_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,

    opened_at         timestamptz NOT NULL DEFAULT now(),
    resolved_at       timestamptz,
    resolution_notes  text,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT ndr_cases_code_unique UNIQUE (organization_id, case_code),
    CONSTRAINT ndr_cases_resolution_recorded
        CHECK (status NOT IN ('RESOLVED_DELIVERED','RESOLVED_RTO','RESOLVED_CANCELLED','CLOSED')
               OR resolved_at IS NOT NULL)
);

-- One open case per shipment: a second failure joins the existing case as
-- another attempt rather than starting a parallel one.
CREATE UNIQUE INDEX ndr_cases_active_idx ON ndr_cases (shipment_id)
    WHERE status IN ('OPEN','PENDING_CUSTOMER','SCHEDULED','ESCALATED');
CREATE INDEX ndr_cases_branch_status_idx ON ndr_cases (branch_id, status, next_attempt_at);
CREATE INDEX ndr_cases_org_created_idx ON ndr_cases (organization_id, created_at DESC, id DESC);
CREATE INDEX ndr_cases_shipment_idx ON ndr_cases (shipment_id, opened_at DESC);
CREATE INDEX ndr_cases_due_idx ON ndr_cases (organization_id, next_attempt_at)
    WHERE status IN ('OPEN','SCHEDULED') AND next_attempt_at IS NOT NULL;

CREATE TRIGGER ndr_cases_touch BEFORE UPDATE ON ndr_cases FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER ndr_cases_bump_version BEFORE UPDATE ON ndr_cases FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- NDR attempts and actions (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE ndr_attempts (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'nat')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    ndr_case_id     bigint      NOT NULL REFERENCES ndr_cases (id) ON DELETE CASCADE,
    delivery_attempt_id bigint  REFERENCES delivery_attempts (id) ON DELETE SET NULL,
    attempt_number  integer     NOT NULL CHECK (attempt_number >= 1),
    reason_code     text        NOT NULL,
    remarks         text,
    -- Evidence uploaded by the agent (photo of a locked door, for instance).
    evidence_object_keys text[] NOT NULL DEFAULT ARRAY[]::text[],
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    recorded_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT ndr_attempts_number_unique UNIQUE (ndr_case_id, attempt_number)
);

CREATE INDEX ndr_attempts_case_idx ON ndr_attempts (ndr_case_id, attempt_number DESC);
CREATE TRIGGER ndr_attempts_append_only
    BEFORE UPDATE OR DELETE ON ndr_attempts
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

CREATE TABLE ndr_actions (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'nac')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    ndr_case_id     bigint      NOT NULL REFERENCES ndr_cases (id) ON DELETE CASCADE,
    sequence        integer     NOT NULL CHECK (sequence >= 1),

    action          text        NOT NULL CHECK (action IN
                    ('REATTEMPT','RESCHEDULE','CONTACT_REQUIRED','ADDRESS_CORRECTION',
                     'CUSTOMER_PICKUP','RTO','ESCALATE','CLOSE')),
    -- Who asked for it: operations, the customer over the phone, the consignee
    -- through a self-service link, or an automatic policy.
    requested_by    text        NOT NULL DEFAULT 'OPERATIONS' CHECK (requested_by IN
                    ('OPERATIONS','CUSTOMER','CONSIGNEE','SYSTEM','PARTNER')),
    instructions    text,
    scheduled_for   timestamptz,
    contact_name    text,
    contact_phone   text,
    contact_notes   text,

    status          text        NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING','APPLIED','SUPERSEDED','CANCELLED')),
    applied_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    created_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT ndr_actions_sequence_unique UNIQUE (ndr_case_id, sequence)
);

-- At most one pending instruction per case, so an agent is never shown two
-- conflicting directions.
CREATE UNIQUE INDEX ndr_actions_pending_idx ON ndr_actions (ndr_case_id) WHERE status = 'PENDING';
CREATE INDEX ndr_actions_case_idx ON ndr_actions (ndr_case_id, sequence DESC);

-- ---------------------------------------------------------------------------
-- RTO cases
-- ---------------------------------------------------------------------------
-- RTO reuses the forward network. The case tracks the reverse journey, the
-- reverse route resolved for it, and where the parcel currently is on that
-- journey. Physical movement itself uses the same bags, manifests and trips.
CREATE TABLE rto_cases (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rto')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    case_code         text        NOT NULL,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    ndr_case_id       bigint      REFERENCES ndr_cases (id) ON DELETE SET NULL,

    status            text        NOT NULL DEFAULT 'INITIATED' CHECK (status IN
                      ('INITIATED','IN_TRANSIT','AT_ORIGIN_BRANCH','OUT_FOR_RETURN',
                       'RETURNED','RETURN_FAILED','DISPOSED','CANCELLED')),

    reason_code       text        NOT NULL,
    reason_notes      text,
    initiated_at      timestamptz NOT NULL DEFAULT now(),
    initiated_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,

    -- Reverse path, resolved by the routing engine with origin and destination
    -- swapped, or supplied as an explicit override.
    return_branch_id  bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    return_hub_id     bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    origin_hub_id     bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    route_resolution_source text  CHECK (route_resolution_source IS NULL OR route_resolution_source IN
                      ('REVERSE_ROUTING','OVERRIDE','SAME_BRANCH')),
    route_legs        jsonb       NOT NULL DEFAULT '[]'::jsonb,
    route_explanation jsonb       NOT NULL DEFAULT '{}'::jsonb,

    -- Return address snapshot: normally the sender, but a business customer can
    -- nominate a different returns warehouse.
    return_contact_name text,
    return_phone      text,
    return_line1      text,
    return_line2      text,
    return_city       text,
    return_state      text,
    return_pincode    text,

    -- Charges are computed here and settled in Release 3.
    rto_charge_minor  bigint      NOT NULL DEFAULT 0 CHECK (rto_charge_minor >= 0),
    currency          char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    charge_bearer     text        NOT NULL DEFAULT 'CUSTOMER'
                                  CHECK (charge_bearer IN ('CUSTOMER','FRANCHISE','ORGANIZATION','WAIVED')),
    charge_rule_notes text,

    returned_at       timestamptz,
    returned_to_name  text,
    closed_at         timestamptz,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT rto_cases_code_unique UNIQUE (organization_id, case_code)
);

CREATE UNIQUE INDEX rto_cases_active_idx ON rto_cases (shipment_id)
    WHERE status NOT IN ('RETURNED','DISPOSED','CANCELLED');
CREATE INDEX rto_cases_org_created_idx ON rto_cases (organization_id, created_at DESC, id DESC);
CREATE INDEX rto_cases_branch_idx ON rto_cases (return_branch_id, status)
    WHERE return_branch_id IS NOT NULL;
CREATE INDEX rto_cases_shipment_idx ON rto_cases (shipment_id, initiated_at DESC);

CREATE TRIGGER rto_cases_touch BEFORE UPDATE ON rto_cases FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER rto_cases_bump_version BEFORE UPDATE ON rto_cases FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- Reverse movement legs, appended as the parcel travels back. This is how
-- "track all reverse movement through branch, hub, line haul, origin" is
-- satisfied without inventing a parallel set of shipment statuses.
CREATE TABLE rto_legs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    rto_case_id     bigint      NOT NULL REFERENCES rto_cases (id) ON DELETE CASCADE,
    sequence        integer     NOT NULL CHECK (sequence >= 1),
    from_unit_id    bigint      REFERENCES operating_units (id) ON DELETE SET NULL,
    to_unit_id      bigint      REFERENCES operating_units (id) ON DELETE SET NULL,
    leg_type        text        NOT NULL CHECK (leg_type IN
                    ('BRANCH_TO_HUB','HUB_TO_HUB','HUB_TO_BRANCH','BRANCH_TO_SENDER','DIRECT')),
    status          text        NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING','IN_PROGRESS','COMPLETED','SKIPPED')),
    trip_id         bigint      REFERENCES trips (id) ON DELETE SET NULL,
    manifest_id     bigint      REFERENCES manifests (id) ON DELETE SET NULL,
    started_at      timestamptz,
    completed_at    timestamptz,
    CONSTRAINT rto_legs_sequence_unique UNIQUE (rto_case_id, sequence)
);

CREATE INDEX rto_legs_case_idx ON rto_legs (rto_case_id, sequence);
