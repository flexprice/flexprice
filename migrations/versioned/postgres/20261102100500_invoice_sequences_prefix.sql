-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "invoice_sequences" ADD COLUMN IF NOT EXISTS "prefix" varchar(20) NULL;

-- migrate:down
ALTER TABLE "invoice_sequences" DROP COLUMN IF EXISTS "prefix";
