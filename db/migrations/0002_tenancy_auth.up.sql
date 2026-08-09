-- 0002 Multi-tenancy, authentication, RBAC and the audit foundation (M01).

-- ---------------------------------------------------------------------------
-- Organizations (tenants)
-- ---------------------------------------------------------------------------
CREATE TABLE organizations (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'org')),
    code            text        NOT NULL UNIQUE CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    name            text        NOT NULL CHECK (length(btrim(name)) BETWEEN 2 AND 160),
    legal_name      text,
    status          text        NOT NULL DEFAULT 'ACTIVE'
                                CHECK (status IN ('ACTIVE','SUSPENDED','CLOSED')),
    -- is_platform marks the operator's own tenant. SUPER_ADMIN users live here
    -- and are the only principals permitted to act across tenants.
    is_platform     boolean     NOT NULL DEFAULT false,
    timezone        text        NOT NULL DEFAULT 'Asia/Kolkata',
    currency        char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    awb_prefix      text        NOT NULL CHECK (awb_prefix ~ '^[A-Z]{2,4}$'),
    contact_email   text,
    contact_phone   text,
    gst_number      text,
    settings        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Exactly one platform tenant may exist.
CREATE UNIQUE INDEX organizations_single_platform_idx ON organizations ((true)) WHERE is_platform;
CREATE INDEX organizations_status_idx ON organizations (status) WHERE status <> 'ACTIVE';

CREATE TRIGGER organizations_set_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Users
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'usr')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    -- Email is stored lower-cased so uniqueness is case-insensitive without
    -- requiring the citext extension.
    email               text        NOT NULL CHECK (email = lower(email) AND email LIKE '%@%.%'),
    password_hash       text        NOT NULL,
    full_name           text        NOT NULL CHECK (length(btrim(full_name)) BETWEEN 1 AND 160),
    phone               text,
    status              text        NOT NULL DEFAULT 'ACTIVE'
                                    CHECK (status IN ('ACTIVE','INACTIVE','LOCKED')),
    is_super_admin      boolean     NOT NULL DEFAULT false,
    must_change_password boolean    NOT NULL DEFAULT false,
    failed_login_count  integer     NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
    locked_until        timestamptz,
    last_login_at       timestamptz,
    password_changed_at timestamptz NOT NULL DEFAULT now(),
    created_by          bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique_per_org UNIQUE (organization_id, email)
);

-- Login resolves a user by email across the whole platform (the tenant is
-- derived from the user, never supplied by the client), so this lookup must be
-- indexed globally.
CREATE INDEX users_email_idx ON users (email);
CREATE INDEX users_org_status_idx ON users (organization_id, status, id);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Permissions catalogue (global, immutable reference data)
-- ---------------------------------------------------------------------------
CREATE TABLE permissions (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id   text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'perm')),
    code        text        NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    resource    text        NOT NULL,
    action      text        NOT NULL,
    description text        NOT NULL,
    module      text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX permissions_module_idx ON permissions (module, code);

-- ---------------------------------------------------------------------------
-- Roles
-- ---------------------------------------------------------------------------
CREATE TABLE roles (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'rol')),
    -- System roles are shared across tenants and have organization_id NULL.
    organization_id bigint      REFERENCES organizations (id) ON DELETE CASCADE,
    code            text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z][A-Z0-9_]{1,47}$'),
    name            text        NOT NULL,
    description     text        NOT NULL DEFAULT '',
    is_system       boolean     NOT NULL DEFAULT false,
    -- Scope declares whether a grant of this role must name an operating unit.
    scope_required  boolean     NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT roles_system_has_no_org CHECK ((is_system AND organization_id IS NULL) OR NOT is_system)
);

CREATE UNIQUE INDEX roles_system_code_idx ON roles (code) WHERE organization_id IS NULL;
CREATE UNIQUE INDEX roles_org_code_idx ON roles (organization_id, code) WHERE organization_id IS NOT NULL;

