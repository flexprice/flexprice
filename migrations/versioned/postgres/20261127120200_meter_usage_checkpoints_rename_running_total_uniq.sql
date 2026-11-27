-- migrate:up
-- Step 3 of 3. Restore the Ent-derived name. Metadata-only, but takes ACCESS EXCLUSIVE briefly.
SET lock_timeout = '3s';
ALTER INDEX meterusagecheckpoint_running_total_v2 RENAME TO meterusagecheckpoint_customer_meter_aggregated_until;

-- migrate:down
SET lock_timeout = '3s';
ALTER INDEX meterusagecheckpoint_customer_meter_aggregated_until RENAME TO meterusagecheckpoint_running_total_v2;
