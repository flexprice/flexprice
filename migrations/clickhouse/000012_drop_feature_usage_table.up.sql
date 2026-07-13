-- Drop feature_usage table after successful migration to meter_usage.
-- The feature_usage tracking service, ClickHouse repo, and all readers were
-- removed; nothing writes to or reads from this table anymore.
DROP TABLE IF EXISTS flexprice.feature_usage;
