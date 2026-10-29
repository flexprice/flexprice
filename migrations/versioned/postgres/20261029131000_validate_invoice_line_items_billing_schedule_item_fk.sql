-- migrate:up transaction:false
-- Validation scans existing rows while ordinary writes continue. apply.sh supplies
-- lock_timeout=3s and statement_timeout=0, so lock acquisition is bounded and the scan can finish.
ALTER TABLE "invoice_line_items" VALIDATE CONSTRAINT "invoice_line_items_billing_schedule_items_source_line";

-- migrate:down transaction:false
-- Restore the pre-validation state while retaining enforcement for new writes.
ALTER TABLE "invoice_line_items"
  DROP CONSTRAINT "invoice_line_items_billing_schedule_items_source_line",
  ADD CONSTRAINT "invoice_line_items_billing_schedule_items_source_line"
    FOREIGN KEY ("billing_schedule_item_id") REFERENCES "billing_schedule_items" ("id") ON DELETE RESTRICT NOT VALID;
