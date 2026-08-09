-- 0021 Commission engine (M21).
--
-- Commission is versioned configuration plus an immutable record of every
-- calculation (§24). The two must not be confused:
--
--   * commission_rules holds *what the policy is now*.
--   * commission_rule_versions holds *what the policy was on any past date*.
--   * commission_calculations holds *what was actually computed*, including a
--     full copy of the inputs and the arithmetic trace.
--
-- The consequence is the property the Constitution demands: editing a rate card
-- or a commission rule today can never change what a franchise earned last
-- month, because last month's calculation already stored its own version id,
-- its own inputs and its own result.

-- ---------------------------------------------------------------------------
-- Schemes — a named bundle of rules, assigned to franchises by code.
--
-- franchise_agreements.commission_plan_code already carries the assignment, so
-- this table gives that code a definition rather than introducing a second
-- assignment mechanism.
-- ---------------------------------------------------------------------------
CREATE TABLE commission_schemes (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'csm')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    code            text        NOT NULL,
    name            text        NOT NULL,
    description     text,
    currency        char(3)     NOT NULL,

    is_default      boolean     NOT NULL DEFAULT false,
    status          text        NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('DRAFT','ACTIVE','INACTIVE')),

    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX commission_schemes_code_idx
    ON commission_schemes (organization_id, code);
-- At most one default scheme, so an unassigned franchise resolves deterministically.
CREATE UNIQUE INDEX commission_schemes_default_idx
    ON commission_schemes (organization_id) WHERE is_default AND status = 'ACTIVE';

CREATE TRIGGER commission_schemes_touch
    BEFORE UPDATE ON commission_schemes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Rules — the scope. What has to be true for this rule to apply.
--
-- Every scope column is nullable, and NULL means "any". specificity is derived
-- from how many are non-NULL and is the primary sort key when several rules
-- match, so a rule naming a franchise and a service always beats a rule naming
-- only a service. It is maintained by trigger rather than by the application,
-- because a wrong specificity silently pays the wrong party.
-- ---------------------------------------------------------------------------
CREATE TABLE commission_rules (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'crl')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    scheme_id           bigint      NOT NULL REFERENCES commission_schemes (id) ON DELETE RESTRICT,

    code                text        NOT NULL,
    name                text        NOT NULL,

    commission_type     text        NOT NULL CHECK (commission_type IN
                        ('BOOKING','PICKUP','ORIGIN_HANDLING','DESTINATION_HANDLING',
                         'DELIVERY','COD','VOLUME_INCENTIVE','CUSTOM')),

    -- Who earns it. The recipient is resolved at calculation time from the
    -- shipment's operational facts; this says which role of the journey earns.
    recipient_role      text        NOT NULL CHECK (recipient_role IN
                        ('ORIGIN_FRANCHISE','DESTINATION_FRANCHISE','PICKUP_AGENT',
                         'DELIVERY_AGENT','ORIGIN_UNIT','DESTINATION_UNIT','CUSTOM')),

    -- Scope. NULL = matches anything.
    franchise_id        bigint      REFERENCES franchises (id) ON DELETE CASCADE,
    franchise_category  text,
    operating_unit_id   bigint      REFERENCES operating_units (id) ON DELETE CASCADE,
    service_id          bigint      REFERENCES courier_services (id) ON DELETE CASCADE,
    origin_zone_id      bigint      REFERENCES zones (id) ON DELETE CASCADE,
    destination_zone_id bigint      REFERENCES zones (id) ON DELETE CASCADE,
    customer_category   text,
    payment_mode        text        CHECK (payment_mode IS NULL OR payment_mode IN ('PREPAID','COD','CREDIT','TO_PAY')),

    -- Derived; see commission_rules_specificity() below.
    specificity         integer     NOT NULL DEFAULT 0,
    -- Manual override for the rare genuine tie. Higher wins.
    priority            integer     NOT NULL DEFAULT 0,

    status              text        NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('DRAFT','ACTIVE','INACTIVE')),
    description         text,

    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX commission_rules_code_idx
    ON commission_rules (organization_id, code);
