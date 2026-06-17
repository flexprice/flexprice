-- 000010_align_events_with_prod_schema
--
-- WHY: The production ClickHouse `flexprice.events` table has drifted out-of-band
-- from migration 000001. Production carries column-level CODECs, LowCardinality
-- typing on event_name/source, a `proj_by_customer_event` PROJECTION, and has
-- DROPPED the three secondary indexes that 000001 created — none of which were
-- ever committed to this repo. The existing staging CH still matches 000001.
--
-- This was surfaced by the prod->staging CH migration rehearsal (2026-06-17):
-- a `clickhouse-backup restore --data` ATTACH of production parts into a
-- 000001-built `events` table FAILS, because ATTACH requires byte-identical
-- column types, codecs and projections. The AWS->GCP cutover restores prod
-- parts into the new GCP CH, so the GCP `events` schema MUST match production.
--
-- This migration makes the repo the source of truth for the *production*
-- `events` schema so a fresh deploy (e.g. the new GCP CH) is ATTACH-compatible
-- with production backups.
--
-- ── Fresh deploy (new env, e.g. GCP CH): CREATE matches prod exactly ──────────
CREATE TABLE IF NOT EXISTS flexprice.events
(
    `id` String CODEC(ZSTD(3)),
    `tenant_id` String CODEC(ZSTD(3)),
    `external_customer_id` String CODEC(ZSTD(3)),
    `environment_id` String CODEC(ZSTD(3)),
    `event_name` LowCardinality(String) CODEC(ZSTD(3)),
    `customer_id` String DEFAULT '' CODEC(ZSTD(3)),
    `source` LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `timestamp` DateTime64(3) DEFAULT now() CODEC(Delta(8), ZSTD(3)),
    `ingested_at` DateTime64(3) DEFAULT now() CODEC(Delta(8), ZSTD(3)),
    `properties` String CODEC(ZSTD(6)),
    CONSTRAINT check_event_name CHECK event_name != '',
    CONSTRAINT check_tenant_id CHECK tenant_id != '',
    CONSTRAINT check_event_id CHECK id != '',
    CONSTRAINT check_environment_id CHECK environment_id != '',
    PROJECTION proj_by_customer_event
    (
        SELECT *
        ORDER BY
            tenant_id,
            environment_id,
            external_customer_id,
            event_name,
            timestamp,
            id
    )
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 16384,
    parts_to_delay_insert = 2000,
    parts_to_throw_insert = 4000,
    max_bytes_to_merge_at_max_space_in_pool = 5368709120,
    deduplicate_merge_projection_mode = 'rebuild';

-- ── Existing env reconciliation (envs already on the 000001 schema) ───────────
-- These ALTERs bring an EXISTING events table in line with production. They are
-- idempotent (IF EXISTS / IF NOT EXISTS, and MODIFY COLUMN to the target type is
-- a no-op when already that type), so on a FRESH env the CREATE above already
-- built the final schema and every ALTER below no-ops.
--
-- ⚠️ HEAVY ON LARGE EXISTING TABLES. `MODIFY COLUMN` (codec/LowCardinality change)
-- and `MATERIALIZE PROJECTION` rewrite every data part in the BACKGROUND. CH
-- accepts the statements quickly (mutation is async — see system.mutations), but
-- the actual rewrite of a multi-hundred-GiB events table takes hours of disk I/O.
-- Schedule the deploy that ships this migration into a low-traffic window and
-- watch `SELECT * FROM system.mutations WHERE is_done = 0` until it drains.
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
ALTER TABLE flexprice.events DROP INDEX IF EXISTS external_customer_id_idx;
ALTER TABLE flexprice.events DROP INDEX IF EXISTS event_name_idx;
ALTER TABLE flexprice.events DROP INDEX IF EXISTS source_idx;
ALTER TABLE flexprice.events ADD PROJECTION IF NOT EXISTS proj_by_customer_event
  (SELECT * ORDER BY tenant_id, environment_id, external_customer_id, event_name, timestamp, id);
ALTER TABLE flexprice.events MATERIALIZE PROJECTION proj_by_customer_event;
ALTER TABLE flexprice.events MODIFY SETTING parts_to_delay_insert = 2000, parts_to_throw_insert = 4000, deduplicate_merge_projection_mode = 'rebuild';
