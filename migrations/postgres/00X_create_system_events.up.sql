CREATE TABLE IF NOT EXISTS system_events (
    id UUID PRIMARY KEY,
    tenant_id VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_by VARCHAR(255) NOT NULL,
    updated_by VARCHAR(255) NOT NULL,
    workflow_id VARCHAR(255)
);

CREATE INDEX idx_system_events_workflow_id ON system_events(workflow_id);
CREATE INDEX idx_system_events_type ON system_events(type);
CREATE INDEX idx_system_events_status ON system_events(status);
CREATE INDEX idx_system_events_tenant_id ON system_events(tenant_id);