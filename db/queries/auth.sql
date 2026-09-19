-- Tenancy, authentication and RBAC queries (M01).

-- ---------------------------------------------------------------------------
-- Organizations
-- ---------------------------------------------------------------------------

-- name: CreateOrganization :one
-- country decides tax identifier validation and address defaults. It falls back
-- to the column default when the caller does not say, which is the home market.
INSERT INTO organizations (
    public_id, code, name, legal_name, timezone, currency, awb_prefix,
    contact_email, contact_phone, gst_number, is_platform, settings, country
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('code'), sqlc.arg('name'), sqlc.narg('legal_name'),
    sqlc.arg('timezone'), sqlc.arg('currency'), sqlc.arg('awb_prefix'),
    sqlc.narg('contact_email'), sqlc.narg('contact_phone'), sqlc.narg('gst_number'),
    sqlc.arg('is_platform'), sqlc.arg('settings'),
    COALESCE(sqlc.narg('country')::char(2), 'NG')
)
RETURNING *;

-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE id = $1;

-- name: GetOrganizationByPublicID :one
SELECT * FROM organizations WHERE public_id = $1;

-- name: GetOrganizationByCode :one
SELECT * FROM organizations WHERE code = $1;

-- name: GetPlatformOrganization :one
SELECT * FROM organizations WHERE is_platform LIMIT 1;

-- name: UpdateOrganization :one
UPDATE organizations
SET name = COALESCE(sqlc.narg('name'), name),
    legal_name = COALESCE(sqlc.narg('legal_name'), legal_name),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    contact_email = COALESCE(sqlc.narg('contact_email'), contact_email),
    contact_phone = COALESCE(sqlc.narg('contact_phone'), contact_phone),
    gst_number = COALESCE(sqlc.narg('gst_number'), gst_number),
    status = COALESCE(sqlc.narg('status'), status),
    settings = COALESCE(sqlc.narg('settings'), settings)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListOrganizations :many
SELECT * FROM organizations
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY code
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountOrganizations :one
SELECT count(*) FROM organizations
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'));

-- ---------------------------------------------------------------------------
-- Users
-- ---------------------------------------------------------------------------

-- name: CreateUser :one
INSERT INTO users (
    public_id, organization_id, email, password_hash, full_name, phone,
    status, is_super_admin, must_change_password, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- GetUserForLogin resolves a credential by email across the platform.
--
-- The tenant is derived from the matched user (Constitution §9); the client
-- never supplies an organization on the login request, so it cannot influence
-- which tenant it authenticates against.
-- name: GetUserForLogin :one
SELECT u.*, o.status AS organization_status, o.public_id AS organization_public_id,
       o.code AS organization_code, o.is_platform AS organization_is_platform
FROM users u
JOIN organizations o ON o.id = u.organization_id
WHERE u.email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByPublicID :one
SELECT * FROM users WHERE public_id = $1 AND organization_id = $2;

-- name: GetUserWithOrganization :one
SELECT u.*, o.status AS organization_status, o.public_id AS organization_public_id,
       o.code AS organization_code, o.is_platform AS organization_is_platform,
       o.currency AS organization_currency, o.timezone AS organization_timezone,
       o.country AS organization_country,
       o.awb_prefix AS organization_awb_prefix
FROM users u
JOIN organizations o ON o.id = u.organization_id
WHERE u.id = $1;

-- name: ListUsers :many
SELECT u.*, count(*) OVER () AS total_count
FROM users u
WHERE u.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('status')::text IS NULL OR u.status = sqlc.narg('status'))
  AND (sqlc.narg('search')::text IS NULL
       OR u.email LIKE '%' || lower(sqlc.narg('search')) || '%'
       OR lower(u.full_name) LIKE '%' || lower(sqlc.narg('search')) || '%')
  AND (sqlc.narg('role_code')::text IS NULL OR EXISTS (
        SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
        WHERE ur.user_id = u.id AND r.code = sqlc.narg('role_code')))
ORDER BY u.full_name, u.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateUser :one
UPDATE users
SET full_name = COALESCE(sqlc.narg('full_name'), full_name),
    phone = COALESCE(sqlc.narg('phone'), phone),
    email = COALESCE(sqlc.narg('email'), email)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- name: SetUserStatus :one
