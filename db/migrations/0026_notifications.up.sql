-- 0026 Notifications (M26).
--
-- The governing rule is at the top of the module spec and is worth restating
-- here because the schema is shaped around it:
--
--   "Shipment success must not depend on notification provider availability."
--
-- So nothing in this schema is written inside a business transaction that could
-- fail because a provider is down. A notification is *enqueued* — a row plus a
-- job — and delivery happens later, out of band. A booking that cannot reach
-- Twilio is still a booking.
--
-- Retry, backoff, dedupe and dead-lettering are not reimplemented here: the
-- platform job queue (0005) already does all four, and notification_attempts
-- records what each try actually did so an operator can see why a message
-- never arrived.

-- ---------------------------------------------------------------------------
-- Templates
--
-- Provider-neutral by construction: a template holds subject and body text
-- with {{variable}} placeholders, and knows nothing about how it will be sent.
-- ---------------------------------------------------------------------------
CREATE TABLE notification_templates (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ntt')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    code            text        NOT NULL,
    name            text        NOT NULL,
    -- The business event this template answers. Not an enum: a tenant may add
    -- its own events without a migration.
    event_type      text        NOT NULL,
    channel         text        NOT NULL CHECK (channel IN ('EMAIL','SMS','WHATSAPP','PUSH')),
    -- BCP-47. One template per (event, channel, locale), so adding a language
    -- is data rather than a schema change.
    locale          text        NOT NULL DEFAULT 'en',

    subject         text,
    body            text        NOT NULL,
    -- Declared placeholders, used to validate a render before sending and to
    -- show an editor what is available.
    variables       jsonb       NOT NULL DEFAULT '[]'::jsonb,

    -- Provider-specific identifiers (a WhatsApp approved-template name, an
    -- SMS sender id). Held as metadata so the core stays provider-neutral.
    provider_ref    text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,

    is_active       boolean     NOT NULL DEFAULT true,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT notification_templates_subject_for_email CHECK (
        channel <> 'EMAIL' OR (subject IS NOT NULL AND length(btrim(subject)) > 0)
    )
);

CREATE UNIQUE INDEX notification_templates_code_idx
    ON notification_templates (organization_id, code);
-- The resolution lookup: the template for an event on a channel in a locale.
CREATE UNIQUE INDEX notification_templates_lookup_idx
    ON notification_templates (organization_id, event_type, channel, locale)
    WHERE is_active;

CREATE TRIGGER notification_templates_touch
    BEFORE UPDATE ON notification_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Preferences — who wants what, on which channel.
--
-- Keyed on a recipient rather than a user, because most notification targets
-- are consignees who have no account at all.
-- ---------------------------------------------------------------------------
CREATE TABLE notification_preferences (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'npf')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    party_type      text        NOT NULL CHECK (party_type IN
                    ('CUSTOMER','USER','FRANCHISE','CONSIGNEE','PARTNER')),
    party_id        bigint,
    -- For a consignee with no record of their own, the phone or email is the
    -- identity. Lowercased on write by the service.
    party_key       text,

    channel         text        NOT NULL CHECK (channel IN ('EMAIL','SMS','WHATSAPP','PUSH')),
    event_type      text,       -- NULL means "all events"
    enabled         boolean     NOT NULL DEFAULT true,
    locale          text        NOT NULL DEFAULT 'en',

    -- Quiet hours in the tenant's timezone. A notification raised inside the
    -- window is deferred rather than dropped.
    quiet_from      time,
    quiet_to        time,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT notification_preferences_identity CHECK (
        party_id IS NOT NULL OR (party_key IS NOT NULL AND length(btrim(party_key)) > 0)
    ),
    CONSTRAINT notification_preferences_quiet_pair CHECK (
        (quiet_from IS NULL) = (quiet_to IS NULL)
    )
);

CREATE UNIQUE INDEX notification_preferences_party_idx
    ON notification_preferences (organization_id, party_type, party_id, channel,
                                 COALESCE(event_type, ''))
    WHERE party_id IS NOT NULL;
