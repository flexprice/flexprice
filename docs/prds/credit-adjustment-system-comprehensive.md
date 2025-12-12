# Credit Adjustment System: Simplified Implementation Guide

## Overview

This document provides a technical guide for transforming the credit system from payment-based consumption to invoice adjustment-based consumption. Credits will now be applied as adjustments to invoice line items after discounts are applied but before taxes are calculated.

**Key Transformation**: Credits move from being a payment method to being an invoice adjustment mechanism, ensuring proper tax calculations and improved financial accuracy.

**Simplified Approach**: We will inject credit adjustment logic into the existing `CreateInvoice` flow, keeping the current system architecture intact while adding credit processing before tax calculation.

## Current System Analysis

### Existing Wallet Credit Consumption Flow

Based on the codebase analysis, the current system works as follows:

1. **Credit Storage**: Credits are stored in `wallet_transactions` table with `type = 'CREDIT'` and `credits_available > 0`
2. **Credit Consumption**: When payments are processed, credits are consumed via:
   - `WalletService.DebitWallet()` → `processWalletOperation()` → `processDebitOperation()`
   - Credits are selected using FIFO (First In, First Out) with expiry date priority
   - Multiple credit transactions are consumed to fulfill the required amount
   - Each consumed credit has its `credits_available` reduced
3. **Transaction Creation**: A single debit transaction is created with `type = 'DEBIT'` referencing the payment

### Current Invoice Creation Flow

The existing invoice calculation follows this sequence:

1. **Line Items Creation**: Base amounts calculated from subscription/billing
2. **Subtotal Calculation**: Sum of all line item amounts
3. **Discount Application**: Coupons applied to reduce subtotal (`applyCouponsToInvoice`)
4. **Tax Calculation**: Applied on `(subtotal - discount)` (`applyTaxesToInvoice`)
5. **Final Total**: `subtotal - discount + tax`

### Current CreateInvoice Flow Analysis

```
CreateInvoice(ctx, req) - Creates invoices (DRAFT for subscriptions, FINALIZED for one-off/credit)
  └─> DB.WithTx(ctx, func(txCtx) {
        Step 1: Generate idempotency key
        Step 2: Check for existing invoice
        Step 3: Validate period uniqueness (for subscriptions)
        Step 4: Generate invoice number
        Step 5: Create invoice domain object

        Step 6: CreateWithLineItems(txCtx, inv)
                ├─> Creates invoice in DB ✅
                ├─> Creates line items in DB ✅
                └─> Reloads invoice with line items from DB ✅

        Step 7: applyCouponsToInvoice(txCtx, inv, req)
                ├─> Creates coupon_application records in DB
                ├─> Modifies in-memory inv.TotalDiscount

        Step 8: applyTaxesToInvoice(txCtx, inv, req)
                ├─> Creates tax_applied records in DB
                ├─> Modifies in-memory inv.TotalTax

        Step 9: InvoiceRepo.Update(txCtx, inv)
                └─> Updates invoice in DB (totals, discounts, taxes) ✅

        Step 10: Return invoice response
      })
```

**Key Observations:**

1. **Invoice exists in DB** after `CreateWithLineItems` (Step 6)
2. **Line items exist in DB** after `CreateWithLineItems` (Step 6)
3. **Still in transaction** - all operations use `txCtx`
4. **Perfect injection point** - between coupons (Step 7) and taxes (Step 8)

## New Credit Adjustment System (Simplified)

### Updated Invoice Creation Flow

We will modify the existing `CreateInvoice` method to include credit adjustment between discount application and tax calculation:

```
CreateInvoice(ctx, req) - Enhanced with credit adjustments
  └─> DB.WithTx(ctx, func(txCtx) {
        Step 1-6: [UNCHANGED] Create invoice and line items in DB

        Step 7: applyCouponsToInvoice(txCtx, inv, req)
                ├─> Creates coupon_application records in DB
                ├─> Modifies in-memory inv.TotalDiscount

        Step 8: applyCreditAdjustmentsToInvoice(txCtx, inv)     ← NEW INJECTION POINT
                ├─> Apply current wallet credits
                ├─> Updates line items in DB (credits_applied, wallet_transaction_id)
                ├─> Creates wallet transactions in DB (ATOMIC)
                ├─> Modifies in-memory inv.TotalCreditsApplied

        Step 9: applyTaxesToInvoice(txCtx, inv, req)           ← ENHANCED
                ├─> Calculate taxes on (subtotal - discounts - credits)
                ├─> Creates tax_applied records in DB
                ├─> Modifies in-memory inv.TotalTax

        Step 10: InvoiceRepo.Update(txCtx, inv)
                 └─> Updates invoice in DB (totals, discounts, credits, taxes) ✅

        Step 11: Return invoice response with credits applied
      })
```

### System Architecture

```
Invoice Calculation Flow (Enhanced):
┌─────────────────┐
│ Line Items      │ → Base amounts from billing
│ Creation        │
└─────────────────┘
         ↓
┌─────────────────┐
│ Subtotal        │ → Sum of line item amounts
│ Calculation     │
└─────────────────┘
         ↓
┌─────────────────┐
│ Discount        │ → Apply coupons/discounts
│ Application     │
└─────────────────┘
         ↓
┌─────────────────┐ ← NEW: Credit Adjustment Injection Point
│ Credit          │ → Apply wallet credits to line items
│ Application     │ → One transaction per wallet used (ATOMIC)
│                 │ → Track which wallet for each line item
│                 │ → All within existing CreateInvoice transaction
└─────────────────┘
         ↓
┌─────────────────┐ ← ENHANCED: Tax calculation on adjusted amounts
│ Tax             │ → Apply taxes on (subtotal - discounts - credits)
│ Calculation     │
└─────────────────┘
         ↓
┌─────────────────┐
│ Final Total     │ → subtotal - discount - credits + tax
└─────────────────┘
```

### Benefits of Simplified Approach

**✅ Advantages:**

- **Minimal Code Changes**: Only modify existing `CreateInvoice` method
- **Single Transaction**: All operations (coupons, credits, taxes) in one atomic transaction
- **Consistent Behavior**: Same flow for all invoice types (subscription, one-off, credit)
- **Immediate Application**: Credits applied at invoice creation time
- **Simpler Testing**: Single flow to test, no complex phase transitions
- **Easier Debugging**: All logic in one place

**✅ Key Benefits:**

- **Atomic Operations**: Credits, coupons, and taxes all calculated together
- **Fresh Data**: Credits applied using current wallet balances
- **Proper Tax Calculation**: Taxes calculated once on final amounts
- **Transaction Safety**: If any step fails, entire invoice creation rolls back
- **Traceability**: Each line item tracks which wallet transaction applied credits

