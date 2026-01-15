-- Create default tenant
INSERT INTO public.tenants (
    id,
    name,
    status,
    created_at,
    updated_at,
    billing_details,
    metadata
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'Default Tenant',
    'published',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    '{}'::jsonb,
    NULL
) ON CONFLICT (id) DO NOTHING;

-- Create default user
INSERT INTO public.users (
    id,
    tenant_id,
    status,
    created_at,
    updated_at,
    created_by,
    updated_by,
    email,
    type,
    roles
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000',
    'published',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000',
    'admin@flexprice.dev',
    'user',
    '{}'::text[]
) ON CONFLICT (id) DO NOTHING;

-- Create two default ENVironments
INSERT INTO public.environments (
    id,
    name,
    type,
    tenant_id,
    status,
    created_by,
    updated_by,
    created_at,
    updated_at
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'Sandbox',
    'development',
    '00000000-0000-0000-0000-000000000000',
    'published',
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
) ON CONFLICT (id) DO NOTHING;

INSERT INTO public.environments (
    id,
    name,
    type,
    tenant_id,
    status,
    created_by,
    updated_by,
    created_at,
    updated_at
) VALUES (
    '00000000-0000-0000-0000-000000000001',
    'Production',
    'production',
    '00000000-0000-0000-0000-000000000000',
    'published',
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
) ON CONFLICT (id) DO NOTHING;
