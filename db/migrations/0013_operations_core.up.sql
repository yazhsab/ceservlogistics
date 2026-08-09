-- 0013 Operational event core, custody and scanning (M10) plus pickup (M09).
--
-- Release 2 turns the shipment from a booked record into a physical object that
-- moves. Two things must be true of every movement:
--
--   1. History is append-only. shipment_events already enforces that with a
--      trigger; this migration widens it with the operational fields the
--      Constitution's §"Operational Event Integrity" requires (device, source,
--      facility) rather than burying them in the metadata blob where they
--      cannot be indexed or constrained.
--
--   2. Custody is explicit. A parcel is held by exactly one facility, agent,
--      bag or trip at a time, and a transition is legal only from the holder.

-- ---------------------------------------------------------------------------
-- Operational fields on shipment_events
-- ---------------------------------------------------------------------------
-- occurred_at (client clock) and recorded_at (server clock) already exist and
-- stay distinct: a field app that scanned while offline reports when the scan
-- happened, but the server timestamp is the one any security or financial
-- decision uses. Constitution §"Never trust arbitrary client timestamps".
ALTER TABLE shipment_events
    ADD COLUMN source        text NOT NULL DEFAULT 'API'
        CHECK (source IN ('WEB','MOBILE','SCANNER','API','SYSTEM','PARTNER','PUBLIC')),
    ADD COLUMN device_id     text,
    ADD COLUMN device_model  text,
    ADD COLUMN latitude      double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    ADD COLUMN longitude     double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    -- Set when the client reported an occurrence time the server had to clamp,
    -- so an auditor can see the raw claim rather than a silently rewritten one.
    ADD COLUMN client_occurred_at timestamptz;

-- The event vocabulary widens for physical operations. SCAN covers the
-- scanner-grade operations in M10; the others name workflow milestones so a
-- tracking view can filter without parsing descriptions.
ALTER TABLE shipment_events DROP CONSTRAINT shipment_events_event_type_check;
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_event_type_check
    CHECK (event_type IN (
        'BOOKED','STATUS_CHANGED','CANCELLED','EXCEPTION','REMARK','LOCATION_UPDATE','SYSTEM',
        'SCAN','PICKUP','BAG','MANIFEST','TRIP','DELIVERY','NDR','RTO','POD','HOLD','RELEASE'));

-- Occurrence order for the public tracking timeline, which sorts by when the
-- event happened rather than when it was uploaded.
CREATE INDEX shipment_events_occurred_idx ON shipment_events (shipment_id, occurred_at DESC, sequence DESC);

-- ---------------------------------------------------------------------------
-- Custody on the shipment
-- ---------------------------------------------------------------------------
-- current_custody_unit_id and current_custody_user_id arrived in 0011. Bag and
-- trip custody complete the picture: they answer "where physically is this
-- parcel right now" without walking the event history.
ALTER TABLE shipments
    ADD COLUMN current_bag_id  bigint,
    ADD COLUMN current_trip_id bigint,
    -- Direction of travel. RTO reuses the same facilities and trips as the
    -- forward flow, so a flag is the honest model — not a duplicated set of
    -- reverse statuses. See ADR 0009.
    ADD COLUMN movement_direction text NOT NULL DEFAULT 'FORWARD'
        CHECK (movement_direction IN ('FORWARD','REVERSE')),
    ADD COLUMN delivery_attempt_count integer NOT NULL DEFAULT 0
        CHECK (delivery_attempt_count >= 0),
    ADD COLUMN pickup_attempt_count integer NOT NULL DEFAULT 0
        CHECK (pickup_attempt_count >= 0),
    ADD COLUMN first_ofd_at    timestamptz,
    ADD COLUMN delivered_at    timestamptz,
    ADD COLUMN picked_up_at    timestamptz,
    -- Set when a facility puts the parcel on hold; blocks delivery dispatch
    -- until released.
    ADD COLUMN is_held         boolean NOT NULL DEFAULT false,
    ADD COLUMN hold_reason     text;

