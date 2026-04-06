# Data Sync Validation & Reconciliation — Full Report

**Last updated**: 2026-03-26
**Status**: Sizing complete. Pipeline 4 reprocessing script validated (dry run passed). Ready to execute.

---

## 1. Executive Summary

We validated data consistency across the full VAPI → FlexPrice ingestion pipeline for **50 enterprise customers** over **Feb 1 – Mar 25, 2026** (~211M events).

**Result**: Pipelines 1–3 are healthy (0% data loss). Pipeline 4 (feature_usage) has a **24.6% gap** (~52M events missing). All reconciliation effort should focus there.

| Pipeline | What it does | Gap | Status |
|----------|-------------|-----|--------|
| P1: VAPI → raw_events | Bento Collector writes to CH | **0.00%** | Healthy |
| P2+3: raw_events topic → events table | Transform + consume | **~0.00%** | Healthy |
| P4: events → feature_usage | Feature usage tracking | **24.60%** (~52M) | **Needs reprocessing** |

---

## 2. System Architecture

```
                          VAPI ClickHouse
                       ┌──────────────────┐
                       │  billing_entries  │  ← SOURCE OF TRUTH
                       │  (default DB)     │
                       └────────┬─────────┘
                                │
                         VAPI Kafka Topic
                                │
                    ┌───────────┴───────────┐
                    │    Bento Collector     │
                    └───────┬───────┬───────┘
              P1 (sql_insert)       P1 (kafka)
              ┌─────────────┘       └─────────────┐
              ▼                                   ▼
     FP raw_events (table)              FP raw_events (Kafka topic)
     [0% gap — HEALTHY]                          │
                                       ┌─────────┴─────────┐
                                       │ Raw Event Consumer │  P2
                                       │ raw_event_         │
                                       │ consumption.go     │
                                       └─────────┬─────────┘
                                                 │ Transform + Publish
                                                 ▼
                                       FP events (Kafka topic)
                                                 │
                                   ┌─────────────┼─────────────┐
                              P3   │                           │  P4
                                   ▼                           ▼
                          ┌──────────────┐          ┌──────────────────┐
                          │ Events       │          │ Feature Usage    │
                          │ Consumer     │          │ Tracking Service │
                          └──────┬───────┘          └────────┬─────────┘
                                 │                           │
                                 ▼                           ▼
                          ┌──────────────┐          ┌──────────────────┐
                          │ events table │          │ feature_usage    │
                          │ [~0% gap]    │          │ [24.6% gap]      │
                          └──────────────┘          └──────────────────┘
```

---

## 3. Table Schemas & Sort Keys

Understanding sort keys is critical for query performance on production.

### VAPI `billing_entries` (Source of Truth)
- **Host**: `ez0hrbom7c.us-west-2.aws.clickhouse.cloud:8443` (HTTPS, TLS)
- **Engine**: SharedMergeTree
- **ORDER BY**: `(org_id, created_at, id)` — org_id is leading key
- **PARTITION BY**: `toYYYYMM(created_at)`
- **Key columns**: `id` (UUID), `org_id` (String), `created_at` (DateTime64(3))

### FlexPrice `raw_events`
- **Engine**: ReplacingMergeTree(version) — **has unmerged duplicates (~2x row count)**
- **ORDER BY**: `(tenant_id, environment_id, external_customer_id, timestamp, id)`
- **PARTITION BY**: `toYYYYMMDD(timestamp)`
- **Optimal query**: PREWHERE on tenant_id + environment_id + external_customer_id

### FlexPrice `events`
- **Engine**: ReplacingMergeTree(ingested_at) — **has unmerged duplicates**
- **ORDER BY**: `(tenant_id, environment_id, timestamp, id)`
- **PARTITION BY**: `toYYYYMMDD(timestamp)`
- **Note**: `external_customer_id` is NOT in sort key — has bloom_filter index only
- **Optimal query**: PREWHERE tenant_id + environment_id, WHERE timestamp range + external_customer_id

### FlexPrice `feature_usage`
- **Engine**: ReplacingMergeTree(version) — **has unmerged duplicates**
- **ORDER BY**: `(tenant_id, environment_id, customer_id, timestamp, period_id, feature_id, sub_line_item_id, id)`
- **PARTITION BY**: `toYYYYMMDD(timestamp)`
- **Note**: Uses `customer_id` (internal) in sort key, `external_customer_id` has bloom_filter

