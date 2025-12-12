# Credit System Transformation: Technical Implementation Guide

## Overview

This document provides a comprehensive technical guide for transforming the credit system from payment instruments to invoice adjustments. The transformation involves two major phases: **Removal** of existing credit payment infrastructure and **Implementation** of the new credit adjustment system.

## Phase 1: Credit Payment System Removal

### 1.1 Payment Method Type System Changes

**File: `internal/types/payment.go`**

```go
// REMOVE: Line 55
// PaymentMethodTypeCredits     PaymentMethodType = "CREDITS"

// REMOVE: Line 68 from validation array
func (s PaymentMethodType) Validate() error {
    allowed := []PaymentMethodType{
        PaymentMethodTypeCard,
        PaymentMethodTypeACH,
        PaymentMethodTypeOffline,
        // PaymentMethodTypeCredits,  // REMOVE THIS LINE
        PaymentMethodTypePaymentLink,
    }
    // ... rest of validation
}
```

**Impact**: Removes credits as a valid payment method system-wide.

### 1.2 Payment Processor Core Removal

**File: `internal/service/payment_processor.go`**

```go
// REMOVE: Lines 102-103 from switch statement
switch paymentObj.PaymentMethodType {
case types.PaymentMethodTypeOffline:
    processErr = nil
// case types.PaymentMethodTypeCredits:          // REMOVE
//     processErr = p.handleCreditsPayment(ctx, paymentObj)  // REMOVE
case types.PaymentMethodTypePaymentLink:
    // ... existing logic
}

// REMOVE: Entire function (Lines 451-525)
// func (p *paymentProcessor) handleCreditsPayment(ctx context.Context, paymentObj *payment.Payment) error {
//     // ENTIRE FUNCTION TO BE REMOVED
// }

// UPDATE: Line 124 - Error message
processErr = ierr.NewError(fmt.Sprintf("unsupported payment method type: %s", paymentObj.PaymentMethodType)).
    WithHint("Supported payment methods: CARD, ACH, OFFLINE, PAYMENT_LINK"). // Remove CREDITS
    WithReportableDetails(map[string]interface{}{
        "payment_id": paymentObj.ID,
    }).
    Mark(ierr.ErrInvalidOperation)
```

### 1.3 Subscription Payment Processor Cleanup

**File: `internal/service/subscription_payment_processor.go`**

```go
// REMOVE: Line 18 from interface
type SubscriptionPaymentProcessor interface {
    HandlePaymentBehavior(ctx context.Context, subscription *subscription.Subscription, invoice *dto.InvoiceResponse, behavior types.PaymentBehavior, flowType types.InvoiceFlowType) error
    // ProcessCreditsPaymentForInvoice(ctx context.Context, inv *dto.InvoiceResponse, sub *subscription.Subscription) decimal.Decimal  // REMOVE
}

// REMOVE: Line 502 from processPayment function
func (s *subscriptionPaymentProcessor) processPayment(...) *PaymentResult {
    // ... existing logic
    
    // Step 4: Process wallet payment (only if card payment succeeded or not needed)
    if walletAmount.GreaterThan(decimal.Zero) {
        // creditsUsed := s.processCreditsPayment(ctx, sub, inv)  // REMOVE THIS BLOCK
        // if creditsUsed.GreaterThan(decimal.Zero) {
        //     result.AmountPaid = result.AmountPaid.Add(creditsUsed)
        //     result.RemainingAmount = result.RemainingAmount.Sub(creditsUsed)
        //     result.PaymentMethods = append(result.PaymentMethods, PaymentMethodUsed{
        //         Type:   "credits",
        //         Amount: creditsUsed,
        //     })
        // }
    }
    
    // ... rest of logic
}

// REMOVE: Entire functions (Lines 538-605)
// func (s *subscriptionPaymentProcessor) processCreditsPayment(...) decimal.Decimal { ... }
// func (s *subscriptionPaymentProcessor) ProcessCreditsPaymentForInvoice(...) decimal.Decimal { ... }
// func (s *subscriptionPaymentProcessor) checkAvailableCredits(...) decimal.Decimal { ... }
```

### 1.4 Wallet Payment Service Elimination

**File: `internal/service/wallet_payment.go`**

