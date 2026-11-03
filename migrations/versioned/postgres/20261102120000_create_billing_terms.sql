-- migrate:up
-- Billing terms and the instalments they produce. Brand-new tables, so their indexes build
-- instantly inside this transactional migration; CONCURRENTLY is unnecessary.
-- Base-mixin columns match ent/migrate/schema.go exactly (BaseMixin + EnvironmentMixin).
-- Index names longer than 63 chars use Ent's truncated-plus-md5 form.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "billing_terms" (
  "id"              varchar(50)   NOT NULL,
  "tenant_id"       varchar(50)   NOT NULL,
  "status"          varchar(20)   NOT NULL DEFAULT 'published',
  "created_at"      timestamptz   NOT NULL,
  "updated_at"      timestamptz   NOT NULL,
  "created_by"      varchar       NULL,
  "updated_by"      varchar       NULL,
  "environment_id"  varchar(50)   NULL DEFAULT '',
  "owner_type"      varchar(30)   NOT NULL,
  "owner_id"        varchar(50)   NOT NULL,
  "kind"            varchar(20)   NOT NULL,
  "value_type"      varchar(20)   NOT NULL,
  "value"           numeric(20,8) NULL,
  "bill_trigger"    varchar(20)   NULL,
  "bill_on"         timestamptz   NULL,
  "start_date"      timestamptz   NULL,
  "end_date"        timestamptz   NULL,
  "period_count"    bigint        NULL,
  "invoice_cadence" varchar(20)   NULL,
  "split"           varchar(20)   NULL,
  "invoice_label"   varchar(255)  NULL,
  "metadata"        jsonb         NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "billingterm_tenant_id_environment_id_owner_type_owner_id_status"
  ON "billing_terms" ("tenant_id","environment_id","owner_type","owner_id","status");

CREATE TABLE IF NOT EXISTS "billing_instalments" (
  "id"                   varchar(50)   NOT NULL,
  "tenant_id"            varchar(50)   NOT NULL,
  "status"               varchar(20)   NOT NULL DEFAULT 'published',
  "created_at"           timestamptz   NOT NULL,
  "updated_at"           timestamptz   NOT NULL,
  "created_by"           varchar       NULL,
  "updated_by"           varchar       NULL,
  "environment_id"       varchar(50)   NULL DEFAULT '',
  "owner_type"           varchar(30)   NOT NULL,
  "owner_id"             varchar(50)   NOT NULL,
  "billing_term_id"      varchar(50)   NULL,
  "customer_id"          varchar(50)   NOT NULL,
  "subscription_id"      varchar(50)   NULL,
  "currency"             varchar(10)   NOT NULL,
  "kind"                 varchar(20)   NOT NULL,
  "bill_trigger"         varchar(20)   NOT NULL,
  "bill_on"              timestamptz   NOT NULL,
  "triggered_at"         timestamptz   NULL,
  "amount"               numeric(20,8) NOT NULL,
  "invoice_label"        varchar(255)  NOT NULL,
  "service_period_start" timestamptz   NOT NULL,
  "service_period_end"   timestamptz   NOT NULL,
  "delivery"             varchar(30)   NOT NULL,
  "instalment_status"    varchar(20)   NOT NULL,
  "invoice_id"           varchar(50)   NULL,
  "metadata"             jsonb         NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "billinginstalment_tenant_id_environment_id_owner_type_owner_id"
  ON "billing_instalments" ("tenant_id","environment_id","owner_type","owner_id");

CREATE INDEX IF NOT EXISTS "billinginstalment_tenant_id_en_736e880ab6fdf4fd8b131b6a6236d3f4"
  ON "billing_instalments" ("tenant_id","environment_id","subscription_id","instalment_status","bill_on");

CREATE INDEX IF NOT EXISTS "billinginstalment_tenant_id_en_b4c4e9ffb2b45810a8064478be5929b8"
  ON "billing_instalments" ("tenant_id","environment_id","instalment_status","delivery","bill_on");

CREATE INDEX IF NOT EXISTS "billinginstalment_tenant_id_environment_id_invoice_id"
  ON "billing_instalments" ("tenant_id","environment_id","invoice_id");

ALTER TABLE "invoice_line_items" ADD COLUMN IF NOT EXISTS "billing_instalment_id" varchar(50) NULL;

-- migrate:down
ALTER TABLE "invoice_line_items" DROP COLUMN IF EXISTS "billing_instalment_id";
DROP TABLE IF EXISTS "billing_instalments";
DROP TABLE IF EXISTS "billing_terms";
