CREATE TABLE IF NOT EXISTS flexprice.events_processed (
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
    meter_id              String NOT NULL,
    feature_id            String NOT NULL,
    period_id             UInt64 NOT NULL,   -- epoch-ms period start

    /* times */
    timestamp             DateTime64(3) NOT NULL,
    ingested_at           DateTime64(3) NOT NULL,
    processed_at          DateTime64(3) NOT NULL DEFAULT now64(3),

    /* payload snapshot */
    source                Nullable(String),
    properties            String CODEC(ZSTD),

    /* dedup & metrics */
    unique_hash           Nullable(String),
    qty_total             Decimal(25,15) NOT NULL,   -- see §6 on precision
    qty_billable          Decimal(25,15) NOT NULL,
    qty_free_applied      Decimal(25,15) NOT NULL,
    tier_snapshot         Decimal(25,15) NOT NULL,
    unit_cost             Decimal(25,15) NOT NULL,
    cost                  Decimal(25,15) NOT NULL,
    currency              LowCardinality(String) NOT NULL,

    /* audit */
    version               UInt64 NOT NULL DEFAULT toUnixTimestamp64Milli(now64()),
    sign                  Int8   NOT NULL DEFAULT 1,
    final_lag_ms          UInt32 MATERIALIZED
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


ALTER TABLE flexprice.events_processed
ADD INDEX bf_subscription subscription_id TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_feature      feature_id      TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_source       source          TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_unique_hash  unique_hash     TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX set_event_name  event_name      TYPE set(0)             GRANULARITY 128;


------ materialized views

CREATE TABLE IF NOT EXISTS flexprice.agg_usage_period_totals
(
    tenant_id   String NOT NULL,
    environment_id String NOT NULL,
    customer_id String NOT NULL,
    subscription_id String NOT NULL,
    period_id UInt64 NOT NULL,
    feature_id String NOT NULL,
    sub_line_item_id String NOT NULL,
    qty_state   AggregateFunction(sum, Decimal(25,15)) NOT NULL,
    free_state  AggregateFunction(sum, Decimal(25,15)) NOT NULL,
    cost_state  AggregateFunction(sum, Decimal(25,15)) NOT NULL
) ENGINE = AggregatingMergeTree()
  PARTITION BY (tenant_id, environment_id, customer_id, period_id)
  ORDER BY (tenant_id, environment_id, customer_id, period_id, subscription_id, feature_id, sub_line_item_id);

--------------------------------
-- usage_period_totals
--------------------------------
CREATE MATERIALIZED VIEW IF NOT EXISTS flexprice.mv_usage_period_totals
TO flexprice.agg_usage_period_totals AS
SELECT
    tenant_id,
    environment_id,
    customer_id,
    subscription_id,
    period_id,
    feature_id,
    sub_line_item_id,
    sumState(assumeNotNull(qty_billable)) AS qty_state,
    sumState(assumeNotNull(qty_free_applied)) AS free_state,
    sumState(assumeNotNull(cost)) AS cost_state
FROM flexprice.events_processed
GROUP BY
    tenant_id, environment_id, customer_id,
    subscription_id, period_id, feature_id, sub_line_item_id;