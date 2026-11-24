-- migrate:up
CREATE TABLE IF NOT EXISTS flexprice.activity_logs ON CLUSTER '{cluster}'
(
    `id` String,
    `tenant_id` String,
    `environment_id` String,
    `entity_type` LowCardinality(String),
    `entity_id` String,
    `entity_label` String,
    `customer_id` String,
    `action` LowCardinality(String),
    `actor_type` LowCardinality(String),
    `actor_id` String,
    `source` LowCardinality(String),
    `request_id` String,
    `ip` String,
    `user_agent` String,
    `state` String CODEC(ZSTD(3)),
    `schema_version` UInt16,
    `occurred_at` DateTime64(3, 'UTC'),
    `ingested_at` DateTime64(3, 'UTC') DEFAULT now64(3),
    INDEX idx_customer_id customer_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_entity_id entity_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_actor_id actor_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_request_id request_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{installation}/{shard}/flexprice/activity_logs', '{replica}', ingested_at)
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, environment_id, occurred_at, id)
SETTINGS index_granularity = 8192

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
