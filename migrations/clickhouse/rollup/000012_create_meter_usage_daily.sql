-- Opt-in; apply with --file.
CREATE TABLE IF NOT EXISTS flexprice.meter_usage_daily
(
    tenant_id             LowCardinality(String)   NOT NULL,
    environment_id        LowCardinality(String)   NOT NULL,
    external_customer_id  LowCardinality(String)   NOT NULL,
    meter_id              LowCardinality(String)   NOT NULL,
    bucket                DateTime                 NOT NULL,
    qty_sum               Decimal(38, 15)          NOT NULL,
    event_count           UInt64                   NOT NULL
)
ENGINE = SummingMergeTree((qty_sum, event_count))
PARTITION BY toYYYYMM(bucket)
ORDER BY (tenant_id, environment_id, external_customer_id, bucket, meter_id)
SETTINGS index_granularity = 256;

CREATE MATERIALIZED VIEW IF NOT EXISTS flexprice.meter_usage_daily_mv
TO flexprice.meter_usage_daily
AS
SELECT
    tenant_id,
    environment_id,
    external_customer_id,
    meter_id,
    toStartOfDay(timestamp, 'UTC') AS bucket,
    sum(toDecimal128(qty_total, 15)) AS qty_sum,
    count() AS event_count
FROM flexprice.meter_usage
GROUP BY tenant_id, environment_id, external_customer_id, meter_id, bucket;
