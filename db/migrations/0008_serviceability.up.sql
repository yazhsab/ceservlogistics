-- 0008 Serviceability and routing (M04).
--
-- Routing must be deterministic (Constitution §20). Every table here carries an
-- explicit `priority` and a stable tiebreaker so that two configurations which
-- both match an input can never produce different answers on different runs.
-- The documented precedence is:
--
--   routing_overrides  >  service-specific route  >  generic route  >  fallback
--
-- and within each level: higher priority, then later effective_from, then lower
-- id. Migration 0008 provides the storage; internal/serviceability implements
-- and tests the ordering.

-- ---------------------------------------------------------------------------
-- Service areas: which operating unit serves which PIN code
-- ---------------------------------------------------------------------------
CREATE TABLE service_areas (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'sva')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    operating_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE CASCADE,
    pincode_id        bigint      NOT NULL REFERENCES pincodes (id) ON DELETE CASCADE,
    area_type         text        NOT NULL CHECK (area_type IN ('PICKUP','DELIVERY','BOTH')),
    -- Higher priority wins when several units cover the same PIN code.
    priority          integer     NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    is_remote         boolean     NOT NULL DEFAULT false,
    -- Cut-off after which a booking is treated as next-day for this area.
    cutoff_time       text        CHECK (cutoff_time IS NULL OR cutoff_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    effective_from    timestamptz NOT NULL DEFAULT now(),
    effective_to      timestamptz,
    status            text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT service_areas_effective_range CHECK (effective_to IS NULL OR effective_to > effective_from)
);

-- A unit may not declare the same PIN code twice for the same purpose while
-- active; overlapping coverage must come from *different* units so that
-- priority resolves it.
CREATE UNIQUE INDEX service_areas_unique_active_idx
    ON service_areas (organization_id, operating_unit_id, pincode_id, area_type) WHERE status = 'ACTIVE';
-- The resolution query: pincode + area_type + org, ordered by priority.
CREATE INDEX service_areas_resolve_idx
    ON service_areas (organization_id, pincode_id, area_type, priority DESC, id)
    WHERE status = 'ACTIVE';
CREATE INDEX service_areas_unit_idx ON service_areas (operating_unit_id, area_type) WHERE status = 'ACTIVE';

CREATE TRIGGER service_areas_set_updated_at BEFORE UPDATE ON service_areas
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Serviceability rules: explicit allow/deny lanes
-- ---------------------------------------------------------------------------
CREATE TABLE serviceability_rules (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'svr')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name                text        NOT NULL,
    rule_type           text        NOT NULL CHECK (rule_type IN ('ALLOW','DENY')),
    -- NULL in a scope column means "any". A rule matches only when every
    -- non-NULL scope column matches the request.
    courier_service_id  bigint      REFERENCES courier_services (id) ON DELETE CASCADE,
    origin_zone_id      bigint      REFERENCES zones (id) ON DELETE CASCADE,
    destination_zone_id bigint      REFERENCES zones (id) ON DELETE CASCADE,
    origin_pincode_id   bigint      REFERENCES pincodes (id) ON DELETE CASCADE,
    destination_pincode_id bigint   REFERENCES pincodes (id) ON DELETE CASCADE,
    origin_state_id     bigint      REFERENCES states (id) ON DELETE CASCADE,
    destination_state_id bigint     REFERENCES states (id) ON DELETE CASCADE,
    priority            integer     NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    -- Structured restrictions surfaced to the caller, e.g.
    -- {"maxWeightGrams":25000,"codAllowed":false,"reason":"Air cargo embargo"}
    restrictions        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    reason_code         text,
    message             text,
    effective_from      timestamptz NOT NULL DEFAULT now(),
    effective_to        timestamptz,
    status              text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT serviceability_rules_range CHECK (effective_to IS NULL OR effective_to > effective_from),
    -- A rule with no scope at all would silently allow or deny the entire
    -- network; require at least one dimension.
    CONSTRAINT serviceability_rules_has_scope CHECK (
        courier_service_id IS NOT NULL OR origin_zone_id IS NOT NULL OR destination_zone_id IS NOT NULL
        OR origin_pincode_id IS NOT NULL OR destination_pincode_id IS NOT NULL
        OR origin_state_id IS NOT NULL OR destination_state_id IS NOT NULL)
);

CREATE INDEX serviceability_rules_resolve_idx
    ON serviceability_rules (organization_id, rule_type, priority DESC, id) WHERE status = 'ACTIVE';
CREATE INDEX serviceability_rules_origin_pin_idx
    ON serviceability_rules (origin_pincode_id) WHERE origin_pincode_id IS NOT NULL AND status = 'ACTIVE';
CREATE INDEX serviceability_rules_dest_pin_idx
    ON serviceability_rules (destination_pincode_id) WHERE destination_pincode_id IS NOT NULL AND status = 'ACTIVE';
CREATE INDEX serviceability_rules_service_idx
    ON serviceability_rules (courier_service_id) WHERE courier_service_id IS NOT NULL AND status = 'ACTIVE';

