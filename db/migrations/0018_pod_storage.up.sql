-- 0018 Proof of delivery (M19) and the object-storage register.
--
-- Constitution §31: photos, signatures and generated documents live in
-- S3-compatible object storage; PostgreSQL keeps the metadata. stored_objects
-- is the single register for every binary the platform holds, so retention,
-- orphan sweeping and access auditing have one place to look rather than one
-- per feature.

-- ---------------------------------------------------------------------------
-- Stored objects
-- ---------------------------------------------------------------------------
CREATE TABLE stored_objects (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'obj')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    -- The storage key. Generated server-side and never derived from the client
    -- filename, which is the path-traversal and overwrite control (§31).
    object_key      text        NOT NULL,
    bucket          text        NOT NULL,
    -- Logical grouping, which also drives retention policy.
    purpose         text        NOT NULL CHECK (purpose IN
                    ('POD_SIGNATURE','POD_PHOTO','POD_DOCUMENT','NDR_EVIDENCE','PICKUP_EVIDENCE',
                     'EXCEPTION_EVIDENCE','MANIFEST_DOCUMENT','LABEL','EXPORT','IMPORT','OTHER')),

    mime_type       text        NOT NULL,
    size_bytes      bigint      NOT NULL CHECK (size_bytes > 0),
    -- SHA-256 of the stored bytes. Lets a retrieval prove it got what was
    -- uploaded, and makes duplicate detection cheap.
    checksum_sha256 bytea       NOT NULL CHECK (length(checksum_sha256) = 32),
    -- What the client called it. Kept for display only; never used to build a
    -- path or to decide the content type.
    original_filename text,

    uploaded_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    uploaded_at     timestamptz NOT NULL DEFAULT now(),
    -- Storage is private; retrieval is via a short-lived signed URL issued only
    -- after an authorization check.
    is_public       boolean     NOT NULL DEFAULT false,
    retention_until timestamptz,
    deleted_at      timestamptz,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,

    CONSTRAINT stored_objects_key_unique UNIQUE (bucket, object_key)
);

CREATE INDEX stored_objects_org_purpose_idx ON stored_objects (organization_id, purpose, uploaded_at DESC);
CREATE INDEX stored_objects_checksum_idx ON stored_objects (organization_id, checksum_sha256);
CREATE INDEX stored_objects_retention_idx ON stored_objects (retention_until)
    WHERE retention_until IS NOT NULL AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Proof of delivery
-- ---------------------------------------------------------------------------
CREATE TABLE proof_of_delivery (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pod')),
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    shipment_id       bigint      NOT NULL REFERENCES shipments (id) ON DELETE CASCADE,
    delivery_attempt_id bigint    REFERENCES delivery_attempts (id) ON DELETE SET NULL,

    pod_type          text        NOT NULL DEFAULT 'DELIVERY'
                                  CHECK (pod_type IN ('DELIVERY','RTO_RETURN','CUSTOMER_PICKUP','HANDOVER')),

    recipient_name    text        NOT NULL,
    recipient_relationship text   NOT NULL DEFAULT 'SELF' CHECK (recipient_relationship IN
                      ('SELF','FAMILY','NEIGHBOUR','SECURITY','RECEPTION','COLLEAGUE','OTHER')),
    recipient_phone   text,
    -- Government id shown at the door, masked before storage. Never the full
    -- number: §35 keeps sensitive identifiers out of the database when a
    -- partial serves the business purpose.
    recipient_id_type text,
    recipient_id_masked text,

    -- Verification methods actually used.
    otp_verified      boolean     NOT NULL DEFAULT false,
    signature_captured boolean    NOT NULL DEFAULT false,
    photo_captured    boolean     NOT NULL DEFAULT false,

    delivered_at      timestamptz NOT NULL,
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    delivered_by_user_id bigint   REFERENCES users (id) ON DELETE SET NULL,
    operating_unit_id bigint      REFERENCES operating_units (id) ON DELETE SET NULL,

    -- Approximate position at the door. Deliberately "approximate": consumer
    -- GPS is not survey-grade and the accuracy figure is stored alongside so a
    -- dispute can weigh it honestly.
    latitude          double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude         double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    location_accuracy_m integer   CHECK (location_accuracy_m IS NULL OR location_accuracy_m >= 0),

    device_id         text,
    device_model      text,
    remarks           text,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,

    -- One POD per shipment per type: a delivery POD and an RTO-return POD can
    -- coexist, two delivery PODs cannot.
    CONSTRAINT proof_of_delivery_unique UNIQUE (shipment_id, pod_type)
);

