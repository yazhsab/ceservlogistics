-- 0011 Shipment booking, AWB allocation and shipment events (M08).

-- ---------------------------------------------------------------------------
-- AWB sequences
-- ---------------------------------------------------------------------------
-- Constitution §11/§22: AWB allocation must be concurrency-safe across multiple
-- application instances and must not depend on a process-local lock.
--
-- Allocation is a single atomic statement:
--
--   INSERT INTO awb_sequences (...) VALUES (..., 1)
--   ON CONFLICT (organization_id, prefix, period_key)
--   DO UPDATE SET current_value = awb_sequences.current_value + 1, ...
--   RETURNING current_value;
--
-- PostgreSQL serialises the row update, so N concurrent bookers receive N
-- distinct values. shipments.awb additionally carries a UNIQUE constraint, so
-- even a defect in the allocator cannot produce a duplicate AWB.
CREATE TABLE awb_sequences (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    prefix          text        NOT NULL CHECK (prefix ~ '^[A-Z]{2,4}$'),
    -- Period over which the counter resets, e.g. '260808' for 2026-08-08.
    -- A single literal period ('ALL') gives a non-resetting sequence.
    period_key      text        NOT NULL CHECK (period_key ~ '^[A-Z0-9]{1,12}$'),
    current_value   bigint      NOT NULL DEFAULT 0 CHECK (current_value >= 0),
    -- Guards against silently wrapping past the fixed-width numeric segment.
    max_value       bigint      NOT NULL DEFAULT 999999 CHECK (max_value > 0),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT awb_sequences_unique UNIQUE (organization_id, prefix, period_key)
);

-- ---------------------------------------------------------------------------
-- Shipments
-- ---------------------------------------------------------------------------
CREATE TABLE shipments (
    id                    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id             text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'shp')),
    organization_id       bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    -- The human-readable business identifier. Globally unique so public
    -- tracking never needs a tenant hint, and because the tenant's AWB prefix
    -- already namespaces it.
    awb                   text        NOT NULL UNIQUE CHECK (awb ~ '^[A-Z]{2,4}[0-9]{6,}$'),
    -- Customer's own reference (PO number, order id). Unique per customer when
    -- present, which makes duplicate submissions detectable at the source.
    reference_number      text,

    customer_id           bigint      NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    courier_service_id    bigint      NOT NULL REFERENCES courier_services (id) ON DELETE RESTRICT,
    booked_by_user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    booking_unit_id       bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,

    payment_mode          text        NOT NULL CHECK (payment_mode IN ('PREPAID','COD','CREDIT','TO_PAY')),

    -- Denormalised current state (Constitution §13). shipment_events remains the
    -- append-only truth; this column exists so list and dashboard queries do
    -- not have to aggregate history.
    current_status        text        NOT NULL DEFAULT 'BOOKED' CHECK (current_status IN (
                            'BOOKED','PICKUP_SCHEDULED','PICKUP_ASSIGNED','PICKED_UP',
                            'ORIGIN_BRANCH_RECEIVED','ORIGIN_BAGGED','ORIGIN_DISPATCHED','IN_TRANSIT',
                            'TRANSIT_HUB_RECEIVED','TRANSIT_HUB_DISPATCHED','DESTINATION_HUB_RECEIVED',
                            'DESTINATION_BRANCH_RECEIVED','OUT_FOR_DELIVERY','DELIVERED','DELIVERY_FAILED',
                            'NDR','RTO_INITIATED','RTO_IN_TRANSIT','RTO_DELIVERED','CANCELLED','LOST','DAMAGED')),
    status_changed_at     timestamptz NOT NULL DEFAULT now(),
    -- Monotonic counter mirroring the highest shipment_events.sequence, used to
    -- allocate the next event sequence without scanning the event table.
    event_sequence        integer     NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),

    -- Resolved network path. Snapshotted in full in shipment_route_snapshots;
    -- these columns exist for operational filtering and joins.
    origin_branch_id      bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    origin_hub_id         bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    destination_hub_id    bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    destination_branch_id bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    route_definition_id   bigint      REFERENCES route_definitions (id) ON DELETE SET NULL,
    origin_pincode        text        NOT NULL,
    destination_pincode   text        NOT NULL,
    is_remote_origin      boolean     NOT NULL DEFAULT false,
    is_remote_destination boolean     NOT NULL DEFAULT false,

    -- Custody: which facility or agent physically holds the parcel right now.
    -- Release 2 drives this from scan events; Release 1 sets it at booking.
    current_custody_unit_id bigint    REFERENCES operating_units (id) ON DELETE RESTRICT,
    current_custody_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,

    piece_count           integer     NOT NULL CHECK (piece_count BETWEEN 1 AND 500),
    actual_weight_grams   integer     NOT NULL CHECK (actual_weight_grams > 0),
    volumetric_weight_grams integer   NOT NULL DEFAULT 0 CHECK (volumetric_weight_grams >= 0),
    chargeable_weight_grams integer   NOT NULL CHECK (chargeable_weight_grams > 0),

    currency              char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    declared_value_minor  bigint      NOT NULL DEFAULT 0 CHECK (declared_value_minor >= 0),
    cod_amount_minor      bigint      NOT NULL DEFAULT 0 CHECK (cod_amount_minor >= 0),
    insurance_required    boolean     NOT NULL DEFAULT false,
    total_amount_minor    bigint      NOT NULL CHECK (total_amount_minor >= 0),

    sla_hours             integer     CHECK (sla_hours IS NULL OR sla_hours > 0),
    promised_delivery_at  timestamptz,
    booked_at             timestamptz NOT NULL DEFAULT now(),

    content_description   text        NOT NULL,
    special_instructions  text,
    is_fragile            boolean     NOT NULL DEFAULT false,
    is_dangerous_goods    boolean     NOT NULL DEFAULT false,

    cancelled_at          timestamptz,
    cancelled_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    cancellation_reason   text,

    metadata              jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    version               integer     NOT NULL DEFAULT 1,

    CONSTRAINT shipments_cod_requires_cod_mode
        CHECK ((payment_mode = 'COD') = (cod_amount_minor > 0)),
    CONSTRAINT shipments_cancellation_recorded
        CHECK (current_status <> 'CANCELLED' OR (cancelled_at IS NOT NULL AND cancellation_reason IS NOT NULL)),
    -- Chargeable weight is the greater of actual and volumetric, rounded up to
    -- the product's slab granularity, so it can never be below either input.
    CONSTRAINT shipments_chargeable_covers_inputs
        CHECK (chargeable_weight_grams >= GREATEST(actual_weight_grams, volumetric_weight_grams))
);

