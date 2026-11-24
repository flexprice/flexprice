-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "contract_value" numeric(20,8) NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "contracts" DROP COLUMN IF EXISTS "contract_value";
