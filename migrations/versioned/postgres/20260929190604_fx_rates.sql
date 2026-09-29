-- migrate:up
-- Adaptive multi-currency (FLE-1383): tenant-configured FX rates.
-- Columns carry IF NOT EXISTS; index creation uses CONCURRENTLY by hand.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "fx_rates" (
  "id"             varchar(50)    NOT NULL,
  "tenant_id"      varchar(50)    NOT NULL,
  "environment_id" varchar(50)    NOT NULL DEFAULT '',
  "scope"          varchar(20)    NOT NULL,
  "scope_id"       varchar(50)    NOT NULL,
  "from_currency"  varchar(10)    NOT NULL,
  "to_currency"    varchar(10)    NOT NULL,
  "rate"           numeric(24,12) NOT NULL,
  "valid_from"     timestamptz    NULL,
  "valid_to"       timestamptz    NULL,
  "status"         varchar(20)    NOT NULL DEFAULT 'published',
  "metadata"       jsonb          NULL,
  "created_at"     timestamptz    NOT NULL DEFAULT now(),
  "updated_at"     timestamptz    NOT NULL DEFAULT now(),
  "created_by"     varchar(50)    NULL,
  "updated_by"     varchar(50)    NULL,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "idx_fx_rate_tenant_live"
  ON "fx_rates" ("tenant_id","environment_id","scope","scope_id","from_currency","to_currency")
  WHERE ((status)::text = 'published'::text) AND ((scope)::text = 'tenant'::text);

CREATE INDEX CONCURRENTLY IF NOT EXISTS "idx_fx_rate_override"
  ON "fx_rates" ("tenant_id","environment_id","scope","scope_id","from_currency","to_currency","valid_from");

-- migrate:down
DROP TABLE IF EXISTS "fx_rates";
