-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

-- An uploaded contract is a draft with no customer or currency until its terms are reviewed.
ALTER TABLE "contracts" ALTER COLUMN "customer_id" DROP NOT NULL;
ALTER TABLE "contracts" ALTER COLUMN "currency" DROP NOT NULL;

ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "document" jsonb NULL;
ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "extraction_status" character varying(20) NULL;
ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "extraction" jsonb NULL;
ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "extraction_error" text NULL;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "contracts" DROP COLUMN IF EXISTS "extraction_error";
ALTER TABLE "contracts" DROP COLUMN IF EXISTS "extraction";
ALTER TABLE "contracts" DROP COLUMN IF EXISTS "extraction_status";
ALTER TABLE "contracts" DROP COLUMN IF EXISTS "document";

-- Draft contracts have no customer or currency; restore NOT NULL only when none remain.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM "contracts" WHERE "customer_id" IS NULL) THEN
    ALTER TABLE "contracts" ALTER COLUMN "customer_id" SET NOT NULL;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM "contracts" WHERE "currency" IS NULL) THEN
    ALTER TABLE "contracts" ALTER COLUMN "currency" SET NOT NULL;
  END IF;
END $$;
