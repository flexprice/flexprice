-- migrate:up
-- Customer and subscription overrides of environment settings (e.g. grace period).
-- Nullable, so existing rows need no backfill.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers" ADD COLUMN IF NOT EXISTS "settings" jsonb NULL;
ALTER TABLE "subscriptions" ADD COLUMN IF NOT EXISTS "settings" jsonb NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers" DROP COLUMN IF EXISTS "settings";
ALTER TABLE "subscriptions" DROP COLUMN IF EXISTS "settings";
