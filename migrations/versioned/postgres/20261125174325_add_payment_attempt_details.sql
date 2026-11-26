-- migrate:up
-- Gateway payment method and decline reason per payment attempt.
-- Nullable, so existing rows need no backfill.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "payment_attempts" ADD COLUMN IF NOT EXISTS "payment_method_details" jsonb NULL, ADD COLUMN IF NOT EXISTS "error_details" jsonb NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "payment_attempts" DROP COLUMN IF EXISTS "payment_method_details", DROP COLUMN IF EXISTS "error_details";
