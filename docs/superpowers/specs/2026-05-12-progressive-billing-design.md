# Progressive Billing (Auto-Invoice Threshold) — Design Spec

**Date:** 2026-05-12  
**Status:** Approved

## Overview

Progressive billing automatically triggers an invoice mid-period when a subscription's current-period usage amount crosses a configured threshold. Unlike Lago (which uses lifetime usage), Flexprice tracks usage for the **current billing period only**. When the threshold is crossed, an invoice is generated for `current_period_start → now`, and `current_period_start` is advanced to `now` so the next check starts fresh.

Checking is done by a dedicated Temporal cron workflow that runs every 5 minutes.

---

## Schema Changes

### `plan` table
Add one nullable field:
```
auto_invoice_threshold  decimal  nullable
```

### `subscription` table
Add one nullable field:
```
auto_invoice_threshold  decimal  nullable
```

### Effective threshold resolution (service layer)
```
effectiveThreshold = subscription.auto_invoice_threshold
                  ?? plan.auto_invoice_threshold
                  ?? nil  // progressive billing disabled
```

- `NULL` on subscription → inherit from plan
- `NULL` on both → progressive billing disabled for this subscription
- No separate table, no opt-out flag — `NULL` is the disable mechanism

---

## Scheduled Job

**New Temporal cron workflow:** `threshold_billing_workflow.go`  
**Location:** `internal/temporal/workflows/cron/`  
**Schedule:** Every 5 minutes (separate from the existing `subscription_billing_periods_workflow`)

### Flow

1. Fetch active subscriptions with a non-null effective threshold in batches (batch size: 100) via new repo method `GetSubscriptionsWithThreshold(ctx, limit, offset)`.
2. For each subscription:
   a. Skip if status is `paused`, `cancelled`, `draft`, or type is `inherited`.
   b. Resolve `effectiveThreshold` from subscription → plan fallback.
   c. Calculate current period usage: `CalculateUsageCharges(ctx, periodStart=current_period_start, periodEnd=now)` → sum total dollar amount.
   d. If `totalUsage >= effectiveThreshold`: trigger invoice creation (see below).
   e. Otherwise: no-op, continue to next subscription.
3. Continue fetching next batch until all subscriptions are processed.
4. Failures for individual subscriptions are logged and skipped — one bad subscription does not block the batch.

### New Repository Method

```go
// GetSubscriptionsWithThreshold returns active subscriptions that have
// an auto_invoice_threshold set directly or via their plan.
GetSubscriptionsWithThreshold(ctx context.Context, limit, offset int) ([]*Subscription, error)
// WHERE subscription.status = 'active'
// AND (
//   subscription.auto_invoice_threshold IS NOT NULL
//   OR plan.auto_invoice_threshold IS NOT NULL
// )
```

---

## Invoice Creation on Threshold Cross

When `totalUsage >= effectiveThreshold`:

1. **Create draft invoice** via `CreateDraftInvoiceForSubscription(ctx, subscriptionID, current_period_start, now, referencePoint)`.
   - `InvoiceType = subscription`
   - `BillingReason = "threshold_billing"` (new constant)
2. **Compute invoice** via existing `ComputeInvoice` — calculates line items, applies discounts/credits, assigns invoice number if non-zero.
3. **Advance period start:** Update `subscription.current_period_start = now` — only after successful invoice creation. A failed invoice does not advance the period.
4. **`current_period_end` is unchanged** — the regular end-of-period billing workflow continues unaffected. The threshold invoice is an intermediate invoice within the period.

On the next 5-min job run, usage is calculated from the new `current_period_start`, so already-invoiced usage is excluded naturally. At `current_period_end`, the normal billing workflow invoices the remaining usage since the last threshold invoice.

---

## Edge Cases

| Case | Behavior |
|------|----------|
| Usage < threshold | No-op, skip subscription |
| Subscription paused / cancelled / draft / inherited | Skip |
| Invoice creation fails | Log error, skip subscription, do not advance `current_period_start` |
| Computed invoice = $0 (all credits) | Existing logic marks it `SKIPPED`; `current_period_start` still advances to avoid re-checking same window |
| Job runs twice in quick succession | Idempotency key = `subscription_id + current_period_start.Unix()` — since `current_period_start` only advances after a successful invoice, the same key cannot be generated twice for the same window |
| Plan threshold changed mid-period | Next job run uses the new effective threshold; no retroactive effect |
| Subscription overrides plan threshold | Subscription value always wins; `NULL` on subscription means fall back to plan |

---

## What Is Not in Scope

- Multiple step thresholds per plan/subscription (single threshold only for now)
- Recurring threshold (bill every N dollars after last threshold — not needed yet)
- Lifetime usage across periods (current period only)
- Webhook notification on threshold cross (can be added later as a `system_event`)
- UI changes (API-first)
