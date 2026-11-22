-- migrate:up transaction:false
-- Collections board lists and totals invoices paid in the last week, newest first.
-- statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent;
CREATE INDEX CONCURRENTLY idx_invoices_paid_recent
  ON invoices (tenant_id, environment_id, paid_at DESC)
  WHERE ((((status)::text = 'published'::text) AND ((invoice_status)::text = 'FINALIZED'::text)) AND ((payment_status)::text = 'SUCCEEDED'::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent;
