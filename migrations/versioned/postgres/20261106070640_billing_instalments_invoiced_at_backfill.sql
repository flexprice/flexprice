-- migrate:up
-- Fills invoiced_at for instalments invoiced before the column existed, from the finalized_at of the invoice that
-- billed them. Only empty rows are touched, so a re-run is a no-op.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

UPDATE "billing_instalments" bi
SET "invoiced_at" = i."finalized_at"
FROM "invoices" i
WHERE bi."invoiced_at" IS NULL
  AND bi."instalment_status" = 'invoiced'
  AND i."id" = bi."invoice_id"
  AND i."tenant_id" = bi."tenant_id"
  AND i."environment_id" = bi."environment_id"
  AND i."invoice_status" = 'FINALIZED'
  AND i."finalized_at" IS NOT NULL;

-- migrate:down
-- Data-only; the column drop in 20261106070639's down block removes the values.
