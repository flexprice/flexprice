--
-- Default seed data for local development.
--
-- This file runs twice:
--   1. Automatically at container first-init via /docker-entrypoint-initdb.d/
--      (tables may not exist yet — DO blocks below guard against that)
--   2. Explicitly via `make seed-db`, which runs after `make migrate-ent`
--      (tables exist at that point, so the inserts take effect)
--
-- All statements are idempotent: ON CONFLICT DO NOTHING.
--

DO $$
BEGIN
  -- ── Default Tenant ─────────────────────────────────────────────────────────
  IF EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = 'tenants'
  ) THEN
    INSERT INTO public.tenants (id, name, created_at, updated_at)
    VALUES (
      '00000000-0000-0000-0000-000000000000',
      'Default Tenant',
      CURRENT_TIMESTAMP,
      CURRENT_TIMESTAMP
    ) ON CONFLICT (id) DO NOTHING;
  END IF;

  -- ── Default User ───────────────────────────────────────────────────────────
  IF EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = 'users'
  ) THEN
    INSERT INTO public.users (
      id, email, tenant_id,
      created_at, updated_at, created_by, updated_by
    ) VALUES (
      '00000000-0000-0000-0000-000000000000',
      'admin@flexprice.dev',
      '00000000-0000-0000-0000-000000000000',
      CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
      '00000000-0000-0000-0000-000000000000',
      '00000000-0000-0000-0000-000000000000'
    ) ON CONFLICT (id) DO NOTHING;
  END IF;

  -- ── Default Environments ───────────────────────────────────────────────────
  IF EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = 'environments'
  ) THEN
    INSERT INTO public.environments (
      id, name, type, tenant_id, status,
      created_by, updated_by, created_at, updated_at
    ) VALUES
    (
      '00000000-0000-0000-0000-000000000000',
      'Sandbox', 'development',
      '00000000-0000-0000-0000-000000000000',
      'published',
      '00000000-0000-0000-0000-000000000000',
      '00000000-0000-0000-0000-000000000000',
      CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    ) ON CONFLICT (id) DO NOTHING;

    INSERT INTO public.environments (
      id, name, type, tenant_id, status,
      created_by, updated_by, created_at, updated_at
    ) VALUES
    (
      '00000000-0000-0000-0000-000000000001',
      'Production', 'production',
      '00000000-0000-0000-0000-000000000000',
      'published',
      '00000000-0000-0000-0000-000000000000',
      '00000000-0000-0000-0000-000000000000',
      CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
    ) ON CONFLICT (id) DO NOTHING;
  END IF;
END $$;
