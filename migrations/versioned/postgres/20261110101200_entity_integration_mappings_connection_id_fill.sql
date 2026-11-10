-- migrate:up transaction:false
-- Sets connection_id from the environment's single published, non-storage connection of the row's
-- provider; ambiguous or missing connections are left NULL. Batches of 10,000 commit separately.
-- Only NULL rows are touched, so a re-run is a no-op.
DO $$
DECLARE
  updated_rows integer;
  previous_lock_timeout text := current_setting('lock_timeout');
BEGIN
  -- Session-level so it holds across the batch commits; a single statement keeps COMMIT legal.
  PERFORM set_config('lock_timeout', '3s', false);
  LOOP
    WITH batch AS (
      SELECT m.id, r.connection_id
        FROM "entity_integration_mappings" m
        CROSS JOIN LATERAL (
          SELECT min(c.id) AS connection_id
            FROM "connections" c
           WHERE c.tenant_id = m.tenant_id
             AND c.environment_id = m.environment_id
             AND c.provider_type = m.provider_type
             AND c.status = 'published'
             AND c.provider_type NOT IN ('s3', 'gcs')
          HAVING count(*) = 1
        ) r
       WHERE m.connection_id IS NULL
       LIMIT 10000
    )
    UPDATE "entity_integration_mappings" m
       SET connection_id = batch.connection_id
      FROM batch
     WHERE m.id = batch.id;
    GET DIAGNOSTICS updated_rows = ROW_COUNT;
    COMMIT;
    EXIT WHEN updated_rows = 0;
  END LOOP;
  PERFORM set_config('lock_timeout', previous_lock_timeout, false);
END $$;

-- migrate:down transaction:false
-- Data-only; the column drop in the previous file's down block removes the values.
