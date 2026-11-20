-- migrate:up transaction:false
-- Step 4 of 4. Drop the full index: it would make a per-period row collide with a running
-- total at the same instant. CONCURRENTLY: a plain DROP INDEX takes ACCESS EXCLUSIVE.
DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_customer_meter_aggregated_until;

-- migrate:down transaction:false
-- Fails while a per-period row shares an instant with a running total. Delete the per-period
-- rows first (they are rebuilt by the checkpoint job): DELETE ... WHERE period_start IS NOT NULL;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS meterusagecheckpoint_customer_meter_aggregated_until
  ON meter_usage_checkpoints (tenant_id, environment_id, customer_id, meter_id, aggregated_until);
