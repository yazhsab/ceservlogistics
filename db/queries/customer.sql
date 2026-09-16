-- Customer domain queries (M07).

-- ---------------------------------------------------------------------------
-- Customers
-- ---------------------------------------------------------------------------

-- name: CreateCustomer :one
INSERT INTO customers (
    public_id, organization_id, code, customer_type, name, email, phone,
    owning_unit_id, gst_number, pan_number, notes, metadata, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING *;

-- name: GetCustomerByPublicID :one
SELECT c.*, ou.code AS owning_unit_code, ou.name AS owning_unit_name
FROM customers c
LEFT JOIN operating_units ou ON ou.id = c.owning_unit_id
WHERE c.public_id = sqlc.arg('public_id') AND c.organization_id = sqlc.arg('organization_id');

-- name: GetCustomerByID :one
SELECT * FROM customers WHERE id = $1 AND organization_id = $2;

-- name: GetCustomerByCode :one
SELECT * FROM customers WHERE organization_id = $1 AND code = $2;

-- name: ListCustomers :many
SELECT c.*, ou.code AS owning_unit_code,
       ba.account_code, ba.credit_limit_minor AS account_credit_limit_minor,
       cp.credit_status, cp.credit_used_minor,
       count(*) OVER () AS total_count
FROM customers c
LEFT JOIN operating_units ou ON ou.id = c.owning_unit_id
LEFT JOIN business_accounts ba ON ba.customer_id = c.id
LEFT JOIN credit_profiles cp ON cp.customer_id = c.id
WHERE c.organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('customer_type')::text IS NULL OR c.customer_type = sqlc.narg('customer_type'))
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status'))
  AND (sqlc.narg('owning_unit_id')::bigint IS NULL OR c.owning_unit_id = sqlc.narg('owning_unit_id'))
  AND (sqlc.narg('search')::text IS NULL
       OR lower(c.name) LIKE '%' || lower(sqlc.narg('search')) || '%'
       OR c.code LIKE upper(sqlc.narg('search')) || '%'
       OR c.phone LIKE sqlc.narg('search') || '%'
       OR c.email LIKE lower(sqlc.narg('search')) || '%')
  AND (sqlc.narg('scoped_unit_ids')::bigint[] IS NULL
       OR c.owning_unit_id = ANY(sqlc.narg('scoped_unit_ids')::bigint[]))
  AND (sqlc.narg('customer_ids')::bigint[] IS NULL OR c.id = ANY(sqlc.narg('customer_ids')::bigint[]))
ORDER BY c.name, c.id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: UpdateCustomer :one
UPDATE customers
SET name = COALESCE(sqlc.narg('name'), name),
    email = COALESCE(sqlc.narg('email'), email),
    phone = COALESCE(sqlc.narg('phone'), phone),
    owning_unit_id = COALESCE(sqlc.narg('owning_unit_id'), owning_unit_id),
    gst_number = COALESCE(sqlc.narg('gst_number'), gst_number),
    pan_number = COALESCE(sqlc.narg('pan_number'), pan_number),
    notes = COALESCE(sqlc.narg('notes'), notes),
    metadata = COALESCE(sqlc.narg('metadata'), metadata)
WHERE public_id = sqlc.arg('public_id')
  AND organization_id = sqlc.arg('organization_id')
  AND version = sqlc.arg('expected_version')
RETURNING *;

-- name: SetCustomerStatus :one
UPDATE customers
SET status = sqlc.arg('status'),
    suspended_at = CASE WHEN sqlc.arg('status')::text = 'SUSPENDED' THEN now() ELSE suspended_at END,
    suspension_reason = CASE WHEN sqlc.arg('status')::text = 'SUSPENDED'
                             THEN sqlc.narg('suspension_reason') ELSE NULL END
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Business accounts
-- ---------------------------------------------------------------------------

