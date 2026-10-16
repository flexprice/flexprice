-- migrate:up
-- FLE-1662: users may belong to multiple tenants via tenant_memberships.
-- Roles for a human user in a tenant live on the membership row. users.roles
-- remains the home-tenant copy for backward compatibility and service accounts.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "tenant_memberships" (
  "id"         character varying(50) NOT NULL,
  "tenant_id"  character varying(50) NOT NULL,
  "status"     character varying(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" character varying NULL,
  "updated_by" character varying NULL,
  "user_id"    character varying(50) NOT NULL,
  "roles"      jsonb NULL,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_tenant_membership_user_tenant"
  ON "tenant_memberships" ("user_id", "tenant_id")
  WHERE ((status)::text = 'published'::text);

CREATE INDEX IF NOT EXISTS "idx_tenant_membership_tenant_status"
  ON "tenant_memberships" ("tenant_id", "status");

CREATE INDEX IF NOT EXISTS "idx_tenant_membership_user_status"
  ON "tenant_memberships" ("user_id", "status");

-- Backfill one membership per published human user from the existing 1:1 model.
INSERT INTO "tenant_memberships" (
  "id", "tenant_id", "status", "created_at", "updated_at",
  "created_by", "updated_by", "user_id", "roles"
)
SELECT
  'tm_' || replace(gen_random_uuid()::text, '-', ''),
  u."tenant_id",
  u."status",
  u."created_at",
  u."updated_at",
  u."created_by",
  u."updated_by",
  u."id",
  COALESCE(u."roles", '[]'::jsonb)
FROM "users" u
WHERE u."status" = 'published'
  AND u."type" = 'user'
  AND NOT EXISTS (
    SELECT 1 FROM "tenant_memberships" m
    WHERE m."user_id" = u."id"
      AND m."tenant_id" = u."tenant_id"
      AND m."status" = 'published'
  );

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DROP TABLE IF EXISTS "tenant_memberships";
