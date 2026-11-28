-- migrate:up
-- Skips granules lacking the queried customer.
ALTER TABLE flexprice.meter_usage
    ADD INDEX IF NOT EXISTS bf_external_customer_id external_customer_id TYPE bloom_filter(0.01) GRANULARITY 1

-- migrate:down
ALTER TABLE flexprice.meter_usage DROP INDEX IF EXISTS bf_external_customer_id
