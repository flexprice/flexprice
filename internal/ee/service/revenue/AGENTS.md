# Revenue Facts — `internal/ee/service/revenue`

The `revenue_facts` system: a **shadow write-path** that re-runs the billing
preview and decomposes every charge into per-day (or per-period) revenue rows,
plus the analytics read surface over those rows.

Design doc: [FLE-1257 analytics platform ERD](../../../../docs/design/2026-09-10-FLE-1257-analytics-platform-erd.md)
· Export contract: [docs/export/revenue-facts.md](../../../../docs/export/revenue-facts.md)

## Hard invariants

- **Never mutate billing.** This package re-runs `PrepareSubscriptionInvoiceRequest`
  under `ReferencePointRevenueFacts` and only ever writes `revenue_facts` rows.
  Coupon amounts are dry-run (nothing persisted, no redemption counted).
- **Reconciliation is shadow-only.** Row / line-item / invoice sums are checked
  against the engine's own amounts; a mismatch is logged
  (`revenue_reconciliation_mismatch`, `revenue_facts_drift`) — it never blocks
  a write or an invoice operation.
- **Row identity** (marginal usage rows):
  `net == usage_at_list_rate + tier_delta − entitlement_amount − line_discount − invoice_discount`.
- **`invoice_id` names the source, `status` says whether it counts.** The JIT
  `rollupFromInvoice` stamps the invoice and line item on the PROVISIONAL rows
  it derives, so a row that never flips stays traceable to where it came from.
  Booked revenue is therefore `invoice_id AND status = FINAL`, never
  `invoice_id` alone — `ListByInvoiceID` enforces that, because counting
  provisional rows would let an invoice whose rows never flipped look
  reconciled to the drift sweep.
- **Idempotency everywhere.** Upserts land on the provisional grain
  (tenant, environment, subscription, price, sub_line_item, day, revenue_source);
  recomputes bump `version` in place. FINAL rows are immutable — a void appends
  contra rows (`is_revert = true`), never edits.
- **The scan may over-scope, never under-scope.** A subscription rolled
  unnecessarily costs one batched read and a diff that writes nothing; one
  wrongly skipped goes silently stale. Every uncertain answer in `scanScopeFor`
  widens to a full pass, and a scheduled full rebuild is the net underneath.
  There is no flag to turn the scan off: at production scale a full pass per
  run does not finish, so correctness has to come from the triggers being
  right, not from being able to disable them.
- **Usage is not the only trigger.** A subscription with no usage at all still
  owes fixed charges, commitment true-ups and — for bucketed windowed
  commitments — one true-up row per empty window. Most of those are written when
  the period opens, so `current_period_start`, never-rolled and stale-coverage
  are triggers in their own right, not conveniences.
- **A commitment accrues without usage.** The bucketed curve is clamped to
  today, so a windowed commitment's true-up gains a window every day even when
  nothing is metered. Those customers are rolled every pass
  (`customersWithAccruingCommitments`); every other trigger reads them as quiet,
  and their accrual would stop after the period's opening roll.
- **A row must carry something.** A row with no money and no quantity behind it
  records that a line item existed and delivered nothing, which no query asks
  for — and at hundreds of line items per subscription it is the whole table
  (measured on production: 13,464,148 of 13,465,791 rows). Such a row is
  written only when one already exists for its grain, because the upsert never
  deletes and that write is what stops a stale non-zero row standing as
  revenue. Zero net with a consumed entitlement is NOT nothing: it is how free
  usage stays visible.
- **Reads are batched per subscription, writes are diffed.** `buildUsageCurve`
  must read from the pre-fetched `rollupInputs.usage(meterID)`; falling back to
  its own query is one round-trip per line item, which is what made a full pass
  take hours. Reconciliation runs on the full row set, before the diff.
