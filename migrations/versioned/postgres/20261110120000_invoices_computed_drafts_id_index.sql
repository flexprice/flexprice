-- migrate:up transaction:false
-- Draft finalization cron pages computed drafts by id.
-- statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_computed_drafts_id;
CREATE INDEX CONCURRENTLY idx_invoices_computed_drafts_id
  ON invoices (id)
  WHERE (((status)::text = 'published'::text) AND ((invoice_status)::text = 'DRAFT'::text) AND (last_computed_at IS NOT NULL));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_invoices_computed_drafts_id;
