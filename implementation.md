#### Create a `GetUsageBySubscriptionV2` function which is super fast


##### Problem
The current GetUsageBySubscription function takes price, subscription does the calculation and returns. I makes multiple clickhouse queries for each meter/feature and fires them parallely using go routines which is nice but sometimes my clickhouse gets down.


##### GOAL
My goal is to optmize this function which makes a single clickhouse query and brings up the data by keeping the output format same.
Below is the query
```
    SELECT 
        feature_id,
        sum(qty_total)                     AS sum_total,
        max(qty_total)                     AS max_total,
        count(DISTINCT id)                 AS count_distinct_ids,
        count(DISTINCT unique_hash)        AS count_unique_qty,
        argMax(qty_total, "timestamp")     AS latest_qty
    FROM events_processed
    WHERE 
    subscription_id = 'subs_01K4SBWGY9C4Q0XSS8F7RR7MCS'
    AND external_customer_id = 'cust-customer-1'
    AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
    AND tenant_id = '00000000-0000-0000-0000-000000000000'
    AND "timestamp" >= toDateTime64('2025-09-10 08:29:10.100', 3)
    AND "timestamp" <  toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY feature_id;
```


##### Steps

1. Analyze the current flow
2. Implement the above query
3. Make sure to keep the response same