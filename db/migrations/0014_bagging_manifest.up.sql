-- 0014 Bagging (M11) and manifest (M12).
--
-- A bag is the physical unit of consolidation; a manifest is the paper (now
-- electronic) contract between two facilities describing what left one and
-- should arrive at the other. Both are first-class entities with a lifecycle,
-- not status columns on something else.
--
-- The central rule for both: closure freezes contents. It is enforced with a
-- trigger, not with a service-layer check, because a defect in one handler must
-- not be able to rewrite what a driver already signed for.

-- ---------------------------------------------------------------------------
-- Bags
-- ---------------------------------------------------------------------------
CREATE TABLE bags (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id          text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'bag')),
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    -- Scannable code printed on the bag tag, e.g. BLR001-BAG-260808-000017.
    bag_code           text        NOT NULL,
    -- The compact barcode payload. Separate from bag_code because tag printers
    -- have width limits and the human-readable code carries separators.
    barcode            text        NOT NULL,

    status             text        NOT NULL DEFAULT 'OPEN' CHECK (status IN
                       ('OPEN','CLOSED','DISPATCHED','RECEIVED','OPENED','RECONCILED','CANCELLED')),

    bag_type           text        NOT NULL DEFAULT 'STANDARD'
                                   CHECK (bag_type IN ('STANDARD','SECURE','FRAGILE','DOCUMENT','RETURN')),

    origin_unit_id     bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    -- Where the bag is meant to be opened. Every shipment added must be routed
    -- through this facility, which is the compatibility rule in M11.
    destination_unit_id bigint     NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    -- Optional service restriction: an EXPRESS bag refuses SURFACE shipments.
    courier_service_id bigint      REFERENCES courier_services (id) ON DELETE RESTRICT,
    -- REVERSE bags carry RTO traffic and accept only reverse-direction parcels.
    direction          text        NOT NULL DEFAULT 'FORWARD'
                                   CHECK (direction IN ('FORWARD','REVERSE')),

    shipment_count     integer     NOT NULL DEFAULT 0 CHECK (shipment_count >= 0),
    piece_count        integer     NOT NULL DEFAULT 0 CHECK (piece_count >= 0),
    total_weight_grams bigint      NOT NULL DEFAULT 0 CHECK (total_weight_grams >= 0),
    -- Tare plus contents, weighed at closure. Reconciliation compares this to
    -- what the receiving facility weighs.
    gross_weight_grams integer     CHECK (gross_weight_grams IS NULL OR gross_weight_grams > 0),
    max_weight_grams   integer     CHECK (max_weight_grams IS NULL OR max_weight_grams > 0),
    max_shipments      integer     CHECK (max_shipments IS NULL OR max_shipments > 0),

    opened_by_user_id  bigint      REFERENCES users (id) ON DELETE SET NULL,
    closed_by_user_id  bigint      REFERENCES users (id) ON DELETE SET NULL,
    closed_at          timestamptz,
    dispatched_at      timestamptz,
    received_at        timestamptz,
    received_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    received_unit_id   bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    opened_at_destination_at timestamptz,
    reconciled_at      timestamptz,

    -- Snapshot of contents taken at closure. Reconciliation compares scans
    -- against this frozen list, so a later correction cannot rewrite what the
    -- bag was declared to hold.
    closed_contents    jsonb,

    metadata           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            integer     NOT NULL DEFAULT 1,

    CONSTRAINT bags_code_unique UNIQUE (organization_id, bag_code),
    CONSTRAINT bags_barcode_unique UNIQUE (organization_id, barcode),
    CONSTRAINT bags_distinct_endpoints CHECK (origin_unit_id <> destination_unit_id),
    CONSTRAINT bags_closure_recorded
        CHECK (status NOT IN ('CLOSED','DISPATCHED','RECEIVED','OPENED','RECONCILED')
               OR (closed_at IS NOT NULL AND closed_contents IS NOT NULL))
);

