-- Rollback 0019.
--
-- Deleting the permissions cascades to role_permissions, so the grants above
-- disappear with them. NDR reasons seeded here are removed by code; a tenant
-- that added its own keeps them, which is why the delete is filtered.

DELETE FROM ndr_reasons WHERE code IN (
    'CUSTOMER_NOT_AVAILABLE','PREMISES_CLOSED','ADDRESS_INCOMPLETE','ADDRESS_NOT_FOUND',
    'CUSTOMER_REFUSED','COD_NOT_READY','CUSTOMER_RESCHEDULED','PHONE_UNREACHABLE',
    'RESTRICTED_ACCESS','WEATHER_DISRUPTION','VEHICLE_BREAKDOWN','SHIPMENT_DAMAGED');

DELETE FROM permissions WHERE module IN
    ('pickup','scanning','bagging','manifest','linehaul','hubops','delivery','ndr','rto','pod');
DELETE FROM permissions WHERE code IN ('shipment.declare_exception','shipment.override_custody');
