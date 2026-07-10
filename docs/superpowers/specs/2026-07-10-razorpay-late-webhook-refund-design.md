# Razorpay: refund late webhook capture on expired checkout

## Problem

`internal/integration/razorpay/webhook/handler.go`'s `completeCheckoutSessionIfPending`
looks up the checkout session tied to a FlexPrice payment ID with a filter of
`CheckoutStatuses=[Pending]`. If the checkout-session expiry cron
(`CleanupAllExpiredSessions` → `cleanupCheckoutSession`,
`internal/ee/service/checkout_session.go`) transitions the session to `Expired` (and
archives the draft `Payment`/`Invoice`/`Subscription`) before Razorpay's
`payment_link.paid` or `payment.captured` webhook arrives, this lookup returns nothing:

- `handlePaymentLinkPaid` silently returns — Razorpay captured real money, FlexPrice
  records nothing and never refunds it.
- `handlePaymentCaptured` (UPI Autopay mandate flow) falls through to "standalone
  payment" handling and incorrectly marks the already-archived payment `Succeeded`.

Since the checkout expired, the customer never received the product (subscription
was archived, not activated). The captured amount must be refunded.

## Scope

- Applies to both `handlePaymentLinkPaid` (standard payment-link checkouts) and
  `handlePaymentCaptured` (mandate/authorization-link checkouts) — both currently
  route through the same broken pending-only lookup.
- Triggers only on `CheckoutStatusExpired`. `CheckoutStatusFailed` sessions are left
  as-is (separate, rarer case; out of scope for this change).
- Refund is always for the full captured amount (no partial-refund scenario here —
  nothing was delivered).
- If the Razorpay refund API call fails, log at Error level and stop; no automatic
  retry (webhook handlers always return 200 OK to Razorpay, so Razorpay will not
  retry the webhook for us; a future cron reconciliation job is out of scope).

## Design

### 1. Status-aware checkout session lookup

Replace the `CheckoutStatuses=[Pending]`-filtered lookup with a status-agnostic one,
then branch on the result:

```go
session := h.findCheckoutSessionForPayment(ctx, flexpricePaymentID, services)
if session == nil {
    return false // no checkout session exists — unchanged fallback behavior
}
switch session.CheckoutStatus {
case types.CheckoutStatusPending:
    h.completeCheckoutSession(ctx, session.ID, flexpricePaymentID, razorpayPaymentID, services)
case types.CheckoutStatusExpired:
    if err := h.paymentSvc.RefundLateCapturedPayment(ctx, flexpricePaymentID, razorpayPaymentID, services.PaymentService); err != nil {
        h.logger.Error(ctx, "failed to refund late-captured payment on expired checkout", "error", err, ...)
    }
default: // Completed, Failed, Initiated
    h.logger.Info(ctx, "checkout session in non-actionable status, ignoring webhook", "status", session.CheckoutStatus)
}
return true
```

`findCheckoutSessionForPayment` and `completeCheckoutSession` are both used by
`handlePaymentLinkPaid` and `handlePaymentCaptured`, replacing the current
`completeCheckoutSessionIfPending`. Behavior for "no session found at all" is
unchanged at each call site (standalone reconciliation for `payment.captured`, no-op
for `payment_link.paid`).

### 2. New Razorpay gateway call

`internal/integration/razorpay/client.go` — add to `RazorpayClient` interface and
`Client`:

```go
RefundPayment(ctx context.Context, paymentID string, amountPaise int64) (map[string]interface{}, error)
```

Implemented via the SDK's `razorpayClient.Payment.Refund(paymentID, int(amountPaise), nil, nil)`
(POST `/v1/payments/{id}/refund`), same error-wrapping pattern as `CreateOrder` /
`CreateRecurringPayment`. `amountPaise` is always passed explicitly (derived from the
FlexPrice `Payment.Amount`) rather than relying on Razorpay's omit-amount-for-full-refund
behavior.

### 3. Refund orchestration — `PaymentService.RefundLateCapturedPayment`

New method on `internal/integration/razorpay/payment.go`'s `PaymentService`, mirroring
the lock → re-check-status → act → persist-mapping shape of `InvoiceSyncService.executeAutoCharge`:

```go
func (s *PaymentService) RefundLateCapturedPayment(
    ctx context.Context,
    flexpricePaymentID string,
    razorpayPaymentID string,
    paymentService interfaces.PaymentService,
) error
```

Steps:

1. If `s.locker == nil`, log Error and return (no refund attempted without a working
   distributed lock — avoids double-refund risk under concurrent webhook delivery).
2. Acquire lock `razorpay:webhook-refund:<tenant>:<env>:<flexpricePaymentID>` (short
   TTL, ~1 minute — single API call, not a multi-step submission). If not acquired,
   log Info and skip (another webhook delivery is already handling it).
3. `defer lock.Release(ctx)`.
4. `paymentService.GetPayment(ctx, flexpricePaymentID)` — works even though the
   payment was archived by the expiry cron (`PaymentRepo.Get` has no status filter;
   archiving only overwrites the `payment_status` column, doesn't touch the generic
   soft-delete `status` field).
5. If `payment.PaymentStatus` is already `Refunded` or `PartiallyRefunded`, log Info
   and skip (idempotent against duplicate/retried webhook delivery).
6. Call `client.RefundPayment(ctx, razorpayPaymentID, toPaise(payment.Amount))`.
7. On gateway error: log Error, return nil. No retry (see Scope).
8. On success: `paymentService.UpdatePayment` with `PaymentStatus=Refunded`,
   `RefundedAt=now`, `GatewayPaymentID=razorpayPaymentID`.
9. Best-effort audit mapping via `entityIntegrationMappingRepo.Create`:
   `EntityType=Payment, EntityID=flexpricePaymentID, ProviderEntityID=<razorpay refund
   id>, metadata.provider_entity_type="refund"` — mirrors
   `InvoiceSyncService.createAutoChargeMappings`. Failure here is logged but non-fatal
   (the refund itself already succeeded).

**Wiring**: `PaymentService` does not currently hold a `cache.Locker` or
`entityintegrationmapping.Repository` (only `InvoiceSyncService` does). Add both as
constructor params to `NewPaymentService(...)` and thread `f.locker` /
`f.entityIntegrationMappingRepo` through in `internal/integration/factory.go`'s
`GetRazorpayIntegration` — both already exist on the factory.

## Testing

Unit tests alongside the webhook handler (mirroring the `TestExistingRefunded_Skip` /
`TestExistingPartiallyRefunded_Skip` naming convention from
`internal/integration/razorpay/invoice_autocharge_test.go`):

- Pending session → completes as before (regression coverage for the refactor).
- Expired session → refund submitted exactly once, payment marked `Refunded`.
- Duplicate webhook delivery on an already-refunded payment → skipped, no second
  gateway call.
- Lock contention (concurrent webhook delivery) → second caller skips.
- Gateway refund API error → logged, no panic, payment status unchanged.
- No checkout session found → existing fallback behavior preserved unchanged.
- Completed / Failed / Initiated session status → no-op, logged.