CREATE INDEX bags_origin_status_idx ON bags (origin_unit_id, status, created_at DESC);
CREATE INDEX bags_destination_status_idx ON bags (destination_unit_id, status, created_at DESC);
CREATE INDEX bags_org_created_idx ON bags (organization_id, created_at DESC, id DESC);
-- The "open bags at my facility" panel, which every counter clerk hits.
CREATE INDEX bags_open_idx ON bags (origin_unit_id, destination_unit_id)
    WHERE status = 'OPEN';

CREATE TRIGGER bags_touch BEFORE UPDATE ON bags FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER bags_bump_version BEFORE UPDATE ON bags FOR EACH ROW EXECUTE FUNCTION bump_row_version();

ALTER TABLE shipments ADD CONSTRAINT shipments_current_bag_fk
    FOREIGN KEY (current_bag_id) REFERENCES bags (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Bag items
-- ---------------------------------------------------------------------------
CREATE TABLE bag_items (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    bag_id          bigint      NOT NULL REFERENCES bags (id) ON DELETE CASCADE,
    shipment_id     bigint      NOT NULL REFERENCES shipments (id) ON DELETE RESTRICT,

    status          text        NOT NULL DEFAULT 'IN_BAG' CHECK (status IN
                    ('IN_BAG','REMOVED','MISSING','EXCESS','DAMAGED','DELIVERED_SHORT')),
    piece_count     integer     NOT NULL DEFAULT 1 CHECK (piece_count >= 1),
    weight_grams    integer     NOT NULL DEFAULT 0 CHECK (weight_grams >= 0),

    added_at        timestamptz NOT NULL DEFAULT now(),
    added_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    removed_at      timestamptz,
    removed_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,
    removal_reason  text,
    -- Set when the item was added or removed through the privileged exception
    -- workflow after closure (§15: "post-close correction requires explicit
    -- exception workflow"). Nulls are the normal, unexceptional path.
    exception_id    bigint,
    -- Recorded when the destination scanned this item out of the bag.
    verified_at     timestamptz,
    verified_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,

    CONSTRAINT bag_items_unique UNIQUE (bag_id, shipment_id),
    CONSTRAINT bag_items_removal_recorded
        CHECK (status <> 'REMOVED' OR (removed_at IS NOT NULL AND removal_reason IS NOT NULL))
);

-- A shipment can be inside only one bag at a time. This is the "conflicting
-- active bag membership rejected" rule from §15, enforced by the database so a
-- concurrent double-bagging is a constraint violation rather than a race.
CREATE UNIQUE INDEX bag_items_active_membership_idx
    ON bag_items (shipment_id) WHERE status = 'IN_BAG';
CREATE INDEX bag_items_bag_idx ON bag_items (bag_id, status);
CREATE INDEX bag_items_shipment_idx ON bag_items (shipment_id, added_at DESC);

-- Contents are mutable only while the bag is OPEN. Enforced in the database:
-- §15 says closure freezes contents, and a rule that important should not
-- depend on every handler remembering to check.
CREATE FUNCTION bag_items_guard() RETURNS trigger AS $$
DECLARE
    bag_status text;
BEGIN
    SELECT status INTO bag_status FROM bags
     WHERE id = COALESCE(NEW.bag_id, OLD.bag_id);

    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'BAG_ITEM_IMMUTABLE: bag contents are append-only; remove items by status change while the bag is OPEN'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF TG_OP = 'INSERT' AND bag_status <> 'OPEN' THEN
        RAISE EXCEPTION 'BAG_NOT_OPEN: shipments can only be added while the bag is OPEN (bag is %)', bag_status
            USING ERRCODE = 'restrict_violation';
    END IF;

    -- After closure the only permitted changes are reconciliation outcomes and
    -- the exception workflow, never a plain removal.
    IF TG_OP = 'UPDATE' AND bag_status <> 'OPEN' THEN
        IF NEW.shipment_id <> OLD.shipment_id OR NEW.bag_id <> OLD.bag_id THEN
            RAISE EXCEPTION 'BAG_CONTENTS_FROZEN: closed bag contents cannot be reassigned'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF NEW.status = 'REMOVED' AND OLD.status = 'IN_BAG' AND NEW.exception_id IS NULL THEN
            RAISE EXCEPTION 'BAG_CONTENTS_FROZEN: removing an item from a closed bag requires an exception record'
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER bag_items_guard_trigger
    BEFORE INSERT OR UPDATE OR DELETE ON bag_items
    FOR EACH ROW EXECUTE FUNCTION bag_items_guard();

-- ---------------------------------------------------------------------------
-- Bag seals
-- ---------------------------------------------------------------------------
-- A seal is a tamper-evident tag. Its number is recorded at closure and checked
-- at receipt; a mismatch is a SEAL_BROKEN exception.
CREATE TABLE bag_seals (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    bag_id          bigint      NOT NULL REFERENCES bags (id) ON DELETE CASCADE,
    seal_number     text        NOT NULL CHECK (length(seal_number) BETWEEN 3 AND 64),
    seal_type       text        NOT NULL DEFAULT 'PLASTIC'
                                CHECK (seal_type IN ('PLASTIC','METAL','ELECTRONIC','TAPE')),
    applied_at      timestamptz NOT NULL DEFAULT now(),
    applied_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,
    applied_unit_id bigint      REFERENCES operating_units (id) ON DELETE SET NULL,

    -- Verification at the receiving end.
    verified_at     timestamptz,
    verified_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    verification_result text    CHECK (verification_result IS NULL OR
                                verification_result IN ('INTACT','BROKEN','MISSING','MISMATCH')),
    verification_remarks text,
    -- Seal broken deliberately when the bag was opened at destination.
    broken_at       timestamptz,
    broken_by_user_id bigint    REFERENCES users (id) ON DELETE SET NULL,

    CONSTRAINT bag_seals_number_unique UNIQUE (organization_id, seal_number)
);

CREATE INDEX bag_seals_bag_idx ON bag_seals (bag_id, applied_at DESC);

-- ---------------------------------------------------------------------------
-- Bag events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE bag_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'bev')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    bag_id          bigint      NOT NULL REFERENCES bags (id) ON DELETE CASCADE,
    sequence        integer     NOT NULL CHECK (sequence >= 1),
    event_type      text        NOT NULL CHECK (event_type IN
                    ('CREATED','ITEM_ADDED','ITEM_REMOVED','CLOSED','SEALED','DISPATCHED',
                     'RECEIVED','OPENED','RECONCILED','EXCEPTION','CANCELLED')),
    from_status     text,
    to_status       text,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    actor_user_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    operating_unit_id bigint    REFERENCES operating_units (id) ON DELETE SET NULL,
    description     text        NOT NULL,
    reason_code     text,
    request_id      text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT bag_events_sequence_unique UNIQUE (bag_id, sequence)
);

