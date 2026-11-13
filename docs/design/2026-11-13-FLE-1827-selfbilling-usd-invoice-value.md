# FLE-1827 — Self-billing: invoice value in USD

- **Ticket:** [FLE-1827](https://linear.app/flexprice/issue/FLE-1827)
- **Date:** 2026-11-13
- **Author:** Ankit Malik
- **Status:** Approved

---

## 1. Why

Flexprice Cloud bills each tenant 0.5% of its finalized invoice value, priced in USD. The hourly push sends one `invoice_value` event per tenant, environment, currency and hour, with `amount` (the field the meter sums) in the invoice's own currency. A tenant that invoices ₹1,00,000 is metered as 100,000 USD.

## 2. Goals and non-goals

**Goals**
- `amount` on every `invoice_value` event is in USD.
- The event also carries the USD amount and the rate used, and keeps every other field in the invoice currency.
- Rates are managed in one place for every region, without a deploy.

**Non-goals**
- A historical rate per hour. The rate in force at push time is used, and since re-sends reuse event IDs, the first push of an hour fixes its rate.
- Market rates. The FX service rejects them today.
- Correcting hours already sent before this ships.

## 3. Approaches

| | Approach | Trade-off |
|---|---|---|
| **A (picked)** | FX rates on the billing tenant on Flexprice Cloud, read by each push through `sdk.FXRates.QueryFXRates` | One source for all regions; a rate change needs no deploy; one extra remote call per hourly run |
| B | Static `billing.usd_rates` table in each region's config | Least code, but every rate change is a redeploy in every region, and regions can drift |
| — | Market-rate feed | Not supported by the FX service; adds a vendor for a 0.5% fee |
| — | Each tenant's own FX rates | A new cross-tenant read, and tenants would set the rate of their own fee |
| — | One meter per currency on Cloud, rate in the price | Superseded: we convert before sending |

## 4. Design

### 4.1 Rates

- Ops create **tenant-scope, fixed** FX rates on the billing tenant's environment on Flexprice Cloud, one per invoice currency: `from = <currency>`, `to = usd`. FX rates mean `to = from × rate`, so INR→USD is about `0.012`.
- `billingClient.USDRates` makes one `QueryFXRates` call filtered on scope `tenant`, `to_currency = usd`, status `published`, and keys the result by lower-cased `from_currency`. Market-source rows are left out.

### 4.2 Conversion in `pushUsage`

- If any invoice row is non-USD, `USDRates` is called once. A USD-only push never calls it. A failed call fails the push like any other read failure, and the next hourly run re-sends the lookback window.
- USD rows use rate `1`.
- A row whose currency has no rate is still sent, with `amount = 0` and no `usd_amount` or `usd_rate`, so Flexprice Cloud keeps its invoice amounts and nothing is billed for it. One `Error` log names the missing currencies. See §5 for recovering these rows.
- `usd_amount = RoundToCurrencyPrecision(total × usd_rate, "usd")`, the helper invoice conversion already uses. A USD row keeps `total` exactly, unrounded.
- The billing tenant's own rows are never sent, so they never trigger a rate fetch.

### 4.3 Event properties

| Property | Value |
|---|---|
| `currency`, `total`, `subtotal`, `total_discount`, `total_prepaid_credits_applied`, `total_tax`, `amount_due`, `amount_paid` | Unchanged, in the invoice currency |
| `usd_rate` (new) | Rate used, `1` for USD |
| `usd_amount` (new) | `total × usd_rate` rounded to cents; exactly `total` for USD |
| `amount` | Equal to `usd_amount`; `0` when the currency has no rate |

`amount` was only ever a copy of `total` for the meter. A meter's aggregation field cannot be changed once created, so pointing the meter at `usd_amount` would need a new meter, a new plan price and a move of every subscription. Redefining `amount` needs no change on Flexprice Cloud, and past USD events already hold the right value.

The event ID keeps the invoice currency (`inv:<region>:<tenant>:<env>:<CURRENCY>:<hour>`), so rows of different currencies in one hour stay distinct and re-sends still dedupe.

## 5. Rollout

How Flexprice Cloud treats these events decides the order:

- Meter filters are applied when an event is processed into `meter_usage`, not when an invoice is computed. A filter change only affects events processed after it.
- Updating a meter only adds filter values (`mergeFilters`); it cannot remove a filter.
- A re-sent event ID replaces the stored event in raw `events` and, once processed, in `meter_usage` (billing reads it with `FINAL`). With Redis event-ID deduplication on, Cloud skips processing a repeated ID for 24 hours, taking the lock before the filter check. No API forces reprocessing of these events; the `/events/raw/reprocess` endpoints only re-transform Bento raw payloads.

Steps:

1. Create the FX rates on Flexprice Cloud for every currency tenants invoice in.
2. Deploy every region.
3. Remove the `currency = USD` filter from the `invoice_value` meter on Flexprice Cloud. The API cannot remove it, so it is a direct edit of the meter's `filters`. Do it after step 2 and before the next hourly push (pushes run at :20), so no region sends a native-currency `amount` past the meter, and no converted row is dropped.
4. If step 3 lands after a push, the non-USD rows of the hours between the deploy and the filter change were dropped by the filter. Re-ingest them on Flexprice Cloud with the same event IDs and timestamps once 24 hours have passed since they were sent (immediately if deduplication is off).

Rows sent with `amount = 0` (no rate) recover the same way: add the rate, then re-send the row with the same event ID and timestamp and its USD amount. With deduplication off, the next hourly re-send inside the 6-hour lookback does this on its own. With it on, the re-send is skipped, so re-ingest on Flexprice Cloud after 24 hours.

Hours sent before the deploy were dropped by the `currency = USD` filter when processed and stay out of billing. Whether to bill them for the current period is an ops decision.
