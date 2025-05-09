# Flexprice • Real-time Event → Billing Ledger Architecture  
_Last updated: 2025-05-09_

---

## 0 . Objectives

| Goal                              | Constraint / KPI                       |
|-----------------------------------|----------------------------------------|
| **Sub-100 ms balance lookup**     | 95ᵗʰ ≤ 100 ms at 100 k events · customer⁻¹ |
| **Zero-loss financial accuracy**  | Duplicate processing ⇒ *₀* over-/under-charge |
| **Audit trail for 7 years**       | Append-only ledger, reversible via `sign` |
| **Easy Day-1 ops**                | Single-shard ClickHouse, no Redis |

---

## 1 . Pipeline at a glance
## 1 .  High-level flow

```
           +---------------+         +------------------+
SDK  --->  | Kafka topic   |         | Postgres wallet  |
           | raw_events    |         | ledger           |
           +--+---------+--+         +---------+--------+
              |         |                        ^
              |         |                        |
              |    +----v-----+  INSERT          |
              |    | Go worker|------------------+
              |    +----+-----+  (Watermill)
              |         |
              |     INSERT … async
              v         |
     +-------------------------+
     | ClickHouse:             |
     |  events_processed       |
     |  mv_usage_period_totals |
     |  agg_usage_period_totals|
     +-------------------------+
```

* **Worker (Go + Watermill)** resolves customer → subscription → meter, dedups, applies free units / tier math, and inserts **one row per charged line-item** into `events_processed`.
* A materialised view (`mv_usage_period_totals`) streams rows into `agg_usage_period_totals`, giving running totals per **(subscription × line-item × period)**.
* Balance API = *wallet credits* – *cost* from the aggregate table.
  No raw-event scans on the hot path.

---

## 2 . Physical schema

### 2 . 1 Ledger table `events_processed`

```sql
CREATE TABLE events_processed (
    id                    String,
    tenant_id             String,
    environment_id        String,
    external_customer_id  String,
    event_name            String,

    customer_id           String,
    subscription_id       String,
    sub_line_item_id      String,
    price_id              String,
    meter_id              String,
    feature_id            String,
    period_id             UInt64,      -- epoch-ms of period start

    timestamp             DateTime64(3),
    ingested_at           DateTime64(3),
    processed_at          DateTime64(3) DEFAULT now64(3),

    source                Nullable(String),
    properties            String CODEC(ZSTD),

    unique_hash           Nullable(String),
    qty_total             Decimal(25,15),
    qty_billable          Decimal(25,15),
    qty_free_applied      Decimal(25,15),
    tier_snapshot         Decimal(25,15),
    unit_cost             Decimal(25,15),
    cost                  Decimal(25,15),
    currency              LowCardinality(String),

    version               UInt64  DEFAULT toUnixTimestamp64Milli(now64()),
    sign                  Int8    DEFAULT 1,
    final_lag_ms          UInt32  MATERIALIZED
                              datediff('millisecond', timestamp, processed_at)
)
ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(timestamp)
PRIMARY KEY (tenant_id, environment_id, customer_id)
ORDER BY (
    tenant_id, environment_id, customer_id,
    period_id, feature_id, timestamp, sub_line_item_id, id)
SETTINGS index_granularity = 8192;
```

**Secondary indexes**

```sql
ALTER TABLE events_processed
ADD INDEX bf_subscription subscription_id TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_feature      feature_id      TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_source       source          TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX bf_unique_hash  unique_hash     TYPE bloom_filter(0.01) GRANULARITY 128,
ADD INDEX set_event_name  event_name      TYPE set(0)             GRANULARITY 128;
```

### 2 . 2 Aggregate table `agg_usage_period_totals`

```sql
CREATE TABLE agg_usage_period_totals (
    tenant_id        String,
    environment_id   String,
    customer_id      String,
    subscription_id  String,
    period_id        UInt64,
    feature_id       String,
    sub_line_item_id String,

    qty_state  AggregateFunction(sum,  Decimal(25,15)),
    free_state AggregateFunction(sum,  Decimal(25,15)),
    cost_state AggregateFunction(sum,  Decimal(25,15))
)
ENGINE = AggregatingMergeTree
PARTITION BY (tenant_id, environment_id, customer_id, period_id)
ORDER BY (tenant_id, environment_id, customer_id,
          period_id, subscription_id, feature_id, sub_line_item_id);
```

### 2 . 3 Materialised view

```sql
CREATE MATERIALIZED VIEW mv_usage_period_totals
TO agg_usage_period_totals AS
SELECT
    tenant_id,
    environment_id,
    customer_id,
    subscription_id,
    period_id,
    feature_id,
    sub_line_item_id,

    sumState(assumeNotNull(qty_billable))     AS qty_state,
    sumState(assumeNotNull(qty_free_applied)) AS free_state,
    sumState(assumeNotNull(cost))             AS cost_state
FROM events_processed
GROUP BY
    tenant_id, environment_id, customer_id,
    subscription_id, period_id, feature_id, sub_line_item_id;
```

---

