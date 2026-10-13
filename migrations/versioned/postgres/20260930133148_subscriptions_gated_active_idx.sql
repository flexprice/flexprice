-- migrate:up transaction:false
-- Backs the overdue-renewal sweep: it lists environments with active payment-gated
-- subscriptions and marks the overdue ones incomplete. Gating is opt-in, so the
-- partial index holds a small fraction of subscriptions.
--
-- statement_timeout must be 0 on the connection — a build killed by a timeout
-- leaves an INVALID index behind. Deliberately no IF NOT EXISTS, so a retry
-- after such a failure fails loudly rather than skipping the broken index.
-- Check for one first:
--   SELECT indexrelid::regclass FROM pg_index WHERE NOT indisvalid;
CREATE INDEX CONCURRENTLY "idx_subscriptions_gated_active" ON "subscriptions" ("tenant_id", "environment_id") WHERE (((status)::text = 'published'::text) AND ((subscription_status)::text = 'active'::text) AND ((payment_behavior)::text = ANY (ARRAY[('allow_incomplete'::character varying)::text, ('default_incomplete'::character varying)::text, ('error_if_incomplete'::character varying)::text])));

-- migrate:down transaction:false
DROP INDEX CONCURRENTLY IF EXISTS "idx_subscriptions_gated_active";
