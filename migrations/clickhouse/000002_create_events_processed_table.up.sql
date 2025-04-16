CREATE TABLE IF NOT EXISTS events_processed (
    -- Original event fields
    id String,
    tenant_id String,
    external_customer_id String,
    customer_id String,
    event_name String,
    source String,
    timestamp DateTime64(3),
    ingested_at DateTime64(3),
    properties String,
    
    -- Additional processing fields
    processed_at DateTime64(3) DEFAULT now(),
    environment_id String,
    subscription_id String,
    price_id String,
    meter_id String,
    aggregation_field String,
    aggregation_field_value String,
    quantity UInt64,
    cost Decimal(18,9),
    currency String,
    version UInt32 DEFAULT 1,
    
    -- Ensure required fields
    CONSTRAINT check_event_id CHECK (id != '')
) ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(timestamp)
ORDER BY (id, tenant_id, external_customer_id, customer_id, event_name, timestamp)
SETTINGS index_granularity = 8192; 