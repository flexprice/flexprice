# meter_usage — ClickHouse Table Design (Final)

## Context

- Central aggregation/enrichment table on top of raw events
- ~1B+ events/month ingested, 1M RPM writes, 500K RPM reads (billing), 100K RPM reads (analytics)
- Single-node self-hosted ClickHouse: target 16 vCPU / 64 GB RAM
- ReplacingMergeTree for deduplication (same event can arrive 2-3x)
- Each event can belong to multiple meters; aggregation types include SUM, COUNT, COUNT_UNIQUE, MAX, AVG with optional time-window bucketing

---

## Final Schema

```sql
CREATE TABLE IF NOT EXISTS flexprice.meter_usage
(
    -- dedup identity
    id                    String                   NOT NULL  CODEC(ZSTD(1)),

    -- tenant scope
    tenant_id             LowCardinality(String)   NOT NULL,
    environment_id        LowCardinality(String)   NOT NULL,

    -- query dimensions
    external_customer_id  LowCardinality(String)   NOT NULL,
    meter_id              LowCardinality(String)   NOT NULL,

    -- time
    timestamp             DateTime                 NOT NULL  CODEC(DoubleDelta, ZSTD(1)),
    ingested_at           DateTime64(3)            NOT NULL  DEFAULT now64(3)
                                                             CODEC(Delta, ZSTD(1)),

    -- metric
    qty_total             Decimal(18, 8)           NOT NULL  CODEC(ZSTD(1)),

    -- COUNT_UNIQUE support
    unique_hash           String                   NOT NULL  DEFAULT ''
                                                             CODEC(ZSTD(1)),

    -- analytics dimensions (cold path — not read by billing queries)
    source                LowCardinality(String)   NOT NULL  DEFAULT '',
    properties            String                   NOT NULL  DEFAULT ''
                                                             CODEC(ZSTD(3)),

    -- skip index: backfill reconciliation
    INDEX idx_ingested_at ingested_at TYPE minmax GRANULARITY 1
)
ENGINE = ReplacingMergeTree(ingested_at)
PARTITION BY toYYYYMMDD(timestamp)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, meter_id, timestamp)
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)
SETTINGS
    index_granularity                        = 8192,
    parts_to_delay_insert                    = 150,
    parts_to_throw_insert                    = 300,
    max_bytes_to_merge_at_max_space_in_pool  = 2147483648,
    min_bytes_for_wide_part                  = 10485760,
    enable_mixed_granularity_parts           = 1;
```

---

## Column Decisions

| Column | Type | Why |
|---|---|---|
| `id` | `String CODEC(ZSTD(1))` | Event identity, tail of ORDER BY for dedup uniqueness. Variable-length IDs (evt_*) so String not FixedString. |
| `tenant_id` | `LowCardinality(String)` | Hundreds of distinct values. LC stores dictionary per part, replaces string with int index. Saves GB at scale. |
| `environment_id` | `LowCardinality(String)` | Even fewer distinct values (prod/staging/dev). Same LC win. |
| `external_customer_id` | `LowCardinality(String)` | Tens of thousands of distinct values — within LC's ~1M per-part ceiling. Falls back gracefully if exceeded. |
| `meter_id` | `LowCardinality(String)` | 5K meters per customer but bounded total distinct values across table. LC is ideal. |
| `timestamp` | `DateTime CODEC(DoubleDelta, ZSTD(1))` | Downgraded from DateTime64(3). Aggregation table doesn't need ms precision. 4 bytes vs 8 bytes/row. DoubleDelta compresses sequential second-precision timestamps to near-zero. |
| `ingested_at` | `DateTime64(3) CODEC(Delta, ZSTD(1))` | Kept at ms precision — this is the RMT version column and must distinguish duplicate ingestions arriving within the same second. Delta codec because values are monotonically increasing. |
| `qty_total` | `Decimal(18,8) CODEC(ZSTD(1))` | Downgraded from Decimal(25,15). Fits in Decimal64 (8 bytes) instead of Decimal128 (16 bytes). 10 integer digits + 8 decimal places covers any billing quantity. Decimal not Float64 because billing math must be exact. |
| `unique_hash` | `String DEFAULT '' CODEC(ZSTD(1))` | Only populated for COUNT_UNIQUE meters. Empty string for all others compresses to near-zero under ZSTD. |
| `source` | `LowCardinality(String) DEFAULT ''` | Needed for analytics API filter/group-by. Low cardinality values (API, SDK, webhook, etc.). DEFAULT '' instead of Nullable — avoids 1 byte/row null bitmap overhead. |
| `properties` | `String DEFAULT '' CODEC(ZSTD(3))` | JSON blob for analytics filter/group-by on arbitrary keys. ZSTD(3) not ZSTD(1) — 15-25% better compression on JSON, worth the marginal CPU on the fattest column. Columnar storage means billing queries never touch it. |

