# Meter Usage Detailed Analytics — Complete Flow

This document traces the full execution path of the `POST /v1/meter-usage/detailed-analytics` endpoint, from HTTP request through every function call and SQL query fired to ClickHouse.

---

## 1. Entry Point: HTTP Handler

**File**: `internal/api/v1/meter_usage.go` → `GetDetailedAnalytics()`

```
HTTP POST /v1/meter-usage/detailed-analytics
  → Parse JSON body into dto.MeterUsageDetailedAnalyticsRequest
  → req.Validate()
  → req.ToParams(tenantID, environmentID)  →  *events.MeterUsageDetailedAnalyticsParams
  → meterUsageService.GetDetailedAnalytics(ctx, params)
  → Return *dto.GetUsageAnalyticsResponse as JSON
```

---

## 2. Service Layer: `meterUsageService.GetDetailedAnalytics()`

**File**: `internal/service/meter_usage.go:62`

### 2.1 Set Defaults
- `EndTime` defaults to `time.Now().UTC()`
- `StartTime` defaults to `EndTime - 6 hours`
- `GroupBy` defaults to `["meter_id"]`

### 2.2 Fetch Meter Configs: `fetchMeters()`

**File**: `internal/service/meter_usage.go:487`

```
→ MeterRepo.List(ctx, filter)  // PostgreSQL query via Ent
```

Builds `meterMap[meter_id] → *meter.Meter` and collects aggregation types.

### 2.3 Split Meters by Type

Each meter is classified into one of:
- **Bucketed MAX** — `meter.IsBucketedMaxMeter()` (Aggregation.Type=MAX + BucketSize set)
- **Bucketed SUM** — `meter.IsBucketedSumMeter()` (Aggregation.Type=SUM + BucketSize set)
- **Standard** — everything else (SUM, COUNT, COUNT_UNIQUE, AVG, LATEST without bucket)

### 2.4 Process Each Meter Category

```
┌─────────────────────────┐
│ For each bucketed MAX   │ → getBucketedMeterAnalytics()  [Section 3]
│ For each bucketed SUM   │ → getBucketedMeterAnalytics()  [Section 3]
│ All standard meters     │ → repo.GetDetailedAnalytics()  [Section 4]
└─────────────────────────┘
           │
           ▼
   allResults []*MeterUsageDetailedResult
           │
           ▼
   buildAnalyticsResponse()  [Section 5]
```

---

## 3. Bucketed Meter Analytics: `getBucketedMeterAnalytics()`

**File**: `internal/service/meter_usage.go:514`

Called once per bucketed meter (MAX or SUM with `bucket_size`).

### 3.1 Build Query Params

```go
bucketParams := &MeterUsageQueryParams{
    MeterID:         m.ID,
    AggregationType: m.Aggregation.Type,   // MAX or SUM
    WindowSize:      m.Aggregation.BucketSize,  // e.g. "HOUR", "DAY"
    GroupByProperty: m.Aggregation.GroupBy,      // e.g. "krn" for per-resource MAX
}
```

### 3.2 Execute: `repo.GetUsageForBucketedMeters()`

**File**: `internal/repository/clickhouse/meter_usage.go:282`

Calls `qb.BuildBucketedQuery(params)` which produces one of two SQL shapes:

#### 3.2a Without GroupBy (2-level CTE)

```sql
WITH bucket_maxes AS (   -- or bucket_sums
    SELECT
        toStartOfHour(timestamp) as bucket_start,
        MAX(qty_total) as bucket_max   -- or SUM(qty_total) as bucket_sum
    FROM meter_usage [FINAL]
    WHERE tenant_id = ?
      AND environment_id = ?
      AND external_customer_id = ?
      AND meter_id = ?
      AND timestamp >= ?
      AND timestamp < ?
    GROUP BY bucket_start
    ORDER BY bucket_start
)
SELECT
    (SELECT sum(bucket_max) FROM bucket_maxes) as total,
    bucket_start as timestamp,
    bucket_max as value
FROM bucket_maxes
ORDER BY bucket_start
[SETTINGS do_not_merge_across_partitions_select_final = 1]
```

**Result**: `(total, bucket_start, value)` per row → scanned into `AggregationResult`

#### 3.2b With GroupBy (3-level CTE)

