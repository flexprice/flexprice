# Portal Usage by Event Properties — Design ERD (Phase 1)

Status: **Proposed**
Date: 2026-10-15
Author: Harshit Gupta

---

## 1. The Problem

Customers can only see usage and cost per feature (SKU) in the customer portal. They cannot split it by
the properties on their events.

A typical event:

```json
{ "cloud_provider": "aws", "region": "us-west-2", "instance_id": "instance-zuve4s7kg",
  "cpu_core_hours": "2", "memory_gb_hours": "8", "node_hours": "0" }
```

The customer wants to know what each environment costs, not just total CPU. That means they need to:

1. **Group by** any property, e.g. cost per `region` or per `instance_id`.
2. **Filter by** any property, e.g. `cloud_provider = aws`.
3. **See which keys and values exist**, so they can choose what to group or filter by.

What exists today:

- `POST /v1/customer/portal/analytics/revenue` already accepts `group_by: ["properties.<key>"]` and
`property_filters`, and returns usage and cost per feature × group.
- The portal UI never sends either. It sends only the date range, `window_size: DAY` and
`expand: ["price"]`.
- There is no customer-facing SKU picker. The tenant's include/exclude feature list is applied in the
browser after the data is fetched.
- Nothing lists the property keys or values that exist.

## 2. The Solution

Use the existing portal analytics engine. Add two lookup endpoints and a top-N limit, then build the
portal UI on top. Analytics views are not used: a view has fixed dimensions, and this feature needs the
group-by chosen at query time.

### 2.1 Property keys

`POST /v1/customer/portal/analytics/properties`

Lists the property keys that exist for the customer on the selected features.

- **Input:** `feature_ids` (optional, all features if empty), `start_time`, `end_time`.
- **Query:** keys and raw values (`JSONExtractKeysAndValuesRaw`) over the latest 10k `meter_usage`
rows, split evenly across the selected meters. Rows are filtered by tenant, environment, customer
(from the session), meter and the time window. Each meter is sampled separately (`meter_id = ?`), so
ClickHouse can read the latest rows in table order and stop at the limit.
- **Hidden keys:**
  - Quantity keys: the union of `aggregation.field` and the properties used in `aggregation.expression`,
  across every meter on the same `event_name`. Meter `group_by` and filter keys stay visible.
  - Keys whose sampled values are all numeric, e.g. `node_hours`. Each raw value is tested against
  `^"?-?[0-9]+(\.[0-9]+)?"?$`, which matches numeric strings (`"0"`) and JSON numbers (`0`). A key is
  hidden when no sampled value fails the test. `"us-west-2"` fails it, so `region` stays visible.
- **Output:** keys ordered by how often they appear in the sample.

### 2.2 Property values

`POST /v1/customer/portal/analytics/properties/values`

Lists the values of one key, with an optional search by prefix.

- **Input:** `key`, `feature_ids`, `start_time`, `end_time`, `prefix` (optional), `limit` (default 50,
max 200).
- **Query:** `JSONExtractString(properties, key)` grouped and counted over the window, with an optional
`startsWith` on `prefix`. Ordered by count, highest first.
- **Validation:** `key` must match the existing property-name pattern (`^[A-Za-z0-9_.]+$`).
- **Bounds:** customer, meters and window from the request, the existing ClickHouse memory cap, and a
query timeout.

### 2.3 Top-N on portal analytics

Add `limit` to `POST /v1/customer/portal/analytics/revenue`.

- Applies only when `group_by` includes a `properties.*` key.
- The response adds `total_groups`, so the UI can show "top 10 of 1,240".

**Today's query problem.** With `window_size` set (the portal always sends `DAY`), the repository runs
one aggregate query, then **one points query per result row**
(`MeterUsageRepository.GetDetailedAnalytics`). Grouping 2 features by `instance_id` across 1,240
instances runs 2,481 sequential ClickHouse queries. Top-N applied afterwards does not help, because
every group's points are already fetched.

**New flow.** Points are fetched only for the groups that survive the cut:

1. **Aggregate query.** One query, grouped by `meter_id` and the property keys, with no time buckets.
  One row per feature × group. Cheap: no points.
