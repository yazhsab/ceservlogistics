-- 0009 Pricing engine (M06).
--
-- Constitution §19: pricing is versioned, auditable, and historical shipments
-- must never change because a rate card was later edited.
--
-- The mechanism is rate_card_versions. A version is DRAFT and freely editable
-- until it is activated; from that moment the version row and every child row
-- (weight slabs, zone rates, surcharges, discounts) are immutable, enforced by
-- triggers below rather than by convention. Changing prices means creating a
-- new version with a later effective_from. Booking additionally copies the full
-- computed breakdown into shipment_charge_snapshots, so even deleting a rate
-- card cannot alter a past shipment's charges.
--
-- All percentages are integer basis points (1850 = 18.50%). All money is bigint
-- minor units with an explicit currency inherited from the rate card.

CREATE TABLE rate_cards (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rc')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code            text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,47}$'),
    name            text        NOT NULL,
    description     text        NOT NULL DEFAULT '',
    scope           text        NOT NULL CHECK (scope IN ('RETAIL','BUSINESS','FRANCHISE')),
    -- BUSINESS cards bind to one customer; FRANCHISE cards to one franchise.
    customer_id     bigint      REFERENCES customers (id) ON DELETE CASCADE,
    franchise_id    bigint      REFERENCES franchises (id) ON DELETE CASCADE,
    currency        char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    -- Exactly one RETAIL card may be the tenant default used when a customer
    -- has no card of its own.
    is_default      boolean     NOT NULL DEFAULT false,
    status          text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT rate_cards_code_unique UNIQUE (organization_id, code),
    CONSTRAINT rate_cards_scope_binding CHECK (
        (scope = 'RETAIL'    AND customer_id IS NULL     AND franchise_id IS NULL) OR
        (scope = 'BUSINESS'  AND customer_id IS NOT NULL AND franchise_id IS NULL) OR
        (scope = 'FRANCHISE' AND customer_id IS NULL     AND franchise_id IS NOT NULL)),
    CONSTRAINT rate_cards_default_is_retail CHECK (NOT is_default OR scope = 'RETAIL')
);

CREATE UNIQUE INDEX rate_cards_default_idx
    ON rate_cards (organization_id) WHERE is_default AND status = 'ACTIVE';
