-- migrate:up
CREATE TABLE IF NOT EXISTS revenue_facts (
    id                   TEXT NOT NULL,
    tenant_id            TEXT NOT NULL,
    environment_id       TEXT NOT NULL,
    customer_id          TEXT NOT NULL,
    subscription_id      TEXT NOT NULL,
    sub_line_item_id     TEXT,
    price_id             TEXT,
    meter_id             TEXT,
    aggregation_type     TEXT,
    revenue_source       TEXT NOT NULL,
    period_start         DATE NOT NULL,
    period_end           DATE NOT NULL,
    day                  DATE NOT NULL,
    service_start        DATE,
    service_end          DATE,
    recognition_method   TEXT,
    usage_at_list_rate   NUMERIC(38,9) NOT NULL,
    tier_delta           NUMERIC(38,9) NOT NULL,
    entitlement_amount   NUMERIC(38,9) NOT NULL,
    line_discount        NUMERIC(38,9) NOT NULL,
    invoice_discount     NUMERIC(38,9) NOT NULL,
    net_amount           NUMERIC(38,9) NOT NULL,
    billable_qty         NUMERIC(38,9) NOT NULL,
    entitlement_qty      NUMERIC(38,9) NOT NULL,
    decomposition_mode   TEXT NOT NULL,
    currency             TEXT NOT NULL,
    status               TEXT NOT NULL,
    is_revert            BOOLEAN NOT NULL DEFAULT false,
    invoice_id           TEXT,
    invoice_line_item_id TEXT,
    lock_adjusted_day    DATE,
    computed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    version              BIGINT NOT NULL DEFAULT 1,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS revenue_facts_provisional_grain ON revenue_facts
    (tenant_id, environment_id, subscription_id, price_id, sub_line_item_id, day, revenue_source)
    WHERE status = 'PROVISIONAL';
CREATE INDEX IF NOT EXISTS revenue_facts_read    ON revenue_facts (tenant_id, environment_id, day, revenue_source);
CREATE INDEX IF NOT EXISTS revenue_facts_invoice ON revenue_facts (tenant_id, environment_id, invoice_id);

-- migrate:down
DROP TABLE IF EXISTS revenue_facts;
