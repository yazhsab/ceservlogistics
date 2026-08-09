ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_operating_unit_fk;
ALTER TABLE user_roles DROP CONSTRAINT IF EXISTS user_roles_operating_unit_fk;
DROP TABLE IF EXISTS franchise_agreements;
DROP TABLE IF EXISTS franchises;
DROP TABLE IF EXISTS operating_unit_capabilities;
DROP TABLE IF EXISTS operating_units;
DROP TABLE IF EXISTS regions;
