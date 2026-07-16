# Payment Gateway Polling on GetPayment

**Date:** 2026-07-16  
**Branch:** feat/payments-polling  
**Status:** Approved

## Problem

Payments that reach a terminal state at the gateway (succeeded, failed) can get stuck in `PENDING` or `PROCESSING` in FlexPrice if the gateway webhook is delayed or dropped. The only update path today is the webhook, which is unreliable under network hiccups or misconfiguration.

## Solution

When `GET /payments/{id}` is called and the payment is in a non-terminal, in-flight state (`PENDING` or `PROCESSING`) and has a `gateway_payment_id`, synchronously fetch the latest status from the payment gateway before returning. If the gateway status differs from the DB state, apply the appropriate lifecycle transition and return the fresh record.

## Scope

- **In scope:** Stripe, Razorpay, Moyasar
- **Out of scope (skipped gracefully):** Nomod, Paddle (no status-fetch API)
- **Not included:** throttling/cooldown, background async sync, proactive polling crons

## Design

### Trigger Conditions

Gateway polling fires inside `GetPayment` if **all** of the following are true:

1. `payment.PaymentStatus` is `PENDING` or `PROCESSING`
2. `payment.GatewayPaymentID` is non-nil and non-empty
3. `payment.PaymentGateway` is one of `stripe`, `razorpay`, `moyasar`

All other cases (terminal states, missing gateway ID, unsupported gateway) return the DB state immediately.

### Code Structure

No new files. Changes are confined to:

- `internal/ee/service/payment.go` — add `syncPaymentStatusFromGateway` private method; call it from `GetPayment`
- `internal/integration/stripe/payment.go` — surface `GetPaymentStatus(ctx, paymentIntentID, environmentID)`
- `internal/integration/razorpay/payment.go` — add `GetPaymentStatus(ctx, razorpayPaymentID, environmentID)`
- `internal/integration/moyasar/payment.go` — wrap existing `GetPaymentStatus` to return `types.PaymentStatus`

### `GetPayment` Change

After the existing DB fetch and before response building, one call is added:

```go
// Best-effort gateway sync; degrades gracefully on error
p, _ = s.syncPaymentStatusFromGateway(ctx, p)
```

The error is intentionally discarded here — `syncPaymentStatusFromGateway` logs internally and always returns the best available payment (fresh if sync succeeded, original if not).

### `syncPaymentStatusFromGateway` Method

Private method on `paymentService`:

```
// TODO: extract into a GatewayStatusSyncer when supporting more gateways or throttling
func (s *paymentService) syncPaymentStatusFromGateway(ctx, p) (*Payment, error)
```

Steps:
1. Guard check (status, gateway ID, supported gateway) — return `p, nil` if not applicable
2. Switch on `p.PaymentGateway` → call the relevant integration's `GetPaymentStatus`
3. On gateway error → log with payment ID + gateway name, return original `p` + error (caller ignores error)
4. Map gateway-native status → `types.PaymentStatus`
5. If mapped status == current DB status → no-op, return `p, nil`
6. Apply lifecycle transition (see table below)
7. Re-fetch payment from repo and return fresh record

### Status Mapping

#### Stripe (`payment_intent` status)

| Stripe status | FlexPrice status |
|---|---|
| `succeeded` | `SUCCEEDED` |
| `requires_payment_method`, `canceled` | `FAILED` |
| all others | `PENDING` (no-op) |

#### Razorpay

| Razorpay status | FlexPrice status |
|---|---|
| `captured` | `SUCCEEDED` |
| `failed` | `FAILED` |
| all others | `PENDING` (no-op) |

#### Moyasar

| Moyasar status | FlexPrice status |
|---|---|
| `paid` | `SUCCEEDED` |
| `failed` | `FAILED` |
| all others | `PENDING` (no-op) |

### Lifecycle Transitions

| Gateway says | DB status | Action |
|---|---|---|
| `SUCCEEDED` | `PENDING` / `PROCESSING` | `lifecycle.RecordPaymentSuccess` |
| `FAILED` | `PENDING` / `PROCESSING` | `lifecycle.RecordPaymentFailure` |
| same as DB | any | no-op |

`RecordPaymentSuccess` and `RecordPaymentFailure` are idempotent — safe if a concurrent webhook already applied the transition.

### Error Handling

All errors from the gateway call are:
- Logged at `warn` level with `payment_id`, `gateway`, `gateway_payment_id`
- Swallowed — `GetPayment` returns 200 with DB state

The caller never sees a 5xx due to a gateway being unavailable.

## What Is Not Changing

- No changes to webhook handlers
- No changes to `PaymentLifecycle` internals
- No new DB columns or migrations
- No throttling or Redis cooldown keys
- `SUCCEEDED`, `VOIDED`, `REFUNDED`, `FAILED` payments are never polled (already at or past actionable state)

## Future Improvements

- Extract `syncPaymentStatusFromGateway` into a standalone `GatewayStatusSyncer` in `internal/integration/payments/` with a `PaymentStatusFetcher` interface — enables per-gateway throttling, retries, and easier testing
- Add per-payment Redis cooldown (e.g. 30s TTL) to avoid gateway rate limits under aggressive polling
- Extend to Nomod/Paddle if those gateways add status-fetch APIs
