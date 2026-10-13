-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

-- refunds is unused scaffold: nothing writes it, so every deployment holds zero
-- rows and the NOT NULL adds below need no default or backfill. Confirm before
-- applying:  SELECT count(*) FROM refunds;
--
-- Catalog-only on PG 11+, and the table carries no traffic, so the ACCESS
-- EXCLUSIVE lock is uncontended.

-- A refund no longer requires an originating payment: wallet refunds and refunds
-- of value that never had a payment row carry none of these three.
ALTER TABLE "refunds" ALTER COLUMN "payment_id" DROP NOT NULL;
ALTER TABLE "refunds" ALTER COLUMN "payment_gateway" DROP NOT NULL;
ALTER TABLE "refunds" ALTER COLUMN "gateway_idempotency_token" DROP NOT NULL;

-- invoice_id is the only guaranteed anchor: a void refund of prepaid credits has
-- neither a payment nor a credit note.
ALTER TABLE "refunds" ADD COLUMN IF NOT EXISTS "invoice_id" character varying(50) NOT NULL;
ALTER TABLE "refunds" ADD COLUMN IF NOT EXISTS "credit_note_id" character varying(50) NULL;
ALTER TABLE "refunds" ADD COLUMN IF NOT EXISTS "settled_amount" numeric(20,8) NOT NULL;
ALTER TABLE "refunds" ADD COLUMN IF NOT EXISTS "refund_destination" character varying(50) NOT NULL DEFAULT '';
ALTER TABLE "refunds" ADD COLUMN IF NOT EXISTS "refund_destination_id" character varying(50) NULL;
ALTER TABLE "refunds" ADD COLUMN IF NOT EXISTS "attempt" bigint NOT NULL DEFAULT 1;

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "refunds" DROP COLUMN IF EXISTS "attempt";
ALTER TABLE "refunds" DROP COLUMN IF EXISTS "refund_destination_id";
ALTER TABLE "refunds" DROP COLUMN IF EXISTS "refund_destination";
ALTER TABLE "refunds" DROP COLUMN IF EXISTS "settled_amount";
ALTER TABLE "refunds" DROP COLUMN IF EXISTS "credit_note_id";
ALTER TABLE "refunds" DROP COLUMN IF EXISTS "invoice_id";

-- Restoring these NOT NULLs is only safe while refunds is still empty.
ALTER TABLE "refunds" ALTER COLUMN "gateway_idempotency_token" SET NOT NULL;
ALTER TABLE "refunds" ALTER COLUMN "payment_gateway" SET NOT NULL;
ALTER TABLE "refunds" ALTER COLUMN "payment_id" SET NOT NULL;
