-- Reverse 0026.
DROP TRIGGER IF EXISTS notification_attempts_append_only ON notification_attempts;
DROP TABLE IF EXISTS notification_attempts;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS notification_subscriptions;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notification_templates;