UPDATE users
SET status = sqlc.arg('status'),
    failed_login_count = CASE WHEN sqlc.arg('status')::text = 'ACTIVE' THEN 0 ELSE failed_login_count END,
    locked_until = CASE WHEN sqlc.arg('status')::text = 'ACTIVE' THEN NULL ELSE locked_until END
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- UpdateUserPassword also clears a lockout: setting a new credential is proof
-- enough that the legitimate owner is back in control, and leaving the account
-- LOCKED after a successful reset would strand them.
-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = sqlc.arg('password_hash'),
    password_changed_at = now(),
    must_change_password = false,
    failed_login_count = 0,
    locked_until = NULL,
    status = CASE WHEN status = 'LOCKED' THEN 'ACTIVE' ELSE status END
WHERE id = sqlc.arg('id');

-- name: RecordLoginSuccess :exec
UPDATE users
SET last_login_at = now(), failed_login_count = 0, locked_until = NULL
WHERE id = $1;

-- RecordLoginFailure increments the counter and locks the account once the
-- threshold is reached. Doing both in one statement keeps the lockout decision
-- atomic under concurrent guessing.
-- name: RecordLoginFailure :one
UPDATE users
SET failed_login_count = failed_login_count + 1,
    locked_until = CASE
        WHEN failed_login_count + 1 >= sqlc.arg('max_failures')::int
        THEN now() + make_interval(secs => sqlc.arg('lockout_seconds')::int)
        ELSE locked_until END,
    status = CASE
        WHEN failed_login_count + 1 >= sqlc.arg('max_failures')::int AND status = 'ACTIVE'
        THEN 'LOCKED' ELSE status END
WHERE id = sqlc.arg('id')
RETURNING failed_login_count, locked_until, status;

-- name: UnlockExpiredUserLocks :execrows
UPDATE users
SET status = 'ACTIVE', locked_until = NULL, failed_login_count = 0
WHERE status = 'LOCKED' AND locked_until IS NOT NULL AND locked_until < now();

-- name: CountOrganizationAdmins :one
SELECT count(DISTINCT u.id)
FROM users u
JOIN user_roles ur ON ur.user_id = u.id
JOIN roles r ON r.id = ur.role_id
WHERE u.organization_id = $1 AND u.status = 'ACTIVE' AND r.code IN ('ORG_ADMIN','SUPER_ADMIN');

-- ---------------------------------------------------------------------------
-- Roles and permissions
-- ---------------------------------------------------------------------------

-- name: ListPermissions :many
SELECT * FROM permissions ORDER BY module, code;

-- name: GetPermissionByCode :one
SELECT * FROM permissions WHERE code = $1;

-- name: ListRolesForOrganization :many
SELECT r.*, count(rp.permission_id) AS permission_count
FROM roles r
LEFT JOIN role_permissions rp ON rp.role_id = r.id
WHERE r.organization_id IS NULL OR r.organization_id = sqlc.arg('organization_id')
GROUP BY r.id
ORDER BY r.is_system DESC, r.code;

-- name: GetRoleByPublicID :one
SELECT * FROM roles
WHERE public_id = sqlc.arg('public_id')
  AND (organization_id IS NULL OR organization_id = sqlc.arg('organization_id'));

-- name: GetSystemRoleByCode :one
SELECT * FROM roles WHERE code = $1 AND organization_id IS NULL;

-- name: GetRoleByCodeForOrganization :one
SELECT * FROM roles
WHERE code = sqlc.arg('code')
  AND (organization_id IS NULL OR organization_id = sqlc.arg('organization_id'))
ORDER BY organization_id NULLS LAST
LIMIT 1;

-- name: CreateRole :one
INSERT INTO roles (public_id, organization_id, code, name, description, is_system, scope_required)
VALUES ($1,$2,$3,$4,$5,false,$6)
RETURNING *;

-- name: UpdateRole :one
UPDATE roles
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    scope_required = COALESCE(sqlc.narg('scope_required'), scope_required)
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id') AND NOT is_system
RETURNING *;

