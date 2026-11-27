-- migrate:up transaction:false
-- Step 2 of 2. statement_timeout must be 0; a killed build leaves an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_period_total;
CREATE UNIQUE INDEX CONCURRENTLY meterusagecheckpoint_period_total
  ON meter_usage_checkpoints (tenant_id, environment_id, customer_id, meter_id, period_start)
  WHERE (period_start IS NOT NULL);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_period_total;
