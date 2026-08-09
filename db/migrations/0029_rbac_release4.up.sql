-- 0029 Release 4 permission catalogue and role grants.
--
-- Two things are new in this release's authorization model.
--
-- First, the **portal roles**. CUSTOMER and FRANCHISE_OWNER now have real
-- surfaces of their own, and their permissions are deliberately narrow: a
-- portal permission grants access to *your own* data, and the scoping that
-- makes "your own" true lives in the service layer, not in the permission.
-- `portal.customer` does not mean "read customers"; it means "use the customer
-- portal, which only ever shows you yourself".
--
-- Second, **partner API scopes are not permissions**. A partner key carries
-- scopes (`shipment:create`), checked against the key; a user carries
-- permissions (`apikey.manage`), checked against their roles. They are separate
-- systems because a partner is not a user and must never acquire one's rights.

INSERT INTO permissions (public_id, code, resource, action, module, description) VALUES
    -- Notifications (M26)
    (gen_seed_public_id('perm'), 'notification.read',      'notification', 'read',    'notification', 'View notifications and their delivery attempts'),
    (gen_seed_public_id('perm'), 'notification.send',      'notification', 'send',    'notification', 'Raise a notification manually'),
    (gen_seed_public_id('perm'), 'notification.template',  'notification', 'template','notification', 'Create and edit notification templates'),
    (gen_seed_public_id('perm'), 'notification.preference','notification', 'preference','notification','Manage notification preferences and subscriptions'),
    (gen_seed_public_id('perm'), 'notification.retry',     'notification', 'retry',   'notification', 'Retry or cancel a failed notification'),

    -- Portals (M27, M28, M29)
    (gen_seed_public_id('perm'), 'portal.customer',        'portal',       'customer','portal',       'Use the customer portal, scoped to your own account'),
    (gen_seed_public_id('perm'), 'portal.franchise',       'portal',       'franchise','portal',      'Use the franchise portal, scoped to your own franchise'),
    (gen_seed_public_id('perm'), 'portal.console',         'portal',       'console', 'portal',       'Use the hub and branch console'),

    -- Command centre (M30)
    (gen_seed_public_id('perm'), 'command.read',           'command',      'read',    'analytics',    'View operational KPIs and the command centre'),

    -- Reporting (M31)
    (gen_seed_public_id('perm'), 'report.read',            'report',       'read',    'reporting',    'List reports and download completed ones'),
    (gen_seed_public_id('perm'), 'report.run',             'report',       'run',     'reporting',    'Request a report run'),
    (gen_seed_public_id('perm'), 'report.finance',         'report',       'finance', 'reporting',    'Run reports containing financial data'),

    -- Partner API and webhooks (M32)
    (gen_seed_public_id('perm'), 'apikey.read',            'apikey',       'read',    'partner',      'View API keys and their usage'),
    (gen_seed_public_id('perm'), 'apikey.manage',          'apikey',       'manage',  'partner',      'Issue and revoke API keys'),
    (gen_seed_public_id('perm'), 'webhook.read',           'webhook',      'read',    'partner',      'View webhook endpoints and delivery history'),
    (gen_seed_public_id('perm'), 'webhook.manage',         'webhook',      'manage',  'partner',      'Create, pause and delete webhook endpoints'),
    (gen_seed_public_id('perm'), 'webhook.replay',         'webhook',      'replay',  'partner',      'Replay a webhook delivery')
ON CONFLICT (code) DO NOTHING;

-- SUPER_ADMIN holds everything by construction.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND r.organization_id IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM (VALUES
    -- ORG_ADMIN: the whole product surface within the tenant.
    ('ORG_ADMIN','notification.read'),('ORG_ADMIN','notification.send'),
    ('ORG_ADMIN','notification.template'),('ORG_ADMIN','notification.preference'),
    ('ORG_ADMIN','notification.retry'),
    ('ORG_ADMIN','portal.console'),('ORG_ADMIN','command.read'),
    ('ORG_ADMIN','report.read'),('ORG_ADMIN','report.run'),('ORG_ADMIN','report.finance'),
    ('ORG_ADMIN','apikey.read'),('ORG_ADMIN','apikey.manage'),
    ('ORG_ADMIN','webhook.read'),('ORG_ADMIN','webhook.manage'),('ORG_ADMIN','webhook.replay'),

    -- OPERATIONS_ADMIN: runs the network. Reads finance reports but cannot
    -- issue partner credentials, which is a security-administration act.
    ('OPERATIONS_ADMIN','notification.read'),('OPERATIONS_ADMIN','notification.send'),
    ('OPERATIONS_ADMIN','notification.retry'),
    ('OPERATIONS_ADMIN','portal.console'),('OPERATIONS_ADMIN','command.read'),
    ('OPERATIONS_ADMIN','report.read'),('OPERATIONS_ADMIN','report.run'),
    ('OPERATIONS_ADMIN','webhook.read'),

    -- FINANCE_MANAGER: finance reporting, and the notifications finance sends.
    ('FINANCE_MANAGER','notification.read'),('FINANCE_MANAGER','notification.send'),
    ('FINANCE_MANAGER','command.read'),
    ('FINANCE_MANAGER','report.read'),('FINANCE_MANAGER','report.run'),('FINANCE_MANAGER','report.finance'),

    -- HUB_MANAGER and BRANCH_MANAGER: the console and their own numbers.
    ('HUB_MANAGER','portal.console'),('HUB_MANAGER','command.read'),
    ('HUB_MANAGER','report.read'),('HUB_MANAGER','report.run'),
    ('HUB_MANAGER','notification.read'),
    ('BRANCH_MANAGER','portal.console'),('BRANCH_MANAGER','command.read'),
    ('BRANCH_MANAGER','report.read'),('BRANCH_MANAGER','report.run'),
    ('BRANCH_MANAGER','notification.read'),

    -- FRANCHISE_OWNER: the franchise portal and reports about themselves.
    -- Scoping to their own franchise is enforced in the service, not here.
    ('FRANCHISE_OWNER','portal.franchise'),
    ('FRANCHISE_OWNER','report.read'),('FRANCHISE_OWNER','report.run'),
    ('FRANCHISE_OWNER','notification.read'),('FRANCHISE_OWNER','notification.preference'),

    -- FRANCHISE_OPERATOR: day-to-day franchise work, no reporting.
    ('FRANCHISE_OPERATOR','portal.franchise'),('FRANCHISE_OPERATOR','portal.console'),

    -- CUSTOMER_SUPPORT: needs to see what a customer was told and resend it.
    ('CUSTOMER_SUPPORT','notification.read'),('CUSTOMER_SUPPORT','notification.send'),
    ('CUSTOMER_SUPPORT','notification.retry'),
    ('CUSTOMER_SUPPORT','command.read'),('CUSTOMER_SUPPORT','report.read'),

    -- CUSTOMER: the customer portal only. Every endpoint behind it is scoped
    -- to the calling user's own customer account.
    ('CUSTOMER','portal.customer'),('CUSTOMER','notification.preference'),

    -- Field agents get the console; the payloads it returns are already
    -- narrowed to what a scanner needs.
    ('PICKUP_AGENT','portal.console'),
    ('DELIVERY_AGENT','portal.console')
) AS grants(role_code, permission_code)
JOIN roles r ON r.code = grants.role_code AND r.organization_id IS NULL
JOIN permissions p ON p.code = grants.permission_code
ON CONFLICT DO NOTHING;
