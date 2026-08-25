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
-- an INVALID index behind.
--
-- Deliberately NO 'IF NOT EXISTS'. With it, a retry after such a failure skips the
-- broken index silently — and then 000200 drops the index that was still enforcing
-- and 000300 renames the invalid one over it, leaving the table with no effective
-- uniqueness. Without it the retry fails loudly with "relation already exists", and
-- the operator clears it first:
--
--   DROP INDEX CONCURRENTLY IF EXISTS entitlement_uniq_v2;
--
-- Check for one before running this migration:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE UNIQUE INDEX CONCURRENTLY entitlement_uniq_v2
  ON entitlements (tenant_id, environment_id, entity_type, entity_id, feature_id)
  WHERE (((status)::text = 'published'::text) AND ((aggregation_mode)::text <> 'parallel'::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS entitlement_uniq_v2;
