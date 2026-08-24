# Prepaid Subscriptions — Charge-level `advance_usage` + Wallet-Settled Snapshots

- **Ticket:** TBD
- **Date:** 2026-08-05 (revised 2026-08-21 with snapshot-based design)
- **Author:** Ankit Malik
- **Status:** **Proposed** — v1

---

## Revision notes (2026-08-21)

The original draft introduced a `plans.plan_type` enum with a `PREPAID` value that gated a large restriction bundle. That framing has been dropped. The current design:

- **Plan-level enum removed.** No `plan_type`.
- **Charge-level primitive.** A new `usage_charge_mode` field on `price` distinguishes `arrear_usage` (default, today's behavior) from `advance_usage`.
- **Prepaid-settled subscription is derived, not stored.** A subscription is prepaid-settled iff *every* usage line item on it has `usage_charge_mode = advance_usage`. Mixed subscriptions are not permitted in v1.
- **Snapshot pipeline** replaces the "daily debit with cursor" model. Hourly buckets, +2h late-arrival buffer, Temporal-scheduled wallet debits, per-window audit rows in postgres.
- **Fast reads** via ClickHouse hourly rollup (reconciler-populated, not materialized view) + redis for the current open window.
- **Entitlements** stay on the existing EG-eval workflow; only its data source moves from raw `meter_usage` queries to `rollup + redis`. Legacy entitlements on prepaid subs are **synthesized as grants** rather than blocked.
- **Backdated events** allowed up to a per-plan config-driven cap (default 7 days).

---

## 1. Summary

`price.usage_charge_mode` values:

| Value | Meaning |
|---|---|
| `arrear_usage` (default) | Today's behavior. Usage bills at cycle boundary via `GenerateInvoiceForBillingPeriod`. |
| `advance_usage` | Usage debits wallet at each hourly snapshot. No invoice generated for these line items. |

A **prepaid-settled subscription** = every usage line item has `usage_charge_mode = advance_usage`. Fixed line items follow existing `invoice_cadence = ADVANCE`/`ARREAR` semantics unchanged. A subscription with *any* `arrear_usage` line item is a normal arrear subscription and takes today's invoice path.

**Why charge-level, not plan-level:** the settlement cadence is a property of *how a specific price is priced*, not the plan as a whole. Charge-level lets a plan later mix line items freely if we relax the all-advance restriction. Plan-level was a strictly less flexible way to say the same thing.

**Non-goals:**
- No new pricing math. `advance_usage` uses `FLAT_FEE` only.
- No new wallet primitives. Uses `wallets` + `wallet_transactions` as-is.
- No changes to arrear behavior. Any subscription with an arrear usage line item is untouched by this design.

---

## 2. Concepts

| Term | Meaning |
|---|---|
| **`usage_charge_mode`** | Field on `price`. Values `arrear_usage` (default) or `advance_usage`. Immutable once any subscription uses the price. |
| **Prepaid-settled subscription** | Subscription where all usage line items have `usage_charge_mode = advance_usage`. Derived, not stored. |
| **Snapshot window** | A time bucket (default 1 hour) over which usage is measured, wallet-debited, and persisted as one row. First/last window of a period clamp to period boundaries; interior windows are exactly 1h. |
| **Snapshot workflow** | Temporal activity that runs `window_end + 2h` per active window per prepaid-settled sub. Debits wallet, writes snapshot row, deletes redis current-window key. |
| **Reconciler** | Temporal schedule (hourly per environment) that reads `meter_usage FINAL` for its `ingested_at` slice and writes the deduplicated hourly rollup to ClickHouse. |
| **Hourly rollup** | ClickHouse table `subscription_meter_hourly_rollup` (`ReplacingMergeTree`), keyed on `(tenant, env, ext_customer, meter, hour)`. Populated only by the reconciler. |
| **Current window** | The open, not-yet-snapshotted window from `last_snapshot.window_end` to now. Usage lives in redis. |
| **`max_event_lateness`** | Per-plan cap on `now − event.timestamp` at ingest. Default 7 days, min 1h, max 30d. Config-driven. |
| **EG** | EntitlementGrant. Existing concept unchanged in this design. |
| **EG-eval** | Existing 5-min debounced Temporal workflow that refreshes `grant.usage` and stamps `quota_crossed_at`. Data source changes for prepaid subs. |

---

## 3. Restrictions

### 3.1 On `advance_usage` charges

| Constraint | Value | Enforcement |
|---|---|---|
| Price `billing_model` | `FLAT_FEE` only | `PriceService.Create`/`Update` validator |
| Meter aggregation | `SUM`, `COUNT`, `SUM_WITH_MULTIPLIER` (linear only) | `PlanService.AddPrice` |
| Meter bucketed aggregation | Not allowed | `PlanService.AddPrice` |
| Pricing tiers | Not allowed (already implied by FLAT_FEE) | Price validator |
| Commitments (min-commit, overage true-up) | Not allowed | `PlanService.AddPrice`, `PlanService.SetCommitment` |
| Price update | Only future-dated `start_date`, at least 30 min in the future | `PriceService.Update` |
| `usage_charge_mode` mutation | Immutable once price is referenced by any subscription line item | `PriceService.Update` |

### 3.2 On prepaid-settled subscriptions

Applied whenever a subscription's line items make it all-advance:

| Surface | Rule | Enforcement |
|---|---|---|
| Mixing arrear + advance usage line items | Not allowed in v1 | `SubscriptionService.AddLineItem`/`Update` |
| Discounts | Percentage only; no fixed-value | `SubscriptionService` / `Coupon` validator |
| Addons | Not allowed in v1 (may relax once addon prices are gated the same way) | `AddonService.AttachToSubscription` |
| Event modification / deletion / reprocessing | Not allowed on events belonging to a prepaid-settled sub (they are settled and immutable) | Reprocess / admin-delete paths |
| Backdated price edits | Not allowed | `PriceService.Update` — reject if any prepaid-settled sub references the price |
| Late-arrival ingestion | Allowed within `max_event_lateness`; triggers snapshot recompute | `EventService.Ingest` |
| Invoice generation | `GenerateInvoiceForBillingPeriod` returns `(nil, nil)` | `BillingService.GenerateInvoiceForBillingPeriod` |
| Credit notes | Not allowed. Refunds via `WalletService.CreditWallet` (reason `PREPAID_REFUND`). | Credit note API |
| Subscription trial | Snapshot workflow still runs; wallet debit skipped during trial window | `SnapshotActivity` |
| Subscription pause | Snapshot workflow halts; current-window redis key TTLs out | `SnapshotActivity` |
| Dunning | Not applicable. Reuse existing wallet low-balance/zero-balance alerts. | `WalletAlertWorkflow` (unchanged) |

**Meter aggregation note.** Meters are per-tenant/environment and shared across plans. Restriction is enforced at `PlanService.AddPrice`: attaching an `advance_usage` price whose meter uses a non-linear aggregation is rejected.

---

## 4. Data model

### 4.1 `price.usage_charge_mode`

```sql
ALTER TABLE prices
  ADD COLUMN usage_charge_mode varchar(20) NOT NULL DEFAULT 'arrear_usage';
```

Values enforced by application layer against `types.UsageChargeMode` (`arrear_usage`, `advance_usage`).

Ent schema: `field.String("usage_charge_mode").Default("arrear_usage")`. Regenerate via `make generate-ent`, migration via `make generate-migration`.

### 4.2 `subscription_usage_snapshots` (postgres)

```sql
CREATE TABLE subscription_usage_snapshots (
    id                      varchar(50) PRIMARY KEY,
    tenant_id               varchar NOT NULL,
    environment_id          varchar NOT NULL,
    subscription_id         varchar(50) NOT NULL,
    window_start            timestamptz NOT NULL,
    window_end              timestamptz NOT NULL,
    raw_usage_at_start      jsonb NOT NULL,     -- {meter_id → decimal string}, cumulative from period_start
    raw_usage_at_end        jsonb NOT NULL,     -- {meter_id → decimal string}, cumulative from period_start
    raw_usage_in_window     jsonb NOT NULL,     -- {meter_id → decimal string}, delta
    billable_in_window      numeric(25,15) NOT NULL,  -- total across all meters, post-grants, post-discount
    debit_wallet_txn_id     varchar(50),        -- 1:1; nullable while status='recomputing'
    status                  varchar(20) NOT NULL, -- active | archived | recomputing
    created_at              timestamptz NOT NULL DEFAULT now(),
    computed_at             timestamptz,
    archived_at             timestamptz
) PARTITION BY RANGE (window_start);

-- Monthly partitions created ahead of time.
CREATE INDEX idx_snap_sub_window ON subscription_usage_snapshots(subscription_id, window_start);
CREATE INDEX idx_snap_tenant_env ON subscription_usage_snapshots(tenant_id, environment_id);
CREATE UNIQUE INDEX idx_snap_sub_window_active
  ON subscription_usage_snapshots(subscription_id, window_start)
  WHERE status = 'active';
```

**One row per subscription per *active* window** (window with any usage). Lazy: no row for quiet subs/quiet windows. Cumulative counters give O(1) "usage as of time T" lookups for audit tooling and backdated-event recompute verification.

### 4.3 `subscription_meter_hourly_rollup` (ClickHouse)

```sql
CREATE TABLE subscription_meter_hourly_rollup (
    tenant_id             String,
    environment_id        String,
    external_customer_id  String,
    meter_id              String,
    hour                  DateTime,
    usage                 Decimal(38, 15),
    version               DateTime
) ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(hour)
ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, hour);
```

Populated **only by the reconciler** (no MV). Reads use `SELECT ... FROM ... FINAL` (rollup rows are ~1/1000 of `meter_usage` rows, so `FINAL` is cheap here).

### 4.4 Redis current-window fast path

```
Key:   flexprice:sub_usage:{env_id}:{subscription_id}
Type:  hash
Field: {meter_id}__{window_start_unix}__{window_end_unix}
Value: decimal string
TTL:   86400s (24h) on the key — safety net; snapshot workflow HDELs explicitly
```

Written after `meter_usage` insert succeeds (in `EventConsumptionService`). Never a source of truth; on redis miss/eviction, fall back to `meter_usage FINAL` for the current window and re-warm.

Only prepaid-settled subs use this path — event ingestion for arrear subs is unchanged.

---

## 5. Snapshot pipeline

### 5.1 Event ingest (prepaid-settled subs only)

1. Insert into ClickHouse `meter_usage` (unchanged).
2. **Trust check** on `redis:sub_usage:{env}:{sub}` — see §5.4. If stale/missing, the ingest still succeeds via the delta-key rebuild-safe write path (§5.4); an async rebuild is triggered.
3. `HINCRBYFLOAT` into redis current-window hash for `(sub_id, meter_id, current_window)`.
4. **Inline grant-crossing detection** — for each grant applicable to `(sub, meter)`:
   ```
   projected = grant.usage_cached + HGET redis current window for meter
   if projected >= grant.quota
      AND HEXISTS redis:grant_crossing:{grant_id} is 0
      AND postgres grant.quota_crossed_at is nil:
     SET redis:grant_crossing:{grant_id} = { ts: now(), detected_after_snapshot_end: last_snapshot.window_end }
     signal Temporal PersistCrossingActivity
   ```
   `grant.usage_cached` is postgres `grant.usage` refreshed on some interval (e.g. on each EG-eval pass and each snapshot). Slight staleness is safe — the `>=` check only ever *fires* the crossing detection; it can't miss one that already happened (that path is caught by EG-eval or snapshot fallback).
5. **Maintain running billable cache** (§6.5) — decide billability at event time `T = event.timestamp` using cached grant state:
   ```
   billable = false
   for each grant applicable to (sub, meter):
     if grant.quota_crossed_at set
        AND grant.quota_crossed_at <= T < grant.valid_to:
       billable = true; break
   if billable:
     amount = qty × price(meter)
     HINCRBYFLOAT redis:pending_billable:{env}:{sub}   value  by amount
     // (rebuild-safe write path — see §5.4)
   ```
   Billability is a per-event boolean based on parallel/additive grant state at `event.timestamp`. No range queries. For amount-measure grants, an event that straddles the crossing boundary (grant.usage goes from `quota − 3` to `quota + 5` in one event) is classified fully-billable or fully-not by this check — bounded error of ≤1 event's cost per crossing per grant, self-corrected at the next snapshot.
6. Schedule Temporal workflow `SnapshotWorkflow(sub_id, window_start)` at `window_end + 2h` (Temporal dedup key: `(sub_id, window_start)`).

The +2h buffer absorbs late-arriving events from network/queue lag. Events arriving *after* the buffer are handled by the [backdated-event flow](#7-backdated-event-handling).

### 5.2 Snapshot workflow (per active window)

Fires at `window_end + 2h`. Steps:

1. Acquire redis lock on `subscription_id` (short TTL, blocks other workflows/backdated recompute for this sub).
2. Query CH `meter_usage FINAL` for events with `timestamp ∈ [window_start, window_end)` AND `ingested_at < now`. Aggregate per meter → `raw_usage_in_window`.
3. **Synchronous EG-eval trigger** — one invocation that refreshes all applicable grants for this subscription over `[grant.valid_from, window_end)` per grant. Blocks until every `grant.usage` and `quota_crossed_at` reflects the window's usage. This *also* reconciles any redis crossing that didn't make it to postgres (persistence-worker gap, redis eviction) by reading `meter_usage FINAL` for the window and re-deriving the crossing point. (This resolves the ordering rough edge from earlier design discussion — snapshot workflow doesn't rely on the 5-min debounced EG-eval to have caught up.)
4. **Fold grants via the existing billing code path.** For each meter, call `adjustMeterUsageGrants` / `mergedOverage` (`internal/ee/service/billing_meter_usage_grants.go`) with:
   - Data source for the window: `meter_usage FINAL` (bounded to 1h). No approximation.
   - Grant state: freshly refreshed in step 3 (postgres).
   
   Handles additive vs parallel, quantity vs amount measure, and `first_usage` allocation identically to invoice billing. This is the *same code* used by `GenerateInvoiceForBillingPeriod` today — no divergence between snapshot debit and what an equivalent invoice would compute.
5. Compute total `billable_in_window = Σ_meter (result.amount)`.
6. Debit wallet via `WalletService.DebitWallet`:
   - `reason = PREPAID_SETTLEMENT`
   - `reference_type = SUBSCRIPTION`, `reference_id = subscription_id`
   - `idempotency_key = (subscription_id, window_start)`
7. Insert `subscription_usage_snapshots` row with `status = active`, `debit_wallet_txn_id = <returned>`, cumulative counters computed from previous row + delta.
8. **Re-anchor the running billable cache** to ground truth for the slice already accrued after this window ended. Also HDEL the raw current-window per-meter hash. See §5.4 for the atomic sequence — critical to avoid losing events that arrived during the snapshot workflow itself:
   ```
   post_window_billable = adjustMeterUsageGrants(...) for [window_end, snapshot_run_time)
                          // one bounded CH pass (~2h of meter_usage max)
   atomic_reset_pending_billable_cache(sub, post_window_billable, _reset_at = snapshot_run_time)
   HDEL redis:sub_usage:{env}:{sub}  (per-meter raw hash for the just-closed window fields)
   ```
9. Release lock.

**Negative wallet balance is allowed.** The debit always succeeds; existing wallet low-balance / zero-balance alerts (already used by `PROMOTIONAL` and `PAID` wallets) are the customer-facing signal. Tenants are expected to wire these up as part of onboarding (see §8).

### 5.3 Reconciler

Temporal schedule, hourly per environment. Query (exactly as agreed):

```sql
SELECT tenant_id, environment_id, external_customer_id, meter_id,
       toStartOfHour(timestamp) AS hour,
       SUM(qty_total),
       now() AS version
FROM meter_usage FINAL
WHERE ingested_at >= '{last_reconciled_max_ingested_at}'
  AND ingested_at <  '{now}'
GROUP BY tenant_id, environment_id, external_customer_id, meter_id, hour;
```

Writes into `subscription_meter_hourly_rollup` (`ReplacingMergeTree` collapses older versions on merge). Tracks `last_reconciled_max_ingested_at` per environment in postgres to avoid gaps or re-scans.

Groups by `toStartOfHour(timestamp)` (event time), *filters by* `ingested_at` (wall-clock ingest time). A single reconciler pass may touch multiple hours if late-arriving events landed with older event timestamps.

### 5.4 Redis trust, rebuild, and fail-closed reads

Three redis key families need trust and recovery: raw per-meter current-window usage, grant crossing markers, and the running billable cache. All follow the same principle: **CH is the source of truth; redis is a cache with a rebuild protocol; reads fail-closed on missing/untrusted keys**.

**Key families and trust markers:**

```
redis:sub_usage:{env}:{sub}                      # per-meter raw current-window usage (for EG-eval, inline crossing)
  _window_start                                  = unix ts of last snapshot HDEL; must equal last_snapshot.window_end
  {meter}__{window_start}__{window_end}          = decimal usage

redis:grant_crossing:{grant_id}                  # inline crossing timestamp (for persistence worker)
  ts                                             = sub-second unix ts when crossing detected
  detected_after_snapshot                        = last_snapshot.window_end at detection time

redis:pending_billable:{env}:{sub}               # running billable cache (for balance API)
  value                                          = decimal accumulated since last _reset_at
  _reset_at                                      = last snapshot re-anchor time (or rebuild time)
  _state                                         = healthy | rebuilding
  _rebuild_generation                            = monotonic counter, bumped every rebuild
```

**Trust rule (applied on every read):** the marker `_reset_at` / `_window_start` / `detected_after_snapshot` must be `>= last_snapshot.window_end`. Any older marker is stale.

#### Read behavior — fail-closed

Real-time wallet balance API and entitlement usage API **return an error** (HTTP 503 with `code = cache_rebuilding`) when the corresponding redis key is missing or untrusted, AND simultaneously trigger the rebuild activity. Rationale: it is better for the customer-facing API to say "recomputing, retry in a few seconds" than to return a stale or reconstructed-but-lossy value. Rebuilds finish in milliseconds to seconds; retry succeeds.

Non-realtime paths (EG-eval, snapshot workflow, backdated recompute) do **not** fail-closed — they either read from CH directly (snapshot, backdated recompute) or wait for the rebuild to complete before their next tick (EG-eval on next 5-min cadence).

#### Rebuild protocol — no-data-loss with concurrent ingest

Rebuild is a per-subscription single-writer activity. All three key families use the same shape:

**Ingest during rebuild** uses a temporary delta key so no event is lost:

```
# Lua script (atomic per event write)
if HGET pending_billable_state == "rebuilding":
    HINCRBYFLOAT redis:pending_billable_delta:{env}:{sub}:{generation}  value  by amount
else if HGET pending_billable value exists:
    HINCRBYFLOAT redis:pending_billable:{env}:{sub}  value  by amount
else:
    # key missing — bootstrap into rebuild mode
    SET redis:pending_billable_state:{env}:{sub} = "rebuilding" NX EX 300
    signal RebuildPendingBillableActivity(sub)
    # write goes to delta so it's captured
    HINCRBYFLOAT redis:pending_billable_delta:{env}:{sub}:{new_generation}  value  by amount
```

**Rebuild activity** (idempotent, single-writer via redis lock):

```
1. SETNX redis:rebuild_lock:{env}:{sub} = 1 EX 300; if not acquired → another rebuild is running, exit.
2. Ensure state is "rebuilding" and generation G is fresh (bump if needed). All writes from now on go to delta:{G}.
3. Pick T_high_water = now() − safety_margin (e.g. 30s) to guarantee CH visibility of any consumer-processed event with ingested_at ≤ T_high_water.
4. Compute base from CH:
      base = adjustMeterUsageGrants over [last_snapshot.window_end, T_high_water)
             for pending_billable
      raw  = SUM(qty_total) per meter for the current window
             for sub_usage
      crossing = walk meter_usage FINAL for grant_id
             for grant_crossing (see EG-eval crossing re-derivation, §5.2 step 3)
5. Atomically (Lua):
      delta = GET pending_billable_delta:{G} or 0
      DEL pending_billable_delta:{G}
      HSET pending_billable value = base + delta,
                            _reset_at = T_high_water,
                            _state = "healthy",
                            _rebuild_generation = G
6. DEL redis:rebuild_lock:{env}:{sub}.
```

Correctness invariant: **every event contributes to `value` exactly once**. Events with `ingested_at ≤ T_high_water` are in `base` (via CH). Events with `ingested_at > T_high_water` that arrive during rebuild go into `delta:{G}`. The atomic `base + delta` merge captures both without duplication.

**Small residual race:** an event whose consumer-processing occurs during rebuild but whose CH `ingested_at` is ≤ T_high_water (i.e., consumer is >30s slower than CH insert visibility) can be counted in both `base` and `delta` → over-count. This is bounded to a handful of events per rebuild and is fully corrected at the next snapshot re-anchor (§5.2 step 8).

**Safety-margin tuning caveat:** the 30s default is illustrative, not magic. It should be tuned per-env against observed p99 consumer lag between `meter_usage` insert and consumer processing:

| If observed p99 consumer lag is… | Set safety margin to… | Effect |
|---|---|---|
| < 10s (healthy) | 30s (default) | Rare-to-none double-count on rebuild |
| 30–60s (mildly lagging) | 90s | Slightly longer rebuild lock window, no double-count |
| > 60s (chronically lagging) | Investigate consumer scaling first | Over-tuned safety margin masks a real backpressure problem |

Under-tuned → over-count during rebuild (bounded, self-corrects at snapshot). Over-tuned → longer rebuild lock window (harmless as long as no read is fail-closed for that whole window; it usually isn't). Expose as an env-level config so ops can bump without a code change.

**Persistence worker for grant crossings** (Temporal schedule, per env, every 5 min):

```
for each redis:grant_crossing:{grant_id}:
  trust-check the crossing (marker >= last_snapshot.window_end)
  if trusted AND postgres grant.quota_crossed_at is nil:
    UPDATE entitlement_grants SET quota_crossed_at = crossing.ts
      WHERE id = grant_id AND quota_crossed_at IS NULL
```

Once postgres holds `quota_crossed_at`, it's immutable. Reads short-circuit to postgres, redis key can be lazily deleted or TTL'd.

**Failure modes summary:**

| Failure | Impact | Recovery |
|---|---|---|
| Balance API cache evicted | 503 on read; ingests write to delta | Rebuild activity, milliseconds to seconds |
| Grant crossing key evicted | Missed inline detection until next 5-min EG-eval or snapshot | Auto-recovered by EG-eval / snapshot re-derivation |
| Raw per-meter hash evicted | EG-eval and inline crossing miss for current window | Rebuild activity + fallback to `meter_usage FINAL` |
| Consumer down for N min | No writes; cache goes stale by ≤ N min | Snapshot re-anchor at next hour boundary corrects; balance API returns stale-but-not-wrong (since ingests are Kafka-backed and will replay) |
| Snapshot workflow fails mid-run | Snapshot row not written, cache not re-anchored | Temporal retry; on retry, idempotent via `(sub_id, window_start)` |

**Redis eviction policy** should be `noeviction` (fail writes when memory-full) for this workload. `pending_billable` and `grant_crossing` keys carry no TTL. `sub_usage` per-meter hash carries a 24h TTL as a safety net (§4.4). Sizing redis for peak-sub × grants-per-sub is straightforward and small compared to CH; memory pressure is not the expected failure mode.

### 5.5 Cost profile — where CH is touched

| Operation | `meter_usage FINAL` | Rollup query | Redis | Postgres |
|---|---|---|---|---|
| Event ingest (prepaid) | 1 insert | — | 1 `HINCRBYFLOAT` (raw per-meter) + inline crossing check + 1 `HINCRBYFLOAT` (pending_billable) via Lua atomic | — (cached grant + crossing reads only) |
| Rebuild activity (rare, on eviction) | 1 range scan (bounded ~2h) + billing fold | — | rewrite pending_billable + delta merge | — |
| Reconciler (hourly, per env) | 1 range scan by `ingested_at` | 1 batch insert | — | 1 `last_reconciled_max_ingested_at` update |
| EG-eval (5-min debounced, per grant, prepaid) | — | 1 range scan (`FINAL`) | 1 `HGETALL` (current window) | grant.usage update |
| Persistence worker (5-min, per env) | — | — | scan crossing keys | grant.quota_crossed_at UPDATE (only when nil) |
| Snapshot workflow (per active sub per active hour) | 2 range scans (1h window + post-window re-anchor slice) | — | HDEL raw + atomic reset of pending_billable | 1 snapshot row + wallet txn |
| Balance API (prepaid) | — (fail-closed on miss; rebuild is async) | — | 1 `HGET pending_billable` | 1 wallet read |
| `mergedOverage` at invoice time (arrear) | Fringe only (rare mid-hour edges) | 1 per range | — | — |
| Backdated event recompute | 1 per affected hour | — | — | affected snapshot rows |

Balance API is now the leanest hot path: **1 redis HGET + 1 postgres wallet read, no CH touch, no fallback**. When redis is missing, it returns 503 rather than fall back — see §5.4.

**Hot path never hits `meter_usage FINAL`** under normal operation. Only the reconciler (bounded to its `ingested_at` slice), the snapshot workflow (bounded to 1h per active sub), rare `mergedOverage` fringes, and rare trust-check rebuilds touch it. All are scheduled, invoice-time, or exceptional — not per-request.

---

## 6. Entitlements

### 6.1 Additive vs parallel grants (recap)

- **Additive.** Multiple ECs on the same feature sum into one grant on the primary EC. One time window per feature; usage counted once.
- **Parallel.** Each EC gets its own independent grant. Each grant accumulates the **same** usage stream in parallel. `mergedOverage` bills usage inside the union of `[quota_crossed_at, valid_to)` intervals across all quota-crossed grants.

Design correction from an earlier draft: parallel grants do **not** consume in serial priority order. There is no "soonest expiry first" drawdown. Each grant tracks the full usage independently; billing kicks in on any crossing.

### 6.2 EG-eval data source change (prepaid subs only)

`refreshEntitlementGrantUsage` today queries `meter_usage` directly for `[valid_from, min(now, valid_to))`. For prepaid subs, it changes to:

```
historical = rollup_query(sub, meter, [valid_from, current_window_start))
current    = trust_checked_read(redis:sub_usage:{env}:{sub}, meter)   // §5.4
grant.usage = historical + current
```

Cadence stays 5-min debounced. This removes the direct `meter_usage FINAL` dependency from the hot EG-eval path. If the trust check fails, the read falls back to `meter_usage FINAL` for the current window (bounded to 1h of events) and rewarms redis.

For arrear subs, EG-eval keeps the existing `meter_usage` query path.

### 6.3 Legacy entitlements — synthesize grants

Legacy entitlements (`usage_limit` + `usage_reset_period`, no grant config) present on any feature of a subscription that is about to become prepaid-settled are **synthesized as EntitlementGrants**:

- On subscription create / line-item update where `IsPrepaidSettled()` becomes true.
- For each legacy entitlement:
  - Create an `EntitlementGrant`:
    - `quota = usage_limit`
    - `valid_from = current_period_start`
    - `valid_to = current_period_end`
    - `aggregation_mode = additive` (default; multiple legacy ECs on same feature sum)
    - `measure = quantity`
  - Mark the synthesized grants with a metadata flag (`origin = legacy_synthesized`) for observability.
- On subscription period rollover, new grant windows auto-open via existing `EnsureGrants`.

Rationale for synthesis over fail-closed: keeps the customer's existing entitlement setup working without a migration hoop. The synthesis is one-time and idempotent.

### 6.4 Wallet balance API

Balance API is a **single redis read + single postgres read**, or a 503:

```
value = HGET redis:pending_billable:{env}:{sub}  value
if value is nil OR state is not "healthy" OR _reset_at < last_snapshot.window_end:
   return 503 { code: "cache_rebuilding", retry_after: 5s }
   (and trigger the rebuild activity — §5.4)

balance = wallet.current_balance − value − pending_invoices
```

Zero ClickHouse touch on the hot path. Zero interval math. Handles additive/parallel/quantity/amount/first_usage automatically because the classification happens at ingest time (§5.1 step 5) using existing grant-state semantics.

**"Balance matches what a snapshot-right-now would debit"** holds because:
- Each event classified billable at ingest AND persisted to CH via `meter_usage`.
- The snapshot workflow uses the exact same billing code path (`adjustMeterUsageGrants`) against `meter_usage FINAL` — so its `billable_in_window` matches the sum of ingest-time increments (up to the bounded amount-measure straddle error, §5.1 step 5).
- At snapshot, the workflow debits wallet by the exact billable, and re-anchors the cache to ground truth for `[window_end, now)` (§5.2 step 8) — so any accumulated ingest-time error is zeroed out on every snapshot boundary.

### 6.5 Running billable cache (details)

Purpose: give the balance API an O(1) source for "billable amount accrued since the last snapshot debit". Replaces the per-meter union counter from earlier drafts, which couldn't handle overage intervals spanning multiple snapshot windows.

**Key:**

```
redis:pending_billable:{env}:{sub}
  value                = decimal accumulated (units of subscription currency)
  _reset_at            = timestamp of last snapshot re-anchor (or rebuild)
  _state               = healthy | rebuilding
  _rebuild_generation  = monotonic counter, bumped every rebuild
```

**Write path (per event ingest, §5.1 step 5):**
```
billable = check_billability(event, applicable_grants_state)
   // billable iff any grant is crossed with quota_crossed_at <= event.timestamp < valid_to
if billable:
  atomic_ingest_incr(sub, amount = qty × price)
     // Lua script: writes to main key if state=healthy, delta:{G} if rebuilding, etc. — §5.4
```

**Read path (balance API, §6.4):** single `HGET value`; fail-closed on missing / stale marker.

**Snapshot re-anchor (§5.2 step 8):** at the end of each snapshot workflow, after wallet debit, the workflow computes billable for `[window_end, snapshot_run_time)` via the same billing code path against `meter_usage FINAL` and atomically resets the cache to that value with `_reset_at = snapshot_run_time`. This bounds cache staleness to the snapshot cadence (~1h) and self-corrects any inline classification error.

**Rebuild on eviction:** §5.4 rebuild protocol. Uses a versioned `delta:{G}` key to capture writes during rebuild without loss. Base computed from CH via `adjustMeterUsageGrants` for `[last_snapshot.window_end, T_high_water)`. Atomic merge on completion.

**Why per-subscription, not per-meter?** Billable is a **subscription-level** concept — the amount to debit from the wallet is a single decimal, not per-meter. Storing per-meter would force interval-merge math at read time (the failure mode of the previous design). Per-subscription single decimal collapses all parallel-grant complexity into ingest-time boolean logic.

**Amount vs quantity measure:** ingest-time classification is a boolean (billable / not). Cache stores currency-denominated amounts. Amount-measure grants participate identically — the crossing check triggers on `grant.usage >= grant.quota` where `grant.usage` is currency for amount measure. The `qty × price` computation on billable ingest is the same math the snapshot workflow does at higher precision. Bounded straddle error (§5.1 step 5) is the only divergence.

**Cached grant state on the ingest path:** the "check_billability" call reads `grant.quota_crossed_at` and `grant.valid_to` for every applicable grant. Cache these in-memory per subscription in the consumer process with a 30s TTL. On cold start, first event per sub re-reads postgres.

**Invalidation strategy caveat — pub/sub vs TTL-only.** Two viable options for keeping the consumer's grant-state cache fresh; pick one based on how tight the freshness requirement is:

| Option | How | Trade-off |
|---|---|---|
| **A. TTL-only (simpler, default)** | 30s TTL, no active invalidation. First event after TTL re-reads postgres. | Grant state is up to 30s stale after a persistence-worker write. Missed billability decisions for that window are still bounded — the inline crossing check keeps firing, and the snapshot re-anchor corrects at hour boundary. |
| **B. TTL + Redis pub/sub invalidation** | Persistence worker publishes `invalidate:{sub}` on `quota_crossed_at` write; consumers subscribe and drop the cache entry. | Sub-second freshness on crossing detection. Requires consumers to hold an active Redis Pub/Sub connection; adds an extra failure mode (missed invalidation on network blip → same-as-A staleness for one TTL). |

**Recommendation: start with A (TTL-only)**, add B only if the 30s staleness window is measurably hurting billing accuracy for a design partner. The snapshot workflow's synchronous EG-eval trigger (§5.2 step 3) already provides an hour-boundary correctness anchor, which bounds the impact of a stale consumer cache.

### 6.6 EG alerts

For prepaid subs, alerts fire from the synchronous EG-eval trigger inside the snapshot workflow (once per hour per active sub) and continue to fire from the existing 5-min debounced EG-eval. No new alert surface.

---

## 7. Backdated event handling

`max_event_lateness` is per-plan, config-driven (default 7d, min 1h, max 30d).

**Ingest path:**
- Reject if `now − event.timestamp > max_event_lateness`.
- Accept otherwise; if event falls within an already-snapshotted window, trigger the recompute flow below.

**Recompute flow** (triggered when a late-arriving event's `timestamp < some archived snapshot.window_end`):

1. Acquire redis lock on `subscription_id`.
2. Find snapshots with `window_start >= floor_to_hour(event.timestamp)`, order by `window_start` ascending.
3. For each affected snapshot:
   - Set `status = recomputing`.
   - Issue `WalletService.CreditWallet` reversing `debit_wallet_txn_id` (reason `BACKDATE_REVERSAL`, reference back to the snapshot id).
4. Re-run snapshot workflow for each affected window in oldest-first order. This does **not** depend on the reconciler — snapshot workflow reads `meter_usage FINAL` directly:
   - Reads fresh `meter_usage FINAL` for the window (picks up the late event via `ingested_at` in FINAL semantics).
   - Synchronous EG-eval re-derives `grant.usage` from scratch — no manual grant fix-up needed (self-healing since EG-eval sums the whole window each pass).
   - New wallet debit with new idempotency key `(subscription_id, window_start, recompute_generation)` where `recompute_generation` is a monotonic counter on the sub, incremented every time any window is recomputed.
   - Snapshot `status = active`, cumulative counters recalculated.
5. Reconciler picks up the late event on its next hourly run to keep the rollup fresh for other consumers (EG-eval on other subs, `mergedOverage` on arrear invoices). Not blocking on the recompute above.
6. Release lock.

**Bounded cost:** worst case `max_event_lateness / 1h × active_hours_per_sub`. Typically ≤168 windows at the 7d cap, usually far fewer because most subs aren't active every hour.

---

## 8. Onboarding & subscription-create webhook

`advance_usage` shifts the "make sure the customer can pay" contract from FlexPrice (invoices + dunning) to the **tenant's onboarding workflow**. Before a prepaid-settled subscription starts producing settlement debits, the tenant is expected to have:

1. A currency-matched wallet on the customer.
2. Low-balance / zero-balance alerts configured on that wallet.
3. Optionally, `wallet.auto_topup` configured against a payment method.

FlexPrice does not enforce (1)–(3) at subscription-create time — a tenant may deliberately run "post-paid style" prepaid subs that intentionally go negative and reconcile out-of-band. What FlexPrice provides:

- `subscription.prepaid.created` **webhook** carrying `wallet_id`, `wallet_balance`, `wallet_has_low_balance_alert`, `wallet_has_auto_topup`. Tenant onboarding automation subscribes to this.
- `subscription.prepaid.settled` webhook per snapshot with the shape of `invoice.finalized` where possible (window, per-meter usage, billable, wallet_txn_id).
- Docs / migration guide (out of scope for this design).

---

## 9. Invoice pipeline changes

- `BillingService.GenerateInvoiceForBillingPeriod` — first line: if `subscription.IsPrepaidSettled()`, log `info` "skipped: prepaid-settled subscription" and return `(nil, nil)`.
- `SubscriptionCycleWorkflow` — for prepaid-settled subs, skip the invoice-generation activity and instead verify `last_snapshot.window_end >= period_end` (all usage settled in the closed cycle). If not, enqueue a catch-up snapshot activity for the residual window.
- Read-only endpoint `GET /v1/subscriptions/:id/settlement-history` returns the ledger of `subscription_usage_snapshots` rows grouped by period.
- No PDF generation in v1.

**Non-prepaid subs** (any arrear usage line item) are untouched — today's invoice pipeline runs.

---

## 10. `plan_type` removal

The original draft added `plans.plan_type`. It is being **dropped** entirely:

1. `ALTER TABLE plans DROP COLUMN plan_type` — safe: no production rows carry a non-default value (feature-flagged, never enabled).
2. Remove `types.PlanType`.
3. Remove all references from services (few, since this was in-progress work).

Do this last, after nothing depends on it.

---

## 11. Enforcement point reference

| Rule | Layer | Function |
|---|---|---|
| Only `FLAT_FEE` billing model on `advance_usage` prices | Service | `PriceService.Create` / `Update` |
| Only linear meter aggregations on `advance_usage` prices | Service | `PlanService.AddPrice` |
| No commitments on `advance_usage` line items | Service | `PlanService.AddPrice`, `PlanService.SetCommitment` |
| No mixing arrear + advance usage on one subscription (v1) | Service | `SubscriptionService.AddLineItem` / `Update` |
| Only percentage discounts on prepaid-settled subs | Service | `SubscriptionService` / `Coupon` validator |
| Late-arrival ingest bounded by `max_event_lateness` | API/consumer | `EventService.Ingest` |
| No event modification / deletion on prepaid-settled subs | Service/API | Reprocess / delete / replay paths |
| No backdated price edits touching prepaid-settled subs | Service | `PriceService.Update` |
| `usage_charge_mode` immutable after price is referenced | Service | `PriceService.Update` |
| No invoice generation for prepaid-settled subs | Service | `BillingService.GenerateInvoiceForBillingPeriod` — early return |
| No credit notes on prepaid-settled subs | API | `CreditNoteHandler.Create` |
| Addons blocked on prepaid-settled subs (v1) | Service | `AddonService.AttachToSubscription` |

Every enforcement point returns a typed error (`ierr.NewError(...).WithCode(...)`) with a stable error code.

---

## 12. Testing

- **Unit / table-driven** — one test file per validator; `arrear_usage` and `advance_usage` variants of each case.
- **Integration** (real Postgres + ClickHouse + Redis):
  1. Create wallet with $100, prepaid-settled sub with `FLAT_FEE` advance_usage price ($0.01 × COUNT).
  2. Ingest 500 events over 3 hours. Assert redis current-window increments.
  3. Advance wall-clock 2h past window ends; run snapshot workflow manually.
  4. Assert 3 snapshot rows, cumulative counters correct, 3 wallet debits totaling $5.00, redis current-window empty.
  5. Assert `GenerateInvoiceForBillingPeriod` returns `nil, nil`.
- **Late-arrival path**: ingest an event with `timestamp = 2 days ago` after settlement has already run for that period → assert reversal txns issued, snapshot rows recompute, cumulative counters correct, final wallet balance matches oracle.
- **Over-late path**: ingest with `timestamp > max_event_lateness` → assert 400 with expected code.
- **Modification path**: attempt to reprocess or delete a previously ingested event on a prepaid sub → assert rejected with expected code.
- **Legacy grant synthesis**: subscription with a legacy entitlement is upgraded to prepaid-settled → assert synthesized grant rows exist and behave under EG-eval.
- **Reconciler idempotency**: run reconciler twice over the same `ingested_at` window → assert rollup rows identical after merge.
- **Parallel grants**: multi-EC feature with one grant crossing → assert `mergedOverage` bills union interval; snapshot workflow reflects it.

---

## 13. Rollout

Each step feature-flagged behind `FLEXPRICE_FEATURE_ADVANCE_USAGE_CHARGE_MODE`, rolled per-tenant:

1. Ent schema: `price.usage_charge_mode` field + migration (no behavior change, default `arrear_usage`).
2. `subscription_meter_hourly_rollup` CH table + Reconciler workflow. Verify against a shadow tenant.
3. Redis current-window write path in event consumer (gated on subscription being prepaid-settled).
4. `subscription_usage_snapshots` postgres table + partitioning setup.
5. Snapshot workflow + Temporal schedule.
6. EG-eval data source switch for prepaid subs.
7. Legacy entitlement synthesis on subscription upgrade path.
8. Balance API updates.
9. Invoice pipeline early-return.
10. Webhook additions (`subscription.prepaid.created`, `subscription.prepaid.settled`).
11. Backdated event recompute path.
12. Enable `advance_usage` creation per-tenant via feature flag.
13. GA once at least one design-partner tenant has run a full billing cycle.
14. `plan_type` column drop (last, after nothing depends on it).

---

## 14. Open questions

### 14.1 Reporting parity (MRR / ARR)

MRR/ARR today sources from invoices. Prepaid-settled subs generate none — the parallel source is `subscription_usage_snapshots` + wallet ledger. Is a parallel computation path in scope for v1, or a follow-up? **Recommendation: follow-up.** Design partners for v1 are wallet-native and don't consume the invoice-derived MRR anyway.

### 14.2 Currency mismatch

Wallet currency vs plan currency. **Recommendation: restrict prepaid-settled subs to same-currency wallets in v1.** Revisit if a design partner needs FX.

### 14.3 Multiple wallets per customer

If a customer has multiple wallets (promotional + paid), which is debited? **Recommendation:** existing wallet-priority rules; add explicit `settlement_wallet_id` on the subscription if the default resolution is ambiguous.

### 14.4 Fast-lookup consistency window

Acceptable drift between redis, rollup, and `meter_usage` for the customer-facing balance API. **Recommendation: 5 min p99**, backed by the 5-min EG-eval + hourly reconciler cadence.

---

## Resolved (from earlier drafts)

- ~~Negative wallet balance~~ → **allowed**; tenant onboarding responsible for wallet + low-balance alerts (§8).
- ~~Auto-topup requirement~~ → **not required**; tenant configures as part of onboarding.
- ~~Grace tick~~ → **N/A** now that negative balance is allowed.
- ~~PREPAID addons on PREPAID plans~~ → **blocked in v1**; revisit with concrete need.
- ~~`plan_type` enum~~ → **dropped**; replaced by charge-level `price.usage_charge_mode` (§4.1, §10).
- ~~Daily debit with per-subscription cursor~~ → **replaced** by hourly snapshot workflow with +2h late-arrival buffer (§5).
- ~~ClickHouse materialized view for rollup~~ → **dropped**; MV can't cleanly compose with duplicate-handling. Reconciler writes rollup directly (§5.3).
- ~~Serial "priority" consumption for parallel grants~~ → **corrected**; parallel grants accumulate independently, billing on union of crossings (§6.1).
- ~~`entitlement_consumed` JSONB per-snapshot for revert~~ → **dropped**; EG-eval is idempotent (sums whole window each pass) so grants self-heal on backdated events (§7).
- ~~Legacy entitlements fail-closed on prepaid~~ → **synthesize as grants** instead (§6.3).
- ~~Ordering barrier / blocking primitive for snapshot vs EG-eval~~ → **snapshot workflow triggers EG-eval synchronously** for its window as step 3 (§5.2).
- ~~Grant-boundary precision (hour snap vs minute snap vs no snap)~~ → **minute-level rollup + inline crossing detection in event consumer**. Crossing detected on ingest with sub-second precision, persisted to postgres every 5 min. `mergedOverage` billable ranges are minute-aligned on the interior; ragged edges use small `meter_usage FINAL` fringe queries or minute snap (config-driven). See §5.1 step 4, §5.4 (trust check), §6.4.
- ~~Trust in redis current-window and crossing values~~ → **trust marker (`_window_start` on the hash, `detected_after_snapshot` on the crossing key, `_last_snapshot_end` on the EG counter) compared against `last_snapshot.window_end` on every read**. Stale/missing → rebuild from `meter_usage FINAL` and re-warm both redis and DB. §5.4, §6.5.
- ~~Balance API uniform-usage approximation~~ → **removed**. Billability is decided per-event at ingest time using existing parallel/additive grant-state semantics; the outcome is INCR'd into a per-subscription running cache. Balance API is a single HGET. §5.1 step 5, §6.4.
- ~~Per-meter union counter for current-window overage~~ → **replaced by per-subscription running billable cache**. Union counter couldn't handle overage intervals spanning multiple snapshot windows (e.g. the EC3 case in reviewer feedback where a week-long grant's overage spans hours). Per-sub cache collapses all interval math into an ingest-time boolean. §6.5.
- ~~"Rebuild redis from CH" hand-wave~~ → **explicit versioned-delta rebuild protocol** with no-data-loss guarantee under concurrent ingest. Single-writer via redis lock, ingests during rebuild route to `delta:{generation}`, atomic merge on completion. Small residual double-count (bounded to consumer-lag > safety-margin during rebuild) self-corrects at next snapshot re-anchor. §5.4.
- ~~Real-time API on stale/missing cache~~ → **fail-closed** with HTTP 503 `code = cache_rebuilding` and asynchronous rebuild trigger. Retries succeed in milliseconds to seconds. Non-realtime paths (snapshot workflow, backdated recompute) don't fail-closed — they read directly from CH. §5.4.
- ~~Amount-measure event straddling grant crossing~~ → **acknowledged bounded approximation** — an event that pushes `grant.usage` from `quota − 3` to `quota + 5` in one atomic write is classified fully-billable or fully-not by the ingest-time check. Error is bounded to ≤ 1 event's cost per crossing per grant, self-corrected at next snapshot re-anchor. §5.1 step 5.
