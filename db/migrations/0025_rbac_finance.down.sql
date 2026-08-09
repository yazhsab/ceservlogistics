-- Reverse 0025.
DELETE FROM accounting_periods WHERE code = to_char(now(), 'YYYY-MM');

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
     WHERE module IN ('ledger','commission','cod','settlement','billing')
);

DELETE FROM permissions
 WHERE module IN ('ledger','commission','cod','settlement','billing');