CREATE INDEX rate_cards_customer_idx ON rate_cards (customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX rate_cards_franchise_idx ON rate_cards (franchise_id) WHERE franchise_id IS NOT NULL;
CREATE INDEX rate_cards_org_scope_idx ON rate_cards (organization_id, scope, status);

CREATE TRIGGER rate_cards_set_updated_at BEFORE UPDATE ON rate_cards
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- business_accounts could not reference rate_cards in 0007.
ALTER TABLE business_accounts
    ADD COLUMN rate_card_id bigint REFERENCES rate_cards (id) ON DELETE SET NULL;
CREATE INDEX business_accounts_rate_card_idx ON business_accounts (rate_card_id)
    WHERE rate_card_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Rate card versions
-- ---------------------------------------------------------------------------
CREATE TABLE rate_card_versions (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rcv')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    rate_card_id    bigint      NOT NULL REFERENCES rate_cards (id) ON DELETE CASCADE,
    version         integer     NOT NULL CHECK (version >= 1),
    status          text        NOT NULL DEFAULT 'DRAFT'
                                CHECK (status IN ('DRAFT','ACTIVE','SUPERSEDED','ARCHIVED')),
    effective_from  timestamptz NOT NULL,
    effective_to    timestamptz,
    notes           text        NOT NULL DEFAULT '',
    activated_at    timestamptz,
    activated_by    bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT rate_card_versions_number_unique UNIQUE (rate_card_id, version),
    CONSTRAINT rate_card_versions_range CHECK (effective_to IS NULL OR effective_to > effective_from),
    CONSTRAINT rate_card_versions_activation_recorded
        CHECK (status = 'DRAFT' OR activated_at IS NOT NULL)
);

-- Only one ACTIVE version per card. Superseding is an explicit transition, so a
-- quote can never see two candidate versions.
CREATE UNIQUE INDEX rate_card_versions_active_idx
    ON rate_card_versions (rate_card_id) WHERE status = 'ACTIVE';
CREATE INDEX rate_card_versions_lookup_idx
    ON rate_card_versions (rate_card_id, effective_from DESC, id DESC);
CREATE INDEX rate_card_versions_effective_idx
    ON rate_card_versions (organization_id, status, effective_from);

CREATE TRIGGER rate_card_versions_set_updated_at BEFORE UPDATE ON rate_card_versions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- An activated version's pricing content is frozen. Only the lifecycle columns
-- may change afterwards (ACTIVE -> SUPERSEDED/ARCHIVED, closing effective_to).
CREATE OR REPLACE FUNCTION rate_card_version_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = 'DRAFT' THEN
        RETURN NEW;
    END IF;
    IF NEW.rate_card_id  IS DISTINCT FROM OLD.rate_card_id
       OR NEW.version    IS DISTINCT FROM OLD.version
       OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
       OR NEW.organization_id IS DISTINCT FROM OLD.organization_id THEN
        RAISE EXCEPTION 'rate card version % is activated and its pricing definition is immutable', OLD.id
            USING ERRCODE = 'P0001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER rate_card_versions_immutable
    BEFORE UPDATE ON rate_card_versions
    FOR EACH ROW EXECUTE FUNCTION rate_card_version_guard();

-- Guard used by every child pricing table: rows may not be inserted, changed or
-- removed once their parent version has left DRAFT.
CREATE OR REPLACE FUNCTION rate_card_child_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_id     bigint;
    v_status text;
BEGIN
    v_id := COALESCE(NEW.rate_card_version_id, OLD.rate_card_version_id);
    SELECT status INTO v_status FROM rate_card_versions WHERE id = v_id;
    IF v_status IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    IF v_status <> 'DRAFT' THEN
        RAISE EXCEPTION 'rate card version % is % and its pricing rows are immutable', v_id, v_status
            USING ERRCODE = 'P0001';
    END IF;
    RETURN COALESCE(NEW, OLD);
END;
$$;

-- ---------------------------------------------------------------------------
-- Zone rates: base + additional model
-- ---------------------------------------------------------------------------
CREATE TABLE zone_rates (
    id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id                text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'zrt')),
    organization_id          bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    rate_card_version_id     bigint      NOT NULL REFERENCES rate_card_versions (id) ON DELETE CASCADE,
    courier_service_id       bigint      NOT NULL REFERENCES courier_services (id) ON DELETE CASCADE,
    origin_zone_id           bigint      NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    destination_zone_id      bigint      NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    -- Price for everything up to base_weight_grams.
    base_weight_grams        integer     NOT NULL CHECK (base_weight_grams > 0),
    base_price_minor         bigint      NOT NULL CHECK (base_price_minor >= 0),
    -- Each additional step (or part thereof) beyond the base costs this much.
    additional_step_grams    integer     NOT NULL CHECK (additional_step_grams > 0),
    additional_price_minor   bigint      NOT NULL DEFAULT 0 CHECK (additional_price_minor >= 0),
    min_chargeable_weight_grams integer  NOT NULL DEFAULT 0 CHECK (min_chargeable_weight_grams >= 0),
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT zone_rates_unique UNIQUE (rate_card_version_id, courier_service_id, origin_zone_id, destination_zone_id)
);

CREATE INDEX zone_rates_resolve_idx
    ON zone_rates (rate_card_version_id, courier_service_id, origin_zone_id, destination_zone_id);

CREATE TRIGGER zone_rates_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON zone_rates
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();

