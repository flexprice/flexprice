-- migrate:up transaction:false
-- Lists environments with customer setting overrides for the auto-cancellation cron.
-- statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS idx_customers_settings_override;
CREATE INDEX CONCURRENTLY idx_customers_settings_override
  ON customers (tenant_id, environment_id)
  WHERE ((settings IS NOT NULL) AND ((status)::text = 'published'::text));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS idx_customers_settings_override;
