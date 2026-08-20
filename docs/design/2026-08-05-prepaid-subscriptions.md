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
2. **Trust check** on `redis:sub_usage:{env}:{sub}` — see §5.5. If stale/missing, rebuild from `meter_usage FINAL` synchronously before continuing (rare).
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
5. **Maintain redis EG billable counter** (§6.5) — if any grant applicable to `(sub, meter)` is currently crossed (either `redis:grant_crossing:{grant_id}` set or postgres `grant.quota_crossed_at` set):
   ```
   HINCRBYFLOAT redis:meter_billable_current:{env}:{sub}:{meter}:{window} by qty
   HSETNX  ... _started_at = now, _last_snapshot_end = last_snapshot.window_end
   ```
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
8. `HDEL` redis current-window key AND `redis:meter_billable_current:*:{sub}:*:{window}` keys.
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

### 5.4 Redis trust check (make redis reads safe)

Every read from `redis:sub_usage:{env}:{sub}` or `redis:grant_crossing:{grant_id}` runs the following trust check first. The invariant: **redis is trustworthy only if it represents data that is fresher than the DB snapshot for the same subscription/grant**. If not, disregard redis, rebuild from `meter_usage FINAL`, and re-warm both DB and redis.

**Trust marker in redis current-window hash:**

```
redis:sub_usage:{env}:{sub}
  _window_start        = <unix ts when snapshot workflow HDEL'd the previous state
                          and reopened this window; equals last_snapshot.window_end>
  {meter}__{window}    = <decimal usage>
```

**Trust marker in crossing key:**

```
redis:grant_crossing:{grant_id}
  ts                       = <sub-second unix ts when crossing was detected>
  detected_after_snapshot  = <last_snapshot.window_end at detection time>
```

**Trust check for current-window read** (balance API, EG-eval, inline crossing detection):

```
redis_window_start = HGET redis:sub_usage:{env}:{sub}  _window_start
pg_last_snapshot_end = latest snapshot.window_end for this sub (postgres, single indexed read)

if redis_window_start is nil
  OR redis_window_start < pg_last_snapshot_end:
  // stale/missing — treat as untrusted
  usage_by_meter = meter_usage FINAL for [pg_last_snapshot_end, now)
  HSET redis:sub_usage:{env}:{sub} _window_start = pg_last_snapshot_end
  HMSET redis:sub_usage:{env}:{sub} <per-meter usage>
  return usage_by_meter
else:
  return HGETALL redis:sub_usage:{env}:{sub}
```

**Trust check for grant crossing read:**

```
crossing = HGETALL redis:grant_crossing:{grant_id}
pg_crossed_at  = grant.quota_crossed_at (postgres)
pg_last_snapshot_end = latest snapshot.window_end for this sub

if pg_crossed_at is set: return pg_crossed_at              // DB is authoritative once written
if crossing is nil: return nil                              // no crossing yet
if crossing.detected_after_snapshot < pg_last_snapshot_end:
  // redis crossing marker predates the last DB snapshot — stale
  DEL redis:grant_crossing:{grant_id}
  return nil        // fall back to EG-eval / snapshot workflow to re-derive from CH
return crossing.ts
```

**Persistence worker** (Temporal schedule, per env, every 5 min — reuses EG-eval cadence):

```
for each redis:grant_crossing:{grant_id}:
  trust-check the crossing (as above)
  if trusted AND postgres grant.quota_crossed_at is nil:
    UPDATE entitlement_grants SET quota_crossed_at = crossing.ts
      WHERE id = grant_id AND quota_crossed_at IS NULL
```

Once postgres holds `quota_crossed_at`, it's immutable. Redis key can then be lazily deleted (or left with a TTL) since all future reads short-circuit to postgres.

**Trust check rebuilds are rare.** They fire on: (a) redis eviction / restart, (b) a snapshot workflow that HDEL'd redis but crashed before the rebuild fires. Both are recoverable; neither loses data because `meter_usage` is source of truth.

### 5.5 Cost profile — where CH is touched

| Operation | `meter_usage FINAL` | Rollup query | Redis | Postgres |
|---|---|---|---|---|
| Event ingest (prepaid) | 1 insert | — | trust-check + 1 `HINCRBYFLOAT` (raw) + inline crossing check + 1 `HINCRBYFLOAT` (EG counter, only if any grant crossed) | — (cached grant + crossing reads only) |
| Trust-check rebuild (rare, on redis miss) | 1 range scan (window bounded) | — | rewrite hash | — |
| Reconciler (hourly, per env) | 1 range scan by `ingested_at` | 1 batch insert | — | 1 `last_reconciled_max_ingested_at` update |
| EG-eval (5-min debounced, per grant, prepaid) | — | 1 range scan (`FINAL`) | 1 `HGETALL` (current window) | grant.usage update |
| Persistence worker (5-min, per env) | — | — | scan crossing keys | grant.quota_crossed_at UPDATE (only when nil) |
| Snapshot workflow (per active sub per active hour) | 1 range scan (1h window) | — | 1 `HDEL` batch (raw + EG counter) + set `_window_start` | 1 snapshot row + wallet txn |
| Balance API (prepaid) | Fallback only (EG counter trust-check miss, bounded to <1h) | — | 1 `HGETALL` + crossing check + 1 EG counter read | grant + wallet reads |
| `mergedOverage` at invoice time (arrear) | Fringe only (rare mid-hour edges) | 1 per range | — | — |
| Backdated event recompute | 1 per affected hour | — | — | affected snapshot rows |

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

