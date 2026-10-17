-- migrate:up
-- Lets self-billing's hourly count (filtered only on ingested_at) skip old granules;
-- events is partitioned by toYYYYMMDD(timestamp), so partition pruning cannot help that filter.
-- This versioned ClickHouse set is inert (see scripts/migrations/checksum-check.sh): deployments get the
-- index from the migrations/clickhouse/000009 mirror or by hand, then run once, off-peak, per deployment:
--   ALTER TABLE flexprice.events MATERIALIZE INDEX mm_ingested_at
ALTER TABLE flexprice.events
    ADD INDEX IF NOT EXISTS mm_ingested_at ingested_at TYPE minmax GRANULARITY 1

-- migrate:down
ALTER TABLE flexprice.events DROP INDEX IF EXISTS mm_ingested_at
