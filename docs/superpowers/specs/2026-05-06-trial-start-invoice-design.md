# Trial-Start Invoice Design

**Date:** 2026-05-06  
**Status:** Approved

## Background

When a subscription is created with a trial period, Flexprice currently skips invoice creation entirely for `TRIALING` subscriptions. Stripe, Paddle, and Orb all create a `$0` invoice at trial start — a transparent "here's what you'll be charged after your trial" document that auto-pays immediately and serves as the billing record for the trial period.

## Goal

Create a `$0` invoice at trial subscription start, auto-paid immediately, with full line items at `$0` — matching industry-standard behavior.

## Decisions

| Question | Decision |
|----------|----------|
| Invoice status | `paid` — auto-paid immediately since $0 (Stripe behavior) |
| Trigger | Always automatic for every trialing subscription — no flag needed |
| Line items | Full line items at $0 — shows customer what they'd pay post-trial |
| Billing reason | New `SUBSCRIPTION_TRIAL_START` — distinct from `SUBSCRIPTION_CREATE`, mirrors existing `SUBSCRIPTION_TRIAL_END` |
| Inherited subs | No invoice — only parent subscriptions get trial-start invoices |

## Approach

Dedicated `createTrialStartInvoice` function in `subscription_trial.go`, mirroring the existing `processSubscriptionTrialEnd` pattern. The billing reason stored on the invoice itself (`SUBSCRIPTION_TRIAL_START`) acts as the discriminator in `ComputeInvoice` to auto-pay instead of skip.

## Changes

### 1. `internal/types/invoice.go`

Add new billing reason constant:

```go
// InvoiceBillingReasonSubscriptionTrialStart indicates the $0 invoice created when a trialing subscription starts
InvoiceBillingReasonSubscriptionTrialStart InvoiceBillingReason = "SUBSCRIPTION_TRIAL_START"
```

- Add to `Validate()` allowed list
- Do NOT add to `TriggersSubscriptionActivationOnFullPayment()` — subscription stays `TRIALING` after this invoice; only activates at trial end

### 2. `internal/service/invoice.go` — `ComputeInvoice` (~line 473)

Surgical change to the zero-amount guard: when billing reason is `SUBSCRIPTION_TRIAL_START` and subtotal is zero, mark as `paid` instead of `skipped`.

```go
if inv.InvoiceType == types.InvoiceTypeSubscription && inv.Subtotal.IsZero() {
    now := time.Now().UTC()
    inv.LastComputedAt = &now

    if inv.BillingReason == types.InvoiceBillingReasonSubscriptionTrialStart {
        inv.InvoiceStatus = types.InvoiceStatusPaid
        inv.PaymentStatus = types.PaymentStatusSucceeded
        inv.AmountPaid = inv.Subtotal   // $0
    } else {
        inv.InvoiceStatus = types.InvoiceStatusSkipped
    }

    if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
        return err
    }
    skipped = true
    return nil
}
```

`skipped = true` is still returned so the caller does not attempt payment processing.

### 3. `internal/service/subscription_trial.go` — new function

```go
func (s *subscriptionService) createTrialStartInvoice(
    ctx context.Context,
    sub *subscription.Subscription,
    invoiceService InvoiceService,
) error {
    if sub.SubscriptionType == types.SubscriptionTypeInherited {
        return nil
    }
    if sub.TrialStart == nil || sub.TrialEnd == nil {
        return nil
    }

    paymentParams := dto.NewPaymentParametersFromSubscription(
        sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID,
    ).NormalizePaymentParameters()

    _, _, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
        SubscriptionID: sub.ID,
        PeriodStart:    lo.FromPtr(sub.TrialStart),
        PeriodEnd:      lo.FromPtr(sub.TrialEnd),
        ReferencePoint: types.ReferencePointPeriodStart,
        BillingReason:  types.InvoiceBillingReasonSubscriptionTrialStart,
    }, paymentParams, types.InvoiceFlowSubscriptionCreation, false)

    return err
}
```

### 4. `internal/service/subscription.go` — call site (~line 440)

Add a new independent `if` block immediately after the existing non-trial invoice block:

```go
// Existing — unchanged
if sub.SubscriptionStatus != types.SubscriptionStatusDraft && sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
    // ... existing invoice creation + activation logic ...
}

// New — $0 trial-start invoice for trialing subscriptions
if sub.SubscriptionStatus == types.SubscriptionStatusTrialing {
    if err := s.createTrialStartInvoice(ctx, sub, invoiceService); err != nil {
        return err
    }
}
```

### 5. Testing

File: `internal/service/subscription_trial_test.go`

| Test | Assertion |
|------|-----------|
| Happy path: trialing subscription created | `SUBSCRIPTION_TRIAL_START` invoice exists, status=`paid`, amount=`$0`, full line items present |
| Inherited trialing subscription | No trial-start invoice created |
| Non-trialing subscription | No `SUBSCRIPTION_TRIAL_START` invoice created — existing behavior unchanged |

Uses existing `testutil.SetupTestDB()` pattern, table-driven format.

## What Does Not Change

- The existing `if status != DRAFT && status != TRIALING` guard in `subscription.go` — untouched
- `processSubscriptionTrialEnd` and trial-end invoice logic — untouched
- Zero-amount skip behavior for all other billing reasons — untouched
- Inherited subscription billing — no invoice, consistent with rest of trial lifecycle
