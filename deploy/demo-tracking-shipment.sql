BEGIN;

WITH org AS (
  SELECT id FROM organizations ORDER BY id LIMIT 1
)
INSERT INTO courier_services (
  public_id, organization_id, code, name, description, mode,
  max_weight_grams, sla_transit_hours, status
)
SELECT gen_seed_public_id('svc'), id, 'DEMO_EXPRESS', 'Ceserv Express',
       'Demonstration service for public tracking', 'AIR', 50000, 48, 'ACTIVE'
FROM org
ON CONFLICT (organization_id, code) DO NOTHING;

WITH org AS (
  SELECT id FROM organizations ORDER BY id LIMIT 1
)
INSERT INTO customers (
  public_id, organization_id, code, customer_type, name, phone, status, notes,
  metadata
)
SELECT gen_seed_public_id('cus'), id, 'DEMO_TRACKING', 'RETAIL',
       'Public Tracking Demonstration', '+2348000000000', 'ACTIVE',
       'System demonstration record', '{"demo":true}'::jsonb
FROM org
ON CONFLICT (organization_id, code) DO NOTHING;

WITH org AS (
  SELECT id FROM organizations ORDER BY id LIMIT 1
), refs AS (
  SELECT org.id AS organization_id, c.id AS customer_id, s.id AS service_id
  FROM org
  JOIN customers c ON c.organization_id = org.id AND c.code = 'DEMO_TRACKING'
  JOIN courier_services s ON s.organization_id = org.id AND s.code = 'DEMO_EXPRESS'
)
INSERT INTO shipments (
  public_id, organization_id, awb, reference_number, customer_id,
  courier_service_id, payment_mode, current_status, status_changed_at,
  event_sequence, origin_pincode, destination_pincode, piece_count,
  actual_weight_grams, volumetric_weight_grams, chargeable_weight_grams,
  currency, declared_value_minor, cod_amount_minor, total_amount_minor,
  sla_hours, promised_delivery_at, booked_at, content_description, metadata
)
SELECT gen_seed_public_id('shp'), organization_id, 'CSV260810000001',
       'PUBLIC-DEMO-001', customer_id, service_id, 'PREPAID',
       'OUT_FOR_DELIVERY', now() - interval '45 minutes', 5,
       '100001', '900001', 2, 4200, 5000, 5000, 'NGN', 2500000, 0, 185000,
       48, now() + interval '6 hours', now() - interval '2 days',
       'Two demonstration parcels', '{"demo":true,"publicTracking":true}'::jsonb
FROM refs
ON CONFLICT (awb) DO NOTHING;

WITH shipment AS (
  SELECT id, organization_id FROM shipments WHERE awb = 'CSV260810000001'
), events(sequence, event_type, from_status, to_status, occurred_at, description) AS (
  VALUES
    (1, 'BOOKED', NULL::text, 'BOOKED', now() - interval '2 days',
     'Shipment booked and label generated.'),
    (2, 'STATUS_CHANGED', 'BOOKED', 'PICKUP_SCHEDULED', now() - interval '47 hours',
     'Pickup scheduled with the sender.'),
    (3, 'STATUS_CHANGED', 'PICKUP_SCHEDULED', 'PICKED_UP', now() - interval '40 hours',
     'Both package pieces were collected from the sender.'),
    (4, 'STATUS_CHANGED', 'PICKED_UP', 'IN_TRANSIT', now() - interval '20 hours',
     'Shipment departed the origin facility and is moving to its destination.'),
    (5, 'STATUS_CHANGED', 'IN_TRANSIT', 'OUT_FOR_DELIVERY', now() - interval '45 minutes',
     'Shipment is with the delivery driver for final delivery.')
)
INSERT INTO shipment_events (
  public_id, organization_id, shipment_id, sequence, event_type, from_status,
  to_status, occurred_at, actor_type, description, idempotency_key, metadata
)
SELECT gen_seed_public_id('evt'), shipment.organization_id, shipment.id,
       events.sequence, events.event_type, events.from_status, events.to_status,
       events.occurred_at, 'SYSTEM', events.description,
       'public-demo-' || events.sequence, '{"demo":true}'::jsonb
FROM shipment CROSS JOIN events
ON CONFLICT (shipment_id, sequence) DO NOTHING;

COMMIT;