CREATE INDEX bag_events_bag_idx ON bag_events (bag_id, sequence DESC);

CREATE TRIGGER bag_events_append_only
    BEFORE UPDATE OR DELETE ON bag_events
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- Mirrors shipments.event_sequence so bag events get a gap-free counter.
ALTER TABLE bags ADD COLUMN event_sequence integer NOT NULL DEFAULT 0 CHECK (event_sequence >= 0);

-- ---------------------------------------------------------------------------
-- Manifests
-- ---------------------------------------------------------------------------
CREATE TABLE manifests (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id          text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'mft')),
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    manifest_code      text        NOT NULL,

    status             text        NOT NULL DEFAULT 'DRAFT' CHECK (status IN
                       ('DRAFT','CLOSED','DISPATCHED','RECEIVED','RECONCILED','CANCELLED')),

    origin_unit_id     bigint      NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    destination_unit_id bigint     NOT NULL REFERENCES operating_units (id) ON DELETE RESTRICT,
    -- The trip carrying it. Nullable while the manifest is being built, set
    -- before dispatch, and validated at departure.
    trip_id            bigint,
    direction          text        NOT NULL DEFAULT 'FORWARD'
                                   CHECK (direction IN ('FORWARD','REVERSE')),

    bag_count          integer     NOT NULL DEFAULT 0 CHECK (bag_count >= 0),
    loose_shipment_count integer   NOT NULL DEFAULT 0 CHECK (loose_shipment_count >= 0),
    total_shipment_count integer   NOT NULL DEFAULT 0 CHECK (total_shipment_count >= 0),
    total_piece_count  integer     NOT NULL DEFAULT 0 CHECK (total_piece_count >= 0),
    total_weight_grams bigint      NOT NULL DEFAULT 0 CHECK (total_weight_grams >= 0),
    declared_weight_grams bigint   CHECK (declared_weight_grams IS NULL OR declared_weight_grams >= 0),

    closed_at          timestamptz,
    closed_by_user_id  bigint      REFERENCES users (id) ON DELETE SET NULL,
    dispatched_at      timestamptz,
    dispatched_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,
    received_at        timestamptz,
    received_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    reconciled_at      timestamptz,

    -- The frozen content snapshot taken at closure (§16: "Manifest closure
    -- creates an immutable content snapshot").
    closed_contents    jsonb,
    -- Object key of the generated PDF, produced asynchronously.
    document_object_key text,
    document_generated_at timestamptz,

    event_sequence     integer     NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    remarks            text,
    metadata           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    version            integer     NOT NULL DEFAULT 1,

    CONSTRAINT manifests_code_unique UNIQUE (organization_id, manifest_code),
    CONSTRAINT manifests_distinct_endpoints CHECK (origin_unit_id <> destination_unit_id),
    CONSTRAINT manifests_closure_recorded
        CHECK (status NOT IN ('CLOSED','DISPATCHED','RECEIVED','RECONCILED')
               OR (closed_at IS NOT NULL AND closed_contents IS NOT NULL))
);