- **Tenant scoping.** Every query filters tenant + environment.
- **Opt-in gates every write.** No row is written for an environment whose
  `revenue_analytics_config` is absent or disabled — not by the batch jobs,
  and not by the invoice finalize/void hooks either. An environment that never
  opted in would otherwise accumulate rows no sweep ever reconciles, because
  the sweep only walks opted-in environments. Every write method on
  `interfaces.RevenueService` checks `revenueAnalyticsEnabled` and returns
  silently; `TestWriteEntryPointsRequireOptIn` holds the whole surface.
- **Day-grained bounds, in the subscription's timezone.** `period_start`,
  `period_end` and `day` are DATE columns, so every write and every lookup goes
  through `periodDays` / `dayOf` — and both take a `*time.Location`, because
  `buildUsageCurve` splits usage into **local** days. Truncating in UTC instead
  puts the bound a day off for any non-UTC subscription, and the flip then
  misses a row the curve wrote. `inclusiveLastDay` must also agree with that
  walk: the last day is the one *before* the exclusive end's own local date,
  because that date opens the next period. A period shorter than a day is
  clamped to its own date rather than inverting.

## Dependency rules

- This package **imports `internal/ee/service`** (for `ServiceParams` and the
  billing/price/subscription constructors). The service package must **never
  import this one**.
- The interface is `interfaces.RevenueService` (aliased here as `Service`), so
  the service layer's invoice hooks (`service/revenue_hooks.go`) can call
  finalize/void updates through the `ServiceParams.RevenueFacts` field —
  injected in `cmd/server/main.go` (`enrichServiceParams`), nil-guarded, and
  never constructed inside the service package.
- Leaf consumers (API handler, Temporal activities, main) call `revenue.New`
  directly.

This is the extraction pattern for future sub-services: implementation package
under `ee/service/<name>` importing `service`; interface in
`internal/interfaces` **only if** the service layer must call back into it,
wired as a `ServiceParams` field in main.

## File map

| File | What it holds |
|---|---|
| `rollup.go` | `Service`/`New`, the rollup (preview → decompose per line → reconcile → upsert), finalize flip + JIT invoice fallback, void revert |
| `decompose.go` | Row shapes: `previewLineItem`, `dayCharge`, marginal/period_only writers, `decompositionMode` classifier |
| `curve.go` | Daily cumulative usage curve (SUM-based, re-priced via `CalculateCost`) |
| `overage_curve.go` | Commitment split: curve halves at the boundary, tier-inverse quantity split |
| `grant_curve.go` | Entitlement-grant billing rebuilt per day from quota-crossed windows |
| `reconcile.go` | Row / line-item / invoice identity checks |
| `sweep.go` | `ReconcileBookedInvoices`: drift detection on finalized/voided invoices, `auto_correct` repair |
| `analytics.go` | `GetRevenueAnalytics`: facts → grouped, time-bucketed rows (allocation policy, adjustment breakout) |
| `source.go` | Event-source (`meter_usage.source`) share allocation for `group_by: source` |

Related but deliberately elsewhere: invoice hooks in
`service/revenue_hooks.go`, the scheduled S3 exporter in
`service/sync/export/revenue_facts_export.go`, DTOs in
`api/dto/revenue_analytics.go`, repositories in `domain/revenuefact` +
`repository/ent/revenue_fact.go` + `testutil/inmemory_revenue_fact_store.go`.

## Decomposition decisions

Every line item lands in one of two modes. `marginal` writes a row per day;
`period_only` writes one row for the period, dated by cadence (advance → period
start, arrear → period end). Whole-period is a **last resort**, and each case
below is a decision, not a default — if you add a shape, decide explicitly and
record it here.

**Split per day (`marginal`)**

| Case | How |
|---|---|
| SUM / COUNT / SUM_WITH_MULTIPLIER + flat, graduated (SLAB) or package pricing | `buildUsageCurve` re-prices the cumulative quantity each day; `SUM(qty_total)` reproduces all three (the multiplier is baked in at ingestion) |
| Any bucketed pair whose window is ≤ a day | `buildBucketedCurve` prices each window as the engine does and groups windows into days |
| Entitlement-grant billed usage | `buildGrantOverageCurve` splits along the grants' quota-crossed windows |
| Subscription-level commitment (normal + overage sibling lines) | `splitCurveAtCommitment` cuts the curve at the commitment boundary, quantities at the tier-curve boundary |
| Line-level **windowed** commitment on a bucketed day-grain line | `decomposeBucketedCommitmentRows` dates committed usage, overage and true-up on the day each window covers |

