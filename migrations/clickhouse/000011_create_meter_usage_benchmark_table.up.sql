CREATE TABLE IF NOT EXISTS flexprice.meter_usage_benchmark
(
    tenant_id              LowCardinality(String) NOT NULL,
    environment_id         LowCardinality(String) NOT NULL,
    event_id               String                 NOT NULL CODEC(ZSTD(1)),
    start_time             DateTime64(3)          NOT NULL CODEC(Delta, ZSTD(1)),
    end_time               DateTime64(3)          NOT NULL CODEC(Delta, ZSTD(1)),

    -- request fields retained so we can slice perf by tenant / customer / input shape.
    external_customer_id   String                 NOT NULL DEFAULT '',
    external_customer_ids  Array(String)          NOT NULL DEFAULT [],
    feature_ids            Array(String)          NOT NULL DEFAULT [],
    sources                Array(String)          NOT NULL DEFAULT [],
    group_by               Array(String)          NOT NULL DEFAULT [],
    window_size            LowCardinality(String) NOT NULL DEFAULT '',
    expand                 Array(String)          NOT NULL DEFAULT [],
    include_children       UInt8                  NOT NULL DEFAULT 0,
    has_property_filters   UInt8                  NOT NULL DEFAULT 0,
    request_json           String                 NOT NULL DEFAULT '' CODEC(ZSTD(3)),

    -- no-FINAL perf (wall-clock + server-side counters across main + every points sub-query)
    nofinal_duration_ms      Float64 NOT NULL DEFAULT 0,
    nofinal_scan_rows        UInt64  NOT NULL DEFAULT 0,
    nofinal_scan_bytes       UInt64  NOT NULL DEFAULT 0,
    nofinal_read_disk_bytes  UInt64  NOT NULL DEFAULT 0,
    nofinal_mem_peak_bytes   UInt64  NOT NULL DEFAULT 0,
    nofinal_result_rows      UInt64  NOT NULL DEFAULT 0,

    -- FINAL perf (same shape)
    final_duration_ms        Float64 NOT NULL DEFAULT 0,
    final_scan_rows          UInt64  NOT NULL DEFAULT 0,
    final_scan_bytes         UInt64  NOT NULL DEFAULT 0,
    final_read_disk_bytes    UInt64  NOT NULL DEFAULT 0,
    final_mem_peak_bytes     UInt64  NOT NULL DEFAULT 0,
    final_result_rows        UInt64  NOT NULL DEFAULT 0,

    -- pre-computed signed diffs (final - nofinal); FINAL can be lighter on small ranges.
    duration_diff_ms         Float64 NOT NULL DEFAULT 0,
    scan_rows_diff           Int64   NOT NULL DEFAULT 0,
    scan_bytes_diff          Int64   NOT NULL DEFAULT 0,
    read_disk_bytes_diff     Int64   NOT NULL DEFAULT 0,
    mem_peak_diff_bytes      Int64   NOT NULL DEFAULT 0,

    -- result diff (did FINAL change the answer?)
    nofinal_total_usage      Decimal(25, 15) NOT NULL DEFAULT 0,
    final_total_usage        Decimal(25, 15) NOT NULL DEFAULT 0,
    usage_diff               Decimal(25, 15) NOT NULL DEFAULT 0,
    nofinal_total_cost       Decimal(25, 15) NOT NULL DEFAULT 0,
    final_total_cost         Decimal(25, 15) NOT NULL DEFAULT 0,
    cost_diff                Decimal(25, 15) NOT NULL DEFAULT 0,
    nofinal_item_count       UInt64          NOT NULL DEFAULT 0,
    final_item_count         UInt64          NOT NULL DEFAULT 0,
    results_match            UInt8           NOT NULL DEFAULT 0,

    -- per-side failure surface
    nofinal_error            String          NOT NULL DEFAULT '',
    final_error              String          NOT NULL DEFAULT '',

    first_side               LowCardinality(String) NOT NULL DEFAULT '',

    currency                 LowCardinality(String) NOT NULL DEFAULT '',
    created_at               DateTime64(3) NOT NULL DEFAULT now64(3) CODEC(Delta, ZSTD(1))
)
ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(created_at)
ORDER BY (tenant_id, environment_id, created_at, event_id)
SETTINGS index_granularity = 8192;