-- ---------------------------------------------------------------------------
-- Weight slabs: explicit per-range prices that override the linear model
-- ---------------------------------------------------------------------------
CREATE TABLE weight_slabs (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id            text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'wsl')),
    organization_id      bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    rate_card_version_id bigint      NOT NULL REFERENCES rate_card_versions (id) ON DELETE CASCADE,
    courier_service_id   bigint      NOT NULL REFERENCES courier_services (id) ON DELETE CASCADE,
    origin_zone_id       bigint      NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    destination_zone_id  bigint      NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    -- Half-open interval [from_weight_grams, to_weight_grams). A NULL upper
    -- bound means "and above".
    from_weight_grams    integer     NOT NULL CHECK (from_weight_grams >= 0),
    to_weight_grams      integer     CHECK (to_weight_grams IS NULL OR to_weight_grams > 0),
    price_minor          bigint      NOT NULL CHECK (price_minor >= 0),
    -- Beyond to_weight_grams the linear zone-rate model resumes from this step.
    additional_step_grams  integer   CHECK (additional_step_grams IS NULL OR additional_step_grams > 0),
    additional_price_minor bigint    CHECK (additional_price_minor IS NULL OR additional_price_minor >= 0),
    sequence             integer     NOT NULL DEFAULT 1,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT weight_slabs_range CHECK (to_weight_grams IS NULL OR to_weight_grams > from_weight_grams),
    CONSTRAINT weight_slabs_sequence_unique UNIQUE (rate_card_version_id, courier_service_id, origin_zone_id, destination_zone_id, sequence)
);

-- Slabs for one lane must not overlap. An exclusion constraint enforces it in
-- the database, so an operator cannot create an ambiguous rate card and get a
-- non-deterministic price.
ALTER TABLE weight_slabs
    ADD CONSTRAINT weight_slabs_no_overlap EXCLUDE USING gist (
        rate_card_version_id WITH =,
        courier_service_id   WITH =,
        origin_zone_id       WITH =,
        destination_zone_id  WITH =,
        int4range(from_weight_grams, to_weight_grams) WITH &&
    );

CREATE INDEX weight_slabs_resolve_idx
    ON weight_slabs (rate_card_version_id, courier_service_id, origin_zone_id, destination_zone_id, from_weight_grams);

CREATE TRIGGER weight_slabs_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON weight_slabs
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();

-- ---------------------------------------------------------------------------
-- Surcharges
-- ---------------------------------------------------------------------------
CREATE TABLE surcharge_rules (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id            text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'sur')),
    organization_id      bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    rate_card_version_id bigint      NOT NULL REFERENCES rate_card_versions (id) ON DELETE CASCADE,
    code                 text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_]{1,31}$'),
    name                 text        NOT NULL,
    surcharge_type       text        NOT NULL CHECK (surcharge_type IN
                                     ('FUEL','REMOTE_AREA','COD','INSURANCE','HANDLING','OVERSIZE',
                                      'DOCUMENTATION','PACKAGING','APPOINTMENT','CUSTOM')),
    calc_type            text        NOT NULL CHECK (calc_type IN ('FIXED','PERCENTAGE','PER_KG')),
    -- Exactly one of value_minor / percentage_bp applies, per calc_type.
    value_minor          bigint      CHECK (value_minor IS NULL OR value_minor >= 0),
    percentage_bp        integer     CHECK (percentage_bp IS NULL OR percentage_bp BETWEEN 0 AND 1000000),
    -- What the percentage is applied to.
    applies_to           text        NOT NULL DEFAULT 'FREIGHT'
                                     CHECK (applies_to IN ('FREIGHT','FREIGHT_PLUS_SURCHARGES','DECLARED_VALUE','COD_AMOUNT')),
    min_amount_minor     bigint      CHECK (min_amount_minor IS NULL OR min_amount_minor >= 0),
    max_amount_minor     bigint      CHECK (max_amount_minor IS NULL OR max_amount_minor >= 0),
    courier_service_id   bigint      REFERENCES courier_services (id) ON DELETE CASCADE,
    -- Machine-readable applicability, e.g. {"remoteOrigin":true} or
    -- {"paymentModes":["COD"]}. Empty means "always".
    conditions           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Surcharges are applied in ascending priority so that a percentage rule
    -- based on FREIGHT_PLUS_SURCHARGES sees a deterministic running total.
    priority             integer     NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    is_taxable           boolean     NOT NULL DEFAULT true,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT surcharge_rules_code_unique UNIQUE (rate_card_version_id, code),
    CONSTRAINT surcharge_rules_value_matches_calc CHECK (
        (calc_type = 'PERCENTAGE' AND percentage_bp IS NOT NULL AND value_minor IS NULL) OR
        (calc_type IN ('FIXED','PER_KG') AND value_minor IS NOT NULL AND percentage_bp IS NULL)),
    CONSTRAINT surcharge_rules_bounds CHECK (
        min_amount_minor IS NULL OR max_amount_minor IS NULL OR max_amount_minor >= min_amount_minor)
);