```sql
WITH per_group AS (
    SELECT
        toStartOfHour(timestamp) as bucket_start,
        JSONExtractString(properties, 'krn') as group_key,
        MAX(qty_total) as group_value   -- or SUM
    FROM meter_usage [FINAL]
    WHERE tenant_id = ?
      AND environment_id = ?
      AND external_customer_id = ?
      AND meter_id = ?
      AND timestamp >= ?
      AND timestamp < ?
    GROUP BY bucket_start, group_key
)
SELECT
    (SELECT sum(group_value) FROM per_group) as total,
    bucket_start as timestamp,
    group_value as value,
    group_key
FROM per_group
ORDER BY bucket_start, group_key
[SETTINGS do_not_merge_across_partitions_select_final = 1]
```

**Result**: `(total, bucket_start, value, group_key)` per row

### 3.3 Map Result to `MeterUsageDetailedResult`

- For bucketed MAX: `result.MaxUsage = total`, `result.TotalUsage = total` (SUM of MAXes)
- For bucketed SUM: `result.TotalUsage = total`
- Points are populated from `aggResult.Results[]` if `params.WindowSize` is set

### 3.4 Event Count: `getEventCountForMeter()`

**File**: `internal/service/meter_usage.go:593`

Bucketed queries don't return event counts, so a separate scalar query is fired:

```
→ repo.GetUsage(ctx, countParams)  // AggregationType = COUNT
```

Which produces:

```sql
SELECT
    COUNT(DISTINCT id) AS value,
    COUNT(DISTINCT id) AS event_count
FROM meter_usage [FINAL]
WHERE tenant_id = ?
  AND environment_id = ?
  AND external_customer_id = ?
  AND meter_id = ?
  AND timestamp >= ?
  AND timestamp < ?
[SETTINGS do_not_merge_across_partitions_select_final = 1]
```

---

## 4. Standard Meter Analytics: `repo.GetDetailedAnalytics()`

**File**: `internal/repository/clickhouse/meter_usage.go:389`

### 4.1 Parse GroupBy: `qb.BuildDetailedGroupByColumns()`

Maps group-by fields to SQL:
| Field | SQL Column | SQL Alias |
|-------|-----------|-----------|
| `meter_id` | `meter_id` | `meter_id` |
| `source` | `source` | `source` |
| `properties.region` | `JSONExtractString(properties, 'region')` | `prop_region` |

### 4.2 Build Conditional Aggregation Columns

**File**: `internal/repository/clickhouse/feature_usage.go:54` → `buildConditionalAggregationColumns()`

Based on the aggregation types of the requested meters, selects which aggregations to compute:

| Aggregation Type | SQL Column |
|-----------------|------------|
| SUM | `SUM(qty_total) AS total_usage` |
| MAX | `MAX(qty_total) AS max_usage` |
| LATEST | `argMax(qty_total, timestamp) AS latest_usage` |
| COUNT_UNIQUE | `COUNT(DISTINCT unique_hash) AS count_unique_usage` |
| COUNT | `COUNT(DISTINCT id) AS event_count` |

If an aggregation type is NOT needed, a zero placeholder is used (e.g., `toDecimal128(0, 9) AS total_usage`).

Additionally, if `source` is NOT in `GroupBy`: `groupUniqArray(source) AS sources`

### 4.3 Build WHERE Clause: `qb.BuildDetailedWhereClause()`

```sql
WHERE tenant_id = ?
  AND environment_id = ?
  AND external_customer_id = ?           -- if provided
  AND meter_id IN (?, ?, ...)            -- standard meter IDs
  AND timestamp >= ?
  AND timestamp < ?
  AND source IN (?, ?)                   -- if source filter provided
  AND properties != ''                   -- if property filters exist
  AND JSONExtractString(properties, ?) = ?  -- per property filter
```

### 4.4 Main Aggregation Query

```sql
SELECT
    meter_id,                              -- group-by columns
    SUM(qty_total) AS total_usage,         -- conditional agg columns
    MAX(qty_total) AS max_usage,
    argMax(qty_total, timestamp) AS latest_usage,
    COUNT(DISTINCT unique_hash) AS count_unique_usage,
    COUNT(DISTINCT id) AS event_count,
    groupUniqArray(source) AS sources      -- if source not in group_by
FROM meter_usage [FINAL]
WHERE <detailed_where_clause>
GROUP BY meter_id
[SETTINGS do_not_merge_across_partitions_select_final = 1]
```