### Columns Removed from Original

| Column | Why Removed |
|---|---|
| `event_name` | Redundant after event→meter resolution. Not in any query pattern. Join to raw events on `id` if needed. |

---

## ORDER BY Rationale

```
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)
PRIMARY KEY (tenant_id, environment_id, external_customer_id, meter_id, timestamp)
```

**Position 1-2: tenant_id, environment_id** — Always in WHERE clause. Narrows sparse index immediately.

**Position 3: external_customer_id** — Dominant read pattern is single-customer billing queries. Placing customer before timestamp means the index jumps directly to the customer's data block instead of scanning all customers within a time range. Daily partitions already handle coarse time pruning, so timestamp at position 3 (original schema) was doing redundant work.

**Position 4: meter_id** — Within a customer, all meters are grouped contiguously. Enables efficient `meter_id IN (...)` for batching SUM/COUNT meters into single queries. Also optimal for `GROUP BY meter_id`.

**Position 5: timestamp** — Within each customer+meter block, data is time-sorted. Time range scans become sequential reads.

**Position 6: id** — Only in ORDER BY, excluded from PRIMARY KEY. Ensures row uniqueness for RMT dedup without bloating the sparse index in RAM.

---

## Partitioning

`PARTITION BY toYYYYMMDD(timestamp)` — daily partitions.

- ~30-100M rows per partition at current scale — manageable merge sizes
- Duplicate events share the same timestamp → same partition → RMT dedup works correctly (RMT only deduplicates within partitions)
- Month-long queries touch ~30 partitions, parallelized by ClickHouse
- Monthly partitions would be 2-3B rows → merges become too expensive for 16 vCPU box
- Granular TTL/DROP PARTITION for data lifecycle management

---

## Query Patterns

### Pattern 1: Billing usage (hot path — 500K RPM)
```sql
SELECT meter_id, sum(qty_total)
FROM flexprice.meter_usage FINAL
WHERE tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
  AND external_customer_id = {customer_id:String}
  AND timestamp >= {start:DateTime}
  AND timestamp < {end:DateTime}
GROUP BY meter_id
SETTINGS do_not_merge_across_partitions_select_final = 1
```

### Pattern 2: Batch SUM/COUNT meters in single query
```sql
SELECT meter_id, sum(qty_total)
FROM flexprice.meter_usage FINAL
WHERE tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
  AND external_customer_id = {customer_id:String}
  AND meter_id IN ({meter_ids:Array(String)})
  AND timestamp >= {start:DateTime}
  AND timestamp < {end:DateTime}
GROUP BY meter_id
SETTINGS do_not_merge_across_partitions_select_final = 1
```

### Pattern 3: Backfill reconciliation (hourly job)
```sql
-- Find event IDs ingested in the last hour for comparison with events table
SELECT id
FROM flexprice.meter_usage
WHERE ingested_at >= now() - INTERVAL 2 HOUR
  AND ingested_at < now() - INTERVAL 1 HOUR
  AND tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
-- idx_ingested_at minmax skip index accelerates this
```

### Pattern 4: COUNT_UNIQUE aggregation
```sql
SELECT meter_id, uniqExact(unique_hash)
FROM flexprice.meter_usage FINAL
WHERE tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
  AND external_customer_id = {customer_id:String}
  AND meter_id = {meter_id:String}
  AND timestamp >= {start:DateTime}
  AND timestamp < {end:DateTime}
  AND unique_hash != ''
GROUP BY meter_id
SETTINGS do_not_merge_across_partitions_select_final = 1
```

