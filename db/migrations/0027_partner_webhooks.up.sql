-- 0027 Partner API keys and webhooks (M32).
--
-- Two subsystems that face outward, and both are security-sensitive in ways the
-- internal API is not: a partner key is a long-lived credential held by another
-- company's systems, and a webhook endpoint is a URL this platform will make
-- outbound requests to.
--
-- Secret handling follows the same rule as passwords (§35): the plaintext exists
-- exactly once, in the response to the create call, and is never stored. What is
-- stored is an Argon2id hash. A lost key is replaced, not recovered.
--
-- Webhook delivery reuses the platform job queue for retry, backoff and
-- dead-lettering rather than reimplementing them; webhook_deliveries records
-- what each attempt actually did.

-- ---------------------------------------------------------------------------
-- API keys
-- ---------------------------------------------------------------------------
CREATE TABLE api_keys (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'apk')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    name            text        NOT NULL,
    -- The non-secret half, sent as the identifier. Indexed and safe to log.
    key_id          text        NOT NULL,
    -- Argon2id of the secret half. The plaintext is returned once at creation
    -- and never persisted anywhere.
    secret_hash     text        NOT NULL,
    -- First few characters of the secret, for display only ("sk_live_a1b2…"),
    -- so an operator can tell two keys apart in a list.
    secret_hint     text        NOT NULL,

    -- What this key may do. Enforced per request; a key with no scopes can do
    -- nothing, which is the safe default for a mis-created key.
    scopes          text[]      NOT NULL DEFAULT '{}'::text[],

    -- Optional network restriction. Empty means any source.
    allowed_cidrs   text[]      NOT NULL DEFAULT '{}'::text[],

    status          text        NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE','SUSPENDED','REVOKED','EXPIRED')),
    expires_at      timestamptz,

    -- Observability, not authorization. Updated out of band so a hot partner
    -- key does not serialise every request on one row.
    last_used_at    timestamptz,
    last_used_ip    inet,
    request_count   bigint      NOT NULL DEFAULT 0,

    -- Per-key throttle. NULL falls back to the tenant default.
    rate_limit_per_minute integer CHECK (rate_limit_per_minute IS NULL OR rate_limit_per_minute > 0),

    revoked_at      timestamptz,
    revoked_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    revoke_reason   text,

    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT api_keys_revocation_recorded CHECK (
        status <> 'REVOKED' OR (revoked_at IS NOT NULL AND revoke_reason IS NOT NULL)
    ),
    CONSTRAINT api_keys_scopes_known CHECK (
        scopes <@ ARRAY[
            'serviceability:read','pricing:read',
            'shipment:create','shipment:read','shipment:cancel',
            'tracking:read','pickup:create','pickup:read',
            'label:read','pod:read','webhook:manage'
        ]::text[]
    )
);

-- key_id is globally unique, not per tenant: it arrives before the tenant is
-- known, so it has to identify the organization by itself.
CREATE UNIQUE INDEX api_keys_key_id_idx ON api_keys (key_id);
CREATE INDEX api_keys_org_idx ON api_keys (organization_id, status);
CREATE INDEX api_keys_expiring_idx
    ON api_keys (expires_at) WHERE status = 'ACTIVE' AND expires_at IS NOT NULL;

CREATE TRIGGER api_keys_touch
    BEFORE UPDATE ON api_keys
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A revoked key stays revoked. Reinstating one would let a leaked credential
-- come back to life; issue a new key instead.
CREATE OR REPLACE FUNCTION api_keys_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = 'REVOKED' AND NEW.status <> 'REVOKED' THEN
        RAISE EXCEPTION
            'API_KEY_REVOKED: a revoked key cannot be reinstated; issue a new one'
            USING ERRCODE = 'restrict_violation';
    END IF;
    -- The secret is set once. Rotation means a new key.
    IF NEW.secret_hash IS DISTINCT FROM OLD.secret_hash THEN
        RAISE EXCEPTION
            'API_KEY_IMMUTABLE: an API key secret cannot be changed; issue a new key'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER api_keys_guard_trigger
    BEFORE UPDATE ON api_keys
    FOR EACH ROW EXECUTE FUNCTION api_keys_guard();

