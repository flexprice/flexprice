-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "connection_associations" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "entity_type" varchar(50) NOT NULL,
  "entity_id" varchar(50) NOT NULL,
  "connection_id" varchar(50) NOT NULL,
  "provider_type" varchar(50) NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "idx_connection_assoc_owner_connection" ON "connection_associations" ("tenant_id", "environment_id", "connection_id") WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'billing_entity'::text));
CREATE UNIQUE INDEX IF NOT EXISTS "idx_connection_assoc_owner_provider" ON "connection_associations" ("tenant_id", "environment_id", "entity_id", "provider_type") WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'billing_entity'::text));
CREATE UNIQUE INDEX IF NOT EXISTS "idx_connection_assoc_customer" ON "connection_associations" ("tenant_id", "environment_id", "entity_id") WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'customer'::text));
CREATE INDEX IF NOT EXISTS "idx_connection_assoc_lookup" ON "connection_associations" ("tenant_id", "environment_id", "entity_type", "entity_id");

-- migrate:down
DROP TABLE IF EXISTS "connection_associations";
