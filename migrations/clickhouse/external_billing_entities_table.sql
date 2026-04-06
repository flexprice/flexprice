CREATE TABLE default.billing_entries
(
    `id` UUID,
    `batch_uuid` Nullable(UUID),
    `key` Nullable(String),
    `org_id` String,
    `target_item_type` LowCardinality(Nullable(String)),
    `target_item_id` Nullable(String),
    `reference_type` LowCardinality(Nullable(String)),
    `reference_id` Nullable(String),
    `provider_name` LowCardinality(Nullable(String)),
    `method_name` LowCardinality(Nullable(String)),
    `created_at` DateTime64(3),
    `updated_at` Nullable(DateTime64(3)),
    `started_at` Nullable(DateTime64(3)),
    `ended_at` Nullable(DateTime64(3)),
    `data_interface` LowCardinality(Nullable(String)),
    `byok` Bool DEFAULT false,
    `is_subscribed` Bool DEFAULT false,
    `data` JSON,
    `reference_cost` Nullable(Float64),
    `duration_ms` Nullable(UInt32) MATERIALIZED data.durationMS,
    `num_characters` Nullable(UInt32) MATERIALIZED data.numCharacters,
    `num_channels` Nullable(UInt8) MATERIALIZED data.numChannels,
    `voice_id` Nullable(String) MATERIALIZED data.voiceId,
    `model_name` LowCardinality(Nullable(String)) MATERIALIZED data.modelName
)
ENGINE = SharedMergeTree('/clickhouse/tables/{uuid}/{shard}', '{replica}')
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, created_at, id)
SETTINGS index_granularity = 8192