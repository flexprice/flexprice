--
-- Invoice and billing sequence stored functions.
--
-- The invoice_sequences and billing_sequences tables are owned by the Ent ORM
-- layer (ent/schema/invoicesequence.go, ent/schema/billingsequence.go) and
-- created by `make migrate-ent`.  This file only creates the stored functions
-- that provide convenience wrappers around the raw-SQL upserts; they are safe
-- to CREATE OR REPLACE even before the tables exist (PL/pgSQL validates bodies
-- at call time, not at creation time).
--

-- ── Stored functions ──────────────────────────────────────────────────────────

-- Returns the next sequential invoice number for (tenant, environment, year_month).
-- Atomically upserts; the first call for a new month inserts the row.
CREATE OR REPLACE FUNCTION next_invoice_sequence(p_tenant_id VARCHAR, p_environment_id VARCHAR, p_year_month VARCHAR)
RETURNS BIGINT AS $$
DECLARE
    v_next_val BIGINT;
BEGIN
    INSERT INTO invoice_sequences (tenant_id, environment_id, year_month, last_value)
    VALUES (p_tenant_id, p_environment_id, p_year_month, 1)
    ON CONFLICT (tenant_id, environment_id, year_month)
    DO UPDATE SET
        last_value = invoice_sequences.last_value + 1,
        updated_at = CURRENT_TIMESTAMP
    RETURNING last_value INTO v_next_val;

    RETURN v_next_val;
END;
$$ LANGUAGE plpgsql;

-- Removes entries older than 1 year; safe to call periodically.
CREATE OR REPLACE FUNCTION cleanup_invoice_sequences()
RETURNS void AS $$
BEGIN
    DELETE FROM invoice_sequences
    WHERE year_month < to_char(current_date - interval '1 year', 'YYYYMM');
END;
$$ LANGUAGE plpgsql;

-- Returns the next billing period sequence number for (tenant, subscription).
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
        updated_at    = CURRENT_TIMESTAMP
    RETURNING last_sequence INTO v_next_val;

    RETURN v_next_val;
END;
$$ LANGUAGE plpgsql;
