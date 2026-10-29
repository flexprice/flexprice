-- migrate:up
-- Fills each counter row with its environment's current invoice prefix (stored
-- invoice_config, else the default INV). Only empty rows are touched, so a re-run is a no-op.
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
-- Data-only; the column drop in the previous file's down block removes the values.