```bash
# DELETE ENTIRE FILE
rm internal/service/wallet_payment.go
rm internal/service/wallet_payment_test.go
```

**Functions Removed**:
- `ProcessInvoicePaymentWithWallets`
- `GetWalletsForPayment`
- `processWalletPayments`
- `createWalletPayment`
- `calculatePriceTypeAmounts`
- `calculateAllowedPaymentAmount`
- `updatePriceTypeAmountsAfterPayment`
- `deductFromPriceTypes`

### 1.5 Payment Service Wallet Logic Removal

**File: `internal/service/payment.go`**

```go
// REMOVE: Lines 67-69 wallet selection logic
func (s *paymentService) CreatePayment(ctx context.Context, req *dto.CreatePaymentRequest) (*dto.PaymentResponse, error) {
    // ... existing validation
    
    // REMOVE THIS BLOCK:
    // // select the wallet for the payment in case of credits payment where wallet id is not provided
    // if p.PaymentMethodType == types.PaymentMethodTypeCredits && p.PaymentMethodID == "" {
    //     selectedWallet, err := s.selectWalletForPayment(ctx, invoice, req)
    //     if err != nil {
    //         return nil, err
    //     }
    //     p.PaymentMethodID = selectedWallet.ID
    // }
    
    // ... rest of function
}

// REMOVE: Entire function (Lines 165-215)
// func (s *paymentService) selectWalletForPayment(ctx context.Context, invoice *invoice.Invoice, p *dto.CreatePaymentRequest) (*wallet.Wallet, error) {
//     // ENTIRE FUNCTION TO BE REMOVED
// }
```

### 1.6 Invoice Service Credit Payment Removal

**File: `internal/service/invoice.go`**

```go
// REMOVE: Lines 1688-1737 from attemptPaymentForSubscriptionInvoice
func (s *invoiceService) attemptPaymentForSubscriptionInvoice(ctx context.Context, inv *invoice.Invoice, paymentParams *dto.PaymentParameters, sub *subscription.Subscription, flowType types.InvoiceFlowType) error {
    // ... existing logic
    
    // REMOVE THIS ENTIRE BLOCK:
    // } else if inv.AmountDue.GreaterThan(decimal.Zero) {
    //     // For non-subscription invoices, validate and use credits payment logic
    //     if inv.InvoiceStatus != types.InvoiceStatusFinalized {
    //         return ierr.NewError("invoice must be finalized")...
    //     }
    //     
    //     // Use credits payment logic
    //     paymentProcessor := NewSubscriptionPaymentProcessor(&s.ServiceParams)
    //     invoiceResponse := &dto.InvoiceResponse{...}
    //     amountPaid := paymentProcessor.ProcessCreditsPaymentForInvoice(ctx, invoiceResponse, nil)
    //     // ... rest of credit payment logic
    // }
    
    return nil
}
```

### 1.7 Wallet Configuration Simplification

**File: `internal/types/wallet.go`**

```go
// REMOVE: Lines 283-291 - Price type constants
// type WalletConfigPriceType string
// const (
//     WalletConfigPriceTypeAll   WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_ALL)
//     WalletConfigPriceTypeUsage WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_USAGE)
//     WalletConfigPriceTypeFixed WalletConfigPriceType = WalletConfigPriceType(PRICE_TYPE_FIXED)
// )

// UPDATE: WalletConfig struct (Line 297)
type WalletConfig struct {
    // AllowedPriceTypes []WalletConfigPriceType `json:"allowed_price_types,omitempty"`  // REMOVE
}

// UPDATE: GetDefaultWalletConfig function
func GetDefaultWalletConfig() *WalletConfig {
    return &WalletConfig{
        // AllowedPriceTypes: []WalletConfigPriceType{WalletConfigPriceTypeAll},  // REMOVE
    }
}

// REMOVE: Lines 314-315 - Price type validation
// func (c WalletConfig) Validate() error {
//     // Remove price type validation logic
// }
```

### 1.8 Wallet Service Price Type Logic Removal

**File: `internal/service/wallet.go`**