-- The resolution query: candidates for a type, most specific first.
CREATE INDEX commission_rules_resolve_idx
    ON commission_rules (organization_id, commission_type, specificity DESC, priority DESC, id)
    WHERE status = 'ACTIVE';
CREATE INDEX commission_rules_scheme_idx ON commission_rules (scheme_id);
CREATE INDEX commission_rules_franchise_idx
    ON commission_rules (organization_id, franchise_id) WHERE franchise_id IS NOT NULL;

-- Specificity is the count of bound scope dimensions. Computed in the database
-- so it cannot drift from the columns it describes.
CREATE OR REPLACE FUNCTION commission_rules_specificity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.specificity :=
        (CASE WHEN NEW.franchise_id        IS NOT NULL THEN 32 ELSE 0 END) +
        (CASE WHEN NEW.operating_unit_id   IS NOT NULL THEN 16 ELSE 0 END) +
        (CASE WHEN NEW.franchise_category  IS NOT NULL THEN  8 ELSE 0 END) +
        (CASE WHEN NEW.service_id          IS NOT NULL THEN  4 ELSE 0 END) +
        (CASE WHEN NEW.origin_zone_id      IS NOT NULL THEN  2 ELSE 0 END) +
        (CASE WHEN NEW.destination_zone_id IS NOT NULL THEN  2 ELSE 0 END) +
        (CASE WHEN NEW.customer_category   IS NOT NULL THEN  1 ELSE 0 END) +
        (CASE WHEN NEW.payment_mode        IS NOT NULL THEN  1 ELSE 0 END);
    RETURN NEW;
END;
$$;

CREATE TRIGGER commission_rules_specificity_trigger
    BEFORE INSERT OR UPDATE ON commission_rules
    FOR EACH ROW EXECUTE FUNCTION commission_rules_specificity();

CREATE TRIGGER commission_rules_touch
    BEFORE UPDATE ON commission_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Rule versions — the arithmetic, effective-dated.
--
-- A version is immutable once it has been used by a calculation. Changing a
-- rate means adding a version with a later effective_from, exactly as rate
-- cards work in Release 1.
-- ---------------------------------------------------------------------------
CREATE TABLE commission_rule_versions (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id          text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'crv')),
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    rule_id            bigint      NOT NULL REFERENCES commission_rules (id) ON DELETE RESTRICT,

    version_no         integer     NOT NULL CHECK (version_no > 0),

    calculation_method text        NOT NULL CHECK (calculation_method IN ('FIXED','PERCENTAGE','SLAB')),
    currency           char(3)     NOT NULL,

    -- FIXED: paid per qualifying shipment.
    fixed_amount_minor bigint      CHECK (fixed_amount_minor IS NULL OR fixed_amount_minor >= 0),

    -- PERCENTAGE: basis points against a chosen base (§12 — no floats).
    rate_bp            integer     CHECK (rate_bp IS NULL OR (rate_bp >= 0 AND rate_bp <= 1000000)),
    -- Which number the percentage applies to. Naming it explicitly avoids the
    -- classic dispute about whether commission is on freight or on the total.
    basis              text        CHECK (basis IS NULL OR basis IN
                       ('FREIGHT','SURCHARGE','FREIGHT_PLUS_SURCHARGE','TOTAL_BEFORE_TAX',
                        'TOTAL','COD_AMOUNT','CHARGEABLE_WEIGHT','SHIPMENT_COUNT')),

    -- SLAB: bands held as JSON, validated on write by the service and by the
    -- CHECK below. Shape: [{"fromMinor":0,"toMinor":100000,"amountMinor":500,"rateBp":null}, ...]
    slabs              jsonb,

    min_amount_minor   bigint      CHECK (min_amount_minor IS NULL OR min_amount_minor >= 0),
    max_amount_minor   bigint      CHECK (max_amount_minor IS NULL OR max_amount_minor >= 0),

    effective_from     date        NOT NULL,
    effective_to       date,

    status             text        NOT NULL DEFAULT 'ACTIVE'
                       CHECK (status IN ('DRAFT','ACTIVE','SUPERSEDED')),

    notes              text,
    created_by         bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT commission_rule_versions_range CHECK (
        effective_to IS NULL OR effective_to >= effective_from
    ),
    CONSTRAINT commission_rule_versions_bounds CHECK (
        min_amount_minor IS NULL OR max_amount_minor IS NULL OR max_amount_minor >= min_amount_minor
    ),
    -- Each method must carry the fields it needs and no others, so a
    -- half-configured rule cannot reach the calculator.
    CONSTRAINT commission_rule_versions_method_complete CHECK (
        (calculation_method = 'FIXED'      AND fixed_amount_minor IS NOT NULL) OR
        (calculation_method = 'PERCENTAGE' AND rate_bp IS NOT NULL AND basis IS NOT NULL) OR
        (calculation_method = 'SLAB'       AND slabs IS NOT NULL
                                           AND jsonb_typeof(slabs) = 'array'
                                           AND jsonb_array_length(slabs) > 0
                                           AND basis IS NOT NULL)
    )
);

