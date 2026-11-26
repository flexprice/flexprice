-- migrate:up transaction:false
-- Collections board aggregates and pages open receivables from this index alone (index-only scan).
-- statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_open_receivables;
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_invoices_open_receivables
  ON invoices (tenant_id, environment_id, due_date)
  INCLUDE (id, customer_id, currency, amount_remaining, invoice_number)
  WHERE ((((status)::text = 'published'::text) AND ((invoice_status)::text = 'FINALIZED'::text) AND ((payment_status)::text <> 'SUCCEEDED'::text)) AND (amount_remaining > (0)::numeric));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_open_receivables;
