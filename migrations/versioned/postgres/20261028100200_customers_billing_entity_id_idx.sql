-- migrate:up transaction:false
-- customers is large and live, so the index builds CONCURRENTLY to avoid a write lock.
--
-- statement_timeout must be 0 on the connection; a build killed by a timeout leaves an
-- INVALID index behind. No IF NOT EXISTS, so a retry after such a failure fails loudly.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY "idx_customers_tenant_env_billing_entity" ON "customers" ("tenant_id", "environment_id", "billing_entity_id");

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_customers_tenant_env_billing_entity";
