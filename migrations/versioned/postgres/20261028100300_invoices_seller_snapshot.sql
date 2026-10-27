-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "invoices" ADD COLUMN IF NOT EXISTS "seller_snapshot" jsonb NULL;

-- migrate:down
ALTER TABLE "invoices" DROP COLUMN IF EXISTS "seller_snapshot";