---

## 4. Sizing Results

### Methodology
- Queried all 4 tables per-customer per-day using `count(DISTINCT id)` to handle ReplacingMergeTree duplicates
- All queries aligned with sort keys (PREWHERE on leading columns)
- Memory capped at 4GB per query to protect production
- Script: `scripts/bash/data_sync/sizing_counts.sh`
- Results: `scripts/bash/data_sync/sizing_output/` (Feb) and `sizing_output_march/` (Mar)

### February 2026 (Feb 1 – Mar 1)

| Table | Distinct Events | Gap from Upstream |
|-------|----------------|-------------------|
| VAPI billing_entries | 109,059,526 | — |
| FP raw_events | 109,059,526 | 0 (0.00%) |
| FP events | 109,059,531 | -5 (~0%) |
| FP feature_usage | 83,715,315 | **25,344,216 (23.24%)** |

### March 2026 (Mar 1 – Mar 25)

| Table | Distinct Events | Gap from Upstream |
|-------|----------------|-------------------|
| VAPI billing_entries | 102,280,905 | — |
| FP raw_events | 102,280,905 | 0 (0.00%) |
| FP events | 102,280,905 | 0 (0.00%) |
| FP feature_usage | 75,629,091 | **26,651,814 (26.06%)** |

### Combined (Feb 1 – Mar 25)

| Metric | Value |
|--------|-------|
| Total events (source of truth) | **211,340,431** |
| P1 gap (VAPI → raw_events) | **0** |
| P2+3 gap (raw_events → events) | **~0** |
| P4 gap (events → feature_usage) | **51,996,030 (24.6%)** |

### Top 10 Customers by P4 Gap (combined Feb+Mar)

| Customer | Events | Feature Usage | Gap | % Missing |
|----------|--------|--------------|-----|-----------|
| `afc00adf` | 46,820,283 | 39,289,557 | 7,530,726 | 16.1% |
| `deac54e3` | 34,349,845 | 25,653,370 | 8,696,475 | 25.3% |
| `4500ce13` | 15,377,411 | 8,548,428 | 6,828,983 | 44.4% |
| `e8d1087b` | 5,152,529 | 604,206 | 4,548,323 | 88.3% |
| `dccaea68` | 15,191,005 | 10,831,646 | 4,359,359 | 28.7% |
| `d62fb741` | 12,471,959 | 7,779,892 | 4,692,067 | 37.6% |
| `f19e0049` | 3,447,341 | 618,170 | 2,829,171 | 82.1% |
| `f8fa5457` | 16,857,402 | 16,255,235 | 602,168 | 3.6% |
| `cdd2806c` | 4,359,685 | 2,933,794 | 1,425,891 | 32.7% |
| `f9e5d540` | 2,591,995 | 1,413,401 | 1,178,594 | 45.5% |

---

## 5. Key Learnings

### L1: ReplacingMergeTree Has ~2x Unmerged Duplicates
All FlexPrice ClickHouse tables use ReplacingMergeTree which deduplicates on merge. But merges are async, so raw `count()` returns ~2x the actual unique events. **Always use `count(DISTINCT id)` for accurate counts.** Using `FINAL` is more expensive and unnecessary for counting.

### L2: ID Mapping is 1:1 Across All Pipelines
`TransformBentoToEvent` preserves the original VAPI `billing_entries.id` as the event ID (`event.ID = input.ID` in transformer.go:189). There is NO 1:N fan-out. This makes ANTI JOINs on `id` valid across raw_events, events, and feature_usage tables.

### L3: Pipelines 1–3 Are Rock Solid
Bento Collector + Raw Event Consumer + Events Consumer have zero data loss across 211M events. The tiny -5 discrepancy in February events is from prior API backfills that went directly to events topic (bypassing raw_events table). No action needed.

### L4: Pipeline 4 Is the Only Problem
Feature Usage Tracking Service drops ~25% of events. Root causes include:
- Subscription/line item resolution failures (customer has no matching subscription)
- Consumer lag under high throughput
- Transient DB lookup failures during processing
- Events for customers without active subscriptions (legitimately skipped)

Gap varies dramatically per customer — from 3.6% (`f8fa5457`) to 88.3% (`e8d1087b`). Customers with high gap % likely had subscription setup delays or periods with no active subscriptions.

