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
        l.line_id AS line_id,
        toDecimal256(mu.qty_total, 15) * l.unit_price AS cost,
        if(JSONType(mu.properties, 'user') = 'String', JSONExtractString(mu.properties, 'user'), JSONExtractRaw(mu.properties, 'user')) AS k0
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
        count() AS g_events
    FROM priced
    GROUP BY k0
)
SELECT
    k0,
    toString(g_cost),
    toString(g_qty),
    g_events,
    count() OVER () AS total_count,
    toString(sum(g_cost) OVER ()),
    toString(sum(g_qty) OVER ()),
    sum(g_events) OVER ()
FROM grouped
ORDER BY g_cost DESC, k0 ASC
LIMIT @limit OFFSET @offset
SETTINGS max_memory_usage = 96636764160

-- arguments
-- @line_ids = [li_old li_new]
-- @line_meter_ids = [m_tokens m_tokens]
-- @line_feature_ids = [feat_m_tokens feat_m_tokens]
-- @line_customer_ids = [cust_a cust_a]
-- @line_starts = [1790812800 1792022400]
-- @line_ends = [1792022400 1793491200]
-- @line_unit_prices = [0.002 0.003]
-- @tenant_id = tenant_1
-- @environment_id = env_1
-- @customer_ids = [cust_a]
-- @meter_ids = [m_tokens]
-- @min_start = 2026-10-01T00:00:00Z
-- @max_end = 2026-11-01T00:00:00Z
-- @limit = 25
-- @offset = 0
