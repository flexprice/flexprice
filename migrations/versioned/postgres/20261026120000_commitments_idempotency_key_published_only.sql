-- migrate:up
-- Archived commitments release their idempotency key, so the unique index covers published rows only.
-- commitments is new and small, so the rebuild runs in the migration's transaction.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DROP INDEX IF EXISTS "commitment_tenant_id_environment_id_idempotency_key";
CREATE UNIQUE INDEX "commitment_tenant_id_environment_id_idempotency_key"
  ON "commitments" ("tenant_id", "environment_id", "idempotency_key")
  WHERE ((status)::text = 'published'::text);

-- migrate:down
-- Restoring the full-table index fails once an archived row shares a key with a published one.
DROP INDEX IF EXISTS "commitment_tenant_id_environment_id_idempotency_key";
CREATE UNIQUE INDEX "commitment_tenant_id_environment_id_idempotency_key"
  ON "commitments" ("tenant_id", "environment_id", "idempotency_key");