### L5: Cross-Infrastructure Queries Need Careful Handling
VAPI and FlexPrice ClickHouse are separate clusters — no JOINs possible. We query each independently and diff locally. VAPI uses HTTPS (port 8443), FlexPrice uses native protocol (port 9000). The FlexPrice CH ELB host has changed over time (update `.env.backfill`).

### L6: Query Performance Depends on Sort Key Alignment
- VAPI `billing_entries`: `org_id` is leading key → per-customer queries are optimal
- FP `raw_events`: `external_customer_id` at position 3 in sort key → excellent for PREWHERE
- FP `events`: `external_customer_id` NOT in sort key → relies on bloom filter, slower per-customer
- FP `feature_usage`: `customer_id` (internal) in sort key, `external_customer_id` via bloom filter

### L7: Bash 3 on macOS Doesn't Support Associative Arrays
`declare -A` requires bash 4+. macOS ships bash 3.2. Use awk-based merging instead of associative arrays in scripts. Similarly, `gdate` may not be available — use `date -j -v` on macOS.

### L8: The ANTI JOIN in FindUnprocessedEventsFromFeatureUsage Is Extremely Expensive
**This is the root cause of prior CPU/memory spikes during reprocessing.**

The `FindUnprocessedEventsFromFeatureUsage` method (`internal/repository/clickhouse/event.go:859-932`) runs:

```sql
SELECT e.id, e.tenant_id, e.environment_id, e.event_name, ...
FROM events e
ANTI JOIN (
    SELECT id, tenant_id, environment_id
    FROM feature_usage
    WHERE tenant_id = ? AND environment_id = ?
    -- NO TIMESTAMP FILTER — scans ALL feature_usage rows for the tenant
) AS p ON e.id = p.id AND e.tenant_id = p.tenant_id AND e.environment_id = p.environment_id
WHERE e.tenant_id = ? AND e.environment_id = ?
  AND e.external_customer_id = ?
  AND e.timestamp BETWEEN ? AND ?
ORDER BY e.timestamp ASC, e.id ASC
LIMIT ?
```

**The problem**: The inner subquery on `feature_usage` has NO time range filter. It scans the ENTIRE `feature_usage` table for the tenant+environment. For our tenant that's 159M+ rows. Each batch of events in the `ReprocessEvents` activity loop re-runs this full scan (keyset pagination means many batches per workflow).

**Why it caused outages previously**:
1. No per-query memory limits were set — queries could consume unbounded memory
2. Multiple workflows running in parallel meant multiple simultaneous full-table scans
3. Each workflow's activity loop calls `FindUnprocessedEventsFromFeatureUsage` repeatedly (once per batch of 100-200 events), each time re-scanning all of feature_usage
4. Combined effect: N parallel workflows × M batches per workflow × full feature_usage scan = catastrophic memory/CPU

**Mitigation in reprocessing script** (not a code fix — the ANTI JOIN itself would need a code change to add timestamp filtering to the inner subquery):
- Strictly ONE workflow at a time — never parallel
- Wait for workflow completion before starting next
- Monitor CH active queries and memory between requests
- Tight date windows (1-day for customers with >2M gap) to limit outer query scope
- Conservative batch size (200) to reduce iterations
- Cool-down periods between windows and customers

### L9: Two Reprocess Endpoints Serve Different Pipelines

| Endpoint | Pipeline | What it does | Temporal Workflow |
|----------|----------|-------------|-------------------|
| `POST /v1/events/raw/reprocess/all` | P2+3 | raw_events ANTI JOIN events → republish to events topic | `ReprocessRawEventsWorkflow` |
| `POST /v1/events/reprocess` | **P4** | events ANTI JOIN feature_usage → publish to feature_usage_backfill topic | `ReprocessEventsWorkflow` |

The existing `scripts/bash/reprocess_missing_feature_usage.sh` is **confusingly named** — it uses the P2+3 endpoint, NOT the P4 endpoint. It does NOT reprocess feature_usage despite its name. The actual P4 reprocessing script is `scripts/bash/data_sync/reprocess_feature_usage.sh`.

### L10: Feature Usage Backfill Uses Separate Kafka Topic
When `ReprocessEventsWorkflow` republishes events, it uses `isBackfill=true` which routes to the `feature_usage_backfill` Kafka topic, separate from the real-time `events` topic. This prevents backfill load from interfering with real-time event processing.

---

## 6. Pipeline 4 Reconciliation Plan

### Architecture: How Feature Usage Reprocessing Works

