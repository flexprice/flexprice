CREATE TABLE IF NOT EXISTS events_processed (
    -- Original event fields
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
    
    -- Additional processing fields
    event_status Enum('pending', 'processed', 'failed') DEFAULT 'pending',
    processed_at DateTime64(3) DEFAULT now(),
    subscription_id Nullable(String),
    price_id Nullable(String),
    meter_id Nullable(String),
    feature_id Nullable(String),
    aggregation_field Nullable(String),
    aggregation_field_value Nullable(String),
    quantity Nullable(UInt64),
    cost Nullable(Decimal(18,9)),
    currency Nullable(String),

    CONSTRAINT check_event_name CHECK event_name != '',
    CONSTRAINT check_tenant_id CHECK tenant_id != '',
    CONSTRAINT check_event_id CHECK id != '',
    CONSTRAINT check_environment_id CHECK environment_id != ''
)
ENGINE = ReplacingMergeTree(processed_at)
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id)
ORDER BY (tenant_id, environment_id, timestamp, id)
SETTINGS index_granularity = 8192;

-- Bloom Filter for external_customer_id
ALTER TABLE events_processed
ADD INDEX external_customer_id_idx external_customer_id TYPE bloom_filter GRANULARITY 8192;

-- Bloom Filter for customer_id
ALTER TABLE events_processed
ADD INDEX customer_id_idx customer_id TYPE bloom_filter GRANULARITY 8192;

-- Bloom Filter for subscription_id
ALTER TABLE events_processed
ADD INDEX subscription_id_idx subscription_id TYPE bloom_filter GRANULARITY 8192;

-- Bloom Filter for feature_id
ALTER TABLE events_processed
ADD INDEX feature_id_idx feature_id TYPE bloom_filter GRANULARITY 8192;

-- Set Index for event_name
ALTER TABLE events_processed
ADD INDEX event_name_idx event_name TYPE set(0) GRANULARITY 8192;

-- Set Index for source
ALTER TABLE events_processed
ADD INDEX source_idx source TYPE set(0) GRANULARITY 8192;

-- Set Index for event_status
ALTER TABLE events_processed
ADD INDEX event_status_idx event_status TYPE set(0) GRANULARITY 8192; 