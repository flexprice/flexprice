-- migrate:up transaction:false
-- Collections board lists and totals invoices paid (or overpaid) in the last week, newest first, from this index alone.
-- statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_invoices_paid_recent
  ON invoices (tenant_id, environment_id, paid_at DESC)
  INCLUDE (id, customer_id, currency, amount_due, invoice_number)
  WHERE ((((status)::text = 'published'::text) AND ((invoice_status)::text = 'FINALIZED'::text)) AND ((payment_status)::text = ANY ((ARRAY['SUCCEEDED'::character varying, 'OVERPAID'::character varying])::text[])));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent;
