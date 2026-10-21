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
- **Query:** `JSONExtractKeys(properties)` over the latest 10k `meter_usage`
rows, split evenly across the selected meters. Rows are filtered by tenant, environment, customer
(from the session), meter and the time window. Each meter is sampled separately (`meter_id = ?`), so
ClickHouse can read the latest rows in table order and stop at the limit.
- **Hidden keys:**
  - Quantity keys, i.e. the union of `aggregation.field` and the properties used in
  `aggregation.expression`, across every meter on the same `event_name`. Meter `group_by` and filter
  keys stay visible.
  - Keys excluded by the tenant's visibility config (§2.4).
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
- The response adds `total_groups`, so the UI can show "top 10 of 1,240". `total_cost` and `subtotal`
stay the totals over all groups.

**Today's query problem.** With `window_size` set (the portal always sends `DAY`), the repository runs
one aggregate query, then **one points query per result row**
(`MeterUsageRepository.GetDetailedAnalytics`). Grouping 2 features by `instance_id` across 1,240
instances runs 2,481 sequential ClickHouse queries.

**New flow.** The fix stays inside the repository, so the cost pipeline and every other caller are
unchanged:

1. **Aggregate query.** One query, grouped by `meter_id` and the property keys, with no time buckets.
  One row per feature × group.
2. **One points query for all groups.** Grouped by `meter_id`, the group keys and `window_start`.
  Points are attached to each group by its key. This replaces the per-row points queries.
3. **Price, then cut in Go.** Each group is priced by its line item as today. The groups are then
  ranked by cost summed across the requested features, ties broken by group value, and the top
  `limit` are kept.

Points for groups that are cut are still fetched. That costs one query instead of `G`, and keeps the
change out of the cost pipeline.

### 2.4 Key visibility config

Tenants control which keys the portal exposes through a new typed block in the existing
`customer_portal_config` setting (`CustomerPortalConfig.AnalyticsConfig.UsageProperties`). It is
global for the tenant environment and applies to every portal widget and lookup.

```json
"analytics_config": {
  "usage_properties": {
    "allowed_keys": ["cloud_provider", "region", "instance_id"],
    "hidden_keys": ["internal_trace_id"]
  }
}
```

- `allowed_keys` **(whitelist).** When non-empty, only these keys can be shown, and a new event
property stays hidden until it is added. When empty, every key is allowed.
- `hidden_keys` **(blacklist).** Always hidden, even if listed in `allowed_keys`.
- **Visible keys** = discovered keys ∩ `allowed_keys` (if set) − meter quantity keys − `hidden_keys`.
Meter quantity keys cannot be made visible.
- Each entry must match the property-name pattern.

**Editing.** In the dashboard's existing portal-config editor. Each list is a multi-select fed by the
tenant keys endpoint (§2.7), with free text for keys that haven't been seen yet. Saving goes through
`PUT /v1/settings/customer_portal_config`, which requires a super admin.

### 2.5 Query cost

`R` is the number of `meter_usage` rows for the customer, the selected meters and the window. For
example, 1,240 instances × hourly events × 30 days × 2 meters ≈ 1.8M. `G` is the number of groups.


| Query                   | Reads                            | Cost                                                                                         |
| ----------------------- | -------------------------------- | -------------------------------------------------------------------------------------------- |
| Keys (§2.1)             | 10k rows, latest first           | Constant. Stops at the limit.                                                                |
| Values (§2.2)           | `R`, properties only, no `FINAL` | O(R) JSON parses + a GROUP BY over distinct values                                           |
| Aggregate (§2.3 step 1) | `R` with `FINAL`                 | O(R) JSON parses (group key + each filter) + `COUNT(DISTINCT id)`, which holds ids in memory |
| Points (§2.3 step 2)    | `R` with `FINAL`                 | Same as the aggregate. Returns `G` × windows rows, e.g. 1,240 × 30.                          |
| **Points today**        | `R` per group, `G` times         | **O(G × R)**: the heaviest by far                                                            |


The new lookups are lighter than the analytics queries. Keys is constant and values skips `FINAL` and  
`COUNT(DISTINCT)`. The heaviest part today is the per-group points loop. Replacing it brings a  
grouped request down to two O(R) scans. Each property filter adds one more JSON parse per row.

### 2.8 Portal UI (frontend)

- **SKU picker.** Multi-select over the customer's features, sent as `feature_ids`. The tenant's
`include_list` / `exclude_list` becomes the picker's options and default, so filtering happens in the
request rather than in the browser.
- **Group by.** A single key, chosen from §2.1. Only visible keys are offered.
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
→ { "keys": ["instance_id", "region", "cloud_provider", "node_hours"] }

POST /v1/customer/portal/analytics/revenue
{ "start_time": "2026-09-16T00:00:00Z", "end_time": "2026-10-15T10:00:00Z",
  "window_size": "DAY", "group_by": ["properties.cloud_provider"], "limit": 10, "expand": ["price"] }
→ { "total_cost": "1840.00", "currency": "usd", "total_groups": 2,
    "items": [
      { "feature_id": "feat_cpu", "properties": { "cloud_provider": "aws" }, "total_usage": "620", "total_cost": "1240.00", "points": [...] },
      { "feature_id": "feat_cpu", "properties": { "cloud_provider": "gcp" }, "total_usage": "300", "total_cost": "600.00",  "points": [...] } ] }
