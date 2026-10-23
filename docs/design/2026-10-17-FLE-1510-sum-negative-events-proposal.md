# PRD: SUM with negative events

**Customer:** Optimality · **Ticket:** FLE-1510 · **Updated:** 2026-10-19
**Status:** Phase 1 in progress: PRs 1–2 are drafts; PR 3 grant recovery is implemented locally and awaits review. PR 4 starts only after approval of PR 3.
[Implementation plan](2026-10-18-FLE-1510-sum-negative-events-implementation-plan.md)

## 1. Customer need

Optimality sends changes in storage, such as +10 GB uploaded and -4 GB deleted. They need to know how much storage each customer currently holds, compare it against an allowance, and restore available capacity when resources are deleted. Their application uses the reported usage and entitlement to enforce the limit.

The balance must survive billing-period boundaries. Customers may still hold resources, consume allowance and incur charges in a period with no new events.

Build this through ordinary SUM and the existing metering, grants and billing infrastructure. A separate SIGNED_SUM aggregation is unnecessary.

## 2. Usage and checkpoints

SUM preserves signed event quantities and aggregate results. For example, +100, +20 and -30 produce 90; +50 and -70 produce -20. Raw usage is never floored to zero.

| Meter reset policy | Evaluated usage |
|---|---|
| Period reset | Signed SUM within the existing applicable period or line-item window |
| NEVER | Opening checkpoint plus signed events from the current billing period through the evaluation endpoint |

NEVER carries the balance forward for both usage reads and billing. There is no separate running-billing switch. Reset policy does not select a future grant or price evaluation mode.

Checkpoints store raw signed usage, independent of prices, allowance and payment results. A checkpoint at T covers events before T; events at T belong to the following interval. Routine NEVER reads query only the current period’s ClickHouse events.

New events timestamped before the checkpoint are unsupported initially and must be explicitly rejected or reported. This applies to positive and negative events. Already-accepted queued events must be accounted for before publishing a checkpoint; retries must not count twice. Historical correction and replay are deferred.

Before enabling an existing customer, verify complete signed history or establish an authoritative opening balance and cutoff. Previously zeroed negative quantities cannot be recovered from those stored totals alone.

## 3. Billing

Retain existing subscription line-item pricing. For ordinary per-unit pricing without a grant:

`charge = evaluated usage × line-item unit price`

Period-reset SUM retains each line item’s usage window. NEVER evaluates the balance at each line item’s endpoint. No new duration proration or time weighting is introduced.

For example, +100 units in a first window priced at $1, then +20 in a second window priced at $2:

| Policy | First line | Second line | Total |
|---|---:|---:|---:|
| Period-window usage | 100 × $1 | 20 × $2 | $140 |
| NEVER balance | 100 × $1 | 120 × $2 | $340 |

This can charge the held balance at multiple endpoints after a price change. Final-value pricing using the final applicable price is Phase 2. Whether to exclude mid-period price changes from Optimality’s initial configuration remains a release decision; the formula above is the agreed behavior if supported.

Signed nonlinear pricing, commitments and bucketed/grouped running balances need separate policies. Their handling is deferred; the implementation must define and enforce the initial supported combinations rather than assume every SUM price configuration works.

## 4. Entitlement grants

The initial supported case is **one fixed quantity allowance per billing period**, unchanged within that period. Daily/hourly, additive/parallel and amount-based grants, and mid-period changes affecting the allowance through plan changes or subscription overrides, are outside this initial combination.

A billing-period grant evaluates the current period’s signed usage for a period-reset meter, or the checkpoint-backed balance for NEVER. Renewal gives the existing full quota without erasing the NEVER balance.

| Quota | Evaluated usage | Available | Grant state | Billable overage |
|---:|---:|---:|---|---:|
| 100 | 80 | 20 | Active | 0 |
| 100 | 120 | 0 | Exhausted | 20 |
| 100 | Falls from 120 to 80 | 20 | Active again | 0 |
| 100 | -20 | 100 | Active | 0 |

Available allowance is bounded between zero and quota. With a supported quantity grant, billable usage is `max(evaluated usage - quota, 0)`. Negative usage can restore allowance but does not produce negative overage charges. Raw usage and checkpoints remain signed.

Recovery updates the same valid grant. It does not create another grant or extend validity. Expired allowance stays unusable. Record internal alert recovery so a later exhaustion can produce a fresh alert; a public recovery webhook is not required for Phase 1.

The customer-facing response must distinguish raw usage from grant consumption/availability and preserve the required storage precision. State its freshness clearly. The customer application controls admission and must allow deletions while exhausted; this feature does not introduce atomic reservations.