```go
// REMOVE/UPDATE: Lines 782-787
func (s *walletService) someFunction() {
    // REMOVE:
    // // Determine if we should include usage based on wallet's allowed price types
    // // If wallet has no allowed price types (nil or empty), treat as ALL (include usage)
    // includeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
    //     lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
    //     lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)
    
    // REPLACE WITH:
    includeUsage := true // Always include usage for prepaid wallets
}

// REMOVE/UPDATE: Lines 1801-1804 - Similar price type logic
```

### 1.9 API DTO Updates

**File: `internal/api/dto/payment.go`**

```go
// UPDATE: Line 215 - Remove credits from validation hint
return nil, ierr.NewError("payment method id should not be provided for offline payment methods").
    WithHint("Do not provide payment method ID for offline payment methods"). // Remove "or credits"
    WithReportableDetails(map[string]interface{}{
        "payment_method_type": r.PaymentMethodType,
    }).
    Mark(ierr.ErrValidation)

// UPDATE: Line 236 - Remove credits from payment method type check
} else if r.PaymentMethodType != types.PaymentMethodTypePaymentLink && r.PaymentMethodType != types.PaymentMethodTypeCard {
    // Remove types.PaymentMethodTypeCredits from this condition
```

### 1.10 Credit Grant Service Updates

**File: `internal/service/creditgrant.go`**

```go
// UPDATE: Line 414 - Remove AllowedPriceTypes from wallet creation
walletConfig := &types.WalletConfig{
    // AllowedPriceTypes: []types.WalletConfigPriceType{  // REMOVE ENTIRE FIELD
    //     types.WalletConfigPriceTypeAll,
    // },
}
```

## Phase 2: Credit Adjustment System Implementation

### 2.1 Database Schema Changes

**Migration: `migrations/postgres/add_credit_adjustment_fields.sql`**

```sql
-- Add credit tracking fields to invoice_line_items
ALTER TABLE invoice_line_items 
ADD COLUMN credits_applied BOOLEAN DEFAULT FALSE,
ADD COLUMN credit_amount_applied NUMERIC(20,8) DEFAULT 0;

-- Add total credits field to invoices
ALTER TABLE invoices 
ADD COLUMN total_credits_applied NUMERIC(20,8) DEFAULT 0;

-- Create index for performance
CREATE INDEX idx_invoice_line_items_credits_applied ON invoice_line_items(credits_applied) WHERE credits_applied = true;
CREATE INDEX idx_invoices_total_credits_applied ON invoices(total_credits_applied) WHERE total_credits_applied > 0;
```

### 2.2 Domain Model Updates

**File: `internal/domain/invoice/line_item.go`**

```go
// ADD: New fields to InvoiceLineItem struct
type InvoiceLineItem struct {
    ID               string           `json:"id"`
    InvoiceID        string           `json:"invoice_id"`
    CustomerID       string           `json:"customer_id"`
    // ... existing fields ...
    
    // NEW: Credit adjustment fields
    CreditsApplied      bool            `json:"credits_applied"`
    CreditAmountApplied decimal.Decimal `json:"credit_amount_applied"`
    
    Metadata         types.Metadata   `json:"metadata,omitempty"`
    EnvironmentID    string           `json:"environment_id"`
    types.BaseModel
}
```

**File: `internal/domain/invoice/invoice.go`**

```go
// ADD: New field to Invoice struct
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

### 2.3 Ent Schema Updates

**File: `ent/schema/invoice_line_item.go`**

```go
// ADD: New fields to InvoiceLineItem schema
func (InvoiceLineItem) Fields() []ent.Field {
    return []ent.Field{
        // ... existing fields ...
        
        field.JSON("metadata", map[string]string{}).
            Optional().
            SchemaType(map[string]string{
                "postgres": "jsonb",
            }),
        
        // NEW: Credit adjustment fields
        field.Bool("credits_applied").
            Default(false).
            Comment("Indicates if credits were applied to this line item"),
        
        field.Other("credit_amount_applied", decimal.Decimal{}).
            SchemaType(map[string]string{
                "postgres": "numeric(20,8)",
            }).
            Default(decimal.Zero).
            Comment("Amount of credits applied to this line item"),
    }
}
```

**File: `ent/schema/invoice.go`**

```go
// ADD: New field to Invoice schema
func (Invoice) Fields() []ent.Field {
    return []ent.Field{
        // ... existing fields ...
        
        field.Other("total_discount", decimal.Decimal{}).
            SchemaType(map[string]string{
                "postgres": "numeric(20,8)",
            }).
            Default(decimal.Zero),
        
        // NEW: Credit adjustment field
        field.Other("total_credits_applied", decimal.Decimal{}).
            SchemaType(map[string]string{
                "postgres": "numeric(20,8)",
            }).
            Default(decimal.Zero).
            Comment("Total amount of credits applied to this invoice"),
        
        field.Other("total_tax", decimal.Decimal{}).
            SchemaType(map[string]string{
                "postgres": "numeric(20,8)",
            }).
            Default(decimal.Zero),
        
        // ... rest of fields
    }
}
```

### 2.4 Credit Adjustment Service Implementation

**File: `internal/service/credit_adjustment.go`**

```go
package service

