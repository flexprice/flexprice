# Paddle Subscription Sync — Design Spec

**Date:** 2026-05-20  
**Branch:** feat/paddle-sub-sync  
**Status:** Approved

---

## Problem

The current Paddle integration creates a $0 bootstrap transaction and expects Paddle to return a `subscription_id` synchronously (auto-complete). This only works when the customer already has a saved payment method. For new customers, Paddle keeps the transaction in `ready` state and `subscription_id` is `null` until the customer completes the checkout flow.

This means invoice charges (`CreateSubscriptionCharge`) will always hard-fail for new customers because there is no Paddle subscription to charge until the customer completes checkout.

---

## Goals

1. Bootstrap Paddle subscriptions via a $0 checkout flow (card capture without charging).
2. Activate FlexPrice subscriptions from `incomplete` → `active` (or `trialing`) once the customer completes checkout, via the `subscription.activated` Paddle webhook.
3. Block invoice sync until the Paddle subscription is fully activated; auto-trigger subscription sync if it hasn't been started.
4. Fix invoice line item display names in Paddle by using catalog prices (not inline prices) when creating subscription charges.

---

## Non-Goals

- Changing the FlexPrice subscription creation API shape.
- Handling Paddle subscription cancellation or update webhooks (separate scope).
- Retrying invoice sync automatically after subscription activation (operator re-triggers manually).

---

## Subscription Sync State Machine

A FlexPrice subscription's Paddle sync progresses through two states tracked by two different data stores:

| State | Indicator | Meaning |
|---|---|---|
| **Never synced** | No `entity_integration_mapping`, no `sub.Metadata["paddle_transaction_id"]` | Sync has never been started |
| **Sync initiated** | `sub.Metadata["paddle_transaction_id"]` exists, no mapping | Bootstrap transaction created, customer has not completed checkout |
| **Fully synced** | `entity_integration_mapping` exists with real `sub_xxx` as `provider_entity_id` | Customer completed checkout, `subscription.activated` webhook received |

No `sync_status` field is needed. The presence of the mapping alone signals full activation.

---

## Data Changes

### FlexPrice subscription metadata (new keys)

Set during `SyncSubscriptionToPaddle` activity:

| Key | Value | When set |
|---|---|---|
| `paddle_transaction_id` | `txn_xxx` | Bootstrap transaction created |
| `paddle_checkout_url` | `https://...` | Bootstrap transaction created |
| `paddle_subscription_id` | `sub_xxx` | `subscription.activated` webhook |

### Entity integration mapping (subscription)

Created only when `subscription.activated` fires:

| Field | Value |
|---|---|
| `entity_id` | FlexPrice subscription ID |
| `entity_type` | `subscription` |
| `provider_type` | `paddle` |
| `provider_entity_id` | Paddle `sub_xxx` ID |
| `metadata.paddle_subscription_id` | Paddle `sub_xxx` ID |
| `metadata.synced_at` | RFC3339 timestamp |

---

## Components

### 1. New Temporal Workflow — `PaddleSubscriptionSyncWorkflow`

**File:** `internal/temporal/workflows/paddle_subscription_sync_workflow.go`  
**Constant:** `WorkflowPaddleSubscriptionSync = "PaddleSubscriptionSyncWorkflow"`  
**Type constant:** `types.TemporalPaddleSubscriptionSyncWorkflow`

**Input model** (`internal/temporal/models/paddle_subscription_sync.go`):
```go
type PaddleSubscriptionSyncWorkflowInput struct {
    SubscriptionID string
    TenantID       string
    EnvironmentID  string
}
```

**Initial trigger:** `PaddleSubscriptionSyncWorkflow` is started by the system event dispatcher (`internal/integration/events/dispatch.go`) when a `subscription.created` system event is emitted for a subscription whose `CollectionMethod = charge_automatically` and `PaymentBehavior = allow_incomplete`, and a Paddle connection exists for the environment. This mirrors how `PaddleInvoiceSyncWorkflow` is triggered on invoice creation. The fire-and-forget path in `PaddleInvoiceSyncWorkflow` is a safety net for the race condition, not the primary trigger.

**Steps:**
1. Sleep 2s — let subscription commit to DB.
2. `EnsureCustomerSyncedToPaddle` activity — reuse existing activity; non-retryable on validation errors (missing email, no address).
3. `SyncSubscriptionToPaddle` activity — see below.

