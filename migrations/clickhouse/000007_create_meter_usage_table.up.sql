CREATE TABLE IF NOT EXISTS flexprice.meter_usage
(
    -- dedup identity
    id                    String                   NOT NULL  CODEC(ZSTD(1)),

    -- tenant scope
    tenant_id             LowCardinality(String)   NOT NULL,
    environment_id        LowCardinality(String)   NOT NULL,

    -- query dimensions
    external_customer_id  LowCardinality(String)   NOT NULL,
    meter_id              LowCardinality(String)   NOT NULL,
    event_name            LowCardinality(String)   NOT NULL,

    -- time
    timestamp             DateTime                 NOT NULL  CODEC(DoubleDelta, ZSTD(1)),
    ingested_at           DateTime64(3)            NOT NULL  DEFAULT now64(3)
                                                             CODEC(Delta, ZSTD(1)),

    -- metric
    qty_total             Decimal(18, 8)           NOT NULL  CODEC(ZSTD(1)),

    -- COUNT_UNIQUE support
    unique_hash           String                   NOT NULL  DEFAULT ''
                                                             CODEC(ZSTD(1)),

    -- analytics dimensions (not on hot read path)
    source                LowCardinality(String)   NOT NULL  DEFAULT '',
    properties            String                   NOT NULL  DEFAULT ''
                                                             CODEC(ZSTD(3))
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, meter_id, timestamp)
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)
SETTINGS
    index_granularity                        = 8192,
    parts_to_delay_insert                    = 150,
    parts_to_throw_insert                    = 300,
    max_bytes_to_merge_at_max_space_in_pool  = 2147483648,
    min_bytes_for_wide_part                  = 10485760,
    enable_mixed_granularity_parts           = 1;