-- migrate:up transaction:false
-- Step 4 of 4. Drop an orphaned duplicate that exists only in production.
--
-- entitlement_tenant_id_environment_id_entity_type_entity_id_feat is a leftover
-- from an older Ent index-naming scheme (natural name truncated at 63 chars, before
-- Ent switched to the <prefix>_<hash> form). Ent no longer manages it, so
-- AutoMigrate never touched it — DropIndex is in the skip set.
--
-- After step 3 it is byte-identical to the index Ent does manage: same five
-- columns, same predicate, both UNIQUE. Two identical unique indexes on a hot
-- table means double the write amplification for zero added protection.
--
-- Fresh databases never had it, so this is a no-op there. Guarded by IF EXISTS.
DROP INDEX CONCURRENTLY IF EXISTS entitlement_tenant_id_environment_id_entity_type_entity_id_feat;

-- migrate:down transaction:false
-- Deliberately not reversible. Recreating a duplicate index is never the right
-- recovery: the equivalent index survives under the Ent-managed name.
