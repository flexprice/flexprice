-- migrate:up transaction:false
-- Backs the invoice checkout gate: the guards ask whether a live session owns an invoice.
-- checkout_sessions keeps terminal rows forever, so without this the lookup is a scan of
-- the full history. Partial on the active statuses, so the index only ever holds sessions
-- still in flight rather than everything ever created.
--
-- statement_timeout must be 0 on the connection — a build killed by a timeout
-- leaves an INVALID index behind. Deliberately no IF NOT EXISTS, so a retry
-- after such a failure fails loudly rather than skipping the broken index.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
-- Predicate text matches the Ent annotation verbatim. Postgres stores the expression as
-- written, and the migration sync check compares catalog definitions, so `IN (...)` — which
-- normalises to a different but equivalent form — fails that check.
CREATE INDEX CONCURRENTLY idx_checkout_session_invoice_active
    ON checkout_sessions (tenant_id, environment_id, checkout_invoice_id)
    WHERE ((checkout_invoice_id IS NOT NULL) AND ((checkout_status)::text = ANY (ARRAY[('initiated'::character varying)::text, ('pending'::character varying)::text])));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_checkout_session_invoice_active;
