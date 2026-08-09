-- 0019 Release 2 permission catalogue and role grants.
--
-- Release 1 seeded PICKUP_AGENT and DELIVERY_AGENT with read-only permissions
-- because their operational verbs did not exist yet. This migration adds those
-- verbs and grants them.
--
-- Two principles carried over from 0012:
--
--   * SUPER_ADMIN is granted every permission by construction, so nothing here
--     needs to remember to include it.
--   * Field roles get the narrowest possible set. A delivery agent can complete
--     the delivery in front of them; they cannot create a run, reassign work,
--     or see the network's shipments.

INSERT INTO permissions (public_id, code, resource, action, module, description) VALUES
    -- Pickup (M09)
    (gen_seed_public_id('perm'), 'pickup.read',           'pickup',        'read',       'pickup',   'View pickup requests, runs and attempts'),
    (gen_seed_public_id('perm'), 'pickup.create',         'pickup',        'create',     'pickup',   'Raise pickup requests'),
    (gen_seed_public_id('perm'), 'pickup.schedule',       'pickup',        'schedule',   'pickup',   'Schedule and reschedule pickups'),
    (gen_seed_public_id('perm'), 'pickup.assign',         'pickup',        'assign',     'pickup',   'Assign pickups to field agents and build pickup runs'),
    (gen_seed_public_id('perm'), 'pickup.respond',        'pickup',        'respond',    'pickup',   'Accept or reject an assigned pickup (field agent)'),
    (gen_seed_public_id('perm'), 'pickup.complete',       'pickup',        'complete',   'pickup',   'Record pickup arrival, completion and failure'),
    (gen_seed_public_id('perm'), 'pickup.cancel',         'pickup',        'cancel',     'pickup',   'Cancel a pickup request'),

    -- Scanning (M10)
    (gen_seed_public_id('perm'), 'scan.read',             'scan',          'read',       'scanning', 'View operational scan history'),
    (gen_seed_public_id('perm'), 'scan.inbound',          'scan',          'inbound',    'scanning', 'Perform RECEIVE and ARRIVAL scans'),
    (gen_seed_public_id('perm'), 'scan.outbound',         'scan',          'outbound',   'scanning', 'Perform DEPARTURE scans'),
    (gen_seed_public_id('perm'), 'scan.sort',             'scan',          'sort',       'scanning', 'Perform SORT scans'),
    (gen_seed_public_id('perm'), 'scan.hold',             'scan',          'hold',       'scanning', 'Place a shipment on hold and release it'),
    (gen_seed_public_id('perm'), 'scan.exception',        'scan',          'exception',  'scanning', 'Record DAMAGE and EXCEPTION scans'),

    -- Bagging (M11)
    (gen_seed_public_id('perm'), 'bag.read',              'bag',           'read',       'bagging',  'View bags and their contents'),
    (gen_seed_public_id('perm'), 'bag.manage',            'bag',           'manage',     'bagging',  'Create bags and add or remove shipments while OPEN'),
    (gen_seed_public_id('perm'), 'bag.close',             'bag',           'close',      'bagging',  'Close and seal a bag'),
    (gen_seed_public_id('perm'), 'bag.dispatch',          'bag',           'dispatch',   'bagging',  'Dispatch a closed bag'),
    (gen_seed_public_id('perm'), 'bag.receive',           'bag',           'receive',    'bagging',  'Receive an inbound bag'),
    (gen_seed_public_id('perm'), 'bag.open',              'bag',           'open',       'bagging',  'Open a received bag and break its seal'),
    (gen_seed_public_id('perm'), 'bag.reconcile',         'bag',           'reconcile',  'bagging',  'Reconcile bag contents against the closure snapshot'),
    (gen_seed_public_id('perm'), 'bag.exception_edit',    'bag',           'exception_edit','bagging','Correct a closed bag through the exception workflow'),

    -- Manifest (M12)
    (gen_seed_public_id('perm'), 'manifest.read',         'manifest',      'read',       'manifest', 'View manifests'),
    (gen_seed_public_id('perm'), 'manifest.manage',       'manifest',      'manage',     'manifest', 'Create draft manifests and manage their contents'),
    (gen_seed_public_id('perm'), 'manifest.close',        'manifest',      'close',      'manifest', 'Close a manifest and freeze its contents'),
    (gen_seed_public_id('perm'), 'manifest.dispatch',     'manifest',      'dispatch',   'manifest', 'Dispatch a closed manifest'),
    (gen_seed_public_id('perm'), 'manifest.receive',      'manifest',      'receive',    'manifest', 'Receive an inbound manifest'),
    (gen_seed_public_id('perm'), 'manifest.reconcile',    'manifest',      'reconcile',  'manifest', 'Reconcile a received manifest'),

    -- Line haul (M13)
    (gen_seed_public_id('perm'), 'carrier.read',          'carrier',       'read',       'linehaul', 'View carriers'),
    (gen_seed_public_id('perm'), 'carrier.manage',        'carrier',       'manage',     'linehaul', 'Create and update carriers'),
    (gen_seed_public_id('perm'), 'vehicle.read',          'vehicle',       'read',       'linehaul', 'View vehicles'),
    (gen_seed_public_id('perm'), 'vehicle.manage',        'vehicle',       'manage',     'linehaul', 'Create and update vehicles'),
    (gen_seed_public_id('perm'), 'driver.read',           'driver',        'read',       'linehaul', 'View drivers'),
    (gen_seed_public_id('perm'), 'driver.manage',         'driver',        'manage',     'linehaul', 'Create and update drivers'),
    (gen_seed_public_id('perm'), 'trip.read',             'trip',          'read',       'linehaul', 'View trips and legs'),
    (gen_seed_public_id('perm'), 'trip.manage',           'trip',          'manage',     'linehaul', 'Plan trips, attach manifests and assign crew'),
    (gen_seed_public_id('perm'), 'linehaul.depart',       'linehaul',      'depart',     'linehaul', 'Record trip and leg departure'),
    (gen_seed_public_id('perm'), 'linehaul.arrive',       'linehaul',      'arrive',     'linehaul', 'Record trip and leg arrival'),
    (gen_seed_public_id('perm'), 'trip.cancel',           'trip',          'cancel',     'linehaul', 'Cancel a planned trip'),

    -- Hub operations (M14)
    (gen_seed_public_id('perm'), 'hub.dashboard',         'hub',           'dashboard',  'hubops',   'View hub and branch workload summaries'),
    (gen_seed_public_id('perm'), 'exception.read',        'exception',     'read',       'hubops',   'View operational exceptions'),
    (gen_seed_public_id('perm'), 'exception.create',      'exception',     'create',     'hubops',   'Raise operational exceptions'),
    (gen_seed_public_id('perm'), 'exception.resolve',     'exception',     'resolve',    'hubops',   'Assign and resolve operational exceptions'),
    (gen_seed_public_id('perm'), 'reconciliation.manage', 'reconciliation','manage',     'hubops',   'Start and complete reconciliations'),

    -- Delivery (M15, M16)
    (gen_seed_public_id('perm'), 'delivery.read',         'delivery',      'read',       'delivery', 'View delivery runs and attempts'),
    (gen_seed_public_id('perm'), 'delivery.manage',       'delivery',      'manage',     'delivery', 'Create delivery runs and manage their stops'),
    (gen_seed_public_id('perm'), 'delivery.assign',       'delivery',      'assign',     'delivery', 'Assign a delivery run to an agent'),
    (gen_seed_public_id('perm'), 'delivery.dispatch',     'delivery',      'dispatch',   'delivery', 'Dispatch a delivery run and mark shipments out for delivery'),
    (gen_seed_public_id('perm'), 'delivery.complete',     'delivery',      'complete',   'delivery', 'Record delivery success or failure'),
    (gen_seed_public_id('perm'), 'delivery.otp_issue',    'delivery',      'otp_issue',  'delivery', 'Issue a delivery OTP to the consignee'),

    -- NDR (M17)
    (gen_seed_public_id('perm'), 'ndr.read',              'ndr',           'read',       'ndr',      'View NDR cases'),
    (gen_seed_public_id('perm'), 'ndr.manage',            'ndr',           'manage',     'ndr',      'Record NDR attempts and set the next action'),
    (gen_seed_public_id('perm'), 'ndr.config',            'ndr',           'config',     'ndr',      'Maintain the NDR reason catalogue'),

    -- RTO (M18)
    (gen_seed_public_id('perm'), 'rto.read',              'rto',           'read',       'rto',      'View RTO cases'),
    (gen_seed_public_id('perm'), 'rto.manage',            'rto',           'manage',     'rto',      'Initiate and progress RTO'),

    -- POD (M19)
    (gen_seed_public_id('perm'), 'pod.read',              'pod',           'read',       'pod',      'View proof of delivery and download its artifacts'),
    (gen_seed_public_id('perm'), 'pod.submit',            'pod',           'submit',     'pod',      'Submit proof of delivery with evidence'),

    -- Privileged operational overrides
    (gen_seed_public_id('perm'), 'shipment.declare_exception','shipment',  'declare_exception','shipment','Declare a shipment lost or damaged'),
    (gen_seed_public_id('perm'), 'shipment.override_custody', 'shipment',  'override_custody', 'shipment','Force a state change outside normal custody rules (always audited)');

