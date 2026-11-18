-- migrate:up transaction:false
-- Usage analytics now places instalments by bill_on, so nothing reads them by invoiced_at.
DROP INDEX CONCURRENTLY IF EXISTS "idx_billing_instalments_tenant_env_owner_invoiced_at";

-- migrate:down transaction:false
-- statement_timeout must be 0 on the connection. A failed build leaves an INVALID index holding the name, so a retry
-- fails. Confirm it is invalid, drop it, then retry:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
--   DROP INDEX CONCURRENTLY "idx_billing_instalments_tenant_env_owner_invoiced_at";
CREATE INDEX CONCURRENTLY "idx_billing_instalments_tenant_env_owner_invoiced_at" ON "billing_instalments" ("tenant_id", "environment_id", "owner_id", "invoiced_at");
