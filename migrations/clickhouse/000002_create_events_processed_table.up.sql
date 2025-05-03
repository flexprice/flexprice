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
    feature_id String,
    aggregation_field String,
    aggregation_field_value String,
    quantity UInt64,
    cost Decimal(18,9),
    currency String,
    event_status Enum('pending', 'processed', 'failed') DEFAULT 'pending',
    version UInt32 DEFAULT 1,
    
    -- Ensure required fields
    CONSTRAINT check_event_id CHECK (id != '')
) ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(timestamp)
ORDER BY (id, tenant_id, external_customer_id, customer_id, event_name, timestamp)
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