CREATE INDEX proof_of_delivery_shipment_idx ON proof_of_delivery (shipment_id);
CREATE INDEX proof_of_delivery_org_time_idx ON proof_of_delivery (organization_id, delivered_at DESC, id DESC);
CREATE INDEX proof_of_delivery_agent_idx ON proof_of_delivery (delivered_by_user_id, delivered_at DESC)
    WHERE delivered_by_user_id IS NOT NULL;

CREATE TRIGGER proof_of_delivery_append_only
    BEFORE UPDATE OR DELETE ON proof_of_delivery
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- POD artifacts
-- ---------------------------------------------------------------------------
CREATE TABLE pod_artifacts (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'pda')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    proof_of_delivery_id bigint NOT NULL REFERENCES proof_of_delivery (id) ON DELETE CASCADE,
    stored_object_id bigint     NOT NULL REFERENCES stored_objects (id) ON DELETE RESTRICT,
    artifact_type   text        NOT NULL CHECK (artifact_type IN
                    ('SIGNATURE','PHOTO','ID_PROOF','DOCUMENT','AUDIO')),
    sequence        integer     NOT NULL DEFAULT 1 CHECK (sequence >= 1),
    caption         text,
    captured_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pod_artifacts_unique UNIQUE (proof_of_delivery_id, artifact_type, sequence)
);

CREATE INDEX pod_artifacts_pod_idx ON pod_artifacts (proof_of_delivery_id, artifact_type);
CREATE INDEX pod_artifacts_object_idx ON pod_artifacts (stored_object_id);

CREATE TRIGGER pod_artifacts_append_only
    BEFORE UPDATE OR DELETE ON pod_artifacts
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Object access log
-- ---------------------------------------------------------------------------
-- Who downloaded which POD, and when. A delivery dispute frequently turns on
-- who had access to the evidence, so retrieval is audited rather than silent.
CREATE TABLE object_access_log (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    stored_object_id bigint     NOT NULL REFERENCES stored_objects (id) ON DELETE CASCADE,
    accessed_by_user_id bigint  REFERENCES users (id) ON DELETE SET NULL,
    access_type     text        NOT NULL CHECK (access_type IN ('SIGNED_URL','DIRECT','ADMIN')),
    accessed_at     timestamptz NOT NULL DEFAULT now(),
    request_id      text,
    ip_address      inet,
    expires_at      timestamptz
);

CREATE INDEX object_access_log_object_idx ON object_access_log (stored_object_id, accessed_at DESC);
CREATE INDEX object_access_log_org_time_idx ON object_access_log (organization_id, accessed_at DESC);

CREATE TRIGGER object_access_log_append_only
    BEFORE UPDATE OR DELETE ON object_access_log
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Public tracking support
-- ---------------------------------------------------------------------------
-- Public tracking exposes a milestone timeline, never the raw event stream.
-- The mapping is data rather than a switch statement so an operator can retitle
-- a milestone without a deployment, and so the contract test can assert that
-- every status has customer-safe wording.
CREATE TABLE tracking_milestones (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      REFERENCES organizations (id) ON DELETE CASCADE,
    shipment_status text        NOT NULL,
    milestone_code  text        NOT NULL CHECK (milestone_code IN
                    ('BOOKED','PICKUP','IN_TRANSIT','OUT_FOR_DELIVERY','DELIVERED',
                     'EXCEPTION','RETURNING','RETURNED','CANCELLED')),
    -- Customer-facing wording. Internal descriptions never reach this endpoint.
    public_title    text        NOT NULL,
    public_description text     NOT NULL,
    -- Whether the facility name may be shown. Suppressed for sensitive
    -- facilities such as customs or security-hold areas (§M20).
    show_location   boolean     NOT NULL DEFAULT true,
    display_order   integer     NOT NULL DEFAULT 0,
    is_active       boolean     NOT NULL DEFAULT true,
    -- Null organization_id is the platform default; a tenant row overrides it.
    CONSTRAINT tracking_milestones_unique UNIQUE (organization_id, shipment_status)
);

