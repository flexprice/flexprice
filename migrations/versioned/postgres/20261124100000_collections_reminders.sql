-- migrate:up
CREATE TABLE IF NOT EXISTS "invoice_reminders" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "invoice_id" varchar(50) NOT NULL,
  "reminder_status" varchar(20) NOT NULL DEFAULT 'pending',
  "status_reason" text NULL,
  "approved_at" timestamptz NULL,
  "metadata" jsonb NULL,
  "tone" varchar(20) NOT NULL,
  "subject" text NOT NULL,
  "body" text NOT NULL,
  PRIMARY KEY ("id")
);
-- One live reminder per invoice. The table is new, so a plain build inside this transaction blocks nothing.
CREATE UNIQUE INDEX IF NOT EXISTS "idx_invoice_reminder_tenant_env_invoice" ON "invoice_reminders" ("tenant_id", "environment_id", "invoice_id") WHERE ((status)::text = 'published'::text);

-- migrate:down
DROP TABLE IF EXISTS "invoice_reminders";
