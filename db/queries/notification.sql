-- Notifications (M26).
--
-- Nothing here blocks a business transaction. Raising a notification is one
-- INSERT plus a job enqueue; everything else runs in the worker.

-- ---------------------------------------------------------------------------
-- Templates
-- ---------------------------------------------------------------------------

-- name: CreateNotificationTemplate :one
INSERT INTO notification_templates (
    public_id, organization_id, code, name, event_type, channel, locale,
    subject, body, variables, provider_ref, metadata, is_active, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: ResolveTemplate :one
-- The template for an event on a channel, preferring the requested locale and
-- falling back to the tenant default. A missing template suppresses the
-- notification rather than failing the event that raised it.
SELECT * FROM notification_templates
WHERE organization_id = $1 AND event_type = $2 AND channel = $3 AND is_active
  AND locale IN (sqlc.arg('locale')::text, 'en')
ORDER BY (locale = sqlc.arg('locale')::text) DESC
LIMIT 1;

-- name: GetTemplateByCode :one
SELECT * FROM notification_templates WHERE organization_id = $1 AND code = $2;

-- name: GetTemplateByPublicID :one
SELECT * FROM notification_templates WHERE organization_id = $1 AND public_id = $2;

-- name: ListNotificationTemplates :many
SELECT * FROM notification_templates
WHERE organization_id = $1
  AND (sqlc.narg('event_type')::text IS NULL OR event_type = sqlc.narg('event_type')::text)
  AND (sqlc.narg('channel')::text IS NULL OR channel = sqlc.narg('channel')::text)
ORDER BY event_type, channel, locale
LIMIT $2 OFFSET $3;

-- name: UpdateNotificationTemplate :one
UPDATE notification_templates
   SET name = COALESCE(sqlc.narg('name')::text, name),
       provider_ref = COALESCE(sqlc.narg('provider_ref')::text, provider_ref),
       subject = COALESCE(sqlc.narg('subject')::text, subject),
       body = COALESCE(sqlc.narg('body')::text, body),
       variables = COALESCE(sqlc.narg('variables')::jsonb, variables),
       is_active = COALESCE(sqlc.narg('is_active')::boolean, is_active)
 WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: SeedDefaultNotificationTemplates :exec
-- Give a newly created organization a working template set.
--
-- A tenant with no templates suppresses every notification, which is
-- indistinguishable from "notifications are broken". The list matches what
-- migration 0026 applied to organizations that already existed.
INSERT INTO notification_templates (public_id, organization_id, code, name, event_type,
                                    channel, locale, subject, body, variables)
SELECT gen_seed_public_id('ntt'), sqlc.arg('organization_id'), d.code, d.name,
       d.event_type, d.channel, 'en', d.subject, d.body, d.variables::jsonb
FROM (VALUES
    ('SHIPMENT_BOOKED_SMS', 'Shipment booked (SMS)', 'SHIPMENT_BOOKED', 'SMS', NULL,
     'Your shipment {{awb}} has been booked with {{organizationName}}. Track it at {{trackingUrl}}',
     '["awb","organizationName","trackingUrl"]'),
    ('SHIPMENT_BOOKED_EMAIL', 'Shipment booked (email)', 'SHIPMENT_BOOKED', 'EMAIL',
     'Your shipment {{awb}} is booked',
     'Hello {{recipientName}},

Your shipment {{awb}} has been booked and is on its way from {{originCity}} to {{destinationCity}}.

Track it any time at {{trackingUrl}}.

{{organizationName}}',
     '["awb","recipientName","originCity","destinationCity","trackingUrl","organizationName"]'),
    ('PICKUP_SCHEDULED_SMS', 'Pickup scheduled (SMS)', 'PICKUP_SCHEDULED', 'SMS', NULL,
     'Pickup for {{awb}} is scheduled for {{scheduledDate}}.',
     '["awb","scheduledDate"]'),
    ('SHIPMENT_IN_TRANSIT_SMS', 'In transit (SMS)', 'SHIPMENT_IN_TRANSIT', 'SMS', NULL,
     'Your shipment {{awb}} is in transit to {{destinationCity}}.',
     '["awb","destinationCity"]'),
    ('SHIPMENT_OUT_FOR_DELIVERY_SMS', 'Out for delivery (SMS)', 'SHIPMENT_OUT_FOR_DELIVERY', 'SMS', NULL,
     'Your shipment {{awb}} is out for delivery today.{{codLine}}',
     '["awb","codLine"]'),
    ('SHIPMENT_DELIVERED_SMS', 'Delivered (SMS)', 'SHIPMENT_DELIVERED', 'SMS', NULL,
     'Your shipment {{awb}} was delivered on {{deliveredAt}}. Received by {{receivedBy}}.',
     '["awb","deliveredAt","receivedBy"]'),
    ('SHIPMENT_DELIVERED_EMAIL', 'Delivered (email)', 'SHIPMENT_DELIVERED', 'EMAIL',
     'Delivered: {{awb}}',
     'Hello {{recipientName}},

Your shipment {{awb}} was delivered on {{deliveredAt}} and received by {{receivedBy}}.

{{organizationName}}',
     '["awb","recipientName","deliveredAt","receivedBy","organizationName"]'),
    ('SHIPMENT_NDR_SMS', 'Delivery failed (SMS)', 'SHIPMENT_NDR', 'SMS', NULL,
     'We could not deliver {{awb}}: {{reason}}. We will try again on {{nextAttemptDate}}.',
     '["awb","reason","nextAttemptDate"]'),
    ('SHIPMENT_RTO_SMS', 'Return to origin (SMS)', 'SHIPMENT_RTO', 'SMS', NULL,
     'Shipment {{awb}} is being returned to the sender. Reason: {{reason}}.',
     '["awb","reason"]'),
    ('DELIVERY_OTP_SMS', 'Delivery OTP (SMS)', 'DELIVERY_OTP', 'SMS', NULL,
     'Your delivery OTP for {{awb}} is {{otp}}. Share it with the delivery agent only.',
     '["awb","otp"]'),
    ('INVOICE_ISSUED_EMAIL', 'Invoice issued (email)', 'INVOICE_ISSUED', 'EMAIL',
     'Invoice {{invoiceNumber}} from {{organizationName}}',
     'Hello {{customerName}},

Invoice {{invoiceNumber}} for {{amount}} is now due on {{dueDate}}.

{{organizationName}}',
     '["invoiceNumber","customerName","amount","dueDate","organizationName"]'),
    ('SETTLEMENT_APPROVED_EMAIL', 'Settlement approved (email)', 'SETTLEMENT_APPROVED', 'EMAIL',
     'Settlement {{settlementNumber}} approved',
     'Hello {{franchiseName}},

Your settlement {{settlementNumber}} for {{periodStart}} to {{periodEnd}} has been approved. Net amount: {{netAmount}}.

{{organizationName}}',
     '["settlementNumber","franchiseName","periodStart","periodEnd","netAmount","organizationName"]')
) AS d(code, name, event_type, channel, subject, body, variables)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Preferences and subscriptions
-- ---------------------------------------------------------------------------

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preferences (
    public_id, organization_id, party_type, party_id, party_key,
    channel, event_type, enabled, locale, quiet_from, quiet_to
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (organization_id, party_type, party_id, channel, COALESCE(event_type, ''))
WHERE party_id IS NOT NULL
DO UPDATE SET enabled = EXCLUDED.enabled, locale = EXCLUDED.locale,
              quiet_from = EXCLUDED.quiet_from, quiet_to = EXCLUDED.quiet_to
RETURNING *;

-- name: FindPreference :one
-- The most specific preference wins: an event-specific row beats a blanket one.
SELECT * FROM notification_preferences
WHERE organization_id = $1 AND party_type = $2 AND channel = $3
  AND ((sqlc.narg('party_id')::bigint IS NOT NULL AND party_id = sqlc.narg('party_id')::bigint)
       OR (sqlc.narg('party_key')::text IS NOT NULL AND party_key = sqlc.narg('party_key')::text))
  AND (event_type IS NULL OR event_type = sqlc.arg('event_type'))
ORDER BY (event_type IS NOT NULL) DESC
LIMIT 1;

-- name: ListPreferences :many
SELECT * FROM notification_preferences
WHERE organization_id = $1 AND party_type = $2 AND party_id = $3
ORDER BY channel, event_type NULLS FIRST;

-- name: CreateSubscription :one
INSERT INTO notification_subscriptions (
    public_id, organization_id, party_type, party_id, event_type, channels, scope, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (organization_id, party_type, party_id, event_type)
DO UPDATE SET channels = EXCLUDED.channels, scope = EXCLUDED.scope, is_active = true
RETURNING *;

-- name: ListSubscribers :many
-- Everyone who has asked for this event.
SELECT * FROM notification_subscriptions
WHERE organization_id = $1 AND event_type = $2 AND is_active;

-- name: ListSubscriptionsForParty :many
SELECT * FROM notification_subscriptions
WHERE organization_id = $1 AND party_type = $2 AND party_id = $3
ORDER BY event_type;

-- ---------------------------------------------------------------------------
-- Notifications
-- ---------------------------------------------------------------------------

-- name: CreateNotification :one
--
-- next_attempt_at defaults to the database's now(). The application clock and
-- the database clock are different clocks, and ClaimDueNotifications compares
-- against the database's — scheduling from the app's makes a message that
-- should go out immediately wait for the next poll whenever the two drift.
INSERT INTO notifications (
    public_id, organization_id, event_type, channel, template_id, template_code, locale,
    recipient_type, recipient_id, recipient_address, recipient_name,
    subject, body, variables, shipment_id, invoice_id, settlement_id, reference,
    status, suppressed_reason, max_attempts, next_attempt_at, dedupe_key, request_id,
    provider_ref
) VALUES (
    sqlc.arg('public_id'), sqlc.arg('organization_id'), sqlc.arg('event_type'),
    sqlc.arg('channel'), sqlc.narg('template_id'), sqlc.narg('template_code'),
    sqlc.arg('locale'), sqlc.arg('recipient_type'), sqlc.narg('recipient_id'),
    sqlc.arg('recipient_address'), sqlc.narg('recipient_name'),
    sqlc.narg('subject'), sqlc.arg('body'), sqlc.arg('variables'),
    sqlc.narg('shipment_id'), sqlc.narg('invoice_id'), sqlc.narg('settlement_id'),
    sqlc.narg('reference'), sqlc.arg('status'), sqlc.narg('suppressed_reason'),
    sqlc.arg('max_attempts'),
    -- Only a message that will actually be sent carries a schedule; a
    -- suppressed one keeps next_attempt_at null, because there is no attempt.
    CASE WHEN sqlc.arg('status')::text = 'PENDING'
         THEN COALESCE(sqlc.narg('next_attempt_at')::timestamptz, now())
         ELSE sqlc.narg('next_attempt_at')::timestamptz END,
    sqlc.narg('dedupe_key'), sqlc.narg('request_id'),
    sqlc.narg('provider_ref')
)
RETURNING *;

-- name: FindNotificationByDedupe :one
SELECT * FROM notifications
WHERE organization_id = $1 AND dedupe_key = $2
  AND status NOT IN ('FAILED','DEAD_LETTER','CANCELLED');

-- name: GetNotificationByPublicID :one
SELECT n.*, s.awb
FROM notifications n
LEFT JOIN shipments s ON s.id = n.shipment_id
WHERE n.organization_id = $1 AND n.public_id = $2;

-- name: GetNotificationByID :one
SELECT * FROM notifications WHERE organization_id = $1 AND id = $2;

-- name: LockNotificationForSend :one
-- Claims a notification for exactly one worker.
--
-- The claim is PENDING -> SENDING and nothing else. It deliberately does NOT
-- accept a row already in SENDING: doing so would let a second worker pick up a
-- message the first is still delivering, and the customer would be told twice.
-- The integration test TestConcurrentDeliverySendsOnce pins this down.
--
-- A worker that crashes mid-send leaves a row stuck in SENDING. That is
-- recovered by ReclaimStuckNotifications below, on a staleness window, rather
-- than by weakening this claim.
UPDATE notifications
   SET status = 'SENDING', attempt_count = attempt_count + 1
 WHERE organization_id = $1 AND id = $2
   AND status = 'PENDING'
   AND attempt_count < max_attempts
RETURNING *;

-- name: ReclaimStuckNotifications :many
-- Returns notifications abandoned in SENDING by a worker that died, so they can
-- be retried. The window must be comfortably longer than the per-attempt
-- timeout, or a slow provider would be treated as a crash and double-sent.
--
-- organization_id is optional so the platform maintenance sweep can recover
-- every tenant in one statement; a single-tenant caller still passes it.
UPDATE notifications
   SET status = 'PENDING'
 WHERE (sqlc.narg('organization_id')::bigint IS NULL
        OR organization_id = sqlc.narg('organization_id')::bigint)
   AND status = 'SENDING'
   AND updated_at < now() - sqlc.arg('stale_after')::interval
   AND attempt_count < max_attempts
RETURNING *;

-- name: MarkNotificationSent :one
UPDATE notifications
   SET status = 'SENT', sent_at = now(), provider = sqlc.arg('provider'),
       provider_message_id = sqlc.narg('provider_message_id'),
       next_attempt_at = NULL, last_error = NULL
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: MarkNotificationDelivered :one
-- Set by a provider status callback, where one exists.
UPDATE notifications
   SET status = 'DELIVERED', delivered_at = now()
 WHERE organization_id = $1 AND id = $2 AND status IN ('SENT','DELIVERED')
RETURNING *;

-- name: MarkNotificationFailed :one
-- Retryable failures go back to PENDING with a later next_attempt_at; a
-- permanent failure or an exhausted attempt budget goes to DEAD_LETTER, which
-- is where an operator looks for messages that never arrived.
UPDATE notifications
   SET status = CASE
           WHEN sqlc.arg('retryable')::boolean AND attempt_count < max_attempts THEN 'PENDING'
           ELSE 'DEAD_LETTER'
       END,
       last_error = sqlc.arg('error_message'),
       failed_at = now(),
       next_attempt_at = CASE
           WHEN sqlc.arg('retryable')::boolean AND attempt_count < max_attempts
           THEN now() + sqlc.arg('retry_after')::interval
           ELSE NULL
       END
 WHERE organization_id = sqlc.arg('organization_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: RetryNotification :one
-- Operator-initiated retry of a dead-lettered message. Resets the budget,
-- because the operator has presumably fixed whatever was wrong.
UPDATE notifications
   SET status = 'PENDING', attempt_count = 0, next_attempt_at = now(),
       last_error = NULL, failed_at = NULL
 WHERE organization_id = $1 AND id = $2 AND status IN ('DEAD_LETTER','FAILED')
RETURNING *;

-- name: CancelNotification :one
UPDATE notifications
   SET status = 'CANCELLED', next_attempt_at = NULL
 WHERE organization_id = $1 AND id = $2 AND status IN ('PENDING','DEAD_LETTER','FAILED')
RETURNING *;

-- name: ListDueNotifications :many
-- The safety net for a notification whose send job never made it onto the
-- queue.
--
-- Raise() logs and continues when the enqueue fails, because a queue problem
-- must not roll back the shipment that caused the message. The cost of that
-- choice is a row nobody will ever pick up, and this is what finds it: the
-- maintenance sweep re-enqueues whatever it returns, and the job dedupe key
-- makes a row that already has a live job a no-op.
--
-- Not a claim, despite what an earlier name suggested. The claim is the
-- compare-and-swap in LockNotificationForSend and nowhere else.
SELECT id, organization_id, public_id FROM notifications
WHERE (sqlc.narg('organization_id')::bigint IS NULL
       OR organization_id = sqlc.narg('organization_id')::bigint)
  AND status = 'PENDING'
  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
  AND attempt_count < max_attempts
ORDER BY next_attempt_at NULLS FIRST, id
LIMIT sqlc.arg('row_limit');

-- name: ListNotifications :many
SELECT n.*, s.awb
FROM notifications n
LEFT JOIN shipments s ON s.id = n.shipment_id
WHERE n.organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR n.status = sqlc.narg('status')::text)
  AND (sqlc.narg('channel')::text IS NULL OR n.channel = sqlc.narg('channel')::text)
  AND (sqlc.narg('event_type')::text IS NULL OR n.event_type = sqlc.narg('event_type')::text)
  AND (sqlc.narg('shipment_id')::bigint IS NULL OR n.shipment_id = sqlc.narg('shipment_id')::bigint)
  AND (sqlc.narg('recipient_id')::bigint IS NULL OR n.recipient_id = sqlc.narg('recipient_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR n.id < sqlc.narg('cursor_id')::bigint)
ORDER BY n.id DESC
LIMIT $2;

-- name: CountNotificationsByStatus :many
-- The notification health tile: what is queued, sent and dead-lettered.
SELECT status, count(*)::bigint AS count
FROM notifications
WHERE organization_id = $1
  AND created_at >= sqlc.arg('since')::timestamptz
GROUP BY status;

-- ---------------------------------------------------------------------------
-- Attempts
-- ---------------------------------------------------------------------------

-- name: CreateNotificationAttempt :one
INSERT INTO notification_attempts (
    public_id, organization_id, notification_id, attempt_no, provider, outcome,
    provider_message_id, provider_status, error_message, retryable, duration_ms
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: ListNotificationAttempts :many
SELECT * FROM notification_attempts
WHERE notification_id = $1
ORDER BY attempt_no;
