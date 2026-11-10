-- migrate:up
ALTER TABLE IF EXISTS scheduled_tasks
  ALTER COLUMN connection_id DROP NOT NULL,
  ALTER COLUMN connection_id SET DEFAULT '',
  ADD COLUMN IF NOT EXISTS name character varying NULL,
  ADD COLUMN IF NOT EXISTS managed_copy boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS schedule jsonb NULL;

-- migrate:down
ALTER TABLE IF EXISTS scheduled_tasks
  DROP COLUMN IF EXISTS schedule,
  DROP COLUMN IF EXISTS managed_copy,
  DROP COLUMN IF EXISTS name,
  ALTER COLUMN connection_id DROP DEFAULT;