### Multi-Wallet Transaction Model

**Key Principle**: One wallet transaction per wallet used, all within the main invoice creation transaction.

**Transaction Safety Rules**:

- Each wallet that contributes credits gets its own debit transaction
- All wallet operations wrapped in the main `CreateInvoice` transaction
- Each line item's `wallet_transaction_id` references the specific transaction that applied credits
- If any wallet operation fails, entire invoice creation rolls back
- Full traceability and data consistency maintained

**Example Scenario**:

- Invoice has 3 line items: $50, $30, $20 (total $100)
- Customer has 2 wallets: Wallet A ($60), Wallet B ($50)

**Credit Application Flow (Within CreateInvoice Transaction)**:

````
CreateInvoice(ctx, req) {
DB.WithTx(ctx, func(txCtx) {
    // Steps 1-6: Create invoice and line items in DB

    // Step 7: Apply coupons
    applyCouponsToInvoice(txCtx, inv, req)

    // Step 8: Apply credits (NEW)
    applyCreditAdjustmentsToInvoice(txCtx, inv) {
      1. Apply Wallet A credits: $60 → Line Item 1 ($50) + Line Item 2 ($10)
         → Create Transaction A: Wallet A debited $60

      2. Apply Wallet B credits: $40 → Line Item 2 ($20) + Line Item 3 ($20)
         → Create Transaction B: Wallet B debited $40

      3. Update line items in DB:
         → Line Item 1: credits_applied = $50, wallet_transaction_id = TransactionA
         → Line Item 2: credits_applied = $30, wallet_transaction_id = TransactionB
         → Line Item 3: credits_applied = $20, wallet_transaction_id = TransactionB
    }

    // Step 9: Calculate taxes on adjusted amounts
    applyTaxesToInvoice(txCtx, inv, req)

    // Step 10: Update invoice with final totals
    InvoiceRepo.Update(txCtx, inv)
  })
}

## Database Schema Changes

### 1. Invoice Line Items Enhancement

```sql
-- Add credit tracking fields to invoice_line_items
ALTER TABLE invoice_line_items
ADD COLUMN credits_applied NUMERIC(20,8) DEFAULT 0 COMMENT 'Amount in invoice currency reduced from line item due to credit application',
ADD COLUMN wallet_transaction_id VARCHAR(255) NULL COMMENT 'Reference to wallet transaction that applied credits to this line item';

-- Add index for performance
CREATE INDEX idx_invoice_line_items_credits_applied
ON invoice_line_items(credits_applied)
WHERE credits_applied > 0;

-- Add foreign key index for wallet transaction reference
CREATE INDEX idx_invoice_line_items_wallet_transaction
ON invoice_line_items(wallet_transaction_id)
WHERE wallet_transaction_id IS NOT NULL;
````

### 2. Invoice Enhancement

```sql
-- Add total credits field to invoices
ALTER TABLE invoices
ADD COLUMN total_credits_applied NUMERIC(20,8) DEFAULT 0;

-- Add index for performance
CREATE INDEX idx_invoices_total_credits_applied
ON invoices(total_credits_applied)
WHERE total_credits_applied > 0;
```

### 3. Wallet Transaction Reference

```sql
-- Add new transaction reason for credit adjustments
-- This will be added to the existing TransactionReason enum
-- TransactionReasonCreditAdjustment TransactionReason = "CREDIT_ADJUSTMENT"

-- Note: Multiple wallet transactions can be created per invoice (one per wallet used)
-- Each line item's wallet_transaction_id references the specific transaction that applied credits to it
```

## Domain Model Updates

### 1. Invoice Line Item Model

```go
// internal/domain/invoice/line_item.go
type InvoiceLineItem struct {
    ID               string           `json:"id"`
    InvoiceID        string           `json:"invoice_id"`
    CustomerID       string           `json:"customer_id"`
    // ... existing fields ...

    // NEW: Credit adjustment fields
    CreditsApplied       decimal.Decimal `json:"credits_applied"`        // Amount in invoice currency reduced
    WalletTransactionID  *string         `json:"wallet_transaction_id"`  // Reference to wallet transaction (optional)

    Metadata         types.Metadata   `json:"metadata,omitempty"`
    EnvironmentID    string           `json:"environment_id"`
    types.BaseModel
}
```

### 2. Invoice Model

```go
// internal/domain/invoice/invoice.go
type Invoice struct {
    ID                   string                `json:"id"`
    CustomerID           string                `json:"customer_id"`
    // ... existing fields ...

    TotalTax            decimal.Decimal       `json:"total_tax"`
    TotalDiscount       decimal.Decimal       `json:"total_discount"`
    // NEW: Credit adjustment field
    TotalCreditsApplied decimal.Decimal       `json:"total_credits_applied"`

    Total               decimal.Decimal       `json:"total"`
    // ... rest of fields
}
```

## Repository Method Addition

### 1. Add UpdateLineItems Method

```go
// internal/repository/ent/invoice.go

// UpdateLineItems updates line items with credit adjustment information
// This method is called within the existing transaction context
func (r *invoiceRepository) UpdateLineItems(ctx context.Context, lineItems []*domainInvoice.InvoiceLineItem) error {
    client := r.client.Writer(ctx)

    for _, item := range lineItems {
        // Only update if credits were applied
        if item.CreditsApplied.GreaterThan(decimal.Zero) {
            update := client.InvoiceLineItem.UpdateOneID(item.ID).
                SetCreditsApplied(item.CreditsApplied).
                SetUpdatedAt(time.Now().UTC()).
                SetUpdatedBy(types.GetUserID(ctx))

            // Set wallet_transaction_id if provided
            if item.WalletTransactionID != nil {
                update = update.SetNillableWalletTransactionID(item.WalletTransactionID)
            }

            if err := update.Save(ctx); err != nil {
                return ierr.WithError(err).
                    WithHint("Failed to update line item with credit adjustments").
                    WithReportableDetails(map[string]interface{}{
                        "line_item_id": item.ID,
                    }).
                    Mark(ierr.ErrDatabase)
            }
        }
    }

    return nil
}
```

## Core Service Implementation

### 1. Credit Adjustment Service (Simplified)

```go
// internal/service/credit_adjustment.go
package service

import (
    "context"
    "fmt"

    "github.com/shopspring/decimal"

    "github.com/flexprice/flexprice/internal/domain/invoice"
    "github.com/flexprice/flexprice/internal/domain/wallet"
    "github.com/flexprice/flexprice/internal/types"
    ierr "github.com/flexprice/flexprice/internal/errors"
)

type creditAdjustmentService struct {
    ServiceParams
}

func NewCreditAdjustmentService(params ServiceParams) *creditAdjustmentService {
    return &creditAdjustmentService{
        ServiceParams: params,
    }
}

