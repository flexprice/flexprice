-- migrate:up transaction:false
-- Partial index for the hourly stale export sweep.
CREATE INDEX CONCURRENTLY IF NOT EXISTS "idx_tasks_unfinished_exports" ON "tasks" ("created_at") WHERE (task_type = 'EXPORT' AND task_status IN ('PENDING','PROCESSING') AND status = 'published');

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_tasks_unfinished_exports";
