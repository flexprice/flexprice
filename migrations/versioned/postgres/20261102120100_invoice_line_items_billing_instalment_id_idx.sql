-- migrate:up transaction:false
-- invoice_line_items is large and live, so the index builds CONCURRENTLY to avoid a write lock.
-- Partial: only instalment lines carry a billing_instalment_id.
--
-- statement_timeout must be 0 on the connection; a build killed by a timeout leaves an
-- INVALID index behind. No IF NOT EXISTS, so a retry after such a failure fails loudly.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY "invoicelineitem_tenant_id_environment_id_billing_instalment_id" ON "invoice_line_items" ("tenant_id", "environment_id", "billing_instalment_id") WHERE (billing_instalment_id IS NOT NULL);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "invoicelineitem_tenant_id_environment_id_billing_instalment_id";