CREATE TRIGGER serviceability_rules_set_updated_at BEFORE UPDATE ON serviceability_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Route definitions and legs
-- ---------------------------------------------------------------------------
CREATE TABLE route_definitions (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rte')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code                text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,47}$'),
    name                text        NOT NULL,
    -- Routes connect facilities (usually hub to hub); branches attach via their
    -- parent hub.
    origin_unit_id      bigint      NOT NULL REFERENCES operating_units (id) ON DELETE CASCADE,
    destination_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE CASCADE,
    -- NULL means the route serves every product.
    courier_service_id  bigint      REFERENCES courier_services (id) ON DELETE CASCADE,
    priority            integer     NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    transit_hours       integer     NOT NULL CHECK (transit_hours > 0 AND transit_hours <= 8760),
    cutoff_time         text        CHECK (cutoff_time IS NULL OR cutoff_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    is_fallback         boolean     NOT NULL DEFAULT false,
    effective_from      timestamptz NOT NULL DEFAULT now(),
    effective_to        timestamptz,
    status              text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT route_definitions_code_unique UNIQUE (organization_id, code),
    CONSTRAINT route_definitions_endpoints_differ CHECK (origin_unit_id <> destination_unit_id),
    CONSTRAINT route_definitions_range CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE INDEX route_definitions_resolve_idx
    ON route_definitions (organization_id, origin_unit_id, destination_unit_id, priority DESC, id)
    WHERE status = 'ACTIVE';
CREATE INDEX route_definitions_service_idx
    ON route_definitions (courier_service_id) WHERE courier_service_id IS NOT NULL;
CREATE INDEX route_definitions_dest_idx ON route_definitions (destination_unit_id);

CREATE TRIGGER route_definitions_set_updated_at BEFORE UPDATE ON route_definitions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE route_legs (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rtl')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    route_definition_id bigint      NOT NULL REFERENCES route_definitions (id) ON DELETE CASCADE,
    sequence            integer     NOT NULL CHECK (sequence >= 1),
    from_unit_id        bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    to_unit_id          bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    mode                text        NOT NULL CHECK (mode IN ('ROAD','AIR','RAIL','PARTNER')),
    transit_hours       integer     NOT NULL CHECK (transit_hours > 0),
    partner_name        text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT route_legs_sequence_unique UNIQUE (route_definition_id, sequence),
    CONSTRAINT route_legs_endpoints_differ CHECK (from_unit_id <> to_unit_id)
);

CREATE INDEX route_legs_route_idx ON route_legs (route_definition_id, sequence);

-- ---------------------------------------------------------------------------
-- Routing overrides (highest precedence)
-- ---------------------------------------------------------------------------
CREATE TABLE routing_overrides (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id              text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rov')),
    organization_id        bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    origin_pincode_id      bigint      NOT NULL REFERENCES pincodes (id) ON DELETE CASCADE,
    destination_pincode_id bigint      NOT NULL REFERENCES pincodes (id) ON DELETE CASCADE,
    courier_service_id     bigint      REFERENCES courier_services (id) ON DELETE CASCADE,
    route_definition_id    bigint      NOT NULL REFERENCES route_definitions (id) ON DELETE RESTRICT,
    priority               integer     NOT NULL DEFAULT 500 CHECK (priority BETWEEN 0 AND 1000),
    -- Overrides are an operational exception and must be justified; the reason
    -- is copied into the routing explanation and the audit event.
    reason                 text        NOT NULL CHECK (length(btrim(reason)) >= 5),
    effective_from         timestamptz NOT NULL DEFAULT now(),
    effective_to           timestamptz,
    status                 text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_by             bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT routing_overrides_range CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE INDEX routing_overrides_resolve_idx
    ON routing_overrides (organization_id, origin_pincode_id, destination_pincode_id, priority DESC, id)
    WHERE status = 'ACTIVE';

CREATE TRIGGER routing_overrides_set_updated_at BEFORE UPDATE ON routing_overrides
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Temporary closures
-- ---------------------------------------------------------------------------
CREATE TABLE temporary_closures (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'tcl')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- Exactly one target: a facility or a PIN code.
    operating_unit_id bigint      REFERENCES operating_units (id) ON DELETE CASCADE,
    pincode_id        bigint      REFERENCES pincodes (id) ON DELETE CASCADE,
    closure_type      text        NOT NULL CHECK (closure_type IN ('FULL','PICKUP_ONLY','DELIVERY_ONLY')),
    reason_code       text        NOT NULL CHECK (reason_code IN
                                  ('WEATHER','STRIKE','HOLIDAY','LAW_AND_ORDER','INFRASTRUCTURE','OTHER')),
    reason            text        NOT NULL,
    starts_at         timestamptz NOT NULL,
    ends_at           timestamptz NOT NULL,
    status            text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','CANCELLED')),
    created_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT temporary_closures_window CHECK (ends_at > starts_at),
    CONSTRAINT temporary_closures_one_target CHECK (
        (operating_unit_id IS NOT NULL) <> (pincode_id IS NOT NULL))
);

CREATE INDEX temporary_closures_unit_idx
    ON temporary_closures (organization_id, operating_unit_id, starts_at, ends_at)
    WHERE status = 'ACTIVE' AND operating_unit_id IS NOT NULL;
CREATE INDEX temporary_closures_pincode_idx
    ON temporary_closures (organization_id, pincode_id, starts_at, ends_at)
    WHERE status = 'ACTIVE' AND pincode_id IS NOT NULL;

CREATE TRIGGER temporary_closures_set_updated_at BEFORE UPDATE ON temporary_closures
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