**Balance API uses the same overage computation as billing.** No approximation, no uniform-usage assumption. This is what makes "wallet balance as displayed" == "what a snapshot-right-now would debit" — the invariant customers rely on.

Reuse `adjustMeterUsageGrants` / `mergedOverage` (`internal/ee/service/billing_meter_usage_grants.go`), which already handles:
- Additive vs parallel grants (`perECOverage` fold, then `mergedOverage` for parallel).
- Quantity vs amount measure (measure branching at the end of `adjustMeterUsageGrants`).
- `first_usage` allocation behavior (encoded in grant `valid_from` set at grant open time — no special logic needed at billing).
- Grant pricing guards (rejects tiered / bucketed / non-additive combinations).

```
balance = wallet.current_balance
        − billable_in_current_window
        − pending_invoices

billable_in_current_window computation:
  raw_by_meter = trust_checked_read(redis:sub_usage:{env}:{sub})   // §5.4
  billable = 0

  for each (meter, raw_current) in raw_by_meter:
    grants = load_active_grants(sub, meter, now)                   // postgres, indexed
    
    // Refresh grant.usage_now for exhaustion check (approximate is OK here —
    // we only decide *whether* a grant is crossed. The billable-range math
    // below reads exact usage from redis EG counter or CH.)
    for each grant in grants:
      grant.usage_now = grant.usage_cached + raw_current
      grant.crossing_ts_now = trust_checked_crossing(grant.id) or (
        last_snapshot.window_end if grant.usage_now >= grant.quota else nil
      )

    // Call the existing billing fold. Its mergedOverage step needs a
    // usage_provider(interval) callback (small refactor to make it injectable).
    result = adjustMeterUsageGrants(
      item, matchingCharge, grants, priceService, meter, sub, extCustomerIDs,
      usage_provider = balance_api_usage_provider,   // see below
    )
    billable += result.amount

  return billable

balance_api_usage_provider(interval [start, end)):
  // interval is a merged billable range from mergedOverage;
  // in the balance API it can only lie inside the current window
  // (historical hours are already debited via snapshots).
  if trust_checked_meter_billable_counter(sub, meter, current_window) valid
     AND counter._started_at <= start:
    return counter_value   // exact, O(1)
  else:
    // fallback — bounded to current window, at most 1h of meter_usage
    return SELECT SUM(qty_total) FROM meter_usage FINAL
             WHERE ... AND timestamp >= start AND timestamp < end
```

**Fast path (normal case):** redis EG billable counter is present and covers the entire billable range → zero ClickHouse queries.

**Fallback path:** counter stale/missing → single `meter_usage FINAL` query bounded to a partial hour (`[max(crossing_ts, last_snapshot.window_end), now)`). Cheap because bounded, rare because the counter is maintained inline (§5.1 step 5).

### 6.5 Redis EG billable counter (fast-path storage for balance API)

Purpose: give the balance API an O(1) source for "usage in the current window while at least one applicable grant was crossed", which is exactly what `mergedOverage` needs when its billable range lies inside the current window.

**Keys:**

```
redis:meter_billable_current:{env}:{sub}:{meter}:{window}
  _started_at        = <first timestamp we started accumulating for this window>
  _last_snapshot_end = <last_snapshot.window_end at counter creation, for trust check>
  value              = decimal accumulated units
```

**Write path** (§5.1 step 5): on each event ingest for a prepaid-settled sub, if any grant applicable to `(sub, meter)` is currently crossed (redis crossing key set OR postgres `quota_crossed_at` set), `HINCRBYFLOAT` the value by `qty` and set `_started_at`/`_last_snapshot_end` on first write via `HSETNX`.

**Read path** (§6.4 `balance_api_usage_provider`):
- Trust check: `_last_snapshot_end == current last_snapshot.window_end` AND `_started_at <= interval.start`.
- If trusted → return `value` as the merged-billable usage for that interval.
- Else → fall back to `meter_usage FINAL` for the interval.

**Reset:** snapshot workflow HDELs these keys along with the raw current-window hash (§5.2 step 8).

**Why per-meter union counter, not per-grant?** `mergedOverage` bills usage in the *union* of crossed grants' intervals (see §6.1). The union counter tracks exactly that — usage while any grant is crossed. Per-grant counters would over-count when parallel grants' intervals overlap.

**Amount vs quantity measure:** the counter stores **units** in both cases; the existing measure branching in `adjustMeterUsageGrants` prices units → money for quantity, or hands units to `GetMeterWindowCost` for amount. No branching needed in the counter itself.

**One implementation caveat:** the "any grant crossed" check on the write path is a hot-path read of postgres `grant.quota_crossed_at` per applicable grant. Cache these per subscription in the consumer memory (invalidate on persistence-worker write or on 30s TTL). Consumers restart occasionally; on cold start, first event re-reads postgres.

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
- ~~Balance API uniform-usage approximation~~ → **removed**. Balance API now reuses `adjustMeterUsageGrants` / `mergedOverage` from billing verbatim, with a redis EG billable counter as the O(1) fast path and `meter_usage FINAL` as the bounded fallback. "Wallet balance as displayed" is guaranteed to match "what a snapshot-right-now would debit". §6.4, §6.5.
