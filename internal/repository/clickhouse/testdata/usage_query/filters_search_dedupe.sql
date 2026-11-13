WITH
lines AS (
    SELECT
        l.1 AS line_id,
        l.2 AS meter_id,
        l.3 AS feature_id,
        l.4 AS customer_id,
        toDateTime(l.5, 'UTC') AS w_start,
        toDateTime(l.6, 'UTC') AS w_end,
        toDecimal256(l.7, 20) AS unit_price,
        toDecimal256(l.8, 20) AS list_unit_price
    FROM (SELECT arrayJoin(arrayZip(@line_ids, @line_meter_ids, @line_feature_ids, @line_customer_ids, @line_starts, @line_ends, @line_unit_prices, @line_list_unit_prices)) AS l)
),
priced AS (
    SELECT
        mu.qty_total AS qty,
        l.line_id AS line_id,
        toDecimal256(mu.qty_total, 15) * l.unit_price AS amount,
        toDecimal256(mu.qty_total, 15) * l.list_unit_price AS subtotal,
        if(JSONType(mu.properties, 'user') = 'String', JSONExtractString(mu.properties, 'user'), JSONExtractRaw(mu.properties, 'user')) AS k0,
        toString(mu.source) AS k1,
        if(JSONType(mu.properties, 'app') = 'String', JSONExtractString(mu.properties, 'app'), JSONExtractRaw(mu.properties, 'app')) AS k2
    FROM meter_usage AS mu FINAL
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
        k1,
        sum(amount) AS g_amount,
        sum(subtotal) AS g_subtotal,
        sum(qty) AS g_qty,
        count() AS g_events
    FROM priced
    WHERE has(@filter_0, k2)
      AND NOT has(@filter_1, k1)
      AND (positionCaseInsensitiveUTF8(k0, @search) > 0 OR positionCaseInsensitiveUTF8(k1, @search) > 0)
    GROUP BY k0, k1
)
SELECT
    k0,
    k1,
    toString(g_amount),
    toString(g_subtotal),
    toString(g_qty),
    g_events,
    count() OVER () AS total_count,
    toString(sum(g_amount) OVER ()),
    toString(sum(g_subtotal) OVER ()),
    toString(sum(g_qty) OVER ()),
    sum(g_events) OVER ()
FROM grouped
ORDER BY k0 ASC, k1 ASC
LIMIT @limit OFFSET @offset
SETTINGS max_memory_usage = 96636764160, do_not_merge_across_partitions_select_final = 1

-- arguments
-- @line_ids = [li_1]
-- @line_meter_ids = [m_tokens]
-- @line_feature_ids = [feat_m_tokens]
-- @line_customer_ids = [cust_a]
-- @line_starts = [1790812800]
-- @line_ends = [1793491200]
-- @line_unit_prices = [0.002]
-- @line_list_unit_prices = [0.002]
-- @tenant_id = tenant_1
-- @environment_id = env_1
-- @customer_ids = [cust_a]
-- @meter_ids = [m_tokens]
-- @min_start = 2026-10-01T00:00:00Z
-- @max_end = 2026-11-01T00:00:00Z
-- @filter_0 = [build run]
-- @filter_1 = [test]
-- @search = ali
-- @limit = 25
-- @offset = 50