-- Access patterns:
--  1. GET by public_id                     -> shipments_public_id_key
--  2. GET/track by AWB                     -> shipments_awb_key
--  3. Tenant list, newest first, filtered  -> shipments_org_created_idx
--  4. Tenant list filtered by status       -> shipments_org_status_idx
--  5. Customer portal list                 -> shipments_customer_idx
--  6. Branch/hub work queues               -> shipments_origin_branch_idx / destination_branch_idx
CREATE INDEX shipments_org_created_idx ON shipments (organization_id, created_at DESC, id DESC);
CREATE INDEX shipments_org_status_idx ON shipments (organization_id, current_status, created_at DESC, id DESC);
CREATE INDEX shipments_customer_idx ON shipments (customer_id, created_at DESC, id DESC);
CREATE INDEX shipments_service_idx ON shipments (organization_id, courier_service_id, created_at DESC);
CREATE INDEX shipments_origin_branch_idx ON shipments (origin_branch_id, current_status, created_at DESC)
    WHERE origin_branch_id IS NOT NULL;
CREATE INDEX shipments_destination_branch_idx ON shipments (destination_branch_id, current_status, created_at DESC)
    WHERE destination_branch_id IS NOT NULL;
CREATE INDEX shipments_booking_unit_idx ON shipments (booking_unit_id, created_at DESC)
    WHERE booking_unit_id IS NOT NULL;
CREATE INDEX shipments_reference_idx ON shipments (organization_id, reference_number)
    WHERE reference_number IS NOT NULL;
-- Search by AWB prefix in the operations console.
CREATE INDEX shipments_awb_prefix_idx ON shipments (awb text_pattern_ops);

-- A customer reference, when supplied, identifies exactly one shipment. This is
-- a second line of defence against duplicate bookings alongside Idempotency-Key.
CREATE UNIQUE INDEX shipments_customer_reference_idx
    ON shipments (customer_id, reference_number)
    WHERE reference_number IS NOT NULL AND current_status <> 'CANCELLED';

