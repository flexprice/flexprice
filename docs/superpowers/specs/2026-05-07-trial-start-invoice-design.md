# Trial Start Invoice — Design Spec

**Date:** 2026-05-07  
**Status:** Approved  

---

## Problem

When a subscription is created with a trial period, FlexPrice currently creates **no invoice at all**. The first invoice only appears at trial end (`SUBSCRIPTION_TRIAL_END`).

This diverges from industry standard (Stripe, Paddle, Orb) and blocks a critical use case: **card capture during trial**. Paddle has no mechanism to save a payment method other than through a $0 checkout transaction. Without a $0 invoice at trial start, Paddle-integrated tenants cannot collect card details before the trial ends.

---

## Goal

Create a **$0 preview invoice** at the moment a trialing subscription is activated. The invoice:

- Shows the same advance line items (fixed/recurring charges) that will appear on the first real invoice, all with $0 amounts
- Is finalized (not skipped) so it appears in invoice listings and syncs downstream
- Has payment status driven by `collection_method`, not by any actual charge

---

## Industry Research

| Platform | $0 trial invoice? | Period | Payment status |
|---|---|---|---|
| **Stripe** | Yes | `trial_start → trial_end` | Paid immediately (both collection methods) |
| **Paddle** | Yes ($0 transaction) | Trial window | Requires checkout flow for card capture — only mechanism available |
| **Chargebee** | No (invoice at trial end) | — | — |
| **Lago** | Configurable | — | — |

**Key Paddle nuance:** Paddle's only card-save mechanism is their checkout flow, which requires a transaction. A $0 transaction (trial start invoice in PENDING state) must be synced to Paddle to trigger their card-capture UI. This is the primary driver for the `send_invoice → PENDING` decision.

**Stripe nuance:** Stripe marks $0 invoices as paid regardless of collection method. Card saving in Stripe is done via SetupIntent, not the invoice. For FlexPrice, the differentiated behavior (send_invoice → PENDING) is intentional for Paddle compatibility, not a Stripe replica.

---

## Decisions

### 1. New billing reason: `SUBSCRIPTION_TRIAL_START`

Billing reason is the discriminator for all diverging behavior in the compute → finalize → payment pipeline. It is the right lever (vs. a feature flag or a generic `ZeroAmounts` parameter) because it explicitly names the intent.

### 2. Invoice period: `TrialStart → TrialEnd`

Not `CurrentPeriodStart → CurrentPeriodEnd`. For trialing subscriptions, `CurrentPeriodEnd` is the first billing cycle end (e.g., Feb 1 for a monthly plan starting Jan 1), not the trial end (Jan 8). The trial start invoice must explicitly use `sub.TrialStart` and `sub.TrialEnd` as period bounds. This matches Stripe/Paddle behavior.

### 3. Line items: advance charges only, zeroed

`ReferencePointPeriodStart` already restricts computation to advance recurring charges (flat fees, per-seat). Usage/metered items are excluded by the existing classify logic — no special handling needed. Amounts are computed normally to get the correct line item structure (descriptions, quantities, pricing metadata), then all `Amount` fields are forced to `decimal.Zero` via `ZeroOutAmounts()` before the invoice is written.

`Quantity` is deliberately NOT zeroed — it preserves the pricing skeleton (e.g., "1 seat × $99/mo") so the customer sees what they'll be charged.

### 4. Payment status by collection method

| `collection_method` | Payment status | Rationale |
|---|---|---|
| `charge_automatically` | `SUCCEEDED` immediately | Payment method is on file; nothing to charge on a $0 invoice |
| `send_invoice` | `PENDING` | Stays PENDING so the finalized invoice syncs to Stripe/Paddle; Paddle drives card capture via their $0 checkout flow |

The interception happens at the top of `attemptPaymentForSubscriptionInvoice`, **before** the Stripe connection check, so it applies regardless of whether a Stripe integration is configured.

### 5. Subscription stays `TRIALING`