**Retry policy:** Max 3 attempts on transient errors. Validation errors are non-retryable (`temporal.NewNonRetryableApplicationError`).

---

### 2. New Activity — `SyncSubscriptionToPaddle`

**File:** `internal/temporal/activities/paddle/subscription_sync_activities.go`

**Logic (called via `PaddleSyncService.EnsureSubscriptionSynced`):**

```
1. Check entity_integration_mapping for this subscription
   → If mapping exists: return (already fully synced, idempotent)

2. Check sub.Metadata["paddle_transaction_id"]
   → If exists: return existing checkout URL (sync already initiated, skip CreateTransaction)

3. EnsureBulkProductSynced for all price IDs in subscription line items
   → Creates Paddle products for any unmapped FlexPrice prices

4. For each line item price, create a $0 catalog Paddle price:
   CreatePrice(
     product_id  = mapped Paddle product ID,
     name        = line item display name (or price ID fallback),
     unit_price  = $0,
     billing_cycle = subscription billing period + count,
     tax_mode    = account_setting,
   )

5. CreateTransaction(
     customer_id     = paddle customer ID,
     address_id      = paddle address ID,
     collection_mode = automatic,
     items           = [{price_id: $0_catalog_price_id, quantity: 1}, ...],
     custom_data     = {
       flexprice_subscription_id: sub.ID,
       environment_id: environmentID,
     }
   )
   → Transaction status will be "ready" (customer has not added payment method yet)
   → subscriptionId will be null at this point

6. Update sub.Metadata:
   paddle_transaction_id = txn.ID
   paddle_checkout_url   = txn.Checkout.URL (fallback: conn.Metadata["checkout_url"] + "?_ptxn=" + txn.ID)
   Save sub via subscriptionRepo.Update

7. Return checkout URL
```

**Validation errors (non-retryable):** no customer address, no line items.

---

### 3. Updated `PaddleInvoiceSyncWorkflow`

**File:** `internal/temporal/workflows/paddle_invoice_sync_workflow.go`

Adds Step 2.5 between customer sync and invoice sync:

```
Step 1: Sleep 5s
Step 2: EnsureCustomerSyncedToPaddle (existing)
Step 2.5: CheckSubscriptionSyncStatus (new activity)
  → Queries entity_integration_mapping for the invoice's subscription_id
  → Returns: "activated" | "not_synced"
  If "not_synced":
    → ExecuteChildWorkflow(PaddleSubscriptionSyncWorkflow,
        options: ParentClosePolicy=ABANDON, no wait)
    → return temporal.NewNonRetryableApplicationError(
        "paddle subscription sync triggered; re-run invoice sync after customer completes checkout",
        "SubscriptionNotSynced", nil)
  If "activated":
    → continue
Step 3: SyncInvoiceToPaddle (existing, unchanged)
```

The `CheckSubscriptionSyncStatus` activity takes `InvoiceID` as input, fetches the invoice to resolve `subscription_id`, then queries the mapping. Add `SubscriptionID string` to `PaddleInvoiceSyncWorkflowInput` and populate it when starting the workflow so the activity can also receive it directly as an optimisation — but the fetch-from-invoice fallback handles cases where it is empty.

---

### 4. Updated `SyncInvoice` — Catalog Prices for Charges

**File:** `internal/integration/paddle/sync_service.go`

Replace `SubscriptionChargeItemCreateWithPrice` (inline) with pre-created catalog prices:

```
For each invoice line item:
  1. Get mapped Paddle product ID from EnsureBulkProductSynced result
  2. CreatePrice(
       product_id   = paddleProductID,
       name         = line item display name (or price ID fallback),
       description  = line item display name,
       unit_price   = actual invoice amount (in smallest currency unit),
       currency     = line item currency (uppercased),
       tax_mode     = account_setting,
       quantity     = {min: 1, max: 100000},
       // No billing_cycle → one-time price
     )
  3. Collect price.ID

CreateSubscriptionCharge(
  subscription_id = paddleSubID,
  effective_from  = immediately,
  items           = [{price_id: pri_xxx, quantity: 1}, ...]  // catalog refs
)
```

`EnsureSubscriptionSynced` is still called before charges — but now, if the mapping exists, it simply returns the real `paddleSubID` from `provider_entity_id` and the inline syncing is removed from this path. The per-invoice prices are ephemeral (not stored in entity_integration_mapping); invoice-level idempotency is already handled by the existing invoice mapping guard at the top of `SyncInvoice`.

---

