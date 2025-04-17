CREATE TABLE IF NOT EXISTS flexprice.events (
    id String NOT NULL,
    tenant_id String NOT NULL,
    external_customer_id String  NOT NULL,
    environment_id String NOT NULL, 
    event_name String  NOT NULL,
    customer_id Nullable(String),
    source Nullable(String),
    timestamp DateTime64(3) NOT NULL DEFAULT now(),
    ingested_at DateTime64(3) NOT NULL DEFAULT now(),
    properties String,
    CONSTRAINT check_event_name CHECK event_name != '',
    CONSTRAINT check_tenant_id CHECK tenant_id != '',
    CONSTRAINT check_event_id CHECK id != '',
    CONSTRAINT check_environment_id CHECK environment_id != ''
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 8192;

-- Bloom Filter for external_customer_id
ALTER TABLE flexprice.events
ADD INDEX external_customer_id_idx external_customer_id TYPE bloom_filter GRANULARITY 8192;

-- Set Index for event_name
ALTER TABLE flexprice.events
ADD INDEX event_name_idx event_name TYPE set(0) GRANULARITY 8192;

-- Set Index for source
ALTER TABLE flexprice.events
ADD INDEX source_idx source TYPE set(0) GRANULARITY 8192;

