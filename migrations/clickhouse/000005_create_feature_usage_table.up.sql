-- older version of the feature_usage table can be found in 000003_create_feature_usage_table.up.sql

CREATE TABLE IF NOT EXISTS flexprice.feature_usage
(
    /* immutable ids */
    id                    String NOT NULL,
    tenant_id             String NOT NULL,
    environment_id        String NOT NULL,
    external_customer_id  String NOT NULL,
    event_name            String NOT NULL,

    /* resolution result */
    customer_id           String NOT NULL,
    subscription_id       String NOT NULL,
    sub_line_item_id      String NOT NULL,
    price_id              String NOT NULL,
    feature_id            String NOT NULL,
    meter_id              Nullable(String),
    period_id             UInt64 NOT NULL,   -- epoch-ms period start

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
    sign                  Int8   NOT NULL DEFAULT 1
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, customer_id, timestamp)
ORDER BY
(
    tenant_id, environment_id, customer_id,
    timestamp,
    period_id,
    feature_id,
    sub_line_item_id,
    id
)
SETTINGS
    index_granularity = 16384,
    parts_to_delay_insert = 200,
    parts_to_throw_insert = 400,
    max_bytes_to_merge_at_max_space_in_pool = 5368709120; -- 5 GiB
    
ALTER TABLE flexprice.feature_usage
    ADD INDEX IF NOT EXISTS bf_feature_id feature_id TYPE bloom_filter(0.01) GRANULARITY 64;

ALTER TABLE flexprice.feature_usage
    ADD INDEX IF NOT EXISTS mm_ts timestamp TYPE minmax GRANULARITY 1;

ALTER TABLE flexprice.feature_usage
    ADD INDEX IF NOT EXISTS bf_external_customer_id external_customer_id TYPE bloom_filter(0.01) GRANULARITY 64;
