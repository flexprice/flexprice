-- migrate:up
ALTER TABLE IF EXISTS scheduled_tasks ADD COLUMN IF NOT EXISTS source_config jsonb NULL;

-- migrate:down
ALTER TABLE IF EXISTS scheduled_tasks DROP COLUMN IF EXISTS source_config;
