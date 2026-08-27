-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one
-- statement per file. Split from 000002_create_raw_events.sql.
ALTER TABLE flexprice.raw_events
    ADD INDEX IF NOT EXISTS mm_ts timestamp TYPE minmax GRANULARITY 1

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
