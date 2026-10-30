-- migrate:up
SET lock_timeout = '3s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "customer_deletion_requests" ("id" character varying(50) NOT NULL, "tenant_id" character varying(50) NOT NULL, "status" character varying(20) NOT NULL DEFAULT 'published', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" character varying NULL, "updated_by" character varying NULL, "environment_id" character varying(50) NULL DEFAULT '', "customer_id" character varying(50) NOT NULL, "external_id" character varying(255) NOT NULL, "state" character varying(30) NOT NULL, "requested_at" timestamptz NOT NULL, "suppressed_at" timestamptz NULL, "redacted_at" timestamptz NULL, "cooldown_until" timestamptz NULL, "irreversible_started_at" timestamptz NULL, "requested_via" character varying(20) NOT NULL, "retention_policy" jsonb NULL, "failure_reason" text NULL, PRIMARY KEY ("id"));

CREATE TABLE IF NOT EXISTS "customer_deletion_categories" ("id" character varying(50) NOT NULL, "tenant_id" character varying(50) NOT NULL, "status" character varying(20) NOT NULL DEFAULT 'published', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "created_by" character varying NULL, "updated_by" character varying NULL, "environment_id" character varying(50) NULL DEFAULT '', "request_id" character varying(50) NOT NULL, "category" character varying(60) NOT NULL, "disposition" character varying(30) NOT NULL, "legal_basis" text NULL, "verified_at" timestamptz NULL, "verification_result" jsonb NULL, PRIMARY KEY ("id"));

CREATE UNIQUE INDEX IF NOT EXISTS "customerdeletionrequest_tenant_id_environment_id_customer_id" ON "customer_deletion_requests" ("tenant_id", "environment_id", "customer_id");

-- No partial predicate: retirement is permanent
CREATE UNIQUE INDEX IF NOT EXISTS "customerdeletionrequest_tenant_id_environment_id_external_id" ON "customer_deletion_requests" ("tenant_id", "environment_id", "external_id");

CREATE INDEX IF NOT EXISTS "customerdeletionrequest_tenant_id_environment_id_state" ON "customer_deletion_requests" ("tenant_id", "environment_id", "state");

CREATE UNIQUE INDEX IF NOT EXISTS "customerdeletioncategory_request_id_category" ON "customer_deletion_categories" ("request_id", "category");

CREATE INDEX IF NOT EXISTS "customerdeletioncategory_tenant_id_environment_id_disposition" ON "customer_deletion_categories" ("tenant_id", "environment_id", "disposition");

-- migrate:down
DROP TABLE IF EXISTS "customer_deletion_categories";
DROP TABLE IF EXISTS "customer_deletion_requests";
