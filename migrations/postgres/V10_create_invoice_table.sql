CREATE TABLE IF NOT EXISTS invoices (
    id VARCHAR(255) PRIMARY KEY,
    customer_id VARCHAR(255) NOT NULL,
    subscription_id VARCHAR(255),
    invoice_type VARCHAR(50) NOT NULL,
    invoice_status VARCHAR(50) NOT NULL DEFAULT 'DRAFT',
    payment_status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    currency VARCHAR(10) NOT NULL,
    amount_due NUMERIC(20,8) NOT NULL DEFAULT 0,
    amount_paid NUMERIC(20,8) NOT NULL DEFAULT 0,
    amount_remaining NUMERIC(20,8) NOT NULL DEFAULT 0,
    description TEXT,
    due_date TIMESTAMP WITH TIME ZONE,
    paid_at TIMESTAMP WITH TIME ZONE,
    voided_at TIMESTAMP WITH TIME ZONE,
    finalized_at TIMESTAMP WITH TIME ZONE,
    invoice_pdf_url TEXT,
    billing_reason VARCHAR(50),
    metadata JSONB,
    version INTEGER NOT NULL DEFAULT 1,
    status VARCHAR(50) NOT NULL DEFAULT 'PUBLISHED',
    tenant_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(255) NOT NULL,
    updated_by VARCHAR(255) NOT NULL
);

CREATE INDEX idx_invoices_tenant_customer ON invoices(tenant_id, customer_id, invoice_status, payment_status, status);
CREATE INDEX idx_invoices_tenant_subscription ON invoices(tenant_id, subscription_id, invoice_status, payment_status, status);
CREATE INDEX idx_invoices_tenant_type ON invoices(tenant_id, invoice_type, invoice_status, payment_status, status);
CREATE INDEX idx_invoices_tenant_due_date ON invoices(tenant_id, due_date, invoice_status, payment_status, status);
