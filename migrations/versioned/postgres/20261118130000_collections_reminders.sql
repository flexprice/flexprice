-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

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
  "original_tone" varchar(20) NOT NULL,
  "original_subject" text NOT NULL,
  "original_body" text NOT NULL,
  "mail_tone" varchar(20) NOT NULL,
  "mail_subject" text NOT NULL,
  "mail_body" text NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_invoice_reminder_tenant_env_invoice" ON "invoice_reminders" ("tenant_id", "environment_id", "invoice_id") WHERE ((status)::text = 'published'::text);

-- migrate:down
DROP TABLE IF EXISTS "invoice_reminders";
