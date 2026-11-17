-- migrate:up
ALTER TABLE flexprice.raw_events ON CLUSTER '{cluster}'
    ADD INDEX IF NOT EXISTS bf_id id TYPE bloom_filter(0.001) GRANULARITY 4

-- migrate:down
ALTER TABLE flexprice.raw_events ON CLUSTER '{cluster}' DROP INDEX IF EXISTS bf_id
