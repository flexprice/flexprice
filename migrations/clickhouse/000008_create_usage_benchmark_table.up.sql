CREATE TABLE IF NOT EXISTS flexprice.usage_benchmark
(
    tenant_id             LowCardinality(String)  NOT NULL,
    environment_id        LowCardinality(String)  NOT NULL,
    subscription_id       String                  NOT NULL CODEC(ZSTD(1)),
    start_time            DateTime64(3)           NOT NULL CODEC(Delta, ZSTD(1)),
    end_time              DateTime64(3)           NOT NULL CODEC(Delta, ZSTD(1)),
    feature_usage_amount  Decimal(25, 15)        NOT NULL,
    meter_usage_amount    Decimal(25, 15)        NOT NULL,
    diff                  Decimal(25, 15)        NOT NULL,
    currency              LowCardinality(String)  NOT NULL DEFAULT '',
    created_at            DateTime64(3)           NOT NULL DEFAULT now64(3)  CODEC(Delta, ZSTD(1))
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (tenant_id, environment_id, subscription_id, start_time)
SETTINGS
    index_granularity = 8192;
