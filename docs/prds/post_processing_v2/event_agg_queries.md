# Difference between Queries on `events` and `events_processed`

---

## 1. SUM Query

**Events Table**

```sql
SELECT 
     sum(value) AS total
FROM (
    SELECT
         anyLast(JSONExtractFloat(assumeNotNull(properties), 'data')) AS value
    FROM events
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'                
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id 
);
```

**Events Processed Table**

```sql
SELECT 
    sum(value) AS total
FROM (
    SELECT
        anyLast(qty_billable) AS value
    FROM events_processed
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3)
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id
);
```

---

## 2. COUNT Query

**Events Table**

```sql
SELECT 
    count(DISTINCT id) AS total
FROM events
PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
    AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
    AND event_name = 'api_counts'
    AND external_customer_id = 'cust-customer-1'
    AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
    AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3);
```

**Events Processed Table**

```sql
SELECT 
    count(DISTINCT id) AS total
FROM events_processed
PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
     AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
     AND event_name = 'api_counts'
     AND external_customer_id = 'cust-customer-1'
     AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3)
     AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3);
```

---

## 3. COUNT UNIQUE Query

**Events Table**

```sql
SELECT 
    count(DISTINCT property_value) AS total
FROM (
    SELECT
        JSONExtractString(assumeNotNull(properties), 'data') AS property_value
    FROM events
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id, property_value 
);
```

**Events Processed Table**

```sql
SELECT 
    count(DISTINCT qty_billable) AS total
FROM events_processed
PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
     AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
     AND event_name = 'api_sum'
     AND external_customer_id = 'cust-customer-1'
     AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3)
     AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3);
```

---

## 4. SUM with Multiplier

**Events Table**

```sql
SELECT 
    (sum(value) * 1.5000) AS total
FROM (
    SELECT
        anyLast(JSONExtractFloat(assumeNotNull(properties), 'data')) AS value
    FROM events
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id 
);
```

**Events Processed Table**

```sql
SELECT 
    (sum(value) * 1.50000) AS total
FROM (
    SELECT 
        anyLast(qty_billable) AS value
    FROM events_processed
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3)
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id
);
```

---

## 5. Latest Value

**Events Table**

```sql
SELECT 
     argMax(JSONExtractFloat(assumeNotNull(properties), 'data'), timestamp) AS total
FROM events  
PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
    AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
    AND event_name = 'api_sum'
    AND external_customer_id = 'cust-customer-1'
    AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
    AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3);
```

**Events Processed Table**

```sql
SELECT 
    argMax(qty_billable, timestamp) AS total
FROM events_processed 
PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
    AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
    AND event_name = 'api_sum'
    AND external_customer_id = 'cust-customer-1'
    AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
    AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3);
```

---

## 6. MAX Query

**Events Table**

```sql
SELECT 
    max(value) AS total
FROM (
    SELECT
        anyLast(JSONExtractFloat(assumeNotNull(properties), 'data')) AS value
    FROM events
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id 
);
```

**Events Processed Table**

```sql
SELECT
    max(value) AS total
FROM (
    SELECT 
        anyLast(qty_billable) AS value 
    FROM events_processed
    PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
        AND environment_id = 'env_01K4SBHER7FQ1D8A4SR02BD3CG'
        AND event_name = 'api_sum'
        AND external_customer_id = 'cust-customer-1'
        AND timestamp >= toDateTime64('2025-09-10 08:29:10.100', 3) 
        AND timestamp < toDateTime64('2025-10-10 08:29:10.000', 3)
    GROUP BY id 
);
```