-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

-- The reviewer's working copy of the extracted terms, and its version for optimistic saves.
ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "review" jsonb NULL;
ALTER TABLE "contracts" ADD COLUMN IF NOT EXISTS "review_version" bigint NOT NULL DEFAULT 0;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "contracts" DROP COLUMN IF EXISTS "review_version";
ALTER TABLE "contracts" DROP COLUMN IF EXISTS "review";
