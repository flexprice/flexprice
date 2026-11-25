-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "performance_obligations" ADD COLUMN IF NOT EXISTS "product_id" character varying(50) NULL, ADD COLUMN IF NOT EXISTS "allocated_amount" numeric(20,8) NOT NULL DEFAULT '0';

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "performance_obligations" DROP COLUMN IF EXISTS "allocated_amount", DROP COLUMN IF EXISTS "product_id";
