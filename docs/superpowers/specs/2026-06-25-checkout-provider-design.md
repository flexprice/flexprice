# Checkout Payment Provider Interface — Design Spec

**Date:** 2026-06-25
**Branch:** feat/checkout
**Status:** Approved for implementation

---

## 1. Problem

`executeCheckoutAction` creates a FlexPrice subscription, invoice, and payment record but never contacts a payment gateway. The checkout session currently stays in `pending` with no URL for the customer to pay at. There is also no mechanism to route incoming payment webhooks back to `CompleteCheckoutSession`.

---

## 2. Goals

1. Define a unified `CheckoutProvider` interface so `checkoutSessionService` is gateway-agnostic.
2. Wire all 4 supported gateways (Stripe, Razorpay, Nomod, Moyasar) into that interface via thin adapters without modifying their existing `PaymentService.CreatePaymentLink()` methods.
3. On link creation, record an `EntityIntegrationMapping` (`provider_session_id → FlexPrice payment_id`) so incoming webhooks can route back to `CompleteCheckoutSession`.
4. Unify the `CheckoutProviderResult` type: flat structure with `NextAction`, surfaced directly in `CheckoutSessionResponse.PaymentAction`.
5. Tighten `CheckoutSession.ExpiresAt` if the provider URL expires sooner than our calculated expiry.
6. Make `CompleteCheckoutSession` fully idempotent via atomic conditional DB update.
7. Introduce `checkout.session.expired` webhook and Temporal-based expiry worker.
8. Paddle and Whop return a clear "not supported for checkout" error.

---

## 3. Out of Scope

- New checkout actions beyond `create_subscription` (future: `pay_invoice`, `save_card`)
- QR-code or SDK-based payment flows (`PaymentActionType` discriminator is forward-compatible)
- Refund/void flows via checkout
- Abandoned checkout email recovery
- Coupon/discount application during the hosted checkout flow

---

## 4. Type Changes

### 4.1 Move `PaymentAction` to `types`

`PaymentAction` is currently defined in `internal/api/dto/checkout_session.go`. Move it to `internal/types/checkout.go` alongside `PaymentActionType`.

```go
// internal/types/checkout.go
type PaymentAction struct {
    Type PaymentActionType `json:"type"`
    URL  string            `json:"url"`
    // Future: ClientSecret, QRData, SDKPayload — extend here without breaking the interface
}
```

Remove the struct from `internal/api/dto/checkout_session.go` and update all references to use `types.PaymentAction`.

### 4.2 Flatten `CheckoutProviderResult`

Replace the action-scoped nested struct with a flat, action-agnostic struct. `NextAction` has the same shape regardless of which checkout action triggered the payment. Action-scoped nesting forces `ToCheckoutSessionResponse` to switch on action type just to read the URL. (`CheckoutResult` stays action-scoped because FlexPrice entity IDs differ per action.)

**Before:**
```go
type CheckoutProviderResult struct {
    CreateSubscriptionResult *ProviderSubscriptionResult `json:"create_subscription_result,omitempty"`
}
type ProviderSubscriptionResult struct {
    SessionID       string `json:"session_id"`
    SessionURL      string `json:"session_url"`
    PaymentIntentID string `json:"payment_intent_id"`
}
```