CREATE TRIGGER shipments_bump_version BEFORE UPDATE ON shipments
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- customer_credit_entries could not reference shipments in 0007.
ALTER TABLE customer_credit_entries
    ADD CONSTRAINT customer_credit_entries_shipment_fk
    FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE SET NULL;
CREATE INDEX customer_credit_entries_shipment_idx ON customer_credit_entries (shipment_id)
    WHERE shipment_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Packages
-- ---------------------------------------------------------------------------
CREATE TABLE shipment_packages (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id               text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pkg')),
    organization_id         bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id             bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    sequence                integer     NOT NULL CHECK (sequence >= 1),
    -- Per-piece barcode, scannable independently of the AWB.
    piece_barcode           text        NOT NULL UNIQUE,
    reference               text,
    -- Dimensions in integer millimetres: exact, and consistent with the
    -- grams-and-minor-units discipline used everywhere else.
    length_mm               integer     CHECK (length_mm IS NULL OR length_mm > 0),
    width_mm                integer     CHECK (width_mm IS NULL OR width_mm > 0),
    height_mm               integer     CHECK (height_mm IS NULL OR height_mm > 0),
    actual_weight_grams     integer     NOT NULL CHECK (actual_weight_grams > 0),
    volumetric_weight_grams integer     NOT NULL DEFAULT 0 CHECK (volumetric_weight_grams >= 0),
    chargeable_weight_grams integer     NOT NULL CHECK (chargeable_weight_grams > 0),
    content_description     text,
    declared_value_minor    bigint      NOT NULL DEFAULT 0 CHECK (declared_value_minor >= 0),
    created_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT shipment_packages_sequence_unique UNIQUE (shipment_id, sequence)
);

CREATE INDEX shipment_packages_shipment_idx ON shipment_packages (shipment_id, sequence);

-- ---------------------------------------------------------------------------
-- Address snapshots
-- ---------------------------------------------------------------------------
-- Booking copies sender and recipient details here. Editing a customer address
-- afterwards cannot rewrite where a parcel was sent, which matters for disputes,
-- POD and RTO.
CREATE TABLE shipment_address_snapshots (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id        bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    role               text        NOT NULL CHECK (role IN ('SENDER','RECIPIENT','RETURN')),
    -- The address record this was copied from, when one existed.
    source_address_id  bigint      REFERENCES customer_addresses (id) ON DELETE SET NULL,
    contact_name       text        NOT NULL,
    company_name       text,
    phone              text        NOT NULL,
    alt_phone          text,
    email              text,
    line1              text        NOT NULL,
    line2              text,
    landmark           text,
    city_name          text        NOT NULL,
    state_name         text        NOT NULL,
    pincode            text        NOT NULL,
    country_code       char(2)     NOT NULL DEFAULT 'IN',
    latitude           double precision,
    longitude          double precision,
    captured_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT shipment_address_role_unique UNIQUE (shipment_id, role)
);

CREATE TRIGGER shipment_address_snapshots_append_only
    BEFORE UPDATE OR DELETE ON shipment_address_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Charge snapshot
