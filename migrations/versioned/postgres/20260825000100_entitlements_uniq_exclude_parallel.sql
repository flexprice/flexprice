-- migrate:up transaction:false
-- Step 1 of 3. Build the corrected index alongside the existing one.
--
-- ent/schema/entitlement.go narrowed this predicate on 2026-07-25 (ae95c9d54) to
-- exclude aggregation_mode='parallel'. AutoMigrate never applied it — ModifyIndex
-- is in the skip set — so production still enforces uniqueness across ALL published
-- rows, which is broader than the code intends.
--
-- Built under a temporary name so the old index keeps enforcing throughout: there is
-- no window where uniqueness is unenforced. Steps 2 and 3 drop and rename.
--
-- statement_timeout must be 0 on the connection: a build killed by a timeout leaves
-- an INVALID index that CREATE ... IF NOT EXISTS will silently skip forever.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS entitlement_uniq_v2
  ON entitlements (tenant_id, environment_id, entity_type, entity_id, feature_id)
  WHERE (((status)::text = 'published'::text) AND ((aggregation_mode)::text <> 'parallel'::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS entitlement_uniq_v2;