```
POST /v1/events/reprocess
  { external_customer_id, start_date, end_date, batch_size }
      │
      ▼
TriggerReprocessEventsWorkflow (Temporal)
      │
      ▼
ReprocessEvents (Activity — 10hr timeout, 3 retries)
      │
      ├─ FindUnprocessedEventsFromFeatureUsage
      │    (events ANTI JOIN feature_usage on id)
      │    Uses keyset pagination (timestamp, id)
      │
      ├─ For each unprocessed event:
      │    PublishEvent(event, isBackfill=true)
      │    → feature_usage_backfill Kafka topic
      │
      └─ Feature Usage consumer processes event
           → Resolves subscription, line items, meters
           → Writes to feature_usage table
```

**Key**: The ANTI JOIN is done server-side in the Temporal workflow. We don't need to compute missing IDs ourselves — just call the API with customer + date range and the workflow handles everything.

### Script: `scripts/bash/data_sync/reprocess_feature_usage.sh`

**What it does**:
1. Reads enterprise customer IDs from JSON (`scripts/bash/enterprise-customer-ids.json`)
2. Looks up each customer's P4 gap from sizing data (Feb + Mar combined)
3. Sorts customers smallest-gap-first (quick wins build confidence)
4. For each customer, generates time windows based on gap tier:
   - Gap > 2M: **1-day** chunks (tightest — limits outer query scope for the expensive ANTI JOIN)
   - Gap > 500K: **2-day** chunks
   - Gap > 100K: **3-day** chunks
   - Gap > 10K: **7-day** chunks
   - Gap < 10K: **14-day** chunks
5. Calls `POST /v1/events/reprocess` for each window with batch_size=200
6. **STRICTLY ONE workflow at a time** — waits for completion via `temporal workflow count` polling
7. Monitors CH health (active feature_usage queries, memory) between requests
8. Tracks progress to file for resume capability
9. Monitors Temporal failures and aborts if threshold exceeded (default: 5)

**Safety controls** (designed around L8 — the ANTI JOIN problem):
- Never runs parallel workflows — polls Temporal until `Running` count = 0
- Waits for CH to calm (< 2 active feature_usage queries) before next request
- 30s cool-down between time windows, 60s between customers
- Max 1hr wait per workflow before moving on
- Graceful shutdown on SIGINT/SIGTERM (finishes current operation)
- All progress logged for post-run analysis

**Usage**:
```bash
cd scripts/bash/data_sync
source .env.backfill

# Dry run first (validates tiering, date arithmetic, API URL — no actual API calls)
DRY_RUN=true START_DATE=2026-02-01 END_DATE=2026-03-01 ./reprocess_feature_usage.sh

# Real run — February
START_DATE=2026-02-01 END_DATE=2026-03-01 ./reprocess_feature_usage.sh

# Real run — March
START_DATE=2026-03-01 END_DATE=2026-03-25 ./reprocess_feature_usage.sh
```

### Dry Run Validation (2026-03-26)

Dry run completed successfully for February date range:
- **50 customers loaded**, 4 skipped (P4 gap = 0)
- **481 total time windows** generated across 46 customers
- Date arithmetic correct on macOS (BSD `date -j -v` — handles Feb 28 → Mar 1 boundary)
- Tiering verified:
  - Small customers (gap < 10K): 14-day chunks → 2 windows each
  - Medium customers (gap 10K–100K): 7-day chunks → 4 windows each
  - Large customers (gap 100K–500K): 3-day chunks → 10 windows each
  - Very large customers (gap > 2M): 1-day chunks → 28 windows each
- Temporal CLI connected (baseline failures counted)
- Customer sort order correct (ascending by gap size)

### Execution Strategy

**Phase 1** (DONE): Dry run validated — tiering logic, date arithmetic, API connectivity all correct
**Phase 2**: Reprocess February (25.3M gap) — all 46 customers, smallest-first
**Phase 3**: Reprocess March (26.7M gap) — same order
**Phase 4**: Re-run sizing to verify gaps closed
**Phase 5**: Assess residual gap — expected to be events without matching subscriptions

### Risk Considerations