CREATE UNIQUE INDEX commission_rule_versions_no_idx
    ON commission_rule_versions (rule_id, version_no);
-- Effective-dated lookup: the version in force on a date.
CREATE INDEX commission_rule_versions_effective_idx
    ON commission_rule_versions (rule_id, effective_from DESC, id DESC)
    WHERE status = 'ACTIVE';

-- A version that has been used to calculate money is frozen. This is the
-- protection behind "historical values never recalculate": there is no way to
-- edit the arithmetic a past calculation referenced.
CREATE OR REPLACE FUNCTION commission_rule_versions_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    used bigint;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT count(*) INTO used FROM commission_calculations WHERE rule_version_id = OLD.id;
        IF used > 0 THEN
            RAISE EXCEPTION
                'COMMISSION_VERSION_IN_USE: version % has produced % calculation(s) and cannot be deleted',
                OLD.id, used
                USING ERRCODE = 'restrict_violation';
        END IF;
        RETURN OLD;
    END IF;

    -- Status and effective_to may still move (that is how a version is
    -- superseded). The arithmetic may not.
    IF NEW.calculation_method IS DISTINCT FROM OLD.calculation_method
       OR NEW.fixed_amount_minor IS DISTINCT FROM OLD.fixed_amount_minor
       OR NEW.rate_bp            IS DISTINCT FROM OLD.rate_bp
       OR NEW.basis              IS DISTINCT FROM OLD.basis
       OR NEW.slabs              IS DISTINCT FROM OLD.slabs
       OR NEW.min_amount_minor   IS DISTINCT FROM OLD.min_amount_minor
       OR NEW.max_amount_minor   IS DISTINCT FROM OLD.max_amount_minor
       OR NEW.effective_from     IS DISTINCT FROM OLD.effective_from THEN
        SELECT count(*) INTO used FROM commission_calculations WHERE rule_version_id = OLD.id;
        IF used > 0 THEN
            RAISE EXCEPTION
                'COMMISSION_VERSION_IMMUTABLE: version % has produced % calculation(s); create a new version instead',
                OLD.id, used
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

