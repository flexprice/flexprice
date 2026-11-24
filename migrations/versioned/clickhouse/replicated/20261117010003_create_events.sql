-- migrate:up
CREATE TABLE IF NOT EXISTS flexprice.events ON CLUSTER '{cluster}'
(
    `id` String,
    `tenant_id` String,
    `external_customer_id` String,
    `environment_id` String,
    `event_name` String,
    `customer_id` Nullable(String),
    `source` Nullable(String),
    `timestamp` DateTime64(3) DEFAULT now(),
    `ingested_at` DateTime64(3) DEFAULT now(),
    `properties` Nullable(String),
    INDEX external_customer_id_idx external_customer_id TYPE bloom_filter GRANULARITY 8192,
    INDEX event_name_idx event_name TYPE set(0) GRANULARITY 8192,
    INDEX source_idx source TYPE set(0) GRANULARITY 8192,
    INDEX mm_ingested_at ingested_at TYPE minmax GRANULARITY 1,
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
ENGINE = ReplicatedReplacingMergeTree('/clickhouse/tables/{installation}/{shard}/flexprice/events', '{replica}', ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 16384, parts_to_delay_insert = 200, parts_to_throw_insert = 400, max_bytes_to_merge_at_max_space_in_pool = 5368709120, deduplicate_merge_projection_mode = 'rebuild'

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