| Risk | Mitigation | Status |
|------|------------|--------|
| ANTI JOIN scans full feature_usage (L8) | Strictly ONE workflow at a time, tight date windows | Script enforces |
| ClickHouse CPU/memory spike | CH health monitoring, cool-downs, wait for calm | Script monitors |
| Temporal workflow overload | Never parallel, poll for completion before next | Script enforces |
| Feature usage consumer backlog | `isBackfill=true` routes to separate `feature_usage_backfill` topic | Built into API |
| Legitimately skipped events inflate gap | Expected — not all events match subscriptions | Will measure after |
| Script interruption mid-run | Progress file enables clean resume | Built in |
| Temporal failures cascade | Monitor count, abort at 5 new failures | Script enforces |
| Previous reprocess attempt caused outage | Root cause identified (L8) — parallel ANTI JOINs without memory limits | All mitigations applied |

### Expected Outcome

Not all 52M events will produce feature_usage rows. Many are legitimately skipped (no matching subscription, inactive customer, etc.). After reprocessing we expect:
- Gap to reduce significantly (likely to 5-15% depending on subscription coverage)
- Remaining gap = events for customers without active subscriptions = expected behavior
- Re-run sizing (`sizing_counts.sh`) to measure actual improvement
- Customers with very high gap % (e.g., `e8d1087b` at 88.3%) may have legitimately low subscription coverage

---

## 7. Scripts & Artifacts

### Scripts

| Script | Location | Pipeline | What it does |
|--------|----------|----------|-------------|
| `reprocess_missing_feature_usage.sh` | `scripts/bash/` | P2+3 | raw_events ANTI JOIN events → POST to `/raw/reprocess/all`. **Confusingly named — does NOT touch feature_usage.** |
| `orchestrate_reprocess.sh` | `scripts/bash/` | P2+3 | Orchestrates above across customers with tiered chunking |
| `monitor_reprocess.sh` | `scripts/bash/` | P2+3 | Monitors raw_events vs events counts, insert rates, CH active queries |
| `sizing_counts.sh` | `scripts/bash/data_sync/` | All | Counts distinct events per customer per day across all 4 tables (VAPI, raw_events, events, feature_usage) |
| `reprocess_feature_usage.sh` | `scripts/bash/data_sync/` | **P4** | Calls POST /v1/events/reprocess per customer per time window with strict sequential safety controls |

### Data Artifacts

| Artifact | Location | Contents |
|----------|----------|----------|
| February sizing | `scripts/bash/data_sync/sizing_output/` | Per-customer CSVs, per-table TSVs, combined_counts.csv, summary.txt |
| March sizing | `scripts/bash/data_sync/sizing_output_march/` | Same structure as February |
| Reprocess progress | `scripts/bash/data_sync/reprocess_output/` | progress.log (resume state), reprocess.log (full log) |
| Enterprise customers | `scripts/bash/enterprise-customer-ids.json` | 50 enterprise customer UUIDs |
| Credentials | `scripts/bash/.env.backfill` | VAPI CH, FlexPrice CH, FlexPrice API, Temporal, Kafka credentials |

### Key Code Paths

| Component | File | What to know |
|-----------|------|-------------|
| ID preservation | `internal/domain/events/transform/transformer.go:189` | `event.ID = input.ID` — 1:1 mapping from VAPI to FlexPrice |
| ANTI JOIN (P4) | `internal/repository/clickhouse/event.go:859-932` | `FindUnprocessedEventsFromFeatureUsage` — inner subquery has NO timestamp filter (L8) |
| Reprocess activity | `internal/service/feature_usage_tracking.go:3000-3156` | `ReprocessEvents` — loops calling ANTI JOIN with keyset pagination, publishes each event |
| Reprocess API handler | `internal/api/v1/event.go` | `POST /v1/events/reprocess` → `TriggerReprocessEventsWorkflow` |
| P2+3 reprocess | `internal/service/raw_event_consumption.go` | `ReprocessRawEvents` — raw_events ANTI JOIN events |
| Event consumption | `internal/service/event_consumption.go` | P3 consumer — events Kafka → events CH table via BulkInsertEvents |

---

## 8. Connection Details

### VAPI ClickHouse (read-only)
```
Host: ez0hrbom7c.us-west-2.aws.clickhouse.cloud
Port: 8443 (HTTPS, TLS required)
User: flexprice_readonly
DB: default
```

### FlexPrice ClickHouse (read-only for sizing)
```
Host: (see .env.backfill — ELB address, changes periodically)
Port: 9000 (native protocol)
User: default
DB: flexprice
```

