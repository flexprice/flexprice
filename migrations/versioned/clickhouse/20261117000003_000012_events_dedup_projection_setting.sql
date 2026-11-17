-- migrate:up
-- Mirrors migrations/clickhouse/000012.
ALTER TABLE flexprice.events MODIFY SETTING deduplicate_merge_projection_mode = 'rebuild'

-- migrate:down
-- Setting change; no down.