CREATE INDEX shipments_custody_unit_idx ON shipments (current_custody_unit_id, current_status)
    WHERE current_custody_unit_id IS NOT NULL;
CREATE INDEX shipments_custody_user_idx ON shipments (current_custody_user_id, current_status)
    WHERE current_custody_user_id IS NOT NULL;
CREATE INDEX shipments_current_bag_idx ON shipments (current_bag_id) WHERE current_bag_id IS NOT NULL;
CREATE INDEX shipments_current_trip_idx ON shipments (current_trip_id) WHERE current_trip_id IS NOT NULL;
-- The destination branch's delivery-ready queue (M15): a hot, narrow query.
CREATE INDEX shipments_delivery_queue_idx
    ON shipments (destination_branch_id, current_status, promised_delivery_at)
    WHERE current_status IN ('DESTINATION_BRANCH_RECEIVED','NDR','DELIVERY_FAILED');

-- ---------------------------------------------------------------------------
-- Scan events (M10)
-- ---------------------------------------------------------------------------
-- The raw scanner record, kept separate from shipment_events because the two
-- answer different questions:
--
--   shipment_events -> "what happened to this parcel"    (customer/audit view)
--   scan_events     -> "what did this device send us"    (operational forensics)
--
-- A scan that was rejected still lands here with its rejection reason, which is
-- how a supervisor discovers that a device is scanning parcels into the wrong
-- facility. shipment_events would be polluted by those.
CREATE TABLE scan_events (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'scn')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    -- The barcode exactly as the device read it, before normalisation. Kept so
    -- a mis-scan can be diagnosed from the original input.
    raw_barcode       text        NOT NULL CHECK (length(raw_barcode) BETWEEN 1 AND 64),
    -- Resolved references. Null when the barcode matched nothing, which is the
    -- UNKNOWN_BARCODE exception in M14.
    shipment_id       bigint      REFERENCES shipments (id) ON DELETE CASCADE,
    package_id        bigint      REFERENCES shipment_packages (id) ON DELETE SET NULL,

    scan_type         text        NOT NULL CHECK (scan_type IN
                      ('RECEIVE','ARRIVAL','DEPARTURE','SORT','HOLD','RELEASE','DAMAGE','EXCEPTION')),
    operating_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    scanned_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,

    -- Offline safety: the device generates this id before the scan is queued,
    -- so a retry after a dropped connection collapses onto the same row.
    device_event_id   text        CHECK (device_event_id IS NULL OR length(device_event_id) BETWEEN 8 AND 128),
    device_id         text,
    source            text        NOT NULL DEFAULT 'SCANNER'
                                  CHECK (source IN ('WEB','MOBILE','SCANNER','API','SYSTEM')),

    occurred_at       timestamptz NOT NULL DEFAULT now(),
    recorded_at       timestamptz NOT NULL DEFAULT now(),

    -- Outcome. ACCEPTED produced a shipment event; DUPLICATE was a retry of a
    -- scan already applied; REJECTED failed validation and changed nothing.
    outcome           text        NOT NULL CHECK (outcome IN ('ACCEPTED','DUPLICATE','REJECTED')),
    rejection_code    text,
    rejection_message text,
    -- The shipment event this scan produced, when it produced one.
    shipment_event_id bigint      REFERENCES shipment_events (id) ON DELETE SET NULL,
    from_status       text,
    to_status         text,

    latitude          double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude         double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    reference_type    text,
    reference_id      bigint,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT scan_events_rejection_recorded
        CHECK (outcome <> 'REJECTED' OR rejection_code IS NOT NULL)
);

-- Device-level idempotency. Scoped to the device so two handsets cannot collide
-- on a locally generated id, and partial so scans without one are unconstrained.
CREATE UNIQUE INDEX scan_events_device_event_idx
    ON scan_events (organization_id, device_id, device_event_id)
    WHERE device_event_id IS NOT NULL AND device_id IS NOT NULL;

