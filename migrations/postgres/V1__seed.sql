-- Create default tenant
INSERT INTO public.tenants (
    id,
    name,
    created_at,
    updated_at
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'Default Tenant',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
) ON CONFLICT (id) DO NOTHING;

-- Create default auth record
INSERT INTO public.auths (
    user_id,
    provider,
    token,
    status,
    created_at,
    updated_at
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'flexprice',
    'sk_local_flexprice_test_key',
    'published',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
) ON CONFLICT DO NOTHING;

-- Create default user
INSERT INTO public.users (
    id,
    email,
    tenant_id,
    created_at,
    updated_at,
    created_by,
    updated_by
) VALUES (
    '00000000-0000-0000-0000-000000000000',
    'admin@flexprice.dev',
    '00000000-0000-0000-0000-000000000000',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP,
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000'
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

-- Create local test API key
INSERT INTO public.secrets (
    id,
    tenant_id,
    environment_id,
    name,
    type,
    provider,
    value,
    display_id,
    status,
    created_by,
    updated_by,
    created_at,
    updated_at
) VALUES (
    'sk_local_flexprice_test_key',
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000',
    'Local Test Key',
    'private_key',
    'flexprice',
    '0cfd568f22158887f8b77bc019fb245b1f14077b80936342b052985efa7de46c',
    'sk_local_****_test_key',
    'published',
    '00000000-0000-0000-0000-000000000000',
    '00000000-0000-0000-0000-000000000000',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
) ON CONFLICT (id) DO NOTHING;