**After — `internal/types/checkout_configuration.go`:**
```go
type CheckoutProviderResult struct {
    // NextAction is what the customer must do to complete payment.
    // Surfaced directly as CheckoutSessionResponse.PaymentAction via PaymentActionForUser().
    NextAction *PaymentAction `json:"next_action,omitempty"`

    // ProviderSessionID is stored in EntityIntegrationMapping at link creation.
    // Looked up when the payment webhook arrives.
    //   Stripe:   Checkout Session ID  (cs_xxx)
    //   Razorpay: Payment Link ID      (plink_xxx)
    //   Nomod:    Payment Link ID      (NOTE: webhook uses Charge ID; look up by PaymentLinkID field)
    //   Moyasar:  Payment ID
    ProviderSessionID string `json:"provider_session_id,omitempty"`

    // ProviderPaymentIntentID is the provider-side charge/intent ID.
    // Stripe returns this at link creation (pi_xxx); others populate it from the webhook payload.
    // Stored as gateway_payment_id on the Payment record at CompleteCheckoutSession time.
    ProviderPaymentIntentID string `json:"provider_payment_intent_id,omitempty"`

    // ExpiresAt is the provider URL expiry. May be tighter than CheckoutSession.ExpiresAt.
    // When set and earlier, executeCheckoutAction tightens the session expiry to match.
    ExpiresAt *time.Time `json:"expires_at,omitempty"`

    // ProviderMetadata holds provider-specific data that doesn't fit the unified fields.
    // Nomod: reference_id. Razorpay: is_invoice_link. Stripe: livemode.
    // For debugging only — never drive business logic from these.
    ProviderMetadata map[string]string `json:"provider_metadata,omitempty"`
}
```

### 4.3 `PaymentActionForUser()` method

The full `CheckoutProviderResult` is **never serialized to the user** — it contains sensitive gateway data (session tokens, internal IDs, livemode flags). Only the derived `PaymentAction` is surfaced.

```go
// internal/types/checkout_configuration.go
func (r *CheckoutProviderResult) PaymentActionForUser() *PaymentAction {
    if r == nil {
        return nil
    }
    return r.NextAction
}
```

### 4.4 Simplify `ToCheckoutSessionResponse`

```go
// internal/api/dto/checkout_session.go
func ToCheckoutSessionResponse(s *domainCheckout.CheckoutSession) *CheckoutSessionResponse {
    resp := &CheckoutSessionResponse{CheckoutSession: s}
    if s.ProviderResult != nil {
        resp.PaymentAction = s.ProviderResult.PaymentActionForUser()
        // Zero out provider_result — contains sensitive gateway data not safe to expose.
        resp.CheckoutSession.ProviderResult = nil
    }
    return resp
}
```

### 4.5 New webhook event

```go
// internal/types/webhook.go
WebhookEventCheckoutSessionExpired WebhookEventName = "checkout.session.expired"
```

(`CheckoutStatusExpired = "expired"` already exists in `internal/types/checkout.go`.)

---

## 5. Session Creation — DB-First Pattern

### 5.1 CheckoutSession written to DB before any action or provider logic

`Create()` writes the `CheckoutSession` record to the DB with `checkout_status = initiated` as the **very first operation**, before branching on `action`, before calling any provider, before creating any subscription/invoice/payment. This is unconditional.

```
POST /checkout/sessions
  → Validate request
  → CheckoutSessionRepo.Create(ctx, session)   ← DB write, status = initiated
  → executeCheckoutAction(ctx, session)
       → create draft sub/invoice/payment
       → call provider → store EntityIntegrationMapping
       → Update session (status = pending, ProviderResult set)
  → Publish checkout.session.initiated
  → Return session to caller
```

If `executeCheckoutAction` fails at any point, `CleanupCheckoutSession` is called to delete the created entities and set `checkout_status = failed`. The `CheckoutSession` record itself is never deleted — it transitions to a terminal status so the caller can inspect what went wrong.

### 5.2 Idempotency Key

The Ent schema already has a partial unique index:
```
idx_checkout_session_idempotency_key_active
ON (tenant_id, environment_id, idempotency_key)
WHERE idempotency_key IS NOT NULL AND checkout_status IN ('initiated', 'pending')
```

The repository already catches the constraint violation and returns `ErrAlreadyExists` → HTTP 409 with a descriptive hint. No additional code needed in the repository.

The service propagates `ErrAlreadyExists` as-is:

```go
// internal/ee/service/checkout_session.go — Create()
if err := s.CheckoutSessionRepo.Create(ctx, session); err != nil {
    // TODO: on ErrAlreadyExists (idempotency key conflict), consider fetching and returning
    // the existing session transparently (HTTP 200) instead of propagating 409 — matching
    // Stripe's behavior so callers don't need special conflict handling on retries.
    return nil, err
}
```

