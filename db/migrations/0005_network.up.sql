-- 0005 Courier network master (M02).
--
-- Design decision (see docs/adr/0002-operating-units-model-hubs-and-branches.md):
-- Hub and Branch are not separate tables. They are operating_units discriminated
-- by unit_type, because they share every structural attribute (code, address,
-- hierarchy, contact, hours, capabilities, effective dates) and because
-- shipments, bags, manifests and trips all need to reference "a facility"
-- polymorphically. A single table keeps those foreign keys simple and keeps the
-- hierarchy query a single recursive CTE.

CREATE TABLE regions (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id        text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rgn')),
    organization_id  bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code             text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    name             text        NOT NULL,
    parent_region_id bigint      REFERENCES regions (id) ON DELETE RESTRICT,
    status           text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT regions_code_unique UNIQUE (organization_id, code),
    CONSTRAINT regions_not_self_parent CHECK (parent_region_id IS NULL OR parent_region_id <> id)
);

CREATE INDEX regions_org_status_idx ON regions (organization_id, status, code);
CREATE INDEX regions_parent_idx ON regions (parent_region_id) WHERE parent_region_id IS NOT NULL;
CREATE TRIGGER regions_set_updated_at BEFORE UPDATE ON regions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Operating units
-- ---------------------------------------------------------------------------
CREATE TABLE operating_units (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id        text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ou')),
    organization_id  bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code             text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    name             text        NOT NULL CHECK (length(btrim(name)) BETWEEN 2 AND 160),
    unit_type        text        NOT NULL CHECK (unit_type IN
                                 ('HEAD_OFFICE','REGIONAL_HUB','TRANSIT_HUB','DELIVERY_HUB',
                                  'COMPANY_BRANCH','FRANCHISE_BRANCH')),
    -- Reporting/routing hierarchy: a branch's parent is its hub, a hub's parent
    -- is its regional hub or head office.
    parent_unit_id   bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    region_id        bigint      REFERENCES regions (id) ON DELETE RESTRICT,
    status           text        NOT NULL DEFAULT 'ACTIVE'
                                 CHECK (status IN ('ACTIVE','INACTIVE','SUSPENDED')),

    address_line1    text        NOT NULL,
    address_line2    text,
    landmark         text,
    pincode          text        NOT NULL CHECK (pincode ~ '^[0-9A-Z][0-9A-Z -]{2,11}$'),
    pincode_id       bigint      REFERENCES pincodes (id) ON DELETE RESTRICT,
    city_id          bigint      REFERENCES cities (id) ON DELETE RESTRICT,
    state_id         bigint      REFERENCES states (id) ON DELETE RESTRICT,
    latitude         double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude        double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),

    contact_name     text,
    contact_phone    text,
    contact_email    text CHECK (contact_email IS NULL OR contact_email = lower(contact_email)),
    -- Weekly operating hours and cut-off times, e.g.
    -- {"mon":{"open":"09:30","close":"19:00","cutoff":"17:30"}, ...}
    operating_hours  jsonb       NOT NULL DEFAULT '{}'::jsonb,

    effective_from   date        NOT NULL DEFAULT CURRENT_DATE,
    effective_to     date,
    deactivated_at   timestamptz,
    deactivated_by   bigint      REFERENCES users (id) ON DELETE SET NULL,

    created_by       bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    version          integer     NOT NULL DEFAULT 1,

    CONSTRAINT operating_units_code_unique UNIQUE (organization_id, code),
    CONSTRAINT operating_units_not_self_parent CHECK (parent_unit_id IS NULL OR parent_unit_id <> id),
    CONSTRAINT operating_units_effective_range CHECK (effective_to IS NULL OR effective_to >= effective_from),
    -- Deactivation must be attributable. Reactivation leaves the historical
    -- deactivated_at in place, so the implication only runs one way.
    CONSTRAINT operating_units_deactivation_recorded
        CHECK (status <> 'INACTIVE' OR deactivated_at IS NOT NULL)
);

CREATE INDEX operating_units_org_type_status_idx ON operating_units (organization_id, unit_type, status, code);
CREATE INDEX operating_units_parent_idx ON operating_units (parent_unit_id) WHERE parent_unit_id IS NOT NULL;
CREATE INDEX operating_units_region_idx ON operating_units (region_id) WHERE region_id IS NOT NULL;
CREATE INDEX operating_units_pincode_idx ON operating_units (organization_id, pincode);
CREATE INDEX operating_units_name_search_idx ON operating_units (organization_id, lower(name));

