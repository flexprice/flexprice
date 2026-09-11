-- migrate:up
-- Provenance, not live state: "checkout" means a hosted checkout session created this
-- invoice. The guards read it to decide whether a session lookup is possible at all, so
-- every ordinary invoice skips the query. Immutable once set.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

ALTER TABLE "invoices" ADD COLUMN IF NOT EXISTS "source_type" character varying NULL;

-- migrate:down
ALTER TABLE "invoices" DROP COLUMN IF EXISTS "source_type";