import (
    "context"
    "fmt"

    "github.com/flexprice/flexprice/internal/domain/invoice"
    "github.com/flexprice/flexprice/internal/domain/wallet"
    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
)

type CreditAdjustmentService interface {
    // ApplyCreditsToInvoice applies credits to eligible line items during invoice finalization
    ApplyCreditsToInvoice(ctx context.Context, invoice *invoice.Invoice) error
    
    // GetEligibleLineItems returns line items eligible for credit adjustment
    GetEligibleLineItems(ctx context.Context, invoice *invoice.Invoice) ([]*invoice.InvoiceLineItem, error)
    
    // CalculateCreditApplication calculates how much credit to apply to each line item
    CalculateCreditApplication(ctx context.Context, customerID string, lineItems []*invoice.InvoiceLineItem, currency string) (*CreditApplicationResult, error)
}

type creditAdjustmentService struct {
    ServiceParams
}

type CreditApplicationResult struct {
    TotalCreditsApplied decimal.Decimal
    LineItemAdjustments map[string]decimal.Decimal // lineItemID -> creditAmount
    WalletOperations    []*wallet.WalletOperation
}

func NewCreditAdjustmentService(params ServiceParams) CreditAdjustmentService {
    return &creditAdjustmentService{
        ServiceParams: params,
    }
}

func (s *creditAdjustmentService) ApplyCreditsToInvoice(ctx context.Context, inv *invoice.Invoice) error {
    s.Logger.Infow("applying credits to invoice",
        "invoice_id", inv.ID,
        "customer_id", inv.CustomerID,
        "currency", inv.Currency)

    // Get eligible line items (arrear usage only)
    eligibleLineItems, err := s.GetEligibleLineItems(ctx, inv)
    if err != nil {
        return err
    }

    if len(eligibleLineItems) == 0 {
        s.Logger.Infow("no eligible line items for credit adjustment",
            "invoice_id", inv.ID)
        return nil
    }

    // Calculate credit application
    creditResult, err := s.CalculateCreditApplication(ctx, inv.CustomerID, eligibleLineItems, inv.Currency)
    if err != nil {
        return err
    }

    if creditResult.TotalCreditsApplied.IsZero() {
        s.Logger.Infow("no credits available for application",
            "invoice_id", inv.ID,
            "customer_id", inv.CustomerID)
        return nil
    }

    // Apply credits in a transaction
    return s.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Execute wallet operations (debit credits)
        walletService := NewWalletService(s.ServiceParams)
        for _, operation := range creditResult.WalletOperations {
            if err := walletService.DebitWallet(txCtx, operation); err != nil {
                return err
            }
        }

        // Update line items with credit applications
        for _, lineItem := range eligibleLineItems {
            if creditAmount, exists := creditResult.LineItemAdjustments[lineItem.ID]; exists && creditAmount.GreaterThan(decimal.Zero) {
                lineItem.CreditsApplied = true
                lineItem.CreditAmountApplied = creditAmount
                
                // Reduce line item amount by credit applied
                lineItem.Amount = lineItem.Amount.Sub(creditAmount)
                
                if err := s.InvoiceRepo.UpdateLineItem(txCtx, lineItem); err != nil {
                    return err
                }
            }
        }

        // Update invoice totals
        inv.TotalCreditsApplied = creditResult.TotalCreditsApplied
        
        // Recalculate invoice totals after credit adjustments
        s.recalculateInvoiceTotals(inv)
        
        if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
            return err
        }

        s.Logger.Infow("successfully applied credits to invoice",
            "invoice_id", inv.ID,
            "total_credits_applied", creditResult.TotalCreditsApplied,
            "line_items_affected", len(creditResult.LineItemAdjustments))

        return nil
    })
}

