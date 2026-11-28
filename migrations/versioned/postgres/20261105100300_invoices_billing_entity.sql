-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "invoices" ADD COLUMN IF NOT EXISTS "billing_entity" jsonb NULL;

-- migrate:down
ALTER TABLE "invoices" DROP COLUMN IF EXISTS "billing_entity";
