-- migrate:up
-- A new, empty table: the unique index builds instantly, so it needs no concurrent build.
CREATE TABLE IF NOT EXISTS "meter_usage_checkpoints" ("id" character varying(50) NOT NULL, "tenant_id" character varying(50) NOT NULL, "status" character varying(20) NOT NULL DEFAULT 'published', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" character varying NULL, "updated_by" character varying NULL, "environment_id" character varying(50) NULL DEFAULT '', "customer_id" character varying(50) NOT NULL, "meter_id" character varying(50) NOT NULL, "aggregation_type" character varying(50) NOT NULL, "total" numeric(34,15) NOT NULL, "aggregated_until" timestamptz NOT NULL, "invoice_id" character varying(50) NULL, PRIMARY KEY ("id"));
CREATE UNIQUE INDEX IF NOT EXISTS "meterusagecheckpoint_customer_meter_aggregated_until" ON "meter_usage_checkpoints" ("tenant_id", "environment_id", "customer_id", "meter_id", "aggregated_until");

-- migrate:down
DROP TABLE IF EXISTS "meter_usage_checkpoints";
