-- migrate:up
DO $$
BEGIN
  IF to_regclass('invoice_sequences') IS NOT NULL THEN
    ALTER TABLE "invoice_sequences" ALTER COLUMN "year_month" TYPE character varying(8);
  END IF;
END $$;

-- migrate:down
-- Fails once a YYYYMMDD period row exists.
ALTER TABLE "invoice_sequences" ALTER COLUMN "year_month" TYPE character varying(6);
