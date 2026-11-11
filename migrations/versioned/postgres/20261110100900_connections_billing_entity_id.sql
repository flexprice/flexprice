-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "connections" ADD COLUMN IF NOT EXISTS "billing_entity_id" varchar(50) NULL;
CREATE UNIQUE INDEX IF NOT EXISTS "idx_connections_owner_provider" ON "connections" ("tenant_id", "environment_id", "billing_entity_id", "provider_type") WHERE (((status)::text = 'published'::text) AND (billing_entity_id IS NOT NULL));

-- migrate:down
DROP INDEX IF EXISTS "idx_connections_owner_provider";
ALTER TABLE "connections" DROP COLUMN IF EXISTS "billing_entity_id";
