-- 0007 Customer domain (M07).

CREATE TABLE customers (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cus')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code            text        NOT NULL CHECK (code = upper(code) AND code ~ '^[A-Z0-9][A-Z0-9_-]{1,31}$'),
    customer_type   text        NOT NULL CHECK (customer_type IN ('RETAIL','BUSINESS')),
    name            text        NOT NULL CHECK (length(btrim(name)) BETWEEN 2 AND 160),
    email           text        CHECK (email IS NULL OR (email = lower(email) AND email LIKE '%@%.%')),
    phone           text        NOT NULL,
    -- The operating unit that owns the relationship (booking branch/franchise).
    owning_unit_id  bigint      REFERENCES operating_units (id) ON DELETE RESTRICT,
    status          text        NOT NULL DEFAULT 'ACTIVE'
                                CHECK (status IN ('ACTIVE','SUSPENDED','CLOSED')),
    suspended_at    timestamptz,
    suspension_reason text,
    gst_number      text,
    pan_number      text,
    notes           text,
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    version         integer     NOT NULL DEFAULT 1,
    CONSTRAINT customers_code_unique UNIQUE (organization_id, code),
    CONSTRAINT customers_suspension_recorded CHECK (status <> 'SUSPENDED' OR suspended_at IS NOT NULL)
);

CREATE INDEX customers_org_status_idx ON customers (organization_id, status, id DESC);
CREATE INDEX customers_org_type_idx ON customers (organization_id, customer_type, id DESC);
CREATE INDEX customers_phone_idx ON customers (organization_id, phone);
CREATE INDEX customers_email_idx ON customers (organization_id, email) WHERE email IS NOT NULL;
CREATE INDEX customers_name_search_idx ON customers (organization_id, lower(name));
CREATE INDEX customers_unit_idx ON customers (owning_unit_id) WHERE owning_unit_id IS NOT NULL;

CREATE TRIGGER customers_bump_version BEFORE UPDATE ON customers
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

-- ---------------------------------------------------------------------------
-- Business accounts
-- ---------------------------------------------------------------------------
-- rate_card_id is added by migration 0009 once rate_cards exists.
CREATE TABLE business_accounts (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id              text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'bac')),
    organization_id        bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id            bigint      NOT NULL UNIQUE REFERENCES customers (id) ON DELETE CASCADE,
    account_code           text        NOT NULL CHECK (account_code = upper(account_code)),
    legal_name             text        NOT NULL,
    industry               text,
    credit_limit_minor     bigint      NOT NULL DEFAULT 0 CHECK (credit_limit_minor >= 0),
    currency               char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    payment_terms_days     integer     NOT NULL DEFAULT 0 CHECK (payment_terms_days BETWEEN 0 AND 180),
    billing_cycle          text        NOT NULL DEFAULT 'MONTHLY'
                                       CHECK (billing_cycle IN ('WEEKLY','FORTNIGHTLY','MONTHLY')),
    account_manager_user_id bigint     REFERENCES users (id) ON DELETE SET NULL,
    tax_metadata           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    status                 text        NOT NULL DEFAULT 'ACTIVE'
                                       CHECK (status IN ('ACTIVE','ON_HOLD','CLOSED')),
    contract_start         date,
    contract_end           date,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT business_accounts_code_unique UNIQUE (organization_id, account_code),
    CONSTRAINT business_accounts_contract_range CHECK (contract_end IS NULL OR contract_start IS NULL OR contract_end >= contract_start)
);

CREATE INDEX business_accounts_org_status_idx ON business_accounts (organization_id, status);
CREATE INDEX business_accounts_manager_idx ON business_accounts (account_manager_user_id)
    WHERE account_manager_user_id IS NOT NULL;

CREATE TRIGGER business_accounts_set_updated_at BEFORE UPDATE ON business_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Contacts
-- ---------------------------------------------------------------------------
CREATE TABLE customer_contacts (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cct')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id     bigint      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    name            text        NOT NULL,
    designation     text,
    email           text        CHECK (email IS NULL OR email = lower(email)),
    phone           text        NOT NULL,
    contact_type    text        NOT NULL DEFAULT 'PRIMARY'
                                CHECK (contact_type IN ('PRIMARY','BILLING','OPERATIONS','ESCALATION')),
    is_primary      boolean     NOT NULL DEFAULT false,
    status          text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_contacts_customer_idx ON customer_contacts (customer_id, status);
CREATE UNIQUE INDEX customer_contacts_primary_idx
    ON customer_contacts (customer_id) WHERE is_primary AND status = 'ACTIVE';

CREATE TRIGGER customer_contacts_set_updated_at BEFORE UPDATE ON customer_contacts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Addresses
-- ---------------------------------------------------------------------------
-- Addresses are mutable master data. Shipments never point at them directly;
-- booking copies the address into shipment_address_snapshots so that editing a
-- customer address can never rewrite where a past parcel was sent.
CREATE TABLE customer_addresses (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'adr')),
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id     bigint      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    label           text        NOT NULL,
    address_type    text        NOT NULL DEFAULT 'BOTH'
                                CHECK (address_type IN ('PICKUP','DELIVERY','BILLING','BOTH')),
    contact_name    text        NOT NULL,
    contact_phone   text        NOT NULL,
    alt_phone       text,
    line1           text        NOT NULL CHECK (length(btrim(line1)) BETWEEN 3 AND 200),
    line2           text,
    landmark        text,
    pincode         text        NOT NULL CHECK (pincode ~ '^[0-9A-Z][0-9A-Z -]{2,11}$'),
    pincode_id      bigint      REFERENCES pincodes (id) ON DELETE SET NULL,
    city_id         bigint      REFERENCES cities (id) ON DELETE SET NULL,
    state_id        bigint      REFERENCES states (id) ON DELETE SET NULL,
    city_name       text        NOT NULL,
    state_name      text        NOT NULL,
    country_code    char(2)     NOT NULL DEFAULT 'IN',
    latitude        double precision CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    longitude       double precision CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180),
    is_default      boolean     NOT NULL DEFAULT false,
    status          text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_addresses_customer_idx ON customer_addresses (customer_id, status, id DESC);