**Scan**: Each row is scanned into `MeterUsageDetailedResult` with dynamic scan targets based on group-by column count.

### 4.5 Time-Series Points: `getDetailedAnalyticsPoints()` (per group row)

**File**: `internal/repository/clickhouse/meter_usage.go:536`

Only if `params.WindowSize` is set. For each result row, fires a sub-query:

`qb.BuildDetailedPointsQuery()` produces:

```sql
SELECT
    toStartOfHour(timestamp) AS window_start,   -- or toStartOfDay, etc.
    SUM(qty_total) AS total_usage,
    MAX(qty_total) AS max_usage,
    argMax(qty_total, timestamp) AS latest_usage,
    COUNT(DISTINCT unique_hash) AS count_unique_usage,
    COUNT(DISTINCT id) AS event_count
FROM meter_usage [FINAL]
WHERE <detailed_where_clause>
  AND meter_id = ?          -- narrow to this group's meter
  AND source = ?            -- narrow to this group's source (if grouped)
  AND JSONExtractString(properties, ?) = ?  -- narrow to property values
GROUP BY window_start
ORDER BY window_start ASC
[SETTINGS do_not_merge_across_partitions_select_final = 1]
```

**Scan**: `(window_start, total_usage, max_usage, latest_usage, count_unique_usage, event_count)` per point.

---

## 5. Response Building: `buildAnalyticsResponse()`

**File**: `internal/service/meter_usage.go:153`

```
allResults []*MeterUsageDetailedResult
           │
           ▼
   buildAnalyticsData()                    [Section 5.1]
           │
           ▼
   AnalyticsData (with subscription/pricing context)
           │
           ▼
   featureUsageTrackingService.CalculateCostsForAnalytics()  [Section 6]
           │
           ▼
   toUsageAnalyticsResponseDTO()           [Section 5.3]
           │
           ▼
   *dto.GetUsageAnalyticsResponse
```

### 5.1 Build Analytics Data: `buildAnalyticsData()`

**File**: `internal/service/meter_usage.go:188`

#### 5.1.1 Resolve Customer & Subscriptions: `resolveCustomerAndSubscriptions()`

```
→ CustomerRepo.GetByLookupKey(ctx, externalCustomerID)     // PostgreSQL
→ SubscriptionService.ListSubscriptions(ctx, filter)        // PostgreSQL
    filter.CustomerID = customer.ID
    filter.WithLineItems = true
    filter.SubscriptionStatus = [Active, Trialing, Paused, Cancelled]
```

Result: `customer`, `subscriptions` (with line items)

#### 5.1.2 Build Meter→LineItem→Price Mapping

```
For each subscription:
  For each line_item:
    → data.SubscriptionLineItems[li.ID] = li
    → collect li.PriceID

→ PriceRepo.List(ctx, priceFilter)  // PostgreSQL - batch fetch all prices

For each line_item with MeterID and PriceID:
    → meterToLineItem[meter_id] = first matching line_item
    → meterToPrice[meter_id] = corresponding price
```

#### 5.1.3 Resolve Meter→Feature Mapping

```
→ FeatureRepo.List(ctx, featureFilter)  // PostgreSQL
    featureFilter.MeterIDs = all meter IDs
```

Maps `meterToFeature[meter_id] → *feature.Feature`

#### 5.1.4 Convert Results to DetailedUsageAnalytic

Each `MeterUsageDetailedResult` → `events.DetailedUsageAnalytic`:

| MeterUsageDetailedResult | DetailedUsageAnalytic | Source |
|---|---|---|
| MeterID | MeterID | direct |
| TotalUsage | TotalUsage | direct |
| MaxUsage | MaxUsage | direct |
| LatestUsage | LatestUsage | direct |
| CountUniqueUsage | CountUniqueUsage | direct |
| EventCount | EventCount | direct |
| Source/Sources | Source/Sources | direct |
| Properties | Properties | direct |
| — | EventName | `meter.EventName` |
| — | AggregationType | `meter.Aggregation.Type` |
| — | FeatureID/FeatureName | from `meterToFeature` |
| — | Unit/UnitPlural | from `feature.UnitSingular/UnitPlural` |
| — | PriceID/SubLineItemID/SubscriptionID | from `meterToLineItem` |
| Points[].WindowStart | Points[].Timestamp | direct |
| Points[].TotalUsage | Points[].Usage | direct |
| Points[].MaxUsage | Points[].MaxUsage | direct |

