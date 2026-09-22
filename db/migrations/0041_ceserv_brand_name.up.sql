-- Correct the customer-facing brand spelling while retaining the established
-- CESERVE tenant code and other stable integration identifiers.
UPDATE organizations
SET name = replace(replace(replace(name, 'CESERVE', 'CESERV'), 'Ceserve', 'Ceserv'), 'ceserve', 'ceserv'),
    updated_at = now()
WHERE code = 'CESERVE' AND name ILIKE '%ceserve%';