```

```sql
-- keys: sample the latest 5k rows per meter (run once each for mtr_cpu and mtr_mem)
SELECT key, count() AS seen
FROM (SELECT properties FROM meter_usage
      WHERE tenant_id = ? AND environment_id = ? AND external_customer_id = 'acme'
        AND meter_id = 'mtr_cpu' AND timestamp >= ? AND timestamp < ?
      ORDER BY timestamp DESC LIMIT 5000)
ARRAY JOIN JSONExtractKeys(properties) AS key
GROUP BY key ORDER BY seen DESC
-- Go merges both results, then drops the meters' quantity keys

-- 1. aggregate: one row per feature × provider
SELECT meter_id, JSONExtractString(properties, 'cloud_provider') AS prop_cloud_provider,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope>
GROUP BY meter_id, JSONExtractString(properties, 'cloud_provider')
SETTINGS do_not_merge_across_partitions_select_final = 1

-- 2. points for every provider, in one query
SELECT meter_id, JSONExtractString(properties, 'cloud_provider') AS prop_cloud_provider,
       toStartOfDay(timestamp, 'UTC') AS window_start,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope>
GROUP BY meter_id, prop_cloud_provider, window_start
ORDER BY window_start
SETTINGS do_not_merge_across_partitions_select_final = 1

-- 3. Go: price each row, rank providers by summed cost → ['aws', 'gcp'] (2 of 2)
```

`cpu_core_hours` and `memory_gb_hours` are hidden because the CPU and Memory meters aggregate them.
`node_hours` stays visible, because no meter aggregates it.

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

-- 2. points for every instance, in one query (today: 2,480 separate queries)
SELECT meter_id, JSONExtractString(properties, 'instance_id') AS prop_instance_id,
       toStartOfDay(timestamp, 'UTC') AS window_start,
       SUM(qty_total) AS total_usage, COUNT(DISTINCT id) AS event_count
FROM meter_usage FINAL
WHERE <scope>
  AND JSONExtractString(properties, 'cloud_provider') = 'aws'
  AND JSONExtractString(properties, 'region') = 'us-west-2'
GROUP BY meter_id, prop_instance_id, window_start
ORDER BY window_start
SETTINGS do_not_merge_across_partitions_select_final = 1

-- 3. Go: price, rank instances by summed cost → keep top 10 of 1,240
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

### 3.4 Tenant hides a key

The tenant adds `node_hours` to `hidden_keys` in the dashboard's portal-config editor.

```http
PUT /v1/settings/customer_portal_config
{ "value": { ..., "analytics_config": { "usage_properties": { "hidden_keys": ["node_hours"] } } } }

POST /v1/customer/portal/analytics/properties
{ "start_time": "...", "end_time": "..." }
→ { "keys": ["instance_id", "region", "cloud_provider"] }

POST /v1/customer/portal/analytics/revenue
{ "start_time": "...", "end_time": "...", "group_by": ["properties.node_hours"] }
→ 400 { "error": "property key \"node_hours\" is not available" }
```

If the tenant instead sets `allowed_keys: ["cloud_provider", "region"]`, the portal shows only those
two. `instance_id` and `node_hours` are hidden, and so is any key added to events later.

## 4. Limitations

- **Cost per group is exact only for flat per-unit prices.** Each group is priced on its own usage, so
tiered, volume and package prices will not add up to the invoice.
- **Commitments are skipped** whenever results are grouped or filtered.
- **No "other" row.** Groups cut by top-N are dropped, so the totals shown can be lower than the invoice.
The "top N of M" label makes the cut visible.
- **Non-additive meters** (MAX, LATEST, COUNT_UNIQUE). Group values for these meters cannot be added up.
Phase 1 shows them as they are, without adjusting.
- **Key sampling can miss rare keys.** Only the latest 10k rows are read.
- **Only meter quantity keys are hidden automatically.** Other quantity-like fields that no meter
aggregates, e.g. `node_hours`, stay visible unless the tenant lists them in `hidden_keys` or leaves
them out of `allowed_keys`.
- **Visibility is global.** One config applies to every feature; a key can't be hidden for only one
feature.
- **Values stored as JSON numbers read as** `""` through `JSONExtractString`.
- **Nested properties are not supported.** `a.b` is looked up as a key literally named `"a.b"`.
- **The UI groups by one key at a time.** The API accepts more than one.
- **Value lookups scan the window.** They are bounded by customer, meter and time. Very large customers
may see slow lookups on long windows.



## 5. Follow-ups

1. **Price the total once, then split by share.** Price each line item's unfiltered, ungrouped total
  once and allocate it by usage share, starting with SUM/COUNT meters. This fixes tiered, volume and
   package cost per group. Zero-usage cost is split evenly.
2. **"Other" row** for SUM/COUNT meters, so the totals shown match the invoice.
3. **Read properties with type-agnostic extraction,** so numeric values are no longer read as `""`.
4. **Property index.** A ClickHouse materialized view keyed by tenant, environment, customer, meter,
  key and value, if lookups get slow.
5. **Top-N inside ClickHouse** (two passes), if per-customer group counts make responses heavy.
  Points would then be fetched only for the groups that are kept.
6. **Batch the bucketed-meter points query.** `GetUsageForBucketedMetersDetailed` also runs one points
  query per group, for MAX/SUM meters with `bucket_size`.
7. **Multi-key group-by** in the UI.
8. **Analytics views as saved reports.** Add a cost metric, run views from the portal, and schedule
  exports.

