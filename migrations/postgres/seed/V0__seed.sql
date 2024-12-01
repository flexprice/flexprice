--------- Queries to seed the database ---------

--------- Define variables ---------

SET @tenant_id = '00000000-0000-0000-0000-000000000000';
SET @user_id = '00000000-0000-0000-0000-000000000000';
SET @environment_id = '00000000-0000-0000-0000-000000000000';

--------- Create default environment ---------

INSERT INTO environments (id, name, is_default, tenant_id, created_at, created_by, updated_at, updated_by)
VALUES (@environment_id, 'Development', true, @tenant_id, NOW(), @user_id, NOW(), @user_id);