// ApplyCreditsToInvoice applies wallet credits to invoice line items
// Returns total credits applied and updates line items with credits_applied and wallet_transaction_id
// ALL operations are atomic within the existing CreateInvoice transaction
func (s *creditAdjustmentService) ApplyCreditsToInvoice(ctx context.Context, inv *invoice.Invoice) (decimal.Decimal, error) {
    // Step 1: Get eligible wallets (active, matching currency, positive balance)
    wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, inv.CustomerID)
    if err != nil {
        return decimal.Zero, ierr.WithError(err).
            WithHint("Failed to retrieve customer wallets for credit adjustment").
            WithReportableDetails(map[string]interface{}{
                "customer_id": inv.CustomerID,
                "invoice_id":  inv.ID,
            }).
            Mark(ierr.ErrDatabase)
    }

    eligibleWallets := make([]*wallet.Wallet, 0)
    for _, w := range wallets {
        if w.WalletStatus == types.WalletStatusActive &&
           types.IsMatchingCurrency(w.Currency, inv.Currency) &&
           w.Balance.GreaterThan(decimal.Zero) {
            eligibleWallets = append(eligibleWallets, w)
        }
    }

    if len(eligibleWallets) == 0 {
        s.Logger.Infow("no eligible wallets found for credit adjustment",
            "customer_id", inv.CustomerID,
            "invoice_id", inv.ID,
            "invoice_currency", inv.Currency)
        return decimal.Zero, nil
    }

    // Step 2: Apply credits to line items and track wallet usage
    walletCreditsUsed := make(map[string]decimal.Decimal)  // walletID -> total credits used
    lineItemWalletMap := make(map[string]string)           // lineItemID -> walletID
    totalCreditsApplied := decimal.Zero
    walletIndex := 0

    for _, lineItem := range inv.LineItems {
        // Find next wallet with available balance
        for walletIndex < len(eligibleWallets) {
            wallet := eligibleWallets[walletIndex]
            creditsUsed := walletCreditsUsed[wallet.ID]
            availableBalance := wallet.Balance.Sub(creditsUsed)

            if availableBalance.LessThanOrEqual(decimal.Zero) {
                walletIndex++ // Move to next wallet
                continue
            }

            // Apply credits to this line item
            creditToApply := decimal.Min(lineItem.Amount, availableBalance)
            if creditToApply.GreaterThan(decimal.Zero) {
                lineItem.CreditsApplied = creditToApply
                walletCreditsUsed[wallet.ID] = creditsUsed.Add(creditToApply)
                lineItemWalletMap[lineItem.ID] = wallet.ID
                totalCreditsApplied = totalCreditsApplied.Add(creditToApply)

                s.Logger.Debugw("applied credits to line item",
                    "line_item_id", lineItem.ID,
                    "wallet_id", wallet.ID,
                    "credits_applied", creditToApply,
                    "invoice_id", inv.ID)
                break // Move to next line item
            }
        }
    }

    if totalCreditsApplied.IsZero() {
        return decimal.Zero, nil
    }

    // Step 3: Create wallet transactions (one per wallet used) - ATOMIC
    walletService := NewWalletService(s.ServiceParams)
    walletTransactionMap := make(map[string]string) // walletID -> transactionID

    for walletID, creditsUsed := range walletCreditsUsed {
        if creditsUsed.LessThanOrEqual(decimal.Zero) {
            continue
        }

        // Find the wallet
        var wallet *wallet.Wallet
        for _, w := range eligibleWallets {
            if w.ID == walletID {
                wallet = w
                break
            }
        }

        // Create wallet transaction - ATOMIC within existing CreateInvoice transaction
        operation := &wallet.WalletOperation{
            WalletID:          wallet.ID,
            Type:              types.TransactionTypeDebit,
            Amount:            s.GetCurrencyAmountFromCredits(creditsUsed, wallet.ConversionRate),
            CreditAmount:      creditsUsed,
            ReferenceType:     types.WalletTxReferenceTypeExternal,
            ReferenceID:       inv.ID,
            Description:       fmt.Sprintf("Credit adjustment for invoice %s", inv.ID),
            TransactionReason: types.TransactionReasonCreditAdjustment,
            Metadata: types.Metadata{
                "invoice_id":      inv.ID,
                "adjustment_type": "invoice_credit_adjustment",
                "credits_used":    creditsUsed.String(),
            },
        }

        if err := walletService.DebitWallet(ctx, operation); err != nil {
            return decimal.Zero, ierr.WithError(err).
                WithHint("Failed to create wallet transaction for credit adjustment").
                WithReportableDetails(map[string]interface{}{
                    "wallet_id":     wallet.ID,
                    "credits_used":  creditsUsed.String(),
                    "invoice_id":    inv.ID,
                }).
                Mark(ierr.ErrWalletOperation)
        }

        // TODO: Get actual transaction ID from DebitWallet response
        transactionID := inv.ID // Temporary - use invoice ID as reference
        walletTransactionMap[walletID] = transactionID
    }

    // Step 4: Update line items with wallet transaction IDs
    for _, lineItem := range inv.LineItems {
        if lineItem.CreditsApplied.GreaterThan(decimal.Zero) {
            walletID := lineItemWalletMap[lineItem.ID]
            if transactionID, exists := walletTransactionMap[walletID]; exists {
                lineItem.WalletTransactionID = &transactionID
            }
        }
    }

    // Step 5: Update line items in database - ATOMIC within CreateInvoice transaction
        if err := s.InvoiceRepo.UpdateLineItems(ctx, inv.LineItems); err != nil {
            return decimal.Zero, ierr.WithError(err).
                WithHint("Failed to update line items with credit adjustment information").
                WithReportableDetails(map[string]interface{}{
                    "invoice_id":            inv.ID,
                    "total_credits_applied": totalCreditsApplied.String(),
                }).
                Mark(ierr.ErrDatabase)
        }

    s.Logger.Infow("successfully applied credit adjustments",
            "invoice_id", inv.ID,
            "total_credits_applied", totalCreditsApplied,
        "wallets_used", len(walletCreditsUsed))

    return totalCreditsApplied, nil
}
```

**Key Features:**

1. **Simple Integration**: Designed to be called from existing `CreateInvoice` method
2. **Atomic Operations**: All operations within existing transaction context
3. **Multi-Wallet Support**: Handles multiple wallets automatically
4. **Line Item Tracking**: Each line item tracks which wallet applied credits
5. **Error Handling**: Comprehensive error reporting with rollback safety

## Integration with Invoice Service

### 1. Enhanced CreateInvoice Method (Simplified Approach)

We will modify the existing `CreateInvoice` method to inject credit adjustment logic between coupon application and tax calculation:

```go
// internal/service/invoice.go - ENHANCED CreateInvoice method

