-- migrate:up transaction:false
-- Self-billing sums finalized invoices across tenants every hour by finalized_at.
-- Partial, so it holds only published finalized invoices; built concurrently so invoice writes never block.
-- statement_timeout must be 0 on the connection: a build killed by a timeout leaves an INVALID index behind.
-- If a retry fails with "relation already exists", clear the invalid index first:
--   DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_finalized_at_published;
CREATE INDEX CONCURRENTLY idx_invoices_finalized_at_published
  ON invoices (finalized_at)
  WHERE (((status)::text = 'published'::text) AND ((invoice_status)::text = 'FINALIZED'::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_finalized_at_published;
