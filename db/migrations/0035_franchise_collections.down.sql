DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('collection.read','collection.record','collection.manage'));
DELETE FROM permissions WHERE code IN ('collection.read','collection.record','collection.manage');

ALTER TABLE settlement_lines DROP CONSTRAINT settlement_lines_source_type_check;
ALTER TABLE settlement_lines ADD CONSTRAINT settlement_lines_source_type_check CHECK (source_type IN
    ('COMMISSION_CALCULATION','COD_OBLIGATION','COD_ADJUSTMENT','SETTLEMENT_ADJUSTMENT',
     'PREVIOUS_SETTLEMENT','TAX_RULE','MANUAL'));
ALTER TABLE settlement_lines DROP CONSTRAINT settlement_lines_category_check;
ALTER TABLE settlement_lines ADD CONSTRAINT settlement_lines_category_check CHECK (category IN
    ('BOOKING_COMMISSION','PICKUP_COMMISSION','ORIGIN_HANDLING','DESTINATION_HANDLING',
     'DELIVERY_COMMISSION','COD_COMMISSION','VOLUME_INCENTIVE','CUSTOM_COMMISSION',
     'COD_LIABILITY','CHARGE','PENALTY','INCENTIVE','ADJUSTMENT','TAX','WITHHOLDING','OPENING_BALANCE'));

ALTER TABLE settlements DROP COLUMN collections_minor;
DROP TABLE franchise_collections;