---

## 6. CheckoutProvider Interface

**File:** `internal/interfaces/checkout_provider.go`

```go
package interfaces

// CheckoutProvider is implemented by each payment gateway that supports hosted checkout.
// The checkout session service calls this without knowing the underlying provider.
type CheckoutProvider interface {
    CreatePaymentLink(ctx context.Context, req CheckoutProviderRequest, customerSvc CustomerService, invoiceSvc InvoiceService) (*CheckoutProviderResponse, error)
}

type CheckoutProviderRequest struct {
    InvoiceID     string
    CustomerID    string
    Amount        decimal.Decimal
    Currency      string
    PaymentID     string           // FlexPrice payment ID — embedded in provider metadata for idempotency
    EnvironmentID string
    SuccessURL    string
    FailureURL    string
    CancelURL     string
    Metadata      map[string]string
}

type CheckoutProviderResponse struct {
    ProviderSessionID       string
    NextAction              types.PaymentAction
    ProviderPaymentIntentID string
    ExpiresAt               *time.Time
    ProviderMetadata        map[string]string
}
```

---

## 7. Per-Provider Adapters

Each adapter wraps the existing `PaymentService.CreatePaymentLink()` — that method is **not modified**.

**Provider → unified field mapping:**

| Provider | `ProviderSessionID` | `NextAction.Type` | `NextAction.URL` | `ProviderPaymentIntentID` |
|---|---|---|---|---|
| Stripe | `cs_xxx` | `checkout_url` | Stripe-hosted URL | `pi_xxx` (at creation) |
| Razorpay | `plink_xxx` | `payment_link` | Short URL | `""` |
| Nomod | payment link ID | `payment_link` | Nomod URL | `""` |
| Moyasar | payment ID | `payment_link` | Transaction URL | payment ID |

**Stripe adapter** (`internal/integration/stripe/checkout_adapter.go`):
```go
type CheckoutAdapter struct{ Svc *PaymentService }

func (a *CheckoutAdapter) CreatePaymentLink(ctx context.Context, req interfaces.CheckoutProviderRequest, customerSvc interfaces.CustomerService, invoiceSvc interfaces.InvoiceService) (*interfaces.CheckoutProviderResponse, error) {
    r, err := a.Svc.CreatePaymentLink(ctx, &dto.CreateStripePaymentLinkRequest{
        InvoiceID: req.InvoiceID, CustomerID: req.CustomerID,
        Amount: req.Amount, Currency: req.Currency,
        SuccessURL: req.SuccessURL, CancelURL: req.CancelURL,
        Metadata: req.Metadata, EnvironmentID: req.EnvironmentID, PaymentID: req.PaymentID,
    }, customerSvc, invoiceSvc)
    if err != nil {
        return nil, err
    }
    return &interfaces.CheckoutProviderResponse{
        ProviderSessionID:       r.ID,
        NextAction:              types.PaymentAction{Type: types.PaymentActionTypeCheckoutURL, URL: r.PaymentURL},
        ProviderPaymentIntentID: r.PaymentIntentID,
    }, nil
}
```

Razorpay, Nomod, Moyasar adapters follow the same pattern — map their existing response fields to the unified struct, set `NextAction.Type = PaymentActionTypePaymentLink`.

**Nomod adapter note:** `ProviderMetadata: {"reference_id": r.ReferenceID}` — see Section 9.4 for why the reference ID is logged.

---

## 8. Factory Method

**File:** `internal/integration/factory.go`

