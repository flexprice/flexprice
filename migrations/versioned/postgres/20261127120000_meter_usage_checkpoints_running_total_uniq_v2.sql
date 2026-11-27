-- migrate:up transaction:false
-- Step 1 of 3. The (customer, meter, aggregated_until) unique index covers period rows too, so a customer whose
-- subscriptions have different period starts gets two period rows with the same aggregated_until and the
-- checkpoint batch fails. Build it again for running totals only, alongside the old one.
-- statement_timeout must be 0; no IF NOT EXISTS, so a retry after a killed build fails loudly. Clear it with:
--   DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_running_total_v2;
CREATE UNIQUE INDEX CONCURRENTLY meterusagecheckpoint_running_total_v2
  ON meter_usage_checkpoints (tenant_id, environment_id, customer_id, meter_id, aggregated_until)
  WHERE (period_start IS NULL);

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS meterusagecheckpoint_running_total_v2;