-- name: DeleteRole :execrows
DELETE FROM roles
WHERE id = sqlc.arg('id') AND organization_id = sqlc.arg('organization_id') AND NOT is_system;

-- name: CountRoleAssignments :one
SELECT count(*) FROM user_roles WHERE role_id = $1;

-- name: ListRolePermissionCodes :many
SELECT p.code FROM role_permissions rp
JOIN permissions p ON p.id = rp.permission_id
WHERE rp.role_id = $1
ORDER BY p.code;

-- name: ClearRolePermissions :exec
DELETE FROM role_permissions WHERE role_id = $1;

-- name: SetRolePermissions :execrows
INSERT INTO role_permissions (role_id, permission_id)
SELECT sqlc.arg('role_id'), p.id FROM permissions p
WHERE p.code = ANY(sqlc.arg('permission_codes')::text[])
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Role assignments and effective permissions
-- ---------------------------------------------------------------------------

-- GetUserAuthorization returns everything the authorization layer needs in one
-- round trip: the distinct permission codes the user holds, their role codes,
-- and the operating units their scoped grants cover.
--
-- A single query avoids an N+1 on the hottest path in the system (every
-- authenticated request) and keeps the result cacheable as one unit.
-- name: GetUserAuthorization :one
SELECT
    COALESCE(ARRAY(
        SELECT DISTINCT p.code
        FROM user_roles ur
        JOIN role_permissions rp ON rp.role_id = ur.role_id
        JOIN permissions p ON p.id = rp.permission_id
        WHERE ur.user_id = sqlc.arg('user_id')
        ORDER BY p.code
    ), '{}')::text[] AS permission_codes,
    COALESCE(ARRAY(
        SELECT DISTINCT r.code
        FROM user_roles ur JOIN roles r ON r.id = ur.role_id
        WHERE ur.user_id = sqlc.arg('user_id')
        ORDER BY r.code
    ), '{}')::text[] AS role_codes,
    COALESCE(ARRAY(
        SELECT DISTINCT ou.public_id
        FROM user_roles ur
        JOIN operating_units ou ON ou.id = ur.operating_unit_id
        WHERE ur.user_id = sqlc.arg('user_id') AND ur.operating_unit_id IS NOT NULL
        ORDER BY ou.public_id
    ), '{}')::text[] AS scoped_unit_public_ids,
    COALESCE(ARRAY(
        SELECT DISTINCT ur.operating_unit_id
        FROM user_roles ur
        WHERE ur.user_id = sqlc.arg('user_id') AND ur.operating_unit_id IS NOT NULL
    ), '{}')::bigint[] AS scoped_unit_ids,
    EXISTS (
        SELECT 1 FROM user_roles ur
        WHERE ur.user_id = sqlc.arg('user_id') AND ur.operating_unit_id IS NULL
    ) AS has_unscoped_role;

-- name: ListUserRoleAssignments :many
SELECT ur.id, ur.granted_at, r.public_id AS role_public_id, r.code AS role_code,
       r.name AS role_name, r.is_system, r.scope_required,
       ou.public_id AS operating_unit_public_id, ou.code AS operating_unit_code,
       ou.name AS operating_unit_name, ou.unit_type AS operating_unit_type,
       g.public_id AS granted_by_public_id, g.full_name AS granted_by_name
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
LEFT JOIN operating_units ou ON ou.id = ur.operating_unit_id
LEFT JOIN users g ON g.id = ur.granted_by
WHERE ur.user_id = $1
ORDER BY r.code, ou.code NULLS FIRST;

-- Resolve assignments for one tenant's current page without a query per user.
-- name: ListRoleAssignmentsForUsers :many
SELECT ur.user_id, ur.granted_at, r.public_id AS role_public_id,
       r.code AS role_code, r.name AS role_name,
       ou.public_id AS operating_unit_public_id, ou.code AS operating_unit_code,
       ou.name AS operating_unit_name, ou.unit_type AS operating_unit_type,
       g.full_name AS granted_by_name
