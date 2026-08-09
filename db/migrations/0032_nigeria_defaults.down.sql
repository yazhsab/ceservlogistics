ALTER TABLE billing_profiles         ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE business_accounts        ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE credit_profiles          ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE customer_credit_entries  ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE delivery_attempts        ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE delivery_runs            ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE franchise_agreements     ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE rate_cards               ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE rto_cases                ALTER COLUMN currency SET DEFAULT 'INR';
ALTER TABLE shipments                ALTER COLUMN currency SET DEFAULT 'INR';

ALTER TABLE organizations
    ALTER COLUMN currency SET DEFAULT 'INR',
    ALTER COLUMN timezone SET DEFAULT 'Asia/Kolkata',
    ALTER COLUMN country  SET DEFAULT 'IN';

ALTER TABLE customer_addresses         ALTER COLUMN country_code SET DEFAULT 'IN';
ALTER TABLE shipment_address_snapshots ALTER COLUMN country_code SET DEFAULT 'IN';