func (s *creditAdjustmentService) GetEligibleLineItems(ctx context.Context, inv *invoice.Invoice) ([]*invoice.InvoiceLineItem, error) {
    var eligible []*invoice.InvoiceLineItem

    for _, lineItem := range inv.LineItems {
        if s.isArrearUsageLineItem(ctx, lineItem, inv) {
            eligible = append(eligible, lineItem)
        }
    }

    s.Logger.Infow("identified eligible line items for credit adjustment",
        "invoice_id", inv.ID,
        "total_line_items", len(inv.LineItems),
        "eligible_line_items", len(eligible))

    return eligible, nil
}

func (s *creditAdjustmentService) isArrearUsageLineItem(ctx context.Context, lineItem *invoice.InvoiceLineItem, inv *invoice.Invoice) bool {
    // Check if line item is usage-based
    if lineItem.PriceType == nil || *lineItem.PriceType != string(types.PRICE_TYPE_USAGE) {
        return false
    }

    // Check if line item is arrear by looking up subscription line item cadence
    if inv.SubscriptionID != nil {
        cadence, err := s.getLineItemCadence(ctx, lineItem, *inv.SubscriptionID)
        if err != nil {
            s.Logger.Warnw("failed to get line item cadence, assuming not eligible",
                "error", err,
                "line_item_id", lineItem.ID)
            return false
        }
        return cadence == types.InvoiceCadenceArrear
    }

    // For non-subscription invoices, assume usage items are arrear
    return true
}

func (s *creditAdjustmentService) getLineItemCadence(ctx context.Context, lineItem *invoice.InvoiceLineItem, subscriptionID string) (types.InvoiceCadence, error) {
    // Get subscription line item to determine cadence
    if lineItem.PriceID == nil {
        return "", ierr.NewError("line item has no price ID").Mark(ierr.ErrValidation)
    }

    subLineItems, err := s.SubRepo.GetLineItemsBySubscriptionAndPrice(ctx, subscriptionID, *lineItem.PriceID)
    if err != nil {
        return "", err
    }

    if len(subLineItems) == 0 {
        return "", ierr.NewError("no subscription line item found").Mark(ierr.ErrNotFound)
    }

    return subLineItems[0].InvoiceCadence, nil
}

