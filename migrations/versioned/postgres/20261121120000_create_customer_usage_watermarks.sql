-- migrate:up
-- How far each customer's usage has been added to their period totals: one row per customer.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "customer_usage_watermarks" ("id" character varying(50) NOT NULL, "tenant_id" character varying(50) NOT NULL, "status" character varying(20) NOT NULL DEFAULT 'published', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" character varying NULL, "updated_by" character varying NULL, "environment_id" character varying(50) NULL DEFAULT '', "customer_id" character varying(50) NOT NULL, "aggregated_until" timestamptz NOT NULL, PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "customerusagewatermark_customer" ON "customer_usage_watermarks" ("tenant_id", "environment_id", "customer_id");

-- migrate:down
DROP TABLE IF EXISTS "customer_usage_watermarks";
