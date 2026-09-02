-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one
-- statement per file. Split from 000002_create_raw_events.sql.
CREATE TABLE IF NOT EXISTS flexprice.raw_events
(
    `id` String,
    `tenant_id` String,
    `environment_id` String,
    `external_customer_id` String,
    `event_name` String DEFAULT '',
    `source` Nullable(String),
    `payload` String CODEC(ZSTD(3)),
    `field1` Nullable(String),
    `field2` Nullable(String),
    `field3` Nullable(String),
    `field4` Nullable(String),
    `field5` Nullable(String),
    `field6` Nullable(String),
    `field7` Nullable(String),
    `field8` Nullable(String),
    `field9` Nullable(String),
    `field10` Nullable(String),
    `timestamp` DateTime64(3),
    `ingested_at` DateTime64(3) DEFAULT now64(3),
    `version` UInt64 DEFAULT toUnixTimestamp64Milli(now64()),
    `sign` Int8 DEFAULT 1,
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, timestamp)
ORDER BY (tenant_id, environment_id, external_customer_id, timestamp, id)
SETTINGS index_granularity = 16384,
    parts_to_delay_insert = 200,
    parts_to_throw_insert = 400,
    max_bytes_to_merge_at_max_space_in_pool = 5368709120

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
