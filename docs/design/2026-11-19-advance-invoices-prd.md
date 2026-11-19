# Advance Invoices

- **Author:** Harshit Gupta
- **Date:** 2026-11-19
- **Status:** Draft

---

## 1. The problem

We create the renewal invoice on the renewal date. The due date and grace period count from there.

Some auto charges take days to finish. Razorpay mandates can take up to 3 days. If the charge fails, those days are already gone from the grace period. The tenant then has less time to collect the payment by hand before the subscription is cancelled.

Tenants don't want a longer grace period. They want the renewal invoice created a few days before the renewal date, so the charge starts earlier.

---

## 2. Solution

Create the renewal invoice a set number of days (lead days) before the renewal date. It is the normal subscription cycle invoice, just created earlier. No other invoice is created for that period.

### Dates

Let `X` be the renewal date and `Y` the end of the next period.

| Date | Value |
|---|---|
| Issue date | `X - lead days` |
| Service period | `[X, Y)` |
| Due date (default) | `X + payment terms` |
| Due date (from issue date) | `issue date + payment terms` |
| Cancellation | `due date + grace days` |

By default, payment terms count from the renewal date, so the customer gets the same time to pay as today, plus the lead days for the auto charge.

### Who gets it

- Active subscriptions with advance charges only.
- The subscription renews into the next period (not cancelling at period end).
- Lead days are shorter than the billing period and the current period.

### Settings

- Lead days, and whether payment terms count from the renewal date or the issue date.
- Set at tenant level, overridden per customer, then per subscription.

### Generation

- A cron checks each subscription's next renewal date. When `X - lead days` is reached, it creates the invoice.
- If that date has already passed (short period, or a new subscription inside the lead window), it creates the invoice on the next run.
- Manual trigger: creates the advance invoice now, even without lead days set. It fails if one already exists for the period.
- A preview shows the invoice before it is created.
- The issue date is always the time the invoice is created. It cannot be set to another date.

### Changes after the invoice is created

- Scheduled changes that start at `X` (plan change, add-ons, quantity, line items) are included when the invoice is created.
- Changes made after the invoice is created do not update it. Immediate changes still get the usual prorated invoice for the current period.
- To correct the invoice, the tenant voids it or issues a credit note. A void is final. No new invoice is created for that period.

### Everything else

- Proration, taxes and discounts work as on any cycle invoice.
- AR and deferred revenue are recorded on the issue date. Revenue is recognised over `[X, Y)`.
- The invoice syncs to integrations and is charged as soon as it is created, like any cycle invoice.

---

## 3. Scope

### Current

- One advance invoice for the next period of an active subscription with advance charges only.
- Created by lead days or by manual trigger.
- Out: trialing, incomplete and paused subscriptions, and subscriptions with any arrear charges.

### Future

- Advance invoices for several periods at once, as separate invoices or one combined invoice.
- Arrear fixed charges.
- A payment request for the next period without creating the invoice, with the invoice created once it is paid.
- Recreate the advance invoice automatically when the subscription changes.

---
