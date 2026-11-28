-- migrate:up
-- A billing term may deliver its instalments differently from its owner. Adding a nullable column only touches the
-- catalog, and re-running it is a no-op.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "billing_terms" ADD COLUMN IF NOT EXISTS "instalment_invoicing" varchar(30) NULL;

-- migrate:down
ALTER TABLE "billing_terms" DROP COLUMN IF EXISTS "instalment_invoicing";
