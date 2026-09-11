-- migrate:up transaction:false
-- Backs the invoice checkout gate: the guards ask whether a live session owns an invoice.
-- checkout_sessions keeps terminal rows forever, so without this the lookup scans the full
-- history. Partial on the active statuses, so it only holds sessions still in flight.
--
-- UNIQUE enforces one active session per invoice, previously only a property of the code.
-- A pre-existing duplicate makes this build fail and leaves an INVALID index; check first:
--   SELECT tenant_id, environment_id, checkout_invoice_id, count(*) FROM checkout_sessions
--   WHERE checkout_invoice_id IS NOT NULL AND checkout_status IN ('initiated','pending')
--   GROUP BY 1,2,3 HAVING count(*) > 1;
--
-- Predicate text matches the Ent annotation verbatim: Postgres stores the expression as
-- written and the sync check compares catalog definitions.
--
-- No IF NOT EXISTS: on a concurrent build it silently skips an INVALID index left by an
-- earlier failure. Drop that first, then retry:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE UNIQUE INDEX CONCURRENTLY "idx_checkout_session_invoice_active" ON "checkout_sessions" ("tenant_id", "environment_id", "checkout_invoice_id") WHERE ((checkout_invoice_id IS NOT NULL) AND ((checkout_status)::text = ANY (ARRAY[('initiated'::character varying)::text, ('pending'::character varying)::text])));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_checkout_session_invoice_active";