The trial start invoice does not gate or advance subscription lifecycle. `SUBSCRIPTION_TRIAL_START` is explicitly excluded from `IsFirstSubscriptionOpenInvoiceReason()`, so paying (or marking SUCCEEDED) this invoice never calls `HandleIncompleteSubscriptionPayment` or changes subscription status. Subscription activation still happens exclusively at `processSubscriptionTrialEnd`.

### 6. Not SKIPPED

The existing zero-dollar SKIPPED logic is bypassed for `SUBSCRIPTION_TRIAL_START`. The invoice is intentionally $0 and must be finalized and visible in listings.

---

## Billing Reason Taxonomy (updated)

| Reason | Flow | Compute | Zero-dollar | Notes |
|---|---|---|---|---|
| `SUBSCRIPTION_CREATE` | `CreateSubscription` (non-trial) | Advance charges, `ReferencePointPeriodStart` | SKIPPED | Activates subscription when paid |
| `SUBSCRIPTION_CYCLE` | Renewal workflow | Arrear + next-period advance, `ReferencePointPeriodEnd` | SKIPPED | — |
| `SUBSCRIPTION_UPDATE` | Plan change / adjustment | Proration or advance, `ReferencePointPeriodStart` | SKIPPED | Activates INCOMPLETE subscription when paid |
| `SUBSCRIPTION_TRIAL_END` | `processSubscriptionTrialEnd` | Advance charges from `trial_end`, `ReferencePointPeriodStart` | SKIPPED → activates | Billing anchor reset to `trial_end` |
| `SUBSCRIPTION_TRIAL_START` | `CreateSubscription` (trialing) | Advance charges for `trial_start → trial_end`, zeroed | **NOT SKIPPED** | $0 preview; PENDING or SUCCEEDED by collection method; subscription stays TRIALING |
| `PRORATION` | `ChangeSubscription` | `ReferencePointCancel` | SKIPPED | — |
| `MANUAL` | Manual invoice API | Caller-provided | SKIPPED | — |

---

## Files Changed

| File | Change |
|---|---|
| `internal/types/invoice.go` | Add `InvoiceBillingReasonSubscriptionTrialStart` constant; replace all billing reason comments with full taxonomy; add to `Validate()` allowed list |
| `internal/api/dto/invoice.go` | Add `ZeroOutAmounts()` method on `CreateInvoiceRequest` |
| `internal/service/invoice.go` | `ComputeInvoice`: add `SUBSCRIPTION_TRIAL_START` to `ReferencePointPeriodStart` switch; call `ZeroOutAmounts()` after `PrepareSubscriptionInvoiceRequest`; exempt from SKIPPED check |
| `internal/service/subscription.go` | Add trialing branch in `CreateSubscription` to create trial start invoice |
| `internal/service/subscription_payment_processor.go` | Add early-return block in `attemptPaymentForSubscriptionInvoice` for `SUBSCRIPTION_TRIAL_START` |

---

## Detailed Changes

### `internal/types/invoice.go`

```go
const (
    InvoiceBillingReasonSubscriptionCreate     InvoiceBillingReason = "SUBSCRIPTION_CREATE"
    InvoiceBillingReasonSubscriptionCycle      InvoiceBillingReason = "SUBSCRIPTION_CYCLE"
    InvoiceBillingReasonSubscriptionUpdate     InvoiceBillingReason = "SUBSCRIPTION_UPDATE"
    InvoiceBillingReasonSubscriptionTrialEnd   InvoiceBillingReason = "SUBSCRIPTION_TRIAL_END"
    InvoiceBillingReasonSubscriptionTrialStart InvoiceBillingReason = "SUBSCRIPTION_TRIAL_START"
    InvoiceBillingReasonProration              InvoiceBillingReason = "PRORATION"
    InvoiceBillingReasonManual                 InvoiceBillingReason = "MANUAL"
)
```

Add `InvoiceBillingReasonSubscriptionTrialStart` to `Validate()` allowed slice.

`IsFirstSubscriptionOpenInvoiceReason()` — no change. `SUBSCRIPTION_TRIAL_START` must not be included.

### `internal/api/dto/invoice.go`

