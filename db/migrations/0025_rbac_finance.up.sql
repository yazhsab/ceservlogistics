-- 0025 Release 3 finance permission catalogue and role grants.
--
-- Two principles specific to finance, on top of the ones 0012 and 0019 already
-- established:
--
--   * Maker and checker are separate permissions, never granted together to a
--     role that is expected to do both halves. `settlement.calculate` and
--     `settlement.approve` are distinct; a FINANCE_MANAGER holds both because
--     an organization may have one finance person, but the *rows* still refuse
--     the same human to be both (see the maker_is_not_checker constraints in
--     0022, 0023 and 0024). Permission grants control who may act; the database
--     constraint controls that two different people acted.
--
--   * A franchise sees its own money and nothing else. FRANCHISE_OWNER gets
--     read permissions only — no posting, no approval, no adjustment. Their
--     operating-unit scope, applied by the existing tenancy layer, narrows the
--     rows further.

INSERT INTO permissions (public_id, code, resource, action, module, description) VALUES
    -- Ledger (M22)
    (gen_seed_public_id('perm'), 'ledger.read',              'ledger',     'read',       'ledger',     'View accounts, journals, statements and the trial balance'),
    (gen_seed_public_id('perm'), 'ledger.account_manage',    'ledger',     'account_manage','ledger',  'Create and edit chart-of-accounts entries'),
    (gen_seed_public_id('perm'), 'ledger.post',              'ledger',     'post',       'ledger',     'Post a manual journal transaction'),
    (gen_seed_public_id('perm'), 'ledger.reverse',           'ledger',     'reverse',    'ledger',     'Reverse a posted journal transaction'),
    (gen_seed_public_id('perm'), 'ledger.period_manage',     'ledger',     'period_manage','ledger',   'Open, close and reopen accounting periods'),

    -- Commission (M21)
    (gen_seed_public_id('perm'), 'commission.read',          'commission', 'read',       'commission', 'View commission rules, versions and calculations'),
    (gen_seed_public_id('perm'), 'commission.config',        'commission', 'config',     'commission', 'Create and version commission schemes and rules'),
    (gen_seed_public_id('perm'), 'commission.simulate',      'commission', 'simulate',   'commission', 'Run a commission simulation without posting'),
    (gen_seed_public_id('perm'), 'commission.calculate',     'commission', 'calculate',  'commission', 'Calculate commission for qualifying events'),
    (gen_seed_public_id('perm'), 'commission.post',          'commission', 'post',       'commission', 'Post calculated commission to the ledger'),
    (gen_seed_public_id('perm'), 'commission.reverse',       'commission', 'reverse',    'commission', 'Reverse a posted commission'),

    -- COD (M23)
    (gen_seed_public_id('perm'), 'cod.read',                 'cod',        'read',       'cod',        'View COD obligations, custody and reconciliations'),
    (gen_seed_public_id('perm'), 'cod.collect',              'cod',        'collect',    'cod',        'Record COD collected at the doorstep'),
    (gen_seed_public_id('perm'), 'cod.transfer',             'cod',        'transfer',   'cod',        'Declare a COD custody hand-off'),
    (gen_seed_public_id('perm'), 'cod.accept',               'cod',        'accept',     'cod',        'Accept an incoming COD custody hand-off'),
    (gen_seed_public_id('perm'), 'cod.reconcile',            'cod',        'reconcile',  'cod',        'Open and complete a COD reconciliation'),
    (gen_seed_public_id('perm'), 'cod.remit',                'cod',        'remit',      'cod',        'Record and confirm a COD remittance'),
    (gen_seed_public_id('perm'), 'cod.adjust_request',       'cod',        'adjust_request','cod',     'Raise a COD adjustment for approval (maker)'),
    (gen_seed_public_id('perm'), 'cod.adjust_approve',       'cod',        'adjust_approve','cod',     'Approve or reject a COD adjustment (checker)'),
    (gen_seed_public_id('perm'), 'cod.dispute',              'cod',        'dispute',    'cod',        'Raise and resolve COD disputes'),

    -- Settlement (M24)
    (gen_seed_public_id('perm'), 'settlement.read',          'settlement', 'read',       'settlement', 'View settlements, lines and payments'),
    (gen_seed_public_id('perm'), 'settlement.calculate',     'settlement', 'calculate',  'settlement', 'Generate and calculate a settlement (maker)'),
    (gen_seed_public_id('perm'), 'settlement.submit',        'settlement', 'submit',     'settlement', 'Submit a calculated settlement for review'),
    (gen_seed_public_id('perm'), 'settlement.approve',       'settlement', 'approve',    'settlement', 'Approve or reject a settlement (checker)'),
    (gen_seed_public_id('perm'), 'settlement.pay',           'settlement', 'pay',        'settlement', 'Record a settlement payment'),
    (gen_seed_public_id('perm'), 'settlement.adjust_request', 'settlement','adjust_request','settlement','Raise a settlement adjustment (maker)'),
    (gen_seed_public_id('perm'), 'settlement.adjust_approve', 'settlement','adjust_approve','settlement','Approve a settlement adjustment (checker)'),
    (gen_seed_public_id('perm'), 'settlement.cancel',        'settlement', 'cancel',     'settlement', 'Cancel a settlement before it is approved'),

    -- Billing (M25)
    (gen_seed_public_id('perm'), 'invoice.read',             'invoice',    'read',       'billing',    'View invoices, notes and payments'),
    (gen_seed_public_id('perm'), 'invoice.create',           'invoice',    'create',     'billing',    'Create draft invoices and run billing cycles'),
    (gen_seed_public_id('perm'), 'invoice.issue',            'invoice',    'issue',      'billing',    'Issue an invoice, allocating its statutory number'),
    (gen_seed_public_id('perm'), 'invoice.cancel',           'invoice',    'cancel',     'billing',    'Cancel an issued invoice'),
    (gen_seed_public_id('perm'), 'invoice.payment',          'invoice',    'payment',    'billing',    'Record a customer payment against an invoice'),
    (gen_seed_public_id('perm'), 'creditnote.create',        'creditnote', 'create',     'billing',    'Raise a credit or debit note (maker)'),
    (gen_seed_public_id('perm'), 'creditnote.approve',       'creditnote', 'approve',    'billing',    'Approve and issue a credit or debit note (checker)'),
    (gen_seed_public_id('perm'), 'tax.config',               'tax',        'config',     'billing',    'Configure tax components and rates')
