-- 000010_align_events_with_prod_schema
--
-- WHY: The production ClickHouse `flexprice.events` table has drifted out-of-band
-- from migration 000001. Production carries column-level CODECs, LowCardinality
-- typing on event_name/source, a `proj_by_customer_event` PROJECTION, and has
-- DROPPED the three secondary indexes that 000001 created — none of which were
-- ever committed to this repo. The existing staging CH still matches 000001.
--
-- Surfaced by the prod->staging CH migration rehearsal (2026-06-17): a
-- `clickhouse-backup restore --data` ATTACH of production parts into a
-- 000001-built `events` table FAILS, because ATTACH requires byte-identical
-- column types, codecs and projections. The AWS->GCP cutover restores prod parts
-- into the new GCP CH, so the GCP `events` schema MUST match production.
--
-- HOW: 000001 already created `flexprice.events` (every env, including a fresh
-- GCP CH, runs 000001 before 000010). So this migration does NOT re-create the
-- table — it ALTERs the existing 000001-era table into the production schema.
-- Idempotent (IF EXISTS / IF NOT EXISTS, MODIFY COLUMN to target type is a no-op
-- when already applied), so re-running is safe and a table already at the prod
-- schema is left unchanged.
--
-- ⚠️ ORDER MATTERS: a column that backs a secondary index cannot be MODIFYed
-- (CH error 524 ALTER_OF_COLUMN_IS_FORBIDDEN). So DROP the indexes that 000001
-- created on event_name / source FIRST, then re-type the columns.
--
-- ⚠️ HEAVY ON LARGE EXISTING TABLES. `MODIFY COLUMN` (codec/LowCardinality change)
-- and `MATERIALIZE PROJECTION` rewrite every data part in the BACKGROUND. CH
-- accepts the statements quickly (mutation is async — see system.mutations), but
-- the actual rewrite of a multi-hundred-GiB events table takes hours of disk I/O.
-- Ship the deploy carrying this migration in a low-traffic window and watch
-- `SELECT * FROM system.mutations WHERE is_done = 0` until it drains.

-- 1. Drop the 000001 secondary indexes FIRST (they block MODIFY of their columns).
ALTER TABLE flexprice.events DROP INDEX IF EXISTS external_customer_id_idx;
ALTER TABLE flexprice.events DROP INDEX IF EXISTS event_name_idx;
ALTER TABLE flexprice.events DROP INDEX IF EXISTS source_idx;

-- 2. Re-type columns + add codecs to match prod.
ALTER TABLE flexprice.events MODIFY COLUMN event_name LowCardinality(String) CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN source LowCardinality(String) DEFAULT '' CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN customer_id String DEFAULT '' CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN id String CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN tenant_id String CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN external_customer_id String CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN environment_id String CODEC(ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN timestamp DateTime64(3) DEFAULT now() CODEC(Delta(8), ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN ingested_at DateTime64(3) DEFAULT now() CODEC(Delta(8), ZSTD(3));
ALTER TABLE flexprice.events MODIFY COLUMN properties String CODEC(ZSTD(6));

-- 3. Set merge/insert settings FIRST. deduplicate_merge_projection_mode MUST be
--    set before ADD PROJECTION — a ReplacingMergeTree rejects a projection while
--    the mode is the default 'throw' (CH error 344 SUPPORT_IS_DISABLED).
ALTER TABLE flexprice.events MODIFY SETTING parts_to_delay_insert = 2000, parts_to_throw_insert = 4000, deduplicate_merge_projection_mode = 'rebuild';

-- 4. Add + materialize the prod projection.
ALTER TABLE flexprice.events ADD PROJECTION IF NOT EXISTS proj_by_customer_event
  (SELECT * ORDER BY tenant_id, environment_id, external_customer_id, event_name, timestamp, id);
ALTER TABLE flexprice.events MATERIALIZE PROJECTION proj_by_customer_event;
