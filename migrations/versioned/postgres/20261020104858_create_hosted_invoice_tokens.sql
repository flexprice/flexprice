-- migrate:up
-- Hosted invoice tokens: one row per public token issued for an invoice.
-- Brand-new table, so its index builds instantly inside this transactional migration;
-- CONCURRENTLY is unnecessary. Timeouts come from the connection (scripts/migrations/apply.sh).
-- Base-mixin columns match ent/migrate/schema.go exactly (BaseMixin + EnvironmentMixin).
CREATE TABLE IF NOT EXISTS "hosted_invoice_tokens" (
  "id"             varchar(50) NOT NULL,
  "tenant_id"      varchar(50) NOT NULL,
  "status"         varchar(20) NOT NULL DEFAULT 'published',
  "created_at"     timestamptz NOT NULL,
  "updated_at"     timestamptz NOT NULL,
  "created_by"     varchar     NULL,
  "updated_by"     varchar     NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "invoice_id"     varchar(50) NOT NULL,
  "expires_at"     timestamptz NOT NULL,
  "revoked_at"     timestamptz NULL,
  PRIMARY KEY ("id")
);

-- At most one live (unrevoked) token per invoice.
CREATE UNIQUE INDEX IF NOT EXISTS "idx_hosted_invoice_token_invoice_live"
  ON "hosted_invoice_tokens" ("tenant_id","environment_id","invoice_id")
  WHERE revoked_at IS NULL;

-- migrate:down
DROP TABLE IF EXISTS "hosted_invoice_tokens";
