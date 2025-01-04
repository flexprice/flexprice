-- Add period fields to invoices table
ALTER TABLE invoices
ADD COLUMN period_start TIMESTAMP WITH TIME ZONE,
ADD COLUMN period_end TIMESTAMP WITH TIME ZONE;

-- Create index on period fields
CREATE INDEX idx_invoices_period ON invoices(period_start, period_end);

-- Create invoice line items table
CREATE TABLE invoice_line_items (
    id VARCHAR(255) PRIMARY KEY,
    tenant_id VARCHAR(255) NOT NULL,
    invoice_id VARCHAR(255) NOT NULL REFERENCES invoices(id),
    customer_id VARCHAR(255) NOT NULL,
    subscription_id VARCHAR(255),
    price_id VARCHAR(255) NOT NULL,
    meter_id VARCHAR(255),
    amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    quantity NUMERIC(20,8) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL,
    period_start TIMESTAMP WITH TIME ZONE,
    period_end TIMESTAMP WITH TIME ZONE,
    metadata JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    created_by VARCHAR(255) NOT NULL,
    updated_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for invoice line items
CREATE INDEX idx_invoice_line_items_tenant_invoice ON invoice_line_items(tenant_id, invoice_id, status);
CREATE INDEX idx_invoice_line_items_tenant_customer ON invoice_line_items(tenant_id, customer_id, status);
CREATE INDEX idx_invoice_line_items_tenant_subscription ON invoice_line_items(tenant_id, subscription_id, status);
CREATE INDEX idx_invoice_line_items_tenant_price ON invoice_line_items(tenant_id, price_id, status);
CREATE INDEX idx_invoice_line_items_tenant_meter ON invoice_line_items(tenant_id, meter_id, status);
CREATE INDEX idx_invoice_line_items_period ON invoice_line_items(period_start, period_end);
