# Paddle Subscription-Based Invoice Sync — Design Spec

**Date:** 2026-05-19  
**Branch:** `feat/paddle-sub-sync`  
**Scope:** Refactor `internal/integration/paddle/` only. Zoho and other integrations are unchanged.

---

## Background

The current Paddle invoice sync (`InvoiceSyncService.SyncInvoiceToPaddle`) creates a standalone
`CreateTransaction` for every invoice. This works but has two gaps:

1. It does not link the charge to a Paddle subscription, so Paddle cannot associate recurring charges
   with a subscription lifecycle.
2. It creates inline (non-catalog) products per transaction instead of referencing catalog price IDs,
   making the Paddle dashboard noisy and preventing price-level reporting.

The new design replaces this with a **subscription-charge based flow**: every FlexPrice invoice
becomes a `CreateSubscriptionCharge` call on a zero-dollar Paddle subscription that is lazily
created per FlexPrice subscription.

---

## Goals

- Every invoice sync goes through a Paddle subscription charge, not a bare transaction.
- Each step (customer, product, subscription, invoice) is independently callable and idempotent.
- All steps can be wrapped individually as Temporal activities.
- No new invoice sync creates a duplicate Paddle subscription — idempotency via `entity_integration_mapping`.
- The zero-dollar anchor price used to bootstrap subscriptions is created once per Paddle connection
  and reused forever (stored in `connection.Metadata`).
- All collection modes use `automatic` (removed manual mode).
- All metadata keys are constants (no raw string literals).

---

## Architecture

### Two layers

```
PaddleClient          — pure API, one method per Paddle endpoint, no mapping logic
PaddleSyncService     — all sync orchestration, idempotency, mapping persistence
```

The four existing scattered types (`CustomerService`, `InvoiceSyncService`, plus the two inline
helpers) are replaced by a single `PaddleSyncService`.

---

## PaddleClient — new methods required

Add to `PaddleClient` interface and `Client` struct in `client.go`:

| Method | Paddle endpoint |
|--------|----------------|
| `CreateProduct(ctx, req) (*paddle.Product, error)` | `POST /products` |
| `CreatePrice(ctx, req) (*paddle.Price, error)` | `POST /prices` |
| `CreateSubscriptionCharge(ctx, req) (*paddle.Subscription, error)` | `POST /subscriptions/{id}/charge` |
| `ListTransactions(ctx, req) (*paddle.Collection[*paddle.Transaction], error)` | `GET /transactions` — used to fetch the transaction created by a subscription charge |

---

## PaddleSyncService — method contracts

All methods live in `internal/integration/paddle/sync_service.go`.

### 1. `EnsureCustomerSynced`

```
Request:  EnsureCustomerSyncedRequest  { CustomerID string }
Response: EnsureCustomerSyncedResponse { PaddleCustomerID string; PaddleAddressID string; Created bool }
```

**Logic:**
1. Load FlexPrice customer.
2. Check `entity_integration_mapping` (EntityType=customer, ProviderType=paddle, EntityID=customerID).
3. If mapping exists → call `syncPaddleAddress` (upsert address) → return cached IDs.
4. If no mapping → `CreateCustomer` in Paddle → `CreateAddress` if country present → store mapping.
5. Always returns a valid `PaddleCustomerID`. Returns error (fail fast) if customer has no email.