CREATE INDEX customer_addresses_pincode_idx ON customer_addresses (organization_id, pincode);
CREATE UNIQUE INDEX customer_addresses_default_idx
    ON customer_addresses (customer_id, address_type) WHERE is_default AND status = 'ACTIVE';

CREATE TRIGGER customer_addresses_set_updated_at BEFORE UPDATE ON customer_addresses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Billing profile
-- ---------------------------------------------------------------------------
CREATE TABLE billing_profiles (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id              text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'bpr')),
    organization_id        bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id            bigint      NOT NULL UNIQUE REFERENCES customers (id) ON DELETE CASCADE,
    legal_name             text        NOT NULL,
    gst_number             text,
    pan_number             text,
    billing_address_id     bigint      REFERENCES customer_addresses (id) ON DELETE SET NULL,
    billing_email          text        CHECK (billing_email IS NULL OR billing_email = lower(billing_email)),
    invoice_delivery       text        NOT NULL DEFAULT 'EMAIL'
                                       CHECK (invoice_delivery IN ('EMAIL','PORTAL','BOTH')),
    currency               char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    -- Place of supply decides CGST+SGST versus IGST at quote time.
    place_of_supply_state_id bigint    REFERENCES states (id) ON DELETE SET NULL,
    status                 text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER billing_profiles_set_updated_at BEFORE UPDATE ON billing_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- Credit profile and credit ledger
-- ---------------------------------------------------------------------------
-- credit_used_minor is a maintained counter, not an editable field: every
-- change is written together with an append-only entry in
-- customer_credit_entries inside the same transaction, so the counter is always
-- reproducible from history. Release 3 replaces it with the double-entry ledger.
CREATE TABLE credit_profiles (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id          text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'cpr')),
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id        bigint      NOT NULL UNIQUE REFERENCES customers (id) ON DELETE CASCADE,
    currency           char(3)     NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
    credit_limit_minor bigint      NOT NULL DEFAULT 0 CHECK (credit_limit_minor >= 0),
    credit_used_minor  bigint      NOT NULL DEFAULT 0 CHECK (credit_used_minor >= 0),
    payment_terms_days integer     NOT NULL DEFAULT 0 CHECK (payment_terms_days BETWEEN 0 AND 180),
    credit_status      text        NOT NULL DEFAULT 'GOOD'
                                   CHECK (credit_status IN ('GOOD','WARNING','ON_HOLD','BLOCKED')),
    blocked_reason     text,
    last_reviewed_at   timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT credit_profiles_block_reason CHECK (credit_status <> 'BLOCKED' OR blocked_reason IS NOT NULL)
);

CREATE INDEX credit_profiles_status_idx ON credit_profiles (organization_id, credit_status)
    WHERE credit_status <> 'GOOD';

CREATE TRIGGER credit_profiles_set_updated_at BEFORE UPDATE ON credit_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE customer_credit_entries (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id    bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id        bigint      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    entry_type         text        NOT NULL CHECK (entry_type IN ('RESERVE','RELEASE','ADJUST','LIMIT_CHANGE')),
    currency           char(3)     NOT NULL DEFAULT 'INR',
    amount_minor       bigint      NOT NULL,
    balance_after_minor bigint     NOT NULL CHECK (balance_after_minor >= 0),
    -- shipment_id is added by migration 0011 once shipments exists.
    shipment_id        bigint,
    reason             text,
    created_by         bigint      REFERENCES users (id) ON DELETE SET NULL,
    request_id         text,
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_credit_entries_customer_idx
    ON customer_credit_entries (customer_id, created_at DESC, id DESC);

CREATE TRIGGER customer_credit_entries_append_only
    BEFORE UPDATE OR DELETE ON customer_credit_entries
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

-- ---------------------------------------------------------------------------
-- Customer portal users
-- ---------------------------------------------------------------------------
CREATE TABLE customer_users (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id bigint      NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    customer_id     bigint      NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    user_id         bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    portal_role     text        NOT NULL DEFAULT 'MEMBER' CHECK (portal_role IN ('OWNER','MEMBER','VIEWER')),
    status          text        NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT customer_users_unique UNIQUE (customer_id, user_id)
);

-- A CUSTOMER-role principal is restricted to the customers listed here; this
-- index backs that lookup on every request they make.
CREATE INDEX customer_users_user_idx ON customer_users (user_id, status);

CREATE TRIGGER customer_users_set_updated_at BEFORE UPDATE ON customer_users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