2. **Price and rank in Go.** Each row is priced by its line item. Groups are ranked by cost summed
  across the requested features, ties broken by group value, and the top `limit` are kept.
3. **One points query** for the surviving group values only (`… IN (top values)`), grouped by
  `meter_id`, group keys and `window_start`. This replaces the per-row points queries.

### 2.4 Query cost

`R` is the number of `meter_usage` rows for the customer, the selected meters and the window. For
example, 1,240 instances × hourly events × 30 days × 2 meters ≈ 1.8M. `G` is the number of groups.


| Query                   | Reads                            | Cost                                                                                               |
| ----------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------- |
| Keys (§2.1)             | 10k rows, latest first           | Constant. Stops at the limit.                                                                      |
| Values (§2.2)           | `R`, properties only, no `FINAL` | O(R) JSON parses + a GROUP BY over distinct values                                                 |
| Aggregate (§2.3 step 1) | `R` with `FINAL`                 | O(R) JSON parses (group key + each filter) + `COUNT(DISTINCT id)`, which holds ids in memory       |
| Points (§2.3 step 3)    | `R` with `FINAL`                 | Same as the aggregate. The `IN` cuts the rows aggregated but not the rows read; JSON has no index. |
| **Points today**        | `R` per group, `G` times         | **O(G × R)**: the heaviest by far                                                                  |


The new lookups are lighter than the analytics queries. Keys is constant and values skips `FINAL` and
`COUNT(DISTINCT)`. The heaviest part today is the per-group points loop. Replacing it brings a
grouped request down to two O(R) scans. Each property filter adds one more JSON parse per row.

### 2.5 Delivery priority

Delivery order is **grouping → filtering by values → top-N**. Each step is usable on its own.

1. **Grouping:** the keys endpoint (§2.1), the group-by and SKU picker UI, and the single batched
  points query (§2.3 steps 1 and 3, with no ranking). The batched query belongs here because grouping
   by a key like `instance_id` is unusable with the per-group points loop.
2. **Filtering:** the values endpoint (§2.2) and the filter chips.
3. **Top-N:** ranking in Go (§2.3 step 2), `limit`, `total_groups` and the "top N of M" label.

### 2.6 Tenant API parity

The same two lookups go on the tenant API, with `external_customer_id` as a required input:

- `POST /v1/meter-usage/properties`
- `POST /v1/meter-usage/properties/values`

Tenants can then build their own reports, and our dashboard can reuse them.

### 2.5 Portal UI (frontend)

- **SKU picker.** Multi-select over the customer's features, sent as `feature_ids`. The tenant's
`include_list` / `exclude_list` becomes the picker's options and default, so filtering happens in the
request rather than in the browser.
- **Group by.** A single key, chosen from §2.1.
- **Filters.** Chips per key, with values from §2.2. Multiple values in one chip mean `IN`; separate
chips are combined with `AND`.
- **Top N.** 10 by default, with "top N of M" shown.
- **Trend chart.** One line per group value, e.g. `aws` and `gcp`, with daily cost summed across the
selected features. When exactly one feature is selected, it can show usage instead. Today the chart
draws one usage line per feature.
- **Breakdown table.** One row per group value with its total cost. Each row expands into that group's
features, with usage and cost. The existing expandable table can be reused.
- **Date range.** The existing presets drive every call, including both lookups.

## 3. Examples

The customer is `acme`, with features CPU (`mtr_cpu`) and Memory (`mtr_mem`). Both are SUM meters on
one subscription, so each request runs one aggregate query and one points query. Values are written
inline for readability; the code binds them as parameters. `<scope>` below stands for:

```sql
tenant_id = ? AND environment_id = ? AND external_customer_id = 'acme'
AND meter_id IN ('mtr_cpu', 'mtr_mem')
AND timestamp >= '2026-09-16 00:00:00' AND timestamp < '2026-10-15 10:00:00'
```

### 3.1 Cost per cloud provider, last 30 days

Portal: Group by **cloud_provider**. The chart shows one line per provider; the table shows CPU and
memory cost per provider.

