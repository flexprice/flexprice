-- migrate:up
-- prices.bucket_size arrived on develop in c0b294ddf (2026-08-19), after the
-- baseline dump was taken, so the baseline does not contain it.
--
-- Nullable with no default: catalog-only in Postgres, instant regardless of table
-- size. Lane A.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE prices ADD COLUMN IF NOT EXISTS bucket_size character varying(20) NULL;

-- migrate:down
SET lock_timeout = '3s';
ALTER TABLE prices DROP COLUMN IF EXISTS bucket_size;
