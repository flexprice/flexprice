-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

-- Nullable with no default, so the add is catalog-only.
ALTER TABLE "commitments" ADD COLUMN IF NOT EXISTS "cancellation_fee_amount" numeric(20,8) NULL;

-- migrate:down
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "commitments" DROP COLUMN IF EXISTS "cancellation_fee_amount";