```go
func (f *IntegrationFactory) GetCheckoutProvider(ctx context.Context, provider types.CheckoutPaymentProvider) (interfaces.CheckoutProvider, error) {
    switch provider {
    case types.CheckoutPaymentProviderStripe:
        i, err := f.GetStripeIntegration(ctx); if err != nil { return nil, err }
        return &stripe.CheckoutAdapter{Svc: i.PaymentSvc}, nil
    case types.CheckoutPaymentProviderRazorpay:
        i, err := f.GetRazorpayIntegration(ctx); if err != nil { return nil, err }
        return &razorpay.CheckoutAdapter{Svc: i.PaymentSvc}, nil
    case types.CheckoutPaymentProviderNomod:
        i, err := f.GetNomodIntegration(ctx); if err != nil { return nil, err }
        return &nomod.CheckoutAdapter{Svc: i.PaymentSvc}, nil
    case types.CheckoutPaymentProviderMoyasar:
        i, err := f.GetMoyasarIntegration(ctx); if err != nil { return nil, err }
        return &moyasar.CheckoutAdapter{Svc: i.PaymentSvc}, nil
    default:
        return nil, ierr.NewError("payment provider not supported for checkout").
            WithHintf("%s does not support hosted checkout sessions", provider).
            Mark(ierr.ErrValidation)
    }
}
```

---

## 9. Enum Expansion

**File:** `internal/types/checkout.go`

```go
CheckoutPaymentProviderRazorpay CheckoutPaymentProvider = "razorpay"
CheckoutPaymentProviderNomod    CheckoutPaymentProvider = "nomod"
CheckoutPaymentProviderMoyasar  CheckoutPaymentProvider = "moyasar"
```

Update `Validate()` and `SessionExpiry()`:
- Razorpay: 15 minutes
- Nomod: 30 minutes
- Moyasar: 15 minutes

---

## 10. Service Wiring — `executeCheckoutAction`

**File:** `internal/ee/service/checkout_session_actions.go`

The full `executeCheckoutAction` flow (called after the session is already in DB with `status = initiated`):

```
[action = create_subscription]
1. createDraftSubscription → draft Subscription + draft Invoice (amounts computed)
2. createCheckoutPayment  → FlexPrice Payment record (status = pending)

[all actions]
3. GetCheckoutProvider(ctx, session.PaymentProvider)
4. Build CheckoutProviderRequest from session + invoice + payment
5. Call provider.CreatePaymentLink()
6. IF providerResp.ExpiresAt != nil && providerResp.ExpiresAt.Before(session.ExpiresAt):
       session.ExpiresAt = *providerResp.ExpiresAt   // tighten to provider's URL expiry
7. Create EntityIntegrationMapping:
       ProviderEntityID = providerResp.ProviderSessionID
       EntityID         = payResp.ID   (FlexPrice payment ID)
       EntityType       = IntegrationEntityTypePayment
       ProviderType     = session.PaymentProvider.String()
8. Set session.ProviderResult = &CheckoutProviderResult{
       NextAction:              &providerResp.NextAction,
       ProviderSessionID:       providerResp.ProviderSessionID,
       ProviderPaymentIntentID: providerResp.ProviderPaymentIntentID,
       ExpiresAt:               providerResp.ExpiresAt,
       ProviderMetadata:        providerResp.ProviderMetadata,
   }
9.  Set session.CheckoutStatus = CheckoutStatusPending
10. CheckoutSessionRepo.Update(ctx, session)   ← second DB write, status = pending

[back in Create()]
11. Publish checkout.session.initiated webhook
12. Return CheckoutSessionResponse to caller (ProviderResult zeroed; PaymentAction surfaced)
```

If any step 1–10 fails, `CleanupCheckoutSession(ctx, session, err)` is called: deletes created entities (IsNotFound = ok), sets `checkout_status = failed`, publishes `checkout.session.failed`.

---

## 11. `CompleteCheckoutSession` — Atomic Idempotency

### 11.1 New repository method

Add to `internal/domain/checkout/repository.go`:

```go
// MarkCompleted atomically transitions the session from pending/initiated to completed.
// Returns (true, nil) if this call claimed the transition.
// Returns (false, nil) if the session was already in a terminal state — idempotent no-op.
MarkCompleted(ctx context.Context, sessionID string, completedAt time.Time, providerResult *types.CheckoutProviderResult) (bool, error)
```

Implement in `internal/repository/ent/checkout_session.go` using Ent's conditional update:

