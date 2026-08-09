-- Reverse 0029.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
     WHERE module IN ('notification','portal','analytics','reporting','partner')
);
DELETE FROM permissions
 WHERE module IN ('notification','portal','analytics','reporting','partner');
