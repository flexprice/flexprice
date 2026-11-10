WITH
lines AS (
    SELECT
        l.1 AS line_id,
        l.2 AS meter_id,
        l.3 AS feature_id,
        l.4 AS customer_id,
        toDateTime(l.5, 'UTC') AS w_start,
        toDateTime(l.6, 'UTC') AS w_end,
        toDecimal256(l.7, 20) AS unit_price
    FROM (SELECT arrayJoin(arrayZip(@line_ids, @line_meter_ids, @line_feature_ids, @line_customer_ids, @line_starts, @line_ends, @line_unit_prices)) AS l)
),
priced AS (
    SELECT
        mu.qty_total AS qty,
        toDecimal256(mu.qty_total, 15) * l.unit_price AS cost,
        if(JSONType(mu.properties, 'user') = 'String', JSONExtractString(mu.properties, 'user'), JSONExtractRaw(mu.properties, 'user')) AS k0,
        formatDateTime(mu.timestamp, '%Y-%m', 'Asia/Kolkata') AS k1
    FROM meter_usage AS mu
    INNER JOIN lines AS l ON mu.meter_id = l.meter_id AND mu.external_customer_id = l.customer_id
    WHERE mu.tenant_id = @tenant_id
      AND mu.environment_id = @environment_id
      AND mu.external_customer_id IN @customer_ids
      AND mu.meter_id IN @meter_ids
      AND mu.timestamp >= @min_start
      AND mu.timestamp < @max_end
      AND mu.timestamp >= l.w_start
      AND mu.timestamp < l.w_end
),
grouped AS (
    SELECT
        k0,
        sum(cost) AS g_cost,
        sum(qty) AS g_qty,
        count() AS g_events,
        sumMap(map(k1, cost)) AS g_columns
    FROM priced
    GROUP BY k0
)
SELECT
    k0,
    toString(g_cost),
    toString(g_qty),
    g_events,
    mapApply((k, v) -> (k, toString(v)), g_columns),
    count() OVER () AS total_count,
    toString(sum(g_cost) OVER ()),
    toString(sum(g_qty) OVER ()),
    sum(g_events) OVER (),
    mapApply((k, v) -> (k, toString(v)), sumMap(g_columns) OVER ())
FROM grouped
ORDER BY g_qty DESC, k0 ASC
LIMIT @limit OFFSET @offset

-- arguments
-- @line_ids = [li_1]
-- @line_meter_ids = [m_tokens]
-- @line_feature_ids = [feat_m_tokens]
-- @line_customer_ids = [cust_a]
-- @line_starts = [1790812800]
-- @line_ends = [1793491200]
-- @line_unit_prices = [0.002]
-- @tenant_id = tenant_1
-- @environment_id = env_1
-- @customer_ids = [cust_a]
-- @meter_ids = [m_tokens]
-- @min_start = 2026-10-01T00:00:00Z
-- @max_end = 2026-11-01T00:00:00Z
-- @limit = 25
-- @offset = 0
