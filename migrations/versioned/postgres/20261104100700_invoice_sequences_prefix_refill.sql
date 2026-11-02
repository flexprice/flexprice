-- migrate:up
-- Fills counter rows that pods older than the prefix column created during its rollout,
-- so every row is found by the (tenant, environment, prefix, year_month) key.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

UPDATE "invoice_sequences" s
SET "prefix" = COALESCE(
  (SELECT NULLIF(st.value->>'prefix', '')
     FROM "settings" st
    WHERE st.tenant_id = s.tenant_id
      AND st.environment_id = s.environment_id
      AND st.key = 'invoice_config'
      AND st.status = 'published'),
  'INV')
WHERE s."prefix" IS NULL;

-- migrate:down
-- Data-only; nothing to undo.