CREATE INDEX scan_events_shipment_idx ON scan_events (shipment_id, occurred_at DESC)
    WHERE shipment_id IS NOT NULL;
CREATE INDEX scan_events_unit_time_idx ON scan_events (operating_unit_id, occurred_at DESC, id DESC);
CREATE INDEX scan_events_org_outcome_idx ON scan_events (organization_id, outcome, occurred_at DESC)
    WHERE outcome <> 'ACCEPTED';

CREATE TRIGGER scan_events_append_only
    BEFORE UPDATE OR DELETE ON scan_events
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Pickup requests (M09)
-- ---------------------------------------------------------------------------
CREATE TABLE pickup_requests (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id          text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pkr')),
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    reference_code     text        NOT NULL,

    customer_id        bigint      NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    -- The branch that owns the pickup. Resolved from the address's PIN code at
    -- creation, so the work lands in the right queue.
    branch_id          bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,

    pickup_type        text        NOT NULL CHECK (pickup_type IN ('SCHEDULED','ON_DEMAND','BULK','RECURRING')),
    status             text        NOT NULL DEFAULT 'REQUESTED' CHECK (status IN
                       ('REQUESTED','SCHEDULED','ASSIGNED','ACCEPTED','IN_PROGRESS',
                        'COMPLETED','PARTIALLY_COMPLETED','FAILED','CANCELLED','EXPIRED')),

    -- Address snapshot. A pickup must go where the customer said at the time of
    -- the request, not where their address record points today.
    contact_name       text        NOT NULL,
    contact_phone      text        NOT NULL,
    alt_phone          text,
    line1              text        NOT NULL,
    line2              text,
    landmark           text,
    city_name          text        NOT NULL,
    state_name         text        NOT NULL,
    pincode            text        NOT NULL,
    latitude           double precision,
    longitude          double precision,
    source_address_id  bigint      REFERENCES customer_addresses (id) ON DELETE SET NULL,

    -- Requested window. The end bound is enforced to follow the start.
    scheduled_date     date        NOT NULL,
    window_start       timestamptz NOT NULL,
    window_end         timestamptz NOT NULL,

    -- What the customer says is waiting. Actuals are recorded on the attempt.
    expected_piece_count integer   NOT NULL DEFAULT 1 CHECK (expected_piece_count BETWEEN 1 AND 5000),
    expected_weight_grams integer  CHECK (expected_weight_grams IS NULL OR expected_weight_grams > 0),
    actual_piece_count   integer   NOT NULL DEFAULT 0 CHECK (actual_piece_count >= 0),

    special_instructions text,
    attempt_count      integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts       integer     NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),

    requested_by_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,
    completed_at       timestamptz,
    cancelled_at       timestamptz,
    cancellation_reason text,
    failure_reason     text,

    metadata           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            integer     NOT NULL DEFAULT 1,

    CONSTRAINT pickup_requests_reference_unique UNIQUE (organization_id, reference_code),
    CONSTRAINT pickup_requests_window_ordered CHECK (window_end > window_start),
    CONSTRAINT pickup_requests_cancellation_recorded
        CHECK (status <> 'CANCELLED' OR (cancelled_at IS NOT NULL AND cancellation_reason IS NOT NULL))
);

CREATE INDEX pickup_requests_branch_queue_idx
    ON pickup_requests (branch_id, scheduled_date, status);
CREATE INDEX pickup_requests_org_created_idx
    ON pickup_requests (organization_id, created_at DESC, id DESC);
CREATE INDEX pickup_requests_customer_idx
    ON pickup_requests (customer_id, created_at DESC, id DESC);
CREATE INDEX pickup_requests_open_idx ON pickup_requests (organization_id, status, window_end)
    WHERE status IN ('REQUESTED','SCHEDULED','ASSIGNED','ACCEPTED','IN_PROGRESS');

