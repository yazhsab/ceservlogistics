-- 0003 Platform runtime: idempotency ledger and the background job queue (M00).

-- ---------------------------------------------------------------------------
-- Idempotency
-- ---------------------------------------------------------------------------
-- Constitution §21: idempotency for finance-critical operations is backed by a
-- database uniqueness constraint, not by Redis. A retry of a booking that was
-- already committed replays the stored response instead of creating a second
-- shipment, and the guarantee survives a cache flush.
CREATE TABLE idempotency_keys (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- Scoping by (org, endpoint, key) lets a client reuse a key across
    -- different operations without collision, while preventing one tenant from
    -- probing another tenant's key space.
    endpoint          text        NOT NULL,
    idempotency_key   text        NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    -- SHA-256 over the canonicalised request. A second call with the same key
    -- but a different body is a client bug and is rejected, never silently
    -- served the first response.
    request_hash      bytea       NOT NULL CHECK (octet_length(request_hash) = 32),
    user_id           bigint      REFERENCES users (id) ON DELETE SET NULL,
    status            text        NOT NULL DEFAULT 'IN_PROGRESS'
                                  CHECK (status IN ('IN_PROGRESS','COMPLETED','FAILED')),
    response_status   integer     CHECK (response_status IS NULL OR response_status BETWEEN 100 AND 599),
    response_body     jsonb,
    resource_type     text,
    resource_public_id text,
    request_id        text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    expires_at        timestamptz NOT NULL,
    CONSTRAINT idempotency_keys_unique UNIQUE (organization_id, endpoint, idempotency_key),
    CONSTRAINT idempotency_completed_has_response
        CHECK (status <> 'COMPLETED' OR (response_status IS NOT NULL AND completed_at IS NOT NULL))
);

CREATE INDEX idempotency_keys_expiry_idx ON idempotency_keys (expires_at);
CREATE INDEX idempotency_keys_resource_idx ON idempotency_keys (resource_type, resource_public_id)
    WHERE resource_public_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Background jobs
-- ---------------------------------------------------------------------------
-- A PostgreSQL-backed queue keeps the initial deployment to one durable store
-- (Constitution §5 forbids Kafka/RabbitMQ without an ADR). Claiming uses
-- FOR UPDATE SKIP LOCKED, which scales comfortably past the throughput a 4 vCPU
-- box can produce.
CREATE TABLE jobs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'job')),
    organization_id bigint      REFERENCES organizations (id) ON DELETE CASCADE,
    queue           text        NOT NULL DEFAULT 'default' CHECK (queue ~ '^[a-z][a-z0-9_-]{0,31}$'),
    job_type        text        NOT NULL CHECK (job_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
    payload         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    status          text        NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING','RUNNING','SUCCEEDED','FAILED','DEAD','CANCELLED')),
    priority        smallint    NOT NULL DEFAULT 100 CHECK (priority BETWEEN 0 AND 1000),
    run_at          timestamptz NOT NULL DEFAULT now(),
    attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts    integer     NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 50),
    last_error      text,
    error_count     integer     NOT NULL DEFAULT 0,
    -- Optional deduplication: enqueueing the same logical work twice while the
    -- first copy is still pending is a no-op.
    dedupe_key      text,
    locked_at       timestamptz,
    locked_by       text,
    started_at      timestamptz,
    completed_at    timestamptz,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- The claim query: WHERE status='PENDING' AND run_at<=now() AND queue = ANY(...)
-- ORDER BY priority, run_at, id.
CREATE INDEX jobs_claim_idx ON jobs (queue, priority, run_at, id) WHERE status = 'PENDING';
CREATE INDEX jobs_stuck_idx ON jobs (locked_at) WHERE status = 'RUNNING';
CREATE INDEX jobs_org_type_idx ON jobs (organization_id, job_type, created_at DESC);
CREATE UNIQUE INDEX jobs_dedupe_idx ON jobs (dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status IN ('PENDING','RUNNING');
CREATE INDEX jobs_dead_idx ON jobs (completed_at DESC) WHERE status = 'DEAD';

CREATE TRIGGER jobs_set_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
