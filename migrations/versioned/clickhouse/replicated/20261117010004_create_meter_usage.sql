-- migrate:up
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
    `properties` String DEFAULT '' CODEC(ZSTD(3)),
    INDEX bf_external_customer_id external_customer_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{installation}/{shard}/flexprice/meter_usage', '{replica}', ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, meter_id, timestamp)
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)
SETTINGS index_granularity = 8192, parts_to_delay_insert = 150, parts_to_throw_insert = 300, max_bytes_to_merge_at_max_space_in_pool = 2147483648, min_bytes_for_wide_part = 10485760, enable_mixed_granularity_parts = 1

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
