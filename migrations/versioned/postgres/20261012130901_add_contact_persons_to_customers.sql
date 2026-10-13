-- migrate:up
-- Contact persons on a customer, synced to the Zoho Books contact.
-- Nullable, so existing rows need no backfill.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers" ADD COLUMN IF NOT EXISTS "contact_persons" jsonb NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers" DROP COLUMN IF EXISTS "contact_persons";
