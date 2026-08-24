-- migrate:up transaction:false
-- Step 2 of 3. Drop the broader index now that entitlement_uniq_v2 covers it.
-- CONCURRENTLY: a plain DROP INDEX takes ACCESS EXCLUSIVE and queues every query
-- arriving behind it (RCA: prod DDL-lock incident 2026-06-25).
DROP INDEX CONCURRENTLY IF EXISTS entitlement_tenant_id_environm_4be9d447f26ab17e315682af3a45d8ea;

-- migrate:down transaction:false
-- Recreating this takes as long as the original build. Run by hand, off the deploy path:
--   CREATE UNIQUE INDEX CONCURRENTLY entitlement_tenant_id_environm_4be9d447f26ab17e315682af3a45d8ea
--     ON entitlements (tenant_id, environment_id, entity_type, entity_id, feature_id)
--     WHERE ((status)::text = 'published'::text);
