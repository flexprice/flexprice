-- ClickHouse schema baseline
-- Database: flexprice
-- Server:   26.3.3.20
--
-- Generated with SHOW CREATE against the live server.
-- Ordered: tables first, then views and materialized views.

CREATE DATABASE IF NOT EXISTS flexprice;


-- ----------------------------------------------------------------
-- events  (ReplacingMergeTree)
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS flexprice.events
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
ENGINE = ReplacingMergeTree(ingested_at)
-- DEVIATION FROM PROD: prod partitions this table monthly, toYYYYMM(timestamp).
-- Changed to daily here per intended design. Applying this to an existing prod
-- table requires a new table + backfill + EXCHANGE TABLES; the partition key
-- cannot be altered in place.
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 8192, deduplicate_merge_projection_mode = 'rebuild'
;

-- ----------------------------------------------------------------
-- events_16_06_25  (ReplacingMergeTree)
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS flexprice.events_16_06_25
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
    CONSTRAINT check_event_name CHECK event_name != '',
    CONSTRAINT check_tenant_id CHECK tenant_id != '',
    CONSTRAINT check_event_id CHECK id != '',
    CONSTRAINT check_environment_id CHECK environment_id != ''
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 8192
;


-- ----------------------------------------------------------------
-- meter_usage  (ReplacingMergeTree)
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS flexprice.meter_usage
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
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, meter_id, timestamp)
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)
SETTINGS index_granularity = 8192, parts_to_delay_insert = 150, parts_to_throw_insert = 300, max_bytes_to_merge_at_max_space_in_pool = 2147483648, min_bytes_for_wide_part = 10485760, enable_mixed_granularity_parts = 1
;

