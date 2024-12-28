CREATE TABLE IF NOT EXISTS prices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    -- Amount stored in main currency units (e.g., dollars, not cents)
    -- For USD: 12.50 means $12.50, not cents
    amount DECIMAL(25,15) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    display_amount VARCHAR(255) NOT NULL,
    plan_id VARCHAR(255) NOT NULL,
    type VARCHAR(20) NOT NULL,
    billing_period VARCHAR(20) NOT NULL,
    billing_period_count INTEGER NOT NULL,
    billing_model VARCHAR(20) NOT NULL,
    billing_cadence VARCHAR(20) NOT NULL,
    meter_id VARCHAR(255),
    filter_values JSONB,
    tier_mode VARCHAR(20),
    tiers JSONB,
    transform_quantity JSONB,
    lookup_key VARCHAR(255) NOT NULL,
    description TEXT,
    metadata JSONB,
    tenant_id VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'published',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(255) NOT NULL,
    updated_by VARCHAR(255) NOT NULL
);

-- Add indexes
CREATE INDEX idx_prices_tenant_id ON prices(tenant_id);
CREATE INDEX idx_prices_lookup_key ON prices(lookup_key);
CREATE INDEX idx_prices_plan_id ON prices(plan_id);
