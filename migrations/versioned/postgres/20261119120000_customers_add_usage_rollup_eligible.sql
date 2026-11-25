-- migrate:up
-- A nullable column is metadata-only, but still takes ACCESS EXCLUSIVE briefly. NULL = not evaluated yet.
SET lock_timeout = '3s';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS usage_rollup_eligible boolean;

-- migrate:down
SET lock_timeout = '3s';
ALTER TABLE customers DROP COLUMN IF EXISTS usage_rollup_eligible;