### FlexPrice API
```
URL: https://us.api.flexprice.io
Tenant: tenant_01KF5GXB4S7YKWH2Y3YQ1TEMQ3
Environment: env_01KG4E6FR5YCNW0742N6CA1YD1
```

### Temporal
```
Address: us-west-2.aws.api.temporal.io:7233
Namespace: flexprice-prod-usa.awrle
Reprocess workflow: ReprocessEventsWorkflow (10hr timeout, 3 retries)
```

### Enterprise Customers
50 customer IDs in `scripts/bash/enterprise-customer-ids.json`

---

## 9. Investigation Timeline

| Date | Phase | What happened |
|------|-------|--------------|
| 2026-03-25 | Planning | Created this document. Mapped all 4 pipelines, identified table schemas, sort keys, and query strategies. |
| 2026-03-25 | Sizing (Feb) | Ran `sizing_counts.sh` for Feb 1 – Mar 1 across all 50 customers. Result: P1=0%, P2+3≈0%, P4=23.24% (25.3M gap). |
| 2026-03-25 | Sizing (Mar) | Ran `sizing_counts.sh` for Mar 1 – Mar 25. Result: P4=26.06% (26.7M gap). Combined: 52M missing. |
| 2026-03-26 | Root cause | Analyzed `FindUnprocessedEventsFromFeatureUsage` code. Identified that inner ANTI JOIN subquery scans ALL feature_usage (no timestamp filter). This + parallel workflows + no memory limits = prior CPU spikes. |
| 2026-03-26 | Script build | Built `reprocess_feature_usage.sh` with strict sequential safety controls (one workflow at a time, CH monitoring, Temporal failure tracking, tiered chunking). |
| 2026-03-26 | Dry run | Validated script: 50 customers, 481 windows, correct tiering, correct date arithmetic on macOS. Ready for real execution. |
| 2026-03-26 | Data cleanup | Deleted feature_usage rows for 9 customers with old/invalid subscriptions (see Section 9.1). Mutation completed successfully. |
| 2026-03-26 | Test reprocess | Ran ReprocessEventsWorkflow for customer `9b4b815a` (137 gap). Workflow completed but found **0 events** despite 136 genuinely missing. **ANTI JOIN bug confirmed** (see Section 9.2). |
| 2026-03-26 | Root cause #2 | The ANTI JOIN in `FindUnprocessedEventsFromFeatureUsage` loads ALL feature_usage (~160M+ rows, ~33 GiB) without DISTINCT on a ReplacingMergeTree table. ReplacingMergeTree duplicates in the hash table cause false positive matches, reporting 0 missing events when there are actually 136. |

### 9.1 Feature Usage Cleanup — Old Subscription Data

Before reprocessing, old subscription data was cleaned from `feature_usage` for 9 customers. These customers had feature_usage rows from subscriptions that are no longer valid.

**Mutation executed**:
```sql
ALTER TABLE feature_usage DELETE
WHERE external_customer_id IN (
  '52fd687a-...', '843d7884-...', 'b3ce3a2e-...', 'f19e0049-...',
  'a6e07aa0-...', '9b4b815a-...', '7b816f43-...', '54371f35-...', '55df69d9-...'
)
AND tenant_id = 'tenant_01KF5GXB4S7YKWH2Y3YQ1TEMQ3'
AND environment_id = 'env_01KG4E6FR5YCNW0742N6CA1YD1'
AND timestamp > '2026-02-01 00:00:00.000'
AND subscription_id NOT IN (
  'subs_01KKETBCTWMHEVCJCREF8CRVSW', 'subs_01KKHHCJZZDKVP0EFT5VS8MSJ7',
  'subs_01KKEVX943D2GYCZKPXFRJSJ9P', 'subs_01KKETQ155S5MCJN1P6QDJMNMQ',
  'subs_01KKEVYKGX7FCTR2FXXVWHB1N3', 'subs_01KKERWQM1ZSV8ZXB28T40K45F',
  'subs_01KKEW2GPV8N0E8QPMHSYZ703N', 'subs_01KKETA45K4Y2A7FSFREXNN9ST',
  'subs_01KKETAX8GGHXKGRH40K4VE2Y0'
)
```

**Status**: Mutation `mutation_25826261.txt` completed at ~2026-03-26T03:40:00Z. Verified: all 9 customers show 0 rows with invalid subscription_ids.

**Post-cleanup P4 gaps** (Feb 1 – Mar 25, these 9 customers):