-- ---------------------------------------------------------------------------
-- SUPER_ADMIN keeps every permission
-- ---------------------------------------------------------------------------
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND r.organization_id IS NULL
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Role grants
-- ---------------------------------------------------------------------------
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM (VALUES
    -- ORG_ADMIN: everything operational within the tenant.
    ('ORG_ADMIN','pickup.read'),('ORG_ADMIN','pickup.create'),('ORG_ADMIN','pickup.schedule'),
    ('ORG_ADMIN','pickup.assign'),('ORG_ADMIN','pickup.complete'),('ORG_ADMIN','pickup.cancel'),
    ('ORG_ADMIN','scan.read'),('ORG_ADMIN','scan.inbound'),('ORG_ADMIN','scan.outbound'),
    ('ORG_ADMIN','scan.sort'),('ORG_ADMIN','scan.hold'),('ORG_ADMIN','scan.exception'),
    ('ORG_ADMIN','bag.read'),('ORG_ADMIN','bag.manage'),('ORG_ADMIN','bag.close'),('ORG_ADMIN','bag.dispatch'),
    ('ORG_ADMIN','bag.receive'),('ORG_ADMIN','bag.open'),('ORG_ADMIN','bag.reconcile'),('ORG_ADMIN','bag.exception_edit'),
    ('ORG_ADMIN','manifest.read'),('ORG_ADMIN','manifest.manage'),('ORG_ADMIN','manifest.close'),
    ('ORG_ADMIN','manifest.dispatch'),('ORG_ADMIN','manifest.receive'),('ORG_ADMIN','manifest.reconcile'),
    ('ORG_ADMIN','carrier.read'),('ORG_ADMIN','carrier.manage'),('ORG_ADMIN','vehicle.read'),('ORG_ADMIN','vehicle.manage'),
    ('ORG_ADMIN','driver.read'),('ORG_ADMIN','driver.manage'),('ORG_ADMIN','trip.read'),('ORG_ADMIN','trip.manage'),
    ('ORG_ADMIN','linehaul.depart'),('ORG_ADMIN','linehaul.arrive'),('ORG_ADMIN','trip.cancel'),
    ('ORG_ADMIN','hub.dashboard'),('ORG_ADMIN','exception.read'),('ORG_ADMIN','exception.create'),
    ('ORG_ADMIN','exception.resolve'),('ORG_ADMIN','reconciliation.manage'),
    ('ORG_ADMIN','delivery.read'),('ORG_ADMIN','delivery.manage'),('ORG_ADMIN','delivery.assign'),
    ('ORG_ADMIN','delivery.dispatch'),('ORG_ADMIN','delivery.complete'),('ORG_ADMIN','delivery.otp_issue'),
    ('ORG_ADMIN','ndr.read'),('ORG_ADMIN','ndr.manage'),('ORG_ADMIN','ndr.config'),
    ('ORG_ADMIN','rto.read'),('ORG_ADMIN','rto.manage'),
    ('ORG_ADMIN','pod.read'),('ORG_ADMIN','pod.submit'),
    ('ORG_ADMIN','shipment.declare_exception'),('ORG_ADMIN','shipment.override_custody'),

    -- OPERATIONS_ADMIN: network-wide operations, no custody override.
    ('OPERATIONS_ADMIN','pickup.read'),('OPERATIONS_ADMIN','pickup.create'),('OPERATIONS_ADMIN','pickup.schedule'),
    ('OPERATIONS_ADMIN','pickup.assign'),('OPERATIONS_ADMIN','pickup.cancel'),
    ('OPERATIONS_ADMIN','scan.read'),('OPERATIONS_ADMIN','scan.inbound'),('OPERATIONS_ADMIN','scan.outbound'),
    ('OPERATIONS_ADMIN','scan.sort'),('OPERATIONS_ADMIN','scan.hold'),('OPERATIONS_ADMIN','scan.exception'),
    ('OPERATIONS_ADMIN','bag.read'),('OPERATIONS_ADMIN','bag.manage'),('OPERATIONS_ADMIN','bag.close'),
    ('OPERATIONS_ADMIN','bag.dispatch'),('OPERATIONS_ADMIN','bag.receive'),('OPERATIONS_ADMIN','bag.open'),
    ('OPERATIONS_ADMIN','bag.reconcile'),('OPERATIONS_ADMIN','bag.exception_edit'),
    ('OPERATIONS_ADMIN','manifest.read'),('OPERATIONS_ADMIN','manifest.manage'),('OPERATIONS_ADMIN','manifest.close'),
    ('OPERATIONS_ADMIN','manifest.dispatch'),('OPERATIONS_ADMIN','manifest.receive'),('OPERATIONS_ADMIN','manifest.reconcile'),
    ('OPERATIONS_ADMIN','carrier.read'),('OPERATIONS_ADMIN','carrier.manage'),
    ('OPERATIONS_ADMIN','vehicle.read'),('OPERATIONS_ADMIN','vehicle.manage'),
    ('OPERATIONS_ADMIN','driver.read'),('OPERATIONS_ADMIN','driver.manage'),
    ('OPERATIONS_ADMIN','trip.read'),('OPERATIONS_ADMIN','trip.manage'),
    ('OPERATIONS_ADMIN','linehaul.depart'),('OPERATIONS_ADMIN','linehaul.arrive'),('OPERATIONS_ADMIN','trip.cancel'),
    ('OPERATIONS_ADMIN','hub.dashboard'),('OPERATIONS_ADMIN','exception.read'),('OPERATIONS_ADMIN','exception.create'),
    ('OPERATIONS_ADMIN','exception.resolve'),('OPERATIONS_ADMIN','reconciliation.manage'),
    ('OPERATIONS_ADMIN','delivery.read'),('OPERATIONS_ADMIN','delivery.manage'),('OPERATIONS_ADMIN','delivery.assign'),
    ('OPERATIONS_ADMIN','delivery.dispatch'),('OPERATIONS_ADMIN','delivery.otp_issue'),
    ('OPERATIONS_ADMIN','ndr.read'),('OPERATIONS_ADMIN','ndr.manage'),('OPERATIONS_ADMIN','ndr.config'),
    ('OPERATIONS_ADMIN','rto.read'),('OPERATIONS_ADMIN','rto.manage'),
    ('OPERATIONS_ADMIN','pod.read'),
    ('OPERATIONS_ADMIN','shipment.declare_exception'),

    -- HUB_MANAGER: everything that happens inside a hub, within their units.
    ('HUB_MANAGER','scan.read'),('HUB_MANAGER','scan.inbound'),('HUB_MANAGER','scan.outbound'),
    ('HUB_MANAGER','scan.sort'),('HUB_MANAGER','scan.hold'),('HUB_MANAGER','scan.exception'),
    ('HUB_MANAGER','bag.read'),('HUB_MANAGER','bag.manage'),('HUB_MANAGER','bag.close'),('HUB_MANAGER','bag.dispatch'),
    ('HUB_MANAGER','bag.receive'),('HUB_MANAGER','bag.open'),('HUB_MANAGER','bag.reconcile'),
    ('HUB_MANAGER','manifest.read'),('HUB_MANAGER','manifest.manage'),('HUB_MANAGER','manifest.close'),
    ('HUB_MANAGER','manifest.dispatch'),('HUB_MANAGER','manifest.receive'),('HUB_MANAGER','manifest.reconcile'),
    ('HUB_MANAGER','carrier.read'),('HUB_MANAGER','vehicle.read'),('HUB_MANAGER','driver.read'),
    ('HUB_MANAGER','trip.read'),('HUB_MANAGER','trip.manage'),
    ('HUB_MANAGER','linehaul.depart'),('HUB_MANAGER','linehaul.arrive'),
    ('HUB_MANAGER','hub.dashboard'),('HUB_MANAGER','exception.read'),('HUB_MANAGER','exception.create'),
    ('HUB_MANAGER','exception.resolve'),('HUB_MANAGER','reconciliation.manage'),
    ('HUB_MANAGER','ndr.read'),('HUB_MANAGER','rto.read'),('HUB_MANAGER','pod.read'),
    ('HUB_MANAGER','shipment.declare_exception'),

    -- BRANCH_MANAGER: pickup, counter, bagging, last mile and NDR.
    ('BRANCH_MANAGER','pickup.read'),('BRANCH_MANAGER','pickup.create'),('BRANCH_MANAGER','pickup.schedule'),
    ('BRANCH_MANAGER','pickup.assign'),('BRANCH_MANAGER','pickup.complete'),('BRANCH_MANAGER','pickup.cancel'),
    ('BRANCH_MANAGER','scan.read'),('BRANCH_MANAGER','scan.inbound'),('BRANCH_MANAGER','scan.outbound'),
    ('BRANCH_MANAGER','scan.sort'),('BRANCH_MANAGER','scan.hold'),('BRANCH_MANAGER','scan.exception'),
    ('BRANCH_MANAGER','bag.read'),('BRANCH_MANAGER','bag.manage'),('BRANCH_MANAGER','bag.close'),
    ('BRANCH_MANAGER','bag.dispatch'),('BRANCH_MANAGER','bag.receive'),('BRANCH_MANAGER','bag.open'),
    ('BRANCH_MANAGER','bag.reconcile'),
    ('BRANCH_MANAGER','manifest.read'),('BRANCH_MANAGER','manifest.manage'),('BRANCH_MANAGER','manifest.close'),
    ('BRANCH_MANAGER','manifest.dispatch'),('BRANCH_MANAGER','manifest.receive'),('BRANCH_MANAGER','manifest.reconcile'),
    ('BRANCH_MANAGER','trip.read'),('BRANCH_MANAGER','linehaul.depart'),('BRANCH_MANAGER','linehaul.arrive'),
    ('BRANCH_MANAGER','hub.dashboard'),('BRANCH_MANAGER','exception.read'),('BRANCH_MANAGER','exception.create'),
    ('BRANCH_MANAGER','exception.resolve'),('BRANCH_MANAGER','reconciliation.manage'),
    ('BRANCH_MANAGER','delivery.read'),('BRANCH_MANAGER','delivery.manage'),('BRANCH_MANAGER','delivery.assign'),
    ('BRANCH_MANAGER','delivery.dispatch'),('BRANCH_MANAGER','delivery.complete'),('BRANCH_MANAGER','delivery.otp_issue'),
    ('BRANCH_MANAGER','ndr.read'),('BRANCH_MANAGER','ndr.manage'),
    ('BRANCH_MANAGER','rto.read'),('BRANCH_MANAGER','rto.manage'),
    ('BRANCH_MANAGER','pod.read'),('BRANCH_MANAGER','pod.submit'),
    ('BRANCH_MANAGER','shipment.declare_exception'),

    -- FRANCHISE_OWNER: runs a franchise branch, same shape as BRANCH_MANAGER
    -- minus network-level trip control.
    ('FRANCHISE_OWNER','pickup.read'),('FRANCHISE_OWNER','pickup.create'),('FRANCHISE_OWNER','pickup.schedule'),
    ('FRANCHISE_OWNER','pickup.assign'),('FRANCHISE_OWNER','pickup.complete'),('FRANCHISE_OWNER','pickup.cancel'),
    ('FRANCHISE_OWNER','scan.read'),('FRANCHISE_OWNER','scan.inbound'),('FRANCHISE_OWNER','scan.outbound'),
    ('FRANCHISE_OWNER','scan.sort'),('FRANCHISE_OWNER','scan.hold'),('FRANCHISE_OWNER','scan.exception'),
    ('FRANCHISE_OWNER','bag.read'),('FRANCHISE_OWNER','bag.manage'),('FRANCHISE_OWNER','bag.close'),
    ('FRANCHISE_OWNER','bag.dispatch'),('FRANCHISE_OWNER','bag.receive'),('FRANCHISE_OWNER','bag.open'),
    ('FRANCHISE_OWNER','bag.reconcile'),
    ('FRANCHISE_OWNER','manifest.read'),('FRANCHISE_OWNER','manifest.manage'),('FRANCHISE_OWNER','manifest.close'),
    ('FRANCHISE_OWNER','manifest.dispatch'),('FRANCHISE_OWNER','manifest.receive'),('FRANCHISE_OWNER','manifest.reconcile'),
    ('FRANCHISE_OWNER','trip.read'),
    ('FRANCHISE_OWNER','hub.dashboard'),('FRANCHISE_OWNER','exception.read'),('FRANCHISE_OWNER','exception.create'),
    ('FRANCHISE_OWNER','exception.resolve'),('FRANCHISE_OWNER','reconciliation.manage'),
    ('FRANCHISE_OWNER','delivery.read'),('FRANCHISE_OWNER','delivery.manage'),('FRANCHISE_OWNER','delivery.assign'),
    ('FRANCHISE_OWNER','delivery.dispatch'),('FRANCHISE_OWNER','delivery.complete'),('FRANCHISE_OWNER','delivery.otp_issue'),
    ('FRANCHISE_OWNER','ndr.read'),('FRANCHISE_OWNER','ndr.manage'),
    ('FRANCHISE_OWNER','rto.read'),('FRANCHISE_OWNER','rto.manage'),
    ('FRANCHISE_OWNER','pod.read'),('FRANCHISE_OWNER','pod.submit'),

    -- FRANCHISE_OPERATOR: counter staff. Can scan and bag; cannot close a
    -- manifest, dispatch a trip or resolve an exception.
    ('FRANCHISE_OPERATOR','pickup.read'),('FRANCHISE_OPERATOR','pickup.create'),('FRANCHISE_OPERATOR','pickup.schedule'),
    ('FRANCHISE_OPERATOR','scan.read'),('FRANCHISE_OPERATOR','scan.inbound'),('FRANCHISE_OPERATOR','scan.outbound'),
    ('FRANCHISE_OPERATOR','scan.sort'),
    ('FRANCHISE_OPERATOR','bag.read'),('FRANCHISE_OPERATOR','bag.manage'),('FRANCHISE_OPERATOR','bag.close'),
    ('FRANCHISE_OPERATOR','bag.receive'),('FRANCHISE_OPERATOR','bag.open'),
    ('FRANCHISE_OPERATOR','manifest.read'),('FRANCHISE_OPERATOR','manifest.manage'),('FRANCHISE_OPERATOR','manifest.receive'),
    ('FRANCHISE_OPERATOR','hub.dashboard'),('FRANCHISE_OPERATOR','exception.read'),('FRANCHISE_OPERATOR','exception.create'),
    ('FRANCHISE_OPERATOR','delivery.read'),('FRANCHISE_OPERATOR','ndr.read'),('FRANCHISE_OPERATOR','rto.read'),
    ('FRANCHISE_OPERATOR','pod.read'),

    -- FINANCE_MANAGER: read-only operational visibility, since operational
    -- facts drive commission and settlement in Release 3.
    ('FINANCE_MANAGER','pickup.read'),('FINANCE_MANAGER','scan.read'),('FINANCE_MANAGER','bag.read'),
    ('FINANCE_MANAGER','manifest.read'),('FINANCE_MANAGER','trip.read'),('FINANCE_MANAGER','carrier.read'),
    ('FINANCE_MANAGER','vehicle.read'),('FINANCE_MANAGER','driver.read'),
    ('FINANCE_MANAGER','hub.dashboard'),('FINANCE_MANAGER','exception.read'),
    ('FINANCE_MANAGER','delivery.read'),('FINANCE_MANAGER','ndr.read'),('FINANCE_MANAGER','rto.read'),
    ('FINANCE_MANAGER','pod.read'),

    -- CUSTOMER_SUPPORT: sees everything operational, changes only NDR
    -- instructions and RTO requests on the customer's behalf.
    ('CUSTOMER_SUPPORT','pickup.read'),('CUSTOMER_SUPPORT','pickup.create'),('CUSTOMER_SUPPORT','pickup.schedule'),
    ('CUSTOMER_SUPPORT','pickup.cancel'),
    ('CUSTOMER_SUPPORT','scan.read'),('CUSTOMER_SUPPORT','bag.read'),('CUSTOMER_SUPPORT','manifest.read'),
    ('CUSTOMER_SUPPORT','trip.read'),('CUSTOMER_SUPPORT','hub.dashboard'),
    ('CUSTOMER_SUPPORT','exception.read'),('CUSTOMER_SUPPORT','exception.create'),
    ('CUSTOMER_SUPPORT','delivery.read'),
    ('CUSTOMER_SUPPORT','ndr.read'),('CUSTOMER_SUPPORT','ndr.manage'),
    ('CUSTOMER_SUPPORT','rto.read'),('CUSTOMER_SUPPORT','rto.manage'),
    ('CUSTOMER_SUPPORT','pod.read'),

    -- PICKUP_AGENT: the narrowest useful set. Respond to what was assigned,
    -- record the visit, hand the parcels in at the branch.
    ('PICKUP_AGENT','pickup.read'),('PICKUP_AGENT','pickup.respond'),('PICKUP_AGENT','pickup.complete'),
    ('PICKUP_AGENT','scan.inbound'),('PICKUP_AGENT','scan.read'),

    -- DELIVERY_AGENT: complete the deliveries on their own run, record NDR
    -- attempts, submit POD. They cannot build or reassign a run.
    ('DELIVERY_AGENT','delivery.read'),('DELIVERY_AGENT','delivery.complete'),
    ('DELIVERY_AGENT','scan.read'),('DELIVERY_AGENT','scan.inbound'),
    ('DELIVERY_AGENT','ndr.read'),('DELIVERY_AGENT','ndr.manage'),
    ('DELIVERY_AGENT','pod.read'),('DELIVERY_AGENT','pod.submit'),
    ('DELIVERY_AGENT','rto.read'),

    -- CUSTOMER portal: raise a pickup, watch progress, ask for a reattempt or
    -- an RTO. Never an operational verb.
    ('CUSTOMER','pickup.read'),('CUSTOMER','pickup.create'),('CUSTOMER','pickup.cancel'),
    ('CUSTOMER','delivery.read'),('CUSTOMER','ndr.read'),('CUSTOMER','rto.read'),('CUSTOMER','pod.read')
) AS grants(role_code, permission_code)
JOIN roles r ON r.code = grants.role_code AND r.organization_id IS NULL
JOIN permissions p ON p.code = grants.permission_code
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Default NDR reasons for every existing tenant
-- ---------------------------------------------------------------------------
-- A tenant that has not configured its own catalogue still needs working
-- reasons on day one. New tenants get these from the provisioning path.
INSERT INTO ndr_reasons (public_id, organization_id, code, name, description, category,
                         default_action, max_attempts, is_customer_fault, requires_evidence,
                         auto_rto_after_max, display_order)