-- ---------------------------------------------------------------------------
-- Calculations — the immutable record of one computed commission.
--
-- Everything needed to explain and to reproduce the number is stored on the
-- row: the rule version, the base amount, the method, and a JSON trace of the
-- arithmetic. A dispute six months later is answered from this row alone.
-- ---------------------------------------------------------------------------
CREATE TABLE commission_calculations (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ccl')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    commission_type     text        NOT NULL CHECK (commission_type IN
                        ('BOOKING','PICKUP','ORIGIN_HANDLING','DESTINATION_HANDLING',
                         'DELIVERY','COD','VOLUME_INCENTIVE','CUSTOM')),

    -- Source. shipment_id is NULL for aggregate commissions such as
    -- VOLUME_INCENTIVE, which qualify on a period rather than on one parcel.
    shipment_id         bigint      REFERENCES shipments (id) ON DELETE RESTRICT,
    -- The operational fact that made this commission due — a delivery, a
    -- pickup completion, a period close.
    qualifying_event    text        NOT NULL,
    qualifying_event_id bigint,
    qualified_at        timestamptz NOT NULL,

    -- Who earns it.
    recipient_type      text        NOT NULL CHECK (recipient_type IN ('FRANCHISE','OPERATING_UNIT','AGENT')),
    recipient_id        bigint      NOT NULL,
    franchise_id        bigint      REFERENCES franchises (id) ON DELETE RESTRICT,
    operating_unit_id   bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,

    -- The rule that decided it, pinned to the exact version.
    rule_id             bigint      NOT NULL REFERENCES commission_rules (id) ON DELETE RESTRICT,
    rule_version_id     bigint      NOT NULL REFERENCES commission_rule_versions (id) ON DELETE RESTRICT,
    rule_code           text        NOT NULL,
    rule_version_no     integer     NOT NULL,

    calculation_method  text        NOT NULL CHECK (calculation_method IN ('FIXED','PERCENTAGE','SLAB')),
    basis               text,
    -- The number the method was applied to.
    base_amount_minor   bigint      NOT NULL DEFAULT 0,
    rate_bp             integer,
    -- Before min/max clamping, so a clamp is visible rather than silent.
    gross_amount_minor  bigint      NOT NULL DEFAULT 0,
    amount_minor        bigint      NOT NULL,
    currency            char(3)     NOT NULL,

    -- Full inputs and a step-by-step trace. This is what an operator is shown
    -- when they ask "why is this 42.50?".
    calculation_inputs  jsonb       NOT NULL DEFAULT '{}'::jsonb,
    calculation_trace   jsonb       NOT NULL DEFAULT '[]'::jsonb,

    status              text        NOT NULL DEFAULT 'CALCULATED'
                        CHECK (status IN ('CALCULATED','POSTED','REVERSED','CANCELLED')),

    -- Ledger linkage, filled when posted.
    journal_transaction_id bigint   REFERENCES journal_transactions (id) ON DELETE RESTRICT,
    posted_at           timestamptz,
    -- Settlement linkage, filled when swept into a settlement.
    settlement_id       bigint,
    reversed_by_id      bigint      REFERENCES commission_calculations (id) ON DELETE RESTRICT,

    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    request_id          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT commission_calculations_posted_recorded CHECK (
        (status <> 'POSTED') OR (journal_transaction_id IS NOT NULL AND posted_at IS NOT NULL)
    )
);

-- The idempotency guard for shipment-scoped commission: one live calculation
-- per (shipment, type, recipient). A retried delivery webhook cannot pay twice.
CREATE UNIQUE INDEX commission_calculations_shipment_idx
    ON commission_calculations (organization_id, shipment_id, commission_type, recipient_type, recipient_id)
    WHERE shipment_id IS NOT NULL AND status IN ('CALCULATED','POSTED');

CREATE INDEX commission_calculations_recipient_idx
    ON commission_calculations (organization_id, recipient_type, recipient_id, qualified_at DESC);
-- The settlement sweep: everything posted for a franchise in a period that is
-- not yet on a settlement.
CREATE INDEX commission_calculations_sweep_idx
    ON commission_calculations (organization_id, franchise_id, qualified_at)
    WHERE status = 'POSTED' AND settlement_id IS NULL;
CREATE INDEX commission_calculations_settlement_idx
    ON commission_calculations (settlement_id) WHERE settlement_id IS NOT NULL;
