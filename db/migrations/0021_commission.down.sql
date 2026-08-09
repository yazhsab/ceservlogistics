-- Reverse 0021.
DROP TRIGGER IF EXISTS commission_entries_append_only ON commission_entries;
DROP TRIGGER IF EXISTS commission_calculations_guard_trigger ON commission_calculations;
DROP TRIGGER IF EXISTS commission_rule_versions_guard_trigger ON commission_rule_versions;
DROP TRIGGER IF EXISTS commission_calculations_touch ON commission_calculations;
DROP TRIGGER IF EXISTS commission_rules_touch ON commission_rules;
DROP TRIGGER IF EXISTS commission_rules_specificity_trigger ON commission_rules;
DROP TRIGGER IF EXISTS commission_schemes_touch ON commission_schemes;

DROP FUNCTION IF EXISTS commission_calculations_guard();
DROP FUNCTION IF EXISTS commission_rule_versions_guard();
DROP FUNCTION IF EXISTS commission_rules_specificity();

DROP TABLE IF EXISTS commission_entries;
DROP TABLE IF EXISTS commission_calculations;
DROP TABLE IF EXISTS commission_rule_versions;
DROP TABLE IF EXISTS commission_rules;
DROP TABLE IF EXISTS commission_schemes;