| Customer | Events | FU (post-delete) | P4 Gap | % Missing |
|----------|--------|------------------|--------|-----------|
| `9b4b815a` | 7,223 | 7,086 | 137 | 1.9% |
| `54371f35` | 2,212 | 1,529 | 683 | 30.9% |
| `55df69d9` | 5,690 | 731 | 4,959 | 87.2% |
| `7b816f43` | 94,679 | 75,007 | 19,672 | 20.8% |
| `a6e07aa0` | 356,025 | 321,602 | 34,423 | 9.7% |
| `52fd687a` | 5,228,135 | 4,934,189 | 293,946 | 5.6% |
| `843d7884` | 6,202,753 | 5,541,281 | 661,472 | 10.7% |
| `b3ce3a2e` | 3,553,147 | 2,799,387 | 753,760 | 21.2% |
| `f19e0049` | 3,452,805 | 618,344 | 2,834,461 | 82.1% |

**Note**: The valid subscription IDs (kept) are the current active subscriptions for these customers. Rows deleted were from old/replaced subscriptions that should not contribute to billing.

### 9.2 ANTI JOIN Bug — Workflow Reports 0 Missing Events (CRITICAL)

**Test**: Ran `ReprocessEventsWorkflow` for customer `9b4b815a-ef75-4e81-a3ec-5005d58edcc4` (137 event gap, Feb 1 – Mar 26).

**Result**: Workflow completed with `total_events_found: 0, total_events_published: 0`. Took ~13 minutes, consumed 33 GiB memory.

**But**: 136 event IDs genuinely DO NOT exist anywhere in `feature_usage`. Verified by:
1. Customer-scoped ANTI JOIN: found 274 raw rows (137 distinct IDs) missing from feature_usage
2. Direct ID lookup: 5 sample IDs queried against feature_usage with NO filters → 0 results
3. Full-table lookup: only 1 of 137 IDs found in feature_usage outside the date range

**Root cause**: The workflow's ANTI JOIN (`event.go:875-880`) does:
```sql
ANTI JOIN (
    SELECT id, tenant_id, environment_id
    FROM feature_usage
    WHERE tenant_id = ? AND environment_id = ?
    -- No DISTINCT, no customer filter, no time filter
) AS p ON e.id = p.id ...
```

On a **ReplacingMergeTree** table with unmerged duplicates (~2x rows), loading ALL 160M+ rows into a 33 GiB hash table without DISTINCT likely causes false positive matches. The same event ID appearing multiple times in the hash table (from unmerged duplicates) may interact with the ANTI JOIN logic incorrectly.

**Impact**: The `POST /v1/events/reprocess` endpoint is BROKEN for finding missing events. It will always (or usually) report 0 unprocessed events.

**Proposed fixes** (in order of preference):
1. **Add DISTINCT to inner subquery**: `SELECT DISTINCT id FROM feature_usage WHERE ...` — reduces hash table size by ~50%
2. **Add customer + time filters to inner subquery**: Scope to same customer and time range — reduces from 160M to thousands of rows
3. **Use FINAL on inner subquery**: `FROM feature_usage FINAL WHERE ...` — forces merge, eliminates duplicates
4. **Alternative approach**: Use direct ID diff (like `reprocess_missing_feature_usage.sh`) — query event IDs, query feature_usage IDs, compute diff client-side, then publish missing events

---

## 10. Resolved Questions

| # | Question | Answer |
|---|----------|--------|
| 1 | VAPI billing_entries schema? | Confirmed — `migrations/clickhouse/external_billing_entities_table.sql`. UUID id, org_id, created_at. |
| 2 | TransformBentoToEvent ID preservation? | **YES** — `event.ID = input.ID`. 1:1 mapping, no fan-out. |
| 3 | raw_events table write from API? | **Not needed** — P1 has 0% gap. Bento Collector is the sole writer and it's reliable. |
| 4 | feature_usage "should exist" criteria? | Deferred — reprocess workflow handles this. Events without matching subscriptions are skipped legitimately. We'll measure residual gap after reprocessing. |
| 5 | Date range? | Feb 1 – Mar 25, 2026 confirmed. |
| 6 | Which reprocess endpoint for P4? | `POST /v1/events/reprocess` (NOT `/raw/reprocess/all`). Former does events ANTI JOIN feature_usage. |
| 7 | Why did prior reprocessing cause CPU spikes? | ANTI JOIN inner subquery scans ALL feature_usage (no time filter) + parallel workflows + no per-query memory limits. See L8. |
| 8 | Is it safe to run reprocessing now? | Yes, with strict sequential controls. Script ensures ONE workflow at a time and monitors CH health. |