-- name: CreateBusinessAccount :one
INSERT INTO business_accounts (
    public_id, organization_id, customer_id, account_code, legal_name, industry,
    credit_limit_minor, currency, payment_terms_days, billing_cycle,
    account_manager_user_id, rate_card_id, tax_metadata, contract_start, contract_end
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;

-- name: GetBusinessAccountByCustomer :one
SELECT ba.*, rc.public_id AS rate_card_public_id, rc.code AS rate_card_code
FROM business_accounts ba
LEFT JOIN rate_cards rc ON rc.id = ba.rate_card_id
WHERE ba.customer_id = $1;

-- name: UpdateBusinessAccount :one
UPDATE business_accounts
SET legal_name = COALESCE(sqlc.narg('legal_name'), legal_name),
    industry = COALESCE(sqlc.narg('industry'), industry),
    credit_limit_minor = COALESCE(sqlc.narg('credit_limit_minor'), credit_limit_minor),
    payment_terms_days = COALESCE(sqlc.narg('payment_terms_days'), payment_terms_days),
    billing_cycle = COALESCE(sqlc.narg('billing_cycle'), billing_cycle),
    account_manager_user_id = COALESCE(sqlc.narg('account_manager_user_id'), account_manager_user_id),
    rate_card_id = COALESCE(sqlc.narg('rate_card_id'), rate_card_id),
    tax_metadata = COALESCE(sqlc.narg('tax_metadata'), tax_metadata),
    contract_start = COALESCE(sqlc.narg('contract_start'), contract_start),
    contract_end = COALESCE(sqlc.narg('contract_end'), contract_end),
    status = COALESCE(sqlc.narg('status'), status)
WHERE customer_id = sqlc.arg('customer_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Contacts
-- ---------------------------------------------------------------------------

-- name: CreateCustomerContact :one
INSERT INTO customer_contacts (
    public_id, organization_id, customer_id, name, designation, email, phone, contact_type, is_primary
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: ListCustomerContacts :many
SELECT * FROM customer_contacts
WHERE customer_id = $1 AND status = 'ACTIVE'
ORDER BY is_primary DESC, contact_type, name;

-- name: GetCustomerContactByPublicID :one
SELECT * FROM customer_contacts WHERE public_id = $1 AND organization_id = $2;

-- name: ClearPrimaryContact :execrows
UPDATE customer_contacts SET is_primary = false
WHERE customer_id = $1 AND is_primary AND status = 'ACTIVE';

-- name: UpdateCustomerContact :one
UPDATE customer_contacts
SET name = COALESCE(sqlc.narg('name'), name),
    designation = COALESCE(sqlc.narg('designation'), designation),
    email = COALESCE(sqlc.narg('email'), email),
    phone = COALESCE(sqlc.narg('phone'), phone),
    contact_type = COALESCE(sqlc.narg('contact_type'), contact_type),
    is_primary = COALESCE(sqlc.narg('is_primary'), is_primary),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Addresses
-- ---------------------------------------------------------------------------

-- name: CreateCustomerAddress :one
INSERT INTO customer_addresses (
    public_id, organization_id, customer_id, label, address_type,
    contact_name, contact_phone, alt_phone, line1, line2, landmark,
    pincode, pincode_id, city_id, state_id, city_name, state_name, country_code,
    latitude, longitude, is_default
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
RETURNING *;

-- name: GetCustomerAddressByPublicID :one
SELECT * FROM customer_addresses
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id');

-- name: ListCustomerAddresses :many
SELECT * FROM customer_addresses
WHERE customer_id = sqlc.arg('customer_id')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('address_type')::text IS NULL OR address_type IN (sqlc.narg('address_type'), 'BOTH'))
ORDER BY is_default DESC, label, id;

-- name: ClearDefaultAddress :execrows
UPDATE customer_addresses SET is_default = false
WHERE customer_id = sqlc.arg('customer_id') AND address_type = sqlc.arg('address_type')
  AND is_default AND status = 'ACTIVE';

-- name: UpdateCustomerAddress :one
UPDATE customer_addresses
SET country_code = COALESCE(sqlc.narg('country_code'), country_code),
 label = COALESCE(sqlc.narg('label'), label),
    address_type = COALESCE(sqlc.narg('address_type'), address_type),
    contact_name = COALESCE(sqlc.narg('contact_name'), contact_name),
    contact_phone = COALESCE(sqlc.narg('contact_phone'), contact_phone),
    alt_phone = COALESCE(sqlc.narg('alt_phone'), alt_phone),
    line1 = COALESCE(sqlc.narg('line1'), line1),
    line2 = COALESCE(sqlc.narg('line2'), line2),
    landmark = COALESCE(sqlc.narg('landmark'), landmark),
    pincode = COALESCE(sqlc.narg('pincode'), pincode),
    pincode_id = COALESCE(sqlc.narg('pincode_id'), pincode_id),
    city_id = COALESCE(sqlc.narg('city_id'), city_id),
    state_id = COALESCE(sqlc.narg('state_id'), state_id),
    city_name = COALESCE(sqlc.narg('city_name'), city_name),
    state_name = COALESCE(sqlc.narg('state_name'), state_name),
    latitude = COALESCE(sqlc.narg('latitude'), latitude),
    longitude = COALESCE(sqlc.narg('longitude'), longitude),
    is_default = COALESCE(sqlc.narg('is_default'), is_default),
    status = COALESCE(sqlc.narg('status'), status)
WHERE public_id = sqlc.arg('public_id') AND organization_id = sqlc.arg('organization_id')
RETURNING *;

-- ---------------------------------------------------------------------------
-- Billing and credit profiles
-- ---------------------------------------------------------------------------

-- name: UpsertBillingProfile :one
INSERT INTO billing_profiles (
    public_id, organization_id, customer_id, legal_name, gst_number, pan_number,
    billing_address_id, billing_email, invoice_delivery, currency, place_of_supply_state_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (customer_id) DO UPDATE
    SET legal_name = EXCLUDED.legal_name,
        gst_number = EXCLUDED.gst_number,
        pan_number = EXCLUDED.pan_number,
        billing_address_id = EXCLUDED.billing_address_id,
        billing_email = EXCLUDED.billing_email,
        invoice_delivery = EXCLUDED.invoice_delivery,
        place_of_supply_state_id = EXCLUDED.place_of_supply_state_id
RETURNING *;

-- name: GetBillingProfileByCustomer :one
SELECT bp.*, s.gst_state_code AS place_of_supply_gst_code, s.name AS place_of_supply_state_name
FROM billing_profiles bp
LEFT JOIN states s ON s.id = bp.place_of_supply_state_id
WHERE bp.customer_id = $1;

-- name: UpsertCreditProfile :one
INSERT INTO credit_profiles (
    public_id, organization_id, customer_id, currency, credit_limit_minor,
    payment_terms_days, credit_status, blocked_reason, last_reviewed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
ON CONFLICT (customer_id) DO UPDATE
    SET credit_limit_minor = EXCLUDED.credit_limit_minor,
        payment_terms_days = EXCLUDED.payment_terms_days,
        credit_status = EXCLUDED.credit_status,
        blocked_reason = EXCLUDED.blocked_reason,
        last_reviewed_at = now()
RETURNING *;

-- name: GetCreditProfileByCustomer :one
SELECT * FROM credit_profiles WHERE customer_id = $1;

-- LockCreditProfile takes a row lock for the duration of a booking
-- transaction, so two concurrent bookings on the same credit account cannot
-- both pass the limit check (Constitution §22).
-- name: LockCreditProfile :one
SELECT * FROM credit_profiles
WHERE customer_id = sqlc.arg('customer_id') AND organization_id = sqlc.arg('organization_id')
FOR UPDATE;

-- name: AdjustCreditUsage :one
UPDATE credit_profiles
SET credit_used_minor = credit_used_minor + sqlc.arg('delta_minor')
WHERE customer_id = sqlc.arg('customer_id') AND organization_id = sqlc.arg('organization_id')
RETURNING credit_used_minor, credit_limit_minor, credit_status;

-- name: InsertCreditEntry :one
INSERT INTO customer_credit_entries (
    organization_id, customer_id, entry_type, currency, amount_minor,
    balance_after_minor, shipment_id, reason, created_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: ListCreditEntries :many
SELECT e.*, s.awb, count(*) OVER () AS total_count
FROM customer_credit_entries e
LEFT JOIN shipments s ON s.id = e.shipment_id
WHERE e.customer_id = sqlc.arg('customer_id') AND e.organization_id = sqlc.arg('organization_id')
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- ---------------------------------------------------------------------------
-- Customer portal users
-- ---------------------------------------------------------------------------

-- name: LinkCustomerUser :one
INSERT INTO customer_users (organization_id, customer_id, user_id, portal_role)
VALUES ($1,$2,$3,$4)
ON CONFLICT (customer_id, user_id) DO UPDATE SET portal_role = EXCLUDED.portal_role, status = 'ACTIVE'
RETURNING *;

-- ListCustomerIDsForUser backs object-level authorization for CUSTOMER-role
-- principals: they may only see shipments belonging to these customers.
-- name: ListCustomerIDsForUser :many
SELECT customer_id FROM customer_users
WHERE user_id = $1 AND status = 'ACTIVE';

-- name: ListCustomerUsers :many
SELECT cu.*, u.public_id AS user_public_id, u.email, u.full_name, u.status AS user_status
FROM customer_users cu
JOIN users u ON u.id = cu.user_id
WHERE cu.customer_id = $1
ORDER BY u.full_name;

-- name: UnlinkCustomerUser :execrows
UPDATE customer_users SET status = 'INACTIVE'
WHERE customer_id = $1 AND user_id = $2;
