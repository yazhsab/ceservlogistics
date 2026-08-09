-- Rollback 0013.

DROP TABLE IF EXISTS operational_sequences;
DROP TABLE IF EXISTS pickup_attempts;
DROP TABLE IF EXISTS pickup_assignments;
DROP TABLE IF EXISTS pickup_runs;
DROP TABLE IF EXISTS pickup_request_shipments;
DROP TABLE IF EXISTS pickup_requests;
DROP TABLE IF EXISTS scan_events;

DROP INDEX IF EXISTS shipments_delivery_queue_idx;
DROP INDEX IF EXISTS shipments_current_trip_idx;
DROP INDEX IF EXISTS shipments_current_bag_idx;
DROP INDEX IF EXISTS shipments_custody_user_idx;
DROP INDEX IF EXISTS shipments_custody_unit_idx;

ALTER TABLE shipments
    DROP COLUMN IF EXISTS hold_reason,
    DROP COLUMN IF EXISTS is_held,
    DROP COLUMN IF EXISTS picked_up_at,
    DROP COLUMN IF EXISTS delivered_at,
    DROP COLUMN IF EXISTS first_ofd_at,
    DROP COLUMN IF EXISTS pickup_attempt_count,
    DROP COLUMN IF EXISTS delivery_attempt_count,
    DROP COLUMN IF EXISTS movement_direction,
    DROP COLUMN IF EXISTS current_trip_id,
    DROP COLUMN IF EXISTS current_bag_id;

DROP INDEX IF EXISTS shipment_events_occurred_idx;

ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_event_type_check;
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_event_type_check
    CHECK (event_type IN
        ('BOOKED','STATUS_CHANGED','CANCELLED','EXCEPTION','REMARK','LOCATION_UPDATE','SYSTEM'));

ALTER TABLE shipment_events
    DROP COLUMN IF EXISTS client_occurred_at,
    DROP COLUMN IF EXISTS longitude,
    DROP COLUMN IF EXISTS latitude,
    DROP COLUMN IF EXISTS device_model,
    DROP COLUMN IF EXISTS device_id,
    DROP COLUMN IF EXISTS source;
