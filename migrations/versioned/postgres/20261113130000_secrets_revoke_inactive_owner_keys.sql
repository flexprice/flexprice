-- migrate:up
-- Revoke API keys of inactive owners.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

UPDATE "secrets" s
SET "status" = 'deleted',
    "updated_at" = now()
FROM "users" u
WHERE u.id = s.user_id
  AND u.tenant_id = s.tenant_id
  AND u.status <> 'published'
  AND s.status = 'published'
  AND s.type IN ('private_key', 'publishable_key');

-- migrate:down
-- Irreversible; prior status not recorded.