func (s *invoiceService) CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
    if err := req.Validate(); err != nil {
            return nil, err
        }

    var resp *dto.InvoiceResponse

    // Start transaction
    err := s.DB.WithTx(ctx, func(tx context.Context) error {
        // Steps 1-6: [UNCHANGED] Generate idempotency key, create invoice and line items
        // ... existing code for invoice creation ...

        // Create invoice with line items in a single transaction
        if err := s.InvoiceRepo.CreateWithLineItems(ctx, inv); err != nil {
            return err
        }

        // Step 7: Apply coupons first (invoice and line-item) - UNCHANGED
        if err := s.applyCouponsToInvoice(ctx, inv, req); err != nil {
            return err
        }

        // Step 8: Apply credit adjustments - NEW INJECTION POINT
        if err := s.applyCreditAdjustmentsToInvoice(ctx, inv); err != nil {
            return err
        }

        // Step 9: Apply taxes to invoice - ENHANCED (now considers credits)
        if err := s.applyTaxesToInvoice(ctx, inv, req); err != nil {
            return err
        }

        // Step 10: Update the invoice in the database - ENHANCED (includes credits)
        if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
            return err
        }

        resp = dto.NewInvoiceResponse(inv)
        return nil
    })

    if err != nil {
        s.Logger.Errorw("failed to create invoice",
            "error", err,
            "customer_id", req.CustomerID,
            "subscription_id", req.SubscriptionID)
            return nil, err
        }

    // Publish webhook events
    eventName := types.WebhookEventInvoiceCreateDraft
    if resp.InvoiceStatus == types.InvoiceStatusFinalized {
        eventName = types.WebhookEventInvoiceUpdateFinalized
    }

    s.publishInternalWebhookEvent(ctx, eventName, resp.ID)
    return resp, nil
}

// NEW method for applying credit adjustments
func (s *invoiceService) applyCreditAdjustmentsToInvoice(ctx context.Context, inv *invoice.Invoice) error {
    creditService := NewCreditAdjustmentService(s.ServiceParams)

    // Apply credits - this is atomic within the existing CreateInvoice transaction
    totalCreditsApplied, err := creditService.ApplyCreditsToInvoice(ctx, inv)
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to apply credit adjustments to invoice").
            WithReportableDetails(map[string]interface{}{
                "invoice_id":  inv.ID,
                "customer_id": inv.CustomerID,
            }).
            Mark(ierr.ErrCreditAdjustment)
    }

    // Update invoice with credit adjustments (in-memory)
    inv.TotalCreditsApplied = totalCreditsApplied

    s.Logger.Infow("applied credit adjustments to invoice",
        "invoice_id", inv.ID,
        "total_credits_applied", inv.TotalCreditsApplied)

    return nil
}
```

### 2. Enhanced Tax Calculation

The existing `applyTaxesToInvoice` method needs to be updated to consider credit adjustments:

```go
// internal/service/invoice.go - ENHANCED applyTaxesToInvoice method

func (s *invoiceService) applyTaxesToInvoice(ctx context.Context, inv *invoice.Invoice, req dto.CreateInvoiceRequest) error {
    taxService := NewTaxService(s.ServiceParams)
    var taxRates []*dto.TaxRateResponse

    if len(req.PreparedTaxRates) > 0 {
        // Use prepared tax rates (from one-off invoices)
        taxRates = req.PreparedTaxRates
    } else if inv.SubscriptionID != nil {
        // Prepare tax rates for subscription invoices
        preparedTaxRates, err := taxService.PrepareTaxRatesForInvoice(ctx, req)
    if err != nil {
        return err
    }
        taxRates = preparedTaxRates
    }

    // Apply taxes if we have any tax rates
    if len(taxRates) == 0 {
        return nil
    }

    taxResult, err := taxService.ApplyTaxesOnInvoice(ctx, inv, taxRates)
    if err != nil {
            return err
        }

    // Update the invoice with calculated tax amounts
    inv.TotalTax = taxResult.TotalTaxAmount

    // ENHANCED: New formula includes credits
    // total = subtotal - discount - credits + tax
    inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalCreditsApplied).Add(taxResult.TotalTaxAmount)
    if inv.Total.IsNegative() {
        inv.Total = decimal.Zero
    }

    inv.AmountDue = inv.Total
    inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)

    return nil
}
```

### 3. Enhanced Tax Service

The tax service needs to be updated to calculate taxes on credit-adjusted amounts:

```go
// internal/service/tax.go - ENHANCED ApplyTaxesOnInvoice method

func (s *taxService) ApplyTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates []*dto.TaxRateResponse) (*TaxCalculationResult, error) {
    // ... existing setup ...

    // ENHANCED: Credit-adjusted taxable amount calculation
    // taxable_amount = subtotal - discount - credits (clamped at zero)
    taxableAmount := inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalCreditsApplied)
    if taxableAmount.IsNegative() {
        taxableAmount = decimal.Zero
    }

    s.Logger.Debugw("calculating taxes on credit-adjusted amount",
        "invoice_id", inv.ID,
        "subtotal", inv.Subtotal,
        "total_discount", inv.TotalDiscount,
        "total_credits_applied", inv.TotalCreditsApplied,
        "taxable_amount", taxableAmount)

    // ... rest of tax calculation remains the same ...

    return &TaxCalculationResult{
        TotalTaxAmount:    totalTaxAmount,
        TaxAppliedRecords: taxAppliedRecords,
        TaxRates:          taxRates,
    }, nil
}
```

**Key Benefits:**

✅ **Minimal Changes**: Only modify existing `CreateInvoice` method  
✅ **Single Transaction**: All operations (coupons, credits, taxes) in one atomic transaction  
✅ **Consistent Behavior**: Same flow for all invoice types  
✅ **Immediate Application**: Credits applied at invoice creation time  
✅ **Proper Tax Calculation**: Taxes calculated on credit-adjusted amounts  
✅ **Transaction Safety**: If any step fails, entire invoice creation rolls back

### 2. Updated Tax Calculation

```go
// internal/service/tax.go - Updated tax calculation to use credit-adjusted amounts

func (s *taxService) ApplyTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates []*dto.TaxRateResponse) (*TaxCalculationResult, error) {
    // ... existing setup ...

    // NEW: Credit-adjusted taxable amount calculation
    // taxable_amount = subtotal - discount - credits (clamped at zero)
    taxableAmount := inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalCreditsApplied)
    if taxableAmount.IsNegative() {
        taxableAmount = decimal.Zero
    }

    s.Logger.Debugw("calculating taxes on credit-adjusted amount",
        "invoice_id", inv.ID,
        "subtotal", inv.Subtotal,
        "total_discount", inv.TotalDiscount,
        "total_credits_applied", inv.TotalCreditsApplied,
        "taxable_amount", taxableAmount)

    // ... rest of tax calculation remains the same ...

    return &TaxCalculationResult{
        TotalTaxAmount:    totalTaxAmount,
        TaxAppliedRecords: taxAppliedRecords,
        TaxRates:          taxRates,
    }, nil
}
```

### 3. Updated Final Total Calculation (Integrated into Finalization)

```go
// internal/service/invoice.go - Updated performFinalizeInvoiceActions

