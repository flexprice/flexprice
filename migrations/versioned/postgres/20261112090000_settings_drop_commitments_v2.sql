-- migrate:up
-- Commitments are on in every environment, so the commitments_v2 setting is gone; its rows, published and archived,
-- go with it. settings is small, and re-running deletes nothing.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DO $$
BEGIN
  IF to_regclass('settings') IS NOT NULL THEN
    DELETE FROM "settings" WHERE "key" = 'commitments_v2';
  END IF;
END $$;

-- migrate:down
-- Irreversible: the rows only toggled a gate the code no longer has.