func (s *creditAdjustmentService) CalculateCreditApplication(ctx context.Context, customerID string, lineItems []*invoice.InvoiceLineItem, currency string) (*CreditApplicationResult, error) {
    // Get customer's prepaid wallets
    wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
    if err != nil {
        return nil, err
    }

    // Filter for active prepaid wallets with matching currency
    var eligibleWallets []*wallet.Wallet
    for _, w := range wallets {
        if w.WalletStatus == types.WalletStatusActive &&
            w.WalletType == types.WalletTypePrePaid &&
            types.IsMatchingCurrency(w.Currency, currency) &&
            w.Balance.GreaterThan(decimal.Zero) {
            eligibleWallets = append(eligibleWallets, w)
        }
    }

    if len(eligibleWallets) == 0 {
        return &CreditApplicationResult{
            TotalCreditsApplied: decimal.Zero,
            LineItemAdjustments: make(map[string]decimal.Decimal),
            WalletOperations:    []*wallet.WalletOperation{},
        }, nil
    }

    // Sort wallets by priority (promotional first, then by expiry date)
    s.sortWalletsByPriority(eligibleWallets)

    result := &CreditApplicationResult{
        TotalCreditsApplied: decimal.Zero,
        LineItemAdjustments: make(map[string]decimal.Decimal),
        WalletOperations:    []*wallet.WalletOperation{},
    }

    // Apply credits to line items using available wallet balances
    for _, lineItem := range lineItems {
        remainingLineItemAmount := lineItem.Amount
        
        for _, wallet := range eligibleWallets {
            if remainingLineItemAmount.IsZero() || wallet.Balance.IsZero() {
                break
            }

            // Calculate credit to apply (min of remaining amount and wallet balance)
            creditToApply := decimal.Min(remainingLineItemAmount, wallet.Balance)
            
            // Update tracking
            result.LineItemAdjustments[lineItem.ID] = result.LineItemAdjustments[lineItem.ID].Add(creditToApply)
            result.TotalCreditsApplied = result.TotalCreditsApplied.Add(creditToApply)
            remainingLineItemAmount = remainingLineItemAmount.Sub(creditToApply)
            
            // Reduce wallet balance for next iterations
            wallet.Balance = wallet.Balance.Sub(creditToApply)
            
            // Create wallet operation for this debit
            operation := &wallet.WalletOperation{
                WalletID:          wallet.ID,
                Type:              types.TransactionTypeDebit,
                Amount:            creditToApply,
                CreditAmount:      s.getCreditAmountFromCurrency(creditToApply, wallet.ConversionRate),
                ReferenceType:     types.WalletTxReferenceTypeInvoice,
                ReferenceID:       lineItem.InvoiceID,
                Description:       fmt.Sprintf("Credit adjustment for invoice line item %s", lineItem.ID),
                TransactionReason: types.TransactionReasonCreditAdjustment,
                Metadata: types.Metadata{
                    "invoice_id":      lineItem.InvoiceID,
                    "line_item_id":    lineItem.ID,
                    "adjustment_type": "arrear_usage_credit",
                },
            }
            
            result.WalletOperations = append(result.WalletOperations, operation)
        }
    }

    s.Logger.Infow("calculated credit application",
        "customer_id", customerID,
        "total_credits_applied", result.TotalCreditsApplied,
        "line_items_affected", len(result.LineItemAdjustments),
        "wallet_operations", len(result.WalletOperations))

    return result, nil
}

func (s *creditAdjustmentService) sortWalletsByPriority(wallets []*wallet.Wallet) {
    // Sort by wallet type (promotional first) then by expiry date
    // Implementation depends on wallet priority logic
    // For now, simple sort by balance (highest first)
    for i := 0; i < len(wallets)-1; i++ {
        for j := i + 1; j < len(wallets); j++ {
            if wallets[i].Balance.LessThan(wallets[j].Balance) {
                wallets[i], wallets[j] = wallets[j], wallets[i]
            }
        }
    }
}

func (s *creditAdjustmentService) getCreditAmountFromCurrency(currencyAmount decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal {
    if conversionRate.IsZero() {
        return currencyAmount
    }
    return currencyAmount.Div(conversionRate)
}

func (s *creditAdjustmentService) recalculateInvoiceTotals(inv *invoice.Invoice) {
    // Recalculate subtotal from line items
    subtotal := decimal.Zero
    for _, lineItem := range inv.LineItems {
        subtotal = subtotal.Add(lineItem.Amount)
    }
    
    inv.Subtotal = subtotal
    
    // Recalculate total: subtotal - discount + tax
    inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Add(inv.TotalTax)
    if inv.Total.IsNegative() {
        inv.Total = decimal.Zero
    }
    
    // Update amount due and remaining
    inv.AmountDue = inv.Total
    inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)
}
```

### 2.5 Invoice Service Integration

**File: `internal/service/invoice.go`**

```go
// UPDATE: performFinalizeInvoiceActions function
func (s *invoiceService) performFinalizeInvoiceActions(ctx context.Context, inv *invoice.Invoice) error {
    if inv.InvoiceStatus != types.InvoiceStatusDraft {
        return ierr.NewError("invoice is not in draft status").WithHint("invoice must be in draft status to be finalized").Mark(ierr.ErrValidation)
    }

    if inv.Total.IsZero() {
        inv.PaymentStatus = types.PaymentStatusSucceeded
    }

    now := time.Now().UTC()
    inv.InvoiceStatus = types.InvoiceStatusFinalized
    inv.FinalizedAt = &now

    // Apply coupons first (existing logic)
    // Note: This assumes coupons are applied during invoice creation
    // If not, add coupon application logic here

    // NEW: Apply credit adjustments after discounts, before taxes
    creditAdjustmentService := NewCreditAdjustmentService(s.ServiceParams)
    if err := creditAdjustmentService.ApplyCreditsToInvoice(ctx, inv); err != nil {
        s.Logger.Errorw("failed to apply credit adjustments",
            "error", err,
            "invoice_id", inv.ID)
        return err
    }

    // Apply taxes after credit adjustments (existing logic)
    // Note: Tax calculation should use the updated line item amounts

    if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
        return err
    }

    s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceUpdateFinalized, inv.ID)
    return nil
}
```

### 2.6 API DTO Updates

**File: `internal/api/dto/invoice.go`**

```go
// UPDATE: InvoiceLineItemResponse struct
type InvoiceLineItemResponse struct {
    ID               string           `json:"id"`
    InvoiceID        string           `json:"invoice_id"`
    CustomerID       string           `json:"customer_id"`
    // ... existing fields ...
    
    Amount           decimal.Decimal  `json:"amount"`
    Quantity         decimal.Decimal  `json:"quantity"`
    Currency         string           `json:"currency"`
    
    // NEW: Credit adjustment fields
    CreditsApplied      bool            `json:"credits_applied"`
    CreditAmountApplied decimal.Decimal `json:"credit_amount_applied"`
    
    PeriodStart      *time.Time       `json:"period_start,omitempty"`
    PeriodEnd        *time.Time       `json:"period_end,omitempty"`
    Metadata         types.Metadata   `json:"metadata,omitempty"`
    // ... rest of fields
}