```go
func (r *checkoutSessionRepository) MarkCompleted(ctx context.Context, sessionID string, completedAt time.Time, providerResult *types.CheckoutProviderResult) (bool, error) {
    n, err := r.client.Writer(ctx).CheckoutSession.Update().
        Where(
            entCheckout.ID(sessionID),
            entCheckout.TenantID(types.GetTenantID(ctx)),
            entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
            entCheckout.CheckoutStatusIn(types.CheckoutStatusPending, types.CheckoutStatusInitiated),
        ).
        SetCheckoutStatus(types.CheckoutStatusCompleted).
        SetCompletedAt(completedAt).
        SetProviderResult(providerResult).
        Save(ctx)
    if err != nil {
        return false, ierr.WithError(err).WithHint("failed to mark checkout session completed").Mark(ierr.ErrDatabase)
    }
    return n > 0, nil
}
```

### 11.2 Revised `CompleteCheckoutSession` flow

```
1. Validate sessionID not empty
2. GET session (for completeCheckoutAction context)
3. Fast-path guard: if status is already completed/failed/expired → return ErrConflict immediately
4. completeCheckoutAction(ctx, session, providerResult)
   — each sub-step is idempotent (see 11.3); safe to run in parallel with another webhook delivery
5. MarkCompleted(ctx, sessionID, now, providerResult)
   — atomic conditional UPDATE WHERE status IN (pending, initiated)
   — if returned false: another process claimed it simultaneously → return ErrConflict
   — if returned true: WE completed it → publish checkout.session.completed webhook
```

Step 4 before step 5 ensures the subscription/invoice/payment are in final state BEFORE the session transitions. If two webhooks race, both run step 4 idempotently; only one claims step 5.

### 11.3 Idempotent sub-steps in `completeSubscriptionCheckout`

Each downstream write must be conditional:

| Step | Idempotent guard |
|---|---|
| Activate subscription | `UPDATE WHERE status = 'draft'` — no-op if already active |
| Finalize invoice | Check `invoice.Status != finalized` before calling `FinalizeInvoice` |
| Mark payment succeeded | `UpdatePayment` only sets fields that differ; already-succeeded is a no-op |
| Reconcile invoice | `ReconcilePaymentStatus` is already idempotent (sets paid if not already) |

### 11.4 Webhook handler on `ErrConflict`

```go
err = checkoutSessionSvc.CompleteCheckoutSession(ctx, session.ID, providerResult)
if err != nil {
    if ierr.IsConflict(err) {
        return nil  // already completed — idempotent, return HTTP 200 to gateway
    }
    return err  // real error — return HTTP 500, gateway will retry
}
```

---

## 12. Webhook Routing

### Decision tree (all 4 providers)

```
Webhook arrives
  → Verify signature
  → Extract lookup ID (provider-specific, see below)
  → IF lookup ID empty → existing invoice reconciliation flow, done
  → EntityIntegrationMappingRepo.List({ProviderEntityIDs:[id], ProviderType, EntityType:payment})
  → IF no mapping → existing invoice reconciliation flow, done
  → Get FlexPrice payment via mapping.EntityID
  → CheckoutSessionRepo.List({CheckoutPaymentID: payment.ID, Statuses: [pending, initiated]})
  → IF no session → existing invoice reconciliation flow, done
  → Build CheckoutProviderResult{ProviderPaymentIntentID: <from webhook>}
  → CompleteCheckoutSession(ctx, session.ID, providerResult)
  → IF ErrConflict → swallow, return 200
  → Return 200 — do NOT fall through to invoice reconciliation
```

### 12.1 Stripe — `checkout.session.completed`

**Lookup ID:** `checkoutSession.ID` (`cs_xxx`)
**ProviderPaymentIntentID:** `checkoutSession.PaymentIntent.ID`

Current handler reads `flexprice_payment_id` from metadata. **Keep this as fallback** for sessions created before this feature ships: if EntityIntegrationMapping lookup returns nothing, fall through to the existing metadata path.

### 12.2 Razorpay — `payment_link.paid`

