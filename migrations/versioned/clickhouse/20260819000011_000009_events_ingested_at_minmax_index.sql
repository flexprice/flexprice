-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one statement per file.
-- Mirrors migrations/clickhouse/000009: lets self-billing's hourly count (filtered only on ingested_at) skip old granules.
-- Then run once, off-peak, per deployment: ALTER TABLE flexprice.events MATERIALIZE INDEX mm_ingested_at
ALTER TABLE flexprice.events
    ADD INDEX IF NOT EXISTS mm_ingested_at ingested_at TYPE minmax GRANULARITY 1

-- migrate:down
ALTER TABLE flexprice.events DROP INDEX IF EXISTS mm_ingested_at