CREATE UNIQUE INDEX notification_preferences_key_idx
    ON notification_preferences (organization_id, party_type, party_key, channel,
                                 COALESCE(event_type, ''))
    WHERE party_key IS NOT NULL AND party_id IS NULL;

CREATE TRIGGER notification_preferences_touch
    BEFORE UPDATE ON notification_preferences
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Subscriptions — a standing interest in an event stream.
--
-- Distinct from a preference: a preference says "if you send me this, use SMS";
-- a subscription says "send me this at all". A shipper wanting every NDR on
-- their account subscribes; a consignee does not.
-- ---------------------------------------------------------------------------
CREATE TABLE notification_subscriptions (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'nsb')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    party_type      text        NOT NULL CHECK (party_type IN
                    ('CUSTOMER','USER','FRANCHISE','PARTNER')),
    party_id        bigint      NOT NULL,

    event_type      text        NOT NULL,
    channels        text[]      NOT NULL DEFAULT ARRAY['EMAIL']::text[],
    -- Optional narrowing: only this operating unit, only this service.
    scope           jsonb       NOT NULL DEFAULT '{}'::jsonb,

    is_active       boolean     NOT NULL DEFAULT true,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT notification_subscriptions_channels CHECK (
        array_length(channels, 1) >= 1
        AND channels <@ ARRAY['EMAIL','SMS','WHATSAPP','PUSH']::text[]
    )
);

CREATE UNIQUE INDEX notification_subscriptions_unique_idx
    ON notification_subscriptions (organization_id, party_type, party_id, event_type);
CREATE INDEX notification_subscriptions_event_idx
    ON notification_subscriptions (organization_id, event_type) WHERE is_active;

CREATE TRIGGER notification_subscriptions_touch
    BEFORE UPDATE ON notification_subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Notifications — one intended message.
--
-- Created inside the business transaction that raised the event, so raising a
-- notification is atomic with the thing that caused it. Actually *sending* it
-- is a job, which is what keeps provider availability out of the booking path.
-- ---------------------------------------------------------------------------
CREATE TABLE notifications (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'ntf')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,

    event_type      text        NOT NULL,
    channel         text        NOT NULL CHECK (channel IN ('EMAIL','SMS','WHATSAPP','PUSH')),
    template_id     bigint      REFERENCES notification_templates (id) ON DELETE RESTRICT,
    template_code   text,
    locale          text        NOT NULL DEFAULT 'en',

    -- Where it is going. Stored resolved, so a later change to a customer's
    -- phone number does not rewrite the history of what was sent where.
    recipient_type  text        NOT NULL CHECK (recipient_type IN
                    ('CUSTOMER','USER','FRANCHISE','CONSIGNEE','PARTNER')),
    recipient_id    bigint,
    recipient_address text      NOT NULL,
    recipient_name  text,

    subject         text,
    body            text        NOT NULL,
    -- The variables the body was rendered from, kept so a support agent can
    -- see exactly what the customer was told.
    variables       jsonb       NOT NULL DEFAULT '{}'::jsonb,

    -- What this is about, for the operator's timeline view.
    shipment_id     bigint      REFERENCES shipments (id) ON DELETE SET NULL,
    invoice_id      bigint      REFERENCES invoices (id) ON DELETE SET NULL,
    settlement_id   bigint      REFERENCES settlements (id) ON DELETE SET NULL,
    reference       text,

    status          text        NOT NULL DEFAULT 'PENDING' CHECK (status IN
                    ('PENDING','SENDING','SENT','DELIVERED','FAILED','DEAD_LETTER',
                     'SUPPRESSED','CANCELLED')),
    -- Why a message was never attempted: opted out, quiet hours, no address,
    -- no template. Suppression is a normal outcome, not a failure.
    suppressed_reason text,

    attempt_count   integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts    integer     NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    next_attempt_at timestamptz,
    sent_at         timestamptz,
    delivered_at    timestamptz,
    failed_at       timestamptz,
    last_error      text,

    -- Provider-neutral: whichever adapter sent it records its own id here.
    provider        text,
    provider_message_id text,

    -- Deduplication key. Two identical events — a webhook replayed, a status
    -- transition retried — collapse onto one message rather than telling the
    -- customer twice.
    dedupe_key      text,

    job_id          bigint      REFERENCES jobs (id) ON DELETE SET NULL,
    request_id      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT notifications_suppression_recorded CHECK (
        status <> 'SUPPRESSED' OR suppressed_reason IS NOT NULL
    ),
    CONSTRAINT notifications_failure_recorded CHECK (
        status NOT IN ('FAILED','DEAD_LETTER') OR last_error IS NOT NULL
    )
);

