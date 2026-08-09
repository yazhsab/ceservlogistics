-- 0004 Geography and zones (M03).
--
-- Design decision (see docs/adr/0003-geography-is-global-reference-data.md):
-- countries, states, districts, cities, pincodes and localities are GLOBAL
-- reference data with no organization_id. Every courier tenant on the platform
-- delivers to the same India Post PIN codes; duplicating ~19,300 rows per
-- tenant would waste cache, complicate imports and give each tenant a private,
-- divergent copy of objective facts.
--
-- Tenant-specific overlays live in pincode_zone_mappings: which pricing zone a
-- PIN code falls into, and whether the tenant treats it as a remote area.

CREATE TABLE countries (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id   text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cnt')),
    iso2        char(2)     NOT NULL UNIQUE CHECK (iso2 ~ '^[A-Z]{2}$'),
    iso3        char(3)     NOT NULL UNIQUE CHECK (iso3 ~ '^[A-Z]{3}$'),
    name        text        NOT NULL,
    phone_code  text        NOT NULL,
    currency    char(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status      text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER countries_set_updated_at BEFORE UPDATE ON countries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE states (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id      text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'stt')),
    country_id     bigint      NOT NULL REFERENCES countries (id) ON DELETE RESTRICT,
    code           text        NOT NULL CHECK (code = upper(code)),
    name           text        NOT NULL,
    -- Two-digit GST state code; required for CGST/SGST vs IGST determination.
    gst_state_code text        CHECK (gst_state_code IS NULL OR gst_state_code ~ '^[0-9]{2}$'),
    status         text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT states_code_unique UNIQUE (country_id, code)
);

CREATE INDEX states_country_idx ON states (country_id, name);
CREATE TRIGGER states_set_updated_at BEFORE UPDATE ON states
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE districts (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id  text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dst')),
    state_id   bigint      NOT NULL REFERENCES states (id) ON DELETE RESTRICT,
    code       text        NOT NULL CHECK (code = upper(code)),
    name       text        NOT NULL,
    status     text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT districts_code_unique UNIQUE (state_id, code)
);

CREATE INDEX districts_state_idx ON districts (state_id, name);
CREATE TRIGGER districts_set_updated_at BEFORE UPDATE ON districts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE cities (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id   text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cty')),
    state_id    bigint      NOT NULL REFERENCES states (id) ON DELETE RESTRICT,
    district_id bigint      REFERENCES districts (id) ON DELETE SET NULL,
    name        text        NOT NULL,
    -- Metro/tier classification drives default zone assignment and surcharges.
    tier        text        NOT NULL DEFAULT 'OTHER'
                            CHECK (tier IN ('METRO','TIER_1','TIER_2','TIER_3','OTHER')),
    latitude    double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude   double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    status      text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT cities_name_unique UNIQUE (state_id, name)
);

CREATE INDEX cities_district_idx ON cities (district_id) WHERE district_id IS NOT NULL;
CREATE INDEX cities_name_idx ON cities (lower(name));
CREATE TRIGGER cities_set_updated_at BEFORE UPDATE ON cities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- PIN codes
-- ---------------------------------------------------------------------------
CREATE TABLE pincodes (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id   text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pin')),
    country_id  bigint      NOT NULL REFERENCES countries (id) ON DELETE RESTRICT,
    code        text        NOT NULL CHECK (code ~ '^[0-9A-Z][0-9A-Z -]{2,11}$'),
    state_id    bigint      NOT NULL REFERENCES states (id) ON DELETE RESTRICT,
    district_id bigint      REFERENCES districts (id) ON DELETE SET NULL,
    city_id     bigint      REFERENCES cities (id) ON DELETE SET NULL,
    -- Region name as published by the postal authority; kept for display when
    -- the city mapping is coarse.
    office_name text,
    latitude    double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude   double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    -- Platform-wide remote/ODA default. A tenant may override it per PIN code
    -- in pincode_zone_mappings.
    is_remote   boolean     NOT NULL DEFAULT false,
    status      text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    metadata    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pincodes_code_unique UNIQUE (country_id, code)
);

-- The hottest read in the platform: exact PIN lookup during booking and
-- serviceability. pincodes_code_unique already serves it; this covering index
-- lets the common "resolve a PIN to its state/city" read be index-only.
CREATE INDEX pincodes_lookup_idx ON pincodes (code) INCLUDE (id, state_id, city_id, district_id, is_remote, status);
CREATE INDEX pincodes_state_idx ON pincodes (state_id, code);
CREATE INDEX pincodes_city_idx ON pincodes (city_id, code) WHERE city_id IS NOT NULL;
CREATE INDEX pincodes_prefix_idx ON pincodes (code text_pattern_ops);

CREATE TRIGGER pincodes_set_updated_at BEFORE UPDATE ON pincodes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE localities (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id  text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'loc')),
    pincode_id bigint      NOT NULL REFERENCES pincodes (id) ON DELETE CASCADE,
    name       text        NOT NULL,
    status     text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT localities_name_unique UNIQUE (pincode_id, name)
);

CREATE INDEX localities_name_idx ON localities (lower(name));
CREATE TRIGGER localities_set_updated_at BEFORE UPDATE ON localities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Zones (tenant-scoped)
-- ---------------------------------------------------------------------------
CREATE TABLE zones (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'zn')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code            text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{0,31}$'),
    name            text        NOT NULL,
    zone_type       text        NOT NULL DEFAULT 'REGIONAL'
                                CHECK (zone_type IN ('LOCAL','INTRA_STATE','METRO','REGIONAL','NATIONAL','SPECIAL','INTERNATIONAL')),
    description     text        NOT NULL DEFAULT '',
    sort_order      integer     NOT NULL DEFAULT 0,
    status          text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT zones_code_unique UNIQUE (organization_id, code)
);

CREATE INDEX zones_org_status_idx ON zones (organization_id, status, sort_order);
CREATE TRIGGER zones_set_updated_at BEFORE UPDATE ON zones
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- PIN code -> zone mapping (tenant overlay)
-- ---------------------------------------------------------------------------
CREATE TABLE pincode_zone_mappings (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'zmp')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    pincode_id        bigint      NOT NULL REFERENCES pincodes (id) ON DELETE CASCADE,
    zone_id           bigint      NOT NULL REFERENCES zones (id) ON DELETE RESTRICT,
    -- NULL means "inherit the platform default from pincodes.is_remote".
    is_remote_override boolean,
    effective_from    timestamptz NOT NULL DEFAULT now(),
    effective_to      timestamptz,
    status            text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pincode_zone_effective_range CHECK (effective_to IS NULL OR effective_to > effective_from)
);

-- One active mapping per (tenant, PIN code). Historical rows remain for audit
-- but only one may be ACTIVE, which makes zone resolution deterministic.
CREATE UNIQUE INDEX pincode_zone_active_idx
    ON pincode_zone_mappings (organization_id, pincode_id) WHERE status = 'ACTIVE';
CREATE INDEX pincode_zone_lookup_idx
    ON pincode_zone_mappings (organization_id, pincode_id, effective_from DESC);
CREATE INDEX pincode_zone_zone_idx ON pincode_zone_mappings (zone_id);

CREATE TRIGGER pincode_zone_mappings_set_updated_at BEFORE UPDATE ON pincode_zone_mappings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
