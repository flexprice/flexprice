-- ClickHouse schema for subscriptions (synced from PostgreSQL)
-- Primary join: feature_usage.subscription_id -> subscriptions.id
-- Note: PG column `version` (business field) kept as-is; RMT version column is `_version`

CREATE TABLE IF NOT EXISTS flexprice.subscriptions (
    id                        String NOT NULL,
    tenant_id                 String NOT NULL,
    environment_id            String NOT NULL,
    status                    LowCardinality(String) NOT NULL,
    lookup_key                Nullable(String),
    customer_id               String NOT NULL,
    plan_id                   String NOT NULL,
    subscription_status       LowCardinality(String) NOT NULL,
    currency                  LowCardinality(String) NOT NULL,
    billing_anchor            DateTime64(3) NOT NULL,
    start_date                DateTime64(3) NOT NULL,
    end_date                  Nullable(DateTime64(3)),
    current_period_start      DateTime64(3) NOT NULL,
    current_period_end        DateTime64(3) NOT NULL,
    cancelled_at              Nullable(DateTime64(3)),
    cancel_at                 Nullable(DateTime64(3)),
    cancel_at_period_end      Bool NOT NULL DEFAULT false,
    trial_start               Nullable(DateTime64(3)),
    trial_end                 Nullable(DateTime64(3)),
    billing_cadence           LowCardinality(String) NOT NULL,
    billing_period            LowCardinality(String) NOT NULL,
    billing_period_count      Int64 NOT NULL DEFAULT 1,
    version                   Int64 NOT NULL DEFAULT 1,
    metadata                  Nullable(String) CODEC(ZSTD),
    pause_status              LowCardinality(String) NOT NULL,
    active_pause_id           Nullable(String),
    billing_cycle             LowCardinality(String) NOT NULL,
    commitment_amount         Nullable(Decimal(20,6)),
    overage_factor            Nullable(Decimal(10,6)),
    payment_behavior          LowCardinality(String) NOT NULL,
    collection_method         LowCardinality(String) NOT NULL,
    gateway_payment_method_id Nullable(String),
    customer_timezone         String NOT NULL DEFAULT 'UTC',
    proration_behavior        LowCardinality(String) NOT NULL,
    enable_true_up            Bool NOT NULL DEFAULT false,
    invoicing_customer_id     Nullable(String),
    commitment_duration       Nullable(String),
    parent_subscription_id    Nullable(String),
    payment_terms             Nullable(String),
    created_at                DateTime64(3) NOT NULL,
    updated_at                DateTime64(3) NOT NULL,
    _version                  UInt64 NOT NULL DEFAULT toUnixTimestamp64Milli(now64())
)
ENGINE = ReplacingMergeTree(_version)
ORDER BY (tenant_id, environment_id, customer_id, id)
SETTINGS index_granularity = 8192;

ALTER TABLE flexprice.subscriptions
    ADD INDEX IF NOT EXISTS bf_plan_id plan_id TYPE bloom_filter(0.01) GRANULARITY 64,
    ADD INDEX IF NOT EXISTS bf_sub_status subscription_status TYPE set(0) GRANULARITY 64;