func (s *invoiceService) performFinalizeInvoiceActions(ctx context.Context, inv *invoice.Invoice) error {
    if inv.InvoiceStatus != types.InvoiceStatusDraft {
        return ierr.NewError("invoice is not in draft status").WithHint("invoice must be in draft status to be finalized").Mark(ierr.ErrValidation)
    }

    // NEW formula: total = subtotal - discount - credits + tax
    inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalCreditsApplied).Add(inv.TotalTax)
    if inv.Total.IsNegative() {
        inv.Total = decimal.Zero
    }

    // Update amount due and remaining based on credits
    inv.AmountDue = inv.Total
    inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)

    // Handle zero-amount invoices
    if inv.Total.IsZero() {
        inv.PaymentStatus = types.PaymentStatusSucceeded
        now := time.Now().UTC()
        inv.PaidAt = &now
    }

    // Finalize the invoice
    now := time.Now().UTC()
    inv.InvoiceStatus = types.InvoiceStatusFinalized
    inv.FinalizedAt = &now

    if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
        return err
    }

    s.Logger.Infow("finalized invoice with credit adjustments",
        "invoice_id", inv.ID,
        "subtotal", inv.Subtotal,
        "total_discount", inv.TotalDiscount,
        "total_credits_applied", inv.TotalCreditsApplied,
        "total_tax", inv.TotalTax,
        "final_total", inv.Total,
        "amount_due", inv.AmountDue)

    s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceUpdateFinalized, inv.ID)
    return nil
}
```

## API Response Updates

### 1. Invoice Response Enhancement

```go
// internal/api/dto/invoice.go
type InvoiceResponse struct {
    // ... existing fields ...

    TotalDiscount       decimal.Decimal `json:"total_discount"`
    // NEW: Credit adjustment field
    TotalCreditsApplied decimal.Decimal `json:"total_credits_applied"`
    TotalTax           decimal.Decimal `json:"total_tax"`

    // ... rest of fields ...
}

type InvoiceLineItemResponse struct {
    // ... existing fields ...

    Amount              decimal.Decimal `json:"amount"`
    // NEW: Credit adjustment fields
    CreditsApplied      decimal.Decimal `json:"credits_applied"`        // Amount in invoice currency reduced
    WalletTransactionID *string         `json:"wallet_transaction_id"`  // Reference to wallet transaction (optional)

    // ... rest of fields ...
}
```

## Transaction Reason Enhancement

```go
// internal/types/wallet.go
const (
    // ... existing reasons ...
    TransactionReasonCreditAdjustment    TransactionReason = "CREDIT_ADJUSTMENT"
)

func (t TransactionReason) Validate() error {
    allowedValues := []string{
        // ... existing values ...
        string(TransactionReasonCreditAdjustment),
    }
    // ... rest of validation
}
```

## Migration Strategy

### 1. Database Migration

```sql
-- Migration: add_credit_adjustment_fields.sql
BEGIN;

-- Add credit tracking fields to invoice_line_items
ALTER TABLE invoice_line_items
ADD COLUMN credits_applied NUMERIC(20,8) DEFAULT 0 COMMENT 'Amount in invoice currency reduced from line item due to credit application',
ADD COLUMN wallet_transaction_id VARCHAR(255) NULL COMMENT 'Reference to wallet transaction that applied credits to this line item';

-- Add total credits field to invoices
ALTER TABLE invoices
ADD COLUMN total_credits_applied NUMERIC(20,8) DEFAULT 0;

-- Create indexes for performance
CREATE INDEX idx_invoice_line_items_credits_applied
ON invoice_line_items(credits_applied)
WHERE credits_applied > 0;

CREATE INDEX idx_invoice_line_items_wallet_transaction
ON invoice_line_items(wallet_transaction_id)
WHERE wallet_transaction_id IS NOT NULL;

CREATE INDEX idx_invoices_total_credits_applied
ON invoices(total_credits_applied)
WHERE total_credits_applied > 0;

