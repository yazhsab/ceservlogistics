-- 0016 Hub operations (M14): exceptions and reconciliation.
--
-- Reconciliation answers one question at every facility handover: does what
-- arrived match what was declared? The declaration is the frozen snapshot taken
-- at bag/manifest closure; the actual is what got scanned. Every difference
-- becomes an exception with an owner and a resolution, because an unexplained
-- discrepancy in a courier network is either a theft, a misroute or a billing
-- dispute waiting to happen.

-- ---------------------------------------------------------------------------
-- Operational exceptions
-- ---------------------------------------------------------------------------
CREATE TABLE operational_exceptions (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'exc')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    exception_code    text        NOT NULL,

    exception_type    text        NOT NULL CHECK (exception_type IN
                      ('MISSING','EXCESS','DAMAGED','MISROUTED','SEAL_BROKEN','UNKNOWN_BARCODE',
                       'WEIGHT_MISMATCH','COUNT_MISMATCH','WRONG_FACILITY','CUSTODY_OVERRIDE','OTHER')),
    severity          text        NOT NULL DEFAULT 'MEDIUM'
                                  CHECK (severity IN ('LOW','MEDIUM','HIGH','CRITICAL')),
    status            text        NOT NULL DEFAULT 'OPEN' CHECK (status IN
                      ('OPEN','INVESTIGATING','RESOLVED','WRITTEN_OFF','CANCELLED')),

    -- Where it was raised, and against what. The subject is polymorphic because
    -- an exception can be about a shipment, a bag, a manifest or a bare
    -- barcode that matched nothing at all.
    operating_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    shipment_id       bigint      REFERENCES shipments (id) ON DELETE SET NULL,
    bag_id            bigint      REFERENCES bags (id) ON DELETE SET NULL,
    manifest_id       bigint      REFERENCES manifests (id) ON DELETE SET NULL,
    trip_id           bigint      REFERENCES trips (id) ON DELETE SET NULL,
    reconciliation_id bigint,
    scan_event_id     bigint      REFERENCES scan_events (id) ON DELETE SET NULL,
    raw_barcode       text,

    description       text        NOT NULL,
    -- Quantities in dispute, for shortage and overage cases.
    expected_count    integer     CHECK (expected_count IS NULL OR expected_count >= 0),
    actual_count      integer     CHECK (actual_count IS NULL OR actual_count >= 0),
    expected_weight_grams bigint  CHECK (expected_weight_grams IS NULL OR expected_weight_grams >= 0),
    actual_weight_grams bigint    CHECK (actual_weight_grams IS NULL OR actual_weight_grams >= 0),

    raised_at         timestamptz NOT NULL DEFAULT now(),
    raised_by_user_id bigint      REFERENCES users (id) ON DELETE SET NULL,
    assigned_to_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,

    resolution_action text        CHECK (resolution_action IS NULL OR resolution_action IN
                      ('FOUND','RETURNED_TO_ORIGIN','FORWARDED','REBAGGED','DELIVERED','WRITTEN_OFF',
                       'CLAIM_FILED','CORRECTED','NO_ACTION')),
    resolution_notes  text,
    resolved_at       timestamptz,
    resolved_by_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,

    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT operational_exceptions_code_unique UNIQUE (organization_id, exception_code),
    CONSTRAINT operational_exceptions_resolution_recorded
        CHECK (status NOT IN ('RESOLVED','WRITTEN_OFF')
               OR (resolved_at IS NOT NULL AND resolution_action IS NOT NULL AND resolution_notes IS NOT NULL))
);

CREATE INDEX operational_exceptions_unit_status_idx
    ON operational_exceptions (operating_unit_id, status, raised_at DESC);
CREATE INDEX operational_exceptions_org_created_idx
    ON operational_exceptions (organization_id, created_at DESC, id DESC);
CREATE INDEX operational_exceptions_shipment_idx
    ON operational_exceptions (shipment_id, raised_at DESC) WHERE shipment_id IS NOT NULL;
CREATE INDEX operational_exceptions_bag_idx
    ON operational_exceptions (bag_id) WHERE bag_id IS NOT NULL;
CREATE INDEX operational_exceptions_open_idx
    ON operational_exceptions (organization_id, severity, raised_at)
    WHERE status IN ('OPEN','INVESTIGATING');

CREATE TRIGGER operational_exceptions_touch BEFORE UPDATE ON operational_exceptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER operational_exceptions_bump_version BEFORE UPDATE ON operational_exceptions
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