### Pattern 5: Cross-customer tenant-level (10K queries/day)
```sql
SELECT external_customer_id, sum(qty_total)
FROM flexprice.meter_usage FINAL
WHERE tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
  AND meter_id = {meter_id:String}
  AND timestamp >= {start:DateTime}
  AND timestamp < {end:DateTime}
GROUP BY external_customer_id
SETTINGS do_not_merge_across_partitions_select_final = 1
```

If this becomes a bottleneck, add projection:
```sql
ALTER TABLE flexprice.meter_usage
    ADD PROJECTION proj_tenant_meter
    (
        SELECT * ORDER BY tenant_id, environment_id, meter_id, timestamp, external_customer_id, id
    );
```

### Pattern 6: Analytics with properties filter/group-by (100K RPM)
```sql
-- Filter by property
SELECT meter_id, sum(qty_total)
FROM flexprice.meter_usage FINAL
WHERE tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
  AND external_customer_id = {customer_id:String}
  AND timestamp >= {start:DateTime}
  AND timestamp < {end:DateTime}
  AND JSONExtractString(properties, 'model_name') = {value:String}
GROUP BY meter_id
SETTINGS do_not_merge_across_partitions_select_final = 1

-- Group by property
SELECT JSONExtractString(properties, 'model_name') AS model, sum(qty_total)
FROM flexprice.meter_usage FINAL
WHERE tenant_id = {tenant_id:String}
  AND environment_id = {environment_id:String}
  AND external_customer_id = {customer_id:String}
  AND meter_id = {meter_id:String}
  AND timestamp >= {start:DateTime}
  AND timestamp < {end:DateTime}
GROUP BY model
SETTINGS do_not_merge_across_partitions_select_final = 1
```

---

## Server-Level Configuration (16 vCPU / 64 GB RAM)

### Merge pool — `/etc/clickhouse-server/config.d/merge_pool.xml`
```xml
<clickhouse>
    <background_pool_size>4</background_pool_size>
    <background_merges_mutations_concurrency_ratio>1</background_merges_mutations_concurrency_ratio>
    <background_move_pool_size>2</background_move_pool_size>
</clickhouse>
```

### Query limits — `/etc/clickhouse-server/users.d/query_limits.xml`
```xml
<clickhouse>
    <profiles>
        <default>
            <max_threads>8</max_threads>
            <do_not_merge_across_partitions_select_final>1</do_not_merge_across_partitions_select_final>
            <max_memory_usage>8589934592</max_memory_usage>
            <max_server_memory_usage_to_ram_ratio>0.6</max_server_memory_usage_to_ram_ratio>
        </default>
    </profiles>
</clickhouse>
```

### CPU budget (16 cores)

| Function | Threads | Notes |
|---|---|---|
| Background merges | 4 | Steady-state, throttled by part size cap |
| Query execution | 8 | Per-query max, concurrent queries share |
| Insert handling | 2-3 | Parsing, part creation |
| OS / internals | 1-2 | Filesystem, network, scheduler |

---

## Critical Application-Side Requirements

1. **Batch inserts: 10K-50K rows per INSERT.** At 1M RPM with 10K rows/batch = ~100 parts/minute (manageable). Individual row inserts = 1M parts/minute (box dies).

2. **Properties optimization:** Send `''` (empty string) for customers not on analytics plans. ZSTD compresses empty strings to near-zero. This is the single biggest storage lever.

3. **Monitor merge health:**
```sql
SELECT
    partition,
    count() AS active_parts,
    sum(rows) AS total_rows,
    formatReadableSize(sum(bytes_on_disk)) AS disk_size
FROM system.parts
WHERE table = 'meter_usage' AND active
GROUP BY partition
ORDER BY partition DESC
LIMIT 10;
```
If you see hundreds of parts per partition, merges are falling behind.

---

## Storage Estimate

| Scenario | Per Row (uncompressed) | Per Row (compressed) | 1B rows/month |
|---|---|---|---|
| Properties on 30% of rows (avg 200B) | ~140 bytes | ~14 bytes | ~14 GB/month |
| Properties on 100% of rows (avg 200B) | ~280 bytes | ~28 bytes | ~28 GB/month |
| Properties empty for all (future state) | ~80 bytes | ~8 bytes | ~8 GB/month |

Original schema estimate: 60-100 GB/month. The optimized schema is 4-7x smaller.
