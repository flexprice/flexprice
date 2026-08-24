-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one
-- statement per file. Split from 000001_create_events_table.up.sql.
-- Set Index for source
ALTER TABLE flexprice.events
ADD INDEX IF NOT EXISTS source_idx source TYPE set(0) GRANULARITY 8192

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