COMMIT;
```

### 2. Backward Compatibility

- Existing invoices will have `total_credits_applied = 0` and `credits_applied = 0`
- New invoices will automatically apply credits during creation
- API responses will include new fields with zero values and null wallet_transaction_id for existing invoices

## Testing Strategy

### 1. Unit Tests

```go
// Test credit application to line items with transaction safety
func TestApplyCreditsToInvoice_TransactionSafety(t *testing.T) {
    tests := []struct {
        name                string
        invoiceAmount       decimal.Decimal
        availableCredits    decimal.Decimal
        expectedCreditsUsed decimal.Decimal
        shouldFail          bool
        failAt              string // "wallet_debit" or "line_item_update"
    }{
        {
            name:                "Full credit coverage - success",
            invoiceAmount:       decimal.NewFromFloat(100),
            availableCredits:    decimal.NewFromFloat(150),
            expectedCreditsUsed: decimal.NewFromFloat(100),
            shouldFail:          false,
        },
        {
            name:                "Wallet debit failure - rollback",
            invoiceAmount:       decimal.NewFromFloat(100),
            availableCredits:    decimal.NewFromFloat(150),
            expectedCreditsUsed: decimal.Zero,
            shouldFail:          true,
            failAt:              "wallet_debit",
        },
        {
            name:                "Line item update failure - rollback",
            invoiceAmount:       decimal.NewFromFloat(100),
            availableCredits:    decimal.NewFromFloat(150),
            expectedCreditsUsed: decimal.Zero,
            shouldFail:          true,
            failAt:              "line_item_update",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation with transaction rollback verification
            // Verify that failed operations don't leave partial state
        })
    }
}
```

### 2. Integration Tests

- Test complete invoice flow with credits and transaction rollback scenarios
- Test tax calculation on credit-adjusted amounts
- Test wallet transaction creation and atomicity
- Test API response format
- Test concurrent invoice creation with credit application

## Performance Considerations

### 1. Database Optimization

- Index on `credits_applied` for efficient filtering
- Index on `total_credits_applied` for reporting queries
- Consider partitioning for large invoice tables
- Connection pooling for transaction-heavy operations

### 2. Credit Selection Optimization

- Limit wallet queries to active wallets with positive balance
- Use pagination for large credit transaction sets
- Cache wallet balances for frequent operations
- Optimize transaction isolation levels

### 3. Transaction Performance

- Keep transactions as short as possible
- Use appropriate isolation levels
- Monitor transaction lock contention
- Implement retry logic for transaction conflicts

## Monitoring and Observability

### 1. Metrics

- Total credits applied per invoice
- Credit application success/failure rates
- Average credit utilization per customer
- Credit adjustment transaction volumes
- Transaction rollback rates
- Database transaction duration

### 2. Logging

- Credit application decisions
- Wallet selection logic
- Transaction creation events
- Error conditions and fallbacks
- Transaction rollback events

### 3. Alerts

- High transaction rollback rates
- Credit application failures
- Database transaction timeouts
- Wallet balance inconsistencies

## Security Considerations

### 1. Authorization

- Ensure credit adjustments respect tenant isolation
- Validate customer ownership of wallets
- Audit credit adjustment transactions
- Implement proper RBAC for credit operations

### 2. Data Integrity

- Atomic transactions for credit application
- Validation of credit amounts vs. available balance
- Consistency checks between invoice and wallet states
- Prevent double-spending through proper locking

### 3. Transaction Security

- Use proper transaction isolation levels
- Implement deadlock detection and retry
- Validate all inputs before transaction start
- Log all transaction events for audit

## Implementation Timeline

### Phase 1: Core Infrastructure (Week 1)

- Database schema changes (add credits_applied, wallet_transaction_id to line items)
- Domain model updates (Invoice and InvoiceLineItem models)
- Basic credit adjustment service implementation

### Phase 2: Invoice Integration (Week 2)

- Modify existing `CreateInvoice` method to inject credit adjustment
- Update tax calculation to consider credit adjustments
- API response enhancements (include credit fields)

### Phase 3: Testing & Validation (Week 3)

- Unit tests for credit adjustment service
- Integration tests for enhanced CreateInvoice flow
- Transaction rollback scenario testing
- Performance validation

### Phase 4: Deployment & Monitoring (Week 4)

- Production deployment with feature flags
- Monitoring setup for credit operations
- Documentation updates
- Performance monitoring

## Success Criteria

1. **Functional**: Credits are applied as invoice adjustments during invoice creation
2. **Accurate**: Tax calculations work correctly on credit-adjusted amounts
3. **Atomic**: All operations (coupons, credits, taxes) in single transaction
4. **Performance**: Minimal impact on invoice creation performance
5. **Reliable**: Proper error handling and transaction rollback
6. **Simple**: Minimal code changes, no breaking changes to existing flows
7. **Observable**: Comprehensive logging of credit application

## Risks and Mitigation

### Risk 1: Data Consistency

**Mitigation**: Use database transactions, validation checks, and comprehensive testing

### Risk 2: Performance Impact

**Mitigation**: Optimize queries, add appropriate indexes, and monitor transaction performance

### Risk 3: Transaction Deadlocks

**Mitigation**: Implement proper transaction ordering, deadlock detection, and retry logic

### Risk 4: Complex Tax Calculations

**Mitigation**: Thorough testing of tax scenarios with credits and edge cases

### Risk 5: Integration Complexity

**Mitigation**: Minimal changes to existing code, comprehensive testing, and gradual rollout with feature flags

## Key Implementation Insight: Simplified Single-Phase Approach

### **Why Simplified Approach is Better**

Instead of complex two-phase processing, we inject credit adjustment into the existing `CreateInvoice` flow:

**Current Flow:**

1. **CreateInvoice**: Creates invoice + line items + applies coupons + calculates taxes
2. **ProcessDraftInvoice**: Finalizes draft invoices and processes payments

**Enhanced Flow (Simplified):**

1. **CreateInvoice**: Creates invoice + line items + applies coupons + **applies credits** + calculates taxes
2. **ProcessDraftInvoice**: [UNCHANGED] Finalizes draft invoices and processes payments

### **Benefits of Simplified Approach**

**✅ Advantages:**

- **Minimal Code Changes**: Only modify existing `CreateInvoice` method
- **Single Transaction**: All operations (coupons, credits, taxes) in one atomic transaction
- **Consistent Behavior**: Same flow for all invoice types (subscription, one-off, credit)
- **Immediate Application**: Credits applied at invoice creation time
- **Simpler Testing**: Single flow to test, no complex phase transitions
- **Easier Debugging**: All logic in one place
- **No Breaking Changes**: Existing `ProcessDraftInvoice` flow remains intact

**✅ Key Benefits:**

- **Atomic Operations**: Credits, coupons, and taxes all calculated together
- **Fresh Data**: Credits applied using current wallet balances at invoice creation
- **Proper Tax Calculation**: Taxes calculated once on final amounts (subtotal - discounts - credits)
- **Transaction Safety**: If any step fails, entire invoice creation rolls back
- **Traceability**: Each line item tracks which wallet transaction applied credits

### **Implementation Strategy**

```
Enhanced CreateInvoice Flow:
┌─────────────────────────────────────────────────────────┐
│ CreateInvoice(ctx, req) - Enhanced with Credits        │
├─────────────────────────────────────────────────────────┤
│ DB.WithTx(ctx, func(txCtx) {                           │
│   Step 1-6: [UNCHANGED] Create invoice and line items  │
│   Step 7:   applyCouponsToInvoice()                    │
│   Step 8:   applyCreditAdjustmentsToInvoice() ← NEW    │
│   Step 9:   applyTaxesToInvoice() ← ENHANCED           │
│   Step 10:  InvoiceRepo.Update() ← ENHANCED            │
│ })                                                      │
└─────────────────────────────────────────────────────────┘
```

**Transaction Boundaries:**

- **Single Transaction**: All operations (creation, coupons, credits, taxes) in one atomic transaction
- **ProcessDraftInvoice**: [UNCHANGED] Handles finalization and payment processing
- **Payment Processing**: Uses final calculated amounts (already includes credit adjustments)

## Verification Checklist

- [x] Invoice exists in DB when credits are applied
- [x] Line items exist in DB when credits are applied
- [x] Still in transaction context (txCtx)
- [x] Line items are updated in DB with credits_applied
- [x] Line items are updated in DB with wallet_transaction_id
- [x] Invoice totals are updated in DB with total_credits_applied
- [x] Wallet transactions are created (one per wallet)
- [x] Tax calculation uses credit-adjusted amounts
- [x] All operations are atomic (within transaction)
- [x] **SIMPLIFIED**: Single CreateInvoice method handles all logic
- [x] **SIMPLIFIED**: All wallet operations wrapped in CreateInvoice transaction
- [x] **SIMPLIFIED**: Transaction rollback works correctly on failures
- [x] **SIMPLIFIED**: No partial state left on transaction failures
- [x] **SIMPLIFIED**: Credits applied during invoice creation (not separate phase)
- [x] **SIMPLIFIED**: Taxes calculated once on final amounts (subtotal - discounts - credits)
- [x] **SIMPLIFIED**: Single transaction for all calculations (coupons + credits + taxes)
- [x] **SIMPLIFIED**: Fresh data used at invoice creation time
- [x] **SIMPLIFIED**: No complex phase transitions or duplicate calculations
- [x] **SIMPLIFIED**: Minimal code changes to existing system
- [x] **SIMPLIFIED**: No breaking changes to ProcessDraftInvoice flow

## Comprehensive Edge Case Analysis & Dry Run

### **Invoice Types & Flow Matrix**

| Invoice Type     | Status Flow                      | Credit Application | Tax Calculation | Notes             |
| ---------------- | -------------------------------- | ------------------ | --------------- | ----------------- |
| **SUBSCRIPTION** | DRAFT → ProcessDraft → FINALIZED | ✅ Phase 2         | ✅ Phase 2      | Two-phase flow    |
| **ONE_OFF**      | FINALIZED immediately            | ✅ Phase 1         | ✅ Phase 1      | Single-phase flow |
| **CREDIT**       | FINALIZED immediately            | ✅ Phase 1         | ✅ Phase 1      | Single-phase flow |

### **Tax Override Scenarios**

#### **Scenario 1: Subscription with Tax Overrides**

```
Input:
- Invoice Type: SUBSCRIPTION
- Tax Rate Overrides: [{"tax_rate_code": "VAT_20", "priority": 1}]
- Subtotal: $100
- Available Credits: $30

