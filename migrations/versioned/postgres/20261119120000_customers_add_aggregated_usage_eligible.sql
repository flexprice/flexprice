-- migrate:up
-- A constant default is metadata-only on Postgres 11+, but still takes ACCESS EXCLUSIVE briefly.
SET lock_timeout = '3s';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS aggregated_usage_eligible boolean NOT NULL DEFAULT false;

-- migrate:down
SET lock_timeout = '3s';
ALTER TABLE customers DROP COLUMN IF EXISTS aggregated_usage_eligible;
