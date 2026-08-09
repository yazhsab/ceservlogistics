-- 0006 Courier products / services (M05).
--
-- Constitution §M05: product names must never be encoded in business logic.
-- Every behavioural difference between "Express Air" and "Surface Economy" is a
-- column here — mode, weight and dimension limits, volumetric divisor, COD and
-- insurance eligibility, SLA — so a new product is configuration, not a deploy.

CREATE TABLE courier_services (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id             text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'svc')),
    organization_id       bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code                  text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    name                  text        NOT NULL CHECK (length(btrim(name)) BETWEEN 2 AND 120),
    description           text        NOT NULL DEFAULT '',
    mode                  text        NOT NULL CHECK (mode IN ('AIR','SURFACE','RAIL','LOCAL')),

    -- Weight and dimension envelope. Grams and centimetres keep every limit an
    -- exact integer.
    min_weight_grams      integer     NOT NULL DEFAULT 1 CHECK (min_weight_grams > 0),
    max_weight_grams      integer     NOT NULL CHECK (max_weight_grams > 0),
    max_length_mm         integer     CHECK (max_length_mm IS NULL OR max_length_mm > 0),
    max_width_mm          integer     CHECK (max_width_mm IS NULL OR max_width_mm > 0),
    max_height_mm         integer     CHECK (max_height_mm IS NULL OR max_height_mm > 0),
    max_dimension_sum_mm  integer     CHECK (max_dimension_sum_mm IS NULL OR max_dimension_sum_mm > 0),
    -- Volumetric weight = (L_cm * W_cm * H_cm) / divisor, in kilograms.
    -- 5000 is the common air-cargo divisor, 4000/4750 are typical for surface.
    volumetric_divisor    integer     NOT NULL DEFAULT 5000 CHECK (volumetric_divisor > 0),
    -- Chargeable weight is rounded up to this granularity (e.g. 500 g slabs).
    weight_rounding_grams integer     NOT NULL DEFAULT 500 CHECK (weight_rounding_grams > 0),

    cod_allowed           boolean     NOT NULL DEFAULT true,
    max_cod_amount_minor  bigint      CHECK (max_cod_amount_minor IS NULL OR max_cod_amount_minor >= 0),
    insurance_allowed     boolean     NOT NULL DEFAULT true,
    max_declared_value_minor bigint   CHECK (max_declared_value_minor IS NULL OR max_declared_value_minor >= 0),

    -- Baseline SLA in hours; a matched route may tighten or extend it.
    sla_transit_hours     integer     NOT NULL CHECK (sla_transit_hours > 0),
    -- Additional SLA policy: cut-off time, excluded days, remote-area uplift.
    -- {"cutoffTime":"17:00","excludeSundays":true,"remoteAreaExtraHours":24}
    sla_rules             jsonb       NOT NULL DEFAULT '{}'::jsonb,
    cutoff_time           text        CHECK (cutoff_time IS NULL OR cutoff_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),

    status                text        NOT NULL DEFAULT 'ACTIVE'
                                      CHECK (status IN ('ACTIVE','INACTIVE')),
    effective_from        date        NOT NULL DEFAULT CURRENT_DATE,
    effective_to          date,
    sort_order            integer     NOT NULL DEFAULT 0,

    created_by            bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    version               integer     NOT NULL DEFAULT 1,

    CONSTRAINT courier_services_code_unique UNIQUE (organization_id, code),
    CONSTRAINT courier_services_weight_range CHECK (max_weight_grams >= min_weight_grams),
    CONSTRAINT courier_services_effective_range CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CONSTRAINT courier_services_cod_limit_requires_cod
        CHECK (max_cod_amount_minor IS NULL OR cod_allowed)
);

CREATE INDEX courier_services_org_status_idx
    ON courier_services (organization_id, status, sort_order, code);
CREATE INDEX courier_services_mode_idx ON courier_services (organization_id, mode) WHERE status = 'ACTIVE';

CREATE TRIGGER courier_services_bump_version BEFORE UPDATE ON courier_services
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();