### 5.2 Set Currency

From first subscription's currency.

### 5.3 Convert to Response DTO: `toUsageAnalyticsResponseDTO()`

**File**: `internal/service/meter_usage.go:388`

For each `DetailedUsageAnalytic`:
1. Calls `getCorrectMeterUsageValue()` to pick the right usage field based on aggregation type
2. Maps to `dto.UsageAnalyticItem` with all enriched fields
3. Sets `WindowSize` from meter's bucket size (bucketed) or params (standard)
4. Maps points to `dto.UsageAnalyticPoint` with cost and commitment info

#### `getCorrectMeterUsageValue()` Logic

| AggregationType | Usage Field Used |
|---|---|
| COUNT_UNIQUE | `decimal(CountUniqueUsage)` |
| MAX | `TotalUsage` (sum of bucket maxes) if non-zero, else `MaxUsage` |
| LATEST | `LatestUsage` |
| SUM, COUNT, AVG, default | `TotalUsage` |

Final response sorted by `FeatureName` ascending.

---

## 6. Cost Calculation Pipeline

**File**: `internal/service/feature_usage_tracking.go:2069`

```go
func (s *featureUsageTrackingService) CalculateCostsForAnalytics(ctx context.Context, data *AnalyticsData) error {
    return s.calculateCosts(ctx, data)
}
```

### 6.1 `calculateCosts()` — Main Loop

**File**: `internal/service/feature_usage_tracking.go:2073`

```
For each analytic item in data.Analytics:
    → Resolve feature from data.Features[item.FeatureID]
    → Resolve meter from data.Meters[feature.MeterID]
    → Resolve price from data.Prices[item.PriceID]
    
    if meter.IsBucketedMaxMeter() || meter.IsBucketedSumMeter():
        → calculateBucketedCost()   [Section 6.2]
    else:
        → calculateRegularCost()    [Section 6.3]
```

### 6.2 `calculateBucketedCost()`

**File**: `internal/service/feature_usage_tracking.go:2109`

Resolves commitment state:
```
lineItem = data.SubscriptionLineItems[item.SubLineItemID]
hasCommitment = lineItem.HasCommitment()
isWindowed = hasCommitment && lineItem.CommitmentWindowed
hasTrueUp = isWindowed && lineItem.CommitmentTrueUpEnabled
```

#### With Time-Series Points → `processPointsWithBuckets()`

```
1. extractBucketValues(points, aggType)
    → []decimal.Decimal (one per point, using correct usage field for aggType)

2. Calculate aggregate cost:
   - No commitment: priceService.CalculateBucketedCost(price, bucketedValues)
   - Windowed commitment: decimal.Zero (summed from per-point costs later)
   - Non-windowed commitment: applyLineItemCommitment(bucketedValues)

3. calculatePointCosts():
   - No windowed: per point → priceService.CalculateCost(price, usage)
   - Windowed: per point → commitmentCalculator.applyWindowCommitmentToLineItem()
       → Sets Cost, ComputedCommitmentUtilizedAmount, ComputedOverageAmount, ComputedTrueUpAmount

4. If hasTrueUp: fillMissingWindowsAndRecalculate()
   → generateBucketStarts(periodStart, periodEnd, bucketSize, billingAnchor)
   → Fill missing windows with zero-usage points
   → Recalculate total via applyWindowCommitmentToLineItem()

5. mergeBucketPointsByWindow()
   → Groups bucket-level points by their WindowStart
   → Aggregates (MAX→max, SUM→sum) usage within each window

6. If isWindowed && !hasTrueUp: total = sum of all point costs
```

#### Without Points → `processSingleBucket()`

```
totalUsage = getSingleBucketUsage(item, aggType)
if totalUsage > 0:
    bucketedValues = [totalUsage]
    baseCost = priceService.CalculateBucketedCost(price, bucketedValues)
    if hasCommitment: applyLineItemCommitment(bucketedValues, baseCost)
    return baseCost
if hasCommitment && hasTrueUp: fillZeroUsageWindows()
else if hasCommitment: applyLineItemCommitment(nil, Zero)
```

### 6.3 `calculateRegularCost()`

**File**: `internal/service/feature_usage_tracking.go:2342`