## 5. Invoices and settlement

Without a grant, negative evaluated usage can create negative charges. Negative lines remain on the invoice and contribute to its signed total. A $20 charge and a -$30 usage charge produce a -$10 total before other adjustments.

For an invoice with no prior payment:

| Signed invoice total | Collectible due / remaining | Amount paid |
|---:|---:|---:|
| $70 | $70 | $0 |
| $0 | $0 | $0 |
| -$30 | $0 | $0 |

Finalize invoices with nothing collectible without requesting payment. Never create negative payments, automatic wallet credits, refunds or future-invoice offsets from a negative total. Preserve the signed total through recalculation, tax application and currency conversion. Existing payments must not be erased by recomputation.

**Decided credit policy:** keep negative line items on the invoice, where they reduce the signed total. Apply prepaid credits independently to each positive usage line after discounts, limited by available credits. Fixed and negative lines receive no prepaid credits. Negative lines do not offset another line’s credit eligibility; there is no cap based on net usage across the invoice. This policy applies at both finalization and expiring-credit settlement.

For example, separate +$50 and -$45 usage lines with sufficient credits consume $50 credits against the positive line. The invoice total becomes -$45, the amount due is zero, and no wallet refund or credit is created. Per-line allocation is the chosen behavior, not a temporary limitation pending cross-line netting.

Expiring credits can already settle against draft invoices. Reconciliation if usage later decreases is deferred; the initial compatibility boundary for that combination must be explicit. External billing/accounting routes need verification before claiming negative-invoice support.

## 6. Initial scope and Phase 2

**Agreed initial scope:** signed SUM; checkpoint-backed NEVER; existing period-window pricing; one unchanged billing-period quantity grant; grant recovery and capped availability; signed invoices with nonnegative collection; existing prepaid allocation.

Restrictions apply to newly enabled combinations and must be enforced without unnecessarily changing other customers’ existing configurations.

**Phase 2 / open points:**

- Evaluation settings and prorated endpoint-balance pricing are agreed directions for Phase 2, detailed below. Final-value pricing using only the month-end price remains a separate open option. Neither evaluation setting is required for the restricted initial case.
- Mid-period allowance replacement, successor carry-forward, plan/override transitions and shorter, additive, parallel or amount-based grants.
- Negative totals with nonlinear prices, commitments and bucketed/grouped balance evaluation.
- Credit-expiry settlement reconciliation when an already-settled line’s charge later decreases.
- Historical event/checkpoint correction, finalized-invoice adjustments, automatic refunds and future offsets.
- Time-weighted billing, which belongs to a separate aggregation/billing model.

### Agreed Phase 2 direction: evaluation across entitlement and price changes

These decisions extend the product after Phase 1; they are not part of the current PR 3 patch. Add independent evaluation settings on entitlements and prices, provisionally `usage_evaluation_mode = windowed | current_balance`. Names remain provisional. Window-based behavior is the default; existing Phase 1 NEVER behavior is retained.

| Evaluation | Period-reset SUM | NEVER SUM |
|---|---|---|
| Existing window-based behavior | Events inside the grant or price line-item window | Retain the Phase 1 checkpoint-backed balance behavior |
| Current balance | Signed events from subscription billing-period start through the evaluation endpoint | Checkpoint-backed cumulative balance through the endpoint |

The entitlement/price setting chooses the evaluation technique. The meter reset policy chooses whether the balance resets each billing period. Current balance does not mean mathematical absolute value or lifetime usage for a period-reset meter.

**Entitlements.** In current-balance mode, each grant window identifies when its quota applies. Evaluate usage through `min(evaluation time, grant.ValidTo)` using the range above, rather than restarting at the successor's `ValidFrom`. A change creates a successor with its own full applicable quota; do not calculate its quota as the predecessor's remaining allowance plus a delta. Obtain usage from the shared meter evaluator/checkpoints, not by copying or adding grant snapshots. Availability remains `clamp(quota - balance, 0, quota)`.

Example: quota 100 and balance 120; change quota to 150. The successor starts with balance 120 and 30 available. A later -40 event makes the balance 80 and availability 70. The predecessor remains a historical window; normal window-based successor behavior remains unchanged.

**Pricing.** In current-balance mode, each line uses the balance at its own endpoint, its applicable price and duration proration:

`line amount = endpoint balance × line unit price × line active-duration fraction of billing period`

With a supported quantity grant, substitute `max(endpoint balance - quota applicable at that endpoint, 0)` for endpoint balance. Do not sum cumulative grant usage or grant-window overages and reuse that sum for every price line. Evaluate the relevant balance and quota for each line endpoint.

