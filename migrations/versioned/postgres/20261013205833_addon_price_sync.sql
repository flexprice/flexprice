-- migrate:up
-- ADD COLUMN with a constant default is metadata-only on PostgreSQL 11+, so this
-- holds only a brief lock and needs no concurrent build.
SET lock_timeout = '3s';

ALTER TABLE "addon_associations" ADD COLUMN IF NOT EXISTS "synced_price_sequence" bigint NOT NULL DEFAULT '0';

-- migrate:down
ALTER TABLE "addon_associations" DROP COLUMN IF EXISTS "synced_price_sequence";
