-- migrate:up transaction:false
-- Counters are keyed by prefix from here on. Pods on the previous release name no
-- unique index when incrementing, so they keep working once this is gone.
-- CONCURRENTLY: a plain DROP INDEX takes ACCESS EXCLUSIVE and queues every query behind it.
DROP INDEX CONCURRENTLY IF EXISTS "invoicesequence_tenant_id_environment_id_year_month";

-- migrate:down transaction:false
-- Fails if two prefixes already have a row for the same period. Run by hand, off the deploy path:
--   CREATE UNIQUE INDEX CONCURRENTLY "invoicesequence_tenant_id_environment_id_year_month"
--     ON "invoice_sequences" ("tenant_id", "environment_id", "year_month");
