-- 0012 Permission catalogue and system roles (M01).
--
-- Permissions are platform-wide reference data; roles listed here are system
-- roles shared by every tenant (organization_id NULL). Tenants may additionally
-- define their own roles, which reuse this same permission catalogue.
--
-- Constitution §10: roles are not a substitute for object-level authorization.
-- A permission grants the *ability* to attempt an action; the service layer
-- still verifies tenant ownership and operating-unit scope on every object.

INSERT INTO permissions (public_id, code, resource, action, module, description) VALUES
    -- Platform / tenancy
    (gen_seed_public_id('perm'), 'organization.read',        'organization',        'read',       'tenancy',   'View organization profile and settings'),
    (gen_seed_public_id('perm'), 'organization.update',      'organization',        'update',     'tenancy',   'Update organization profile and settings'),
    (gen_seed_public_id('perm'), 'organization.create',      'organization',        'create',     'tenancy',   'Create a new tenant organization (platform operator only)'),
    -- Users, roles, audit
    (gen_seed_public_id('perm'), 'user.read',                'user',                'read',       'auth',      'View users'),
    (gen_seed_public_id('perm'), 'user.create',              'user',                'create',     'auth',      'Create users'),
    (gen_seed_public_id('perm'), 'user.update',              'user',                'update',     'auth',      'Update user profile details'),
    (gen_seed_public_id('perm'), 'user.deactivate',          'user',                'deactivate', 'auth',      'Activate or deactivate users'),
    (gen_seed_public_id('perm'), 'user.assign_role',         'user',                'assign_role','auth',      'Grant and revoke role assignments'),
    (gen_seed_public_id('perm'), 'user.reset_password',      'user',                'reset_password','auth',   'Force a password reset for another user'),
    (gen_seed_public_id('perm'), 'user.revoke_session',      'user',                'revoke_session','auth',   'Revoke another user''s active sessions'),
    (gen_seed_public_id('perm'), 'role.read',                'role',                'read',       'auth',      'View roles and their permissions'),
    (gen_seed_public_id('perm'), 'role.create',              'role',                'create',     'auth',      'Create custom roles'),
    (gen_seed_public_id('perm'), 'role.update',              'role',                'update',     'auth',      'Update custom roles and their permissions'),
    (gen_seed_public_id('perm'), 'role.delete',              'role',                'delete',     'auth',      'Delete custom roles'),
    (gen_seed_public_id('perm'), 'permission.read',          'permission',          'read',       'auth',      'View the permission catalogue'),
    (gen_seed_public_id('perm'), 'audit.read',               'audit',               'read',       'audit',     'View the audit trail'),
    -- Network
    (gen_seed_public_id('perm'), 'region.read',              'region',              'read',       'network',   'View regions'),
    (gen_seed_public_id('perm'), 'region.manage',            'region',              'manage',     'network',   'Create, update and deactivate regions'),
    (gen_seed_public_id('perm'), 'operating_unit.read',      'operating_unit',      'read',       'network',   'View hubs and branches'),
    (gen_seed_public_id('perm'), 'operating_unit.manage',    'operating_unit',      'manage',     'network',   'Create, update and deactivate hubs and branches'),
    (gen_seed_public_id('perm'), 'franchise.read',           'franchise',           'read',       'network',   'View franchises'),
    (gen_seed_public_id('perm'), 'franchise.manage',         'franchise',           'manage',     'network',   'Create, update and terminate franchises'),
    (gen_seed_public_id('perm'), 'franchise_agreement.read', 'franchise_agreement', 'read',       'network',   'View franchise agreements'),
    (gen_seed_public_id('perm'), 'franchise_agreement.manage','franchise_agreement','manage',     'network',   'Create and update franchise agreements'),
    -- Geography
    (gen_seed_public_id('perm'), 'geography.read',           'geography',           'read',       'geography', 'Look up countries, states, cities, PIN codes'),
    (gen_seed_public_id('perm'), 'pincode.manage',           'pincode',             'manage',     'geography', 'Maintain the global PIN code dataset (platform operator only)'),
    (gen_seed_public_id('perm'), 'zone.read',                'zone',                'read',       'geography', 'View pricing zones'),
    (gen_seed_public_id('perm'), 'zone.manage',              'zone',                'manage',     'geography', 'Create and update pricing zones and PIN code mappings'),
    (gen_seed_public_id('perm'), 'import.read',              'import',              'read',       'geography', 'View bulk import jobs and their errors'),
    (gen_seed_public_id('perm'), 'import.create',            'import',              'create',     'geography', 'Start bulk imports'),
    -- Serviceability and routing
    (gen_seed_public_id('perm'), 'serviceability.check',     'serviceability',      'check',      'serviceability', 'Run serviceability and routing lookups'),
    (gen_seed_public_id('perm'), 'service_area.read',        'service_area',        'read',       'serviceability', 'View service areas'),
    (gen_seed_public_id('perm'), 'service_area.manage',      'service_area',        'manage',     'serviceability', 'Create and update service areas'),
    (gen_seed_public_id('perm'), 'route.read',               'route',               'read',       'serviceability', 'View routes and legs'),
    (gen_seed_public_id('perm'), 'route.manage',             'route',               'manage',     'serviceability', 'Create and update routes, rules and overrides'),
    (gen_seed_public_id('perm'), 'closure.manage',           'closure',             'manage',     'serviceability', 'Declare and cancel temporary closures'),
    (gen_seed_public_id('perm'), 'routing.debug',            'routing',             'debug',      'serviceability', 'Use the routing debug endpoint with full decision traces'),
    -- Products
    (gen_seed_public_id('perm'), 'courier_service.read',     'courier_service',     'read',       'product',   'View courier products'),
    (gen_seed_public_id('perm'), 'courier_service.manage',   'courier_service',     'manage',     'product',   'Create, update and deactivate courier products'),
    -- Pricing
    (gen_seed_public_id('perm'), 'pricing.quote',            'pricing',             'quote',      'pricing',   'Request a price quote'),
    (gen_seed_public_id('perm'), 'rate_card.read',           'rate_card',           'read',       'pricing',   'View rate cards and versions'),
    (gen_seed_public_id('perm'), 'rate_card.manage',         'rate_card',           'manage',     'pricing',   'Create and edit draft rate card versions'),
    (gen_seed_public_id('perm'), 'rate_card.activate',       'rate_card',           'activate',   'pricing',   'Activate or supersede a rate card version'),
    (gen_seed_public_id('perm'), 'tax_rule.manage',          'tax_rule',            'manage',     'pricing',   'Create and update tax rules'),
    -- Customers
    (gen_seed_public_id('perm'), 'customer.read',            'customer',            'read',       'customer',  'View customers'),
    (gen_seed_public_id('perm'), 'customer.create',          'customer',            'create',     'customer',  'Create customers'),
    (gen_seed_public_id('perm'), 'customer.update',          'customer',            'update',     'customer',  'Update customers, contacts and addresses'),
    (gen_seed_public_id('perm'), 'customer.suspend',         'customer',            'suspend',    'customer',  'Suspend or reinstate customers'),
    (gen_seed_public_id('perm'), 'customer_credit.manage',   'customer_credit',     'manage',     'customer',  'Set credit limits and credit status'),
    -- Shipments
    (gen_seed_public_id('perm'), 'shipment.read',            'shipment',            'read',       'shipment',  'View shipments within the caller''s scope'),
    (gen_seed_public_id('perm'), 'shipment.read_all',        'shipment',            'read_all',   'shipment',  'View every shipment in the organization, ignoring operating-unit scope'),
    (gen_seed_public_id('perm'), 'shipment.create',          'shipment',            'create',     'shipment',  'Book shipments'),
    (gen_seed_public_id('perm'), 'shipment.cancel',          'shipment',            'cancel',     'shipment',  'Cancel shipments'),
    (gen_seed_public_id('perm'), 'shipment.label',           'shipment',            'label',      'shipment',  'Generate shipment labels');

