-- migrate:up transaction:false
-- Requires statement_timeout = 0.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = to_regclass('idx_user_email_lower_unique') AND NOT indisvalid
    ) THEN
        RAISE EXCEPTION 'idx_user_email_lower_unique is INVALID: drop it, then retry';
    END IF;
END
$$;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "idx_user_email_lower_unique" ON "users" (lower((email)::text)) WHERE (((status)::text = 'published'::text) AND (email IS NOT NULL) AND ((email)::text <> ''::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_user_email_lower_unique";
