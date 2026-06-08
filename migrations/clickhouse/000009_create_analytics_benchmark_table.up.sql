CREATE TABLE IF NOT EXISTS flexprice.analytics_benchmark
(
    tenant_id              LowCardinality(String)  NOT NULL,
    environment_id         LowCardinality(String)  NOT NULL,
    event_id               String                  NOT NULL CODEC(ZSTD(1)),
    start_time             DateTime64(3)           NOT NULL CODEC(Delta, ZSTD(1)),
    end_time               DateTime64(3)           NOT NULL CODEC(Delta, ZSTD(1)),

    -- parsed request fields (for SQL filtering)
    external_customer_id   String                  NOT NULL DEFAULT '',
    external_customer_ids  Array(String)           NOT NULL DEFAULT [],
    feature_ids            Array(String)           NOT NULL DEFAULT [],
    sources                Array(String)           NOT NULL DEFAULT [],
    group_by               Array(String)           NOT NULL DEFAULT [],
    window_size            LowCardinality(String)  NOT NULL DEFAULT '',
    expand                 Array(String)           NOT NULL DEFAULT [],
    include_children       UInt8                   NOT NULL DEFAULT 0,
    has_property_filters   UInt8                   NOT NULL DEFAULT 0,
    request_json           String                  NOT NULL DEFAULT ''  CODEC(ZSTD(3)),

    -- per-item row
    feature_id             String                  NOT NULL DEFAULT '',
    group_key              String                  NOT NULL DEFAULT '',
    match_status           LowCardinality(String)  NOT NULL,

    feature_total_usage    Decimal(25, 15)         NOT NULL DEFAULT 0,
    meter_total_usage      Decimal(25, 15)         NOT NULL DEFAULT 0,
    usage_diff             Decimal(25, 15)         NOT NULL DEFAULT 0,

    feature_total_cost     Decimal(25, 15)         NOT NULL DEFAULT 0,
    meter_total_cost       Decimal(25, 15)         NOT NULL DEFAULT 0,
    cost_diff              Decimal(25, 15)         NOT NULL DEFAULT 0,

    feature_event_count    UInt64                  NOT NULL DEFAULT 0,
    meter_event_count      UInt64                  NOT NULL DEFAULT 0,

    currency               LowCardinality(String)  NOT NULL DEFAULT '',
    created_at             DateTime64(3)           NOT NULL DEFAULT now64(3)  CODEC(Delta, ZSTD(1))
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (tenant_id, environment_id, event_id, feature_id, group_key)
SETTINGS index_granularity = 8192;
