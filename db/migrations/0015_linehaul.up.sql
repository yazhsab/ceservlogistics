-- 0015 Line haul (M13): carriers, vehicles, drivers, trips and legs.
--
-- A trip models the actual physical movement between facilities. Manifests ride
-- on trips; shipments move because a trip departed and arrived, never because
-- somebody edited a status.
--
-- Multi-leg trips are first-class: BLR -> HYD -> DEL is one trip with two legs,
-- and a shipment manifested only for the first leg must not be carried to the
-- second. That is why manifests attach to legs, not just to trips.

-- ---------------------------------------------------------------------------
-- Carriers
-- ---------------------------------------------------------------------------
CREATE TABLE carriers (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'car')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    code            text        NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,24}$'),
    name            text        NOT NULL,
    carrier_type    text        NOT NULL CHECK (carrier_type IN ('OWN','CONTRACTED','PARTNER','COURIER_PARTNER')),
    -- Transport modes this carrier can serve; a trip's mode must be one of them.
    modes           text[]      NOT NULL DEFAULT ARRAY['ROAD']::text[]
                                CHECK (modes <@ ARRAY['ROAD','AIR','RAIL','PARTNER']::text[] AND array_length(modes, 1) >= 1),
    contact_name    text,
    contact_phone   text,
    contact_email   text,
    gst_number      text,
    is_active       boolean     NOT NULL DEFAULT true,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT carriers_code_unique UNIQUE (organization_id, code)
);

CREATE INDEX carriers_org_active_idx ON carriers (organization_id, is_active, name);
CREATE TRIGGER carriers_touch BEFORE UPDATE ON carriers FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Vehicles
-- ---------------------------------------------------------------------------
CREATE TABLE vehicles (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'veh')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    carrier_id        bigint      REFERENCES carriers (id) ON DELETE RESTRICT,
    registration_number text      NOT NULL CHECK (length(registration_number) BETWEEN 4 AND 24),
    vehicle_type      text        NOT NULL CHECK (vehicle_type IN
                      ('BIKE','VAN','TEMPO','TRUCK','CONTAINER','TRAILER','OTHER')),
    capacity_weight_grams bigint  CHECK (capacity_weight_grams IS NULL OR capacity_weight_grams > 0),
    capacity_volume_cc bigint     CHECK (capacity_volume_cc IS NULL OR capacity_volume_cc > 0),
    -- Home facility, used to default the origin when planning trips.
    base_unit_id      bigint      REFERENCES operating_units (id) ON DELETE SET NULL,
    insurance_expiry  date,
    fitness_expiry    date,
    permit_expiry     date,
    is_active         boolean     NOT NULL DEFAULT true,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT vehicles_registration_unique UNIQUE (organization_id, registration_number)
);

CREATE INDEX vehicles_org_active_idx ON vehicles (organization_id, is_active, registration_number);
CREATE INDEX vehicles_carrier_idx ON vehicles (carrier_id) WHERE carrier_id IS NOT NULL;
CREATE TRIGGER vehicles_touch BEFORE UPDATE ON vehicles FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Drivers
-- ---------------------------------------------------------------------------
-- A driver may or may not be a system user: contracted carriers supply drivers
-- who never log in. user_id is therefore optional, and the licence details are
-- held here rather than on the user record.
CREATE TABLE drivers (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'drv')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    carrier_id        bigint      REFERENCES carriers (id) ON DELETE RESTRICT,
    user_id           bigint      REFERENCES users (id) ON DELETE SET NULL,
    code              text        NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,24}$'),
    full_name         text        NOT NULL,
    phone             text        NOT NULL,
    licence_number    text,
    licence_expiry    date,
    base_unit_id      bigint      REFERENCES operating_units (id) ON DELETE SET NULL,
    is_active         boolean     NOT NULL DEFAULT true,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT drivers_code_unique UNIQUE (organization_id, code)
);