```http
POST /v1/customer/portal/analytics/properties
{ "start_time": "2026-09-16T00:00:00Z", "end_time": "2026-10-15T10:00:00Z" }
→ { "keys": ["instance_id", "region", "cloud_provider"] }

POST /v1/customer/portal/analytics/revenue
{ "start_time": "2026-09-16T00:00:00Z", "end_time": "2026-10-15T10:00:00Z",
  "window_size": "DAY", "group_by": ["properties.cloud_provider"], "limit": 10, "expand": ["price"] }
→ { "total_cost": "1840.00", "currency": "usd", "total_groups": 2,
    "items": [
      { "feature_id": "feat_cpu", "properties": { "cloud_provider": "aws" }, "total_usage": "620", "total_cost": "1240.00", "points": [...] },
      { "feature_id": "feat_cpu", "properties": { "cloud_provider": "gcp" }, "total_usage": "300", "total_cost": "600.00",  "points": [...] } ] }
```

```sql
-- keys: sample the latest 5k rows per meter (run once each for mtr_cpu and mtr_mem),
-- flag keys whose values are all numeric
SELECT kv.1 AS key, count() AS seen,
       countIf(NOT match(kv.2, '^"?-?[0-9]+(\\.[0-9]+)?"?$')) AS non_numeric
FROM (SELECT properties FROM meter_usage
      WHERE tenant_id = ? AND environment_id = ? AND external_customer_id = 'acme'
        AND meter_id = 'mtr_cpu' AND timestamp >= ? AND timestamp < ?
      ORDER BY timestamp DESC LIMIT 5000)
ARRAY JOIN JSONExtractKeysAndValuesRaw(properties) AS kv
GROUP BY key ORDER BY seen DESC
-- Go merges both results, then drops keys with non_numeric = 0 and the meters' quantity keys

-- 1. aggregate: one row per feature × provider
SELECT meter_id, JSONExtractString(properties, 'cloud_provider') AS prop_cloud_provider,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope>
GROUP BY meter_id, JSONExtractString(properties, 'cloud_provider')
SETTINGS do_not_merge_across_partitions_select_final = 1

-- 2. Go: price each row, rank providers by summed cost → ['aws', 'gcp'] (2 of 2)

-- 3. points for the kept providers only
SELECT meter_id, JSONExtractString(properties, 'cloud_provider') AS prop_cloud_provider,
       toStartOfDay(timestamp, 'UTC') AS window_start,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope> AND JSONExtractString(properties, 'cloud_provider') IN ('aws', 'gcp')
GROUP BY meter_id, prop_cloud_provider, window_start
ORDER BY window_start
SETTINGS do_not_merge_across_partitions_select_final = 1
```

`node_hours`, `cpu_core_hours` and the other numeric keys are hidden from the keys list.

### 3.2 Top instances in one AWS region

Portal: Filters **cloud_provider = aws** and **region = us-west-2**, group by **instance_id**, top 10.
The customer types `instance-zu` to find one instance.

```http
POST /v1/customer/portal/analytics/properties/values
{ "key": "region", "start_time": "...", "end_time": "..." }
→ { "values": [ { "value": "us-west-2", "count": 41200 }, { "value": "eu-west-1", "count": 9800 } ] }

POST /v1/customer/portal/analytics/properties/values
{ "key": "instance_id", "prefix": "instance-zu", "limit": 20, "start_time": "...", "end_time": "..." }
→ { "values": [ { "value": "instance-zuve4s7kg", "count": 720 } ] }

POST /v1/customer/portal/analytics/revenue
{ "start_time": "...", "end_time": "...", "window_size": "DAY",
  "group_by": ["properties.instance_id"],
  "property_filters": { "cloud_provider": ["aws"], "region": ["us-west-2"] },
  "limit": 10 }
→ 10 instances ranked by cost, "total_groups": 1240
```