```
1. item.TotalUsage = getCorrectUsageValue(item, meter.Aggregation.Type)

2. cost = priceService.CalculateCost(ctx, price, item.TotalUsage)

3. If lineItem has commitment:
   a. Windowed commitment with points:
      → bucketedValues from points
      → applyLineItemCommitment(bucketedValues, Zero)
   b. Windowed commitment without points:
      → applyLineItemCommitment(nil, cost)
   c. Non-windowed commitment:
      → applyLineItemCommitment(nil, cost)

4. item.TotalCost = cost
   item.Currency = price.Currency

5. Per-point costs:
   For each point:
       pointUsage = getCorrectUsageValueForPoint(point, aggType)
       point.Cost = priceService.CalculateCost(price, pointUsage)
```

---

## 7. Price Calculation Methods

**File**: `internal/service/price.go`

### 7.1 `CalculateCost(price, quantity)` → `calculateSingletonCost()`

```
switch price.BillingModel:
    FLAT_FEE:  price.CalculateAmount(quantity)
                → unit_amount × quantity

    PACKAGE:   packages = ceil(quantity / divideBy)   -- or floor if ROUND_DOWN
               price.CalculateAmount(packages)

    TIERED:    calculateTieredCost(price, quantity)
```

### 7.2 `CalculateBucketedCost(price, []decimal.Decimal)` → `calculateBucketedMaxCost()`

```
For each bucket value in bucketedValues:
    if TIERED: bucketCost = calculateTieredCost(price, value)
    else:      bucketCost = calculateSingletonCost(price, value)
    totalCost += bucketCost

return totalCost.Round(currencyPrecision)
```

### 7.3 `calculateTieredCost()`

Supports two tier modes:

**VOLUME**: Find the single tier where quantity fits → apply that tier's pricing
```
for each tier (sorted by up_to ASC):
    if quantity <= tier.up_to:
        cost = tier.CalculateTierAmount(quantity)
        break
```

**SLAB** (graduated): Quantity fills tiers sequentially
```
remainingQuantity = quantity
for each tier:
    tierQuantity = min(remainingQuantity, tier.capacity)
    cost += tier.CalculateTierAmount(tierQuantity)
    remainingQuantity -= tierQuantity
    if remainingQuantity <= 0: break
```

---

## 8. Commitment Handling

**File**: `internal/service/feature_usage_tracking.go:3371`

### 8.1 `applyLineItemCommitment()`

```
if lineItem.CommitmentWindowed:
    → commitmentCalculator.applyWindowCommitmentToLineItem(lineItem, bucketedValues, price)
    → Sets item.CommitmentInfo with utilized/overage/true-up amounts
    → Returns adjusted cost

else (non-windowed):
    rawCost = defaultCost or CalculateBucketedCost(bucketedValues)
    → commitmentCalculator.applyCommitmentToLineItem(lineItem, rawCost, price)
    → Returns adjusted cost with commitment info
```

### 8.2 Commitment Types

| Type | Behavior |
|------|----------|
| **Non-windowed** | Single commitment amount applied to total cost. If usage < commitment → pay commitment minimum. If usage > commitment → pay usage cost. |
| **Windowed** | Commitment applied per time window (bucket). Each window independently checked against commitment amount. |
| **True-up (windowed)** | Like windowed, but missing windows are filled with zero-usage points. True-up amount calculated for under-utilized windows. |

---

## 9. Window Size Expressions

**File**: `internal/repository/clickhouse/aggregators.go:114`

| WindowSize | ClickHouse Expression |
|---|---|
| `MINUTE` | `toStartOfMinute(timestamp)` |
| `HOUR` | `toStartOfHour(timestamp)` |
| `DAY` | `toStartOfDay(timestamp)` |
| `WEEK` | `toStartOfWeek(timestamp)` |
| `15_MIN` | `toStartOfInterval(timestamp, INTERVAL 15 MINUTE)` |
| `30_MIN` | `toStartOfInterval(timestamp, INTERVAL 30 MINUTE)` |
| `3_HOUR` | `toStartOfInterval(timestamp, INTERVAL 3 HOUR)` |
| `6_HOUR` | `toStartOfInterval(timestamp, INTERVAL 6 HOUR)` |
| `12_HOUR` | `toStartOfInterval(timestamp, INTERVAL 12 HOUR)` |
| `MONTH` | `toStartOfMonth(timestamp)` |
| `MONTH` + billing anchor | `addDays(toStartOfMonth(addDays(timestamp, -N)), N)` where N = anchor_day - 1 |