-- ---------------------------------------------------------------------------
-- System roles
-- ---------------------------------------------------------------------------
INSERT INTO roles (public_id, organization_id, code, name, description, is_system, scope_required) VALUES
    (gen_seed_public_id('rol'), NULL, 'SUPER_ADMIN',        'Platform Super Administrator', 'Full platform access across every tenant', true, false),
    (gen_seed_public_id('rol'), NULL, 'ORG_ADMIN',          'Organization Administrator',   'Full access within one organization', true, false),
    (gen_seed_public_id('rol'), NULL, 'OPERATIONS_ADMIN',   'Operations Administrator',     'Network, routing and product configuration', true, false),
    (gen_seed_public_id('rol'), NULL, 'HUB_MANAGER',        'Hub Manager',                  'Manages one or more hubs', true, true),
    (gen_seed_public_id('rol'), NULL, 'BRANCH_MANAGER',     'Branch Manager',               'Manages one or more branches', true, true),
    (gen_seed_public_id('rol'), NULL, 'FRANCHISE_OWNER',    'Franchise Owner',              'Owns a franchise branch', true, true),
    (gen_seed_public_id('rol'), NULL, 'FRANCHISE_OPERATOR', 'Franchise Operator',           'Counter staff at a franchise branch', true, true),
    (gen_seed_public_id('rol'), NULL, 'FINANCE_MANAGER',    'Finance Manager',              'Pricing, credit and financial reporting', true, false),
    (gen_seed_public_id('rol'), NULL, 'CUSTOMER_SUPPORT',   'Customer Support',             'Read-only operational visibility for support', true, false),
    (gen_seed_public_id('rol'), NULL, 'PICKUP_AGENT',       'Pickup Agent',                 'Field agent performing pickups', true, true),
    (gen_seed_public_id('rol'), NULL, 'DELIVERY_AGENT',     'Delivery Agent',               'Field agent performing deliveries', true, true),
    (gen_seed_public_id('rol'), NULL, 'CUSTOMER',           'Customer',                     'External customer portal user', true, false);

