-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "billing_entities" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "name" varchar(255) NOT NULL,
  "legal_name" varchar(255) NOT NULL,
  "is_default" boolean NOT NULL DEFAULT false,
  "invoice_number_prefix" varchar(20) NOT NULL,
  "seller_details" jsonb NOT NULL,
  "tax_identifiers" jsonb NOT NULL,
  "metadata" jsonb NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_billing_entity_tenant_env_default" ON "billing_entities" ("tenant_id", "environment_id") WHERE (is_default AND ((status)::text = 'published'::text));
CREATE UNIQUE INDEX IF NOT EXISTS "idx_billing_entity_tenant_env_prefix" ON "billing_entities" ("tenant_id", "environment_id", "invoice_number_prefix") WHERE ((status)::text = 'published'::text);

-- migrate:down
DROP TABLE IF EXISTS "billing_entities";
