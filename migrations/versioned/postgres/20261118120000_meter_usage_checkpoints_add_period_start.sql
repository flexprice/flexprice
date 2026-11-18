-- migrate:up
-- Step 1 of 4. Nullable with no default: metadata-only, but still takes ACCESS EXCLUSIVE briefly.
SET lock_timeout = '3s';
ALTER TABLE meter_usage_checkpoints ADD COLUMN IF NOT EXISTS period_start timestamptz NULL;

-- migrate:down
SET lock_timeout = '3s';
ALTER TABLE meter_usage_checkpoints DROP COLUMN IF EXISTS period_start;