```go
// ZeroOutAmounts forces all monetary amounts on this invoice request to zero while
// preserving line item structure (descriptions, quantities, price metadata).
//
// Used for trial start invoices: the customer sees exactly which charges will apply
// when the trial ends, but the amount is always $0 during the trial period.
// Quantity and metadata are kept so the pricing skeleton is visible.
func (r *CreateInvoiceRequest) ZeroOutAmounts() {
    r.Subtotal  = decimal.Zero
    r.Total     = decimal.Zero
    r.AmountDue = decimal.Zero
    for i := range r.LineItems {
        r.LineItems[i].Amount = decimal.Zero
    }
}
```

### `internal/service/invoice.go` — `ComputeInvoice`

**Change 1** — refPoint switch:
```go
case types.InvoiceBillingReasonSubscriptionCreate,
    types.InvoiceBillingReasonSubscriptionTrialEnd,
    types.InvoiceBillingReasonSubscriptionTrialStart, // new
    types.InvoiceBillingReasonSubscriptionUpdate:
    refPoint = types.ReferencePointPeriodStart
```

**Change 2** — zero amounts after prepare:
```go
subInvReq, err := billingService.PrepareSubscriptionInvoiceRequest(ctx, params)
if err != nil {
    return false, err
}
// Trial start invoices preview the first period's charges at $0 — amounts are
// computed normally so the line item structure is accurate, then forced to zero.
if types.InvoiceBillingReason(inv.BillingReason) == types.InvoiceBillingReasonSubscriptionTrialStart {
    subInvReq.ZeroOutAmounts()
}
computeReq := subInvReq.ToComputeRequest()
```

**Change 3** — exempt from SKIPPED:
```go
isTrialStart := types.InvoiceBillingReason(inv.BillingReason) == types.InvoiceBillingReasonSubscriptionTrialStart
if inv.InvoiceType == types.InvoiceTypeSubscription && inv.Subtotal.IsZero() && !isTrialStart {
    // existing SKIPPED logic unchanged
}
```

### `internal/service/subscription.go` — `CreateSubscription`

```go
// existing non-trial path
if sub.SubscriptionStatus != types.SubscriptionStatusDraft &&
    sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
    // ... existing invoice creation ...
} else if sub.SubscriptionStatus == types.SubscriptionStatusTrialing &&
    sub.TrialStart != nil && sub.TrialEnd != nil {

    paymentParams := dto.NewPaymentParametersFromSubscription(
        sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID,
    ).NormalizePaymentParameters()

    // trialInvoice is informational only; its value is not used to gate activation.
    _, _, err = invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
        SubscriptionID: sub.ID,
        PeriodStart:    lo.FromPtr(sub.TrialStart),
        PeriodEnd:      lo.FromPtr(sub.TrialEnd),
        ReferencePoint: types.ReferencePointPeriodStart,
        BillingReason:  types.InvoiceBillingReasonSubscriptionTrialStart,
    }, paymentParams, types.InvoiceFlowSubscriptionCreation, false)
    if err != nil {
        return err
    }
    // Subscription stays TRIALING — trial start invoice does not gate activation.
}
```

### `internal/service/subscription_payment_processor.go` — `attemptPaymentForSubscriptionInvoice`

```go
// Trial start invoices are always $0. Skip the payment pipeline entirely:
// charge_automatically → mark succeeded immediately (nothing to charge).
// send_invoice         → leave PENDING so downstream sync (Stripe, Paddle)
//                        drives their card-capture checkout flow.
// Subscription stays TRIALING in both cases.
if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialStart) {
    if types.CollectionMethod(sub.CollectionMethod) == types.CollectionMethodChargeAutomatically {
        zero := decimal.Zero
        return s.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, &zero)
    }
    return nil // send_invoice: stays PENDING
}
```

---

## Out of Scope

- **Cancellation during trial**: if the subscription is cancelled while the trial start invoice is `PENDING`, the invoice stays as-is. It is $0 so there is no financial exposure. Voiding it on cancellation is a separate concern.
- **Trial extension**: extending `TrialEnd` after the trial start invoice is created does not update the invoice period. The invoice is a point-in-time preview.
- **Swagger / SDK regeneration**: billing reason enum needs to be updated in generated docs; this is a follow-on task after implementation.
