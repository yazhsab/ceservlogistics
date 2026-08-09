-- 0032 Nigeria becomes the default market, not a supported one.
--
-- 0031 made Nigeria *possible*. This makes it the default, which is a different
-- thing: every column default, every bootstrap value and every fallback still
-- said India, so a tenant created without stating its market silently became
-- Indian — INR amounts, Asia/Kolkata timestamps, IN addresses.
--
-- The money defaults are the dangerous ones. A Nigerian tenant whose code
-- forgot to pass a currency would have written NGN amounts labelled INR, and
-- the ledger would have summed them into a statement that was quietly wrong.
--
-- Column defaults affect new rows only. Existing data is untouched, and an
-- organization that is genuinely Indian keeps every stored value it has.
--
-- The mechanism stays multi-market. Defaulting to Nigeria is a decision about
-- which market comes first, not a decision to hard-code one — that was the
-- original mistake, and repeating it in a new direction would be no better.

-- Every column that defaulted to INR, found by asking the catalogue rather than
-- by grepping the migrations, so none was missed.
ALTER TABLE billing_profiles         ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE business_accounts        ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE credit_profiles          ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE customer_credit_entries  ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE delivery_attempts        ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE delivery_runs            ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE franchise_agreements     ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE rate_cards               ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE rto_cases                ALTER COLUMN currency SET DEFAULT 'NGN';
ALTER TABLE shipments                ALTER COLUMN currency SET DEFAULT 'NGN';

ALTER TABLE organizations
    ALTER COLUMN currency SET DEFAULT 'NGN',
    ALTER COLUMN timezone SET DEFAULT 'Africa/Lagos',
    ALTER COLUMN country  SET DEFAULT 'NG';

ALTER TABLE customer_addresses         ALTER COLUMN country_code SET DEFAULT 'NG';
ALTER TABLE shipment_address_snapshots ALTER COLUMN country_code SET DEFAULT 'NG';
