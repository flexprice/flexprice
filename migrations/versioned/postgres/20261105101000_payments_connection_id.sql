-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "payments" ADD COLUMN IF NOT EXISTS "connection_id" varchar(50) NULL;

-- migrate:down
ALTER TABLE "payments" DROP COLUMN IF EXISTS "connection_id";
