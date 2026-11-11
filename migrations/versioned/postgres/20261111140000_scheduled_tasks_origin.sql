-- migrate:up
ALTER TABLE IF EXISTS scheduled_tasks ADD COLUMN IF NOT EXISTS origin text NOT NULL DEFAULT 'console';

-- migrate:down
ALTER TABLE IF EXISTS scheduled_tasks DROP COLUMN IF EXISTS origin;
