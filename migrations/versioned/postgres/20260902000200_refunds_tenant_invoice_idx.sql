-- migrate:up transaction:false
-- Backs GET /refunds?invoice_id=X, which is the only way to read settled cash:
-- it is derived from these rows, never stored on the invoice.
--
-- statement_timeout must be 0 on the connection — a build killed by a timeout
-- leaves an INVALID index behind. Deliberately no IF NOT EXISTS, so a retry
-- after such a failure fails loudly rather than skipping the broken index.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY idx_refund_tenant_invoice ON refunds (tenant_id, environment_id, invoice_id);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_refund_tenant_invoice;
