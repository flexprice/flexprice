-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers" ADD COLUMN IF NOT EXISTS "billing_entity_id" varchar(50) NULL;

-- migrate:down
ALTER TABLE "customers" DROP COLUMN IF EXISTS "billing_entity_id";
