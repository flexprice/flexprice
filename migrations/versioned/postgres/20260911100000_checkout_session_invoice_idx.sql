-- migrate:up transaction:false
-- Backs the invoice checkout gate: the guards ask whether a live session owns an invoice.
-- checkout_sessions keeps terminal rows forever, so without this the lookup is a scan of
-- the full history. Partial on the active statuses, so the index only ever holds sessions
-- still in flight rather than everything ever created.
--
-- UNIQUE enforces one active session per invoice — previously only a property of the code
-- (the reuse check plus the payment idempotency key). A pre-existing duplicate would make
-- this build fail and leave an INVALID index; check before deploying:
--   SELECT tenant_id, environment_id, checkout_invoice_id, count(*) FROM checkout_sessions
--   WHERE checkout_invoice_id IS NOT NULL AND checkout_status IN ('initiated','pending')
--   GROUP BY 1,2,3 HAVING count(*) > 1;
--
-- statement_timeout must be 0 on the connection — a build killed by a timeout
-- leaves an INVALID index behind. Deliberately no IF NOT EXISTS, so a retry
-- after such a failure fails loudly rather than skipping the broken index.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
-- Predicate text matches the Ent annotation verbatim. Postgres stores the expression as
-- written, and the migration sync check compares catalog definitions, so `IN (...)` — which
-- normalises to a different but equivalent form — fails that check.
CREATE UNIQUE INDEX CONCURRENTLY idx_checkout_session_invoice_active
    ON checkout_sessions (tenant_id, environment_id, checkout_invoice_id)
    WHERE ((checkout_invoice_id IS NOT NULL) AND ((checkout_status)::text = ANY (ARRAY[('initiated'::character varying)::text, ('pending'::character varying)::text])));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_checkout_session_invoice_active;
