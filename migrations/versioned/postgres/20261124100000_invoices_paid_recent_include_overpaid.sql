-- migrate:up transaction:false
-- Collections board "paid this week" counts overpaid invoices too, and its totals and pages are served from the index alone.
-- Builds the replacement beside the old index, then swaps; statement_timeout must be 0, a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent_next;
CREATE INDEX CONCURRENTLY idx_invoices_paid_recent_next
  ON invoices (tenant_id, environment_id, paid_at DESC)
  INCLUDE (id, customer_id, currency, amount_due, invoice_number)
  WHERE ((((status)::text = 'published'::text) AND ((invoice_status)::text = 'FINALIZED'::text)) AND ((payment_status)::text = ANY ((ARRAY['SUCCEEDED'::character varying, 'OVERPAID'::character varying])::text[])));
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent;
ALTER INDEX idx_invoices_paid_recent_next RENAME TO idx_invoices_paid_recent;

-- migrate:down transaction:false
CREATE INDEX CONCURRENTLY idx_invoices_paid_recent_previous
  ON invoices (tenant_id, environment_id, paid_at DESC)
  WHERE ((((status)::text = 'published'::text) AND ((invoice_status)::text = 'FINALIZED'::text)) AND ((payment_status)::text = 'SUCCEEDED'::text));
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_paid_recent;
ALTER INDEX idx_invoices_paid_recent_previous RENAME TO idx_invoices_paid_recent;
