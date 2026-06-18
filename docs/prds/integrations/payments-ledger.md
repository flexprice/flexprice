# Payment Lifecycle — Integration Layer PRD

## Overview

A universal payments ledger (`internal/integration/ledger`) that every third-party gateway integration uses to track Flexprice-initiated payments end-to-end. All payments Flexprice initiates at any gateway go through this ledger. The module is integration-layer only — it depends on `interfaces.PaymentService` and `interfaces.InvoiceService`, never on integration-specific packages, so there are no circular dependencies.

---

## Problem

Without this module every integration reinvents the same pattern:
- Ad-hoc payment record creation
- Scattered status updates
- No consistent traceability from initiation → gateway confirmation → webhook resolution
- Copy-paste `flexprice_payment_id` metadata wiring
- No shared failure/success recording contract

---

## Payment Lifecycle States

```
INITIATED ──► PENDING ──► SUCCEEDED ──► VOIDED
                     │              └──► REFUNDED
                     └──► FAILED
```

| State | Meaning | Trigger |
|---|---|---|
| `INITIATED` | Flexprice created the payment record; gateway call not yet made | `InitiatePayment` |
| `PENDING` | Gateway accepted the charge; we have a `gateway_payment_id` | `ConfirmGatewayPayment` |
| `SUCCEEDED` | Webhook confirmed payment success | `RecordPaymentSuccess` |
| `FAILED` | Webhook or gateway call confirmed failure | `RecordPaymentFailure` |
| `VOIDED` | AUTH charge reversed at gateway after token was saved | `RecordPaymentVoided` (AUTH only) |
| `REFUNDED` | AUTH charge refunded at gateway after token was saved | `RecordPaymentRefunded` (AUTH only) |

### Terminal States

`VOIDED`, `REFUNDED`, and `FAILED` are terminal — no further transitions.

`SUCCEEDED` is **not** terminal. AUTH payments transition `SUCCEEDED → VOIDED` or `SUCCEEDED → REFUNDED` after the token has been saved.

`INVOICE` payments end at `SUCCEEDED` or `FAILED`. There is no lifecycle-managed refund path for INVOICE — use a separate refund workflow if needed.

`RecordPaymentVoided` and `RecordPaymentRefunded` require `current_status == SUCCEEDED` before transitioning.

---

## Destination Types

| Destination | `destination_id` | Purpose |
|---|---|---|
| `INVOICE` | `invoice_id` | Autopay — charge customer for a due invoice |
| `AUTH` | `customer_id` | Save payment method — gateway verifies card, token stored on success |

---

## Schema Changes

- `payments.voided_at` (`TIMESTAMPTZ`, nullable) — set by `RecordPaymentVoided`
- `payments.refunded_at` (`TIMESTAMPTZ`, nullable) — pre-existing, set by `RecordPaymentRefunded`
- `payments.succeeded_at` (`TIMESTAMPTZ`, nullable) — pre-existing, set by `RecordPaymentSuccess`
- Payments created via `InitiatePayment` start with status `INITIATED` (not `PENDING`) — controlled by `ProcessPayment: false` in `CreatePaymentRequest`

---

## Module Interface

**Package:** `internal/integration/ledger`  
**Type:** `PaymentsLedger`  
**Constructor:** `NewPaymentsLedger` — registered in FX directly alongside service layer providers in `cmd/server/main.go`

```go
type PaymentsLedger struct {
    paymentService interfaces.PaymentService
    invoiceService interfaces.InvoiceService
    logger         *logger.Logger
}

InitiatePayment(ctx, params InitiatePaymentParams) (flexpricePaymentID string, err error)
ConfirmGatewayPayment(ctx, flexpricePaymentID, gatewayPaymentID string) error
RecordPaymentSuccess(ctx, params RecordPaymentSuccessParams) error
RecordPaymentFailure(ctx, params RecordPaymentFailureParams) error
RecordPaymentVoided(ctx, params RecordPaymentVoidedParams) error
RecordPaymentRefunded(ctx, params RecordPaymentRefundedParams) error
```

### Params

```go
type InitiatePaymentParams struct {
    DestinationType   types.PaymentDestinationType // INVOICE | AUTH
    DestinationID     string                        // invoice_id | customer_id
    PaymentMethodType types.PaymentMethodType       // CARD, ACH, etc.
    Gateway           string                        // "moyasar" | "stripe" | ...
    Amount            decimal.Decimal
    Currency          string
}

type RecordPaymentSuccessParams struct {
    FlexpricePaymentID string
    GatewayPaymentID   string    // optional — gateway may confirm async
    SucceededAt        time.Time
}

type RecordPaymentFailureParams struct {
    FlexpricePaymentID string
    GatewayPaymentID   string    // optional
    ErrorMessage       string
}

type RecordPaymentVoidedParams struct {
    FlexpricePaymentID string
    GatewayPaymentID   string
}

type RecordPaymentRefundedParams struct {
    FlexpricePaymentID string
    GatewayPaymentID   string
    RefundedAt         time.Time
}
```

No `PaymentMethodID` or `CustomerID` in any param — payment method resolution is the responsibility of each integration module.

---

## `flexprice_payment_id` Passing Contract

Every gateway call Flexprice initiates **must** pass `flexprice_payment_id` in the gateway's metadata. This is the only link between the gateway's async event and our payment record.