ALTER TABLE bag_items ADD CONSTRAINT bag_items_exception_fk
    FOREIGN KEY (exception_id) REFERENCES operational_exceptions (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Reconciliations
-- ---------------------------------------------------------------------------
CREATE TABLE reconciliations (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rec')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    reconciliation_code text      NOT NULL,

    subject_type      text        NOT NULL CHECK (subject_type IN ('BAG','MANIFEST','TRIP')),
    bag_id            bigint      REFERENCES bags (id) ON DELETE CASCADE,
    manifest_id       bigint      REFERENCES manifests (id) ON DELETE CASCADE,
    trip_id           bigint      REFERENCES trips (id) ON DELETE CASCADE,
    operating_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,

    status            text        NOT NULL DEFAULT 'IN_PROGRESS' CHECK (status IN
                      ('IN_PROGRESS','COMPLETED','COMPLETED_WITH_EXCEPTIONS','ABANDONED')),

    expected_count    integer     NOT NULL DEFAULT 0 CHECK (expected_count >= 0),
    scanned_count     integer     NOT NULL DEFAULT 0 CHECK (scanned_count >= 0),
    matched_count     integer     NOT NULL DEFAULT 0 CHECK (matched_count >= 0),
    missing_count     integer     NOT NULL DEFAULT 0 CHECK (missing_count >= 0),
    excess_count      integer     NOT NULL DEFAULT 0 CHECK (excess_count >= 0),
    damaged_count     integer     NOT NULL DEFAULT 0 CHECK (damaged_count >= 0),

    started_at        timestamptz NOT NULL DEFAULT now(),
    started_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    completed_at      timestamptz,
    completed_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,
    remarks           text,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    version           integer     NOT NULL DEFAULT 1,

    CONSTRAINT reconciliations_code_unique UNIQUE (organization_id, reconciliation_code),
    -- Exactly one subject, matching subject_type.
    CONSTRAINT reconciliations_subject CHECK (
        (subject_type = 'BAG'      AND bag_id IS NOT NULL AND manifest_id IS NULL AND trip_id IS NULL) OR
        (subject_type = 'MANIFEST' AND manifest_id IS NOT NULL AND bag_id IS NULL AND trip_id IS NULL) OR
        (subject_type = 'TRIP'     AND trip_id IS NOT NULL AND bag_id IS NULL AND manifest_id IS NULL)),
    CONSTRAINT reconciliations_completion_recorded
        CHECK (status NOT IN ('COMPLETED','COMPLETED_WITH_EXCEPTIONS') OR completed_at IS NOT NULL)
);

-- One live reconciliation per subject, so two clerks cannot both be counting.
CREATE UNIQUE INDEX reconciliations_active_bag_idx
    ON reconciliations (bag_id) WHERE bag_id IS NOT NULL AND status = 'IN_PROGRESS';
CREATE UNIQUE INDEX reconciliations_active_manifest_idx
    ON reconciliations (manifest_id) WHERE manifest_id IS NOT NULL AND status = 'IN_PROGRESS';
CREATE INDEX reconciliations_unit_idx ON reconciliations (operating_unit_id, status, started_at DESC);
CREATE INDEX reconciliations_org_created_idx ON reconciliations (organization_id, created_at DESC, id DESC);

CREATE TRIGGER reconciliations_touch BEFORE UPDATE ON reconciliations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER reconciliations_bump_version BEFORE UPDATE ON reconciliations
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

ALTER TABLE operational_exceptions ADD CONSTRAINT operational_exceptions_reconciliation_fk
    FOREIGN KEY (reconciliation_id) REFERENCES reconciliations (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Reconciliation items
-- ---------------------------------------------------------------------------
-- One row per expected-or-scanned unit. Rows are created from the frozen
-- declaration when the count starts, then marked as scans arrive; anything
-- scanned that was not declared is inserted as EXCESS.
CREATE TABLE reconciliation_items (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    reconciliation_id bigint      NOT NULL REFERENCES reconciliations (id) ON DELETE CASCADE,

    item_type         text        NOT NULL CHECK (item_type IN ('SHIPMENT','BAG','PACKAGE','UNKNOWN')),
    shipment_id       bigint      REFERENCES shipments (id) ON DELETE SET NULL,
    bag_id            bigint      REFERENCES bags (id) ON DELETE SET NULL,
    raw_barcode       text        NOT NULL,

    outcome           text        NOT NULL DEFAULT 'EXPECTED' CHECK (outcome IN
                      ('EXPECTED','MATCHED','MISSING','EXCESS','DAMAGED','MISROUTED')),
    was_declared      boolean     NOT NULL DEFAULT true,
    scanned_at        timestamptz,
    scanned_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    scan_event_id     bigint      REFERENCES scan_events (id) ON DELETE SET NULL,
    exception_id      bigint      REFERENCES operational_exceptions (id) ON DELETE SET NULL,
    remarks           text,

    CONSTRAINT reconciliation_items_unique UNIQUE (reconciliation_id, raw_barcode)
);

CREATE INDEX reconciliation_items_recon_idx ON reconciliation_items (reconciliation_id, outcome);
CREATE INDEX reconciliation_items_shipment_idx ON reconciliation_items (shipment_id)
    WHERE shipment_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Facility hold register
-- ---------------------------------------------------------------------------
-- HOLD and RELEASE scans need a reason and an owner, and a held parcel must not
-- be dispatched for delivery. shipments.is_held is the fast flag; this table is
-- the history behind it.
CREATE TABLE shipment_holds (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'hld')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    operating_unit_id bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,

    hold_reason_code  text        NOT NULL CHECK (hold_reason_code IN
                      ('CUSTOMS','PAYMENT_PENDING','ADDRESS_ISSUE','CUSTOMER_REQUEST','DAMAGE_ASSESSMENT',
                       'SECURITY_CHECK','DOCUMENTATION','WEATHER','INVESTIGATION','OTHER')),
    reason            text        NOT NULL,
    held_at           timestamptz NOT NULL DEFAULT now(),
    held_by_user_id   bigint      REFERENCES users (id) ON DELETE SET NULL,

    released_at       timestamptz,
    released_by_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,
    release_notes     text,
    expected_release_at timestamptz,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb
);

-- A shipment is held once at a time.
CREATE UNIQUE INDEX shipment_holds_active_idx
    ON shipment_holds (shipment_id) WHERE released_at IS NULL;
CREATE INDEX shipment_holds_unit_idx ON shipment_holds (operating_unit_id, held_at DESC)
    WHERE released_at IS NULL;
CREATE INDEX shipment_holds_shipment_idx ON shipment_holds (shipment_id, held_at DESC);
