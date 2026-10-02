-- migrate:up
-- numeric(25,15) leaves ten digits before the decimal point, so the largest
-- value these columns held was 9,999,999,999. A metered entitlement now derives
-- a grant config on create and carries its ceiling in grant_quota, so a meter
-- counting bytes overflows on anything past ~10 GB and the create fails with a
-- numeric overflow. usage_limit never had this problem: it is a bigint.
--
-- Widening precision and leaving scale alone is lossless, so this rewrites no
-- values: verified on Postgres 17 that pg_relation_filenode is unchanged across
-- the ALTER. quota and usage move together, in one statement so the table is
-- locked once: raising only the ceiling would still overflow once measured usage
-- passed ten digits.
--
-- Guarded on the table existing and idempotent on the type, so a database that
-- already has the wider column is a no-op and a re-run changes nothing.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DO $$
BEGIN
  IF to_regclass('public.entitlements') IS NOT NULL THEN
    ALTER TABLE "entitlements" ALTER COLUMN "grant_quota" TYPE numeric(34,15);
  END IF;

  IF to_regclass('public.entitlement_grants') IS NOT NULL THEN
    ALTER TABLE "entitlement_grants" ALTER COLUMN "quota" TYPE numeric(34,15),
                                     ALTER COLUMN "usage" TYPE numeric(34,15);
  END IF;
END
$$;

-- migrate:down
-- Narrowing again fails on any row that has since used the extra range, which is
-- the point: the rollback refuses rather than truncating a customer's quota.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DO $$
BEGIN
  IF to_regclass('public.entitlements') IS NOT NULL THEN
    ALTER TABLE "entitlements" ALTER COLUMN "grant_quota" TYPE numeric(25,15);
  END IF;

  IF to_regclass('public.entitlement_grants') IS NOT NULL THEN
    ALTER TABLE "entitlement_grants" ALTER COLUMN "quota" TYPE numeric(25,15),
                                     ALTER COLUMN "usage" TYPE numeric(25,15);
  END IF;
END
$$;