CREATE INDEX surcharge_rules_version_idx ON surcharge_rules (rate_card_version_id, priority, id);
CREATE INDEX surcharge_rules_service_idx ON surcharge_rules (courier_service_id) WHERE courier_service_id IS NOT NULL;

CREATE TRIGGER surcharge_rules_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON surcharge_rules
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();

-- ---------------------------------------------------------------------------
-- Discounts
-- ---------------------------------------------------------------------------
CREATE TABLE discount_rules (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id            text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dsc')),
    organization_id      bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    rate_card_version_id bigint      NOT NULL REFERENCES rate_card_versions (id) ON DELETE CASCADE,
    code                 text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_]{1,31}$'),
    name                 text        NOT NULL,
    discount_type        text        NOT NULL CHECK (discount_type IN ('FIXED','PERCENTAGE')),
    value_minor          bigint      CHECK (value_minor IS NULL OR value_minor >= 0),
    percentage_bp        integer     CHECK (percentage_bp IS NULL OR percentage_bp BETWEEN 0 AND 10000),
    applies_to           text        NOT NULL DEFAULT 'FREIGHT'
                                     CHECK (applies_to IN ('FREIGHT','FREIGHT_PLUS_SURCHARGES')),
    courier_service_id   bigint      REFERENCES courier_services (id) ON DELETE CASCADE,
    min_subtotal_minor   bigint      NOT NULL DEFAULT 0 CHECK (min_subtotal_minor >= 0),
    max_discount_minor   bigint      CHECK (max_discount_minor IS NULL OR max_discount_minor >= 0),
    conditions           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    priority             integer     NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    -- When false, only the single highest-priority matching discount applies.
    is_stackable         boolean     NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT discount_rules_code_unique UNIQUE (rate_card_version_id, code),
    CONSTRAINT discount_rules_value_matches_type CHECK (
        (discount_type = 'PERCENTAGE' AND percentage_bp IS NOT NULL AND value_minor IS NULL) OR
        (discount_type = 'FIXED' AND value_minor IS NOT NULL AND percentage_bp IS NULL))
);

CREATE INDEX discount_rules_version_idx ON discount_rules (rate_card_version_id, priority, id);

CREATE TRIGGER discount_rules_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON discount_rules
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();

-- ---------------------------------------------------------------------------
-- Tax
-- ---------------------------------------------------------------------------
-- Tax rules are tenant-level rather than rate-card-level: GST rates are set by
-- statute, not by commercial agreement, and must apply uniformly across cards.
CREATE TABLE tax_rules (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'tax')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code            text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_]{1,31}$'),
    name            text        NOT NULL,
    tax_type        text        NOT NULL CHECK (tax_type IN ('CGST','SGST','IGST','UTGST','CESS','CUSTOM')),
    percentage_bp   integer     NOT NULL CHECK (percentage_bp BETWEEN 0 AND 100000),
    -- NULL applies to both; true only to intra-state; false only to inter-state.
    intra_state_only boolean,
    -- HSN/SAC code recorded on the invoice line.
    hsn_sac_code    text,
    priority        integer     NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    effective_from  timestamptz NOT NULL DEFAULT now(),
    effective_to    timestamptz,
    status          text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tax_rules_code_unique UNIQUE (organization_id, code),
    CONSTRAINT tax_rules_range CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE INDEX tax_rules_resolve_idx
    ON tax_rules (organization_id, status, effective_from, priority, id);

CREATE TRIGGER tax_rules_set_updated_at BEFORE UPDATE ON tax_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