For 30 equal daily lines, line 1 sees day 1's cumulative usage, line 2 sees days 1–2, and so on; each is multiplied by its price and 1/30. A constant balance of 100 at $1 therefore totals $100 with either one monthly line or 30 equal daily lines. This is closing-balance pricing per segment with duration proration, not continuous time-weighted usage. Different segmentation can still change charges when the balance changes.

**Still to specify before implementation:** proration/calendar conventions and rounding; handling an entitlement change inside a price line (including whether to split it); and shorter, additive/parallel or amount-based grant semantics. Plan changes must supply an explicit applicability boundary so incoming grants are not backdated to the old cycle start. Correcting that existing boundary issue can be separate from adding the new evaluation modes. The earlier final-price-at-month-end option remains open.

## 7. Release decisions still required

These decisions complete rollout planning; they do not reopen the agreed behavior:

1. **Supported configuration:** confirm initial price/payment routes, mid-period price-change restrictions, coupons/taxes and expiry-settlement compatibility. Recommended starting point: one unchanged unbucketed per-unit price and one applicable subscription per customer-meter.
2. **Customer cutover:** verify Optimality’s opening data, storage units/precision and usage-read freshness expectations. Define identity changes and cancellation/re-subscription handling so commercial changes cannot silently reset physical usage.
3. **Coordinated release:** release signed SUM ingestion, usage, grants and invoices together; no signed-SUM feature flag or opt-in is required. Verify any existing SIGNED_SUM migration, SUM/NEVER billing changes and previously clamped history before cutover.
4. **Technical contracts:** finalize the queue/checkpoint closure protocol, zero-payable payment-status mapping and any necessary public response changes. These tasks and their code locations are in the implementation plan.

## 8. Acceptance criteria

| Scenario | Required result |
|---|---|
| +100, +20, -30 | Raw SUM 90 |
| Raw -20, then +30 | Raw 10; retain the negative prefix |
| NEVER balance 80, new period with no events | Balance remains 80; quota 100 leaves 20 available |
| NEVER balance 130, quiet renewal | New quota 100; 30 billable overage |
| Grant usage 120 → 80 → 120 | Exhausted → active → exhausted, with correct alert transitions |
| Negative usage with quota 100 | Available never exceeds 100; billable overage is zero |
| Expired grant, later decrease | Old allowance does not become usable |
| No grant, -30 units at $1 | Signed total -$30; due/remaining/paid zero; no collection or automatic credit |
| $0 usage price or skipped payment | Usage, allowance and checkpoints remain correct |
| Event at checkpoint cutoff | Counted exactly once |
| Duplicate event or processing retry | No duplicate usage, debits or alerts |
| Unsupported input/configuration | Explicit rejection or compatibility boundary; no silent corruption |

Usage reads, grant state, preview and invoice calculations must agree for the same cutoff and accepted event set. Positive-only period-reset behavior remains compatible. NEVER accumulation and signed input are intentional changes requiring controlled rollout.

## 9. Technical debt

Three agreed follow-ups remain open.

### 1. Reconcile expired credits after negative usage

If expiring credits settle $100 against a draft usage line and later negative usage reduces that same line’s charge to $60, the wallet still records $100 applied to the invoice. A separate negative line does not reduce the original line’s credit eligibility under the agreed per-line policy.

Reconcile the allocation to $60 consumed and $40 expired. Keep the wallet ledger, invoice credit attribution, and expiry accounting consistent without restoring expired funds to the spendable balance. Per-line credit allocation does not reconcile credits already settled before that line’s charge decreased.

### 2. Bill temporary overage with peak or time-weighted usage

SUM measures net usage within each price-line window; NEVER SUM measures the running balance at its evaluation endpoint. Overage that rises and falls within that window can disappear before billing and therefore go unbilled.

Use peak-balance evaluation to charge for spikes, or time-weighted SUM over the balance to charge by duration. For allowance overage, accumulate `max(balance(t) - quota(t), 0) × duration`. For example, 20 units above quota for 3 hours produces 60 unit-hours of overage. Weight the balance held between events, not the signed event deltas themselves.

### 3. Use the closing price for NEVER SUM billing

Current billing can charge the running balance separately at multiple price-line endpoints after a mid-period price change.

For the same subscription and meter, evaluate the closing balance once and use the price effective immediately before the billing-period end. Earlier price lines remain historical records and do not produce separate usage charges under this rule. No duration proration or time weighting is included.

Before implementation, define the behavior when a price ends without a replacement and when the subscription terminates mid-period, including the effective closing boundary.
