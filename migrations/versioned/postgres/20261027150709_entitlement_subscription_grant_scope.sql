-- migrate:up
-- Subscription grants: an entitlement with no feature that covers every usage line on a
-- subscription. Both changes are catalog-only: dropping NOT NULL scans nothing, and a
-- constant default on a new column is stored once, not written per row.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "entitlements"
  ALTER COLUMN "feature_id" DROP NOT NULL,
  ALTER COLUMN "feature_type" DROP NOT NULL,
  ADD COLUMN IF NOT EXISTS "grant_scope" character varying(20) NOT NULL DEFAULT 'feature';

-- migrate:down
-- Restoring NOT NULL fails while subscription grants exist (feature_id is NULL on them);
-- delete those rows first if a rollback is truly needed.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "entitlements"
  DROP COLUMN IF EXISTS "grant_scope",
  ALTER COLUMN "feature_id" SET NOT NULL,
  ALTER COLUMN "feature_type" SET NOT NULL;
