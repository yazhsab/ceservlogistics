-- State capitals are optional reference data and are not copied into any
-- foreign-key relationship. Address and shipment snapshots remain intact.
ALTER TABLE states
    DROP COLUMN capital_pincode,
    DROP COLUMN capital_city;