CREATE TRIGGER pickup_requests_touch BEFORE UPDATE ON pickup_requests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER pickup_requests_bump_version BEFORE UPDATE ON pickup_requests
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- Shipments booked ahead of collection are linked to the request. A shipment
-- belongs to at most one pickup request.
CREATE TABLE pickup_request_shipments (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    pickup_request_id  bigint      NOT NULL REFERENCES pickup_requests (id) ON DELETE CASCADE,
    shipment_id        bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    status             text        NOT NULL DEFAULT 'PENDING'
                                   CHECK (status IN ('PENDING','PICKED_UP','NOT_AVAILABLE','REJECTED','CANCELLED')),
    added_at           timestamptz NOT NULL DEFAULT now(),
    resolved_at        timestamptz,
    remarks            text,
    CONSTRAINT pickup_request_shipment_unique UNIQUE (pickup_request_id, shipment_id)
);

-- A shipment can only be waiting on one open pickup at a time.
CREATE UNIQUE INDEX pickup_request_shipments_active_idx
    ON pickup_request_shipments (shipment_id) WHERE status = 'PENDING';
CREATE INDEX pickup_request_shipments_request_idx ON pickup_request_shipments (pickup_request_id, status);

-- ---------------------------------------------------------------------------
-- Pickup runs — an agent's route for a shift
-- ---------------------------------------------------------------------------
CREATE TABLE pickup_runs (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'prn')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    run_code          text        NOT NULL,
    branch_id         bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    agent_user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    run_date          date        NOT NULL,
    status            text        NOT NULL DEFAULT 'PLANNED' CHECK (status IN
                      ('PLANNED','STARTED','COMPLETED','CANCELLED')),
    started_at        timestamptz,
    completed_at      timestamptz,
    vehicle_reference text,
    planned_stops     integer     NOT NULL DEFAULT 0 CHECK (planned_stops >= 0),
    completed_stops   integer     NOT NULL DEFAULT 0 CHECK (completed_stops >= 0),
    collected_pieces  integer     NOT NULL DEFAULT 0 CHECK (collected_pieces >= 0),
    created_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,
    CONSTRAINT pickup_runs_code_unique UNIQUE (organization_id, run_code)
);

CREATE INDEX pickup_runs_agent_date_idx ON pickup_runs (agent_user_id, run_date DESC, status);
CREATE INDEX pickup_runs_branch_date_idx ON pickup_runs (branch_id, run_date DESC, status);

CREATE TRIGGER pickup_runs_touch BEFORE UPDATE ON pickup_runs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER pickup_runs_bump_version BEFORE UPDATE ON pickup_runs
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Pickup assignments — one agent's responsibility for one request
-- ---------------------------------------------------------------------------
CREATE TABLE pickup_assignments (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pas')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    pickup_request_id bigint      NOT NULL REFERENCES pickup_requests (id) ON DELETE CASCADE,
    pickup_run_id     bigint      REFERENCES pickup_runs (id) ON DELETE SET NULL,
    agent_user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    assigned_by_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,

    status            text        NOT NULL DEFAULT 'ASSIGNED' CHECK (status IN
                      ('ASSIGNED','ACCEPTED','REJECTED','ARRIVED','COMPLETED','FAILED','CANCELLED','REASSIGNED')),
    -- Position in the run, so the agent app can present an ordered route.
    stop_sequence     integer     CHECK (stop_sequence IS NULL OR stop_sequence >= 1),

    assigned_at       timestamptz NOT NULL DEFAULT now(),
    accepted_at       timestamptz,
    rejected_at       timestamptz,
    rejection_reason  text,
    arrived_at        timestamptz,
    completed_at      timestamptz,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT pickup_assignments_rejection_recorded
        CHECK (status <> 'REJECTED' OR rejection_reason IS NOT NULL)
);

-- Exactly one live assignment per request: reassignment closes the previous one
-- first, and the partial unique index makes that a database guarantee rather
-- than a service-layer convention.
CREATE UNIQUE INDEX pickup_assignments_active_idx
    ON pickup_assignments (pickup_request_id)
    WHERE status IN ('ASSIGNED','ACCEPTED','ARRIVED');
