-- migrate:up
-- First-class subscription commitments. Brand-new table, so its indexes build
-- instantly inside this transactional migration; CONCURRENTLY is unnecessary.
-- Base-mixin columns match ent/migrate/schema.go exactly (BaseMixin + EnvironmentMixin).
-- Index names longer than 63 chars use Ent's truncated-plus-md5 form.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "commitments" (
  "id"                   varchar(50)   NOT NULL,
  "tenant_id"            varchar(50)   NOT NULL,
  "status"               varchar(20)   NOT NULL DEFAULT 'published',
  "created_at"           timestamptz   NOT NULL,
  "updated_at"           timestamptz   NOT NULL,
  "created_by"           varchar       NULL,
  "updated_by"           varchar       NULL,
  "environment_id"       varchar(50)   NULL DEFAULT '',
  "subscription_id"      varchar(50)   NOT NULL,
  "customer_id"          varchar(50)   NOT NULL,
  "name"                 varchar(255)  NOT NULL,
  "invoice_label"        varchar(255)  NOT NULL,
  "invoice_description"  varchar(500)  NULL,
  "currency"             varchar(10)   NOT NULL,
  "scope_entity_type"    varchar(20)   NOT NULL,
  "scope_entity_id"      varchar(50)   NOT NULL,
  "type"                 varchar(20)   NOT NULL,
  "value"                numeric(20,8) NOT NULL,
  "committed_price_id"   varchar(50)   NULL,
  "overage_price_id"     varchar(50)   NULL,
  "measurement"          varchar(20)   NOT NULL,
  "active_windows"       jsonb         NULL,
  "timezone"             varchar(64)   NOT NULL,
  "priority"             bigint        NOT NULL DEFAULT 0,
  "binding"              boolean       NOT NULL DEFAULT false,
  "collection_method"    varchar(20)   NULL,
  "total_amount"         numeric(20,8) NULL,
  "instalment_invoicing" varchar(30)   NULL,
  "start_date"           timestamptz   NOT NULL,
  "end_date"             timestamptz   NOT NULL,
  "on_early_termination" varchar(20)   NULL,
  "commitment_status"    varchar(20)   NOT NULL DEFAULT 'active',
  "terminated_at"        timestamptz   NULL,
  "idempotency_key"      varchar(128)  NULL,
  "metadata"             jsonb         NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "commitment_tenant_id_environme_6baf15c622be0532af77419b7b53bd78"
  ON "commitments" ("tenant_id","environment_id","subscription_id","commitment_status");

CREATE INDEX IF NOT EXISTS "commitment_tenant_id_environme_7d524d374aa0a5d1a2e06bf990c26fd9"
  ON "commitments" ("tenant_id","environment_id","scope_entity_type","scope_entity_id");

CREATE INDEX IF NOT EXISTS "commitment_tenant_id_environment_id_customer_id"
  ON "commitments" ("tenant_id","environment_id","customer_id");

CREATE UNIQUE INDEX IF NOT EXISTS "commitment_tenant_id_environment_id_idempotency_key"
  ON "commitments" ("tenant_id","environment_id","idempotency_key");

ALTER TABLE "invoice_line_items" ADD COLUMN IF NOT EXISTS "commitment_id" varchar(50) NULL;

-- migrate:down
ALTER TABLE "invoice_line_items" DROP COLUMN IF EXISTS "commitment_id";
DROP TABLE IF EXISTS "commitments";
