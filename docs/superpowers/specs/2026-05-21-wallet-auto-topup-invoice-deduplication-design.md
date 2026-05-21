# Wallet Auto-Topup Invoice Deduplication

**Date:** 2026-05-21  
**Status:** Approved  
**Files in scope:** `internal/types/invoice.go`, `internal/api/dto/wallet.go`, `internal/repository/ent/invoice.go`, `internal/service/wallet.go`

---

## Problem

When a wallet's real-time balance drops below the auto-topup threshold and `AutoTopup.Invoicing = true`, `triggerAutoTopup` currently creates a new invoice on **every** incoming event. Since a PURCHASED_CREDIT_INVOICED transaction leaves the wallet balance unchanged (pending payment), every subsequent event also finds the balance below threshold and creates another invoice — flooding the customer.

---

## Solution Overview

Before creating an auto-topup invoice, check if a pending auto-topup invoice already exists for this customer. If one exists, skip. Introduce a new `InvoiceBillingReason` value (`WALLET_AUTO_TOPUP`) to identify these invoices reliably. Once the customer pays, re-trigger the threshold check so a new invoice can be raised if the balance is still below threshold after payment.

---

## Changes

### 1. New `InvoiceBillingReason` — `internal/types/invoice.go`

Add a new constant:

```go
// InvoiceBillingReasonWalletAutoTopup is generated when a wallet balance drops
// below the auto-topup threshold and invoiced top-up is enabled.
InvoiceBillingReasonWalletAutoTopup InvoiceBillingReason = "WALLET_AUTO_TOPUP"
```

Add it to the `Validate()` allowed list.

### 2. `BillingReason` filter on `InvoiceFilter` — `internal/types/invoice.go`

Add field to `InvoiceFilter`:

```go
// billing_reason filters invoices by why they were generated
BillingReason InvoiceBillingReason `json:"billing_reason,omitempty" form:"billing_reason"`
```

### 3. Wire filter in invoice repository — `internal/repository/ent/invoice.go`

In the predicate builder for `List`, apply the filter when set:

```go
if filter.BillingReason != "" {
    predicates = append(predicates, invoice.BillingReasonEQ(string(filter.BillingReason)))
}
```

### 4. Guard check + billing reason in `triggerAutoTopup` — `internal/service/wallet.go`

**Guard check helper:**

```go
func (s *walletService) hasPendingAutoTopupInvoice(ctx context.Context, customerID string) (bool, error) {
    filter := types.NewNoLimitInvoiceFilter()
    filter.CustomerID    = customerID
    filter.BillingReason = types.InvoiceBillingReasonWalletAutoTopup
    filter.PaymentStatus = []types.PaymentStatus{types.PaymentStatusPending}
    filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}
    filter.SkipLineItems = true

    invoices, err := s.InvoiceRepo.List(ctx, filter)
    if err != nil {
        return false, err
    }
    return len(invoices) > 0, nil
}
```

**Updated `triggerAutoTopup`:** before calling `TopUpWallet`, add the guard for invoiced mode:

```go
if w.AutoTopup.Invoicing != nil && *w.AutoTopup.Invoicing {
    hasPending, err := s.hasPendingAutoTopupInvoice(ctx, w.CustomerID)
    if err != nil {
        return err
    }
    if hasPending {
        s.Logger.InfowCtx(ctx, "pending auto-topup invoice exists, skipping",
            "wallet_id", w.ID,
            "customer_id", w.CustomerID,
        )
        return nil
    }
}
```

**Add `BillingReason` to `TopUpWalletRequest` — `internal/api/dto/wallet.go`:**

```go
// billing_reason indicates why this top-up was triggered (e.g. WALLET_AUTO_TOPUP)
BillingReason types.InvoiceBillingReason `json:"billing_reason,omitempty"`
```

**Set it in `triggerAutoTopup`:**

```go
BillingReason: types.InvoiceBillingReasonWalletAutoTopup,
```

**Thread it through in `handlePurchasedCreditInvoicedTransaction`:** when building `invReq`, set:

```go
BillingReason: req.BillingReason,
```

This is the only place `billing_reason` is set on the invoice — no other callers of `TopUpWallet` set it, so existing behaviour is unchanged.

### 5. Re-trigger after payment — `internal/service/wallet.go`

In `completePurchasedCreditTransaction`, after the wallet is credited and the webhook published, publish a balance alert event to trigger re-evaluation:

```go
s.PublishWalletBalanceAlertEvent(ctx, tx.WalletID, tx.CustomerID, true)
```

This is async (Kafka), non-fatal, and reuses the existing pipeline:  
`CheckWalletBalanceAlert` → `triggerAutoTopup` → guard check (no pending invoice now) → new invoice if balance still below threshold.

---

## End-to-End Flow

```
balance < threshold (event arrives)
  ↓
triggerAutoTopup
  ↓
Invoicing=true?
  YES → hasPendingAutoTopupInvoice?
          YES → skip (return nil)
          NO  → TopUpWallet(PURCHASED_CREDIT_INVOICED, BillingReason=WALLET_AUTO_TOPUP)
                  → PENDING wallet tx + FINALIZED invoice (payment_status=PENDING)
                  → balance unchanged
  NO  → TopUpWallet(PURCHASED_CREDIT_DIRECT) → balance credited immediately

--- subsequent events while invoice is pending ---
triggerAutoTopup → hasPendingAutoTopupInvoice → YES → skip ✓

--- customer pays invoice ---
completePurchasedCreditTransaction
  → wallet credited
  → PublishWalletBalanceAlertEvent (async, ForceCalculateBalance=true)
  → Kafka consumer → CheckWalletBalanceAlert → triggerAutoTopup
  → hasPendingAutoTopupInvoice → NO (invoice now SUCCEEDED)
  → balance still < threshold? YES → new invoice ✓
  → balance >= threshold?      YES → skip ✓
```

---

## Edge Cases

| Scenario | Outcome |
|---|---|
| Burst events while invoice pending | Guard finds PENDING invoice → all skip |
| Payment restores balance above threshold | Re-check after payment → balance ≥ threshold → no new invoice |
| Payment restores balance but still below threshold | Re-check after payment → no pending invoice → new invoice created |
| `auto_complete = true` (non-default tenant setting) | Invoice immediately SUCCEEDED, balance credited instantly → guard not needed, balance acts as natural guard (same as current behaviour) |
| `Invoicing = false` (DIRECT mode) | Balance credited immediately → existing behaviour unchanged, guard not applied |

---

## What Does NOT Change

- `AutoTopup` config struct — no new fields
- Wallet model — no schema migration
- Invoice model — `billing_reason` column already exists
- `handlePurchasedCreditInvoicedTransaction` logic — only passes `BillingReason` through; all other behaviour unchanged
- `auto_complete = true` path — no changes needed; existing behaviour is already correct

---

## Scope

**Not in scope:**
- Notifying the customer when an auto-topup invoice is raised (webhook already fires via existing `publishInternalTransactionWebhookEvent`)
- Configuring auto-topup amount to cover the gap to threshold (existing `Amount` field on `AutoTopup`)
- Any UI changes