| Gateway | Metadata field |
|---|---|
| Moyasar | `metadata["flexprice_payment_id"]` |
| Stripe | `metadata["flexprice_payment_id"]` |
| Razorpay | `notes["flexprice_payment_id"]` |
| Paddle | `custom_data["flexprice_payment_id"]` |

If a webhook arrives without `flexprice_payment_id` → it is an external payment (customer paid directly at gateway). That path is handled by provider-specific `ProcessExternalPayment` logic, not this ledger module.

---

## Complete Flow: INVOICE Autopay

```
Temporal workflow
│
├── 1. ledger.InitiatePayment(params)
│        creates payment record  →  status = INITIATED
│        returns flexprice_payment_id
│
├── 2. integration.SyncInvoice(invoiceID)
│        creates invoice at gateway, returns gateway_invoice_id
│
├── 3. gateway.Charge(token, amount, metadata{
│           "flexprice_payment_id": flexprice_payment_id
│       })
│        if gateway call fails:
│            log error with all IDs (flexprice_payment_id, customer_id, invoice_id)
│            surface error up — Temporal will retry
│
├── 4. ledger.ConfirmGatewayPayment(flexprice_payment_id, gateway_payment_id)
│        payment  →  PENDING
│
└── (async) Webhook fires at /v1/webhooks/{provider}/:tenant_id/:environment_id
         │
         ├── payment_paid / payment_captured
         │       → read metadata["flexprice_payment_id"]
         │       → ledger.RecordPaymentSuccess(...)
         │               payment  →  SUCCEEDED
         │               InvoiceService.ReconcilePaymentStatus(invoice_id, amount)
         │
         └── payment_failed
                 → read metadata["flexprice_payment_id"]
                 → ledger.RecordPaymentFailure(...)
                         payment  →  FAILED
```

---

## Complete Flow: AUTH (Save Payment Method)

### Gateway requires payment to tokenize (Moyasar, Razorpay)

```
Frontend
│
├── User submits card on gateway hosted form
├── Gateway processes minimal auth charge
├── Gateway fires webhook payment_paid
│
└── Webhook handler
         → flexprice_payment_id present in metadata
         → ledger.RecordPaymentSuccess(...)   payment → SUCCEEDED
         → extract token from webhook source payload
         → paymentMethodRepo.Create(PaymentMethod{
               CustomerID:          destination_id,  // AUTH.destination_id = customer_id
               Gateway:             gateway,
               GatewayMethodID:     token_id,
               Type:                CARD,
               PaymentMethodStatus: ACTIVE,
               MethodDetails:       { brand, name, last4, ... }
           })
         → idempotency: skip if gateway_method_id already exists for customer+gateway

Cron/Manual void:
         → gateway.VoidCharge(gateway_payment_id)
         → ledger.RecordPaymentVoided(...)   payment → VOIDED, voided_at = now
```

### Gateway supports setup-only (Stripe, Adyen, Braintree)

```
Frontend → gateway SetupIntent → webhook setup_intent.succeeded
→ integration module creates PaymentMethod record directly
→ ledger module NOT involved (no payment, no INITIATED state)
```

---

## Webhook Routing

Each provider has a dedicated URL. URL carries tenant + environment identity:

```
POST /v1/webhooks/moyasar/:tenant_id/:environment_id
POST /v1/webhooks/stripe/:tenant_id/:environment_id
POST /v1/webhooks/razorpay/:tenant_id/:environment_id
```

Webhook handler contract:
1. Read `flexprice_payment_id` from event metadata
2. Call `ledger.RecordPaymentSuccess` or `RecordPaymentFailure`
3. For `INVOICE` destination → reconcile invoice via `InvoiceService.ReconcilePaymentStatus`
4. For `AUTH` destination → create/activate PaymentMethod record
5. **Always return HTTP 200** — errors are logged internally, never surfaced to gateway

---

## Idempotency

- `InitiatePayment` — duplicate calls with same `idempotency_key` return the existing payment ID without creating a new record
- All `Record*` methods — if the payment is already in the specific target state, log and return nil; if in any other terminal state, return an error
- PaymentMethod creation — checks `gateway_method_id` uniqueness per customer+gateway before inserting

---

## FX Wiring

`NewPaymentsLedger` is registered directly in `cmd/server/main.go` alongside other service-layer providers. It takes `interfaces.PaymentService` and `interfaces.InvoiceService` from the FX graph — no circular dependency with the integration factory.

---

## Files

| File | Purpose |
|---|---|
| `internal/integration/ledger/payments_ledger.go` | `PaymentsLedger` struct + 6 methods |
| `internal/integration/ledger/dto.go` | Input param types for all ledger types |
| `internal/types/payment.go` | `PaymentDestinationTypeAuth`, `PaymentStatus.IsTerminal()` |
| `ent/schema/payment.go` | `voided_at` field |
| `internal/domain/payment/model.go` | `VoidedAt` field, `FromEnt` mapping |
| `internal/api/dto/payment.go` | `VoidedAt` in request/response, INITIATED status for ledger-created payments |
| `internal/ee/service/payment.go` | AUTH destination support, scoped invoice logic |

---

## Out of Scope (this PRD)

- Retry workflows for FAILED payments
- Per-provider wiring (Moyasar first, others follow same pattern)
- Service + API layer for PaymentMethod CRUD