CREATE INDEX pickup_assignments_agent_idx ON pickup_assignments (agent_user_id, status, assigned_at DESC);
CREATE INDEX pickup_assignments_run_idx ON pickup_assignments (pickup_run_id, stop_sequence)
    WHERE pickup_run_id IS NOT NULL;
CREATE UNIQUE INDEX pickup_assignments_run_stop_idx
    ON pickup_assignments (pickup_run_id, stop_sequence)
    WHERE pickup_run_id IS NOT NULL AND stop_sequence IS NOT NULL
      AND status NOT IN ('CANCELLED','REASSIGNED','REJECTED');

CREATE TRIGGER pickup_assignments_touch BEFORE UPDATE ON pickup_assignments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER pickup_assignments_bump_version BEFORE UPDATE ON pickup_assignments
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Pickup attempts — append-only record of every visit
-- ---------------------------------------------------------------------------
-- A failed visit is as much a fact as a successful one: NDR-style analysis of
-- pickup failures depends on them surviving a later success.
CREATE TABLE pickup_attempts (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pat')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    pickup_request_id bigint      NOT NULL REFERENCES pickup_requests (id) ON DELETE CASCADE,
    pickup_assignment_id bigint   REFERENCES pickup_assignments (id) ON DELETE SET NULL,
    attempt_number    integer     NOT NULL CHECK (attempt_number >= 1),

    outcome           text        NOT NULL CHECK (outcome IN
                      ('COMPLETED','PARTIAL','FAILED','RESCHEDULED','CANCELLED_ON_SITE')),
    failure_reason_code text,
    remarks           text,

    pieces_collected  integer     NOT NULL DEFAULT 0 CHECK (pieces_collected >= 0),
    -- Weight the agent actually measured, which may differ from what was booked
    -- and is what a reweigh dispute is settled against.
    weight_grams      integer     CHECK (weight_grams IS NULL OR weight_grams >= 0),

    agent_user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    occurred_at       timestamptz NOT NULL DEFAULT now(),
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    latitude          double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude         double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    device_id         text,
    device_event_id   text,
    next_attempt_at   timestamptz,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT pickup_attempts_number_unique UNIQUE (pickup_request_id, attempt_number),
    CONSTRAINT pickup_attempts_failure_reason
        CHECK (outcome NOT IN ('FAILED','RESCHEDULED') OR failure_reason_code IS NOT NULL)
);

CREATE UNIQUE INDEX pickup_attempts_device_event_idx
    ON pickup_attempts (organization_id, device_id, device_event_id)
    WHERE device_event_id IS NOT NULL AND device_id IS NOT NULL;
CREATE INDEX pickup_attempts_request_idx ON pickup_attempts (pickup_request_id, attempt_number DESC);

CREATE TRIGGER pickup_attempts_append_only
    BEFORE UPDATE OR DELETE ON pickup_attempts
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Pickup reference sequences
-- ---------------------------------------------------------------------------
-- Human-readable codes for pickup requests, runs, bags, manifests and trips all
-- share one allocator with the same atomic-UPSERT guarantee as AWB (§22).
CREATE TABLE operational_sequences (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- e.g. 'PKR', 'BAG', 'MFT', 'TRP', 'DRN'
    kind            text        NOT NULL CHECK (kind ~ '^[A-Z]{2,6}$'),
    -- Scope key: usually a facility code plus a period, so two branches never
    -- contend on the same counter row.
    scope_key       text        NOT NULL CHECK (scope_key ~ '^[A-Z0-9_-]{1,32}$'),
    current_value   bigint      NOT NULL DEFAULT 0 CHECK (current_value >= 0),
    max_value       bigint      NOT NULL DEFAULT 999999 CHECK (max_value > 0),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT operational_sequences_unique UNIQUE (organization_id, kind, scope_key)
);
