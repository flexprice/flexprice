-- Sync subscriptions from PostgreSQL to ClickHouse
-- Moderate table — single pass, filtered by updated_at >= {after:String}
-- Pass '1970-01-01 00:00:00' for full sync.

INSERT INTO flexprice.subscriptions (
    id, tenant_id, environment_id, status, lookup_key,
    customer_id, plan_id, subscription_status, currency,
    billing_anchor, start_date, end_date,
    current_period_start, current_period_end,
    cancelled_at, cancel_at, cancel_at_period_end,
    trial_start, trial_end,
    billing_cadence, billing_period, billing_period_count,
    version, metadata, pause_status, active_pause_id,
    billing_cycle, commitment_amount, overage_factor,
    payment_behavior, collection_method, gateway_payment_method_id,
    customer_timezone, proration_behavior, enable_true_up,
    invoicing_customer_id, commitment_duration, parent_subscription_id, payment_terms,
    created_at, updated_at
)
SELECT
    id, tenant_id, environment_id, status, lookup_key,
    customer_id, plan_id, subscription_status, currency,
    billing_anchor, start_date, end_date,
    current_period_start, current_period_end,
    cancelled_at, cancel_at, cancel_at_period_end,
    trial_start, trial_end,
    billing_cadence, billing_period, billing_period_count,
    version, CAST(metadata AS Nullable(String)), pause_status, active_pause_id,
    billing_cycle, commitment_amount, overage_factor,
    payment_behavior, collection_method, gateway_payment_method_id,
    customer_timezone, proration_behavior, enable_true_up,
    invoicing_customer_id, commitment_duration, parent_subscription_id, payment_terms,
    created_at, updated_at
FROM postgresql(
    {pg_host:String} || ':' || {pg_port:String},
    {pg_db:String},
    'subscriptions',
    {pg_user:String},
    {pg_pass:String}
)
WHERE updated_at >= toDateTime64({after:String}, 3);