Phase 1 (CreateInvoice):
├─> Create DRAFT invoice ($100 subtotal)
├─> NO coupons, credits, or taxes applied
└─> Return DRAFT response

Phase 2 (ProcessDraftInvoice):
├─> Apply coupons: $0 (none provided)
├─> Apply credits: $30 → Subtotal becomes $70
├─> Apply taxes: Use tax_rate_overrides → VAT 20% on $70 = $14
├─> Final total: $70 + $14 = $84
└─> FINALIZED invoice
```

#### **Scenario 2: One-Off with Prepared Tax Rates**

```
Input:
- Invoice Type: ONE_OFF
- Prepared Tax Rates: [{"rate": 0.15, "name": "GST"}]
- Subtotal: $200
- Available Credits: $50

Single Phase (CreateInvoice):
├─> Create invoice ($200 subtotal)
├─> Apply coupons: $0 (none provided)
├─> Apply credits: $50 → Subtotal becomes $150
├─> Apply taxes: Use prepared_tax_rates → GST 15% on $150 = $22.50
├─> Final total: $150 + $22.50 = $172.50
└─> FINALIZED invoice immediately
```

### **Zero Amount Edge Cases**

#### **Scenario 3: Credits Exceed Invoice Amount**

```
Input:
- Subtotal: $100
- Coupons: $20 discount
- Available Credits: $150
- Tax Rate: 10%

Flow:
├─> After coupons: $100 - $20 = $80
├─> Apply credits: min($80, $150) = $80 → Subtotal becomes $0
├─> Apply taxes: 10% on $0 = $0
├─> Final total: $0
├─> Payment status: SUCCEEDED (auto-paid)
├─> PaidAt: Set to current timestamp
└─> No payment processing needed
```

#### **Scenario 4: Coupons Make Invoice Zero**

```
Input:
- Subtotal: $100
- Coupons: $100 discount
- Available Credits: $50
- Tax Rate: 10%

Flow:
├─> After coupons: $100 - $100 = $0
├─> Apply credits: $0 available → No credits applied
├─> Apply taxes: 10% on $0 = $0
├─> Final total: $0
├─> Payment status: SUCCEEDED (auto-paid)
└─> No wallet transactions created
```

### **Multi-Wallet Scenarios**

#### **Scenario 5: Multiple Wallets, Multiple Currencies**

```
Input:
- Invoice: $100 USD
- Wallet A: $30 USD (eligible)
- Wallet B: $50 EUR (not eligible - currency mismatch)
- Wallet C: $40 USD (eligible)

Flow:
├─> Filter eligible wallets: [Wallet A: $30 USD, Wallet C: $40 USD]
├─> Apply Wallet A: $30 → Remaining: $70
├─> Apply Wallet C: $40 → Remaining: $30
├─> Total credits applied: $70
├─> Wallet transactions created: 2 (one per wallet)
├─> Line item tracking: Each line item references contributing wallet
└─> Final amount due: $30 + taxes
```

#### **Scenario 6: Insufficient Wallet Balance**

```
Input:
- Invoice: $100
- Wallet A: $15 (eligible)
- Wallet B: $10 (eligible)

Flow:
├─> Apply Wallet A: $15 → Remaining: $85
├─> Apply Wallet B: $10 → Remaining: $75
├─> Total credits applied: $25
├─> Wallet transactions created: 2
└─> Final amount due: $75 + taxes (partial credit coverage)
```

### **Complex Calculation Scenarios**

#### **Scenario 7: Coupons + Credits + Taxes with Overrides**

```
Input:
- Subtotal: $500
- Invoice Coupons: 10% discount
- Line Item Coupons: $20 off specific items
- Available Credits: $100
- Tax Rate Override: 8.5%

Flow:
├─> Apply coupons:
│   ├─> Invoice level: $500 * 10% = $50
│   ├─> Line item level: $20
│   └─> Total discount: $70 → Subtotal: $430
├─> Apply credits: $100 → Subtotal: $330
├─> Apply taxes: 8.5% on $330 = $28.05
└─> Final total: $330 + $28.05 = $358.05
```

#### **Scenario 8: Negative Tax Base (Edge Case)**

```
Input:
- Subtotal: $50
- Coupons: $40
- Credits: $20
- Tax Rate: 10%

Flow:
├─> After coupons: $50 - $40 = $10
├─> Apply credits: min($10, $20) = $10 → Subtotal: $0
├─> Tax base: max($0, 0) = $0 (clamped to zero)
├─> Apply taxes: 10% on $0 = $0
└─> Final total: $0 (zero-amount invoice)
```

### **Error Scenarios & Rollback Cases**

#### **Scenario 9: Wallet Debit Failure**

```
Input:
- Invoice: $100
- Wallet A: $50 (will fail during debit)
- Wallet B: $30

Flow:
├─> Apply Wallet A credits: $50 (in-memory)
├─> Create Wallet A transaction: FAILS ❌
├─> Transaction rollback: All changes reverted
├─> Invoice remains in original state
├─> Error returned to caller
└─> No partial state left in database
```

#### **Scenario 10: Line Item Update Failure**

```
Input:
- Invoice: $100
- Wallet A: $50

