-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "environments" ADD COLUMN IF NOT EXISTS "logo_key" varchar(255) NULL;
ALTER TABLE "environments" ADD COLUMN IF NOT EXISTS "icon_key" varchar(255) NULL;
ALTER TABLE "billing_entities" ADD COLUMN IF NOT EXISTS "logo_key" varchar(255) NULL;
ALTER TABLE "billing_entities" ADD COLUMN IF NOT EXISTS "icon_key" varchar(255) NULL;

-- migrate:down
ALTER TABLE "billing_entities" DROP COLUMN IF EXISTS "icon_key";
ALTER TABLE "billing_entities" DROP COLUMN IF EXISTS "logo_key";
ALTER TABLE "environments" DROP COLUMN IF EXISTS "icon_key";
ALTER TABLE "environments" DROP COLUMN IF EXISTS "logo_key";