---

## 10. ClickHouse Table: `meter_usage`

Engine: `ReplacingMergeTree(ingested_at)` — deduplicates on `(id, meter_id)` using `FINAL` keyword.

### Columns Used in Queries

| Column | Type | Usage |
|---|---|---|
| `id` | String | Event ID, used for `COUNT(DISTINCT id)` |
| `tenant_id` | String | Multi-tenant isolation |
| `environment_id` | String | Environment scope |
| `external_customer_id` | String | Customer filter |
| `meter_id` | String | Meter filter and group-by |
| `timestamp` | DateTime64 | Time range filtering, window grouping |
| `qty_total` | Decimal128 | Main value column for SUM/MAX/AVG/argMax |
| `unique_hash` | String | Used for `COUNT(DISTINCT unique_hash)` |
| `source` | String | Source filtering and grouping |
| `properties` | String (JSON) | Property filtering via `JSONExtractString()` |
| `ingested_at` | DateTime64 | ReplacingMergeTree version column |

---

## 11. Complete SQL Query Summary

| Step | Query | Table | When |
|---|---|---|---|
| Bucketed aggregation (no GroupBy) | 2-level CTE: bucket → total | `meter_usage` | Per bucketed MAX/SUM meter |
| Bucketed aggregation (with GroupBy) | 3-level CTE: per_group → total | `meter_usage` | Per bucketed MAX meter with `group_by` |
| Bucketed event count | Scalar COUNT | `meter_usage` | Per bucketed meter (separate query) |
| Standard aggregation | GROUP BY with conditional aggs | `meter_usage` | All standard meters in one query |
| Standard time-series points | Windowed GROUP BY per group row | `meter_usage` | Per standard result row, if WindowSize set |
| Customer lookup | GetByLookupKey | `customers` (PG) | Once per request |
| Subscriptions lookup | List with line items | `subscriptions` (PG) | Once per request |
| Prices fetch | List by IDs | `prices` (PG) | Once per request |
| Features fetch | List by meter IDs | `features` (PG) | Once per request |
| Meters fetch | List by IDs | `meters` (PG) | Once per request |

---

## 12. Request → Response Data Flow Diagram

```
Request: { meter_ids, external_customer_id, start_time, end_time, group_by, window_size }
                │
                ▼
        ┌───────────────────┐
        │  Fetch Meters (PG)│
        └───────┬───────────┘
                │
        ┌───────┴───────────────────────┐
        │                               │
   Bucketed Meters              Standard Meters
        │                               │
   getBucketedMeterAnalytics()   repo.GetDetailedAnalytics()
        │                               │
   ┌────┴────┐                   ┌──────┴──────┐
   │Bucketed │                   │Main Agg     │
   │CTE Query│                   │Query (CH)   │
   │  (CH)   │                   ├─────────────┤
   ├─────────┤                   │Points Query │
   │Event    │                   │per row (CH) │
   │Count    │                   └──────┬──────┘
   │Query(CH)│                          │
   └────┬────┘                          │
        │                               │
        └───────┬───────────────────────┘
                │
                ▼
        allResults []*MeterUsageDetailedResult
                │
        ┌───────┴───────────┐
        │ buildAnalyticsData│
        │ (resolve PG data) │
        │  - Customer       │
        │  - Subscriptions  │
        │  - LineItems      │
        │  - Prices         │
        │  - Features       │
        └───────┬───────────┘
                │
                ▼
        AnalyticsData
                │
        ┌───────┴───────────────────┐
        │ CalculateCostsForAnalytics│
        │ (feature usage pipeline)  │
        │  - Bucketed cost calc     │
        │  - Regular cost calc      │
        │  - Commitment handling    │
        └───────┬───────────────────┘
                │
                ▼
        toUsageAnalyticsResponseDTO()
                │
                ▼
        GetUsageAnalyticsResponse {
            total_cost,
            currency,
            items: [{
                feature_id, price_id, meter_id,
                subscription_id, sub_line_item_id,
                feature_name, event_name, source,
                unit, unit_plural, aggregation_type,
                total_usage, total_cost, currency,
                event_count, window_size,
                commitment_info,
                points: [{
                    timestamp, usage, cost, event_count,
                    computed_commitment_utilized_amount,
                    computed_overage_amount,
                    computed_true_up_amount
                }]
            }]
        }
```
