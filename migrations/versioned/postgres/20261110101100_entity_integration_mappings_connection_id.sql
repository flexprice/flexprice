-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "entity_integration_mappings" ADD COLUMN IF NOT EXISTS "connection_id" varchar(50) NULL;

-- migrate:down
ALTER TABLE "entity_integration_mappings" DROP COLUMN IF EXISTS "connection_id";
