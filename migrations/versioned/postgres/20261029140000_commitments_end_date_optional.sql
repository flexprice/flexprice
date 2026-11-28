-- migrate:up
-- A commitment without an end_date is open-ended: it runs until terminated (or its subscription ends).
-- Dropping NOT NULL only touches the catalog, and re-running it is a no-op.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DO $$
BEGIN
  IF to_regclass('commitments') IS NOT NULL THEN
    ALTER TABLE "commitments" ALTER COLUMN "end_date" DROP NOT NULL;
  END IF;
END $$;

-- migrate:down
-- Fails once an open-ended commitment exists: give it an end_date or delete it first.
ALTER TABLE "commitments" ALTER COLUMN "end_date" SET NOT NULL;