```sql
-- values for region (count() counts rows; it is only used for ordering)
SELECT JSONExtractString(properties, 'region') AS value, count() AS count
FROM meter_usage
WHERE <scope> AND value != ''
GROUP BY value ORDER BY count DESC LIMIT 50

-- values for instance_id, searched by prefix
SELECT JSONExtractString(properties, 'instance_id') AS value, count() AS count
FROM meter_usage
WHERE <scope> AND startsWith(value, 'instance-zu')
GROUP BY value ORDER BY count DESC LIMIT 20

-- 1. aggregate: ~2,480 rows (1,240 instances × 2 features)
SELECT meter_id, JSONExtractString(properties, 'instance_id') AS prop_instance_id,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope>
  AND JSONExtractString(properties, 'cloud_provider') = 'aws'
  AND JSONExtractString(properties, 'region') = 'us-west-2'
GROUP BY meter_id, JSONExtractString(properties, 'instance_id')
SETTINGS do_not_merge_across_partitions_select_final = 1

-- 2. Go: price, rank instances by summed cost → top 10 of 1,240

-- 3. points for the 10 kept instances only (today: 2,480 separate queries)
SELECT meter_id, JSONExtractString(properties, 'instance_id') AS prop_instance_id,
       toStartOfDay(timestamp, 'UTC') AS window_start,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope>
  AND JSONExtractString(properties, 'cloud_provider') = 'aws'
  AND JSONExtractString(properties, 'region') = 'us-west-2'
  AND JSONExtractString(properties, 'instance_id') IN ('instance-zuve4s7kg', /* … 9 more */)
GROUP BY meter_id, prop_instance_id, window_start
ORDER BY window_start
SETTINGS do_not_merge_across_partitions_select_final = 1
```

### 3.3 One SKU, split by region

Portal: SKU picker set to **CPU** only, group by **region**.

```http
POST /v1/customer/portal/analytics/properties
{ "feature_ids": ["feat_cpu"], "start_time": "...", "end_time": "..." }

POST /v1/customer/portal/analytics/revenue
{ "feature_ids": ["feat_cpu"], "start_time": "...", "end_time": "...",
  "window_size": "DAY", "group_by": ["properties.region"], "limit": 10 }
```

The same queries as §3.1, with `meter_id IN ('mtr_cpu')` in `<scope>` and `region` as the group key.
With one feature, global and per-feature top-N are the same.

The tenant API version of each call is the same body plus `external_customer_id`, sent to
`/v1/meter-usage/properties[/values]` and `/v1/meter-usage/detailed-analytics`.

## 4. Limitations

- **Cost per group is exact only for flat per-unit prices.** Each group is priced on its own usage, so
tiered, volume and package prices will not add up to the invoice.
- **Commitments are skipped** whenever results are grouped or filtered.
- **No "other" row.** Groups cut by top-N are dropped, so the totals shown can be lower than the invoice.
The "top N of M" label makes the cut visible.
- **Non-additive meters** (MAX, LATEST, COUNT_UNIQUE). Group values for these meters cannot be added up.
Phase 1 shows them as they are, without adjusting.
- **Key sampling can miss rare keys.** Only the latest 10k rows are read.
- **Hiding is heuristic.**
  - A real numeric dimension, e.g. `gpu_count`, is hidden, and there is no override yet.
  - Values stored as JSON numbers read as `""` through `JSONExtractString`.
- **Nested properties are not supported.** `a.b` is looked up as a key literally named `"a.b"`.
- **The UI groups by one key at a time.** The API accepts more than one.
- **Value lookups scan the window.** They are bounded by customer, meter and time. Very large customers
may see slow lookups on long windows.

## 5. Follow-ups

1. **Price the total once, then split by share.** Price each line item's unfiltered, ungrouped total
  once and allocate it by usage share, starting with SUM/COUNT meters. This fixes tiered, volume and
   package cost per group. Zero-usage cost is split evenly.
2. **"Other" row** for SUM/COUNT meters, so the totals shown match the invoice.
3. **Tenant setting to always show or always hide keys,** overriding the heuristics.
4. **Read properties with type-agnostic extraction,** so numeric values are no longer read as `""`.
5. **Property index.** A ClickHouse materialized view keyed by tenant, environment, customer, meter,
  key and value, if lookups get slow.
6. **Top-N inside ClickHouse** (two passes), if per-customer group counts make responses heavy.
7. **Multi-key group-by** in the UI.
8. **Analytics views as saved reports.** Add a cost metric, run views from the portal, and schedule
  exports.

