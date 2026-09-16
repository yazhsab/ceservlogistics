-- Reject rollback once commercial records exist rather than losing consent.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shipment_commercial_snapshots) THEN
        RAISE EXCEPTION 'Cannot remove commercial snapshots after shipments have been booked';
    END IF;
END $$;
DROP TABLE shipment_commercial_snapshots;
DROP FUNCTION validate_shipment_commercial_tenant();
-- Keep shared GB reference data: other records may now depend on it.