CREATE INDEX manifests_origin_status_idx ON manifests (origin_unit_id, status, created_at DESC);
CREATE INDEX manifests_destination_status_idx ON manifests (destination_unit_id, status, created_at DESC);
CREATE INDEX manifests_org_created_idx ON manifests (organization_id, created_at DESC, id DESC);
CREATE INDEX manifests_trip_idx ON manifests (trip_id) WHERE trip_id IS NOT NULL;
-- Expected inbound at a hub (M14): everything dispatched towards me.
CREATE INDEX manifests_inbound_idx ON manifests (destination_unit_id, dispatched_at DESC)
    WHERE status = 'DISPATCHED';

CREATE TRIGGER manifests_touch BEFORE UPDATE ON manifests FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER manifests_bump_version BEFORE UPDATE ON manifests FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Manifest contents
-- ---------------------------------------------------------------------------
CREATE TABLE manifest_bags (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    manifest_id     bigint      NOT NULL REFERENCES manifests (id) ON DELETE CASCADE,
    bag_id          bigint      NOT NULL REFERENCES bags (id) ON DELETE RESTRICT,
    status          text        NOT NULL DEFAULT 'LOADED' CHECK (status IN
                    ('LOADED','REMOVED','RECEIVED','MISSING','EXCESS','DAMAGED')),
    -- Counts copied at load time, so reconciliation can compare declared
    -- against actual without re-deriving history.
    declared_shipment_count integer NOT NULL DEFAULT 0 CHECK (declared_shipment_count >= 0),
    declared_piece_count integer NOT NULL DEFAULT 0 CHECK (declared_piece_count >= 0),
    declared_weight_grams bigint NOT NULL DEFAULT 0 CHECK (declared_weight_grams >= 0),
    added_at        timestamptz NOT NULL DEFAULT now(),
    added_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    removed_at      timestamptz,
    removal_reason  text,
    received_at     timestamptz,
    received_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT manifest_bags_unique UNIQUE (manifest_id, bag_id)
);

-- A bag travels on one manifest at a time.
CREATE UNIQUE INDEX manifest_bags_active_idx
    ON manifest_bags (bag_id) WHERE status IN ('LOADED','RECEIVED');
CREATE INDEX manifest_bags_manifest_idx ON manifest_bags (manifest_id, status);

