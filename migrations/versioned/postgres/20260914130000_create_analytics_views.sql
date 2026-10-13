-- migrate:up
CREATE TABLE IF NOT EXISTS "analytics_views" ("id" character varying(50) NOT NULL, "tenant_id" character varying(50) NOT NULL, "status" character varying(20) NOT NULL DEFAULT 'published', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" character varying NULL, "updated_by" character varying NULL, "environment_id" character varying(50) NOT NULL DEFAULT '', "name" character varying NOT NULL, "version" bigint NOT NULL DEFAULT 1, "definition" jsonb NOT NULL, PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "analyticsview_tenant_id_environment_id_status" ON "analytics_views" ("tenant_id", "environment_id", "status");

-- migrate:down
DROP TABLE "analytics_views";