FROM user_roles ur
JOIN users u ON u.id = ur.user_id AND u.organization_id = ur.organization_id
JOIN roles r ON r.id = ur.role_id
LEFT JOIN operating_units ou ON ou.id = ur.operating_unit_id
LEFT JOIN users g ON g.id = ur.granted_by
WHERE ur.organization_id = sqlc.arg('organization_id')
  AND ur.user_id = ANY(sqlc.arg('user_ids')::bigint[])
ORDER BY ur.user_id, r.code, ou.code NULLS FIRST;

-- name: AssignUserRole :one
INSERT INTO user_roles (organization_id, user_id, role_id, operating_unit_id, granted_by)
VALUES ($1,$2,$3,$4,$5)
RETURNING *;

-- name: RevokeUserRole :execrows
DELETE FROM user_roles
WHERE user_id = sqlc.arg('user_id')
  AND role_id = sqlc.arg('role_id')
  AND organization_id = sqlc.arg('organization_id')
  AND ((sqlc.narg('operating_unit_id')::bigint IS NULL AND operating_unit_id IS NULL)
       OR operating_unit_id = sqlc.narg('operating_unit_id'));

-- name: RevokeAllUserRoles :execrows
DELETE FROM user_roles WHERE user_id = $1;

-- ---------------------------------------------------------------------------
-- Sessions
-- ---------------------------------------------------------------------------

-- name: CreateSession :one
INSERT INTO sessions (
    public_id, organization_id, user_id, refresh_token_hash, chain_id,
    parent_session_id, rotation_counter, user_agent, client_ip, expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: GetSessionByRefreshHash :one
SELECT s.*, u.status AS user_status, u.is_super_admin, u.email AS user_email,
       o.status AS organization_status
FROM sessions s
JOIN users u ON u.id = s.user_id
JOIN organizations o ON o.id = s.organization_id
WHERE s.refresh_token_hash = $1;

-- name: GetSessionByPublicID :one
SELECT * FROM sessions WHERE public_id = $1;

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = now(), revoked_reason = sqlc.arg('reason')
WHERE id = sqlc.arg('id') AND revoked_at IS NULL;

-- RevokeSessionChain revokes every session descended from one login.
--
-- Used both on logout-all and on refresh-token reuse detection: if a token that
-- was already rotated is presented again, the chain has leaked and every
-- descendant must die immediately.
-- name: RevokeSessionChain :execrows
UPDATE sessions
SET revoked_at = now(), revoked_reason = sqlc.arg('reason')
WHERE chain_id = sqlc.arg('chain_id') AND revoked_at IS NULL;

-- name: RevokeUserSessions :execrows
UPDATE sessions
SET revoked_at = now(), revoked_reason = sqlc.arg('reason')
WHERE user_id = sqlc.arg('user_id') AND revoked_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = now() WHERE id = $1;

-- name: ListActiveSessionsForUser :many
SELECT * FROM sessions
WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
ORDER BY issued_at DESC;

-- name: PurgeExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < now() - interval '30 days';

-- ---------------------------------------------------------------------------
-- Password reset
-- ---------------------------------------------------------------------------

-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at, request_ip)
VALUES ($1,$2,$3,$4)
RETURNING *;

-- ConsumePasswordResetToken atomically marks a token used.
--
-- `used_at IS NULL` in the WHERE clause is the single-use guarantee: two
-- concurrent redemptions of the same token produce one updated row and one
-- empty result, with no race window.
-- name: ConsumePasswordResetToken :one
UPDATE password_reset_tokens
SET used_at = now()
WHERE token_hash = sqlc.arg('token_hash') AND used_at IS NULL AND expires_at > now()
RETURNING *;

-- name: InvalidateUserResetTokens :execrows
UPDATE password_reset_tokens SET used_at = now()
WHERE user_id = $1 AND used_at IS NULL;

-- name: PurgeExpiredResetTokens :execrows
DELETE FROM password_reset_tokens WHERE expires_at < now() - interval '7 days';

-- name: ListSessionPublicIDsByChain :many
SELECT public_id FROM sessions WHERE chain_id = $1;

-- name: ListSessionPublicIDsForUser :many
SELECT public_id FROM sessions WHERE user_id = $1;

-- name: ListUserIDsWithRole :many
SELECT DISTINCT user_id FROM user_roles WHERE role_id = $1;
