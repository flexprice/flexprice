CREATE TABLE IF NOT EXISTS flexprice.feature_usage (
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
    sign                  Int8   NOT NULL DEFAULT 1,
    processing_lag_ms     UInt32 MATERIALIZED
                              datediff('millisecond', timestamp, processed_at)
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id, customer_id)
ORDER BY (
    tenant_id, environment_id, customer_id,
    period_id, feature_id, timestamp, sub_line_item_id, id
)
SETTINGS index_granularity = 8192;


ALTER TABLE flexprice.feature_usage
ADD INDEX bf_subscription subscription_id TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_feature      feature_id      TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_source       source          TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_unique_hash  unique_hash     TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_price        price_id        TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_meter        meter_id        TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX set_event_name  event_name      TYPE set(0)             GRANULARITY 128;


------ materialized views

CREATE TABLE IF NOT EXISTS flexprice.agg_feature_usage_period_totals
(
    tenant_id   String NOT NULL,
    environment_id String NOT NULL,
    customer_id String NOT NULL,
    subscription_id String NOT NULL,
    period_id UInt64 NOT NULL,
    feature_id String NOT NULL,
    sub_line_item_id String NOT NULL,
    qty_state   AggregateFunction(sum, Decimal(25,15)) NOT NULL
) ENGINE = AggregatingMergeTree()
  PARTITION BY (tenant_id, environment_id, customer_id, period_id)
  ORDER BY (tenant_id, environment_id, customer_id, period_id, subscription_id, feature_id, sub_line_item_id);

--------------------------------
-- feature_usage_period_totals
--------------------------------
CREATE MATERIALIZED VIEW IF NOT EXISTS flexprice.mv_feature_usage_period_totals
TO flexprice.agg_feature_usage_period_totals AS
SELECT
    tenant_id,
    environment_id,
    customer_id,
    subscription_id,
    period_id,
    feature_id,
    sub_line_item_id,
    sumState(assumeNotNull(qty_total)) AS qty_state
FROM flexprice.feature_usage
GROUP BY
    tenant_id, environment_id, customer_id,
    subscription_id, period_id, feature_id, sub_line_item_id;


----------------------------
-- Migration data from events_processed to feature_usage
----------------------------
INSERT INTO flexprice.feature_usage (
    id, tenant_id, environment_id, external_customer_id, event_name,
    customer_id, subscription_id, sub_line_item_id, price_id, feature_id, meter_id, period_id,
    timestamp, ingested_at, processed_at,
    source, properties,
    unique_hash, qty_total,
    version, sign
)
SELECT 
    id, tenant_id, environment_id, external_customer_id, event_name,
    customer_id, subscription_id, sub_line_item_id, price_id, feature_id, meter_id, period_id,
    timestamp, ingested_at, processed_at,
    source, properties,
    unique_hash, qty_total,
    version, sign
FROM flexprice.events_processed
WHERE sign = 1 
  AND timestamp >= date_sub(date_trunc('month', now()), INTERVAL 6 MONTH)  -- 2 months ago start
  AND timestamp < date_sub(date_trunc('month', now()), INTERVAL 2 MONTH)   -- 2 months ago end
SETTINGS max_memory_usage = 500000000; 