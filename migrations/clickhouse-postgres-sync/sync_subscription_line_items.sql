-- Sync subscription_line_items from PostgreSQL to ClickHouse
-- LARGE table (~120M+ rows) — sync.sh batches this by monthly updated_at windows
-- Parameters: {after:String} inclusive lower bound, {before:String} exclusive upper bound

INSERT INTO flexprice.subscription_line_items (
    id, tenant_id, environment_id, status, subscription_id, customer_id,
    entity_id, entity_type, plan_display_name, price_id, price_type,
    meter_id, meter_display_name, price_unit_id, price_unit,
    display_name, quantity, currency,
    billing_period, billing_period_count, invoice_cadence, trial_period_days,
    start_date, end_date, subscription_phase_id,
    metadata, commitment_amount, commitment_quantity,
    commitment_type, commitment_overage_factor,
    commitment_true_up_enabled, commitment_windowed, commitment_duration,
    created_at, updated_at
)
SELECT
    id, tenant_id, environment_id, status, subscription_id, customer_id,
    entity_id, entity_type, plan_display_name, price_id, price_type,
    meter_id, meter_display_name, price_unit_id, price_unit,
    display_name, quantity, currency,
    billing_period, billing_period_count, invoice_cadence, trial_period_days,
    start_date, end_date, subscription_phase_id,
    CAST(metadata AS Nullable(String)), commitment_amount, commitment_quantity,
    commitment_type, commitment_overage_factor,
    commitment_true_up_enabled, commitment_windowed, commitment_duration,
    created_at, updated_at
FROM postgresql(
    {pg_host:String} || ':' || {pg_port:String},
    {pg_db:String},
    'subscription_line_items',
    {pg_user:String},
    {pg_pass:String}
)
WHERE updated_at >= toDateTime64({after:String}, 3)
  AND updated_at <  toDateTime64({before:String}, 3);
