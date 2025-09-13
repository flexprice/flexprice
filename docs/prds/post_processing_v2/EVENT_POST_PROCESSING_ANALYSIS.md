#### BASE QUERY
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


---

### WHAT TO FOCUS ON NEXT

1. Analyze the `/analytics` api, with the new flow how will it work ?
2. Change event post processing pipeline to allow the Other Aggregation Types and Pricing model ?


In plain english......
"We want to change the /analytics api to support multiple aggregation types and change `GetUsageBySubscriptionV2` to work on new types";
