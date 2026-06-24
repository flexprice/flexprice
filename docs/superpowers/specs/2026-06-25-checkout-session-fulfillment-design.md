# Checkout Session Fulfillment — Design Spec

**Date:** 2026-06-25  
**Status:** Approved  

---

## Overview

When a checkout session is created, it should synchronously fulfill the action encoded in its configuration. For the `create_subscription` action this means: creating a draft subscription, creating a draft invoice against it, and creating an initiated payment record against that invoice — then storing all resulting IDs on the checkout session record.

No gateway calls, no webhooks, no finalization occur during this flow. Everything stays in draft/initiated state until the checkout is completed externally (e.g. Stripe redirects back).

---

## Section 1: Draft Subscription via Existing DTO Field

**No new flag is added.** `dto.CreateSubscriptionRequest` already has `SubscriptionStatus types.SubscriptionStatus`. The checkout service always passes `SubscriptionStatus: types.SubscriptionStatusDraft`.

**Current `CreateSubscription` behavior when `status = draft`:**
- Subscription is created in `draft` status
- Invoice creation is **skipped** (lines 421-422 in subscription service)
- `PaymentBehavior` and `CollectionMethod` are left as-is (plan/system defaults)

**Invoice creation (checkout service responsibility):**  
After getting the subscription back, the checkout helper calls `invoiceService.CreateSubscriptionInvoice(ctx, req, params, flow, isDraftSubscription=true)` to create a draft invoice. The `isDraftSubscription=true` flag prevents finalization, gateway calls, and webhooks — the invoice stays in `DRAFT` status with no invoice number.

**What stays unchanged:** All existing callers that create draft subscriptions without wanting an invoice are unaffected.

---

## Section 2: `CreatePaymentForCheckout` on PaymentService

**New method** added to `PaymentService` interface:

```go
// CreatePaymentForCheckout creates a minimal payment record in INITIATED status
// for a checkout session without triggering payment lifecycle processing.
// TODO: migrate to full payment lifecycle method when payment lifecycle service is released
CreatePaymentForCheckout(ctx context.Context, invoice *invoice.Invoice, gateway types.PaymentGateway) (*dto.PaymentResponse, error)
```

**Behavior:**
- Reads `Amount`, `Currency` directly off the passed `*invoice.Invoice` — no DB lookup
- Builds a `payment.Payment` domain object:
  - `DestinationType = INVOICE`, `DestinationID = invoice.ID`
  - `PaymentMethodType = PAYMENT_LINK`
  - `PaymentStatus = INITIATED`
  - `PaymentGateway = gateway`
  - `TrackAttempts = false`
- Calls `paymentRepo.Create(ctx, payment)` directly — no gateway, no processor, no webhooks
- Returns `*dto.PaymentResponse`

---

## Section 3: Checkout Service Refactor

### `Create` flow (revised)

```
Create(ctx, req)
  │
  ├─ validate request
  ├─ build CheckoutSession domain object
  ├─ repo.Create(ctx, session)            ← session persisted first
  │
  └─ fulfillCheckoutAction(ctx, session)  ← new private method
```

If `fulfillCheckoutAction` fails, the session remains in `initiated` status and the error is returned to the caller. No rollback of the session record — it serves as the durable audit trail.

### `fulfillCheckoutAction` dispatcher

```
fulfillCheckoutAction(ctx, session)
  │
  ├─ switch session.Action:
  │    case create_subscription:
  │      sub, invoice  ← createDraftSubscriptionWithInvoice(ctx, session)
  │      payment       ← createCheckoutPayment(ctx, invoice, session.PaymentProvider)
  │      session.CheckoutInvoiceID = &invoice.ID
  │      session.CheckoutPaymentID = &payment.ID
  │      session.Result = &CheckoutResult{
  │          CreateSubscriptionResult: {
  │              SubscriptionID: sub.ID,
  │              InvoiceID:      invoice.ID,
  │              PaymentID:      payment.ID,
  │          }
  │      }
  │
  └─ repo.Update(ctx, session)    ← shared step, always runs after switch
```

The `repo.Update` lives outside the switch so all future action types follow the same pattern — they only differ in how they populate the IDs and result.

### `createDraftSubscriptionWithInvoice` (private helper)

1. Build `dto.CreateSubscriptionRequest` from `session.Configuration.CreateSubscriptionParams`:
   - `CustomerID` from session
   - `PlanID`, `Currency`, `BillingPeriod`, `StartDate`, `EndDate`, `LookupKey`, `Metadata` from config
   - `SubscriptionStatus = types.SubscriptionStatusDraft`
2. Call `subscriptionService.CreateSubscription(ctx, req)` → `*dto.SubscriptionResponse`
3. Build `dto.CreateSubscriptionInvoiceRequest` from the subscription
4. Call `invoiceService.CreateSubscriptionInvoice(ctx, invoiceReq, paymentParams, InvoiceFlowSubscriptionCreation, isDraftSubscription=true)` → `(*dto.InvoiceResponse, *subscription.Subscription, error)`
5. Return `(subResp, invoiceDomain)`

### `createCheckoutPayment` (private helper)

1. Map `session.PaymentProvider` → `types.PaymentGateway`
2. Call `paymentService.CreatePaymentForCheckout(ctx, invoiceDomain, gateway)`
3. Return `*dto.PaymentResponse`

---

## Files Touched

| File | Change |
|------|--------|
| `internal/ee/service/checkout_session.go` | Add `fulfillCheckoutAction`, `createDraftSubscriptionWithInvoice`, `createCheckoutPayment`; update `Create` to call fulfillment |
| `internal/ee/service/payment.go` | Add `CreatePaymentForCheckout` with TODO comment |
| `internal/domain/payment/service.go` (interface) | Add `CreatePaymentForCheckout` to `PaymentService` interface |

No changes to subscription service, invoice service, or any DTO outside the payment service.

---

## What Is NOT in Scope

- Checkout session activation / apply step (converting draft → active after Stripe callback)
- Gateway redirect URL generation
- Webhook events on draft creation
- Subscription phase support in the checkout config (phases are rejected for draft subs)