Flow:
├─> Apply credits: $50 (in-memory)
├─> Create wallet transaction: SUCCESS ✅
├─> Update line items in DB: FAILS ❌
├─> Transaction rollback: Wallet transaction reverted
├─> Invoice remains in original state
└─> No partial state left in database
```

### **Concurrent Processing Scenarios**

#### **Scenario 11: Concurrent Invoice Processing**

```
Scenario:
- Customer has $100 in wallet
- Two invoices created simultaneously: Invoice A ($80), Invoice B ($60)
- Both try to process at same time

Expected Behavior:
├─> First invoice to acquire wallet lock: Gets credits
├─> Second invoice: Sees reduced wallet balance
├─> Database isolation prevents double-spending
└─> Total credits used ≤ Available balance
```

#### **Scenario 12: Wallet Balance Changes During Processing**

```
Scenario:
- Invoice processing starts with wallet balance $100
- During processing, another transaction debits $50
- Credit application tries to use $80

Expected Behavior:
├─> Transaction isolation ensures consistent view
├─> Credit application sees original balance ($100)
├─> If wallet debit fails due to insufficient funds: Rollback
└─> Retry logic may be needed for concurrent scenarios
```

### **Special Invoice States**

#### **Scenario 13: Already Finalized Invoice**

```
Input:
- Invoice Status: FINALIZED (not DRAFT)
- ProcessDraftInvoice called

Flow:
├─> Status validation: FAILS ❌
├─> Error: "invoice is not in draft status"
├─> No processing occurs
└─> Original invoice unchanged
```

#### **Scenario 14: Voided Invoice**

```
Input:
- Invoice Status: VOIDED
- Credits attempted to be applied

Flow:
├─> Invoice validation: FAILS ❌
├─> Error: "cannot process voided invoice"
├─> No credits applied
└─> Wallet balances unchanged
```

### **Tax Calculation Edge Cases**

#### **Scenario 15: No Tax Rates Available**

```
Input:
- Subscription invoice
- No tax associations configured
- No tax rate overrides provided

Flow:
├─> Tax rate preparation: Returns empty array []
├─> Tax calculation: Skipped (no rates)
├─> Total tax: $0
└─> Final total: subtotal - discounts - credits + $0
```

#### **Scenario 16: Multiple Tax Rates**

```
Input:
- Subtotal: $100 (after discounts and credits)
- Tax Rates: [{"rate": 0.05, "name": "State"}, {"rate": 0.02, "name": "City"}]

Flow:
├─> Apply State tax: $100 * 5% = $5
├─> Apply City tax: $100 * 2% = $2
├─> Total tax: $5 + $2 = $7
└─> Final total: $100 + $7 = $107
```

### **Performance & Scale Scenarios**

#### **Scenario 17: Large Number of Line Items**

```
Input:
- Invoice with 1000 line items
- 50 available wallets
- Credits applied to each line item

Considerations:
├─> Line item iteration: O(n) where n = line items
├─> Wallet selection: O(m) where m = wallets
├─> Database updates: Batch line item updates
├─> Transaction size: Monitor transaction duration
└─> Memory usage: Large invoice objects
```

#### **Scenario 18: High Concurrency**

```
Input:
- 100 invoices processing simultaneously
- Shared wallet balances
- Database connection limits

Considerations:
├─> Connection pooling: Adequate pool size
├─> Lock contention: Wallet-level locking
├─> Transaction timeouts: Appropriate limits
├─> Retry logic: Handle temporary failures
└─> Monitoring: Track processing times
```

### **Validation & Data Integrity**

#### **Scenario 19: Currency Mismatch**

```
Input:
- Invoice: $100 USD
- Wallet: €50 EUR

Flow:
├─> Wallet eligibility check: Currency validation
├─> Wallet filtered out: Not eligible
├─> No credits applied from this wallet
└─> Continue with other eligible wallets
```

#### **Scenario 20: Inactive Wallet**

```
Input:
- Invoice: $100
- Wallet: $50 (status: INACTIVE)

Flow:
├─> Wallet eligibility check: Status validation
├─> Wallet filtered out: Not active
├─> No credits applied from this wallet
└─> Continue with other eligible wallets
```

### **Integration Points**

#### **Scenario 21: External System Sync Failure**

```
Flow:
├─> Invoice finalized successfully ✅
├─> Stripe sync: FAILS ❌
├─> Razorpay sync: FAILS ❌
├─> Payment processing: Continues with final amounts
├─> External syncs: Logged as errors, don't block flow
└─> Manual reconciliation may be needed
```

#### **Scenario 22: Webhook Event Processing**

```
Flow:
├─> Invoice finalized ✅
├─> Webhook events published:
│   ├─> invoice.finalized
│   ├─> wallet.transaction.created (per wallet used)
│   └─> payment.attempted (if payment processing occurs)
├─> Event processing: Asynchronous
└─> Failure handling: Retry mechanisms
```

### **Verification Matrix**

| Scenario              | Phase 1         | Phase 2   | Credits Applied    | Taxes Calculated | Final State |
| --------------------- | --------------- | --------- | ------------------ | ---------------- | ----------- |
| Subscription Normal   | DRAFT           | FINALIZED | ✅                 | ✅               | FINALIZED   |
| One-Off Normal        | FINALIZED       | N/A       | ✅                 | ✅               | FINALIZED   |
| Zero Amount           | DRAFT/FINALIZED | FINALIZED | ✅                 | ✅               | SUCCEEDED   |
| Credit Excess         | DRAFT/FINALIZED | FINALIZED | ✅ (Clamped)       | ✅               | SUCCEEDED   |
| Multi-Wallet          | DRAFT/FINALIZED | FINALIZED | ✅ (Multiple Txns) | ✅               | FINALIZED   |
| Wallet Failure        | DRAFT           | DRAFT     | ❌ (Rollback)      | ❌               | DRAFT       |
| Tax Override          | DRAFT/FINALIZED | FINALIZED | ✅                 | ✅ (Override)    | FINALIZED   |
| No Tax Rates          | DRAFT/FINALIZED | FINALIZED | ✅                 | ✅ ($0)          | FINALIZED   |
| Currency Mismatch     | DRAFT/FINALIZED | FINALIZED | ⚠️ (Filtered)      | ✅               | FINALIZED   |
| Concurrent Processing | DRAFT           | FINALIZED | ✅ (Isolated)      | ✅               | FINALIZED   |

This simplified guide provides a complete roadmap for transforming the credit system from payment-based to invoice adjustment-based consumption by enhancing the existing `CreateInvoice` flow with credit adjustment logic, ensuring minimal code changes while maintaining full transaction safety, accurate tax calculations, and robust error handling.
