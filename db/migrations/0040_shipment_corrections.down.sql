DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'shipment.edit');
DELETE FROM permissions WHERE code = 'shipment.edit';

DROP TABLE shipment_address_corrections;
