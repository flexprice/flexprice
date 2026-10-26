# FLE-1513 — Commitments v2 staging test plan

- **Scope:** everything shipped in PRs #3211–#3215, tested end to end on staging before enabling `commitments_v2` for a real tenant.
- **Date:** 2026-10-26
- **Author:** Ankit Malik

How to use this: sections run roughly in order — setup, then validations (cheap, catch wiring problems early), then the billing matrix (the core), then termination, lifecycle edges, and operations. Every billing scenario states the exact expected lines; compute the invoice twice wherever marked **recompute** to confirm idempotency. Record actuals next to each ☐.

Line kinds on invoices: **within** (usage covered by the commitment), **true-up** (unused floor, binding only), **on-demand** (usage beyond capacity), **cancellation-fee** (terminate only).

---

## 0. Setup

1. Staging environment on the release build, Temporal worker and consumer running.
2. A test tenant + environment; enable the `commitments_v2` setting there. Keep a second environment **without** the setting for the gate tests.
3. Meters: one SUM meter (tokens), one MAX-with-daily-bucket meter (gpus), one COUNT meter attached to a **feature/entitlement** (for the rejection test).
4. Plans: monthly / weekly / quarterly / annual, each with a usage price on the tokens meter (one flat, one SLAB-tiered, one PACKAGE); one plan with two usage prices (tokens + gpus); one plan with an entitlement on the COUNT meter.
5. Customers in a non-UTC timezone (Asia/Kolkata) and one in UTC.
6. Usage: ingest through the events API and wait for meter_usage to materialise (billing reads meter_usage only). Keep per-scenario event names/customers isolated so numbers are hand-checkable.

## 1. API, validation and permissions

Happy path:

- ☐ `POST /v1/commitments/preview` returns the commitment + computed value, persists nothing (search right after shows no row).
- ☐ `POST /v1/commitments` (amount, billing_period, binding, price scope) → 201; `GET` and `POST /search` return it; response `commitment_value` = value × number of periods in the term (spot-check one multi-period term).
- ☐ `PUT` updates only name / invoice_label / invoice_description / metadata; changing anything else is rejected; label change shows on the **next** generated invoice only.
- ☐ Create with `idempotency_key`, repeat the same request → same commitment back, no duplicate, no second `commitment.created` webhook.

Rejections (each returns a 4xx naming the problem — note the messages for support):

- ☐ `value <= 0`; `start_date >= end_date`; currency ≠ subscription currency.
- ☐ Scope covering no usage price on the subscription (wrong price id, FIXED price, meter not on the plan).
- ☐ Scope containing an **entitled** meter.
- ☐ Committed price with VOLUME tiers; committed/overage price with a negative amount.
- ☐ `window` measurement on a WEEK- or MONTH-bucketed price, or a group-by bucketed price.
- ☐ Overlapping commitment on the same scope/priority span (also against a **terminated** commitment whose span still covers the window).
- ☐ Idempotency key already used by a commitment on **another** subscription (standalone and inline).
- ☐ Create/terminate against a subscription in another environment (should 404, never leak).
- ☐ Commitment request in the environment **without** `commitments_v2` → rejected with the enable hint; subscription create with inline commitments likewise; subscription create **without** commitments in that environment works untouched.

Permissions (run with three API keys: full, read-only, event-only):

- ☐ Event-only key: all six commitment routes → 403.
- ☐ Read-only key: preview/search/get → 200; create/update/terminate → 403.

## 2. Inline creation

- ☐ Subscription create with an inline **subscription-scoped** commitment omitting `scope.entity_id` → created, scope = the new subscription id.
- ☐ Inline **price-scoped** commitment + `override_line_items` on that same price → created, scoped to the overridden (subscription-scoped) price; first invoice bills within at the committed terms over the overridden price's usage.
- ☐ Inline commitment scoped to a price added via `phases[0].line_items` → created.
- ☐ Duplicate idempotency keys inside one `commitments[]` array → whole request rejected, **no subscription created**.
- ☐ Checkout flow: create with checkout + inline commitment, make the payment-session step fail → subscription archived **and** commitment archived; search works; the same idempotency key is reusable on a fresh create.
- ☐ Inline commitment with a bad scope → whole subscription create rolls back (no orphan subscription, line items or commitment).

## 3. Billing matrix

For every scenario: generate the period invoice at period end (both via the Temporal billing run **and** a manual draft+compute), **recompute** the draft once, and diff — lines and totals must be identical. Check line metadata carries the commitment id and line kind, and that the line's price attribution is a real stored price (no made-up price ids).

### 3.1 Floors and rates (monthly)

- ☐ **Amount floor, under-use:** $100 floor, $60 of usage → within $60 + true-up $40, total $100. Next period over-use ($150 usage) → within $100 + on-demand $50. (Mirrors `TestMonthlyAmountFloor_UnderThenOverUse`.)
- ☐ **Non-binding** same shape: under-use bills only within $60 — **no true-up**.
- ☐ **Quantity + committed + overage price objects:** 1,000 units committed at $0.08, overage $0.12, line price $0.10. 1,300 used → $80 + $36. 700 used → $56 within + $24 true-up.
- ☐ **Tiered committed price (SLAB):** committed price with two slabs; verify period amount = cost(capacity) through the slabs and within splits proportionally across lines.
- ☐ **No usage at all:** binding floor bills true-up = full period amount, exactly one line.
- ☐ **PACKAGE line price:** commitment covers part of a package; remainder bills **fresh** through the package price (e.g. $10 per 100 units, 100 used, 50 covered → on-demand $10, not $5).
- ☐ **SLAB line price, marginal remainder:** covered units consume the cheap slab; on-demand bills the **marginal** tier cost (cost(full) − cost(full − od)).