SELECT gen_seed_public_id('ndrs'), o.id, d.code, d.name, d.description, d.category,
       d.default_action, d.max_attempts, d.is_customer_fault, d.requires_evidence,
       d.auto_rto_after_max, d.display_order
FROM organizations o
CROSS JOIN (VALUES
    ('CUSTOMER_NOT_AVAILABLE','Customer not available','Nobody was present at the delivery address.','CUSTOMER_UNAVAILABLE','REATTEMPT',3,true,false,true,10),
    ('PREMISES_CLOSED','Premises closed','The business or residence was closed at the time of the attempt.','ACCESS','RESCHEDULE',3,false,false,true,20),
    ('ADDRESS_INCOMPLETE','Address incomplete','The address is missing details needed to locate it.','ADDRESS_PROBLEM','ADDRESS_CORRECTION',2,true,false,true,30),
    ('ADDRESS_NOT_FOUND','Address not found','The address could not be located.','ADDRESS_PROBLEM','ADDRESS_CORRECTION',2,true,true,true,40),
    ('CUSTOMER_REFUSED','Customer refused delivery','The consignee declined to accept the shipment.','REFUSED','RTO',1,true,true,true,50),
    ('COD_NOT_READY','COD amount not ready','The consignee could not pay the COD amount.','PAYMENT','REATTEMPT',2,true,false,true,60),
    ('CUSTOMER_RESCHEDULED','Customer asked to reschedule','The consignee requested delivery on another day.','CUSTOMER_UNAVAILABLE','RESCHEDULE',3,true,false,false,70),
    ('PHONE_UNREACHABLE','Consignee unreachable','The consignee could not be contacted by phone.','CUSTOMER_UNAVAILABLE','CONTACT_REQUIRED',3,true,false,true,80),
    ('RESTRICTED_ACCESS','Restricted access','The agent could not enter the premises or area.','ACCESS','CONTACT_REQUIRED',3,false,false,true,90),
    ('WEATHER_DISRUPTION','Weather disruption','Delivery could not be attempted because of weather.','WEATHER','REATTEMPT',5,false,false,false,100),
    ('VEHICLE_BREAKDOWN','Operational delay','The delivery could not be completed for operational reasons.','OPERATIONAL','REATTEMPT',5,false,false,false,110),
    ('SHIPMENT_DAMAGED','Shipment damaged','The shipment was found damaged before delivery.','DAMAGE','ESCALATE',1,false,true,false,120)
) AS d(code, name, description, category, default_action, max_attempts, is_customer_fault,
       requires_evidence, auto_rto_after_max, display_order)
ON CONFLICT DO NOTHING;
