-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one
-- statement per file. Split from 000002_create_raw_events.sql.
ALTER TABLE flexprice.raw_events
    ADD INDEX IF NOT EXISTS bf_event_name event_name TYPE bloom_filter(0.01) GRANULARITY 64

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