CREATE UNIQUE INDEX tracking_milestones_default_idx
    ON tracking_milestones (shipment_status) WHERE organization_id IS NULL;
CREATE INDEX tracking_milestones_org_idx ON tracking_milestones (organization_id)
    WHERE organization_id IS NOT NULL;

-- Platform defaults for every shipment status. Deliberately vague about
-- internal geography: "in transit" rather than "left BLR sort hub bay 4".
INSERT INTO tracking_milestones (organization_id, shipment_status, milestone_code, public_title, public_description, show_location, display_order) VALUES
    (NULL,'BOOKED','BOOKED','Shipment booked','We have received your booking and the shipment is awaiting collection.',false,10),
    (NULL,'PICKUP_SCHEDULED','PICKUP','Pickup scheduled','A pickup has been scheduled for this shipment.',false,20),
    (NULL,'PICKUP_ASSIGNED','PICKUP','Pickup assigned','A courier has been assigned to collect this shipment.',false,30),
    (NULL,'PICKED_UP','PICKUP','Picked up','The shipment has been collected.',true,40),
    (NULL,'ORIGIN_BRANCH_RECEIVED','IN_TRANSIT','Received at origin','The shipment has arrived at our origin facility.',true,50),
    (NULL,'ORIGIN_BAGGED','IN_TRANSIT','Processed at origin','The shipment has been processed for dispatch.',true,60),
    (NULL,'ORIGIN_DISPATCHED','IN_TRANSIT','Dispatched','The shipment has left the origin facility.',true,70),
    (NULL,'IN_TRANSIT','IN_TRANSIT','In transit','The shipment is on its way.',false,80),
    (NULL,'TRANSIT_HUB_RECEIVED','IN_TRANSIT','In transit','The shipment has reached a transit facility.',true,90),
    (NULL,'TRANSIT_HUB_DISPATCHED','IN_TRANSIT','In transit','The shipment has left the transit facility.',true,100),
    (NULL,'DESTINATION_HUB_RECEIVED','IN_TRANSIT','Arrived at destination city','The shipment has reached the destination city.',true,110),
    (NULL,'DESTINATION_BRANCH_RECEIVED','IN_TRANSIT','Ready for delivery','The shipment has reached the delivery facility.',true,120),
    (NULL,'OUT_FOR_DELIVERY','OUT_FOR_DELIVERY','Out for delivery','The shipment is out for delivery today.',true,130),
    (NULL,'DELIVERED','DELIVERED','Delivered','The shipment has been delivered.',true,140),
    (NULL,'DELIVERY_FAILED','EXCEPTION','Delivery attempted','We attempted delivery but could not complete it.',true,150),
    (NULL,'NDR','EXCEPTION','Delivery on hold','We need more information before we can complete delivery.',false,160),
    (NULL,'RTO_INITIATED','RETURNING','Return started','The shipment is being returned to the sender.',false,170),
    (NULL,'RTO_IN_TRANSIT','RETURNING','Return in transit','The shipment is on its way back to the sender.',false,180),
    (NULL,'RTO_DELIVERED','RETURNED','Returned to sender','The shipment has been returned to the sender.',true,190),
    (NULL,'CANCELLED','CANCELLED','Cancelled','This shipment was cancelled.',false,200),
    (NULL,'LOST','EXCEPTION','Under investigation','We are investigating the status of this shipment.',false,210),
    (NULL,'DAMAGED','EXCEPTION','Under investigation','We are investigating the condition of this shipment.',false,220);
