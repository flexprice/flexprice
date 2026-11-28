-- migrate:up
-- Each pause's proration_behavior decides whether its paused days are taken off the fixed charges.
-- A constant default is metadata-only, so existing rows read as none without a rewrite.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "subscription_pauses" ADD COLUMN IF NOT EXISTS "proration_behavior" character varying(50) NOT NULL DEFAULT 'none';

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "subscription_pauses" DROP COLUMN IF EXISTS "proration_behavior";
