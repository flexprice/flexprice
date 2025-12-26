CREATE TABLE IF NOT EXISTS flexprice.costsheet_usage (
    /* immutable ids */
    id                    String NOT NULL,
    tenant_id             String NOT NULL,
    environment_id        String NOT NULL,
    external_customer_id  String NOT NULL,
    event_name            String NOT NULL,

    /* resolution result */
    customer_id           String NOT NULL,
    costsheet_id          String NOT NULL,
    price_id              String NOT NULL,
    meter_id              String NOT NULL,
    feature_id            String NOT NULL,

    /* times */
    timestamp             DateTime64(3) NOT NULL,
    ingested_at           DateTime64(3) NOT NULL,
    processed_at          DateTime64(3) NOT NULL DEFAULT now64(3),

    /* payload snapshot */
    source                Nullable(String),
    properties            String CODEC(ZSTD),

    /* usage metrics */
    unique_hash           Nullable(String),
    qty_total             Decimal(25,15) NOT NULL,

    /* audit */
    version               UInt64 NOT NULL DEFAULT toUnixTimestamp64Milli(now64()),
    sign                  Int8   NOT NULL DEFAULT 1,
    processing_lag_ms     UInt32 MATERIALIZED
                              datediff('millisecond', timestamp, processed_at)
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id, customer_id)
ORDER BY (
    tenant_id, environment_id, customer_id, costsheet_id, feature_id, price_id, meter_id, timestamp, id
)
SETTINGS index_granularity = 8192;


ALTER TABLE flexprice.costsheet_usage
ADD INDEX IF NOT EXISTS bf_costsheet    costsheet_id    TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX IF NOT EXISTS bf_feature      feature_id      TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX IF NOT EXISTS bf_source       source          TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX IF NOT EXISTS bf_unique_hash  unique_hash     TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX IF NOT EXISTS bf_price        price_id        TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX IF NOT EXISTS bf_meter        meter_id        TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX IF NOT EXISTS set_event_name  event_name      TYPE set(0)             GRANULARITY 128;
