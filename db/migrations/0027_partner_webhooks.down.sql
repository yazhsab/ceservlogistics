-- Reverse 0027.
DROP TRIGGER IF EXISTS webhook_attempts_append_only ON webhook_attempts;
DROP TRIGGER IF EXISTS api_key_requests_append_only ON api_key_requests;
DROP TRIGGER IF EXISTS api_keys_guard_trigger ON api_keys;
DROP FUNCTION IF EXISTS api_keys_guard();

DROP TABLE IF EXISTS webhook_attempts;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_subscriptions;
DROP TABLE IF EXISTS webhook_endpoints;
DROP TABLE IF EXISTS api_key_requests;
DROP TABLE IF EXISTS api_keys;