CREATE INDEX commission_calculations_version_idx
    ON commission_calculations (rule_version_id);

CREATE TRIGGER commission_calculations_touch
    BEFORE UPDATE ON commission_calculations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Now that commission_calculations exists, the version guard can reference it.
CREATE TRIGGER commission_rule_versions_guard_trigger
    BEFORE UPDATE OR DELETE ON commission_rule_versions
    FOR EACH ROW EXECUTE FUNCTION commission_rule_versions_guard();

-- A posted calculation is history. Only the linkage fields a later stage fills
-- in may still move.
CREATE OR REPLACE FUNCTION commission_calculations_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status = 'CALCULATED' AND OLD.journal_transaction_id IS NULL THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION
            'COMMISSION_IMMUTABLE: a posted commission cannot be deleted; reverse it instead'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF OLD.status = 'CALCULATED' THEN
        RETURN NEW;
    END IF;

    IF NEW.amount_minor        IS DISTINCT FROM OLD.amount_minor
       OR NEW.gross_amount_minor IS DISTINCT FROM OLD.gross_amount_minor
       OR NEW.base_amount_minor  IS DISTINCT FROM OLD.base_amount_minor
       OR NEW.rule_version_id    IS DISTINCT FROM OLD.rule_version_id
       OR NEW.recipient_id       IS DISTINCT FROM OLD.recipient_id
       OR NEW.recipient_type     IS DISTINCT FROM OLD.recipient_type
       OR NEW.currency           IS DISTINCT FROM OLD.currency
       OR NEW.calculation_trace  IS DISTINCT FROM OLD.calculation_trace THEN
        RAISE EXCEPTION
            'COMMISSION_IMMUTABLE: posted commission % cannot be edited; reverse and recalculate',
            OLD.id
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER commission_calculations_guard_trigger
    BEFORE UPDATE OR DELETE ON commission_calculations
    FOR EACH ROW EXECUTE FUNCTION commission_calculations_guard();

-- ---------------------------------------------------------------------------
-- Commission entries — the per-recipient running detail a franchise sees.
--
-- This is a projection of calculations for presentation and settlement sweep,
-- not a second source of truth: it carries no balance column, and every row
-- points back at the calculation that produced it.
-- ---------------------------------------------------------------------------
CREATE TABLE commission_entries (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cen')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    calculation_id    bigint      NOT NULL REFERENCES commission_calculations (id) ON DELETE RESTRICT,
    franchise_id      bigint      REFERENCES franchises (id) ON DELETE RESTRICT,
    operating_unit_id bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,

    entry_type        text        NOT NULL CHECK (entry_type IN ('EARNED','REVERSAL','ADJUSTMENT')),
    commission_type   text        NOT NULL,
    amount_minor      bigint      NOT NULL,        -- signed: a reversal is negative
    currency          char(3)     NOT NULL,

    shipment_id       bigint      REFERENCES shipments (id) ON DELETE RESTRICT,
    earned_on         date        NOT NULL,
    settlement_id     bigint,

    memo              text,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX commission_entries_franchise_idx
    ON commission_entries (organization_id, franchise_id, earned_on DESC, id DESC);
CREATE INDEX commission_entries_calculation_idx ON commission_entries (calculation_id);
CREATE INDEX commission_entries_settlement_idx
    ON commission_entries (settlement_id) WHERE settlement_id IS NOT NULL;

CREATE TRIGGER commission_entries_append_only
    BEFORE DELETE ON commission_entries
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- A default scheme per organization, so a fresh tenant can calculate
-- commission without configuring anything first.
-- ---------------------------------------------------------------------------
INSERT INTO commission_schemes (public_id, organization_id, code, name, description, currency, is_default, status)
SELECT gen_seed_public_id('csm'), o.id, 'STANDARD', 'Standard franchise scheme',
       'Default commission scheme created at provisioning.', o.currency, true, 'ACTIVE'
FROM organizations o
ON CONFLICT DO NOTHING;
