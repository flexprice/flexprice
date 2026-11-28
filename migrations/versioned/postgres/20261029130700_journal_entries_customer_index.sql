-- migrate:up transaction:false
-- Revenue reports filter journal entries by customer; without this index a one-customer report scans the environment.
-- CONCURRENTLY so a live journal keeps taking writes. No IF NOT EXISTS: a retry after a failed build fails loudly
-- instead of skipping an INVALID index. Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY "idx_journal_entry_customer" ON "journal_entries" ("tenant_id", "environment_id", "customer_id");

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_journal_entry_customer";