ON CONFLICT (code) DO NOTHING;

-- SUPER_ADMIN holds everything by construction.
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
    -- ORG_ADMIN: full finance authority inside the tenant. Holding both halves
    -- of a maker/checker pair is permitted; performing both halves is not,
    -- because the row constraints require two distinct users.
    ('ORG_ADMIN','ledger.read'),('ORG_ADMIN','ledger.account_manage'),('ORG_ADMIN','ledger.post'),
    ('ORG_ADMIN','ledger.reverse'),('ORG_ADMIN','ledger.period_manage'),
    ('ORG_ADMIN','commission.read'),('ORG_ADMIN','commission.config'),('ORG_ADMIN','commission.simulate'),
    ('ORG_ADMIN','commission.calculate'),('ORG_ADMIN','commission.post'),('ORG_ADMIN','commission.reverse'),
    ('ORG_ADMIN','cod.read'),('ORG_ADMIN','cod.transfer'),('ORG_ADMIN','cod.accept'),
    ('ORG_ADMIN','cod.reconcile'),('ORG_ADMIN','cod.remit'),
    ('ORG_ADMIN','cod.adjust_request'),('ORG_ADMIN','cod.adjust_approve'),('ORG_ADMIN','cod.dispute'),
    ('ORG_ADMIN','settlement.read'),('ORG_ADMIN','settlement.calculate'),('ORG_ADMIN','settlement.submit'),
    ('ORG_ADMIN','settlement.approve'),('ORG_ADMIN','settlement.pay'),
    ('ORG_ADMIN','settlement.adjust_request'),('ORG_ADMIN','settlement.adjust_approve'),('ORG_ADMIN','settlement.cancel'),
    ('ORG_ADMIN','invoice.read'),('ORG_ADMIN','invoice.create'),('ORG_ADMIN','invoice.issue'),
    ('ORG_ADMIN','invoice.cancel'),('ORG_ADMIN','invoice.payment'),
    ('ORG_ADMIN','creditnote.create'),('ORG_ADMIN','creditnote.approve'),('ORG_ADMIN','tax.config'),

    -- FINANCE_MANAGER: the finance function. Everything except reshaping the
    -- chart of accounts, which is an ORG_ADMIN act.
    ('FINANCE_MANAGER','ledger.read'),('FINANCE_MANAGER','ledger.post'),('FINANCE_MANAGER','ledger.reverse'),
    ('FINANCE_MANAGER','ledger.period_manage'),
    ('FINANCE_MANAGER','commission.read'),('FINANCE_MANAGER','commission.config'),
    ('FINANCE_MANAGER','commission.simulate'),('FINANCE_MANAGER','commission.calculate'),
    ('FINANCE_MANAGER','commission.post'),('FINANCE_MANAGER','commission.reverse'),
    ('FINANCE_MANAGER','cod.read'),('FINANCE_MANAGER','cod.accept'),('FINANCE_MANAGER','cod.reconcile'),
    ('FINANCE_MANAGER','cod.remit'),('FINANCE_MANAGER','cod.adjust_request'),
    ('FINANCE_MANAGER','cod.adjust_approve'),('FINANCE_MANAGER','cod.dispute'),
    ('FINANCE_MANAGER','settlement.read'),('FINANCE_MANAGER','settlement.calculate'),
    ('FINANCE_MANAGER','settlement.submit'),('FINANCE_MANAGER','settlement.approve'),
    ('FINANCE_MANAGER','settlement.pay'),('FINANCE_MANAGER','settlement.adjust_request'),
    ('FINANCE_MANAGER','settlement.adjust_approve'),('FINANCE_MANAGER','settlement.cancel'),
    ('FINANCE_MANAGER','invoice.read'),('FINANCE_MANAGER','invoice.create'),('FINANCE_MANAGER','invoice.issue'),
    ('FINANCE_MANAGER','invoice.cancel'),('FINANCE_MANAGER','invoice.payment'),
    ('FINANCE_MANAGER','creditnote.create'),('FINANCE_MANAGER','creditnote.approve'),('FINANCE_MANAGER','tax.config'),

    -- OPERATIONS_ADMIN: sees the money its operations generate, and moves COD
    -- custody, but does not post journals, approve settlements or issue
    -- invoices. Separation of duties between operations and finance.
    ('OPERATIONS_ADMIN','ledger.read'),
    ('OPERATIONS_ADMIN','commission.read'),('OPERATIONS_ADMIN','commission.simulate'),
    ('OPERATIONS_ADMIN','cod.read'),('OPERATIONS_ADMIN','cod.transfer'),('OPERATIONS_ADMIN','cod.accept'),
    ('OPERATIONS_ADMIN','cod.reconcile'),('OPERATIONS_ADMIN','cod.dispute'),
    ('OPERATIONS_ADMIN','settlement.read'),
    ('OPERATIONS_ADMIN','invoice.read'),

    -- BRANCH_MANAGER: accepts cash from agents, reconciles it, hands it on.
    -- No adjustments: a branch cannot forgive its own shortage.
    ('BRANCH_MANAGER','cod.read'),('BRANCH_MANAGER','cod.accept'),('BRANCH_MANAGER','cod.transfer'),
    ('BRANCH_MANAGER','cod.reconcile'),('BRANCH_MANAGER','cod.dispute'),
    ('BRANCH_MANAGER','commission.read'),
    ('BRANCH_MANAGER','settlement.read'),
    ('BRANCH_MANAGER','invoice.read'),

    -- HUB_MANAGER: same custody role as a branch.
    ('HUB_MANAGER','cod.read'),('HUB_MANAGER','cod.accept'),('HUB_MANAGER','cod.transfer'),
    ('HUB_MANAGER','cod.reconcile'),('HUB_MANAGER','cod.dispute'),
    ('HUB_MANAGER','settlement.read'),

    -- FRANCHISE_OWNER: reads its own money, confirms cash it is holding, and
    -- disputes what it disagrees with. It cannot approve its own settlement,
    -- adjust its own shortage, or post to the ledger.
    ('FRANCHISE_OWNER','cod.read'),('FRANCHISE_OWNER','cod.accept'),('FRANCHISE_OWNER','cod.transfer'),
    ('FRANCHISE_OWNER','cod.remit'),('FRANCHISE_OWNER','cod.dispute'),
    ('FRANCHISE_OWNER','commission.read'),
    ('FRANCHISE_OWNER','settlement.read'),
    ('FRANCHISE_OWNER','invoice.read'),

    -- FRANCHISE_OPERATOR: day-to-day cash handling only.
    ('FRANCHISE_OPERATOR','cod.read'),('FRANCHISE_OPERATOR','cod.accept'),('FRANCHISE_OPERATOR','cod.transfer'),
    ('FRANCHISE_OPERATOR','commission.read'),
    ('FRANCHISE_OPERATOR','settlement.read'),

    -- DELIVERY_AGENT: records the cash it takes at the door and declares the
    -- hand-in. Nothing else. It cannot see another agent's collections, cannot
    -- reconcile, and cannot adjust.
    ('DELIVERY_AGENT','cod.collect'),('DELIVERY_AGENT','cod.transfer'),

    -- CUSTOMER_SUPPORT: needs to answer "where is my money" questions.
    ('CUSTOMER_SUPPORT','cod.read'),('CUSTOMER_SUPPORT','invoice.read'),

    -- CUSTOMER: their own invoices, through the portal.
    ('CUSTOMER','invoice.read')
) AS grants(role_code, permission_code)
JOIN roles r ON r.code = grants.role_code AND r.organization_id IS NULL
JOIN permissions p ON p.code = grants.permission_code
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- An open accounting period for the current month, per organization, so a
-- fresh tenant can post on day one without a finance user setting one up.
-- Subsequent periods are created by the application.
-- ---------------------------------------------------------------------------
INSERT INTO accounting_periods (public_id, organization_id, code, starts_on, ends_on, status)
SELECT gen_seed_public_id('acp'), o.id,
       to_char(now(), 'YYYY-MM'),
       date_trunc('month', now())::date,
       (date_trunc('month', now()) + interval '1 month - 1 day')::date,
       'OPEN'
FROM organizations o
ON CONFLICT DO NOTHING;
