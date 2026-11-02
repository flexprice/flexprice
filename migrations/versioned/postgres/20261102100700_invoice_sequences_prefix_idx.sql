-- migrate:up transaction:false
-- Built beside the existing (tenant, environment, year_month) unique index, which old
-- pods still use; that index is dropped in a later release.
--
-- statement_timeout must be 0 on the connection; a build killed by a timeout leaves an
-- INVALID index behind. No IF NOT EXISTS, so a retry after such a failure fails loudly.
CREATE UNIQUE INDEX CONCURRENTLY "idx_invoice_sequences_tenant_env_prefix_month" ON "invoice_sequences" ("tenant_id", "environment_id", "prefix", "year_month");

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_invoice_sequences_tenant_env_prefix_month";