-- SUPER_ADMIN holds every permission by construction, so a new permission added
-- in a later migration is automatically available to the platform operator.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND r.organization_id IS NULL;

-- Every other role is granted an explicit list. Platform-only permissions
-- (organization.create, pincode.manage) are deliberately absent from ORG_ADMIN:
-- a tenant administrator must not be able to create tenants or rewrite the
-- shared PIN code dataset.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM (VALUES
    -- ORG_ADMIN
    ('ORG_ADMIN','organization.read'),('ORG_ADMIN','organization.update'),
    ('ORG_ADMIN','user.read'),('ORG_ADMIN','user.create'),('ORG_ADMIN','user.update'),
    ('ORG_ADMIN','user.deactivate'),('ORG_ADMIN','user.assign_role'),('ORG_ADMIN','user.reset_password'),
    ('ORG_ADMIN','user.revoke_session'),
    ('ORG_ADMIN','role.read'),('ORG_ADMIN','role.create'),('ORG_ADMIN','role.update'),('ORG_ADMIN','role.delete'),
    ('ORG_ADMIN','permission.read'),('ORG_ADMIN','audit.read'),
    ('ORG_ADMIN','region.read'),('ORG_ADMIN','region.manage'),
    ('ORG_ADMIN','operating_unit.read'),('ORG_ADMIN','operating_unit.manage'),
    ('ORG_ADMIN','franchise.read'),('ORG_ADMIN','franchise.manage'),
    ('ORG_ADMIN','franchise_agreement.read'),('ORG_ADMIN','franchise_agreement.manage'),
    ('ORG_ADMIN','geography.read'),('ORG_ADMIN','zone.read'),('ORG_ADMIN','zone.manage'),
    ('ORG_ADMIN','import.read'),('ORG_ADMIN','import.create'),
    ('ORG_ADMIN','serviceability.check'),('ORG_ADMIN','service_area.read'),('ORG_ADMIN','service_area.manage'),
    ('ORG_ADMIN','route.read'),('ORG_ADMIN','route.manage'),('ORG_ADMIN','closure.manage'),('ORG_ADMIN','routing.debug'),
    ('ORG_ADMIN','courier_service.read'),('ORG_ADMIN','courier_service.manage'),
    ('ORG_ADMIN','pricing.quote'),('ORG_ADMIN','rate_card.read'),('ORG_ADMIN','rate_card.manage'),
    ('ORG_ADMIN','rate_card.activate'),('ORG_ADMIN','tax_rule.manage'),
    ('ORG_ADMIN','customer.read'),('ORG_ADMIN','customer.create'),('ORG_ADMIN','customer.update'),
    ('ORG_ADMIN','customer.suspend'),('ORG_ADMIN','customer_credit.manage'),
    ('ORG_ADMIN','shipment.read'),('ORG_ADMIN','shipment.read_all'),('ORG_ADMIN','shipment.create'),
    ('ORG_ADMIN','shipment.cancel'),('ORG_ADMIN','shipment.label'),

    -- OPERATIONS_ADMIN
    ('OPERATIONS_ADMIN','organization.read'),('OPERATIONS_ADMIN','user.read'),('OPERATIONS_ADMIN','audit.read'),
    ('OPERATIONS_ADMIN','region.read'),('OPERATIONS_ADMIN','region.manage'),
    ('OPERATIONS_ADMIN','operating_unit.read'),('OPERATIONS_ADMIN','operating_unit.manage'),
    ('OPERATIONS_ADMIN','franchise.read'),('OPERATIONS_ADMIN','franchise_agreement.read'),
    ('OPERATIONS_ADMIN','geography.read'),('OPERATIONS_ADMIN','zone.read'),('OPERATIONS_ADMIN','zone.manage'),
    ('OPERATIONS_ADMIN','import.read'),('OPERATIONS_ADMIN','import.create'),
    ('OPERATIONS_ADMIN','serviceability.check'),('OPERATIONS_ADMIN','service_area.read'),
    ('OPERATIONS_ADMIN','service_area.manage'),('OPERATIONS_ADMIN','route.read'),('OPERATIONS_ADMIN','route.manage'),
    ('OPERATIONS_ADMIN','closure.manage'),('OPERATIONS_ADMIN','routing.debug'),
    ('OPERATIONS_ADMIN','courier_service.read'),('OPERATIONS_ADMIN','courier_service.manage'),
    ('OPERATIONS_ADMIN','pricing.quote'),('OPERATIONS_ADMIN','rate_card.read'),
    ('OPERATIONS_ADMIN','customer.read'),
    ('OPERATIONS_ADMIN','shipment.read'),('OPERATIONS_ADMIN','shipment.read_all'),
    ('OPERATIONS_ADMIN','shipment.create'),('OPERATIONS_ADMIN','shipment.cancel'),('OPERATIONS_ADMIN','shipment.label'),

    -- HUB_MANAGER
    ('HUB_MANAGER','operating_unit.read'),('HUB_MANAGER','region.read'),('HUB_MANAGER','franchise.read'),
    ('HUB_MANAGER','geography.read'),('HUB_MANAGER','zone.read'),
    ('HUB_MANAGER','serviceability.check'),('HUB_MANAGER','service_area.read'),('HUB_MANAGER','route.read'),
    ('HUB_MANAGER','courier_service.read'),('HUB_MANAGER','pricing.quote'),
    ('HUB_MANAGER','customer.read'),
    ('HUB_MANAGER','shipment.read'),('HUB_MANAGER','shipment.label'),

    -- BRANCH_MANAGER
    ('BRANCH_MANAGER','operating_unit.read'),('BRANCH_MANAGER','geography.read'),('BRANCH_MANAGER','zone.read'),
    ('BRANCH_MANAGER','serviceability.check'),('BRANCH_MANAGER','service_area.read'),('BRANCH_MANAGER','route.read'),
    ('BRANCH_MANAGER','courier_service.read'),('BRANCH_MANAGER','pricing.quote'),('BRANCH_MANAGER','rate_card.read'),
    ('BRANCH_MANAGER','customer.read'),('BRANCH_MANAGER','customer.create'),('BRANCH_MANAGER','customer.update'),
    ('BRANCH_MANAGER','shipment.read'),('BRANCH_MANAGER','shipment.create'),
    ('BRANCH_MANAGER','shipment.cancel'),('BRANCH_MANAGER','shipment.label'),

    -- FRANCHISE_OWNER
    ('FRANCHISE_OWNER','operating_unit.read'),('FRANCHISE_OWNER','franchise.read'),
    ('FRANCHISE_OWNER','franchise_agreement.read'),
    ('FRANCHISE_OWNER','geography.read'),('FRANCHISE_OWNER','zone.read'),
    ('FRANCHISE_OWNER','serviceability.check'),('FRANCHISE_OWNER','route.read'),
    ('FRANCHISE_OWNER','courier_service.read'),('FRANCHISE_OWNER','pricing.quote'),('FRANCHISE_OWNER','rate_card.read'),
    ('FRANCHISE_OWNER','customer.read'),('FRANCHISE_OWNER','customer.create'),('FRANCHISE_OWNER','customer.update'),
    ('FRANCHISE_OWNER','shipment.read'),('FRANCHISE_OWNER','shipment.create'),
    ('FRANCHISE_OWNER','shipment.cancel'),('FRANCHISE_OWNER','shipment.label'),

    -- FRANCHISE_OPERATOR
    ('FRANCHISE_OPERATOR','operating_unit.read'),('FRANCHISE_OPERATOR','geography.read'),
    ('FRANCHISE_OPERATOR','serviceability.check'),('FRANCHISE_OPERATOR','courier_service.read'),
    ('FRANCHISE_OPERATOR','pricing.quote'),
    ('FRANCHISE_OPERATOR','customer.read'),('FRANCHISE_OPERATOR','customer.create'),
    ('FRANCHISE_OPERATOR','shipment.read'),('FRANCHISE_OPERATOR','shipment.create'),('FRANCHISE_OPERATOR','shipment.label'),

    -- FINANCE_MANAGER
    ('FINANCE_MANAGER','organization.read'),('FINANCE_MANAGER','audit.read'),
    ('FINANCE_MANAGER','operating_unit.read'),('FINANCE_MANAGER','franchise.read'),
    ('FINANCE_MANAGER','franchise_agreement.read'),('FINANCE_MANAGER','franchise_agreement.manage'),
    ('FINANCE_MANAGER','geography.read'),('FINANCE_MANAGER','zone.read'),
    ('FINANCE_MANAGER','courier_service.read'),
    ('FINANCE_MANAGER','pricing.quote'),('FINANCE_MANAGER','rate_card.read'),('FINANCE_MANAGER','rate_card.manage'),
    ('FINANCE_MANAGER','rate_card.activate'),('FINANCE_MANAGER','tax_rule.manage'),
    ('FINANCE_MANAGER','customer.read'),('FINANCE_MANAGER','customer.update'),('FINANCE_MANAGER','customer_credit.manage'),
    ('FINANCE_MANAGER','shipment.read'),('FINANCE_MANAGER','shipment.read_all'),

    -- CUSTOMER_SUPPORT
    ('CUSTOMER_SUPPORT','operating_unit.read'),('CUSTOMER_SUPPORT','geography.read'),
    ('CUSTOMER_SUPPORT','serviceability.check'),('CUSTOMER_SUPPORT','route.read'),
    ('CUSTOMER_SUPPORT','courier_service.read'),('CUSTOMER_SUPPORT','pricing.quote'),
    ('CUSTOMER_SUPPORT','customer.read'),('CUSTOMER_SUPPORT','customer.update'),
    ('CUSTOMER_SUPPORT','shipment.read'),('CUSTOMER_SUPPORT','shipment.read_all'),('CUSTOMER_SUPPORT','shipment.label'),

    -- PICKUP_AGENT / DELIVERY_AGENT (their operational verbs arrive in Release 2)
    ('PICKUP_AGENT','shipment.read'),('PICKUP_AGENT','geography.read'),('PICKUP_AGENT','operating_unit.read'),
    ('DELIVERY_AGENT','shipment.read'),('DELIVERY_AGENT','geography.read'),('DELIVERY_AGENT','operating_unit.read'),

    -- CUSTOMER portal
    ('CUSTOMER','serviceability.check'),('CUSTOMER','pricing.quote'),('CUSTOMER','geography.read'),
    ('CUSTOMER','courier_service.read'),
    ('CUSTOMER','shipment.read'),('CUSTOMER','shipment.create'),('CUSTOMER','shipment.cancel'),('CUSTOMER','shipment.label')
) AS grants(role_code, permission_code)
JOIN roles r ON r.code = grants.role_code AND r.organization_id IS NULL
JOIN permissions p ON p.code = grants.permission_code;