CREATE TRIGGER operating_units_bump_version
    BEFORE UPDATE ON operating_units
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- user_roles.operating_unit_id could not be declared as a foreign key in 0002
-- because operating_units did not exist yet.
ALTER TABLE user_roles
    ADD CONSTRAINT user_roles_operating_unit_fk
    FOREIGN KEY (operating_unit_id) REFERENCES operating_units (id) ON DELETE CASCADE;

ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_operating_unit_fk
    FOREIGN KEY (operating_unit_id) REFERENCES operating_units (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Capabilities
-- ---------------------------------------------------------------------------
-- Capabilities are what a facility is allowed to do, independent of its type.
-- Routing and custody checks consult these rather than hard-coding "a
-- DELIVERY_HUB can bag", so an operator can enable pickup at a transit hub
-- without a code change.
CREATE TABLE operating_unit_capabilities (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    operating_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE CASCADE,
    capability        text        NOT NULL CHECK (capability IN
                                  ('BOOKING','PICKUP','DELIVERY','BAGGING','MANIFEST','LINEHAUL_ORIGIN',
                                   'LINEHAUL_DESTINATION','TRANSIT','COD_COLLECTION','RTO_PROCESSING',
                                   'CUSTOMER_WALKIN','WAREHOUSING')),
    enabled           boolean     NOT NULL DEFAULT true,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT operating_unit_capability_unique UNIQUE (operating_unit_id, capability)
);

CREATE INDEX ou_capabilities_lookup_idx
    ON operating_unit_capabilities (organization_id, capability, operating_unit_id) WHERE enabled;

CREATE TRIGGER ou_capabilities_set_updated_at BEFORE UPDATE ON operating_unit_capabilities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Franchises
-- ---------------------------------------------------------------------------
CREATE TABLE franchises (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'frn')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code              text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    name              text        NOT NULL,
    -- The FRANCHISE_BRANCH operating unit this franchise runs. One unit, one
    -- franchise: commission and settlement attribution depend on it.
    operating_unit_id bigint      NOT NULL UNIQUE REFERENCES operating_units (id) ON DELETE RESTRICT,
    category          text        NOT NULL DEFAULT 'STANDARD'
                                  CHECK (category IN ('PLATINUM','GOLD','SILVER','STANDARD')),
    owner_name        text        NOT NULL,
    owner_user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    owner_phone       text        NOT NULL,
    owner_email       text        CHECK (owner_email IS NULL OR owner_email = lower(owner_email)),
    gst_number        text,
    pan_number        text,
    status            text        NOT NULL DEFAULT 'ACTIVE'
                                  CHECK (status IN ('ONBOARDING','ACTIVE','SUSPENDED','TERMINATED')),
    onboarded_at      timestamptz,
    terminated_at     timestamptz,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,
    CONSTRAINT franchises_code_unique UNIQUE (organization_id, code)
);

CREATE INDEX franchises_org_status_idx ON franchises (organization_id, status, code);
CREATE INDEX franchises_owner_idx ON franchises (owner_user_id) WHERE owner_user_id IS NOT NULL;

CREATE TRIGGER franchises_bump_version BEFORE UPDATE ON franchises
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

CREATE TABLE franchise_agreements (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id            text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'agr')),
    organization_id      bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    franchise_id         bigint      NOT NULL REFERENCES franchises (id) ON DELETE CASCADE,
    agreement_number     text        NOT NULL,
    status               text        NOT NULL DEFAULT 'DRAFT'
                                     CHECK (status IN ('DRAFT','ACTIVE','EXPIRED','TERMINATED')),
    effective_from       date        NOT NULL,
    effective_to         date,
    currency             char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    security_deposit_minor bigint    NOT NULL DEFAULT 0 CHECK (security_deposit_minor >= 0),
    credit_limit_minor   bigint      NOT NULL DEFAULT 0 CHECK (credit_limit_minor >= 0),
    -- Commission plan is resolved by code in Release 3; stored here so the
    -- agreement remains the single source of contractual terms.
    commission_plan_code text,
    settlement_cycle     text        NOT NULL DEFAULT 'MONTHLY'
                                     CHECK (settlement_cycle IN ('WEEKLY','FORTNIGHTLY','MONTHLY')),
    terms                jsonb       NOT NULL DEFAULT '{}'::jsonb,
    signed_at            timestamptz,
    created_by           bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT franchise_agreements_number_unique UNIQUE (organization_id, agreement_number),
    CONSTRAINT franchise_agreements_range CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

-- At most one ACTIVE agreement per franchise at a time.
CREATE UNIQUE INDEX franchise_agreements_active_idx
    ON franchise_agreements (franchise_id) WHERE status = 'ACTIVE';
CREATE INDEX franchise_agreements_franchise_idx
    ON franchise_agreements (franchise_id, effective_from DESC);

CREATE TRIGGER franchise_agreements_set_updated_at BEFORE UPDATE ON franchise_agreements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
