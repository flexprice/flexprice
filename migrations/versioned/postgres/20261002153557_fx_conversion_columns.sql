-- migrate:up
-- Adaptive multi-currency (FLE-1383): billing currency + frozen conversion columns.
-- All additive and nullable; inert until conversion is turned on at finalize.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "customers"           ADD COLUMN IF NOT EXISTS "billing_currency"  varchar(10)    NULL;
ALTER TABLE "invoices"            ADD COLUMN IF NOT EXISTS "fx_conversion"     jsonb          NULL;
ALTER TABLE "invoice_line_items"  ADD COLUMN IF NOT EXISTS "original_currency" varchar(10)    NULL;
ALTER TABLE "invoice_line_items"  ADD COLUMN IF NOT EXISTS "original_amount"   numeric(20,8)  NULL;

-- migrate:down
ALTER TABLE "invoice_line_items"  DROP COLUMN IF EXISTS "original_amount";
ALTER TABLE "invoice_line_items"  DROP COLUMN IF EXISTS "original_currency";
ALTER TABLE "invoices"            DROP COLUMN IF EXISTS "fx_conversion";
ALTER TABLE "customers"           DROP COLUMN IF EXISTS "billing_currency";
