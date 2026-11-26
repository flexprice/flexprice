-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

-- Nullable with no default: catalog-only on PG 11+, no rewrite or backfill.
ALTER TABLE "customers" ADD COLUMN IF NOT EXISTS "additional_emails" jsonb NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers" DROP COLUMN IF EXISTS "additional_emails";
