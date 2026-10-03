-- 0044 Give manually issued paper waybills a durable alternate identifier.
-- CESERV keeps its immutable AWB; the paper number is an alternate identifier.

ALTER TABLE shipments
    ADD COLUMN manual_waybill_number text
        CHECK (
            manual_waybill_number IS NULL
            OR (
                length(btrim(manual_waybill_number)) BETWEEN 1 AND 64
                AND manual_waybill_number ~ '^[A-Za-z0-9][A-Za-z0-9._:/ -]{0,63}$'
            )
        );

CREATE UNIQUE INDEX shipments_manual_waybill_uq
    ON shipments (upper(manual_waybill_number))
    WHERE manual_waybill_number IS NOT NULL;

COMMENT ON COLUMN shipments.manual_waybill_number IS
    'Globally unique number printed on a manually issued paper waybill; accepted as an alternate tracking and scan identifier.';
