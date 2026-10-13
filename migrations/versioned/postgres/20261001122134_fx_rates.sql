-- migrate:up
-- Adaptive multi-currency (FLE-1383): tenant-configured FX rates.
-- This is a brand-new table, so its indexes build instantly inside this ordinary
-- transactional migration — CONCURRENTLY is unnecessary (it only matters for an index
-- on an already-populated table) and would require a single-statement transaction:false
-- file. Timeouts come from the connection (scripts/migrations/apply.sh), not the file.
-- Base-mixin columns match ent/migrate/schema.go exactly (BaseMixin + EnvironmentMixin):
-- created_*/updated_* have no DB default (Ent sets them in Go); created_by/updated_by are
-- unbounded varchar; environment_id is nullable with a '' default, like every other table.
CREATE TABLE IF NOT EXISTS "fx_rates" (
  "id"             varchar(50)    NOT NULL,
  "tenant_id"      varchar(50)    NOT NULL,
  "status"         varchar(20)    NOT NULL DEFAULT 'published',
  "created_at"     timestamptz    NOT NULL,
  "updated_at"     timestamptz    NOT NULL,
  "created_by"     varchar        NULL,
  "updated_by"     varchar        NULL,
  "environment_id" varchar(50)    NULL DEFAULT '',
  "scope"          varchar(20)    NOT NULL,
  "scope_id"       varchar(50)    NOT NULL,
  "from_currency"  varchar(10)    NOT NULL,
  "to_currency"    varchar(10)    NOT NULL,
  "rate"           numeric(24,12) NOT NULL,
  "source"         varchar(20)    NOT NULL DEFAULT 'fixed',
  "start_date"     timestamptz    NULL,
  "end_date"       timestamptz    NULL,
  "metadata"       jsonb          NULL,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_fx_rate_tenant_live"
  ON "fx_rates" ("tenant_id","environment_id","scope","scope_id","from_currency","to_currency")
  WHERE ((status)::text = 'published'::text) AND ((scope)::text = 'tenant'::text);

CREATE INDEX IF NOT EXISTS "idx_fx_rate_override"
  ON "fx_rates" ("tenant_id","environment_id","scope","scope_id","from_currency","to_currency","start_date");

-- migrate:down
DROP TABLE IF EXISTS "fx_rates";
