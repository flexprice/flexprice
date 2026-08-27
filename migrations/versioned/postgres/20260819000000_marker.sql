-- migrate:up
-- THE LINE. Deliberately executes nothing.
--
-- Every deployment entered versioned migrations at different points and with
-- different schemas: India prod grew under AutoMigrate, GCP staging was
-- DMS-migrated from AWS into AlloyDB and then diverged, each client is its own
-- lineage. Measured on 2026-08-26, staging differed from a prod-derived baseline
-- by 610 catalog lines. No single file can describe all of them.
--
-- So this does not try. It records a line and claims only:
--
--     "whatever this database contains, it does not need anything before here"
--
-- which is true everywhere. Differences that exist at this point stay, unreconciled.
-- The goal is not to make the fleet identical — it is to stop divergence growing.
--
-- Adopt an existing database with:
--   ./scripts/migrations/adopt.sh <url> migrations/versioned/postgres 20260819000000
--
-- Fresh databases get their schema from migrations/baseline/ first, then adopt at
-- this same marker. dbmate never runs the baseline snapshot.
--
-- EVERY MIGRATION AFTER THIS ONE must be safe on a database it has never seen.
-- No assumptions about starting state: guard, check, then act.
SELECT 'migration baseline marker' AS note;

-- migrate:down
-- Nothing to reverse.
SELECT 'migration baseline marker' AS note;
