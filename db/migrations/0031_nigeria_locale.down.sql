ALTER TABLE notifications DROP COLUMN IF EXISTS provider_ref;

ALTER TABLE organizations DROP COLUMN IF EXISTS country;

ALTER TABLE customers
    DROP COLUMN IF EXISTS business_registration_number,
    DROP COLUMN IF EXISTS tax_registration_number;
ALTER TABLE franchises
    DROP COLUMN IF EXISTS business_registration_number,
    DROP COLUMN IF EXISTS tax_registration_number;

DELETE FROM states WHERE country_id = (SELECT id FROM countries WHERE iso2 = 'NG');
DELETE FROM countries WHERE iso2 = 'NG';

ALTER TABLE tax_rules DROP CONSTRAINT tax_rules_tax_type_check;
ALTER TABLE tax_rules ADD CONSTRAINT tax_rules_tax_type_check
    CHECK (tax_type IN ('CGST','SGST','IGST','UTGST','CESS','CUSTOM'));
