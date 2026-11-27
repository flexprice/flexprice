-- migrate:up transaction:false
-- Step 2 of 3. Drop the index that also covered period rows; meterusagecheckpoint_running_total_v2 covers
-- running totals. CONCURRENTLY: a plain DROP INDEX takes ACCESS EXCLUSIVE.
DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_customer_meter_aggregated_until;

-- migrate:down transaction:false
-- Recreating it brings back the period-row collision. Run by hand only:
--   CREATE UNIQUE INDEX CONCURRENTLY meterusagecheckpoint_customer_meter_aggregated_until
--     ON meter_usage_checkpoints (tenant_id, environment_id, customer_id, meter_id, aggregated_until);