## 3 . Worker algorithm

```text
For each Kafka event:
1. Resolve  customer → subscription → active meters.
2. FOR meter IN matched_meters:
      build unique_hash
      if hash exists in ledger THEN qty_billable = 0
      SELECT running totals FROM agg_usage_period_totals
      apply free units + tier math
      INSERT row (version = prev_version+1 if reprocessing)
3. Commit Kafka offset after successful insert + wallet debit.
```

*Exactly-once* is guaranteed by `ReplacingMergeTree(version)`.

---

## 4 . Query playbook

| Purpose                       | SQL                                                                                                                                                                    |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Dedup test**                | `SELECT 1 FROM events_processed WHERE subscription_id=:sid AND meter_id=:mid AND period_id=:pid AND unique_hash=:hash LIMIT 1`                                         |
| **Free / billed to-date**     | `SELECT sumMerge(qty_state) AS qty, sumMerge(free_state) AS free FROM agg_usage_period_totals WHERE sub_line_item_id=:lid AND period_id=:pid`                          |
| **Cost to-date (wallet)**     | `SELECT sumMerge(cost_state) AS cost FROM agg_usage_period_totals WHERE tenant_id=:t AND environment_id=:e AND customer_id=:c AND subscription_id=:s AND period_id=:p` |
| **Period roll-up by feature** | See §5.2                                                                                                                                                               |
| **Live dashboard (last 3 h)** | See §5.3                                                                                                                                                               |

#### 5.1 Free counter & tier snapshot (worker)

```sql
SELECT
    sumMerge(qty_state)  AS qty_billable,
    sumMerge(free_state) AS qty_free
FROM   agg_usage_period_totals
WHERE  sub_line_item_id = :lid
  AND  period_id        = :pid;
```

#### 5.2 Invoice totals per feature

```sql
SELECT feature_id,
       sumMerge(qty_state)  AS qty,
       sumMerge(free_state) AS free,
       sumMerge(cost_state) AS cost
FROM   agg_usage_period_totals
WHERE  tenant_id       = :t
  AND  environment_id  = :e
  AND  customer_id     = :c
  AND  subscription_id = :s
  AND  period_id       = :p
GROUP BY feature_id;
```

#### 5.3 Developer analytics (3 h window)

```sql
SELECT source,
       feature_id,
       sum(cost)         AS cost,
       sum(qty_billable) AS usage
FROM   events_processed
WHERE  tenant_id      = :t
  AND  environment_id = :e
  AND  customer_id    = :c
  AND  timestamp     >= now64(3) - INTERVAL 3 HOUR
GROUP BY source, feature_id
ORDER BY cost DESC
LIMIT 100;
```

---

## 6 . Decimal precision choice

* Payload samples: `27.04277491569519` (17 sig. fig)
* Price samples:  `0.003474410688` (12 dp)

`Decimal(25,15)` keeps **±9 × 10⁹** units at 15 dp – enough headroom for TB-seconds and nano-dollar prices.
All maths (`unit_cost × qty`) happen in the worker, then rounded to 15 dp before insert.

---

## 7 . Ops run-book

| Job                     | Command                                            | SLA / alert                    |
| ----------------------- | -------------------------------------------------- | ------------------------------ |
| **Compaction health**   | `SELECT sum(parts_to_remove) FROM system.replicas` | >0 for 30 min → warn           |
| **Merge lag**           | `SELECT max(queue_size) FROM system.mutations`     | >100 MB → warn                 |
| **Duplicate-row audit** | `SYSTEM DEDUP PARTS events_processed` nightly      | RowsRemoved >0 → investigate   |
| **Disk free**           | 80 % full → scale volume                           |                                |
| **Balance latency**     | p95 > 70 ms                                        | scale CH or add Redis counters |

---

## 8 . Edge-case remediation

| Scenario                       | Fix                                                                           |
| ------------------------------ | ----------------------------------------------------------------------------- |
| **Replay same Kafka event**    | Worker sees existing `version` → inserts row with `version+1` (*idempotent*). |
| **Customer changes dedup key** | `INSERT … SELECT` back-fill rows with new `unique_hash`, `version+1`.         |
| **Mid-period new price**       | New `sub_line_item_id`; invoice engine may merge or show two lines.           |
| **Over-charge discovered**     | Append reversal row (`sign = -1`, neg qty & cost, `version+1`).               |
| **Need new metric tomorrow**   | Emit into `extra_agg` JSON; later add real column & back-fill.                |

---

## 9 . Future levers (no schema change)

| Need                        | Drop-in change                                                                                   |
| --------------------------- | ------------------------------------------------------------------------------------------------ |
| **< 10 ms free counter**    | Add Redis counters; worker swaps SELECT for `GET`.                                               |
| **Shard ClickHouse**        | Create Distributed + local shards; MV definition stays identical.                                |
| **Per-customer encryption** | Encrypt `properties` at worker; decrypt only in support job.                                     |
| **GDPR data purge**         | `ALTER TABLE events_processed DELETE WHERE external_customer_id = …`; aggregates auto-recompute. |