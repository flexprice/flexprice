-- migrate:up
-- Email logs: one row per email sent to a customer, with its delivery status.
-- Brand-new table, so its indexes build instantly inside this transactional migration;
-- CONCURRENTLY is unnecessary. Base-mixin columns match ent/migrate/schema.go exactly.
CREATE TABLE IF NOT EXISTS "email_logs" (
  "id"                  varchar(50)  NOT NULL,
  "tenant_id"           varchar(50)  NOT NULL,
  "status"              varchar(20)  NOT NULL DEFAULT 'published',
  "created_at"          timestamptz  NOT NULL,
  "updated_at"          timestamptz  NOT NULL,
  "created_by"          varchar      NULL,
  "updated_by"          varchar      NULL,
  "environment_id"      varchar(50)  NULL DEFAULT '',
  "entity_type"         varchar(50)  NOT NULL,
  "entity_id"           varchar(50)  NOT NULL,
  "customer_id"         varchar(50)  NOT NULL,
  "template_key"        varchar(100) NOT NULL,
  "trigger"             varchar(20)  NOT NULL,
  "from_address"        varchar(320) NOT NULL,
  "reply_to"            varchar(320) NOT NULL,
  "to_addresses"        jsonb        NOT NULL,
  "cc_addresses"        jsonb        NULL,
  "subject"             text         NOT NULL,
  "provider"            varchar(20)  NOT NULL,
  "provider_message_id" varchar(100) NULL,
  "delivery_status"     varchar(30)  NOT NULL,
  "status_reason"       text         NULL,
  "sent_at"             timestamptz  NULL,
  "delivered_at"        timestamptz  NULL,
  "metadata"            jsonb        NULL,
  PRIMARY KEY ("id")
);

-- Delivery webhooks look emails up by the provider's id.
CREATE UNIQUE INDEX IF NOT EXISTS "emaillog_provider_provider_message_id"
  ON "email_logs" ("provider","provider_message_id")
  WHERE provider_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS "idx_email_log_entity_created_at"
  ON "email_logs" ("tenant_id","environment_id","entity_type","entity_id","created_at" DESC);

-- At most one automatic email per entity and template, even if the trigger is redelivered.
CREATE UNIQUE INDEX IF NOT EXISTS "idx_email_log_auto_once"
  ON "email_logs" ("tenant_id","environment_id","entity_id","template_key")
  WHERE "trigger" = 'auto';

CREATE INDEX IF NOT EXISTS "emaillog_tenant_id_environment_id_delivery_status_created_at"
  ON "email_logs" ("tenant_id","environment_id","delivery_status","created_at");

-- The email dispatcher finds environments with queued emails without scanning every email sent.
CREATE INDEX IF NOT EXISTS "idx_email_log_queued"
  ON "email_logs" ("tenant_id","environment_id","created_at")
  WHERE delivery_status = 'queued';

-- migrate:down
DROP TABLE IF EXISTS "email_logs";