-- The dedupe guard. Excludes terminal-failure states so a message that died can
-- be legitimately re-raised.
CREATE UNIQUE INDEX notifications_dedupe_idx
    ON notifications (organization_id, dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status NOT IN ('FAILED','DEAD_LETTER','CANCELLED');

CREATE INDEX notifications_pending_idx
    ON notifications (organization_id, next_attempt_at)
    WHERE status IN ('PENDING','SENDING');
CREATE INDEX notifications_recipient_idx
    ON notifications (organization_id, recipient_type, recipient_id, created_at DESC);
CREATE INDEX notifications_shipment_idx
    ON notifications (organization_id, shipment_id) WHERE shipment_id IS NOT NULL;
CREATE INDEX notifications_status_idx
    ON notifications (organization_id, status, created_at DESC);
CREATE INDEX notifications_event_idx
    ON notifications (organization_id, event_type, created_at DESC);

CREATE TRIGGER notifications_touch
    BEFORE UPDATE ON notifications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Attempts — what each try actually did.
--
-- Append-only. When a customer says "I never got the message", this table is
-- the answer: which provider, when, what it said back.
-- ---------------------------------------------------------------------------
CREATE TABLE notification_attempts (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'nta')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    notification_id bigint      NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,

    attempt_no      integer     NOT NULL CHECK (attempt_no > 0),
    provider        text        NOT NULL,
    outcome         text        NOT NULL CHECK (outcome IN
                    ('SENT','FAILED','REJECTED','TIMEOUT','RATE_LIMITED','SKIPPED')),

    provider_message_id text,
    -- The provider's own status code and message, verbatim. Never parsed into
    -- a shared enum: providers disagree, and the raw value is what a support
    -- engineer needs.
    provider_status text,
    error_message   text,
    -- Whether the adapter judged this worth retrying. A rejected address is
    -- permanent; a timeout is not.
    retryable       boolean     NOT NULL DEFAULT true,

    duration_ms     integer,
    attempted_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX notification_attempts_no_idx
    ON notification_attempts (notification_id, attempt_no);
CREATE INDEX notification_attempts_notification_idx
    ON notification_attempts (notification_id, attempted_at DESC);
CREATE INDEX notification_attempts_failures_idx
    ON notification_attempts (organization_id, attempted_at DESC)
    WHERE outcome <> 'SENT';

CREATE TRIGGER notification_attempts_append_only
    BEFORE UPDATE OR DELETE ON notification_attempts
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Default templates for every existing organization.
--
-- A tenant with no templates would silently suppress every notification, which
-- looks identical to "notifications are broken". Shipping a working default set
-- means the feature works on day one and a tenant customises rather than
-- configures from nothing.
-- ---------------------------------------------------------------------------
INSERT INTO notification_templates (public_id, organization_id, code, name, event_type,
                                    channel, locale, subject, body, variables)
SELECT gen_seed_public_id('ntt'), o.id, d.code, d.name, d.event_type, d.channel, 'en',
       d.subject, d.body, d.variables::jsonb
FROM organizations o
CROSS JOIN (VALUES
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
