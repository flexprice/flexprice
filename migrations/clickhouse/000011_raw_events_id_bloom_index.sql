-- Point lookups by raw event id.
ALTER TABLE flexprice.raw_events
    ADD INDEX IF NOT EXISTS bf_id id TYPE bloom_filter(0.001) GRANULARITY 4;
