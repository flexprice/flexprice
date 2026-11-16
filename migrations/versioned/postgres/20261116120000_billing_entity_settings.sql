-- migrate:up
-- Billing entity overrides of environment settings (branding today). Nullable, so no backfill.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "billing_entities" ADD COLUMN IF NOT EXISTS "settings" jsonb NULL;

-- migrate:down
ALTER TABLE "billing_entities" DROP COLUMN IF EXISTS "settings";
