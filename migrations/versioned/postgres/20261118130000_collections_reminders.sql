-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "reminder_decisions" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "customer_id" varchar(50) NOT NULL,
  "currency" varchar(10) NOT NULL,
  "draft_status" varchar(20) NOT NULL DEFAULT 'pending',
  "hold_reason" text NULL,
  "decided_at" timestamptz NULL,
  "tone_override" varchar(20) NULL,
  "subject_override" text NULL,
  "body_override" text NULL,
  "invoice_ids" jsonb NOT NULL,
  "invoice_set_hash" varchar(64) NOT NULL,
  "frozen_subject" text NULL,
  "frozen_body" text NULL,
  "frozen_tone" varchar(20) NULL,
  "version" bigint NOT NULL DEFAULT 1,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_reminder_decision_tenant_env_customer_currency" ON "reminder_decisions" ("tenant_id", "environment_id", "customer_id", "currency") WHERE ((status)::text = 'published'::text);

-- migrate:down
DROP TABLE IF EXISTS "reminder_decisions";