// UPDATE: InvoiceResponse struct
type InvoiceResponse struct {
    ID                   string                     `json:"id"`
    CustomerID           string                     `json:"customer_id"`
    // ... existing fields ...
    
    Subtotal            decimal.Decimal            `json:"subtotal"`
    TotalDiscount       decimal.Decimal            `json:"total_discount"`
    // NEW: Credit adjustment field
    TotalCreditsApplied decimal.Decimal            `json:"total_credits_applied"`
    TotalTax            decimal.Decimal            `json:"total_tax"`
    Total               decimal.Decimal            `json:"total"`
    
    // ... rest of fields
}

// UPDATE: NewInvoiceLineItemResponse function
func NewInvoiceLineItemResponse(item *invoice.InvoiceLineItem) *InvoiceLineItemResponse {
    return &InvoiceLineItemResponse{
        ID:               item.ID,
        InvoiceID:        item.InvoiceID,
        CustomerID:       item.CustomerID,
        // ... existing field mappings ...
        
        Amount:           item.Amount,
        Quantity:         item.Quantity,
        Currency:         item.Currency,
        
        // NEW: Credit adjustment field mappings
        CreditsApplied:      item.CreditsApplied,
        CreditAmountApplied: item.CreditAmountApplied,
        
        PeriodStart:      item.PeriodStart,
        PeriodEnd:        item.PeriodEnd,
        Metadata:         item.Metadata,
        // ... rest of field mappings
    }
}

// UPDATE: NewInvoiceResponse function
func NewInvoiceResponse(inv *invoice.Invoice) *InvoiceResponse {
    response := &InvoiceResponse{
        ID:                   inv.ID,
        CustomerID:           inv.CustomerID,
        // ... existing field mappings ...
        
        Subtotal:            inv.Subtotal,
        TotalDiscount:       inv.TotalDiscount,
        // NEW: Credit adjustment field mapping
        TotalCreditsApplied: inv.TotalCreditsApplied,
        TotalTax:            inv.TotalTax,
        Total:               inv.Total,
        
        // ... rest of field mappings
    }
    
    // Map line items
    if inv.LineItems != nil {
        response.LineItems = make([]*InvoiceLineItemResponse, len(inv.LineItems))
        for i, item := range inv.LineItems {
            response.LineItems[i] = NewInvoiceLineItemResponse(item)
        }
    }
    
    return response
}
```

### 2.7 Wallet Types Update

**File: `internal/types/wallet.go`**

```go
// ADD: New transaction reason for credit adjustments
const (
    // ... existing transaction reasons ...
    TransactionReasonCreditAdjustment TransactionReason = "CREDIT_ADJUSTMENT"
)

