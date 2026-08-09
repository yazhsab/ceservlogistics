DELETE FROM role_permissions
WHERE role_id IN (SELECT id FROM roles WHERE is_system AND organization_id IS NULL);
DELETE FROM user_roles
WHERE role_id IN (SELECT id FROM roles WHERE is_system AND organization_id IS NULL);
DELETE FROM roles WHERE is_system AND organization_id IS NULL;
DELETE FROM permissions;
