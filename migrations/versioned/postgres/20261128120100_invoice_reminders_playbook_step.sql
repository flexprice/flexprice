-- migrate:up
-- The playbook step that last wrote a reminder. Nullable, so existing reminders need no backfill.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "invoice_reminders" ADD COLUMN IF NOT EXISTS "playbook_id" varchar(50) NULL, ADD COLUMN IF NOT EXISTS "playbook_step_id" varchar(50) NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "invoice_reminders" DROP COLUMN IF EXISTS "playbook_id", DROP COLUMN IF EXISTS "playbook_step_id";
