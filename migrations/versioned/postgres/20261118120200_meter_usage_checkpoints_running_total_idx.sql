-- migrate:up transaction:false
-- Step 3 of 4. Built beside the old index so uniqueness of running totals is never unenforced.
-- No IF NOT EXISTS: a retry after a killed build must fail loudly, not keep an INVALID index.
-- On "relation already exists": DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_running_total;
CREATE UNIQUE INDEX CONCURRENTLY meterusagecheckpoint_running_total
  ON meter_usage_checkpoints (tenant_id, environment_id, customer_id, meter_id, aggregated_until)
  WHERE (period_start IS NULL);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_running_total;
