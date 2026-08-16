-- Commission earned at each custody point in the courier journey.
--
-- Booking and final-delivery commission existed before this migration. A parcel
-- can pass through several independently accountable hubs, however, and each
-- handling point must be represented explicitly rather than folded into the
-- origin or destination payment.

ALTER TABLE commission_rules DROP CONSTRAINT commission_rules_commission_type_check;
ALTER TABLE commission_rules ADD CONSTRAINT commission_rules_commission_type_check
    CHECK (commission_type IN (
        'BOOKING','PICKUP','ORIGIN_HANDLING','TRANSIT_HANDLING',
        'DESTINATION_HANDLING','DELIVERY','COD','VOLUME_INCENTIVE','CUSTOM'));

ALTER TABLE commission_rules DROP CONSTRAINT commission_rules_recipient_role_check;
ALTER TABLE commission_rules ADD CONSTRAINT commission_rules_recipient_role_check
    CHECK (recipient_role IN (
        'ORIGIN_FRANCHISE','DESTINATION_FRANCHISE','PICKUP_AGENT',
        'DELIVERY_AGENT','ORIGIN_UNIT','TRANSIT_UNIT','DESTINATION_UNIT','CUSTOM'));

ALTER TABLE commission_calculations DROP CONSTRAINT commission_calculations_commission_type_check;
ALTER TABLE commission_calculations ADD CONSTRAINT commission_calculations_commission_type_check
    CHECK (commission_type IN (
        'BOOKING','PICKUP','ORIGIN_HANDLING','TRANSIT_HANDLING',
        'DESTINATION_HANDLING','DELIVERY','COD','VOLUME_INCENTIVE','CUSTOM'));
