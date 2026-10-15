-- migrate:up
-- The default autoanalyze trigger is 10% of the table changing. On a table with
-- hundreds of millions of rows that is tens of millions of changes, so statistics
-- stay stale for months and miss tenants that have grown since. The planner then
-- estimates ~1 row for such a tenant and picks an index that cannot use
-- subscription_id, scanning the whole tenant for one subscription's line items.
-- Refreshing every 50k changed rows keeps the statistics current; each ANALYZE
-- samples a fixed number of rows, so the cost does not grow with the table.
SET lock_timeout = '3s';

ALTER TABLE IF EXISTS "subscription_line_items" SET (
  autovacuum_analyze_scale_factor = 0,
  autovacuum_analyze_threshold = 50000
);

-- migrate:down
SET lock_timeout = '3s';

ALTER TABLE IF EXISTS "subscription_line_items" RESET (
  autovacuum_analyze_scale_factor,
  autovacuum_analyze_threshold
);
