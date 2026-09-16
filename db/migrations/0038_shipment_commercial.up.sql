-- Immutable commercial instructions and customer insurance decisions.
-- Existing shipments are deliberately not backfilled with invented consent.
CREATE TABLE shipment_commercial_snapshots (
    shipment_id bigint PRIMARY KEY REFERENCES shipments(id) ON DELETE RESTRICT,
    organization_id bigint NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    transport_customer_id bigint REFERENCES customers(id) ON DELETE RESTRICT,
    insurance jsonb NOT NULL CHECK (jsonb_typeof(insurance) = 'object'),
    customs jsonb CHECK (customs IS NULL OR jsonb_typeof(customs) = 'object'),
    billing jsonb NOT NULL CHECK (jsonb_typeof(billing) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX shipment_commercial_org_idx ON shipment_commercial_snapshots(organization_id, shipment_id);
CREATE TRIGGER shipment_commercial_append_only
    BEFORE UPDATE OR DELETE ON shipment_commercial_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_mutation();
-- Guard tenant consistency even for an accidental direct SQL insert.
CREATE FUNCTION validate_shipment_commercial_tenant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM shipments WHERE id = NEW.shipment_id AND organization_id = NEW.organization_id) THEN
        RAISE EXCEPTION 'commercial snapshot tenant mismatch' USING ERRCODE = '23514';
    END IF;
    IF NEW.transport_customer_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM customers WHERE id = NEW.transport_customer_id AND organization_id = NEW.organization_id) THEN
        RAISE EXCEPTION 'billing customer tenant mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER shipment_commercial_tenant
    BEFORE INSERT ON shipment_commercial_snapshots
    FOR EACH ROW EXECUTE FUNCTION validate_shipment_commercial_tenant();

-- Country availability does not imply a serviceable route; postcode, zone and
-- network configuration must still be provided through geography operations.
INSERT INTO countries(public_id, iso2, iso3, name, phone_code, currency)
VALUES ('cnt_01J000000000000000000000GB', 'GB', 'GBR', 'United Kingdom', '+44', 'GBP')
ON CONFLICT (iso2) DO NOTHING;
