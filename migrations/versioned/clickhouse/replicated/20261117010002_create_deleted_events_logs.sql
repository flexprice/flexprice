-- migrate:up
CREATE TABLE IF NOT EXISTS flexprice.deleted_events_logs ON CLUSTER '{cluster}'
(
    `event_id` String,
    `meter_id` LowCardinality(String),
    `tenant_id` LowCardinality(String),
    `environment_id` LowCardinality(String),
    `external_customer_id` LowCardinality(String),
    `event_name` LowCardinality(String),
    `source` LowCardinality(String) DEFAULT '',
    `event_timestamp` DateTime64(3),
    `event_ingested_at` DateTime64(3),
    `qty_total` Decimal(25, 15),
    `unique_hash` String DEFAULT '',
    `properties` String DEFAULT '' CODEC(ZSTD(3)),
    `deleted_at` DateTime64(3),
    `deleted_by` String,
    `request_id` String
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{installation}/{shard}/flexprice/deleted_events_logs', '{replica}')
PARTITION BY toYYYYMM(deleted_at)
ORDER BY (tenant_id, environment_id, request_id, event_id, meter_id)
SETTINGS index_granularity = 8192

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
