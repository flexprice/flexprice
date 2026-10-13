-- migrate:up
-- source_type/source_id link a credit or debit to the invoice behind it, for revenue.
-- consumption_breakdown records which credit batches a debit drew from. All nullable,
-- so this is a catalog-only change and the running code ignores the new columns.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "wallet_transactions" ADD COLUMN IF NOT EXISTS "source_type" character varying(50) NULL, ADD COLUMN IF NOT EXISTS "source_id" character varying(50) NULL, ADD COLUMN IF NOT EXISTS "consumption_breakdown" jsonb NULL;

-- migrate:down
ALTER TABLE "wallet_transactions" DROP COLUMN IF EXISTS "consumption_breakdown", DROP COLUMN IF EXISTS "source_id", DROP COLUMN IF EXISTS "source_type";
