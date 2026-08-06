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
    -- row_type tiers the rows so debugging can drill from coarse to fine:
    --   'summary'   – 1 row per event_id (response.TotalCost from each pipeline)
    --   'feature'   – 1 row per (event_id, feature_id, group_key) aggregated
    --                 across line-item splits (robust to duplicate feature_ids)
    --   'line_item' – 1 row per (event_id, feature_id, sub_line_item_id, group_key)
    row_type               LowCardinality(String)  NOT NULL DEFAULT 'line_item',
    feature_id             String                  NOT NULL DEFAULT '',
    meter_id               String                  NOT NULL DEFAULT '',
    sub_line_item_id       String                  NOT NULL DEFAULT '',
    group_key              String                  NOT NULL DEFAULT '',
    match_status           LowCardinality(String)  NOT NULL,

    -- diff_reason classifies the cost_diff so spurious mismatches can be filtered:
    --   'none'                – cost_diff == 0
    --   'unmatched'           – one side produced no item for this key
    --   'multi_feature_meter' – feature is one of >1 features mapped to the same meter
    --                           (meter-side attribution is ambiguous by design)
    --   'multi_item'          – multiple items collapsed at this key on one side
    --                           (look at a finer row_type for the real story)
    --   'material'            – non-zero cost_diff with no above explanation
    diff_reason            LowCardinality(String)  NOT NULL DEFAULT 'none',

    -- price ids used by each pipeline (empty when that side did not match)
    feature_price_id       String                  NOT NULL DEFAULT '',
    meter_price_id         String                  NOT NULL DEFAULT '',

    -- how many response items contributed to each side of this row.
    -- summary rows: total items in the response. feature rows: items with
    -- this feature_id. line_item rows: 1 (or 0 when one side is missing).
    feature_item_count     UInt64                  NOT NULL DEFAULT 0,
    meter_item_count       UInt64                  NOT NULL DEFAULT 0,

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
PARTITION BY toYYYYMMDD(created_at)
ORDER BY (tenant_id, environment_id, event_id, row_type, feature_id, sub_line_item_id, group_key)
SETTINGS index_granularity = 8192;
