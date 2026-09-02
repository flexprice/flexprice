-- ClickHouse schema baseline — REPLICATED variant
-- Database: flexprice
-- Tables:   events, meter_usage (hot path only)
--
-- Same column definitions, indexes, projections, partitioning, ordering, and
-- settings as clickhouse_baseline_20260819.sql. The only differences are:
--
--   1. Engines are Replicated* and take a Keeper path + replica name.
--   2. Statements carry ON CLUSTER so they propagate to every replica.
--
-- REQUIRED MACROS. Each node must define these in its config (the Altinity
-- operator sets them automatically; a hand-rolled cluster needs them in
-- config.d/macros.xml):
--
--   {cluster}  cluster name shared by all nodes
--   {shard}    shard number
--   {replica}  replica identifier, UNIQUE per node in a shard
--
-- Verify before applying:
--   SELECT * FROM system.macros;
--   SELECT cluster, shard_num, replica_num, host_name FROM system.clusters;
--
-- If you apply per-node instead of via the DDL queue, strip the ON CLUSTER
-- clauses and run the file once against each replica. The Keeper path is what
-- joins the replicas together, not the ON CLUSTER clause.
--
-- The Keeper path must be unique per table and stable for the life of the data.
-- Dropping a replicated table leaves its Keeper path in place for
-- database_atomic_delay_before_drop_table_sec (default 480s); recreating the
-- same table inside that window fails with "Replica already exists". Use
-- SYSTEM DROP REPLICA to clear it deliberately.

CREATE DATABASE IF NOT EXISTS flexprice ON CLUSTER '{cluster}';

-- ----------------------------------------------------------------
-- events  (ReplicatedReplacingMergeTree)
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS flexprice.events ON CLUSTER '{cluster}'
(
    `id` String,
    `tenant_id` String,
    `external_customer_id` Nullable(String),
    `environment_id` String,
    `event_name` Nullable(String),
    `customer_id` Nullable(String),
    `source` Nullable(String),
    `timestamp` DateTime64(3) DEFAULT now(),
    `ingested_at` DateTime64(3) DEFAULT now(),
    `properties` Nullable(String),
    INDEX external_customer_id_idx external_customer_id TYPE bloom_filter GRANULARITY 8192,
    INDEX event_name_idx event_name TYPE set(0) GRANULARITY 8192,
    INDEX source_idx source TYPE set(0) GRANULARITY 8192,
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
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{shard}/{database}/{table}', '{replica}', ingested_at)
-- DEVIATION FROM PROD: prod partitions this table monthly, toYYYYMM(timestamp).
-- Daily here per intended design. Changing this on an existing table requires a
-- new table + backfill + EXCHANGE TABLES; the partition key cannot be altered.
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 8192, deduplicate_merge_projection_mode = 'rebuild'
;

-- ----------------------------------------------------------------
-- meter_usage  (ReplicatedReplacingMergeTree)
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS flexprice.meter_usage ON CLUSTER '{cluster}'
(
    `id` String CODEC(ZSTD(1)),
    `tenant_id` LowCardinality(String),
    `environment_id` LowCardinality(String),
    `external_customer_id` LowCardinality(String),
    `meter_id` LowCardinality(String),
    `event_name` LowCardinality(String),
    `timestamp` DateTime CODEC(DoubleDelta, ZSTD(1)),
    `ingested_at` DateTime64(3) DEFAULT now64(3) CODEC(Delta(8), ZSTD(1)),
    `qty_total` Decimal(18, 8) CODEC(ZSTD(1)),
    `unique_hash` String DEFAULT '' CODEC(ZSTD(1)),
    `source` LowCardinality(String) DEFAULT '',
    `properties` String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{shard}/{database}/{table}', '{replica}', ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, meter_id, timestamp)
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)
SETTINGS index_granularity = 8192, parts_to_delay_insert = 150, parts_to_throw_insert = 300, max_bytes_to_merge_at_max_space_in_pool = 2147483648, min_bytes_for_wide_part = 10485760, enable_mixed_granularity_parts = 1
;