### 3.2 Windows and timezones

- ☐ **Daily-window quantity** (gpus, MAX daily): full days, idle days, over-cap days in one month → within/true-up/on-demand GPU-day counts match hand math (mirrors `TestDailyWindowQuantityCommitment`).
- ☐ **active_windows** (e.g. 09:00–21:00 Asia/Kolkata): usage inside the window counts against capacity, outside bills on demand; check a window that wraps midnight.
- ☐ **IST subscription, weekly billing period:** bucket boundaries don't double- or zero-count the Sunday/Monday edge; totals equal the sum of daily usage.

### 3.3 Billing periods and cycles

Run the §3.1 amount-floor shape on each: ☐ weekly ☐ monthly ☐ quarterly ☐ annual (prorate the annual check to the first period), and ☐ one **calendar** billing cycle (first period is partial → floor prorated by days, cumulative-rounded: the first and second invoices' floors sum to exactly 2× the monthly value with no ±1¢ drift).

### 3.4 Stacking and scopes

- ☐ **Two commitments, one price, priorities 1 and 2:** usage fills priority 1's capacity first, then 2's, remainder on demand; each commitment gets its own within/true-up lines.
- ☐ **Price scope + subscription scope stacked:** the price-scoped commitment takes its line's usage first; the subscription-wide one pools what's left across both prices.
- ☐ **Meter scope** over a plan with two prices on the same meter: both lines draw from one pool; each line's usage priced through **its own** price.
- ☐ **Mid-period commitment start:** created on day 15 → first floor prorated; usage before the start bills on demand (mirrors `TestQuarterlyCommitmentCreatedMidPeriod_ProratesItsFirstFloor`).

### 3.5 Line-item changes mid-period

- ☐ **Price edit (versioning):** end-date the line, successor with a new price on the same meter; a meter-scoped commitment covers both versions, each version's usage priced through its own price (mirrors `TestMidPeriodPriceEdit_CommitmentSpansBothVersions`).
- ☐ **Quantity change** (new line item, same price): verify the invoice splits usage per line item. ⚠ Known gap: on the **raw-events charge path** two line items sharing a price double-count — this is on the before-un-gating list; confirm staging bills through meter_usage, and treat any double-count here as the known issue, not a new bug.
- ☐ **Detach guard:** deleting / ending the only scoped line item mid-commitment is rejected unless another line keeps the scope continuously covered.
- ☐ **Attach guards:** adding a price (plan change / addon / new line) whose meter is entitled, or that creates an invalid pairing with an existing commitment → rejected at the modification call.

### 3.6 Invoice-level interactions

- ☐ Commitment lines + a **coupon** on the subscription: discount applies per existing rules without corrupting commitment lines.
- ☐ **Taxes** apply to within/true-up/on-demand lines like any usage line.
- ☐ **Trial** subscription: no invoice during trial; the conversion invoice carries the commitment lines for its first real period.
- ☐ Subscription **without** commitments, feature enabled: invoice byte-identical to before enablement (the regression gate).
- ☐ Invoice **voided** and regenerated: recomputed within/true-up lines match the voided ones.

## 4. Termination

- ☐ **Mid-period with fee:** floor clipped to days run; usage after cutoff on demand; one ONE_OFF invoice, billing reason PRORATION, exactly the fee amount, one cancellation-fee line (mirrors `TestTerminateMidPeriod_ClipsTheFloorAndBillsTheFee`). `commitment.terminated` webhook fires.
- ☐ **Without fee:** no one-off invoice; period invoice clipped the same way.
- ☐ `effective_at` before the current period start → rejected; in the future (this period) → commitment counts until then.
- ☐ Terminate a non-binding commitment: it just stops; no fee allowed.
- ☐ **Repeat the same terminate call** → idempotent (no second fee invoice).
- ☐ **Subscription cancellation** (immediate and end-of-period): commitments terminate via the hook; final invoice clipped accordingly.
- ☐ New commitment on the same scope starting **after** a terminated one's effective end → accepted; starting inside the terminated one's still-binding span → rejected.

## 5. Webhooks, activity log, observability

- ☐ `commitment.created` / `.updated` / `.terminated` delivered with full payloads; `created` cascades a `subscription.updated`; **no** `created` on an idempotent replay.
- ☐ Activity log: entries for create/update/terminate with correct actor, customer linkage, and no leaked secrets in snapshots.
- ☐ Logs/traces: creating + invoicing a commitment produces no ERROR-level noise; spans for the commitment repository appear.

## 6. Load / soak (light)

- ☐ One subscription with 3 stacked commitments + daily windows over a quarterly period: invoice computes within normal latency; memory flat.
- ☐ 50 commitments across 20 subscriptions, run the period billing batch: all invoices correct, no lock contention errors (`commitments:<sub>` advisory lock waits, doesn't fail).
- ☐ Concurrent create with the same idempotency key (two parallel calls): exactly one row, both calls return it.

## 7. Rollout gates

Enable for a real tenant only when:

1. §3.6 regression (invoices without commitments unchanged) holds on a dry-run of a real tenant's past period.
2. The raw-events charge-tagging gap and the later-added-entitlement guard gap are either fixed or confirmed not to apply to the tenant (no shared-price duplicate line items; no entitlement changes planned on scoped meters).
3. All §1–§5 boxes green; §6 green on staging-sized data.

Out of scope (not built, don't test): billing-terms collection, instalments, utilisation endpoint, analytics rollup, legacy commitment migration.
