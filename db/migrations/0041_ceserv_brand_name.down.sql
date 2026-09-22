UPDATE organizations
SET name = replace(replace(replace(name, 'CESERV', 'CESERVE'), 'Ceserv', 'Ceserve'), 'ceserv', 'ceserve'),
    updated_at = now()
WHERE code = 'CESERVE' AND name ILIKE '%ceserv%';
