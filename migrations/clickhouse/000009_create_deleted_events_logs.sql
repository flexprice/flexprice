CREATE TABLE IF NOT EXISTS flexprice.deleted_events_logs
(
    -- identity of what was deleted; meter_id is '' for an event that produced no meter usage
    event_id             String,
    meter_id             LowCardinality(String),

    -- tenant scope
    tenant_id            LowCardinality(String),
    environment_id       LowCardinality(String),
    external_customer_id LowCardinality(String),

    -- event identity, from meter_usage
    event_name           LowCardinality(String),
    source               LowCardinality(String) DEFAULT '',

    -- from events; zero value when the meter_usage row had no events partner
    event_timestamp      DateTime64(3),
    event_ingested_at    DateTime64(3),

    -- metric, from meter_usage; 0 for an unmetered event
    qty_total            Decimal(25, 15),
    unique_hash          String DEFAULT '',

    -- payload, '' unless the request asked to retain it
    properties           String DEFAULT '' CODEC(ZSTD(3)),

    -- stamped by the INSERT ... SELECT
    deleted_at           DateTime64(3),
    deleted_by           String,
    request_id           String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(deleted_at)
ORDER BY (tenant_id, environment_id, request_id, event_id, meter_id)
SETTINGS index_granularity = 8192;