**Lookup ID:** `event.Payload.PaymentLink.Entity.ID` (`plink_xxx`)
**ProviderPaymentIntentID:** `event.Payload.Payment.Entity.ID`

Add a new `case "payment_link.paid"` handler. Existing `payment.captured` handler is unchanged.

### 12.3 Moyasar — `payment_paid`

**Lookup ID:** `payload.ID` (Moyasar payment ID — same at creation and webhook)
**ProviderPaymentIntentID:** `payload.ID`

### 12.4 Nomod — charge success

**Lookup ID:** `payload.PaymentLinkID` — NOT `payload.ID`

```go
// NOTE: Nomod's webhook carries a Charge ID as the primary event ID (payload.ID) and a
// separate PaymentLinkID for payment-link payments. We store the PaymentLinkID in
// EntityIntegrationMapping at creation, so we MUST look up by PaymentLinkID here.
// Using payload.ID (the charge ID) would always miss — this is a Nomod-specific quirk.
if payload.PaymentLinkID != nil && *payload.PaymentLinkID != "" {
    lookupID = *payload.PaymentLinkID
}
```

**ProviderPaymentIntentID:** `payload.ID` (the Charge ID — stored as `gateway_payment_id` on Payment)

### 12.5 Add `CheckoutSessionService` to `ServiceDependencies`

```go
// internal/interfaces/service.go
type ServiceDependencies struct {
    // existing fields ...
    CheckoutSessionService CheckoutSessionService  // ADD
}
```

Wire it in `internal/api/v1/webhook.go` where `ServiceDependencies` is constructed per provider.

---

## 13. `CleanupCheckoutSession` — Idempotency

### 13.1 Early guard

Return immediately if the session is already in a terminal state:

```go
func (s *checkoutSessionService) CleanupCheckoutSession(ctx context.Context, session *domainCheckout.CheckoutSession, reason error) error {
    switch session.CheckoutStatus {
    case types.CheckoutStatusCompleted, types.CheckoutStatusFailed, types.CheckoutStatusExpired:
        return nil  // already terminal — idempotent no-op
    }
    // ... proceed with cleanup
}
```

### 13.2 Treat `IsNotFound` as success on each delete

```go
if err := s.PaymentRepo.Delete(ctx, res.PaymentID); err != nil && !ierr.IsNotFound(err) {
    s.Logger.Error(ctx, "failed to delete checkout payment", ...)
}
// same pattern for InvoiceRepo.Delete and SubRepo.Delete
```

### 13.3 Terminal status depends on reason

```go
if reason != nil {
    session.CheckoutStatus = types.CheckoutStatusFailed
    msg := reason.Error()
    session.FailureReason = &msg
} else {
    session.CheckoutStatus = types.CheckoutStatusExpired  // natural expiry, not an error
}
return s.CheckoutSessionRepo.Update(ctx, session)
```

And publish the appropriate webhook after Update:
- `reason != nil` → `checkout.session.failed`
- `reason == nil` → `checkout.session.expired`

---

## 14. `checkout.session.expired` — Temporal Workflow

**Pattern:** follows `wallet_credit_expiry_workflow.go` — Temporal schedule fires every 5 minutes.

**New file:** `internal/temporal/workflows/cron/checkout_session_expiry_workflow.go`

**Activity:** `ExpireCheckoutSessions(ctx)`

```
1. List sessions WHERE checkout_status IN ('initiated', 'pending') AND expires_at < NOW() LIMIT 100
2. For each session:
   a. CleanupCheckoutSession(ctx, session, nil)   // reason=nil → status=expired
   b. Logs the expiry; webhook is published inside CleanupCheckoutSession
3. If 100 sessions returned, re-trigger immediately (drain the backlog)
```

Register in `internal/temporal/registration.go`. Schedule created on worker startup (same pattern as wallet credit expiry schedule).

---

## 15. File Map