**Whole-period (`period_only`), and why**

| Case | Why it cannot be daily |
|---|---|
| Volume tiering | every unit re-rates at the final tier, so a day's marginal charge is not a day's value |
| LATEST / AVG / WEIGHTED_SUM | not additive across days |
| Plain (non-bucketed) MAX | the charge is one peak, not an accrual |
| COUNT_UNIQUE | a distinct count is not a running sum — the same event across two days must add once |
| Bucketed with week or month windows | the window spans days, so its charge belongs to no single one |
| Bucketed **with an entitlement limit** | the limit is consumed across the period, which a per-window shape does not model |
| Bucketed under a subscription-level commitment split, or a non-windowed line commitment | both walk a cumulative curve, which a bucketed line does not have |
| Grant-billed usage whose windows carry no usage to shape by | the shape is unknowable |
| Fixed prices | not usage; dated by cadence |
| Subscription-level true-up | only knowable once the period's usage is final |
| Multi-period commitments | skipped entirely — the true-up needs a rollup-maintained prior base (ERD Q3) |

Bucketed lines never take the grant path: `GrantPricingGuard` rejects bucketed
meters, so the engine does not fold grants there either.

## What makes a row

A row in `POST /analytics/revenue` is uniquely identified by:

1. **Group values** — the requested `group_by`, plus `revenue_source` and
   `currency`, which are always applied (a revenue number is ambiguous without
   its kind, and amounts across currencies do not add).
2. **Time bucket** — the day, the billing period's bounds, or nothing at
   `total` granularity.
3. **Status** — `FINAL` and `PROVISIONAL` rows for the same bucket stay
   separate. Omit `status` in the request to get both.
4. **Adjustment type** — only when `include_adjustments` splits true-up,
   overage and revert out of their parent buckets.

Everything outside that key is **summed into** the row. That is why adding a
dimension changes what a row *means* rather than just labelling it: grouping by
customer answers "revenue per customer", and adding `subscription_id` answers a
different question with more rows. So dimensions are never added implicitly —
if a caller needs subscription or price on the row, they ask for it in
`group_by`, and the response's `query` echo records what was actually applied.

## What reconciliation actually checks

Shadow-only — every mismatch is logged (`revenue_reconciliation_mismatch`,
`revenue_facts_drift`), never blocking.

| Grain | Check | Applies to |
|---|---|---|
| Row | `net == list + tier − entitlement − line_discount − invoice_discount` | every row that carries a decomposition, **whatever its source** — including the overage rows the commitment split produces |
| Line item | Σ row net == the engine's amount for the line | every line, all sources together |
| Invoice (provisional) | Σ all rows == `Subtotal − TotalDiscount` | each rollup pass |
| Invoice (FINAL) | Σ flipped rows == `Subtotal − TotalDiscount` | after the finalize flip |
| Booked invoices | re-derive vs stamped rows | the daily `ReconcileBookedInvoices` sweep |

A **money-only** row is exempt from the row check by design: a true-up, or an
overage the engine reports as an amount with no units behind it, carries the
charge whole and has no list rate to check against. `carriesDecomposition`
draws that line — do not widen the exemption by source, or rows that do claim
the identity stop being verified.

## Testing

Tests run against the real billing preview via `testutil.BaseServiceTestSuite`
(in-memory stores, no Docker). `rollup_test.go` holds the suite + worked
example; `e2e_test.go` the full lifecycle; `matrix_test.go` the pricing/
metering matrix and grants+wallet lifecycles. Any change here must keep
`Σ net_amount` reconciling to the engine preview with zero mismatch logs.
