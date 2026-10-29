-- migrate:up transaction:false
-- Corrections resolve their original invoice before deciding whether new schedule charges may be collected.
-- Build concurrently; use the migration wrapper's statement_timeout=0 and clear any INVALID index before retrying.
CREATE INDEX CONCURRENTLY invoice_tenant_id_environment_id_recalculated_invoice_id
  ON invoices (tenant_id, environment_id, recalculated_invoice_id);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS invoice_tenant_id_environment_id_recalculated_invoice_id;
