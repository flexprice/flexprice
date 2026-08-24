-- migrate:up
-- ClickHouse rejects multi-statement request bodies, so exactly one
-- statement per file. Split from 000001_create_events_table.up.sql.
-- 5 GiB

-- Bloom Filter for external_customer_id
ALTER TABLE flexprice.events
ADD INDEX IF NOT EXISTS external_customer_id_idx external_customer_id TYPE bloom_filter GRANULARITY 8192

-- migrate:down
-- Baseline object; no down. Recreating is a data-loss operation.