| Action | File |
|---|---|
| Add `PaymentAction` struct | `internal/types/checkout.go` |
| Add `WebhookEventCheckoutSessionExpired` | `internal/types/webhook.go` |
| Flatten `CheckoutProviderResult` + `PaymentActionForUser()` | `internal/types/checkout_configuration.go` |
| Remove `PaymentAction` from dto, simplify `ToCheckoutSessionResponse` | `internal/api/dto/checkout_session.go` |
| `CheckoutProvider` interface + request/response types | `internal/interfaces/checkout_provider.go` |
| Stripe adapter | `internal/integration/stripe/checkout_adapter.go` |
| Razorpay adapter | `internal/integration/razorpay/checkout_adapter.go` |
| Nomod adapter | `internal/integration/nomod/checkout_adapter.go` |
| Moyasar adapter | `internal/integration/moyasar/checkout_adapter.go` |
| `GetCheckoutProvider()` factory method | `internal/integration/factory.go` |
| Expand `CheckoutPaymentProvider` enum + `SessionExpiry()` | `internal/types/checkout.go` |
| Wire provider call + EntityIntegrationMapping + ExpiresAt tightening | `internal/ee/service/checkout_session_actions.go` |
| `MarkCompleted` on Repository interface | `internal/domain/checkout/repository.go` |
| `MarkCompleted` implementation | `internal/repository/ent/checkout_session.go` |
| Revised `CompleteCheckoutSession` (atomic MarkCompleted, idempotent sub-steps) | `internal/ee/service/checkout_session.go` |
| Idempotent `CleanupCheckoutSession` (guard + IsNotFound + expired status) | `internal/ee/service/checkout_session.go` |
| Add `CheckoutSessionService` to `ServiceDependencies` | `internal/interfaces/service.go` |
| Stripe webhook: EntityIntegrationMapping routing + metadata fallback | `internal/integration/stripe/webhook/handler.go` |
| Razorpay webhook: `payment_link.paid` handler | `internal/integration/razorpay/webhook/handler.go` |
| Nomod webhook: PaymentLinkID lookup + ErrConflict swallow | `internal/integration/nomod/webhook/handler.go` |
| Moyasar webhook: EntityIntegrationMapping routing | `internal/integration/moyasar/webhook/handler.go` |
| Wire `CheckoutSessionService` into webhook deps | `internal/api/v1/webhook.go` |
| Checkout session expiry Temporal workflow | `internal/temporal/workflows/cron/checkout_session_expiry_workflow.go` |
| Register expiry workflow + schedule | `internal/temporal/registration.go` |

---

## 16. What Is NOT Changed

- Existing `PaymentService.CreatePaymentLink()` on all 4 providers
- `CheckoutResult` (FlexPrice entity IDs) — stays action-scoped
- Invoice reconciliation paths in existing webhook handlers — preserved as fallback
- `CheckoutStatusExpired` enum value — already exists in `internal/types/checkout.go`

---

## 17. Open Questions Resolved

| Question | Decision |
|---|---|
| Idempotency key conflict → 409 or transparent 200? | 409 for now; TODO in service layer for future Stripe-style transparent return |
| Duplicate webhook completing session twice? | `MarkCompleted` conditional UPDATE + each sub-step idempotent; only claimer publishes webhook |
| `CleanupCheckoutSession` partial failure? | `IsNotFound` treated as success; guard at top skips already-terminal sessions |
| Expiry worker — cron or Temporal? | Temporal (consistent with wallet credit expiry pattern) |
| `reason == nil` in `CleanupCheckoutSession` → failed or expired? | `nil` → `expired` + `checkout.session.expired` webhook; non-nil → `failed` |
| Nomod ID mismatch (link ID vs charge ID)? | Use `payload.PaymentLinkID` for EntityIntegrationMapping lookup; store charge ID as `gateway_payment_id` |
| Stripe metadata fallback? | Keep for pre-existing sessions; EntityIntegrationMapping is primary going forward |
| Paddle / Whop? | `GetCheckoutProvider` returns `ErrValidation` "not supported for checkout" |
| Provider URL expires before session? | Tighten `session.ExpiresAt` to provider's `ExpiresAt` in `executeCheckoutAction` |
