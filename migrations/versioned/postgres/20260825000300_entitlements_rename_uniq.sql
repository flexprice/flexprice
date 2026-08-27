-- migrate:up
-- Step 3 of 3. Restore the Ent-derived name. ALTER INDEX ... RENAME is
-- metadata-only and instant, but still takes ACCESS EXCLUSIVE briefly.
SET lock_timeout = '3s';
ALTER INDEX entitlement_uniq_v2 RENAME TO entitlement_tenant_id_environm_4be9d447f26ab17e315682af3a45d8ea;

-- migrate:down
SET lock_timeout = '3s';
ALTER INDEX entitlement_tenant_id_environm_4be9d447f26ab17e315682af3a45d8ea RENAME TO entitlement_uniq_v2;