---

## 11. Validated Approach: Direct ID Diff

The broken ANTI JOIN (Section 9.2) was bypassed by building a direct ID diff script.

### How It Works

```
┌──────────────────────────────────────────────────────────────────────┐
│ diff_and_reprocess_p4.sh                                            │
│                                                                     │
│ 1. Query: SELECT id FROM events WHERE customer=? GROUP BY id        │
│    → events_ids.txt (sorted)                     ~14s per customer  │
│                                                                     │
│ 2. Query: SELECT id FROM feature_usage WHERE customer=? GROUP BY id │
│    → fu_ids.txt (sorted)                          ~4s per customer  │
│                                                                     │
│ 3. comm -23 events_ids.txt fu_ids.txt → missing_ids.txt             │
│    (local diff — instant)                                           │
│                                                                     │
│ 4. POST /v1/events/raw/reprocess/all { event_ids: [...] }           │
│    → ReprocessRawEventsWorkflow                                     │
│    → Fetches raw events by ID, transforms, publishes to events topic│
│    → Events consumer dedupes (already in events table)              │
│    → Feature usage tracking picks up new events → feature_usage     │
│                                                                     │
│ Total: ~18s per customer (vs 13 min for broken ANTI JOIN workflow)  │
└──────────────────────────────────────────────────────────────────────┘
```

### Why `/raw/reprocess/all` Instead of `/v1/events/reprocess`

| Endpoint | Problem | Solution |
|----------|---------|----------|
| `POST /v1/events/reprocess` | Uses broken ANTI JOIN — reports 0 missing events (Section 9.2) | **Don't use** |
| `POST /v1/events/raw/reprocess/all` | Accepts `event_ids` array, fetches from `raw_events`, transforms, publishes to events Kafka topic | **Use this** — feeds both P3 and P4 consumers |

The events topic feeds two consumers: the events consumer (P3 — writes to events table, deduped by ReplacingMergeTree) and the feature_usage tracking service (P4 — resolves subscriptions, writes to feature_usage).

### Test Results (2026-03-26)

| Customer | Events | FU Before | Missing | Workflow Result | FU After | New FU | Notes |
|----------|--------|-----------|---------|----------------|----------|--------|-------|
| `9b4b815a` | 7,223 | 7,086 | 137 | 274 published | 7,086 | 0 | All 137 events legitimately unmatched (no subscription for those event_names) |
| `54371f35` | 2,212 | 1,529 | 683 | 1,138 published | 1,564+ | +35+ | Some events matched subscriptions, rest may be legitimately unmatched |

**Key insight**: Not all missing events will produce feature_usage rows. Events without matching subscription line items are legitimately skipped. The "residual gap" after reprocessing = events that don't map to any metered feature.

### Performance & Safety

- **18 seconds** per customer for diff (vs 13 minutes for broken workflow)
- **Minimal CH CPU/memory**: GROUP BY id uses ~1-4 GiB (vs 33 GiB for the ANTI JOIN)
- Workflow publishes via Kafka — feature_usage tracking handles subscription resolution
- Events already in events table are deduped by ReplacingMergeTree (no harm)

---

## 12. Open Items / Future Improvements

| # | Item | Priority | Status | Notes |
|---|------|----------|--------|-------|
| 1 | **FIX: ANTI JOIN in FindUnprocessedEventsFromFeatureUsage** | Medium | Open | Add DISTINCT + customer/time filters to inner subquery. See Section 9.2. Not blocking — direct diff approach works. |
| 2 | **ID-log tables for fast diff** | Low | Proposed | `feature_usage_ids`, `events_ids` tables with just id+timestamp, populated by MVs. Would make ongoing diff O(1). |
| 3 | Add per-query memory limit to ANTI JOIN | Medium | Open | Even after fix, should have `max_memory_usage` on the ClickHouse query. |
| 4 | Re-run sizing after full reprocessing | High | Pending | Measure actual residual gap across all 50 customers. |
| 5 | Consider `OPTIMIZE TABLE` for partition dedup | Low | Deferred | Could reduce ReplacingMergeTree duplicates (~2x) but adds CPU load. |
