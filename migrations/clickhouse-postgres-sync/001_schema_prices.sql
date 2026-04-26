-- ClickHouse schema for prices (synced from PostgreSQL)
-- Primary join: feature_usage.price_id -> prices.id

CREATE TABLE IF NOT EXISTS flexprice.prices (
    id                        String NOT NULL,
    tenant_id                 String NOT NULL,
    environment_id            String NOT NULL,
    status                    LowCardinality(String) NOT NULL,
    display_name              Nullable(String),
    amount                    Decimal(25,15) NOT NULL,
    currency                  LowCardinality(String) NOT NULL,
    display_amount            Nullable(String),
    price_unit_type           LowCardinality(String) NOT NULL,
    price_unit                Nullable(String),
    price_unit_id             Nullable(String),
    price_unit_amount         Nullable(Decimal(25,15)),
    display_price_unit_amount Nullable(String),
    conversion_rate           Nullable(Decimal(25,15)),
    min_quantity              Nullable(Decimal(20,8)),
    type                      LowCardinality(String) NOT NULL,
    billing_period            LowCardinality(String) NOT NULL,
    billing_period_count      Int64 NOT NULL,
    billing_model             LowCardinality(String) NOT NULL,
    billing_cadence           LowCardinality(String) NOT NULL,
    invoice_cadence           Nullable(String),
    trial_period_days         Int64 NOT NULL DEFAULT 0,
    meter_id                  Nullable(String),
    filter_values             Nullable(String) CODEC(ZSTD),
    tier_mode                 Nullable(String),
    tiers                     Nullable(String) CODEC(ZSTD),
    price_unit_tiers          Nullable(String) CODEC(ZSTD),
    transform_quantity        Nullable(String) CODEC(ZSTD),
    lookup_key                Nullable(String),
    description               Nullable(String),
    metadata                  Nullable(String) CODEC(ZSTD),
    entity_type               LowCardinality(String) NOT NULL,
    entity_id                 String NOT NULL,
    parent_price_id           Nullable(String),
    start_date                Nullable(DateTime64(3)),
    end_date                  Nullable(DateTime64(3)),
    group_id                  Nullable(String),
    created_at                DateTime64(3) NOT NULL,
    updated_at                DateTime64(3) NOT NULL,
    version                   UInt64 NOT NULL DEFAULT toUnixTimestamp64Milli(now64())
)
ENGINE = ReplacingMergeTree(version)
ORDER BY (tenant_id, environment_id, id)
SETTINGS index_granularity = 8192;

ALTER TABLE flexprice.prices
    ADD INDEX IF NOT EXISTS bf_meter_id meter_id TYPE bloom_filter(0.01) GRANULARITY 64,
    ADD INDEX IF NOT EXISTS bf_entity_id entity_id TYPE bloom_filter(0.01) GRANULARITY 64;