### 5. New Webhook Handler — `subscription.activated`

**Files:**
- `internal/integration/paddle/webhook/types.go` — add `EventSubscriptionActivated PaddleEventType = "subscription.activated"`
- `internal/integration/paddle/webhook/handler.go` — add `handleSubscriptionActivated` case
- `internal/integration/paddle/sync_service.go` — add `ProcessSubscriptionActivatedWebhook`

**Handler logic (`ProcessSubscriptionActivatedWebhook`):**

```
Input: paddlenotification.SubscriptionActivated event data

1. Extract paddleSubID = event.Data.ID
2. Extract flexSubID   = event.Data.CustomData["flexprice_subscription_id"]
   → If empty: log warning, return nil (can't link without it)
   (Context already has tenant + environment set by the webhook router before this method is called)

3. Create entity_integration_mapping:
   entity_id         = flexSubID
   entity_type       = subscription
   provider_type     = paddle
   provider_entity_id = paddleSubID
   metadata = {
     paddle_subscription_id: paddleSubID,
     synced_at: now(),
   }
   → Idempotent: if mapping already exists, skip creation

5. Fetch FlexPrice subscription (flexSubID)
6. Update sub.Metadata["paddle_subscription_id"] = paddleSubID → Save

7. Activate based on current sub status:
   - SubscriptionStatusIncomplete:
       If sub.TrialEnd != nil && sub.TrialEnd.After(now):
         → Set status = trialing → Save sub
         → Publish WebhookEventSubscriptionTrialing system event
       Else:
         → Call ActivateIncompleteSubscription(ctx, flexSubID)
           (handles credit grant processing + publishes WebhookEventSubscriptionActivated)
   - SubscriptionStatusTrialing: no-op (card captured, sub already in correct state)
   - Other: no-op (idempotent)
```

The existing `ActivateIncompleteSubscription` handles credit grant processing and webhook publishing for the `active` path. The `trialing` path requires a direct status update and a `WebhookEventSubscriptionTrialing` publish (mirroring how `syncTrialingStateFromCreateRequest` handles it).

---

### 6. Wiring

| File | Change |
|---|---|
| `internal/types/temporal.go` | Add `TemporalPaddleSubscriptionSyncWorkflow` constant; add to `GetSupportedWorkflows()` and task queue routing |
| `internal/temporal/service/service.go` | Add `case TemporalPaddleSubscriptionSyncWorkflow` in `StartWorkflow` and `buildWorkflowInput`; add `buildPaddleSubscriptionSyncInput` |
| `internal/temporal/registration.go` | Instantiate `SubscriptionSyncActivities`; register `PaddleSubscriptionSyncWorkflow` + `SyncSubscriptionToPaddle` + `CheckSubscriptionSyncStatus` activities |
| `internal/integration/paddle/dto.go` | Add `EnsureSubscriptionSyncedResponse.CheckoutURL string`; `SyncSubscriptionToPaddle` request/response types |
| `cmd/server/main.go` | Provide `SubscriptionSyncActivities` via `fx.Provide` |

---

## Error Handling

| Scenario | Behaviour |
|---|---|
| Customer has no email | `EnsureCustomerSyncedToPaddle` → NonRetryableError |
| Customer has no address country | `SyncSubscriptionToPaddle` → NonRetryableError |
| No line items on subscription | `SyncSubscriptionToPaddle` → NonRetryableError |
| `CreateTransaction` transient failure | Retry up to 3× |
| Invoice sync before sub activated | `CheckSubscriptionSyncStatus` → fire-and-forget sub sync + NonRetryableError |
| `subscription.activated` with no `flexprice_subscription_id` | Log warning, return nil (webhook always 200 OK) |
| Duplicate `subscription.activated` | Mapping creation is idempotent; status update is a no-op if already active |

---

## Testing

- Unit test `EnsureSubscriptionSynced`: three branches (mapping exists, txn in metadata, neither).
- Unit test `ProcessSubscriptionActivatedWebhook`: incomplete→active, incomplete+trial→trialing, trialing→no-op, missing custom_data→no-op.
- Unit test `PaddleInvoiceSyncWorkflow`: mock `CheckSubscriptionSyncStatus` returning `not_synced` → verify child workflow started + NonRetryableError; returning `activated` → verify proceeds to `SyncInvoiceToPaddle`.
- Integration test `SyncInvoice`: verify catalog prices created with correct `name`, verify `CreateSubscriptionCharge` called with `priceId` refs.