CREATE INDEX drivers_org_active_idx ON drivers (organization_id, is_active, full_name);
CREATE INDEX drivers_user_idx ON drivers (user_id) WHERE user_id IS NOT NULL;
CREATE TRIGGER drivers_touch BEFORE UPDATE ON drivers FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Trips
-- ---------------------------------------------------------------------------
CREATE TABLE trips (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'trp')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    trip_code         text        NOT NULL,

    mode              text        NOT NULL CHECK (mode IN ('ROAD','AIR','RAIL','PARTNER')),
    status            text        NOT NULL DEFAULT 'PLANNED' CHECK (status IN
                      ('PLANNED','LOADING','DEPARTED','IN_TRANSIT','ARRIVED','CLOSED','CANCELLED')),

    origin_unit_id    bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    -- Final destination. Intermediate stops are legs.
    destination_unit_id bigint    NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    route_definition_id bigint    REFERENCES route_definitions (id) ON DELETE SET NULL,
    direction         text        NOT NULL DEFAULT 'FORWARD'
                                  CHECK (direction IN ('FORWARD','REVERSE')),

    carrier_id        bigint      REFERENCES carriers (id) ON DELETE RESTRICT,
    vehicle_id        bigint      REFERENCES vehicles (id) ON DELETE RESTRICT,
    primary_driver_id bigint      REFERENCES drivers (id) ON DELETE RESTRICT,
    -- For AIR/RAIL/PARTNER: the flight number, train number or partner AWB.
    external_reference text,

    scheduled_departure timestamptz NOT NULL,
    scheduled_arrival timestamptz NOT NULL,
    actual_departure  timestamptz,
    actual_arrival    timestamptz,
    closed_at         timestamptz,
    cancelled_at      timestamptz,
    cancellation_reason text,

    current_leg_sequence integer  CHECK (current_leg_sequence IS NULL OR current_leg_sequence >= 1),
    leg_count         integer     NOT NULL DEFAULT 1 CHECK (leg_count >= 1),

    manifest_count    integer     NOT NULL DEFAULT 0 CHECK (manifest_count >= 0),
    bag_count         integer     NOT NULL DEFAULT 0 CHECK (bag_count >= 0),
    shipment_count    integer     NOT NULL DEFAULT 0 CHECK (shipment_count >= 0),
    total_weight_grams bigint     NOT NULL DEFAULT 0 CHECK (total_weight_grams >= 0),

    -- Odometer readings for own fleet cost attribution in a later release.
    departure_odometer_km integer CHECK (departure_odometer_km IS NULL OR departure_odometer_km >= 0),
    arrival_odometer_km integer   CHECK (arrival_odometer_km IS NULL OR arrival_odometer_km >= 0),
    seal_number       text,

    event_sequence    integer     NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    created_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT trips_code_unique UNIQUE (organization_id, trip_code),
    CONSTRAINT trips_distinct_endpoints CHECK (origin_unit_id <> destination_unit_id),
    CONSTRAINT trips_schedule_ordered CHECK (scheduled_arrival > scheduled_departure),
    CONSTRAINT trips_departure_recorded
        CHECK (status NOT IN ('DEPARTED','IN_TRANSIT','ARRIVED','CLOSED') OR actual_departure IS NOT NULL),
    CONSTRAINT trips_arrival_recorded
        CHECK (status NOT IN ('ARRIVED','CLOSED') OR actual_arrival IS NOT NULL),
    CONSTRAINT trips_odometer_ordered
        CHECK (arrival_odometer_km IS NULL OR departure_odometer_km IS NULL
               OR arrival_odometer_km >= departure_odometer_km),
    CONSTRAINT trips_cancellation_recorded
        CHECK (status <> 'CANCELLED' OR (cancelled_at IS NOT NULL AND cancellation_reason IS NOT NULL))
);

CREATE INDEX trips_origin_status_idx ON trips (origin_unit_id, status, scheduled_departure DESC);
CREATE INDEX trips_destination_status_idx ON trips (destination_unit_id, status, scheduled_arrival);
CREATE INDEX trips_org_created_idx ON trips (organization_id, created_at DESC, id DESC);
CREATE INDEX trips_vehicle_idx ON trips (vehicle_id, scheduled_departure DESC) WHERE vehicle_id IS NOT NULL;
CREATE INDEX trips_driver_idx ON trips (primary_driver_id, scheduled_departure DESC) WHERE primary_driver_id IS NOT NULL;
-- Inbound board at a facility: trips heading here that have left.
CREATE INDEX trips_inbound_idx ON trips (destination_unit_id, scheduled_arrival)
    WHERE status IN ('DEPARTED','IN_TRANSIT');

CREATE TRIGGER trips_touch BEFORE UPDATE ON trips FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trips_bump_version BEFORE UPDATE ON trips FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- A vehicle cannot be on two live trips at once. Partial unique index rather
-- than a service check, because two dispatchers can plan simultaneously.
CREATE UNIQUE INDEX trips_vehicle_active_idx ON trips (vehicle_id)
    WHERE vehicle_id IS NOT NULL AND status IN ('LOADING','DEPARTED','IN_TRANSIT');
CREATE UNIQUE INDEX trips_driver_active_idx ON trips (primary_driver_id)
    WHERE primary_driver_id IS NOT NULL AND status IN ('LOADING','DEPARTED','IN_TRANSIT');

-- Deferred foreign keys from 0014.
ALTER TABLE manifests ADD CONSTRAINT manifests_trip_fk
    FOREIGN KEY (trip_id) REFERENCES trips (id) ON DELETE SET NULL;