**Idempotency:** mapping check first; race condition on concurrent create handled by `IsAlreadyExists`
check (returns winner's ID).

---

### 2. `EnsureProductSynced`

```
Request:  EnsureProductSyncedRequest  { PriceID string; Name string; Amount decimal.Decimal; Currency string }
Response: EnsureProductSyncedResponse { PaddlePriceID string; PaddleProductID string; Created bool }
```

**Logic:**
1. Check `entity_integration_mapping` (EntityType=price, ProviderType=paddle, EntityID=priceID).
2. If found → return `ProviderEntityID` as `PaddlePriceID` (no-op).
3. If missing:
   - `CreateProduct` (name=price display name, tax_category=standard).
   - `CreatePrice` (product_id, unit_price=amount, billing_cycle=nil i.e. one-time, quantity=1-100).
   - Store mapping: EntityID=priceID, ProviderEntityID=paddlePriceID, Metadata includes paddleProductID.
4. Return `PaddlePriceID` for use in `SubscriptionChargeItemFromCatalog`.

**Note:** The Paddle price is one-time (no billing_cycle), so it can be used as a subscription charge
item without creating a recurring commitment.

---

### 3. `EnsureProductsSynced`

```
Request:  EnsureProductsSyncedRequest  { Items []EnsureProductSyncedRequest }
Response: EnsureProductsSyncedResponse { PriceIDToPaddlePriceID map[string]string }
```

**Logic:**
1. Single bulk query to `entity_integration_mapping` for all price IDs.
2. For each price ID not yet in the result map → call `EnsureProductSynced`.
3. Return complete `priceID → paddlePriceID` map.

Used internally by `SyncInvoice`. Can also be called standalone.

---

### 4. `EnsureSubscriptionSynced`

```
Request:  EnsureSubscriptionSyncedRequest  { SubscriptionID string; CustomerID string }
Response: EnsureSubscriptionSyncedResponse { PaddleSubscriptionID string; Created bool }
```

**Logic:**
1. Check `entity_integration_mapping` (EntityType=subscription, ProviderType=paddle,
   EntityID=subscriptionID).
2. If found → return `ProviderEntityID` (no-op — this is the key idempotency guard that prevents
   creating a new Paddle subscription on every invoice sync for the same FlexPrice subscription).
3. If missing:
   a. `GetOrCreateZeroDollarPrice(ctx)` — see below.
   b. `EnsureCustomerSynced(customerID)` to get paddleCustomerID + paddleAddressID.
   c. `CreateTransaction` ($0, zeroDollarPriceID, paddleCustomerID, paddleAddressID,
      CollectionMode=automatic). Paddle processes the $0 transaction immediately and creates a
      subscription.
   d. Extract `transaction.SubscriptionID` → store mapping.
4. Return `PaddleSubscriptionID`.

#### `GetOrCreateZeroDollarPrice` (private helper)

```
1. Load Paddle connection.
2. Check connection.Metadata[ConnKeyZeroDollarPriceID].
3. If present → return it (reuse forever).
4. If missing:
   - CreateProduct(name="FlexPrice Subscription Anchor", tax_category=standard).
   - CreatePrice(product_id, unit_price=$0, billing_cycle=monthly, collection_mode=automatic).
   - connectionRepo.Update with ConnKeyZeroDollarProductID + ConnKeyZeroDollarPriceID in Metadata.
   - Return new price ID.
```

This is a one-time bootstrap per Paddle connection/environment. All future calls read from the
cached connection metadata without hitting the Paddle API.

**Edge case:** if the stored price ID is invalid in Paddle (e.g. archived), `CreateTransaction`
will fail. Fix: clear `ConnKeyZeroDollarPriceID` from connection metadata to force re-creation on
the next run.

---

### 5. `SyncInvoice`

```
Request:  SyncInvoiceRequest  { InvoiceID string }
Response: SyncInvoiceResponse { PaddleTransactionID string; CheckoutURL string; AlreadySynced bool }
```

**Logic (orchestrator — fail fast on any step):**
1. Load FlexPrice invoice.
2. Check `entity_integration_mapping` (EntityType=invoice) → if found: return early (`AlreadySynced=true`).
3. Secondary idempotency: check `invoice.Metadata[MetaKeyPaddleTransactionID]` → if present: return early.
4. `EnsureCustomerSynced(invoice.CustomerID)` → get paddleCustomerID, paddleAddressID.
5. `EnsureProductsSynced(invoice.LineItems)` → get priceID→paddlePriceID map.
6. `EnsureSubscriptionSynced(invoice.SubscriptionID, invoice.CustomerID)` → get paddleSubscriptionID.
6b. Guard: if `invoice.SubscriptionID` is nil → return error (fail fast; this flow requires a subscription).
7. Build `CreateSubscriptionChargeRequest`:
   - `SubscriptionID = paddleSubscriptionID`
   - `EffectiveFrom = immediately`
   - `Items` = `SubscriptionChargeItemFromCatalog` per line item (using catalog paddlePriceID + quantity)
8. `client.CreateSubscriptionCharge(ctx, req)` → returns `*paddle.Subscription` (no transaction ID in response).
9. Fetch the created transaction: `client.ListTransactions(subscriptionID, orderBy=created_at[DESC], perPage=1)`.
   This gives `txn_xxx` + `checkout.url`.
10. Write invoice metadata (MetaKeyPaddleTransactionID, MetaKeyPaddleCheckoutURL).
11. Store invoice mapping in `entity_integration_mapping` with `ProviderEntityID = txn_xxx`.
12. Append checkout JWT token to CheckoutURL (existing `appendCheckoutToken` logic).

**Tax preview (`previewAndSyncTax`):** retained as an optional pre-sync step before step 7 if
the invoice has non-zero total. Applied to the subscription charge items the same way as before.

---

## Connection metadata keys (already in `keys.go`)

| Constant | Key string | Purpose |
|----------|-----------|---------|
| `ConnKeyZeroDollarProductID` | `paddle_zero_dollar_product_id` | Paddle product used for $0 subscription anchor |
| `ConnKeyZeroDollarPriceID` | `paddle_zero_dollar_price_id` | Paddle price ($0/mo) used to bootstrap subscriptions |
| `ConnKeyRedirectURL` | `redirect_url` | Success URL appended to checkout JWT |

---

## Entity integration mapping — entity types used

| EntityType | EntityID | ProviderEntityID | Notes |
|-----------|----------|-----------------|-------|
| `customer` | FlexPrice customerID | Paddle ctm_xxx | Metadata includes paddle_address_id |
| `price` | FlexPrice priceID | Paddle pri_xxx | Metadata includes paddle_product_id |
| `subscription` | FlexPrice subscriptionID | Paddle sub_xxx | Created once, reused for all invoice charges |
| `invoice` | FlexPrice invoiceID | Paddle txn_xxx (from subscription charge) | Idempotency guard |

---

## File changes

| File | Action |
|------|--------|
| `keys.go` | Done — all metadata constants defined |
| `client.go` | Add `CreateProduct`, `CreatePrice`, `CreateSubscriptionCharge` to interface + impl |
| `dto.go` | Add request/response types for all 5 sync methods |
| `sync_service.go` | **New** — `PaddleSyncService` with all 5 methods |
| `customer.go` | **Deleted** — logic absorbed into `sync_service.go` |
| `invoice.go` | **Deleted** — logic absorbed into `sync_service.go` |
| `payment.go` | Unchanged |
| `webhook/handler.go` | Update to call `syncSvc.EnsureCustomerSynced` instead of old `CustomerService` |
| `internal/integration/factory.go` | Wire `PaddleSyncService`; remove old `CustomerSvc` + `InvoiceSyncSvc` fields from `PaddleIntegration` |
| `internal/temporal/activities/paddle/invoice_sync_activities.go` | Call `syncSvc.SyncInvoice` |
| `internal/temporal/activities/paddle/customer_sync_activities.go` | Call `syncSvc.EnsureCustomerSynced` |
| `internal/service/invoice.go` | `SyncInvoiceToPaddleIfEnabled` → call `syncSvc.SyncInvoice` |

---

## Idempotency summary

| Step | Guard |
|------|-------|
| Customer | `entity_integration_mapping` (EntityType=customer) |
| Product/price | `entity_integration_mapping` (EntityType=price) |
| Subscription | `entity_integration_mapping` (EntityType=subscription) — **primary guard against duplicate subs** |
| Invoice | `entity_integration_mapping` (EntityType=invoice) + `invoice.Metadata[paddle_transaction_id]` secondary guard |
| Zero-dollar price | `connection.Metadata[paddle_zero_dollar_price_id]` |

---

## Out of scope

- Zoho Books, Stripe, Razorpay, or any other integration — no changes.
- Paddle webhook handling beyond the `paddle_address_id` key update already done.
- Changing when `SyncInvoice` is triggered (existing `SyncInvoiceToExternalVendors` call sites unchanged).
