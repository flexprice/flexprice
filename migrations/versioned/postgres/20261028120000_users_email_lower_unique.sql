-- migrate:up transaction:false
-- Drop INVALID index before retry.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "idx_user_email_lower_unique" ON "users" (lower((email)::text)) WHERE (((status)::text = 'published'::text) AND (email IS NOT NULL) AND ((email)::text <> ''::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_user_email_lower_unique";
