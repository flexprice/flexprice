-- migrate:up
-- How far each customer's per-period meter totals are aggregated: one row per customer and billing period.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "meter_usage_period_checkpoints" ("id" character varying(50) NOT NULL, "tenant_id" character varying(50) NOT NULL, "status" character varying(20) NOT NULL DEFAULT 'published', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" character varying NULL, "updated_by" character varying NULL, "environment_id" character varying(50) NULL DEFAULT '', "customer_id" character varying(50) NOT NULL, "period_start" timestamptz NOT NULL, "aggregated_until" timestamptz NOT NULL, PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "meterusageperiodcheckpoint_customer_period" ON "meter_usage_period_checkpoints" ("tenant_id", "environment_id", "customer_id", "period_start");

-- migrate:down
DROP TABLE IF EXISTS "meter_usage_period_checkpoints";