// UPDATE: TransactionReason validation
func (r TransactionReason) Validate() error {
    allowed := []TransactionReason{
        TransactionReasonTopUp,
        TransactionReasonInvoicePayment,
        TransactionReasonRefund,
        TransactionReasonExpiry,
        TransactionReasonAdjustment,
        TransactionReasonCreditGrant,
        // NEW: Credit adjustment reason
        TransactionReasonCreditAdjustment,
    }
    // ... rest of validation
}
```

### 2.8 Feature Flag Implementation

**File: `internal/config/config.go`**

```go
// ADD: Feature flag for credit adjustments
type Config struct {
    // ... existing config fields ...
    
    Features struct {
        // ... existing feature flags ...
        EnableCreditAdjustments bool `mapstructure:"enable_credit_adjustments" default:"false"`
    } `mapstructure:"features"`
}
```

**File: `internal/service/invoice.go`**

```go
// UPDATE: Add feature flag check in performFinalizeInvoiceActions
func (s *invoiceService) performFinalizeInvoiceActions(ctx context.Context, inv *invoice.Invoice) error {
    // ... existing validation ...

    // NEW: Apply credit adjustments if feature is enabled
    if s.Config.Features.EnableCreditAdjustments {
        creditAdjustmentService := NewCreditAdjustmentService(s.ServiceParams)
        if err := creditAdjustmentService.ApplyCreditsToInvoice(ctx, inv); err != nil {
            s.Logger.Errorw("failed to apply credit adjustments",
                "error", err,
                "invoice_id", inv.ID)
            return err
        }
    }

    // ... rest of finalization logic
}
```

## Phase 3: Testing Strategy

### 3.1 Unit Tests

**File: `internal/service/credit_adjustment_test.go`**

```go
package service

import (
    "context"
    "testing"
    
    "github.com/flexprice/flexprice/internal/domain/invoice"
    "github.com/flexprice/flexprice/internal/domain/wallet"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/suite"
)

type CreditAdjustmentServiceSuite struct {
    suite.Suite
    service CreditAdjustmentService
    // ... test setup
}

func (s *CreditAdjustmentServiceSuite) TestApplyCreditsToInvoice_ArrearUsageOnly() {
    // Test that only arrear usage line items receive credit adjustments
    inv := &invoice.Invoice{
        ID:         "inv_test",
        CustomerID: "cust_test",
        Currency:   "USD",
        LineItems: []*invoice.InvoiceLineItem{
            {
                ID:        "li_usage_arrear",
                Amount:    decimal.NewFromFloat(100.00),
                PriceType: &[]string{string(types.PRICE_TYPE_USAGE)}[0],
                // Assume this is arrear cadence
            },
            {
                ID:        "li_fixed_advance",
                Amount:    decimal.NewFromFloat(50.00),
                PriceType: &[]string{string(types.PRICE_TYPE_FIXED)}[0],
                // Assume this is advance cadence
            },
        },
    }
    
    // Setup wallet with sufficient balance
    // ... test implementation
    
    err := s.service.ApplyCreditsToInvoice(context.Background(), inv)
    s.NoError(err)
    
    // Verify only usage line item was adjusted
    s.True(inv.LineItems[0].CreditsApplied)
    s.False(inv.LineItems[1].CreditsApplied)
}

func (s *CreditAdjustmentServiceSuite) TestCalculateCreditApplication_InsufficientBalance() {
    // Test behavior when wallet balance is insufficient
    // ... test implementation
}

func (s *CreditAdjustmentServiceSuite) TestGetEligibleLineItems_FilteringLogic() {
    // Test line item filtering logic
    // ... test implementation
}
```

### 3.2 Integration Tests

**File: `internal/service/invoice_integration_test.go`**

```go
func (s *InvoiceServiceSuite) TestInvoiceFinalization_WithCreditAdjustments() {
    // Test complete invoice finalization flow with credit adjustments
    // 1. Create invoice with arrear usage line items
    // 2. Create customer wallet with credits
    // 3. Finalize invoice
    // 4. Verify credit adjustments were applied
    // 5. Verify wallet balance was debited
    // 6. Verify invoice totals are correct
}

func (s *InvoiceServiceSuite) TestInvoiceFinalization_CreditTaxInteraction() {
    // Test that credits are applied before taxes
    // Verify tax calculation uses adjusted amounts
}
```
