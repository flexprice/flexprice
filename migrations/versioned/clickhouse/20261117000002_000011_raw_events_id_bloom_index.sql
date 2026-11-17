-- migrate:up
-- Mirrors migrations/clickhouse/000011.
ALTER TABLE flexprice.raw_events
    ADD INDEX IF NOT EXISTS bf_id id TYPE bloom_filter(0.001) GRANULARITY 4

-- migrate:down
ALTER TABLE flexprice.raw_events DROP INDEX IF EXISTS bf_id
