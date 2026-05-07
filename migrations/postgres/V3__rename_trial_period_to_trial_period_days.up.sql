-- Rename trial columns when present (safe if tables do not exist yet or column was already renamed).
-- docker-compose mounts this folder to initdb.d before Ent creates schema on fresh volumes.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'prices' AND column_name = 'trial_period'
  ) THEN
    ALTER TABLE prices RENAME COLUMN trial_period TO trial_period_days;
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'subscription_line_items' AND column_name = 'trial_period'
  ) THEN
    ALTER TABLE subscription_line_items RENAME COLUMN trial_period TO trial_period_days;
  END IF;
END $$;
