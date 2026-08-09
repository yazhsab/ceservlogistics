-- Partner API keys and webhooks (M32).

-- ---------------------------------------------------------------------------
-- API keys
-- ---------------------------------------------------------------------------

-- name: CreateAPIKey :one
INSERT INTO api_keys (
    public_id, organization_id, name, key_id, secret_hash, secret_hint,
    scopes, allowed_cidrs, expires_at, rate_limit_per_minute, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: FindAPIKeyByKeyID :one
-- The authentication lookup. key_id is globally unique because it arrives
-- before the tenant is known — it is what identifies the organization.
--
-- Returns the row whatever its status: the caller must distinguish "no such
-- key" from "revoked key" to produce a useful error, and both take the same
-- time so the difference is not observable by timing.
SELECT k.*, o.public_id AS organization_public_id, o.code AS organization_code,
       o.currency AS organization_currency, o.timezone AS organization_timezone,
       o.awb_prefix AS organization_awb_prefix
FROM api_keys k
JOIN organizations o ON o.id = k.organization_id
WHERE k.key_id = $1;

-- name: GetAPIKeyByPublicID :one
SELECT * FROM api_keys WHERE organization_id = $1 AND public_id = $2;

-- name: ListAPIKeys :many
-- Never returns secret_hash. The hint is enough to tell two keys apart.
SELECT id, public_id, organization_id, name, key_id, secret_hint, scopes,
       allowed_cidrs, status, expires_at, last_used_at, last_used_ip,
       request_count, rate_limit_per_minute, revoked_at, revoke_reason,
       created_by, created_at, updated_at,
       -- A window count rather than a second query: an offset page without a
       -- total leaves a client requesting the maximum and guessing whether
       -- there is more.
       count(*) OVER () AS total_count
FROM api_keys
WHERE organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: RevokeAPIKey :one
-- One-way. The api_keys_guard trigger refuses to bring a revoked key back:
-- a leaked credential must never be reinstated.
UPDATE api_keys
   SET status = 'REVOKED', revoked_at = now(), revoked_by = $3, revoke_reason = $4
 WHERE organization_id = $1 AND id = $2 AND status <> 'REVOKED'
RETURNING *;

-- name: SuspendAPIKey :one
UPDATE api_keys
   SET status = CASE WHEN sqlc.arg('suspend')::boolean THEN 'SUSPENDED' ELSE 'ACTIVE' END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
   AND status IN ('ACTIVE','SUSPENDED')
RETURNING *;

-- name: TouchAPIKey :exec
-- Usage tracking, deliberately fire-and-forget and outside the request's
-- transaction: a hot partner key must not serialise every request on one row,
-- and a failed update here must never fail the request it describes.
UPDATE api_keys
   SET last_used_at = now(), last_used_ip = sqlc.narg('client_ip')::inet,
       request_count = request_count + 1
 WHERE id = sqlc.arg('id');

-- name: ExpireAPIKeys :exec
-- Run by the worker. An expired key stops working whether or not anyone
-- remembered to revoke it.
UPDATE api_keys
   SET status = 'EXPIRED'
 WHERE status = 'ACTIVE' AND expires_at IS NOT NULL AND expires_at < now();

-- name: RecordAPIKeyRequest :exec
INSERT INTO api_key_requests (
    organization_id, api_key_id, method, route, status_code, duration_ms,
    request_id, error_code, client_ip
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9);

-- name: ListAPIKeyRequests :many
SELECT * FROM api_key_requests
WHERE organization_id = $1 AND api_key_id = $2
ORDER BY occurred_at DESC
LIMIT $3;

-- name: SummariseAPIKeyUsage :one
SELECT count(*)::bigint AS request_count,
       count(*) FILTER (WHERE status_code >= 400)::bigint AS error_count,
       COALESCE(AVG(duration_ms), 0)::int AS avg_duration_ms
FROM api_key_requests
WHERE api_key_id = $1 AND occurred_at >= sqlc.arg('since')::timestamptz;

-- ---------------------------------------------------------------------------
-- Webhook endpoints
-- ---------------------------------------------------------------------------

-- name: CreateWebhookEndpoint :one
INSERT INTO webhook_endpoints (
    public_id, organization_id, name, url, signing_secret,
    max_attempts, timeout_seconds, api_key_id, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetWebhookEndpointByPublicID :one
SELECT * FROM webhook_endpoints WHERE organization_id = $1 AND public_id = $2;

-- name: GetWebhookEndpointByID :one
SELECT * FROM webhook_endpoints WHERE id = $1;

-- name: ListWebhookEndpoints :many
SELECT id, public_id, organization_id, name, url, status,
       consecutive_failures, disabled_reason, last_success_at, last_failure_at,
       max_attempts, timeout_seconds, created_at, updated_at,
       count(*) OVER () AS total_count
FROM webhook_endpoints
WHERE organization_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: SetWebhookEndpointStatus :one
UPDATE webhook_endpoints
   SET status = sqlc.arg('status'), disabled_reason = sqlc.narg('disabled_reason'),
       consecutive_failures = CASE WHEN sqlc.arg('status')::text = 'ACTIVE' THEN 0
                                   ELSE consecutive_failures END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: RecordWebhookSuccess :exec
UPDATE webhook_endpoints
   SET consecutive_failures = 0, last_success_at = now()
 WHERE id = $1;

-- name: RecordWebhookFailure :one
-- Counts consecutive failures and auto-pauses a persistently dead endpoint, so
-- one broken partner cannot fill the queue with retries forever.
UPDATE webhook_endpoints
   SET consecutive_failures = consecutive_failures + 1,
       last_failure_at = now(),
       status = CASE WHEN consecutive_failures + 1 >= sqlc.arg('pause_after')::int
                     THEN 'PAUSED' ELSE status END,
       disabled_reason = CASE WHEN consecutive_failures + 1 >= sqlc.arg('pause_after')::int
                              THEN 'paused automatically after repeated delivery failures'
                              ELSE disabled_reason END
 WHERE id = sqlc.arg('id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Subscriptions
-- ---------------------------------------------------------------------------

-- name: CreateWebhookSubscription :one
INSERT INTO webhook_subscriptions (
    public_id, organization_id, endpoint_id, event_type, filter
) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (endpoint_id, event_type)
DO UPDATE SET filter = EXCLUDED.filter, is_active = true
RETURNING *;

-- name: ListEndpointsForEvent :many
-- Every active endpoint that has asked for this event. The fan-out query.
SELECT e.*
FROM webhook_endpoints e
JOIN webhook_subscriptions s ON s.endpoint_id = e.id
WHERE e.organization_id = $1
  AND e.status = 'ACTIVE'
  AND s.event_type = $2
  AND s.is_active;

-- name: ListWebhookSubscriptions :many
SELECT * FROM webhook_subscriptions WHERE endpoint_id = $1 ORDER BY event_type;

-- name: DeleteWebhookSubscription :exec
DELETE FROM webhook_subscriptions
WHERE organization_id = $1 AND endpoint_id = $2 AND event_type = $3;

-- ---------------------------------------------------------------------------
-- Deliveries
-- ---------------------------------------------------------------------------

-- name: CreateWebhookDelivery :one
-- next_attempt_at defaults to the database's now(), not the application's.
-- The two are different clocks: a delivery scheduled from the app's clock is
-- compared against the database's in ClaimDeliveryForSend, and a few hundred
-- milliseconds of skew is enough to make a webhook that should go out
-- immediately sit in the queue until the next poll.
INSERT INTO webhook_deliveries (
    public_id, organization_id, endpoint_id, event_type, event_id, payload,
    next_attempt_at, shipment_id, replay_of_id
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('endpoint_id'),
    sqlc.arg('event_type'), sqlc.arg('event_id'), sqlc.arg('payload'),
    COALESCE(sqlc.narg('next_attempt_at')::timestamptz, now()),
    sqlc.narg('shipment_id'), sqlc.narg('replay_of_id')
)
RETURNING *;

-- name: FindDeliveryByEvent :one
-- The dedupe probe, mirroring webhook_deliveries_event_idx.
SELECT * FROM webhook_deliveries
WHERE endpoint_id = $1 AND event_id = $2 AND replay_of_id IS NULL;

-- name: GetDeliveryByPublicID :one
SELECT d.*, e.url, e.name AS endpoint_name
FROM webhook_deliveries d
JOIN webhook_endpoints e ON e.id = d.endpoint_id
WHERE d.organization_id = $1 AND d.public_id = $2;

-- name: ClaimDeliveryForSend :one
-- PENDING -> SENDING only. Never accepts a row already SENDING: that would let
-- a second worker deliver a webhook the first is still sending, and a consumer
-- would see the same event twice from one delivery. A crashed worker's row is
-- recovered by ReclaimStuckDeliveries on a staleness window.
UPDATE webhook_deliveries
   SET status = 'SENDING', attempt_count = attempt_count + 1
 WHERE organization_id = $1 AND id = $2
   AND status = 'PENDING'
RETURNING *;

-- name: ListDueWebhookDeliveries :many
-- The webhook twin of ListDueNotifications: a delivery row whose send job was
-- never enqueued would otherwise sit PENDING forever, and a partner would be
-- missing an event nobody knew about.
SELECT id, organization_id, public_id FROM webhook_deliveries
WHERE (sqlc.narg('organization_id')::bigint IS NULL
       OR organization_id = sqlc.narg('organization_id')::bigint)
  AND status = 'PENDING'
  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
ORDER BY next_attempt_at NULLS FIRST, id
LIMIT sqlc.arg('row_limit');

-- name: ReclaimStuckDeliveries :many
-- The counterpart to ClaimDeliveryForSend, which accepts PENDING and nothing
-- else: a worker that dies mid-send would otherwise strand its row in SENDING
-- forever. organization_id is optional so the maintenance sweep covers every
-- tenant at once.
UPDATE webhook_deliveries
   SET status = 'PENDING'
 WHERE (sqlc.narg('organization_id')::bigint IS NULL
        OR organization_id = sqlc.narg('organization_id')::bigint)
   AND status = 'SENDING'
   AND updated_at < now() - sqlc.arg('stale_after')::interval
RETURNING *;

-- name: MarkDeliverySucceeded :one
UPDATE webhook_deliveries
   SET status = 'DELIVERED', delivered_at = now(),
       last_status_code = sqlc.arg('status_code'), next_attempt_at = NULL,
       last_error = NULL
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: MarkDeliveryFailed :one
UPDATE webhook_deliveries
   SET status = CASE
           WHEN attempt_count < sqlc.arg('max_attempts')::int THEN 'PENDING'
           ELSE 'DEAD_LETTER'
       END,
       last_status_code = sqlc.narg('status_code'),
       last_error = sqlc.arg('error_message'),
       -- Scheduled forward from the database clock, for the same reason
       -- CreateWebhookDelivery is: the caller supplies how long to wait, not
       -- when to wake up.
       next_attempt_at = CASE
           WHEN attempt_count < sqlc.arg('max_attempts')::int
           THEN now() + sqlc.arg('retry_after')::interval
           ELSE NULL
       END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: ListWebhookDeliveries :many
SELECT d.*, e.name AS endpoint_name
FROM webhook_deliveries d
JOIN webhook_endpoints e ON e.id = d.endpoint_id
WHERE d.organization_id = $1
  AND (sqlc.narg('endpoint_id')::bigint IS NULL OR d.endpoint_id = sqlc.narg('endpoint_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR d.status = sqlc.narg('status')::text)
  AND (sqlc.narg('event_type')::text IS NULL OR d.event_type = sqlc.narg('event_type')::text)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR d.id < sqlc.narg('cursor_id')::bigint)
ORDER BY d.id DESC
LIMIT $2;

-- name: CountDeliveriesByStatus :many
SELECT status, count(*)::bigint AS count
FROM webhook_deliveries
WHERE organization_id = $1 AND created_at >= sqlc.arg('since')::timestamptz
GROUP BY status;

-- name: CreateWebhookAttempt :one
INSERT INTO webhook_attempts (
    organization_id, delivery_id, attempt_no, status_code,
    response_body, error_message, duration_ms
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: ListWebhookAttempts :many
SELECT * FROM webhook_attempts WHERE delivery_id = $1 ORDER BY attempt_no;
