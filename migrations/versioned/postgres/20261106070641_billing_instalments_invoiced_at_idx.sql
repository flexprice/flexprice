-- migrate:up transaction:false
-- Reads a commitment's instalments by when they were invoiced. Built CONCURRENTLY so it never blocks billing writes.
--
-- statement_timeout must be 0 on the connection; a build killed by a timeout leaves an
-- INVALID index behind. No IF NOT EXISTS, so a retry after such a failure fails loudly.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY "idx_billing_instalments_tenant_env_owner_invoiced_at" ON "billing_instalments" ("tenant_id", "environment_id", "owner_id", "invoiced_at");

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_billing_instalments_tenant_env_owner_invoiced_at";
