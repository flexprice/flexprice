-- migrate:up transaction:false
CREATE UNIQUE INDEX CONCURRENTLY "idx_user_email_lower_unique" ON "users" (lower((email)::text)) WHERE (((status)::text = 'published'::text) AND (email IS NOT NULL) AND ((email)::text <> ''::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_user_email_lower_unique";
