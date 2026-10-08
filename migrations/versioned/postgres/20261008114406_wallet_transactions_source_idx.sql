-- migrate:up transaction:false
-- Looks up the wallet transactions linked to an invoice. Built concurrently because
-- wallet_transactions is a hot table. statement_timeout must be 0 on the connection:
-- a build killed by a timeout leaves an INVALID index. Deliberately no IF NOT EXISTS,
-- so a retry after such a failure fails loudly; check first with
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY "idx_wallet_transactions_tenant_env_source" ON "wallet_transactions" ("tenant_id", "environment_id", "source_type", "source_id");

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_wallet_transactions_tenant_env_source";