CREATE TRIGGER roles_set_updated_at
    BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE role_permissions (
    role_id       bigint NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission_id bigint NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,
    granted_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX role_permissions_permission_idx ON role_permissions (permission_id);

-- ---------------------------------------------------------------------------
-- Role assignments
-- ---------------------------------------------------------------------------
-- operating_unit_id scopes a grant to one branch/hub. It is a bigint reference
-- resolved in migration 0005 once operating_units exists; declaring it here
-- keeps the assignment model in one place.
CREATE TABLE user_roles (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id   bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id           bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id           bigint      NOT NULL REFERENCES roles (id) ON DELETE RESTRICT,
    operating_unit_id bigint,
    granted_by        bigint      REFERENCES users (id) ON DELETE SET NULL,
    granted_at        timestamptz NOT NULL DEFAULT now()
);

-- A user may hold the same role at several operating units, but never twice at
-- the same one. Two partial unique indexes are required because NULL is not
-- equal to NULL in a plain unique constraint.
CREATE UNIQUE INDEX user_roles_scoped_idx
    ON user_roles (user_id, role_id, operating_unit_id) WHERE operating_unit_id IS NOT NULL;
CREATE UNIQUE INDEX user_roles_global_idx
    ON user_roles (user_id, role_id) WHERE operating_unit_id IS NULL;
CREATE INDEX user_roles_user_idx ON user_roles (user_id);
CREATE INDEX user_roles_role_idx ON user_roles (role_id);
CREATE INDEX user_roles_unit_idx ON user_roles (operating_unit_id) WHERE operating_unit_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Sessions and refresh tokens
-- ---------------------------------------------------------------------------
-- One row per issued refresh token. Rotation inserts a child row and revokes
-- the parent; presenting a revoked token whose child exists is refresh-token
-- reuse and revokes the entire chain.
CREATE TABLE sessions (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ses')),
    organization_id     bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id             bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- SHA-256 of the opaque refresh token. The token itself is never stored.
    refresh_token_hash  bytea       NOT NULL UNIQUE CHECK (octet_length(refresh_token_hash) = 32),
    -- Chain root: all rotations of one login share this value, so revoking a
    -- login revokes every descendant in a single indexed statement. It is
    -- generated by the application at login rather than derived from id, which
    -- is not available until after the insert.
    chain_id            text        NOT NULL CHECK (is_public_id(chain_id, 'ses')),
    parent_session_id   bigint      REFERENCES sessions (id) ON DELETE SET NULL,
    rotation_counter    integer     NOT NULL DEFAULT 0 CHECK (rotation_counter >= 0),
    user_agent          text,
    client_ip           text        CHECK (client_ip IS NULL OR length(client_ip) BETWEEN 3 AND 45),
    issued_at           timestamptz NOT NULL DEFAULT now(),
    last_used_at        timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL,
    revoked_at          timestamptz,
    revoked_reason      text        CHECK (revoked_reason IS NULL OR revoked_reason IN
                                    ('LOGOUT','ROTATED','REUSE_DETECTED','PASSWORD_CHANGED','ADMIN_REVOKED',
                                     'USER_DEACTIVATED','EXPIRED','LOGOUT_ALL')),
    CONSTRAINT sessions_revocation_consistent CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);

CREATE INDEX sessions_user_active_idx ON sessions (user_id, expires_at DESC) WHERE revoked_at IS NULL;
CREATE INDEX sessions_chain_idx ON sessions (chain_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON sessions (expires_at) WHERE revoked_at IS NULL;
CREATE INDEX sessions_org_idx ON sessions (organization_id, issued_at DESC);

-- ---------------------------------------------------------------------------
-- Password reset tokens
-- ---------------------------------------------------------------------------
CREATE TABLE password_reset_tokens (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    request_ip  text        CHECK (request_ip IS NULL OR length(request_ip) BETWEEN 3 AND 45),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Single-use enforcement is `used_at IS NULL` plus an UPDATE ... WHERE used_at
-- IS NULL; this index keeps the outstanding-token lookup cheap.
CREATE INDEX password_reset_tokens_user_idx ON password_reset_tokens (user_id) WHERE used_at IS NULL;
CREATE INDEX password_reset_tokens_expiry_idx ON password_reset_tokens (expires_at) WHERE used_at IS NULL;

-- ---------------------------------------------------------------------------
-- Audit events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE audit_events (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id         text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'aud')),
    organization_id   bigint      REFERENCES organizations (id) ON DELETE RESTRICT,
    actor_user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    actor_type        text        NOT NULL DEFAULT 'USER'
                                  CHECK (actor_type IN ('USER','SYSTEM','API_KEY','ANONYMOUS')),
    actor_label       text,
    action            text        NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    resource_type     text        NOT NULL,
    resource_id       bigint,
    resource_public_id text,
    operating_unit_id bigint,
    request_id        text,
    client_ip           text        CHECK (client_ip IS NULL OR length(client_ip) BETWEEN 3 AND 45),
    reason            text,
    before_state      jsonb,
    after_state       jsonb,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_org_time_idx ON audit_events (organization_id, occurred_at DESC, id DESC);
CREATE INDEX audit_events_resource_idx ON audit_events (resource_type, resource_id, occurred_at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_user_id, occurred_at DESC) WHERE actor_user_id IS NOT NULL;
CREATE INDEX audit_events_action_idx ON audit_events (action, occurred_at DESC);
CREATE INDEX audit_events_request_idx ON audit_events (request_id) WHERE request_id IS NOT NULL;

CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
