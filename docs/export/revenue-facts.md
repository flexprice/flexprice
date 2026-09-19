# Revenue Facts Export

`GET /v1/analytics/revenue-facts/export` streams your revenue facts as CSV.
Requires the revenue analytics setting to be enabled for your tenant.

## How to use it

- **First run (snapshot):** call with no parameters — every row is returned.
- **Incremental runs:** remember the maximum `computed_at` you received and
  pass it back as `?since=<RFC3339>`. Only rows recomputed after that instant
  are returned. Re-running with an older watermark is safe: rows are
  identified by `id`, and a re-exported row simply carries a higher `version`
  — keep the latest `version` per `id`.
- Rows are ordered by `(computed_at, id)`. If a download is interrupted,
  re-run from your last complete watermark.

## Reading the data

- `status = FINAL` rows are booked revenue, stamped with the invoice that
  fixed them. `status = PROVISIONAL` rows are the in-progress view of the
  current billing period and are recomputed (same `id`, higher `version`) as
  usage arrives — including late or backdated usage. Never mix the two in one
  metric without labeling.
- **Always sum with reverts included.** A voided invoice's rows are negated
  by `is_revert = true` twins; filtering reverts out overstates revenue.
- One row is one calendar `day` of one subscription line item's revenue for
  one `revenue_source`. Grant-billed (entitlement grant) usage is split per
  day too: days inside a grant's quota-crossed window are billed, the rest
  show as entitled quantity with zero net. `decomposition_mode = marginal` rows are true per-day
  amounts; `period_only` rows carry a whole period's amount on a single day
  (the period start for advance charges, the period end for arrear, true-up
  and overage charges).
- The per-row identity for marginal usage rows:
  `net_amount = usage_at_list_rate + tier_delta − entitlement_amount − line_discount − invoice_discount`.
- Join back to billing through `invoice_id` / `invoice_line_item_id`.
- Absence of rows means revenue analytics is not enabled or not yet computed —
  never that revenue was zero.
- Tax is excluded throughout; amounts are pre-tax in the subscription currency.

## Columns

| Column | Type | Meaning |
|---|---|---|
| `id` | string | Stable row identifier; re-exports of the same row keep it |
| `customer_id` | string | Customer the revenue belongs to (child customer for grouped invoicing) |
| `subscription_id` | string | The line item's own subscription |
| `sub_line_item_id` | string | Subscription line item; synthetic fallback ids for aggregate true-up/overage lines |
| `price_id` | string | Price billed; synthetic `trueup:`/`overage:` ids for commitment lines |
| `meter_id` | string | Meter for usage rows; empty otherwise |
| `aggregation_type` | string | Meter aggregation (SUM, MAX, …); empty for non-usage rows |
| `revenue_source` | string | `usage`, `fixed`, `commitment_trueup`, or `overage` |
| `period_start` / `period_end` | date | Billing period bounds; `period_end` is the inclusive last day |
| `day` | date | Calendar day this row is attributed to |
| `usage_at_list_rate` | decimal | Gross usage priced at the list (first-tier) rate; marginal usage rows only |
| `tier_delta` | decimal | Deviation from the list rate caused by graduated tiering; on grant-billed rows it also carries the alignment between measured window usage and the billed amount |
| `entitlement_amount` | decimal | Value of free (entitled) quantity at the list rate |
| `line_discount` | decimal | This row's share of the line item's own coupon discount |
| `invoice_discount` | decimal | This row's share of invoice-level discounts |
| `net_amount` | decimal | Revenue net of entitlements and discounts — the number to sum |
| `billable_qty` | decimal | Quantity billed after entitlements |
| `entitlement_qty` | decimal | Free quantity consumed |
| `decomposition_mode` | string | `marginal` (true per-day) or `period_only` (whole period on one day) |
| `currency` | string | Subscription currency (lowercase ISO 4217) |
| `status` | string | `PROVISIONAL` or `FINAL` |
| `is_revert` | bool | True for the negating twin written when an invoice is voided |
| `invoice_id` / `invoice_line_item_id` | string | Set once FINAL; the booking anchor |
| `computed_at` | timestamp | When this row was (re)computed — the export watermark |
| `version` | int | Bumped on every recompute of the same row |
