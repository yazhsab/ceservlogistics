-- Cash/POS/transfer collected by a franchise counter for prepaid shipments.
-- This is distinct from COD: the customer pays at origin and the franchise
-- owes the network, while COD is collected from the consignee at destination.

CREATE TABLE franchise_collections (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id           text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'fcl')),
    organization_id     bigint      NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    shipment_id         bigint      NOT NULL REFERENCES shipments(id) ON DELETE RESTRICT,
    franchise_id        bigint      NOT NULL REFERENCES franchises(id) ON DELETE RESTRICT,
    operating_unit_id   bigint      NOT NULL REFERENCES operating_units(id) ON DELETE RESTRICT,
    customer_id         bigint      NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    amount_minor        bigint      NOT NULL CHECK (amount_minor > 0),
    currency            char(3)     NOT NULL,
    payment_mode        text        NOT NULL CHECK (payment_mode IN ('CASH','POS','TRANSFER','BANK_DEPOSIT')),
    reference           text,
    status              text        NOT NULL DEFAULT 'COLLECTED'
                        CHECK (status IN ('COLLECTED','IN_SETTLEMENT','REMITTED','DISPUTED','REVERSED')),
    collected_at        timestamptz NOT NULL,
    collected_by        bigint      NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    settlement_id       bigint      REFERENCES settlements(id) ON DELETE RESTRICT,
    remitted_at         timestamptz,
    notes               text,
    request_id          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT franchise_collections_shipment_unique UNIQUE (organization_id, shipment_id),
    CONSTRAINT franchise_collections_reference_unique UNIQUE NULLS NOT DISTINCT
        (organization_id, franchise_id, reference)
);
CREATE INDEX franchise_collections_franchise_idx
    ON franchise_collections (organization_id, franchise_id, collected_at DESC, id DESC);
CREATE INDEX franchise_collections_settlement_pool_idx
    ON franchise_collections (organization_id, franchise_id, collected_at, id)
    WHERE status = 'COLLECTED' AND settlement_id IS NULL;
CREATE TRIGGER franchise_collections_touch BEFORE UPDATE ON franchise_collections
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE settlements ADD COLUMN collections_minor bigint NOT NULL DEFAULT 0;

ALTER TABLE settlement_lines DROP CONSTRAINT settlement_lines_category_check;
ALTER TABLE settlement_lines ADD CONSTRAINT settlement_lines_category_check CHECK (category IN
    ('BOOKING_COMMISSION','PICKUP_COMMISSION','ORIGIN_HANDLING','TRANSIT_HANDLING',
     'DESTINATION_HANDLING','DELIVERY_COMMISSION','COD_COMMISSION','VOLUME_INCENTIVE','CUSTOM_COMMISSION',
     'COD_LIABILITY','CUSTOMER_COLLECTION','CHARGE','PENALTY','INCENTIVE','ADJUSTMENT','TAX','WITHHOLDING',
     'OPENING_BALANCE'));

ALTER TABLE settlement_lines DROP CONSTRAINT settlement_lines_source_type_check;
ALTER TABLE settlement_lines ADD CONSTRAINT settlement_lines_source_type_check CHECK (source_type IN
    ('COMMISSION_CALCULATION','COD_OBLIGATION','COD_ADJUSTMENT','FRANCHISE_COLLECTION',
     'SETTLEMENT_ADJUSTMENT','PREVIOUS_SETTLEMENT','TAX_RULE','MANUAL'));

INSERT INTO permissions (public_id, code, resource, module, action, description) VALUES
    (gen_seed_public_id('perm'),'collection.read','collection','finance','read','View franchise counter collections'),
    (gen_seed_public_id('perm'),'collection.record','collection','finance','create','Record customer payment collected at a franchise'),
    (gen_seed_public_id('perm'),'collection.manage','collection','finance','manage','Resolve and reconcile franchise collections')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.code IN ('collection.read','collection.record')
WHERE r.code IN ('FRANCHISE_OWNER','FRANCHISE_OPERATOR') AND r.organization_id IS NULL
ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.code IN ('collection.read','collection.manage')
WHERE r.code IN ('ORG_ADMIN','FINANCE_MANAGER') AND r.organization_id IS NULL
ON CONFLICT DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'SUPER_ADMIN' AND r.organization_id IS NULL
ON CONFLICT DO NOTHING;
