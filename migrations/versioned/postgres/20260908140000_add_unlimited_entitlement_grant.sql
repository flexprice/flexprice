-- migrate:up
-- Unlimited allowances: a grant window that tracks usage but has no ceiling.
-- A boolean rather than a nullable quota, so every existing reader of
-- entitlement_grants.quota stays unchanged; Overage()/Remaining()/IsExhausted()
-- short-circuit on this flag instead.
--
-- Backfill-free: existing rows are all bounded, and the default matches.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "entitlement_grants"
    ADD COLUMN IF NOT EXISTS "unlimited" boolean NOT NULL DEFAULT false;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "entitlement_grants" DROP COLUMN IF EXISTS "unlimited";