-- ---------------------------------------------------------------------------
CREATE TABLE shipment_charge_snapshots (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id         bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id             bigint      NOT NULL UNIQUE REFERENCES shipments (id) ON DELETE CASCADE,

    -- Provenance: which card, which version, which engine produced this price.
    rate_card_id            bigint      REFERENCES rate_cards (id) ON DELETE SET NULL,
    rate_card_version_id    bigint      REFERENCES rate_card_versions (id) ON DELETE SET NULL,
    rate_card_code          text        NOT NULL,
    rate_card_version_no    integer     NOT NULL,
    pricing_engine_version  text        NOT NULL,

    currency                char(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    origin_zone_code        text,
    destination_zone_code   text,
    chargeable_weight_grams integer     NOT NULL,

    freight_minor           bigint      NOT NULL CHECK (freight_minor >= 0),
    surcharge_total_minor   bigint      NOT NULL DEFAULT 0 CHECK (surcharge_total_minor >= 0),
    discount_total_minor    bigint      NOT NULL DEFAULT 0 CHECK (discount_total_minor >= 0),
    taxable_minor           bigint      NOT NULL CHECK (taxable_minor >= 0),
    tax_total_minor         bigint      NOT NULL DEFAULT 0 CHECK (tax_total_minor >= 0),
    rounding_minor          bigint      NOT NULL DEFAULT 0,
    total_minor             bigint      NOT NULL CHECK (total_minor >= 0),

    -- Line-by-line breakdown exactly as returned by POST /pricing/quote, so an
    -- invoice or a dispute can be reconstructed years later without re-running
    -- the engine.
    line_items              jsonb       NOT NULL,
    -- Inputs the engine consumed (weights, flags, resolved zones, matched rule
    -- ids). Together with line_items this makes the price fully reproducible.
    calculation_inputs      jsonb       NOT NULL,
    calculated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX shipment_charge_snapshots_version_idx ON shipment_charge_snapshots (rate_card_version_id)
    WHERE rate_card_version_id IS NOT NULL;

CREATE TRIGGER shipment_charge_snapshots_append_only
    BEFORE UPDATE OR DELETE ON shipment_charge_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Route snapshot
-- ---------------------------------------------------------------------------
CREATE TABLE shipment_route_snapshots (
    id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id          bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id              bigint      NOT NULL UNIQUE REFERENCES shipments (id) ON DELETE CASCADE,
    route_definition_id      bigint      REFERENCES route_definitions (id) ON DELETE SET NULL,
    route_code               text,
    resolution_source        text        NOT NULL CHECK (resolution_source IN
                                         ('OVERRIDE','SERVICE_ROUTE','GENERIC_ROUTE','FALLBACK_ROUTE','LOCAL_DELIVERY')),
    origin_branch_code       text,
    origin_hub_code          text,
    destination_hub_code     text,
    destination_branch_code  text,
    -- Ordered legs: [{"sequence":1,"from":"BLR-HUB","to":"DEL-HUB","mode":"AIR","transitHours":8}]
    legs                     jsonb       NOT NULL DEFAULT '[]'::jsonb,
    transit_hours            integer     NOT NULL,
    sla_hours                integer     NOT NULL,
    promised_delivery_at     timestamptz,
    -- The full decision trace: every candidate considered and why it won or
    -- lost. This is what makes a routing decision defensible months later.
    explanation              jsonb       NOT NULL DEFAULT '{}'::jsonb,
    explanation_id           text,
    resolved_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER shipment_route_snapshots_append_only
    BEFORE UPDATE OR DELETE ON shipment_route_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Shipment events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE shipment_events (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'evt')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    -- Per-shipment monotonic sequence. UNIQUE(shipment_id, sequence) makes a
    -- duplicated scan a constraint violation rather than a duplicated history
    -- row, which is the concurrency control for event append.
    sequence          integer     NOT NULL CHECK (sequence >= 1),
    event_type        text        NOT NULL CHECK (event_type IN
                      ('BOOKED','STATUS_CHANGED','CANCELLED','EXCEPTION','REMARK','LOCATION_UPDATE','SYSTEM')),
    from_status       text,
    to_status         text        NOT NULL,
    occurred_at       timestamptz NOT NULL DEFAULT now(),
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    actor_user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    actor_type        text        NOT NULL DEFAULT 'USER'
                                  CHECK (actor_type IN ('USER','SYSTEM','CUSTOMER','PARTNER')),
    operating_unit_id bigint      REFERENCES operating_units (id) ON DELETE SET NULL,
    location_pincode  text,
    location_name     text,
    -- Customer-facing description; operations may add an internal remark.
    description       text        NOT NULL,
    internal_remarks  text,
    reason_code       text,
    request_id        text,
    -- Idempotency at event level: a retried scan carrying the same key is
    -- rejected by the partial unique index below rather than duplicated.
    idempotency_key   text,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT shipment_events_sequence_unique UNIQUE (shipment_id, sequence)
);

CREATE INDEX shipment_events_shipment_idx ON shipment_events (shipment_id, sequence DESC);
CREATE INDEX shipment_events_org_time_idx ON shipment_events (organization_id, occurred_at DESC, id DESC);
CREATE INDEX shipment_events_status_idx ON shipment_events (organization_id, to_status, occurred_at DESC);
CREATE UNIQUE INDEX shipment_events_idempotency_idx
    ON shipment_events (shipment_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TRIGGER shipment_events_append_only
    BEFORE UPDATE OR DELETE ON shipment_events
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
