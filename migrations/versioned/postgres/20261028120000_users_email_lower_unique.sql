-- migrate:up transaction:false
-- Email lookups are case-insensitive, so two live users differing only by case must not exist.
-- Partial: archived and deleted users may keep a colliding email.
-- users is live, so the build is CONCURRENTLY.
--
-- statement_timeout must be 0 on the connection — a build killed by a timeout
-- leaves an INVALID index behind. Deliberately no IF NOT EXISTS, so a retry
-- after such a failure fails loudly rather than skipping the broken index.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE UNIQUE INDEX CONCURRENTLY "idx_user_email_lower_unique" ON "users" (lower((email)::text)) WHERE (((status)::text = 'published'::text) AND (email IS NOT NULL) AND ((email)::text <> ''::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_user_email_lower_unique";