-- ---------------------------------------------------------------------------
-- Partner API request log
--
-- Separate from the general access log because a partner integration is
-- debugged by its owner, not by us: they need to see their own traffic, and we
-- need enough to answer "did you send that request".
-- ---------------------------------------------------------------------------
CREATE TABLE api_key_requests (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    api_key_id      bigint      NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,

    method          text        NOT NULL,
    route           text        NOT NULL,
    status_code     integer     NOT NULL,
    duration_ms     integer     NOT NULL,
    request_id      text,
    -- The error code returned, if any. Never the response body: it may carry
    -- customer data, and this table is read by the partner.
    error_code      text,
    client_ip       inet,
    occurred_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_key_requests_key_idx ON api_key_requests (api_key_id, occurred_at DESC);
CREATE INDEX api_key_requests_org_idx ON api_key_requests (organization_id, occurred_at DESC);

CREATE TRIGGER api_key_requests_append_only
    BEFORE UPDATE OR DELETE ON api_key_requests
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Webhook endpoints
-- ---------------------------------------------------------------------------
CREATE TABLE webhook_endpoints (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'whe')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    name            text        NOT NULL,
    url             text        NOT NULL,
    -- The signing secret. Stored in plaintext, unlike an API key, because we
    -- must compute the HMAC on every send — it is a shared secret, not a
    -- credential we verify. Access is restricted by permission and it is
    -- redacted from every response after creation.
    signing_secret  text        NOT NULL,

    status          text        NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE','PAUSED','DISABLED')),

    -- Consecutive failures. An endpoint that keeps failing is paused rather
    -- than retried forever, so one dead partner cannot fill the queue.
    consecutive_failures integer NOT NULL DEFAULT 0,
    disabled_reason text,
    last_success_at timestamptz,
    last_failure_at timestamptz,

    max_attempts    integer     NOT NULL DEFAULT 6 CHECK (max_attempts BETWEEN 1 AND 20),
    timeout_seconds integer     NOT NULL DEFAULT 10 CHECK (timeout_seconds BETWEEN 1 AND 60),

    api_key_id      bigint      REFERENCES api_keys (id) ON DELETE SET NULL,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    -- Outbound requests must be to a real HTTPS endpoint. Plain HTTP would send
    -- customer data in clear text over the internet.
    CONSTRAINT webhook_endpoints_https CHECK (url ~ '^https://'),
    CONSTRAINT webhook_endpoints_disable_recorded CHECK (
        status <> 'DISABLED' OR disabled_reason IS NOT NULL
    )
);

CREATE INDEX webhook_endpoints_org_idx ON webhook_endpoints (organization_id, status);

CREATE TRIGGER webhook_endpoints_touch
    BEFORE UPDATE ON webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Webhook subscriptions — which events an endpoint wants.
-- ---------------------------------------------------------------------------
CREATE TABLE webhook_subscriptions (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'whs')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    endpoint_id     bigint      NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,

    event_type      text        NOT NULL,
    -- Optional narrowing, e.g. only shipments for one customer.
    filter          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    is_active       boolean     NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX webhook_subscriptions_unique_idx
    ON webhook_subscriptions (endpoint_id, event_type);
CREATE INDEX webhook_subscriptions_event_idx
    ON webhook_subscriptions (organization_id, event_type) WHERE is_active;

-- ---------------------------------------------------------------------------
-- Webhook deliveries — one intended delivery of one event to one endpoint.
--
-- The event payload is stored, not regenerated at send time. Two reasons: a
-- retry three hours later must send what the event said *then*, not what the
-- object looks like now; and a replay must be byte-identical or the consumer's
-- own dedupe will not recognise it.
-- ---------------------------------------------------------------------------
CREATE TABLE webhook_deliveries (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'whd')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    endpoint_id     bigint      NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,

    event_type      text        NOT NULL,
    -- The id the consumer dedupes on. Sent as the Webhook-Id header and stable
    -- across every retry and replay of this delivery.
    event_id        text        NOT NULL,
    payload         jsonb       NOT NULL,

    status          text        NOT NULL DEFAULT 'PENDING' CHECK (status IN
                    ('PENDING','SENDING','DELIVERED','FAILED','DEAD_LETTER','CANCELLED')),
    attempt_count   integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at timestamptz,

    last_status_code integer,
    last_error      text,
    delivered_at    timestamptz,

    -- Set when this delivery is a manual replay of an earlier one.
    replay_of_id    bigint      REFERENCES webhook_deliveries (id) ON DELETE SET NULL,

    shipment_id     bigint      REFERENCES shipments (id) ON DELETE SET NULL,
    job_id          bigint      REFERENCES jobs (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT webhook_deliveries_failure_recorded CHECK (
        status NOT IN ('FAILED','DEAD_LETTER') OR last_error IS NOT NULL
    )
);

-- One delivery per (endpoint, event). A business event raised twice — a status
-- transition retried, a job replayed — does not notify the partner twice.
CREATE UNIQUE INDEX webhook_deliveries_event_idx
    ON webhook_deliveries (endpoint_id, event_id)
    WHERE replay_of_id IS NULL;

CREATE INDEX webhook_deliveries_pending_idx
    ON webhook_deliveries (organization_id, next_attempt_at)
    WHERE status IN ('PENDING','SENDING');
CREATE INDEX webhook_deliveries_endpoint_idx
    ON webhook_deliveries (endpoint_id, created_at DESC);
CREATE INDEX webhook_deliveries_dead_idx
    ON webhook_deliveries (organization_id, created_at DESC) WHERE status = 'DEAD_LETTER';

CREATE TRIGGER webhook_deliveries_touch
    BEFORE UPDATE ON webhook_deliveries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Webhook attempts — what each HTTP call actually did.
-- ---------------------------------------------------------------------------
CREATE TABLE webhook_attempts (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    delivery_id     bigint      NOT NULL REFERENCES webhook_deliveries (id) ON DELETE CASCADE,

    attempt_no      integer     NOT NULL CHECK (attempt_no > 0),
    status_code     integer,
    -- Bounded: a misconfigured endpoint returning a megabyte of HTML must not
    -- be able to fill the database.
    response_body   text        CHECK (response_body IS NULL OR length(response_body) <= 2048),
    error_message   text,
    duration_ms     integer,
    attempted_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX webhook_attempts_no_idx ON webhook_attempts (delivery_id, attempt_no);
CREATE INDEX webhook_attempts_delivery_idx ON webhook_attempts (delivery_id, attempted_at DESC);

CREATE TRIGGER webhook_attempts_append_only
    BEFORE UPDATE OR DELETE ON webhook_attempts
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
