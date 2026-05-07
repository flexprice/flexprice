-- ClickHouse schema for subscription_line_items (synced from PostgreSQL)
-- Primary join: feature_usage.sub_line_item_id -> subscription_line_items.id
-- This is the largest table (~120M+ rows), sync incrementally via --after flag

CREATE TABLE IF NOT EXISTS flexprice.subscription_line_items (
    id                        String NOT NULL,
    tenant_id                 String NOT NULL,
    environment_id            String NOT NULL,
    status                    LowCardinality(String) NOT NULL,
    subscription_id           String NOT NULL,
    customer_id               String NOT NULL,
    entity_id                 Nullable(String),
    entity_type               LowCardinality(String) NOT NULL,
    plan_display_name         Nullable(String),
    price_id                  String NOT NULL,
    price_type                Nullable(String),
    meter_id                  Nullable(String),
    meter_display_name        Nullable(String),
    price_unit_id             Nullable(String),
    price_unit                Nullable(String),
    display_name              Nullable(String),
    quantity                  Decimal(20,8) NOT NULL,
    currency                  LowCardinality(String) NOT NULL,
    billing_period            LowCardinality(String) NOT NULL,
    billing_period_count      Int64 NOT NULL DEFAULT 1,
    invoice_cadence           Nullable(String),
    trial_period_days         Int64 NOT NULL DEFAULT 0,
    start_date                Nullable(DateTime64(3)),
    end_date                  Nullable(DateTime64(3)),
    subscription_phase_id     Nullable(String),
    metadata                  Nullable(String) CODEC(ZSTD),
    commitment_amount         Nullable(Decimal(20,8)),
    commitment_quantity       Nullable(Decimal(20,8)),
    commitment_type           Nullable(String),
    commitment_overage_factor Nullable(Decimal(10,4)),
    commitment_true_up_enabled Bool NOT NULL DEFAULT false,
    commitment_windowed       Bool NOT NULL DEFAULT false,
    commitment_duration       Nullable(String),
    created_at                DateTime64(3) NOT NULL,
    updated_at                DateTime64(3) NOT NULL,
    version                   UInt64 NOT NULL DEFAULT toUnixTimestamp64Milli(now64())
)
ENGINE = ReplacingMergeTree(version)
ORDER BY (tenant_id, environment_id, subscription_id, id)
SETTINGS index_granularity = 8192;

ALTER TABLE flexprice.subscription_line_items
    ADD INDEX IF NOT EXISTS bf_price_id price_id TYPE bloom_filter(0.01) GRANULARITY 64,
    ADD INDEX IF NOT EXISTS bf_customer_id customer_id TYPE bloom_filter(0.01) GRANULARITY 64,
    ADD INDEX IF NOT EXISTS bf_meter_id meter_id TYPE bloom_filter(0.01) GRANULARITY 64;
