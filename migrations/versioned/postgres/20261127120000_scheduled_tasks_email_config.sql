-- migrate:up
ALTER TABLE IF EXISTS scheduled_tasks ADD COLUMN IF NOT EXISTS email_config jsonb NULL;

-- migrate:down
ALTER TABLE IF EXISTS scheduled_tasks DROP COLUMN IF EXISTS email_config;
