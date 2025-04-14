CREATE TABLE flexprice.events (
    id String,
    tenant_id String,
    external_customer_id String,
    environment_id String,
    event_name String,
    customer_id Nullable(String),
    source Nullable(String),
    timestamp DateTime64(3) DEFAULT now(),
    ingested_at DateTime64(3) DEFAULT now(),
    properties String,
    CONSTRAINT check_event_name CHECK event_name != '',
    CONSTRAINT check_tenant_id CHECK tenant_id != '',
    CONSTRAINT check_event_id CHECK id != '',
    CONSTRAINT check_environment_id CHECK environment_id != ''
)
ENGINE = ReplacingMergeTree()
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 8192, allow_nullable_key = 1;
