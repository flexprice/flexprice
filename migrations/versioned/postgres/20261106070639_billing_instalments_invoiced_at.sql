-- migrate:up
-- When the invoice billing an instalment was finalized, so instalments can be read by billing date. Adding a nullable
-- column only touches the catalog, and re-running it is a no-op.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "billing_instalments" ADD COLUMN IF NOT EXISTS "invoiced_at" timestamptz NULL;

-- migrate:down
ALTER TABLE "billing_instalments" DROP COLUMN IF EXISTS "invoiced_at";
