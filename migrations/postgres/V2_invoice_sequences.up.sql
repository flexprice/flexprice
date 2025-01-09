-- Create invoice sequences table
CREATE TABLE invoice_sequences (
    tenant_id VARCHAR(50) NOT NULL,
    year_month VARCHAR(6) NOT NULL,
    last_value BIGINT DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, year_month)
);

-- Function to get next sequence atomically
CREATE OR REPLACE FUNCTION next_invoice_sequence(p_tenant_id VARCHAR, p_year_month VARCHAR)
RETURNS BIGINT AS $$
DECLARE
    v_next_val BIGINT;
BEGIN
    INSERT INTO invoice_sequences (tenant_id, year_month, last_value)
    VALUES (p_tenant_id, p_year_month, 1)
    ON CONFLICT (tenant_id, year_month)
    DO UPDATE SET 
        last_value = invoice_sequences.last_value + 1,
        updated_at = CURRENT_TIMESTAMP
    RETURNING last_value INTO v_next_val;
    
    RETURN v_next_val;
END;
$$ LANGUAGE plpgsql;

-- Function to cleanup old sequences (can be called periodically)
CREATE OR REPLACE FUNCTION cleanup_invoice_sequences()
RETURNS void AS $$
BEGIN
    DELETE FROM invoice_sequences
    WHERE year_month < to_char(current_date - interval '1 year', 'YYYYMM');
END;
$$ LANGUAGE plpgsql;

-- Create billing sequences table
CREATE TABLE billing_sequences (
    tenant_id VARCHAR(50) NOT NULL,
    subscription_id VARCHAR(50) NOT NULL,
    last_sequence INTEGER DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, subscription_id)
);

-- Function to get next billing sequence atomically
CREATE OR REPLACE FUNCTION next_billing_sequence(p_tenant_id VARCHAR, p_subscription_id VARCHAR)
RETURNS INTEGER AS $$
DECLARE
    v_next_val INTEGER;
BEGIN
    INSERT INTO billing_sequences (tenant_id, subscription_id, last_sequence)
    VALUES (p_tenant_id, p_subscription_id, 1)
    ON CONFLICT (tenant_id, subscription_id)
    DO UPDATE SET 
        last_sequence = billing_sequences.last_sequence + 1,
        updated_at = CURRENT_TIMESTAMP
    RETURNING last_sequence INTO v_next_val;
    
    RETURN v_next_val;
END;
$$ LANGUAGE plpgsql;