ALTER TABLE shipments ADD CONSTRAINT shipments_current_trip_fk
    FOREIGN KEY (current_trip_id) REFERENCES trips (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Trip legs
-- ---------------------------------------------------------------------------
CREATE TABLE trip_legs (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'tlg')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    trip_id           bigint      NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    sequence          integer     NOT NULL CHECK (sequence >= 1),

    origin_unit_id    bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    destination_unit_id bigint    NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    status            text        NOT NULL DEFAULT 'PLANNED' CHECK (status IN
                      ('PLANNED','LOADING','DEPARTED','ARRIVED','SKIPPED','CANCELLED')),

    scheduled_departure timestamptz NOT NULL,
    scheduled_arrival timestamptz NOT NULL,
    actual_departure  timestamptz,
    actual_arrival    timestamptz,
    distance_km       integer     CHECK (distance_km IS NULL OR distance_km >= 0),
    remarks           text,

    CONSTRAINT trip_legs_sequence_unique UNIQUE (trip_id, sequence),
    CONSTRAINT trip_legs_distinct_endpoints CHECK (origin_unit_id <> destination_unit_id),
    CONSTRAINT trip_legs_schedule_ordered CHECK (scheduled_arrival > scheduled_departure)
);

CREATE INDEX trip_legs_trip_idx ON trip_legs (trip_id, sequence);
CREATE INDEX trip_legs_destination_idx ON trip_legs (destination_unit_id, status, scheduled_arrival);

-- A manifest rides on a specific leg, so a shipment for an intermediate stop is
-- unloaded there rather than travelling on.
ALTER TABLE manifests ADD COLUMN trip_leg_id bigint REFERENCES trip_legs (id) ON DELETE SET NULL;
CREATE INDEX manifests_trip_leg_idx ON manifests (trip_leg_id) WHERE trip_leg_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Trip assignments
-- ---------------------------------------------------------------------------
-- Crew beyond the primary driver: co-drivers, loaders, escorts for secure
-- cargo. Departure validation requires at least the roles a policy demands.
CREATE TABLE trip_assignments (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'tas')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    trip_id         bigint      NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    trip_leg_id     bigint      REFERENCES trip_legs (id) ON DELETE CASCADE,
    role            text        NOT NULL CHECK (role IN ('DRIVER','CO_DRIVER','LOADER','ESCORT','SUPERVISOR')),
    driver_id       bigint      REFERENCES drivers (id) ON DELETE RESTRICT,
    user_id         bigint      REFERENCES users (id) ON DELETE RESTRICT,
    status          text        NOT NULL DEFAULT 'ASSIGNED'
                                CHECK (status IN ('ASSIGNED','ACCEPTED','REJECTED','RELEASED')),
    assigned_at     timestamptz NOT NULL DEFAULT now(),
    assigned_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    released_at     timestamptz,
    remarks         text,
    -- One of driver_id or user_id must identify the person.
    CONSTRAINT trip_assignments_person CHECK (driver_id IS NOT NULL OR user_id IS NOT NULL)
);

CREATE INDEX trip_assignments_trip_idx ON trip_assignments (trip_id, role);
CREATE UNIQUE INDEX trip_assignments_active_driver_idx
    ON trip_assignments (trip_id, COALESCE(trip_leg_id, 0), driver_id)
    WHERE driver_id IS NOT NULL AND status IN ('ASSIGNED','ACCEPTED');

-- ---------------------------------------------------------------------------
-- Trip events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE trip_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'tev')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    trip_id         bigint      NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    trip_leg_id     bigint      REFERENCES trip_legs (id) ON DELETE SET NULL,
    sequence        integer     NOT NULL CHECK (sequence >= 1),
    event_type      text        NOT NULL CHECK (event_type IN
                    ('CREATED','MANIFEST_ATTACHED','MANIFEST_DETACHED','LOADING_STARTED','DEPARTED',
                     'LOCATION_UPDATE','LEG_ARRIVED','LEG_DEPARTED','ARRIVED','CLOSED','CANCELLED','EXCEPTION')),
    from_status     text,
    to_status       text,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    actor_user_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    operating_unit_id bigint    REFERENCES operating_units (id) ON DELETE SET NULL,
    latitude        double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude       double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    description     text        NOT NULL,
    reason_code     text,
    request_id      text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT trip_events_sequence_unique UNIQUE (trip_id, sequence)
);

CREATE INDEX trip_events_trip_idx ON trip_events (trip_id, sequence DESC);

CREATE TRIGGER trip_events_append_only
    BEFORE UPDATE OR DELETE ON trip_events
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
