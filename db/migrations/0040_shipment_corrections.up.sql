-- Safe pre-pickup shipment corrections. The original booking snapshots remain
-- append-only; corrected presentation fields are layered on top so an audit can
-- always reconstruct both what was booked and what was corrected.

CREATE TABLE shipment_address_corrections (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id            text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'shc')),
    organization_id      bigint      NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    shipment_id          bigint      NOT NULL REFERENCES shipments(id) ON DELETE RESTRICT,
    role                 text        NOT NULL CHECK (role IN ('SENDER','RECIPIENT')),
    sequence             integer     NOT NULL CHECK (sequence >= 1),
    contact_name         text        NOT NULL,
    company_name         text,
    phone                text        NOT NULL,
    alt_phone            text,
    email                text,
    line1                text        NOT NULL,
    line2                text,
    landmark             text,
    city_name            text        NOT NULL,
    state_name           text        NOT NULL,
    pincode              text        NOT NULL,
    country_code         char(2)     NOT NULL,
    reason               text        NOT NULL,
    corrected_by_user_id bigint      REFERENCES users(id) ON DELETE SET NULL,
    request_id           text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT shipment_address_corrections_sequence_unique
        UNIQUE (shipment_id, role, sequence)
);

CREATE INDEX shipment_address_corrections_latest_idx
    ON shipment_address_corrections (shipment_id, role, sequence DESC);

CREATE TRIGGER shipment_address_corrections_append_only
    BEFORE UPDATE OR DELETE ON shipment_address_corrections
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();

INSERT INTO permissions (public_id, code, resource, action, module, description) VALUES
    (gen_seed_public_id('perm'), 'shipment.edit', 'shipment', 'edit', 'shipment',
     'Correct non-routing shipment details before pickup')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code = 'shipment.edit'
WHERE r.organization_id IS NULL
  AND r.code IN ('SUPER_ADMIN','ORG_ADMIN','OPERATIONS_ADMIN','BRANCH_MANAGER',
                 'FRANCHISE_OWNER','FRANCHISE_OPERATOR','CUSTOMER_SUPPORT','CUSTOMER')
ON CONFLICT DO NOTHING;