CREATE TABLE manifest_loose_shipments (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    manifest_id     bigint      NOT NULL REFERENCES manifests (id) ON DELETE CASCADE,
    shipment_id     bigint      NOT NULL REFERENCES shipments (id) ON DELETE RESTRICT,
    status          text        NOT NULL DEFAULT 'LOADED' CHECK (status IN
                    ('LOADED','REMOVED','RECEIVED','MISSING','EXCESS','DAMAGED')),
    piece_count     integer     NOT NULL DEFAULT 1 CHECK (piece_count >= 1),
    weight_grams    integer     NOT NULL DEFAULT 0 CHECK (weight_grams >= 0),
    added_at        timestamptz NOT NULL DEFAULT now(),
    added_by_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    removed_at      timestamptz,
    removal_reason  text,
    received_at     timestamptz,
    received_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT manifest_loose_unique UNIQUE (manifest_id, shipment_id)
);

-- A loose shipment travels on one manifest at a time, and a shipment that is
-- inside a bag must not also be manifested loose. The second half of that rule
-- is a service check because it spans two tables.
CREATE UNIQUE INDEX manifest_loose_active_idx
    ON manifest_loose_shipments (shipment_id) WHERE status IN ('LOADED','RECEIVED');
CREATE INDEX manifest_loose_manifest_idx ON manifest_loose_shipments (manifest_id, status);

-- Contents frozen once the manifest leaves DRAFT, for the same reason as bags.
CREATE FUNCTION manifest_contents_guard() RETURNS trigger AS $$
DECLARE
    manifest_status text;
BEGIN
    SELECT status INTO manifest_status FROM manifests
     WHERE id = COALESCE(NEW.manifest_id, OLD.manifest_id);

    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'MANIFEST_ITEM_IMMUTABLE: manifest contents are append-only'
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF TG_OP = 'INSERT' AND manifest_status <> 'DRAFT' THEN
        RAISE EXCEPTION 'MANIFEST_NOT_DRAFT: contents can only be added while the manifest is DRAFT (manifest is %)', manifest_status
            USING ERRCODE = 'restrict_violation';
    END IF;

    IF TG_OP = 'UPDATE' AND manifest_status <> 'DRAFT' AND NEW.status = 'REMOVED' THEN
        RAISE EXCEPTION 'MANIFEST_CONTENTS_FROZEN: a closed manifest cannot have contents removed'
            USING ERRCODE = 'restrict_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER manifest_bags_guard_trigger
    BEFORE INSERT OR UPDATE OR DELETE ON manifest_bags
    FOR EACH ROW EXECUTE FUNCTION manifest_contents_guard();
CREATE TRIGGER manifest_loose_guard_trigger
    BEFORE INSERT OR UPDATE OR DELETE ON manifest_loose_shipments
    FOR EACH ROW EXECUTE FUNCTION manifest_contents_guard();

-- ---------------------------------------------------------------------------
-- Manifest events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE manifest_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'mev')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    manifest_id     bigint      NOT NULL REFERENCES manifests (id) ON DELETE CASCADE,
    sequence        integer     NOT NULL CHECK (sequence >= 1),
    event_type      text        NOT NULL CHECK (event_type IN
                    ('CREATED','BAG_ADDED','BAG_REMOVED','SHIPMENT_ADDED','SHIPMENT_REMOVED',
                     'CLOSED','DISPATCHED','RECEIVED','RECONCILED','EXCEPTION','CANCELLED','DOCUMENT_GENERATED')),
    from_status     text,
    to_status       text,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    actor_user_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    operating_unit_id bigint    REFERENCES operating_units (id) ON DELETE SET NULL,
    description     text        NOT NULL,
    reason_code     text,
    request_id      text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT manifest_events_sequence_unique UNIQUE (manifest_id, sequence)
);

CREATE INDEX manifest_events_manifest_idx ON manifest_events (manifest_id, sequence DESC);

CREATE TRIGGER manifest_events_append_only
    BEFORE UPDATE OR DELETE ON manifest_events
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
