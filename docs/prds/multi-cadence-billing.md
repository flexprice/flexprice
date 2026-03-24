# PRD: Multi-Cadence Billing (Mixed Billing Periods)

**Status:** Implementation Ready  
**Author:** AI Assistant  
**Date:** 2026-03-14  
**Epic:** Multi-Cadence Billing Support

---

## Executive Summary

Enable subscriptions to have line items with different billing periods (e.g., monthly + quarterly + annual charges on the same subscription). This allows flexible pricing models like "monthly platform fee + quarterly support + annual license" without requiring separate subscriptions.

**Key behaviors:**

- Subscription billing period = **minimum** cadence across all line items
- Invoices generated at subscription billing period boundary
- Line items appear on invoices when their period boundaries align with the invoice period
- Proration is **disabled** for multi-cadence subscriptions (mutual exclusion)
- Cancellation supports multiple strategies: immediate, min_period_end, max_period_end

---

## Table of Contents

1. [Problem Statement](#1-problem-statement)
2. [Goals & Non-Goals](#2-goals--non-goals)
3. [Core Concepts](#3-core-concepts)
4. [Detailed Examples](#4-detailed-examples)
5. [Invoice Generation Logic](#5-invoice-generation-logic)
6. [Cancellation Behavior](#6-cancellation-behavior)
7. [Proration Rules](#7-proration-rules)
8. [Preview Invoice](#8-preview-invoice)
9. [Validation Rules](#9-validation-rules)
10. [API Changes](#10-api-changes)
11. [Edge Cases](#11-edge-cases)
12. [Implementation Notes](#12-implementation-notes)

---

## 1. Problem Statement

### Current Limitation

Currently, all prices on a subscription must share the same billing period. A subscription can be monthly OR quarterly OR annual, but not a mix. This forces customers into suboptimal patterns:

- Creating multiple subscriptions for the same customer (monthly + annual)
- Forcing all charges to align to a single cadence
- Manual workarounds and custom invoicing

### Real-World Use Cases

1. **SaaS Platform with Tiered Support**
  - Monthly: Usage-based API calls
  - Quarterly: Support retainer
  - Annual: Enterprise license fee
2. **Infrastructure Provider**
  - Monthly: Compute and storage usage
  - Quarterly: Dedicated support engineer
  - Annual: Reserved capacity commitment
3. **B2B Software Company**
  - Monthly: Per-seat licenses
  - Quarterly: Professional services retainer
  - Half-yearly: Compliance & security audit
4. **Enterprise SaaS**
  - Monthly: Base platform access
  - Quarterly: Training credits
  - Annual: Custom integrations package

### Competitor Context

- **Orb**: Full multi-cadence support (subscription term = max cadence, billing period = min cadence)
- **Stripe**: "Mixed interval subscriptions" with flexible billing mode (requires API version 2025-06-30.basil+)
- **Metronome**: Limited support (single billing frequency per subscription)

---

## 2. Goals & Non-Goals

### Goals

✅ **Enable multi-cadence subscriptions**: Allow prices with different billing periods on the same subscription  
✅ **Invoice generation**: Generate invoices at subscription period boundaries with correct line items  
✅ **Cancellation handling**: Support immediate, min_period_end, and max_period_end cancellation strategies  
✅ **Interval alignment**: Validate that all billing periods are valid multiples of the smallest period  
✅ **Proration mutual exclusion**: Disable proration for multi-cadence subscriptions with clear validation  
✅ **Backward compatibility**: Existing single-cadence subscriptions continue to work unchanged  

### Non-Goals

❌ **Usage-based multi-cadence**: Mixed billing periods for usage charges (usage is always arrear within subscription period)  
❌ **Custom proration**: Proration is disabled for multi-cadence (design decision for v1)  
❌ **Mid-period billing cycle changes**: Changing billing period on existing multi-cadence subscriptions  
❌ **Phase-level cadence**: Different cadences per subscription phase (future enhancement)  

---

## 3. Core Concepts

### 3.1 Subscription Billing Period

The **subscription billing period** is the minimum billing period across all line items. This determines how often invoices are generated.

**Example:**

- Line items: Monthly ($10), Quarterly ($100), Annual ($1200)
- Subscription billing period = **MONTHLY** (the smallest)
- Invoices generated: monthly

### 3.2 Invoice Cadence (ARREAR vs ADVANCE)

Each price (and thus line item) has an `invoice_cadence`:

- **ARREAR**: Charge at the **end** of the period (after service delivery)
- **ADVANCE**: Charge at the **start** of the period (before service delivery)

**Example:**

- Monthly platform fee (ADVANCE): Charged on Jan 1 for Jan 1-Feb 1
- Quarterly support (ARREAR): Charged on Apr 1 for Jan 1-Apr 1

### 3.3 Line Item Period vs Subscription Period

Each line item has its own billing period lifecycle:


| Line Item | Billing Period | Period 1                  | Period 2      | Period 3      |
| --------- | -------------- | ------------------------- | ------------- | ------------- |
| Monthly   | MONTHLY        | Jan 1 - Feb 1             | Feb 1 - Mar 1 | Mar 1 - Apr 1 |
| Quarterly | QUARTERLY      | Jan 1 - Apr 1             | Apr 1 - Jul 1 | Jul 1 - Oct 1 |
| Annual    | ANNUAL         | Jan 1 - Jan 1 (next year) | —             | —             |


### 3.4 Interval Alignment

All billing periods must be **multiples** of the smallest billing period (following Stripe's constraint).

**Valid combinations:**

- ✅ Monthly + Quarterly (3 months) + Half-yearly (6 months) + Annual (12 months)
- ✅ Quarterly (3 months) + Half-yearly (6 months) + Annual (12 months)
- ✅ Monthly + Annual

**Invalid combinations:**

- ❌ Weekly + Monthly (not multiples)
- ❌ Bi-monthly (2 months) + Quarterly (3 months) (not multiples)
- ❌ Monthly + 5-monthly (not a standard period)

---

## 4. Detailed Examples

### Example 1: SaaS Platform (Monthly + Quarterly + Half-Yearly)

**Scenario:**

- Subscription starts: Jan 1, 2026 (anniversary billing)
- Line item 1: Monthly platform access, $50, ARREAR
- Line item 2: Quarterly support package, $300, ARREAR
- Line item 3: Half-yearly compliance audit, $1000, ARREAR

**Subscription billing period:** MONTHLY (smallest cadence)

**Invoice Schedule:**


| Invoice Date | Period        | Line Items Included                            | Amounts            | Total |
| ------------ | ------------- | ---------------------------------------------- | ------------------ | ----- |
| Feb 1        | Jan 1 - Feb 1 | Monthly platform                               | $50                | $50   |
| Mar 1        | Feb 1 - Mar 1 | Monthly platform                               | $50                | $50   |
| Apr 1        | Mar 1 - Apr 1 | Monthly platform + Quarterly support (Q1 ends) | $50 + $300         | $350  |
| May 1        | Apr 1 - May 1 | Monthly platform                               | $50                | $50   |
| Jun 1        | May 1 - Jun 1 | Monthly platform                               | $50                | $50   |
| Jul 1        | Jun 1 - Jul 1 | Monthly + Quarterly (Q2) + Half-yearly (H1)    | $50 + $300 + $1000 | $1350 |
| Aug 1        | Jul 1 - Aug 1 | Monthly platform                               | $50                | $50   |
| Sep 1        | Aug 1 - Sep 1 | Monthly platform                               | $50                | $50   |
| Oct 1        | Sep 1 - Oct 1 | Monthly platform + Quarterly support (Q3 ends) | $50 + $300         | $350  |


**Reasoning:**

- Monthly line item appears on every invoice (period ends monthly)
- Quarterly appears every 3 months (Apr 1, Jul 1, Oct 1)
- Half-yearly appears every 6 months (Jul 1, Jan 1 next year)

---

### Example 2: Infrastructure Provider (Monthly Usage + Annual License, Mixed Cadence)

**Scenario:**

- Subscription starts: Jan 15, 2026 (anniversary billing, mid-month start)
- Line item 1: Monthly compute usage, usage-based (metered), ARREAR
- Line item 2: Annual enterprise license, $12,000, ADVANCE

**Subscription billing period:** MONTHLY

**Invoice Schedule:**


| Invoice Date      | Period          | Line Items Included                    | Amounts        | Total   |
| ----------------- | --------------- | -------------------------------------- | -------------- | ------- |
| Jan 15 (creation) | —               | Annual license (ADVANCE)               | $12,000        | $12,000 |
| Feb 15            | Jan 15 - Feb 15 | Monthly usage (actual usage)           | $450           | $450    |
| Mar 15            | Feb 15 - Mar 15 | Monthly usage                          | $520           | $520    |
| ...               | ...             | ...                                    | ...            | ...     |
| Jan 15, 2027      | Dec 15 - Jan 15 | Monthly usage + Annual license renewal | $480 + $12,000 | $12,480 |


**Reasoning:**

- ADVANCE annual license charged immediately at subscription creation
- Monthly usage charged in ARREAR at each monthly boundary
- Annual license renews Jan 15, 2027 (one year from start)

---

### Example 3: B2B Software with Tiered Support (Quarterly Base + Annual Premium)

**Scenario:**

- Subscription starts: Mar 1, 2026 (anniversary)
- Line item 1: Quarterly base support, $500, ARREAR
- Line item 2: Annual premium support, $5,000, ADVANCE

**Subscription billing period:** QUARTERLY (smallest cadence)

**Invoice Schedule:**


| Invoice Date     | Period        | Line Items Included                     | Amounts       | Total  |
| ---------------- | ------------- | --------------------------------------- | ------------- | ------ |
| Mar 1 (creation) | —             | Annual premium (ADVANCE)                | $5,000        | $5,000 |
| Jun 1            | Mar 1 - Jun 1 | Quarterly base support (ARREAR)         | $500          | $500   |
| Sep 1            | Jun 1 - Sep 1 | Quarterly base support                  | $500          | $500   |
| Dec 1            | Sep 1 - Dec 1 | Quarterly base support                  | $500          | $500   |
| Mar 1, 2027      | Dec 1 - Mar 1 | Quarterly base + Annual premium renewal | $500 + $5,000 | $5,500 |


**Reasoning:**

- Subscription ticks quarterly (not monthly) since smallest period is quarterly
- ADVANCE annual charged immediately
- ARREAR quarterly charged at each quarter end

---

### Example 4: Enterprise SaaS (Monthly + Quarterly + Annual, All ADVANCE)

**Scenario:**

- Subscription starts: Jan 1, 2026
- Line item 1: Monthly seats, $200/month, ADVANCE
- Line item 2: Quarterly training credits, $600/quarter, ADVANCE
- Line item 3: Annual custom integrations, $3,600/year, ADVANCE

**Subscription billing period:** MONTHLY

**Invoice Schedule:**


| Invoice Date     | Period        | Line Items Included                      | Amounts              | Total  |
| ---------------- | ------------- | ---------------------------------------- | -------------------- | ------ |
| Jan 1 (creation) | —             | All three (ADVANCE for first periods)    | $200 + $600 + $3,600 | $4,400 |
| Feb 1            | Feb 1 - Mar 1 | Monthly seats (next month ADVANCE)       | $200                 | $200   |
| Mar 1            | Mar 1 - Apr 1 | Monthly seats (next month ADVANCE)       | $200                 | $200   |
| Apr 1            | Apr 1 - May 1 | Monthly + Quarterly (Q2 ADVANCE)         | $200 + $600          | $800   |
| May 1            | May 1 - Jun 1 | Monthly                                  | $200                 | $200   |
| Jun 1            | Jun 1 - Jul 1 | Monthly                                  | $200                 | $200   |
| Jul 1            | Jul 1 - Aug 1 | Monthly + Quarterly (Q3 ADVANCE)         | $200 + $600          | $800   |
| ...              | ...           | ...                                      | ...                  | ...    |
| Jan 1, 2027      | Jan 1 - Feb 1 | Monthly + Quarterly + Annual (all renew) | $200 + $600 + $3,600 | $4,400 |


**Reasoning:**

- All ADVANCE charges bill at the start of their respective periods
- Subscription creation invoice includes first period of all three line items
- Quarterly appears every 3 months (Jan, Apr, Jul, Oct)
- Annual renews once per year

---

## 5. Invoice Generation Logic

### 5.1 Which Line Items Appear on an Invoice?

For each subscription invoice generated at `periodStart` to `periodEnd` (the subscription billing period):

**For ARREAR line items:**

- Include if the line item's **period end** falls within `(periodStart, periodEnd]`
- Boundary: start exclusive, end inclusive
- Example: Monthly invoice Mar 1 - Apr 1 includes quarterly item whose period ends Apr 1

**For ADVANCE line items:**

- Include if the line item's **period start** falls within `[periodStart, periodEnd)`
- Boundary: start inclusive, end exclusive
- Example: Monthly invoice Mar 1 - Apr 1 includes quarterly item whose period starts Mar 1

### 5.2 Reference Points

Invoices are generated at different "reference points" during the subscription lifecycle:


| Reference Point | When                  | Charges Included                                |
| --------------- | --------------------- | ----------------------------------------------- |
| `period_start`  | Subscription creation | ADVANCE charges only (for first period)         |
| `period_end`    | End-of-period renewal | Current ARREAR + Next ADVANCE                   |
| `preview`       | Preview invoice API   | Current ARREAR (prorated to now) + Next ADVANCE |
| `cancel`        | Cancellation          | Current ARREAR only (prorated to cancel date)   |


### 5.3 Line Item Service Period

Each invoice line item has a `period_start` and `period_end` that reflects the **line item's** billing period, not the subscription's:

**Example:** Monthly subscription (Jan 1 start) with quarterly ARREAR line item

**Feb 1 invoice:**

- Period: Feb 1 - Mar 1 (subscription period)
- Quarterly line item: **not included** (period end is Apr 1, not in this window)

**Apr 1 invoice:**

- Period: Apr 1 - May 1 (subscription period)
- Quarterly line item: **included**
  - Service period on invoice line: Jan 1 - Apr 1 (full quarterly period)
  - Amount: full quarterly charge (no proration)

### 5.4 Matching Algorithm

Implemented in `FindMatchingLineItemPeriodForInvoice()`:

1. Generate all line-item periods from `item.StartDate` up to `periodEnd + one-line-item-period`
2. For each line-item period, check:
  - **ADVANCE**: Is `lineItemPeriod.Start` in `[periodStart, periodEnd)`?
  - **ARREAR**: Is `lineItemPeriod.End` in `(periodStart, periodEnd]`?
3. If match found, return the matching line-item period (used as service period on invoice)
4. If no match, skip the line item for this invoice

---

## 6. Cancellation Behavior

### 6.1 Cancellation Strategies

Three cancellation options for multi-cadence subscriptions:

#### 6.1.1 Immediate Cancellation

**Behavior:**

- Subscription ends immediately
- Generate final invoice with ARREAR charges prorated to cancellation date
- All line items (monthly, quarterly, annual) prorated based on consumed time within their respective periods

**Example:** Cancel on Feb 15 (subscription started Jan 1)


| Line Item      | Own Period                         | Days Used | Proration | Amount  |
| -------------- | ---------------------------------- | --------- | --------- | ------- |
| Monthly $50    | Feb 1 - Mar 1 (28 days)            | 15 days   | 15/28     | $26.79  |
| Quarterly $300 | Jan 1 - Apr 1 (90 days)            | 45 days   | 45/90     | $150.00 |
| Annual $1200   | Jan 1 - Jan 1 next year (365 days) | 45 days   | 45/365    | $147.95 |


**Final invoice total:** $324.74

#### 6.1.2 Min Period End Cancellation

**Behavior:**

- Cancel at the **next subscription period end** (earliest boundary)
- Prorate longer-cadence line items to the subscription period end
- Shorter-cadence items charge full amount up to that date

**Example:** Cancel with `min_period_end` on Feb 15 (next subscription period end = Mar 1)

**Mar 1 final invoice:**


| Line Item      | Own Period         | Period Used          | Proration | Amount  |
| -------------- | ------------------ | -------------------- | --------- | ------- |
| Monthly $50    | Feb 1 - Mar 1      | Full period          | 1.0       | $50.00  |
| Quarterly $300 | Jan 1 - Apr 1      | 2 months / 3 months  | 60/90     | $200.00 |
| Annual $1200   | Jan 1 - Jan 1 next | 2 months / 12 months | 60/365    | $197.26 |


**Final invoice total:** $447.26

#### 6.1.3 Max Period End Cancellation

**Behavior:**

- Cancel at the **latest line-item period end** (longest cadence)
- Subscription remains active until then
- All invoices continue normally until the max period end

**Example:** Cancel with `max_period_end` on Feb 15

**What happens:**

- Subscription continues until the **annual** line item period ends (Jan 1, 2027)
- All monthly and quarterly invoices continue as normal
- Final invoice on Jan 1, 2027 includes the last monthly + quarterly + annual charges

**Why this matters:** Honors long-term commitments (e.g., annual contracts) without forcing proration.

### 6.2 ADVANCE Items and Cancellation

If an ADVANCE charge was already billed and cancellation happens mid-period:

- **Immediate cancellation**: Credit the unused portion to customer balance
- **Min/Max period end**: No credit needed (service continues until cancel date)

**Example:** Annual ADVANCE $1200 billed Jan 1, cancel immediate Feb 15

- Used: 45 days
- Unused: 320 days
- Credit to customer balance: $1200 * (320/365) = $1052.05

---

## 7. Proration Rules

### 7.1 Core Rule: Mutual Exclusion

**Proration and multi-cadence are mutually exclusive.**

If a subscription has line items with different billing periods, `proration_behavior` **must** be `none`. Conversely, if `proration_behavior = create_prorations`, all line items **must** have the same billing period.

### 7.2 Rationale

Proration is calculated against a subscription's billing period. When line items have different periods:

- Which period do you prorate against?
- A quarterly item starting mid-month has no clear proration baseline

This matches Stripe's approach: mixed-interval subscriptions require `proration_behavior: none`.

### 7.3 Validation Points

**1. Subscription Creation:**

```
If prices have different billing periods AND proration_behavior = create_prorations
→ Error: "Proration is not supported for subscriptions with mixed billing periods"
```

**2. Subscription Cancellation:**

```
If subscription has mixed periods AND cancel request has proration_behavior = create_prorations
→ Error: "Proration is not supported for subscriptions with mixed billing periods"
```

**3. Subscription Change (Upgrade/Downgrade):**

```
If target plan would result in mixed periods AND proration_behavior = create_prorations
→ Error: "Proration is not supported for subscriptions with mixed billing periods"
```

**4. Runtime Safety Net:**

```
In applyProrationToLineItem():
If subscription.HasMixedBillingPeriods() → skip proration (return original amount)
```

### 7.4 Examples

**Invalid:**

```json
{
  "customer_id": "cust_123",
  "plan_id": "plan_multi_cadence",
  "proration_behavior": "create_prorations"  // ❌ Not allowed
}
```

**Valid:**

```json
{
  "customer_id": "cust_123",
  "plan_id": "plan_multi_cadence",
  "proration_behavior": "none"  // ✅ Allowed
}
```

---

## 8. Preview Invoice

### 8.1 Behavior

Preview invoices follow the **Orb model**: show only charges that would appear on the next actual invoice.

**For ARREAR items:**

- Include only if the line item's period end falls in the current subscription period window
- Prorate to "now" (current date)

**For ADVANCE items:**

- Include if the line item's period start falls in the next subscription period

### 8.2 Example

**Subscription:** Monthly + Quarterly + Annual (all ARREAR), started Jan 1  
**Preview requested:** Feb 15

**Preview invoice shows:**

- Monthly item: prorated Jan 1 - Feb 15 (15 days of Feb period)
- Quarterly item: **not included** (period end is Apr 1, outside current monthly window)
- Annual item: **not included** (period end is Jan 1 next year, outside current monthly window)

**Reasoning:** Next actual invoice (Mar 1) will only inclue the monthly item. Preview reflects this.

### 8.3 Alternative (Stripe-style, not implemented)

Stripe's mixed-interval preview shows **all accrued charges** across all line-item periods:

- Monthly: prorated to Feb 15
- Quarterly: prorated to Feb 15 (45 days of 90-day period)
- Annual: prorated to Feb 15 (45 days of 365-day period)

This is a **design decision** -- Flexprice uses Orb-style (window-based) for v1. Stripe-style could be a future enhancement.

---

## 9. Validation Rules

### 9.1 Interval Alignment Validation

**Rule:** All billing periods must be multiples of the smallest billing period.

**Implementation:**

```go
func IsBillingPeriodMultiple(longer, shorter BillingPeriod) bool {
    longerMonths := BillingPeriodToMonths(longer)
    shorterMonths := BillingPeriodToMonths(shorter)
    return longerMonths % shorterMonths == 0
}
```

**Valid examples:**

- ✅ MONTHLY (1) + QUARTERLY (3) + HALF_YEARLY (6) + ANNUAL (12)
- ✅ QUARTERLY (3) + ANNUAL (12)

**Invalid examples:**

- ❌ MONTHLY (1) + BI_MONTHLY (2) + QUARTERLY (3)  // 3 % 2 != 0
- ❌ WEEKLY (0.25) + MONTHLY (1)  // weeks and months don't align cleanly

### 9.2 Proration Mutual Exclusion

**Rule:** `HasMixedBillingPeriods() == true` ⇒ `ProrationBehavior == none`

**Validation locations:**

- Subscription creation (after line items built)
- Subscription cancellation (before proration calculation)
- Subscription change (after target plan resolved)
- Runtime guard (in `applyProrationToLineItem`)

### 9.3 Currency Consistency

**Rule:** All prices must match subscription currency (unchanged).

### 9.4 Price Scope

**Rule:** Multi-cadence is allowed for:

- ✅ Plan prices (different billing periods across plan prices)
- ✅ Addon prices (addons can have different billing periods)
- ✅ Subscription-level price overrides: billing period must be **equal to or greater than** subscription billing period (e.g., monthly sub can have overrides that are monthly, quarterly, half-yearly, or annual)

---

## 10. API Changes

### 10.1 Subscription Creation

**No breaking changes.** Existing API works as-is:

```json
POST /v1/subscriptions
{
  "customer_id": "cust_123",
  "plan_id": "plan_multi_cadence",
  "billing_period": "MONTHLY",
  "billing_cadence": "RECURRING",
  "proration_behavior": "none"  // Required for multi-cadence
}
```

**What changes:**

- `billing_period` on subscription now represents the **minimum** cadence
- Line items inherit `billing_period` from their **price**, not subscription
- `filterValidPricesForSubscription` now allows prices with `billing_period >= subscription.billing_period`

### 10.2 Subscription Cancellation

**New enum values for `cancel_at`:**

```json
POST /v1/subscriptions/{subscription_id}/cancel
{
  "cancellation_type": "end_of_period",
  "cancel_at": "min_period_end",  // New: cancel at next subscription period end
  "proration_behavior": "none"
}
```

**Options:**

- `immediate`: Cancel now
- `min_period_end`: Cancel at next subscription period end (monthly boundary for monthly sub)
- `max_period_end`: Cancel at the latest line-item period end (e.g., annual boundary if annual item exists)

### 10.3 Preview Invoice

**No API changes.** Existing endpoint:

```
GET /v1/subscriptions/{subscription_id}/preview_invoice
```

Returns line items that would appear on the next actual invoice (window-based, Orb-style).

### 10.4 Response Schema Changes

**Subscription object:**

```json
{
  "id": "sub_123",
  "billing_period": "MONTHLY",  // Now: min cadence across line items
  "has_mixed_billing_periods": true,  // New field (computed)
  "proration_behavior": "none",  // Must be "none" if mixed periods
  "line_items": [
    {
      "id": "li_1",
      "billing_period": "MONTHLY",  // Now: from price, not subscription
      "invoice_cadence": "ARREAR"
    },
    {
      "id": "li_2",
      "billing_period": "QUARTERLY",  // Different from subscription
      "invoice_cadence": "ARREAR"
    }
  ]
}
```

---

## 11. Edge Cases

### 11.1 Subscription Mid-Month Start with Multi-Cadence

**Scenario:** Subscription starts Jan 15 with monthly + quarterly ARREAR items

**Behavior:**

- Monthly item: period = Jan 15 - Feb 15
- Quarterly item: period = Jan 15 - Apr 15
- First invoice (Feb 15): Only monthly item
- Fourth invoice (May 15): Monthly + Quarterly

**No special handling needed** -- anniversary billing aligns all periods to the start date.

### 11.2 Calendar Billing with Multi-Cadence

**Scenario:** Calendar billing, started Jan 15 with monthly + quarterly

**Behavior:**

- Monthly item: period = Feb 1 - Mar 1 (calendar-aligned)
- Quarterly item: period = Apr 1 - Jul 1 (calendar-aligned)
- First invoice (Feb 1): prorated monthly (Jan 15 - Feb 1)
- Invoice (Apr 1): Monthly + prorated quarterly (Jan 15 - Apr 1)

**Complexity:** Calendar alignment with multi-cadence requires careful proration on first invoices. **Recommendation:** Disable calendar billing for multi-cadence in v1, or require anniversary billing only.

### 11.3 Adding a Line Item Mid-Period

**Scenario:** Subscription exists with monthly item, add quarterly item on Feb 15

**Behavior:**

- New quarterly item: period starts Feb 15 (not Jan 1)
- First quarterly charge: May 15 (3 months from Feb 15)
- Does not align with other line items

**Recommendation:** When adding items to existing multi-cadence subscriptions, start date should align with subscription start or current period start for clean alignment.

### 11.4 Subscription Change (Upgrade/Downgrade)

**Scenario:** Change from Plan A (monthly only) to Plan B (monthly + quarterly)

**Behavior:**

- Plan change creates new subscription with Plan B's prices
- All line items start from the change date
- Subscription billing period recalculated to min of new line items

**Validation:** If `proration_behavior = create_prorations`, reject if target plan has mixed periods.

### 11.5 Line Item End Date Before Subscription End

**Scenario:** Monthly sub (ends Dec 31) with quarterly item that ends Sep 30

**Behavior:**

- Quarterly item appears on invoices up to Sep 30
- After Sep 30, only monthly item appears
- Subscription continues until Dec 31 with just monthly charges

**Implementation:** `FindMatchingLineItemPeriodForInvoice` respects `item.EndDate` -- no match if current date exceeds end date.

### 11.6 Zero-Amount Line Items

**Scenario:** Quarterly line item with $0 amount (e.g., trial, included)

**Behavior:**

- Still appears on invoices when period aligns
- Invoice line item shows $0.00
- Useful for transparency ("Included: Quarterly support - $0")

### 11.7 Mixed ARREAR and ADVANCE

**Scenario:** Monthly ADVANCE + Quarterly ARREAR

**Behavior:**

- Monthly ADVANCE: charged at start of each month (Jan 1 for Jan, Feb 1 for Feb, etc.)
- Quarterly ARREAR: charged at end of quarter (Apr 1 for Jan-Mar, Jul 1 for Apr-Jun)
- Subscription creation invoice: Only monthly ADVANCE (quarter hasn't completed yet)

**Valid and supported** -- ARREAR/ADVANCE can be mixed freely.

---

## 12. Implementation Notes

### 12.1 Code Changes Summary

**Already handled (per user confirmation):**

- `filterValidPricesForSubscription`: relaxed to allow different billing periods
- Line 231 subscription.go: `item.BillingPeriod = price.BillingPeriod`

**To implement:**

1. Interval alignment validation
2. `Subscription.BillingPeriod = min(line_items.BillingPeriod)`
3. Proration mutual exclusion (4 validation points)
4. `min_period_end` / `max_period_end` cancellation options
5. Cancellation proration for longer-period ARREAR items
6. Tests

### 12.2 Backward Compatibility

**Existing single-cadence subscriptions:**

- Continue to work unchanged
- `billing_period` remains as-is
- Proration continues to work (all line items same period)

**Migration:** None required. Existing subscriptions are single-cadence by definition.

### 12.3 Performance Considerations

**Invoice generation:**

- `FindMatchingLineItemPeriodForInvoice` generates periods up to `periodEnd + 1 line-item period`
- For annual items, this can be ~12 periods (monthly sub with annual item)
- Acceptable performance impact (O(n*m) where n=line items, m=periods per item)

**Optimization:** Cache line-item periods per subscription to avoid recalculating on every invoice.

### 12.4 Database Schema

**No schema changes required.**

Existing schema already supports:

- `subscription_line_items.billing_period` (per line item)
- `subscription_line_items.invoice_cadence` (per line item)
- `subscriptions.proration_behavior`

### 12.5 Testing Strategy

**Unit tests:**

- `IsBillingPeriodMultiple` (interval alignment)
- `HasMixedBillingPeriods` (detection)
- `FindMatchingLineItemPeriodForInvoice` (matching logic)

**Integration tests:**

- Create multi-cadence subscription → verify invoice schedule
- Cancel immediate/min/max → verify final invoice amounts
- Proration validation → verify rejection at all entry points

**End-to-end tests:**

- Full lifecycle: create → monthly invoices → quarterly alignment → annual alignment → cancel
- All 4 detailed examples from Section 4

---

## Appendix A: Glossary


| Term                      | Definition                                                                                    |
| ------------------------- | --------------------------------------------------------------------------------------------- |
| **Billing Period**        | The interval for a single billing cycle (MONTHLY, QUARTERLY, etc.)                            |
| **Billing Cadence**       | RECURRING or ONETIME (how often billing repeats)                                              |
| **Invoice Cadence**       | ARREAR (end of period) or ADVANCE (start of period)                                           |
| **Line Item Period**      | The specific period a line item covers (e.g., Jan 1 - Apr 1 for quarterly)                    |
| **Subscription Period**   | The subscription's billing interval (minimum line item period)                                |
| **Multi-Cadence**         | Subscription with line items having different billing periods                                 |
| **Mixed Billing Periods** | Synonym for multi-cadence                                                                     |
| **Interval Alignment**    | Requirement that all billing periods are multiples of the smallest                            |
| **Reference Point**       | The lifecycle moment when an invoice is generated (period_start, period_end, cancel, preview) |
| **Min Period End**        | Cancellation strategy: cancel at next subscription period boundary                            |
| **Max Period End**        | Cancellation strategy: cancel at latest line-item period boundary                             |


---

## Appendix B: Decision Log


| Decision                                  | Rationale                                                      | Date       |
| ----------------------------------------- | -------------------------------------------------------------- | ---------- |
| Subscription billing period = min cadence | Follows Orb model; ensures invoices at smallest granularity    | 2026-03-14 |
| Proration disabled for multi-cadence      | Avoids ambiguity; matches Stripe's approach                    | 2026-03-14 |
| Interval alignment required               | Ensures clean period boundaries; prevents edge cases           | 2026-03-14 |
| Orb-style preview (window-based)          | Simpler to understand; matches actual invoice                  | 2026-03-14 |
| Three cancellation strategies             | Flexibility for different business needs (immediate, min, max) | 2026-03-14 |


---

## Appendix C: Comparison with Competitors


| Feature                      | Flexprice (this PRD)          | Orb                        | Stripe                             |
| ---------------------------- | ----------------------------- | -------------------------- | ---------------------------------- |
| Multi-cadence support        | ✅ Yes                         | ✅ Yes                      | ✅ Yes (flexible billing)           |
| Subscription billing period  | Min cadence                   | Min cadence                | Min current_period_end             |
| Interval alignment           | Required (multiples)          | Implicit (works naturally) | Required (strict multiples)        |
| Proration with multi-cadence | ❌ Disabled (mutual exclusion) | ✅ Supported                | ⚠️ Configurable (but complex)      |
| Cancellation options         | 3 (immediate, min, max)       | 2 (immediate, end-of-term) | 2 (min_period_end, max_period_end) |
| Preview invoice              | Window-based (Orb-style)      | Window-based (draft)       | Item-level (all accrued)           |
| API version requirement      | None (v1)                     | N/A                        | 2025-06-30.basil+                  |


---

## Appendix D: Regression Tests (Existing Behavior Must Not Change)

These tests verify that all existing single-cadence and already-tested mixed-cadence behavior is preserved byte-for-byte after multi-cadence changes. Every test below maps to an existing test in the codebase that MUST continue passing with identical results.

---

### R.1 PrepareSubscriptionInvoiceRequest (billing_test.go:412)

Existing: Monthly sub with 3 line items (Fixed $10 ADVANCE, API Calls usage ARREAR, Archive Storage $5 fixed ARREAR). All line items have BillingPeriod = MONTHLY (same as sub). ProrationBehavior not set (defaults to none).

| # | Test Name | Reference Point | Expected Line Items | Expected Amount | Must Still Pass |
|---|---|---|---|---|---|
| 1 | period_start_reference_point | period_start | 1 (Fixed ADVANCE only) | $10 | Yes -- only ADVANCE included at period_start |
| 2 | period_end_reference_point | period_end | 3 (API Calls ARREAR + Archive ARREAR + Fixed ADVANCE next) | $25 | Yes -- current ARREAR + next ADVANCE |
| 3 | preview_reference_point | preview | 3 (same as period_end but skips "already invoiced" filter) | any | Yes -- all 3 included |
| 4 | existing_invoice_check_advance | period_start | 0 (advance already invoiced) | $0 | Yes -- dedup works |
| 5 | existing_invoice_check_arrear | period_end | 1 (only next ADVANCE, arrears already invoiced) | $10 | Yes -- dedup works |

**Regression risk:** Changes to `ClassifyLineItems` or `FilterLineItemsToBeInvoiced` could break the line item selection. The `HasMixedBillingPeriods()` check in `applyProrationToLineItem` must return false for same-period subscriptions so proration path remains unchanged.

---

### R.2 CalculateFixedCharges_MixedCadence (billing_test.go:729)

Existing: Monthly sub (start Jan 1) with Monthly $10 ADVANCE + Quarterly $300 ARREAR. Line items already have different BillingPeriods (M vs Q).

| # | Invoice Period | Expected Line Items | Expected Amounts | Must Still Pass |
|---|---|---|---|---|
| 1 | Apr 1 - May 1 | 1 (Monthly only) | $0-10 (monthly, may be prorated) | Yes -- Q arrear end (Apr 1) not in (Apr 1, May 1] |
| 2 | Mar 1 - Apr 1 | 2 (Monthly + Quarterly) | $300+ (quarterly full + monthly) | Yes -- Q end (Apr 1) in (Mar 1, Apr 1] |
| 3 | Quarterly line period = Jan 1 - Apr 1 | service period on invoice | Jan 1 - Apr 1 | Yes -- full quarterly period used |
| 4 | Quarterly amount | full $300 | no proration | Yes -- longer cadence always full amount |

**Regression risk:** This is the existing mixed-cadence test. `FindMatchingLineItemPeriodForInvoice` boundary logic must not change. The runtime proration guard (`HasMixedBillingPeriods`) should return true but only skips proration -- it must NOT change charge calculation.

---

### R.3 Scenario1_DailySub_12Invoices (billing_test.go:1075)

Existing: Daily sub (start Jan 1 2026) with 10 line items across 5 billing periods x 2 cadences (ADVANCE + ARREAR). This is the most complex existing mixed-cadence test.

Line items: Daily/Weekly/Monthly/Quarterly/Annual x ADVANCE($100/$200/$300/$400/$500) and ARREAR($200/$300/$400/$500/$600).

| Invoice # | Expected Total | Key Items Included |
|---|---|---|
| 1 (Jan 1-2) | $1700 | All advance (100+200+300+400+500) + daily arrear (200) |
| 2 (Jan 2-3) | $300 | Daily advance (100) + daily arrear (200) |
| 7 (Jan 7-8) | $600 | Daily adv (100) + daily arr (200) + weekly arrear (300, ends Jan 8) |
| 8 (Jan 8-9) | $500 | Daily adv (100) + weekly adv (200) + daily arr (200) |

**All 12 invoice totals must match:** `[1700, 300, 300, 300, 300, 300, 600, 500, 300, 300, 300, 300]`

**Regression risk:** This test exercises ADVANCE start-boundary [start, end) and ARREAR end-boundary (start, end] across all cadence combinations. Any change to `FindMatchingLineItemPeriodForInvoice` or `ClassifyLineItems` would break this.

---

### R.4 Scenario2_MonthlySub_12Invoices (billing_test.go:1106)

Existing: Monthly sub (start Jan 1 2026) with 3 ADVANCE line items: Monthly $300, Quarterly $400, Annual $500.

| Invoice # | Month | Expected Total | Key Items |
|---|---|---|---|
| 1 (Jan) | Jan 1-Feb 1 | $1200 | Monthly + Quarterly + Annual (all start in [Jan 1, Feb 1)) |
| 2 (Feb) | Feb 1-Mar 1 | $300 | Monthly only |
| 3 (Mar) | Mar 1-Apr 1 | $300 | Monthly only |
| 4 (Apr) | Apr 1-May 1 | $700 | Monthly + Quarterly (Q2 start in [Apr 1, May 1)) |
| ... | ... | ... | ... |

**All 12 totals must match:** `[1200, 300, 300, 700, 300, 300, 700, 300, 300, 700, 300, 300]`

**Regression risk:** ADVANCE matching at quarterly boundaries. Annual ADVANCE should only appear on invoice #1 (Jan).

---

### R.5 FindMatchingLineItemPeriodForInvoice (billing_test.go:1726)

Existing test with multiple sub-tests covering the period matching algorithm:

| # | Existing Sub-Test | Cadence | Period | Expected | Must Match |
|---|---|---|---|---|---|
| Various | ARREAR boundary checks | ARREAR | Various | Ok=true/false + correct period | Exact match on Ok, LineItemPeriodStart, LineItemPeriodEnd |
| Various | ADVANCE boundary checks | ADVANCE | Various | Ok=true/false + correct period | Exact match |
| Various | BillingPeriodCount > 1 | Both | Various | Correct multi-count periods | Exact match |

**Regression risk:** Any change to truncation (second-precision), boundary inclusivity/exclusivity, or `endDateBoundaryForMatching` would break these.

---

### R.6 ClassifyLineItems (billing_test.go:1956)

Existing test verifying line item classification into advance/arrear buckets:

| # | Setup | Expected Classification | Must Match |
|---|---|---|---|
| 1 | Same-period sub with advance + arrear items | Advance -> CurrentPeriodAdvance + NextPeriodAdvance; Arrear -> CurrentPeriodArrear | Exact |
| 2 | Longer-period line items | Uses FindMatchingLineItemPeriodForInvoice to determine bucket | Exact |

**Regression risk:** Adding `HasMixedBillingPeriods` to `applyProrationToLineItem` must not affect classification. Classification must remain purely based on cadence and period matching.

---

### R.7 FilterLineItemsToBeInvoiced (billing_test.go:1458)

Existing deduplication test ensuring already-invoiced line items are excluded.

| # | Scenario | Expected |
|---|---|---|
| 1 | No existing invoices | All line items pass through |
| 2 | Existing invoice covers some items | Covered items filtered out |
| 3 | Draft invoice exists | Considered as "already invoiced" |
| 4 | Different period invoice | Not filtered (different period) |

**Regression risk:** Must continue to correctly match by PriceID + period overlap. Multi-cadence line items with different service periods (e.g., quarterly Jan-Apr) must not be incorrectly matched against monthly invoices.

---

### R.8 applyProrationToLineItem (billing.go:2486)

Existing behavior that must be preserved:

| # | ProrationBehavior | Period Matches Sub? | PriceType | Expected |
|---|---|---|---|---|
| 1 | none | any | any | Return original amount |
| 2 | create_prorations | yes | USAGE | Return original amount (no proration for usage) |
| 3 | create_prorations | no | FIXED | Return original amount (period mismatch) |
| 4 | create_prorations | yes | FIXED | Apply proration calculation |

**New behavior added:** If `sub.HasMixedBillingPeriods() == true`, return original amount (safety net). This must NOT affect rows 1-4 when HasMixedBillingPeriods is false (single-cadence subscriptions).

---

### R.9 BillingPeriodOrder / BillingPeriodGreaterThan (types/price_test.go)

Existing tests that must continue passing:

| # | Function | Input | Expected |
|---|---|---|---|
| 1 | BillingPeriodOrder(DAILY) | DAILY | 1 |
| 2 | BillingPeriodOrder(WEEKLY) | WEEKLY | 2 |
| 3 | BillingPeriodOrder(MONTHLY) | MONTHLY | 3 |
| 4 | BillingPeriodOrder(QUARTERLY) | QUARTERLY | 4 |
| 5 | BillingPeriodOrder(HALF_YEARLY) | HALF_YEARLY | 5 |
| 6 | BillingPeriodOrder(ANNUAL) | ANNUAL | 6 |
| 7 | BillingPeriodGreaterThan(Q, M) | Q, M | true |
| 8 | BillingPeriodGreaterThan(M, Q) | M, Q | false |
| 9 | BillingPeriodGreaterThan(M, M) | M, M | false |
| 10 | Ordering invariant | each < next in list | true for all adjacent pairs |

**Regression risk:** New helpers (`IsBillingPeriodMultiple`, `BillingPeriodToMonths`, `MinBillingPeriod`) must not alter these existing functions or constants.

---

### R.10 CalculateUsageCharges (billing_test.go:2030+)

Usage calculation must remain completely unaffected by multi-cadence changes:

| # | Test | Must Still Pass |
|---|---|---|
| 1 | TestCalculateUsageChargesWithEntitlements | Usage charges with entitlement limits |
| 2 | TestCalculateUsageChargesWithDailyReset | Daily reset behavior |
| 3 | TestCalculateUsageChargesWithBucketedMaxAggregation | Bucketed meter aggregation |
| 4 | TestCalculateFeatureUsageCharges_SkipsInactiveLineItemWithSamePriceID | Inactive line item filtering |
| 5 | TestCalculateFeatureUsageCharges_MatchesActiveLineItemBySubscriptionLineItemID | Line item matching |
| 6 | TestCalculateFeatureUsageCharges_CumulativeCommitment | Commitment calculations |
| 7 | TestCalculateNeverResetUsage | Never-reset entitlements |

**Regression risk:** Usage charges are always ARREAR and always use the subscription billing period. Multi-cadence changes to fixed charge logic must not leak into usage charge paths. `HasMixedBillingPeriods` must not affect usage line items (only fixed).

---

### R.11 SubscriptionLineItemPeriodScenarios (billing_test.go:1305)

Existing test verifying line item periods are calculated correctly for various subscription configurations.

**Must pass unchanged.** Period calculation (`NextBillingDate`, `CalculateBillingPeriods`) must not be altered.

---

### R.12 Existing Subscription Service Tests

These subscription service tests must continue passing with the same behavior:

| # | Area | Key Assertion |
|---|---|---|
| 1 | Subscription creation with same-period prices | All prices included, line items created |
| 2 | Subscription cancellation with proration (same-period) | Proration calculated correctly |
| 3 | Subscription change with proration (same-period) | Change proration works |
| 4 | Calendar billing anchor calculation | Anchor dates unchanged |
| 5 | Anniversary billing period progression | Period start/end progression unchanged |

**Regression risk:** The new `ValidateBillingPeriodAlignment` validation runs ONLY when line items have different billing periods. Single-cadence subscriptions must skip this validation entirely and follow the exact same code path as before.

---

### R.13 Backward Compatibility Guarantees

| # | Guarantee | Verification |
|---|---|---|
| 1 | Single-cadence subscriptions produce identical invoices | Run all existing tests, compare line items and amounts |
| 2 | `HasMixedBillingPeriods` returns false for all existing test subscriptions | Add assertion in R.2 setup |
| 3 | `ValidateBillingPeriodAlignment` passes for all single-period lists | Tested by R.9 new helper tests |
| 4 | `applyProrationToLineItem` returns same result for non-mixed subs | R.8 cases 1-4 unchanged |
| 5 | No new fields break existing API responses | JSON serialization unchanged |
| 6 | No database schema changes | Schema file diff = empty |
| 7 | Existing `filterValidPricesForSubscription` behavior for same-period prices | Same-period prices still pass filter identically |

---

## Appendix E: Comprehensive New Multi-Cadence Test Cases

All test cases use Jan 1 2026 as the default subscription start date (anniversary billing) unless noted otherwise. "M" = Monthly, "Q" = Quarterly, "H" = Half-Yearly, "A" = Annual.

---

### E.1 Interval Alignment Validation

#### E.1.1 `IsBillingPeriodMultiple`

| # | Longer | Shorter | Expected | Reason |
|---|---|---|---|---|
| 1 | MONTHLY | MONTHLY | true | Same period |
| 2 | QUARTERLY | MONTHLY | true | 3 / 1 = 3 |
| 3 | HALF_YEARLY | MONTHLY | true | 6 / 1 = 6 |
| 4 | ANNUAL | MONTHLY | true | 12 / 1 = 12 |
| 5 | HALF_YEARLY | QUARTERLY | true | 6 / 3 = 2 |
| 6 | ANNUAL | QUARTERLY | true | 12 / 3 = 4 |
| 7 | ANNUAL | HALF_YEARLY | true | 12 / 6 = 2 |
| 8 | MONTHLY | QUARTERLY | false | 1 / 3 != integer |
| 9 | QUARTERLY | HALF_YEARLY | false | 3 / 6 != integer |
| 10 | WEEKLY | MONTHLY | false | Sub-month vs month |
| 11 | DAILY | MONTHLY | false | Sub-month vs month |
| 12 | DAILY | WEEKLY | false | Different sub-month |
| 13 | DAILY | DAILY | true | Same period |
| 14 | WEEKLY | WEEKLY | true | Same period |
| 15 | ANNUAL | ANNUAL | true | Same period |

#### E.1.2 `ValidateBillingPeriodAlignment`

| # | Periods | Expected | Reason |
|---|---|---|---|
| 1 | [M] | valid | Single period |
| 2 | [M, M] | valid | All same |
| 3 | [M, Q] | valid | Q is 3x M |
| 4 | [M, Q, H] | valid | Q=3x M, H=6x M |
| 5 | [M, Q, H, A] | valid | All multiples of M |
| 6 | [Q, A] | valid | A=4x Q |
| 7 | [Q, H] | valid | H=2x Q |
| 8 | [Q, H, A] | valid | All multiples of Q |
| 9 | [M, H, A] | valid | All multiples of M |
| 10 | [WEEKLY, M] | error | Weekly vs monthly not multiples |
| 11 | [DAILY, M] | error | Daily vs monthly not multiples |
| 12 | [DAILY, WEEKLY] | error | Not multiples |
| 13 | [] | valid | Empty list |

#### E.1.3 `MinBillingPeriod`

| # | Periods | Expected |
|---|---|---|
| 1 | [M, Q, H] | MONTHLY |
| 2 | [Q, H, A] | QUARTERLY |
| 3 | [H, A] | HALF_YEARLY |
| 4 | [M] | MONTHLY |
| 5 | [A, M, Q] | MONTHLY |
| 6 | [] | "" (empty) |

---

### E.2 Mixed Billing Period Detection

#### E.2.1 `HasMixedBillingPeriods`

| # | Line Item Billing Periods | Expected | Reason |
|---|---|---|---|
| 1 | [] | false | Empty |
| 2 | [M] | false | Single item |
| 3 | [M, M] | false | All same |
| 4 | [M, M, M] | false | All same |
| 5 | [M, Q] | true | Different |
| 6 | [M, Q, H] | true | Multiple different |
| 7 | [Q, Q, Q] | false | All same |
| 8 | [M, M, Q] | true | Two same, one different |
| 9 | [A, A] | false | All same |
| 10 | [M, A] | true | Min and max |

---

### E.3 Proration Mutual Exclusion

#### E.3.1 Subscription Creation

| # | Billing Periods | ProrationBehavior | Expected | Reason |
|---|---|---|---|---|
| 1 | [M, Q, H] | none | success | Mixed periods + none is allowed |
| 2 | [M, Q, H] | create_prorations | error | Mixed periods + proration forbidden |
| 3 | [M, M, M] | create_prorations | success | Same periods + proration allowed |
| 4 | [M, M, M] | none | success | Same periods + none allowed |
| 5 | [M, Q] | create_prorations | error | Any mix + proration forbidden |
| 6 | [Q, A] | create_prorations | error | Mixed (even without monthly) |
| 7 | [Q, A] | none | success | Mixed + none allowed |
| 8 | [M] | create_prorations | success | Single period, proration OK |

#### E.3.2 Subscription Cancellation

| # | Sub Line Items | Cancel ProrationBehavior | Expected | Reason |
|---|---|---|---|---|
| 1 | M + Q + H | none | success | Mixed + no proration |
| 2 | M + Q + H | create_prorations | error | Mixed + proration forbidden |
| 3 | M + M | create_prorations | success | Same periods, proration OK |
| 4 | M + M | none | success | Same periods, none OK |
| 5 | M + Q | create_prorations | error | Mixed, proration forbidden |

#### E.3.3 Subscription Change (Upgrade/Downgrade)

| # | Current Plan | Target Plan | ProrationBehavior | Expected | Reason |
|---|---|---|---|---|---|
| 1 | M only | M + Q | none | success | Mixed target + none |
| 2 | M only | M + Q | create_prorations | error | Mixed target + proration |
| 3 | M only | M only (higher price) | create_prorations | success | Same periods, proration OK |
| 4 | M + Q | M + Q (higher prices) | none | success | Mixed target + none |
| 5 | M + Q | M only | create_prorations | success | Single period target, proration OK |

#### E.3.4 Runtime Safety Net (applyProrationToLineItem)

| # | Sub ProrationBehavior | HasMixedPeriods | Expected | Reason |
|---|---|---|---|---|
| 1 | create_prorations | true | return original amount | Safety net skips proration |
| 2 | create_prorations | false | apply proration | Normal behavior |
| 3 | none | true | return original amount | None always skips |
| 4 | none | false | return original amount | None always skips |

---

### E.4 Invoice Generation -- FindMatchingLineItemPeriodForInvoice

All scenarios: Sub starts Jan 1, anniversary billing.

#### E.4.1 Monthly Sub + Quarterly ARREAR Line Item

| # | Invoice Period (Sub) | Quarterly Item Period | Line Item Included? | Reason |
|---|---|---|---|---|
| 1 | Jan 1 - Feb 1 | Jan 1 - Apr 1 | No | Period end Apr 1 not in (Jan 1, Feb 1] |
| 2 | Feb 1 - Mar 1 | Jan 1 - Apr 1 | No | Period end Apr 1 not in (Feb 1, Mar 1] |
| 3 | Mar 1 - Apr 1 | Jan 1 - Apr 1 | **Yes** | Period end Apr 1 in (Mar 1, Apr 1] |
| 4 | Apr 1 - May 1 | Apr 1 - Jul 1 | No | Period end Jul 1 not in (Apr 1, May 1] |
| 5 | May 1 - Jun 1 | Apr 1 - Jul 1 | No | Period end Jul 1 not in (May 1, Jun 1] |
| 6 | Jun 1 - Jul 1 | Apr 1 - Jul 1 | **Yes** | Period end Jul 1 in (Jun 1, Jul 1] |

#### E.4.2 Monthly Sub + Half-Yearly ARREAR Line Item

| # | Invoice Period (Sub) | HY Item Period | Line Item Included? | Reason |
|---|---|---|---|---|
| 1 | Jan 1 - Feb 1 | Jan 1 - Jul 1 | No | End Jul 1 not in window |
| 2 | Feb 1 - Mar 1 | Jan 1 - Jul 1 | No | End Jul 1 not in window |
| 3 | Mar 1 - Apr 1 | Jan 1 - Jul 1 | No | End Jul 1 not in window |
| 4 | Apr 1 - May 1 | Jan 1 - Jul 1 | No | End Jul 1 not in window |
| 5 | May 1 - Jun 1 | Jan 1 - Jul 1 | No | End Jul 1 not in window |
| 6 | Jun 1 - Jul 1 | Jan 1 - Jul 1 | **Yes** | End Jul 1 in (Jun 1, Jul 1] |
| 7 | Jul 1 - Aug 1 | Jul 1 - Jan 1 next | No | End Jan 1 not in window |
| 8-11 | ... | Jul 1 - Jan 1 next | No | ... |
| 12 | Dec 1 - Jan 1 | Jul 1 - Jan 1 next | **Yes** | End Jan 1 in (Dec 1, Jan 1] |

#### E.4.3 Monthly Sub + Annual ARREAR Line Item

| # | Invoice Period | Annual Item Period | Included? | Reason |
|---|---|---|---|---|
| 1-11 | Jan-Nov monthly | Jan 1 - Jan 1 next | No | End not in any monthly window |
| 12 | Dec 1 - Jan 1 | Jan 1 - Jan 1 next | **Yes** | End Jan 1 in (Dec 1, Jan 1] |

#### E.4.4 Monthly Sub + Quarterly ADVANCE Line Item

| # | Invoice Period (Sub) | Quarterly Item Period | Line Item Included? | Reason |
|---|---|---|---|---|
| 1 | Jan 1 - Feb 1 | Jan 1 - Apr 1 | **Yes** | Start Jan 1 in [Jan 1, Feb 1) |
| 2 | Feb 1 - Mar 1 | Jan 1 - Apr 1 | No | Start Jan 1 not in [Feb 1, Mar 1) |
| 3 | Mar 1 - Apr 1 | Jan 1 - Apr 1 | No | Start Jan 1 not in [Mar 1, Apr 1) |
| 4 | Apr 1 - May 1 | Apr 1 - Jul 1 | **Yes** | Start Apr 1 in [Apr 1, May 1) |
| 5 | May 1 - Jun 1 | Apr 1 - Jul 1 | No | Start Apr 1 not in [May 1, Jun 1) |
| 6 | Jun 1 - Jul 1 | Apr 1 - Jul 1 | No | Start Apr 1 not in [Jun 1, Jul 1) |
| 7 | Jul 1 - Aug 1 | Jul 1 - Oct 1 | **Yes** | Start Jul 1 in [Jul 1, Aug 1) |

#### E.4.5 Quarterly Sub + Annual ARREAR Line Item

| # | Invoice Period (Sub) | Annual Item Period | Included? | Reason |
|---|---|---|---|---|
| 1 | Jan 1 - Apr 1 | Jan 1 - Jan 1 next | No | End Jan 1 next not in (Jan 1, Apr 1] |
| 2 | Apr 1 - Jul 1 | Jan 1 - Jan 1 next | No | End not in (Apr 1, Jul 1] |
| 3 | Jul 1 - Oct 1 | Jan 1 - Jan 1 next | No | End not in (Jul 1, Oct 1] |
| 4 | Oct 1 - Jan 1 | Jan 1 - Jan 1 next | **Yes** | End Jan 1 in (Oct 1, Jan 1] |

#### E.4.6 Quarterly Sub + Annual ADVANCE Line Item

| # | Invoice Period (Sub) | Annual Item Period | Included? | Reason |
|---|---|---|---|---|
| 1 | Jan 1 - Apr 1 | Jan 1 - Jan 1 next | **Yes** | Start Jan 1 in [Jan 1, Apr 1) |
| 2 | Apr 1 - Jul 1 | Jan 1 - Jan 1 next | No | Start Jan 1 not in [Apr 1, Jul 1) |
| 3 | Jul 1 - Oct 1 | Jan 1 - Jan 1 next | No | Not in window |
| 4 | Oct 1 - Jan 1 | Jan 1 - Jan 1 next | No | Not in window |
| 5 | Jan 1 - Apr 1 (yr2) | Jan 1 next - Jan 1 yr3 | **Yes** | Start in window |

---

### E.5 ClassifyLineItems -- Multi-Cadence Combinations

Sub starts Jan 1, current period = Mar 1 - Apr 1 (monthly sub).

#### E.5.1 Three ARREAR Items (M + Q + H)

| Line Item | BillingPeriod | InvoiceCadence | Classified As | Reason |
|---|---|---|---|---|
| Monthly $10 | M | ARREAR | CurrentPeriodArrear | Equal period, arrear |
| Quarterly $100 | Q | ARREAR | CurrentPeriodArrear | Q period end (Apr 1) in (Mar 1, Apr 1] |
| Half-Yearly $200 | H | ARREAR | (not included) | H period end (Jul 1) not in (Mar 1, Apr 1] |

#### E.5.2 Three ADVANCE Items (M + Q + H)

Current period: Mar 1 - Apr 1. Next period: Apr 1 - May 1.

| Line Item | BillingPeriod | InvoiceCadence | Classified As | Reason |
|---|---|---|---|---|
| Monthly $10 | M | ADVANCE | CurrentPeriodAdvance + NextPeriodAdvance | Equal period, advance |
| Quarterly $100 | Q | ADVANCE | (not in current, check next) | Q period start (Jan 1) not in [Mar 1, Apr 1) |
| Quarterly $100 | Q | ADVANCE | NextPeriodAdvance | Q period start (Apr 1) in [Apr 1, May 1) |
| Half-Yearly $200 | H | ADVANCE | (not included) | H period start (Jan 1) not in current or next |

#### E.5.3 Mixed ARREAR and ADVANCE (M-ARREAR + Q-ADVANCE + H-ARREAR)

Current period: Mar 1 - Apr 1. Next period: Apr 1 - May 1.

| Line Item | BillingPeriod | InvoiceCadence | Classified As | Reason |
|---|---|---|---|---|
| Monthly $10 | M | ARREAR | CurrentPeriodArrear | Equal, arrear |
| Quarterly $100 | Q | ADVANCE | NextPeriodAdvance | Q start (Apr 1) in [Apr 1, May 1) for next window |
| Half-Yearly $200 | H | ARREAR | (not included) | H end (Jul 1) not in current window |

#### E.5.4 Period = Jan 1 - Feb 1 (First Month)

| Line Item | BillingPeriod | InvoiceCadence | Classified As | Reason |
|---|---|---|---|---|
| Monthly $10 | M | ARREAR | CurrentPeriodArrear | Period matches |
| Quarterly $100 | Q | ARREAR | (not included) | Q end (Apr 1) not in (Jan 1, Feb 1] |
| Half-Yearly $200 | H | ARREAR | (not included) | H end (Jul 1) not in (Jan 1, Feb 1] |

#### E.5.5 Period = Jun 1 - Jul 1 (Q2 End + H1 End)

| Line Item | BillingPeriod | InvoiceCadence | Classified As | Reason |
|---|---|---|---|---|
| Monthly $10 | M | ARREAR | CurrentPeriodArrear | Period matches |
| Quarterly $100 | Q | ARREAR | CurrentPeriodArrear | Q end (Jul 1) in (Jun 1, Jul 1] |
| Half-Yearly $200 | H | ARREAR | CurrentPeriodArrear | H end (Jul 1) in (Jun 1, Jul 1] |

---

### E.6 Invoice Generation -- Full 12-Month Schedule

#### E.6.1 Monthly + Quarterly + Half-Yearly (All ARREAR, $10 + $100 + $200)

Sub starts Jan 1. Subscription billing period = MONTHLY.

| # | Invoice Date | Period | M ($10) | Q ($100) | H ($200) | Total |
|---|---|---|---|---|---|---|
| 1 | Feb 1 | Jan-Feb | ✅ | - | - | $10 |
| 2 | Mar 1 | Feb-Mar | ✅ | - | - | $10 |
| 3 | Apr 1 | Mar-Apr | ✅ | ✅ (Q1) | - | $110 |
| 4 | May 1 | Apr-May | ✅ | - | - | $10 |
| 5 | Jun 1 | May-Jun | ✅ | - | - | $10 |
| 6 | Jul 1 | Jun-Jul | ✅ | ✅ (Q2) | ✅ (H1) | $310 |
| 7 | Aug 1 | Jul-Aug | ✅ | - | - | $10 |
| 8 | Sep 1 | Aug-Sep | ✅ | - | - | $10 |
| 9 | Oct 1 | Sep-Oct | ✅ | ✅ (Q3) | - | $110 |
| 10 | Nov 1 | Oct-Nov | ✅ | - | - | $10 |
| 11 | Dec 1 | Nov-Dec | ✅ | - | - | $10 |
| 12 | Jan 1 yr2 | Dec-Jan | ✅ | ✅ (Q4) | ✅ (H2) | $310 |

**Annual total:** $120 + $400 + $400 = **$920**

#### E.6.2 Monthly + Quarterly + Annual (All ARREAR, $50 + $300 + $1200)

Sub starts Jan 1. Subscription billing period = MONTHLY.

| # | Invoice Date | Period | M ($50) | Q ($300) | A ($1200) | Total |
|---|---|---|---|---|---|---|
| 1 | Feb 1 | Jan-Feb | ✅ | - | - | $50 |
| 2 | Mar 1 | Feb-Mar | ✅ | - | - | $50 |
| 3 | Apr 1 | Mar-Apr | ✅ | ✅ (Q1) | - | $350 |
| 4 | May 1 | Apr-May | ✅ | - | - | $50 |
| 5 | Jun 1 | May-Jun | ✅ | - | - | $50 |
| 6 | Jul 1 | Jun-Jul | ✅ | ✅ (Q2) | - | $350 |
| 7 | Aug 1 | Jul-Aug | ✅ | - | - | $50 |
| 8 | Sep 1 | Aug-Sep | ✅ | - | - | $50 |
| 9 | Oct 1 | Sep-Oct | ✅ | ✅ (Q3) | - | $350 |
| 10 | Nov 1 | Oct-Nov | ✅ | - | - | $50 |
| 11 | Dec 1 | Nov-Dec | ✅ | - | - | $50 |
| 12 | Jan 1 yr2 | Dec-Jan | ✅ | ✅ (Q4) | ✅ (A) | $1550 |

**Annual total:** $600 + $1200 + $1200 = **$3000**

#### E.6.3 Monthly ADVANCE + Quarterly ARREAR + Annual ADVANCE ($20 + $150 + $2400)

Sub starts Jan 1. Subscription billing period = MONTHLY.

| # | Invoice Date | Ref Point | M-ADV ($20) | Q-ARR ($150) | A-ADV ($2400) | Total |
|---|---|---|---|---|---|---|
| 1 | Jan 1 (creation) | period_start | ✅ (Jan) | - | ✅ (Year 1) | $2420 |
| 2 | Feb 1 | period_end | ✅ (Feb, next ADV) | - | - | $20 |
| 3 | Mar 1 | period_end | ✅ (Mar) | - | - | $20 |
| 4 | Apr 1 | period_end | ✅ (Apr) | ✅ (Q1) | - | $170 |
| 5 | May 1 | period_end | ✅ (May) | - | - | $20 |
| 6 | Jun 1 | period_end | ✅ (Jun) | - | - | $20 |
| 7 | Jul 1 | period_end | ✅ (Jul) | ✅ (Q2) | - | $170 |
| 8 | Aug 1 | period_end | ✅ (Aug) | - | - | $20 |
| 9 | Sep 1 | period_end | ✅ (Sep) | - | - | $20 |
| 10 | Oct 1 | period_end | ✅ (Oct) | ✅ (Q3) | - | $170 |
| 11 | Nov 1 | period_end | ✅ (Nov) | - | - | $20 |
| 12 | Dec 1 | period_end | ✅ (Dec) | - | - | $20 |
| 13 | Jan 1 yr2 | period_end | ✅ (Jan yr2) | ✅ (Q4) | ✅ (Year 2) | $2570 |

**Note:** period_end invoice includes current ARREAR + next ADVANCE. Creation invoice has only current ADVANCE.

#### E.6.4 Quarterly + Annual (All ARREAR, $500 + $5000)

Sub starts Jan 1. Subscription billing period = QUARTERLY.

| # | Invoice Date | Period | Q ($500) | A ($5000) | Total |
|---|---|---|---|---|---|
| 1 | Apr 1 | Jan-Apr | ✅ (Q1) | - | $500 |
| 2 | Jul 1 | Apr-Jul | ✅ (Q2) | - | $500 |
| 3 | Oct 1 | Jul-Oct | ✅ (Q3) | - | $500 |
| 4 | Jan 1 yr2 | Oct-Jan | ✅ (Q4) | ✅ (A) | $5500 |

**Annual total:** $2000 + $5000 = **$7000**

---

### E.7 Cancellation Scenarios

All scenarios: Sub starts Jan 1, anniversary billing. Line items: M $10 ARREAR + Q $100 ARREAR + H $200 ARREAR.

#### E.7.1 Immediate Cancellation

| # | Cancel Date | Monthly | Quarterly | Half-Yearly | Reasoning |
|---|---|---|---|---|---|
| 1 | Jan 15 | $10 * 14/31 = $4.52 | $100 * 14/90 = $15.56 | $200 * 14/181 = $15.47 | Prorate all from Jan 1 to Jan 15 |
| 2 | Feb 15 | $10 * 15/28 = $5.36 | $100 * 45/90 = $50.00 | $200 * 45/181 = $49.72 | M: Feb period. Q & H: Jan 1 to Feb 15 |
| 3 | Apr 1 | $10 (full March period) | $100 (full Q1 period) | $200 * 90/181 = $99.45 | M and Q complete periods. H partial |
| 4 | Jul 1 | $10 (full June period) | $100 (full Q2 period) | $200 (full H1 period) | All periods complete at Jul 1 |
| 5 | Mar 15 | $10 * 14/31 = $4.52 | $100 * 73/90 = $81.11 | $200 * 73/181 = $80.66 | M: Mar 1-15. Q & H from Jan 1 |

#### E.7.2 Cancel at min_period_end

| # | Cancel Requested | Effective Cancel Date | Monthly | Quarterly | Half-Yearly |
|---|---|---|---|---|---|
| 1 | Feb 15 | Mar 1 | $10 (full Feb) | $100 * 59/90 = $65.56 | $200 * 59/181 = $65.19 |
| 2 | Apr 15 | May 1 | $10 (full Apr) | $100 * 30/90 = $33.33 | $200 * 120/181 = $132.60 |
| 3 | Jun 15 | Jul 1 | $10 (full Jun) | $100 (full Q2) | $200 (full H1) |
| 4 | Sep 15 | Oct 1 | $10 (full Sep) | $100 (full Q3) | $200 * 92/184 = $100.00 |

**Notes:**
- Monthly always charges full (period completes at cancel date)
- Quarterly and half-yearly prorated from their period start to the effective cancel date
- When effective date coincides with a longer-cadence period end, charge full amount

#### E.7.3 Cancel at max_period_end

| # | Cancel Requested | Effective Cancel Date | Behavior |
|---|---|---|---|
| 1 | Feb 15 | Jul 1 (H1 end) | Invoices continue normally until Jul 1. All charges at full rate. |
| 2 | Apr 15 | Jul 1 (H1 end) | Invoices continue. Apr, May, Jun monthly + Q2 on Jul 1 + H1 on Jul 1. |
| 3 | Jul 15 | Jan 1 yr2 (H2 end) | Invoices continue. 6 more monthly + Q3 Oct + Q4 Jan + H2 Jan. |

**Key:** No proration. All line items run to their natural period end. Subscription stays active.

#### D.7.4 Cancellation with Mixed ADVANCE and ARREAR

Sub starts Jan 1. Line items: M $20 ADVANCE + Q $150 ARREAR + A $2400 ADVANCE.

| # | Cancel Date | Type | Monthly (ADV) | Quarterly (ARR) | Annual (ADV) |
|---|---|---|---|---|---|
| 1 | Feb 15 (immediate) | immediate | Credit unused Feb 15-Mar 1: $20 * 13/28 = $9.29 | Prorate Jan 1-Feb 15: $150 * 45/90 = $75.00 | Credit unused Feb 15 - Jan 1 next: $2400 * 320/365 = $2104.11 |
| 2 | Apr 1 (min) | min_period_end | No credit (Apr period not started) | Full Q1: $150 | Credit Apr 1 - Jan 1 next: $2400 * 275/365 = $1808.22 |
| 3 | Jan 1 yr2 (max) | max_period_end | Normal invoicing | Normal invoicing | No credit (period complete) |

**Rule:** ADVANCE items that are partially consumed generate a **credit** for the unused portion. ARREAR items generate a **charge** for the consumed portion.

---

### D.8 Preview Invoice

Sub starts Jan 1. Monthly sub billing period. All ARREAR: M $10 + Q $100 + H $200.

#### D.8.1 Orb-Style Preview (Window-Based, Implemented)

Shows only items whose period end falls in current subscription billing window.

| # | Preview Date | Current Sub Period | Line Items Shown | Reason |
|---|---|---|---|---|
| 1 | Jan 15 | Jan 1 - Feb 1 | M $10 | Only M period ends in window |
| 2 | Feb 20 | Feb 1 - Mar 1 | M $10 | Only M period ends in window |
| 3 | Mar 20 | Mar 1 - Apr 1 | M $10 + Q $100 | Q period end (Apr 1) in window |
| 4 | Jun 20 | Jun 1 - Jul 1 | M $10 + Q $100 + H $200 | All period ends in window |
| 5 | Apr 15 | Apr 1 - May 1 | M $10 | Only M; new Q starts Apr 1 (ends Jul 1) |

---

### D.9 Reference Point Combinations

Sub starts Jan 1. Monthly sub. M $10 ARREAR + Q $100 ARREAR + H $200 ADVANCE.

#### D.9.1 period_start (Subscription Creation)

| Line Item | InvoiceCadence | Included? | Amount | Reason |
|---|---|---|---|---|
| Monthly $10 | ARREAR | No | - | ARREAR not included at period_start |
| Quarterly $100 | ARREAR | No | - | ARREAR not included at period_start |
| Half-Yearly $200 | ADVANCE | Yes | $200 | ADVANCE included at creation |

**Creation invoice total:** $200

#### D.9.2 period_end (Feb 1, first renewal)

| Line Item | InvoiceCadence | Included? | Amount | Reason |
|---|---|---|---|---|
| Monthly $10 | ARREAR (current) | Yes | $10 | M period ends Feb 1 |
| Quarterly $100 | ARREAR (current) | No | - | Q period end Apr 1 not in (Jan 1, Feb 1] |
| Half-Yearly $200 | ADVANCE (next) | No | - | H next period start Jul 1 not in [Feb 1, Mar 1) |

**Feb 1 invoice total:** $10

#### D.9.3 period_end (Jul 1, Q2 + H1 boundary)

| Line Item | InvoiceCadence | Included? | Amount | Reason |
|---|---|---|---|---|
| Monthly $10 | ARREAR (current) | Yes | $10 | M period ends Jul 1 |
| Quarterly $100 | ARREAR (current) | Yes | $100 | Q period end Jul 1 in (Jun 1, Jul 1] |
| Half-Yearly $200 | ADVANCE (next) | Yes | $200 | H next period start Jul 1 in [Jul 1, Aug 1) |

**Jul 1 invoice total:** $310

#### D.9.4 cancel (Cancel Feb 15)

| Line Item | InvoiceCadence | Included? | Amount | Reason |
|---|---|---|---|---|
| Monthly $10 | ARREAR | Yes | prorated | Current M period contains cancel date |
| Quarterly $100 | ARREAR | Yes | prorated | Current Q period contains cancel date |
| Half-Yearly $200 | ADVANCE | Credit | credit for unused | H period was pre-billed, refund unused |

---

### D.10 Mid-Month Start (Anniversary Billing)

Sub starts Jan 15. Monthly sub. M $10 ARREAR + Q $100 ARREAR.

#### D.10.1 Invoice Schedule

| # | Invoice Date | Sub Period | M Included? | Q Included? | Q Period |
|---|---|---|---|---|---|
| 1 | Feb 15 | Jan 15 - Feb 15 | Yes ($10) | No | Jan 15 - Apr 15 (end not in window) |
| 2 | Mar 15 | Feb 15 - Mar 15 | Yes ($10) | No | Jan 15 - Apr 15 |
| 3 | Apr 15 | Mar 15 - Apr 15 | Yes ($10) | Yes ($100) | Jan 15 - Apr 15 (end in window) |
| 4 | May 15 | Apr 15 - May 15 | Yes ($10) | No | Apr 15 - Jul 15 |

---

### D.11 Line Item with End Date Before Subscription

Sub starts Jan 1, ends Dec 31. Monthly sub. M $10 ARREAR (ends Dec 31) + Q $100 ARREAR (ends Sep 30).

#### D.11.1 Invoice Schedule After Q Item Ends

| # | Invoice Date | M ($10) | Q ($100) | Notes |
|---|---|---|---|---|
| 9 | Oct 1 | ✅ | ✅ (Q3: Jul-Oct) | Last quarterly invoice |
| 10 | Nov 1 | ✅ | - | Q item ended Sep 30 |
| 11 | Dec 1 | ✅ | - | Q item ended |
| 12 | Jan 1 yr2 | ✅ | - | Last invoice |

---

### D.12 Addon with Different Billing Period

Sub starts Jan 1. Monthly sub, plan has M $10 ARREAR. Addon added with Q $50 ARREAR.

#### D.12.1 Invoice Schedule

| # | Invoice Date | Plan M ($10) | Addon Q ($50) | Total |
|---|---|---|---|---|
| 1 | Feb 1 | ✅ | - | $10 |
| 2 | Mar 1 | ✅ | - | $10 |
| 3 | Apr 1 | ✅ | ✅ (Q1) | $60 |
| 4 | May 1 | ✅ | - | $10 |
| 5 | Jun 1 | ✅ | - | $10 |
| 6 | Jul 1 | ✅ | ✅ (Q2) | $60 |

---

### D.13 Subscription Price Override Validation

Sub billing period = MONTHLY.

| # | Override BillingPeriod | Expected | Reason |
|---|---|---|---|
| 1 | MONTHLY | valid | Equal to sub |
| 2 | QUARTERLY | valid | Greater than sub (3x) |
| 3 | HALF_YEARLY | valid | Greater than sub (6x) |
| 4 | ANNUAL | valid | Greater than sub (12x) |
| 5 | WEEKLY | error | Less than sub |
| 6 | DAILY | error | Less than sub |

---

### D.14 Stress / Boundary Tests

#### D.14.1 Maximum Number of Different Cadences

Sub with M + Q + H + A line items (all ARREAR). Verify 12-month invoice schedule:

| Month | M | Q | H | A | Total Items |
|---|---|---|---|---|---|
| Feb | ✅ | - | - | - | 1 |
| Mar | ✅ | - | - | - | 1 |
| Apr | ✅ | ✅ | - | - | 2 |
| May | ✅ | - | - | - | 1 |
| Jun | ✅ | - | - | - | 1 |
| Jul | ✅ | ✅ | ✅ | - | 3 |
| Aug | ✅ | - | - | - | 1 |
| Sep | ✅ | - | - | - | 1 |
| Oct | ✅ | ✅ | - | - | 2 |
| Nov | ✅ | - | - | - | 1 |
| Dec | ✅ | - | - | - | 1 |
| Jan yr2 | ✅ | ✅ | ✅ | ✅ | 4 |

**Total invoices:** 12 per year, with varying line item counts.

#### D.14.2 All Same Period (Backward Compatibility)

Sub with 3x Monthly ARREAR. Should behave identically to pre-multi-cadence:

| Month | Item 1 | Item 2 | Item 3 | Total Items |
|---|---|---|---|---|
| Every month | ✅ | ✅ | ✅ | 3 |

#### D.14.3 Single Line Item (No Mix)

Sub with only Q $100 ARREAR. Sub billing period = QUARTERLY.

| Invoice | Period | Included? |
|---|---|---|
| Apr 1 | Jan-Apr | ✅ ($100) |
| Jul 1 | Apr-Jul | ✅ ($100) |
| Oct 1 | Jul-Oct | ✅ ($100) |
| Jan 1 yr2 | Oct-Jan | ✅ ($100) |

#### D.14.4 Leap Year Boundary

Sub starts Jan 29 with M + Q ARREAR. Anniversary billing.

| Invoice | M Period | Q Period | Q Included? |
|---|---|---|---|
| Feb 28 (non-leap) | Jan 29 - Feb 28 | Jan 29 - Apr 29 | No |
| Feb 29 (leap year) | Jan 29 - Feb 29 | Jan 29 - Apr 29 | No |
| Apr 29 | Mar 29 - Apr 29 | Jan 29 - Apr 29 | Yes |

#### D.14.5 Month-End Anchor (Jan 31)

Sub starts Jan 31 with M + Q ARREAR. Anniversary billing.

| Invoice | M Period | Notes |
|---|---|---|
| Feb 28 | Jan 31 - Feb 28 | Feb has no 31st; clipped to 28 |
| Mar 31 | Feb 28 - Mar 31 | Back to 31 |
| Apr 30 | Mar 31 - Apr 30 | Apr has no 31st; clipped to 30 |

Q period: Jan 31 - Apr 30 (clipped). Q included on Apr 30 invoice.

---

### D.15 FilterLineItemsToBeInvoiced (Deduplication)

Verifies that already-invoiced line items are not double-charged.

#### D.15.1 Quarterly Item Already Invoiced

| # | Scenario | Line Items | Existing Invoice | Expected |
|---|---|---|---|---|
| 1 | Q already invoiced for Jan-Apr | M + Q | Invoice with Q line (Jan 1 - Apr 1) | Only M included on Apr 1 invoice |
| 2 | Q not yet invoiced | M + Q | No matching invoice | Both M + Q included |
| 3 | M already invoiced, Q not | M + Q | Invoice with M line (Mar 1 - Apr 1) | Only Q included |
| 4 | Both already invoiced | M + Q | Invoice with both | Neither included |

---

### D.16 Commitment + Multi-Cadence Interaction

| # | Scenario | Expected | Reason |
|---|---|---|---|
| 1 | Sub-level commitment + mixed periods | Allowed | Commitment is sub-level, not line-item |
| 2 | Line-item commitment on M line + Q line (no commit) | Allowed | Commitment on individual item |
| 3 | Line-item commitment on Q line with M sub period | Allowed | Commitment tracks quarterly usage |

---

**End of PRD**