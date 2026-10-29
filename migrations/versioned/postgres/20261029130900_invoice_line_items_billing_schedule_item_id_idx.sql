-- migrate:up transaction:false
-- The live invoice table requires a concurrent build. Apply through apply.sh for
-- connection lock_timeout=3s and statement_timeout=0; an interrupted build may leave
-- an INVALID index. No IF NOT EXISTS, so retries expose that state instead of skipping it.
CREATE UNIQUE INDEX CONCURRENTLY "invoice_line_items_billing_schedule_item_id_key" ON "invoice_line_items" ("billing_schedule_item_id");

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "invoice_line_items_billing_schedule_item_id_key";
