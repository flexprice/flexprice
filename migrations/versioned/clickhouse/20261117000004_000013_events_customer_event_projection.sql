-- migrate:up
-- Mirrors migrations/clickhouse/000013.
ALTER TABLE flexprice.events
    ADD PROJECTION IF NOT EXISTS proj_by_customer_event
    (SELECT * ORDER BY tenant_id, environment_id, external_customer_id, event_name, timestamp, id)

-- migrate:down
ALTER TABLE flexprice.events DROP PROJECTION IF EXISTS proj_by_customer_event
