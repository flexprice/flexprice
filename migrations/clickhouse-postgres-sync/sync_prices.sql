-- Sync prices from PostgreSQL to ClickHouse
-- Small table — single pass, filtered by updated_at >= {after:String}
-- Pass '1970-01-01 00:00:00' for full sync.

INSERT INTO flexprice.prices (
    id, tenant_id, environment_id, status, display_name,
    amount, currency, display_amount, price_unit_type, price_unit,
    price_unit_id, price_unit_amount, display_price_unit_amount, conversion_rate, min_quantity,
    type, billing_period, billing_period_count, billing_model, billing_cadence,
    invoice_cadence, trial_period_days, meter_id,
    filter_values, tier_mode, tiers, price_unit_tiers, transform_quantity,
    lookup_key, description, metadata,
    entity_type, entity_id, parent_price_id, start_date, end_date, group_id,
    created_at, updated_at
)
SELECT
    id, tenant_id, environment_id, status, display_name,
    amount, currency, display_amount, price_unit_type, price_unit,
    price_unit_id, price_unit_amount, display_price_unit_amount, conversion_rate, min_quantity,
    type, billing_period, billing_period_count, billing_model, billing_cadence,
    invoice_cadence, trial_period_days, meter_id,
    CAST(filter_values AS Nullable(String)),
    tier_mode,
    CAST(tiers AS Nullable(String)),
    CAST(price_unit_tiers AS Nullable(String)),
    CAST(transform_quantity AS Nullable(String)),
    lookup_key, description,
    CAST(metadata AS Nullable(String)),
    entity_type, entity_id, parent_price_id, start_date, end_date, group_id,
    created_at, updated_at
FROM postgresql(
    {pg_host:String} || ':' || {pg_port:String},
    {pg_db:String},
    'prices',
    {pg_user:String},
    {pg_pass:String}
)
WHERE updated_at >= toDateTime64({after:String}, 3);
