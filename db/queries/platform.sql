-- Platform runtime queries: idempotency ledger, background jobs, audit trail.

-- ---------------------------------------------------------------------------
-- Idempotency
-- ---------------------------------------------------------------------------

-- ClaimIdempotencyKey attempts to reserve a key.
--
-- ON CONFLICT DO NOTHING makes the reservation atomic: exactly one concurrent
-- caller gets a row back and proceeds, everyone else gets zero rows and must
-- read the existing record. This is the whole concurrency control for retried
-- bookings — no advisory lock, no Redis.
-- name: ClaimIdempotencyKey :one
INSERT INTO idempotency_keys (
    organization_id, endpoint, idempotency_key, request_hash, user_id, request_id, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (organization_id, endpoint, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys
WHERE organization_id = $1 AND endpoint = $2 AND idempotency_key = $3;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys
SET status = 'COMPLETED',
    response_status = $4,
    response_body = $5,
    resource_type = $6,
    resource_public_id = $7,
    completed_at = now()
WHERE organization_id = $1 AND endpoint = $2 AND idempotency_key = $3;

-- FailIdempotencyKey releases a reservation so the client may retry with the
-- same key after a transient failure.
-- name: FailIdempotencyKey :exec
DELETE FROM idempotency_keys
WHERE organization_id = $1 AND endpoint = $2 AND idempotency_key = $3 AND status = 'IN_PROGRESS';

-- name: PurgeExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys WHERE expires_at < now();

-- ---------------------------------------------------------------------------
-- Jobs
-- ---------------------------------------------------------------------------

-- name: EnqueueJob :one
INSERT INTO jobs (
    public_id, organization_id, queue, job_type, payload, priority, run_at, max_attempts, dedupe_key, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('PENDING','RUNNING') DO NOTHING
RETURNING *;

-- ClaimJobs takes ownership of up to $3 runnable jobs.
--
-- FOR UPDATE SKIP LOCKED lets several workers claim disjoint batches without
-- blocking each other; the UPDATE and the SELECT are one statement so a claimed
-- job can never be handed to two workers.
-- name: ClaimJobs :many
WITH claimed AS (
    SELECT id FROM jobs
    WHERE status = 'PENDING'
      AND run_at <= now()
      AND queue = ANY(@queues::text[])
    ORDER BY priority ASC, run_at ASC, id ASC
    LIMIT @max_jobs
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs j
SET status = 'RUNNING',
    attempts = j.attempts + 1,
    locked_at = now(),
    locked_by = @worker_id,
    started_at = COALESCE(j.started_at, now())
FROM claimed c
WHERE j.id = c.id
RETURNING j.*;

-- name: CompleteJob :exec
UPDATE jobs
SET status = 'SUCCEEDED', completed_at = now(), locked_at = NULL, locked_by = NULL
WHERE id = $1;

-- RetryJob reschedules a failed attempt, or moves the job to the dead-letter
-- state once max_attempts is exhausted.
-- name: RetryJob :one
UPDATE jobs
SET status = CASE WHEN attempts >= max_attempts THEN 'DEAD' ELSE 'PENDING' END,
    run_at = CASE WHEN attempts >= max_attempts THEN run_at ELSE now() + make_interval(secs => @backoff_seconds::int) END,
    last_error = @last_error,
    error_count = error_count + 1,
    locked_at = NULL,
    locked_by = NULL,
    completed_at = CASE WHEN attempts >= max_attempts THEN now() ELSE NULL END
WHERE id = @job_id
RETURNING status;

-- ReleaseStuckJobs recovers jobs whose worker died mid-execution.
-- name: ReleaseStuckJobs :execrows
UPDATE jobs
SET status = 'PENDING', locked_at = NULL, locked_by = NULL,
    last_error = 'worker lock expired; job requeued'
WHERE status = 'RUNNING' AND locked_at < now() - make_interval(secs => @stuck_after_seconds::int);

-- name: GetJobByPublicID :one
SELECT * FROM jobs WHERE public_id = $1;

-- name: CountJobsByQueue :many
SELECT queue, count(*) AS pending FROM jobs
WHERE status = 'PENDING' AND run_at <= now()
GROUP BY queue;

-- ---------------------------------------------------------------------------
-- Audit
-- ---------------------------------------------------------------------------

-- name: InsertAuditEvent :one
INSERT INTO audit_events (
    public_id, organization_id, actor_user_id, actor_type, actor_label, action,
    resource_type, resource_id, resource_public_id, operating_unit_id,
    request_id, client_ip, reason, before_state, after_state, metadata, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16, COALESCE($17, now()))
RETURNING id, public_id, occurred_at;

-- name: ListAuditEvents :many
SELECT a.*, u.email AS actor_email, u.full_name AS actor_name
FROM audit_events a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE a.organization_id = sqlc.narg('organization_id')
  AND (sqlc.narg('action')::text IS NULL OR a.action = sqlc.narg('action'))
  AND (sqlc.narg('resource_type')::text IS NULL OR a.resource_type = sqlc.narg('resource_type'))
  AND (sqlc.narg('resource_public_id')::text IS NULL OR a.resource_public_id = sqlc.narg('resource_public_id'))
  AND (sqlc.narg('actor_user_id')::bigint IS NULL OR a.actor_user_id = sqlc.narg('actor_user_id'))
  AND (sqlc.narg('occurred_from')::timestamptz IS NULL OR a.occurred_at >= sqlc.narg('occurred_from'))
  AND (sqlc.narg('occurred_to')::timestamptz IS NULL OR a.occurred_at < sqlc.narg('occurred_to'))
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR a.id < sqlc.narg('cursor_id'))
ORDER BY a.occurred_at DESC, a.id DESC
LIMIT sqlc.arg('row_limit');

-- ---------------------------------------------------------------------------
-- Health
-- ---------------------------------------------------------------------------

-- name: Heartbeat :one
SELECT 1::int AS ok;

-- TakeOverStaleIdempotencyKey reclaims a reservation abandoned by a crashed
-- process.
--
-- This is only safe because the completion write happens inside the same
-- transaction as the business work: if a key is still IN_PROGRESS, the work it
-- guarded provably did not commit, so re-running it cannot duplicate anything.
-- name: TakeOverStaleIdempotencyKey :one
UPDATE idempotency_keys
SET created_at = now(), request_id = sqlc.narg('request_id'), user_id = sqlc.narg('user_id')
WHERE organization_id = sqlc.arg('organization_id')
  AND endpoint = sqlc.arg('endpoint')
  AND idempotency_key = sqlc.arg('idempotency_key')
  AND status = 'IN_PROGRESS'
  AND created_at < now() - make_interval(secs => sqlc.arg('stale_after_seconds')::int)
RETURNING *;
