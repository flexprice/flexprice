-- migrate:up
-- ADD COLUMN with a constant default is metadata-only on PostgreSQL 11+, so this
-- holds only a brief lock and needs no backfill.
SET LOCAL lock_timeout = '3s';

ALTER TABLE "environments" ADD COLUMN IF NOT EXISTS "billing_entity_mode" varchar(20) NOT NULL DEFAULT 'off';

-- migrate:down
ALTER TABLE "environments" DROP COLUMN IF EXISTS "billing_entity_mode";
