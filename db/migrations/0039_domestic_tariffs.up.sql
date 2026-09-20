-- Versioned domestic weight slabs and city-level on-forwarding charges.
--
-- Commercial courier zones are not geography zones. A postcode continues to
-- resolve to its operational/geopolitical zone, while the rate card maps the
-- destination state or the selected delivery city to the carrier tariff zone.

CREATE TABLE domestic_state_rate_zones (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id             text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dsz')),
    organization_id       bigint      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    rate_card_version_id  bigint      NOT NULL REFERENCES rate_card_versions(id) ON DELETE CASCADE,
    origin_state_id       bigint      NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    destination_state_id  bigint      NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    rate_zone_code        text        NOT NULL CHECK (rate_zone_code ~ '^[A-Z0-9_-]{1,16}$'),
    created_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domestic_state_rate_zones_unique UNIQUE
        (rate_card_version_id, origin_state_id, destination_state_id)
);

CREATE TABLE domestic_weight_slabs (
    id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id                text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dws')),
    organization_id          bigint      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    rate_card_version_id     bigint      NOT NULL REFERENCES rate_card_versions(id) ON DELETE CASCADE,
    courier_service_id       bigint      NOT NULL REFERENCES courier_services(id) ON DELETE CASCADE,
    origin_state_id          bigint      NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    rate_zone_code           text        NOT NULL CHECK (rate_zone_code ~ '^[A-Z0-9_-]{1,16}$'),
    from_weight_grams        integer     NOT NULL CHECK (from_weight_grams >= 0),
    to_weight_grams          integer     CHECK (to_weight_grams IS NULL OR to_weight_grams > from_weight_grams),
    price_minor              bigint      NOT NULL CHECK (price_minor >= 0),
    additional_step_grams    integer     CHECK (additional_step_grams IS NULL OR additional_step_grams > 0),
    additional_price_minor   bigint      CHECK (additional_price_minor IS NULL OR additional_price_minor >= 0),
    source_sheet             text        NOT NULL DEFAULT '',
    source_cell              text        NOT NULL DEFAULT '',
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domestic_weight_slabs_unique UNIQUE
        (rate_card_version_id, courier_service_id, origin_state_id, rate_zone_code, from_weight_grams),
    CONSTRAINT domestic_weight_slabs_additional_pair CHECK
        ((additional_step_grams IS NULL) = (additional_price_minor IS NULL))
);

ALTER TABLE domestic_weight_slabs
    ADD CONSTRAINT domestic_weight_slabs_no_overlap EXCLUDE USING gist (
        rate_card_version_id WITH =,
        courier_service_id   WITH =,
        origin_state_id      WITH =,
        rate_zone_code       WITH =,
        int4range(from_weight_grams, to_weight_grams) WITH &&
    );

CREATE TABLE domestic_onforwarding_locations (
    id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id                text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'dof')),
    organization_id          bigint      NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    rate_card_version_id     bigint      NOT NULL REFERENCES rate_card_versions(id) ON DELETE CASCADE,
    destination_state_id     bigint      NOT NULL REFERENCES states(id) ON DELETE RESTRICT,
    city_name                text        NOT NULL,
    normalized_city_name     text        NOT NULL CHECK (normalized_city_name ~ '^[A-Z0-9]+$'),
    centre_area              text        NOT NULL,
    rate_zone_code           text        NOT NULL CHECK (rate_zone_code ~ '^[A-Z0-9_-]{1,16}$'),
    surcharge_code           text        NOT NULL,
    surcharge_type           text        NOT NULL CHECK (surcharge_type IN ('E','R')),
    surcharge_amount_minor   bigint      NOT NULL CHECK (surcharge_amount_minor >= 0),
    source_row_id            integer     NOT NULL,
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domestic_onforwarding_locations_unique UNIQUE
        (rate_card_version_id, destination_state_id, normalized_city_name)
);

CREATE INDEX domestic_state_rate_zones_lookup_idx ON domestic_state_rate_zones
    (rate_card_version_id, origin_state_id, destination_state_id);
CREATE INDEX domestic_weight_slabs_lookup_idx ON domestic_weight_slabs
    (rate_card_version_id, courier_service_id, origin_state_id, rate_zone_code, from_weight_grams);
CREATE INDEX domestic_onforwarding_lookup_idx ON domestic_onforwarding_locations
    (rate_card_version_id, destination_state_id, normalized_city_name);
CREATE INDEX domestic_onforwarding_search_idx ON domestic_onforwarding_locations
    USING gin (normalized_city_name gin_trgm_ops);

CREATE TRIGGER domestic_state_rate_zones_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON domestic_state_rate_zones
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();
CREATE TRIGGER domestic_weight_slabs_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON domestic_weight_slabs
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();
CREATE TRIGGER domestic_onforwarding_version_guard
    BEFORE INSERT OR UPDATE OR DELETE ON domestic_onforwarding_locations
    FOR EACH ROW EXECUTE FUNCTION rate_card_child_guard();
