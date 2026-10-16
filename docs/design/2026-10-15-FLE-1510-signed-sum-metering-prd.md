# PRD: Storage-Style Metering with `SIGNED_SUM`

| | |
|---|---|
| Ticket | FLE-1510 |
| First customer | Optimality |
| Author | Ojas |
| Updated | 2026-10-15 |
| Replaces | The earlier storage-billing-with-`LATEST` and signed-delta drafts (not merged) |
| Build plan | [Implementation plan](./2026-10-15-FLE-1510-signed-sum-implementation.md) |

## 1. Summary

Some customers can only tell us what changed: "+10 GB uploaded", "−4 GB deleted". A new meter type, **`SIGNED_SUM`**, keeps those changes with their sign. With `reset_usage = NEVER` it maintains a running total for each customer: we bill the total held, and let the customer's application check that total against a limit before it allows more usage. With `BILLING_PERIOD` it is a sum that nets out negatives within each period.

Optimality needs this now. They will meter storage in Flexprice and use the entitlement as a hard limit on how many GB a customer may hold.

## 2. What the customer sends

- One event per change, **signed**: `+10` for an upload, `−4` for a delete.
- A **stable event id** per change, so a retry replaces itself instead of counting twice.
- A **usage price on the plan for the meter**, even when it is $0. Usage, and the checkpoint, are only computed for meters with a usage price, so a meter used only for a limit needs a $0 price.
- Existing customers do not send an opening balance. We rebuild their total from their event history once, at cutover.

## 3. The meter

`SIGNED_SUM` is like `SUM`, but negative values are kept. `SUM` is unchanged: it keeps setting negatives to 0, so no existing bill moves.

The meter's `reset_usage` picks one of two modes:

| | `NEVER` (storage, seats held) | `BILLING_PERIOD` (net usage per period) |
|---|---|---|
| Usage | Running total since the start | Net of this period's changes, from 0 |
| Kept per | Customer and meter, across subscriptions | Billing period |
| Checkpoint | Yes (section 4) | Not needed |

Billing works the same way in both modes: each line item bills its own dates at its own price (section 5). The mode only changes what a line item's usage is, and whether a checkpoint is kept.

Optimality uses `NEVER`.

**Exact everywhere, floored only on the bill.** Totals, checkpoints and the usage API keep the exact value, even when it is negative. Only the quantity being billed is raised to 0 when it is negative, because a bill cannot hand money back.

## 4. How the total stays cheap to read (`NEVER` only)

```
total now = last checkpoint + changes since that checkpoint
```

- A **checkpoint** is the total for one customer and meter: the sum of every change before its `aggregated_until`.
- It is written when the period's invoice is created at rollover: previous checkpoint plus that period's changes.
- So a read never adds up more than about one billing period of changes, however old the customer is.
- The first checkpoint for a customer is built once from their full history.
- **Late events are ignored.** A change timestamped before the latest checkpoint is not counted. We accept this for now.

## 5. The bill

Each line item bills only its own dates, at its own price. A price or plan change mid-period splits the period into line items; without one there is a single line item with a share of 1.

```
share    = the line item's days ÷ days in the period

NEVER:          billable = max(0, total at the line item's end − allowance) × share
BILLING_PERIOD: billable = max(0, net inside the line item − allowance × share)

bill     = billable × the line item's price
```

`NEVER` examples: $1 per GB, 10 GB included, 30-day month.

| Situation | Calculation | Bill |
|---|---|---|
| Held 46 GB, no change | (46 − 10) × 1 × $1 | **$36** |
| No allowance | 46 × $1 | **$46** |
| Price changed to $0.9 on day 15; 50 GB held then, 46 GB at month end | (50 − 10) × ½ × $1 + (46 − 10) × ½ × $0.9 | **$36.20** |

- **Each month bills again** for what is still held, like rent.
- This follows Lago's prorated recurring charges. It uses the total at each line item's end rather than a day-by-day average.

`BILLING_PERIOD` example: $1 per call, changed to $0.9 on day 15, 10 included, +100 before the change and −30 after it.

| Line item | Calculation | Bill |
|---|---|---|
| Old (days 1–15) | (100 − 5) × $1 | **$95** |
| New (days 15–30) | max(0, −30 − 5) × $0.9 | **$0** |

- **The bill is never negative.** A negative billable quantity becomes 0, per line item. Stored values stay exact.

## 6. Limits (entitlements)

The entitlement sets how much the customer may hold.

| Field | Meaning |
|---|---|
| `grant_quota` | The ceiling, for example 100 GB |
| `is_soft_limit` | `false`: hard limit, the customer's app should block. `true`: report only. |
| `aggregation_mode` | `additive`: plan and overrides add up |

- No grant rows are opened for `SIGNED_SUM` features. The ceiling is read from the entitlement config whenever it is needed.
- `/customer/usage` returns the current exact total (`NEVER`) or this period's net (`BILLING_PERIOD`), the ceiling, and what remains. Remaining **goes back up** after deletes or refunds.
- A subscription that starts mid-period gets the full ceiling straight away.
- Flexprice reports. The customer's application does the blocking.
- The older `usage_limit` and `usage_reset_period` fields are not used.

## 7. Out of scope

- Addons changing the ceiling.
- Alerts at 80% or 100%.
- Billing on a time-weighted average (GB-months).
- Counting late events behind a checkpoint.
- Making plain `SUM` accept negatives.

## 8. How others do it

| | Metronome | Orb | Lago | Flexprice `SIGNED_SUM` |
|---|---|---|---|---|
| Negatives in a sum | Yes; a total of −30 billed as −$30 | Accepted by ingestion | Yes | Yes, billed total floored at 0 |
| Total carried across periods | No; storage is sent as totals (`LATEST`) | No | Yes (`recurring`) | Yes |
| Limit check | Webhook after a threshold | None found | None found | Synchronous check |

We tested Metronome in its sandbox: +100, −50, −80 gave a usage of −30 and a draft invoice of −$30.

## 9. Open points

1. **Optimality's history.** Their negative changes were not found in raw `events` in the region checked. Find where they are stored before the rebuild.
