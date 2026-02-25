# Mixed-Interval Billing – Implementation Scope

**What we implement this release.** Full context: [mixed-interval-billing.md](mixed-interval-billing.md). Flow: [mixed-interval-billing-logic-diagrams.md](mixed-interval-billing-logic-diagrams.md).

---

## 1. Scope

| In scope | Out of scope |
|----------|--------------|
| **CalculateFixedCharges** – all inclusion and charge logic for fixed line items | ClassifyLineItems, line item creation, usage billing, plan/price validation, classification wiring |

**Principle:** All mixed-interval logic for **fixed** charges is **inside CalculateFixedCharges**. Same inputs as today `(ctx, sub, periodStart, periodEnd)`; no new parameters. Caller passes filtered sub + period; CalculateFixedCharges decides per item: include or skip, and how to compute amount/period.

---

## 2. What we do not change

- **ClassifyLineItems** – unchanged (advance/arrear buckets only; no EXCLUDE/INCLUDE_*).
- **PrepareSubscriptionInvoiceRequest, CalculateCharges, CalculateAllCharges** – no new parameters.
- **subscription.go** – no line item creation or validation changes.
- **Usage billing** – no edits in billing.go for usage.
- **Line item creation** – no setting of `billing_period`/`billing_period_count` from price; invoice logic **reads** them (default count = 1).

---

## 3. Algorithm (all in CalculateFixedCharges)

For each **fixed** line item:

1. **Active check:** Skip if not FIXED, or StartDate > periodEnd, or EndDate < periodStart.
2. **Inclusion (Algorithm A + B):** `invoice_period_days` = CalendarDaysBetween(periodStart, periodEnd). `line_item_interval_days` = LineItemIntervalDays(item.StartDate, anchor, item.BillingPeriodCount, item.BillingPeriod). Compare: **shorter** → INCLUDE_PRORATED_SHORTER; **equal** → INCLUDE_FULL; **longer** → Algorithm B (IsLineItemIntervalEnd) → INCLUDE_LONGER_ALIGNED or EXCLUDE.
3. **If EXCLUDE:** skip (no invoice line).
4. **Resolve price;** then by type: **PRORATED_SHORTER** → F1/F2 (effective_days, quantity, amount), period = effective start → period_end; **LONGER_ALIGNED** → F4 (interval_start), full amount, period = interval_start → period_end; **FULL** → CalculateCost + applyProrationToLineItem, period = periodStart..periodEnd.
5. Round with RoundToCurrencyPrecision; build invoice line; append.

Algorithms and formulas: main PRD [§5.6](mixed-interval-billing.md#56-formulas-reference), [§5.7](mixed-interval-billing.md#57-algorithms-implementation-reference).

---

## 4. Deliverables

- **Date helpers** ([internal/types/date.go](internal/types/date.go)): IsLineItemIntervalEnd, LineItemIntervalDays, CalendarDaysBetween, EffectiveDaysForProration (add if missing). NextBillingDate / PreviousBillingDate unchanged.
- **CalculateFixedCharges** ([internal/service/billing.go](internal/service/billing.go)): implement flow above; signature unchanged; backward compatible when interval equals invoice period (INCLUDE_FULL, existing behavior).
- **No other code changes** – no ClassifyLineItems edits, no new params on CalculateCharges/PrepareSubscriptionInvoiceRequest.
