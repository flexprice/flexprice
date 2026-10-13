-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one statement per file.
CREATE TABLE IF NOT EXISTS flexprice.activity_logs
(
    -- Identity and tenancy
    id              String,
    tenant_id       String,
    environment_id  String,

    -- What changed
    entity_type     LowCardinality(String),
    entity_id       String,
    entity_label    String,
    customer_id     String,
    action          LowCardinality(String),

    -- Who changed it
    actor_type      LowCardinality(String),
    actor_id        String,

    -- Where the request came from
    source          LowCardinality(String),
    request_id      String,
    ip              String,
    user_agent      String,

    -- State after the change (snapshot JSON; empty when not stored)
    state           String CODEC(ZSTD(3)),
    schema_version  UInt16,

    -- Time
    occurred_at     DateTime64(3, 'UTC'),
    ingested_at     DateTime64(3, 'UTC') DEFAULT now64(3),

    -- Customer tab and entity timeline: skip granules without that customer or entity.
    INDEX idx_customer_id customer_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_entity_id entity_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_actor_id actor_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_request_id request_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, environment_id, occurred_at, id)
SETTINGS index_granularity = 8192

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
