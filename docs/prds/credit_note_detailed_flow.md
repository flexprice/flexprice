# Credit Note Implementation - Detailed Flow and Payment Status Scenarios

## Executive Summary

This document provides a comprehensive guide for implementing credit note functionality in FlexPrice, covering all payment status scenarios and detailed flow of amount calculations for both adjustment and refund credit notes.

## Payment Status Decision Matrix

| Payment Status     | Credit Note Type | Invoice Amount Updates | Customer Balance Impact | Validation Rules                             |
| ------------------ | ---------------- | ---------------------- | ----------------------- | -------------------------------------------- |
| PENDING            | ADJUSTMENT       | ✓ Reduce amount_due    | ✗ No direct impact      | Max = invoice.total - existing_adjustments   |
| PROCESSING         | ADJUSTMENT       | ✓ Reduce amount_due    | ✗ No direct impact      | Max = invoice.total - existing_adjustments   |
| SUCCEEDED          | REFUND           | ✗ No invoice changes   | ✓ Add to wallet balance | Max = invoice.amount_paid - existing_refunds |
| FAILED             | ADJUSTMENT       | ✓ Reduce amount_due    | ✗ No direct impact      | Max = invoice.total - existing_adjustments   |
| REFUNDED           | ❌ NOT ALLOWED   | ✗ No changes           | ✗ No changes            | Return error                                 |
| PARTIALLY_REFUNDED | REFUND           | ✗ No invoice changes   | ✓ Add to wallet balance | Max = invoice.amount_paid - existing_refunds |

## Detailed Implementation Flow

### 1. Initial Validation and Type Determination

```go
func (s *creditNoteService) ValidateCreditNoteCreation(ctx context.Context, req *dto.CreateCreditNoteRequest) error {
    // Get invoice with existing credit notes
    invoice, err := s.InvoiceRepo.Get(ctx, req.InvoiceID)
    if err != nil {
        return err
    }

    // Check invoice status - must be FINALIZED
    if invoice.InvoiceStatus != types.InvoiceStatusFinalized {
        return ierr.NewError("invoice status is not allowed").
            WithHintf("Invoice must be finalized to issue a credit note").
            WithReportableDetails(map[string]any{
                "invoice_status": invoice.InvoiceStatus,
            }).
            Mark(ierr.ErrValidation)
    }

    // Determine credit note type and validate eligibility
    creditNoteType, err := s.determineCreditNoteType(ctx, invoice)
    if err != nil {
        return err
    }

    // Get maximum creditable amount based on type
    maxCreditableResponse, err := s.getMaxCreditableAmountByType(ctx, invoice, creditNoteType)
    if err != nil {
        return err
    }

    // Validate requested amount against maximum
    return s.validateRequestedAmount(ctx, req, maxCreditableResponse)
}

func (s *creditNoteService) determineCreditNoteType(ctx context.Context, invoice *invoice.Invoice) (types.CreditNoteType, error) {
    switch invoice.PaymentStatus {
    case types.PaymentStatusPending, types.PaymentStatusProcessing, types.PaymentStatusFailed:
        return types.CreditNoteTypeAdjustment, nil
    case types.PaymentStatusSucceeded, types.PaymentStatusPartiallyRefunded:
        return types.CreditNoteTypeRefund, nil
    case types.PaymentStatusRefunded:
        return "", ierr.NewError("cannot create credit note for fully refunded invoice").
            WithHintf("Invoice is already fully refunded").
            Mark(ierr.ErrValidation)
    default:
        return "", ierr.NewError("invalid payment status for credit note creation").
            WithHintf("Unsupported payment status: %s", invoice.PaymentStatus).
            Mark(ierr.ErrValidation)
    }
}
```

### 2. Maximum Creditable Amount Calculation by Type

#### For ADJUSTMENT Credit Notes

```go
func (s *creditNoteService) getMaxCreditableAmountForAdjustment(ctx context.Context, invoice *invoice.Invoice) (*dto.GetMaxCreditableAmountResponse, error) {
    // Get all existing ADJUSTMENT credit notes for this invoice
    existingAdjustmentCredits, err := s.CreditNoteRepo.List(ctx, &types.CreditNoteFilter{
        InvoiceID:          invoice.ID,
        CreditNoteType:     string(types.CreditNoteTypeAdjustment),
        CreditNoteStatus:   []types.CreditNoteStatus{types.CreditNoteStatusIssued},
        QueryFilter: types.QueryFilter{
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return nil, err
    }

    // Calculate total existing adjustment credits
    totalExistingAdjustments := decimal.Zero
    for _, credit := range existingAdjustmentCredits {
        totalExistingAdjustments = totalExistingAdjustments.Add(credit.Total)
    }

    // For adjustment credits: Max = invoice.total - sum(existing_adjustment_credits)
    maxCreditable := invoice.Total.Sub(totalExistingAdjustments)
    if maxCreditable.IsNegative() {
        maxCreditable = decimal.Zero
    }

    return &dto.GetMaxCreditableAmountResponse{
        InvoiceTotal:                 invoice.Total,
        InvoiceAmountDue:            invoice.AmountDue,
        InvoiceAmountPaid:           invoice.AmountPaid,
        InvoiceAmountRemaining:      invoice.AmountRemaining,
        AlreadyCreditedAmount:       totalExistingAdjustments,
        MaxCreditableAmount:         maxCreditable,
        AvailableCreditableAmount:   maxCreditable,
        CreditNoteType:              types.CreditNoteTypeAdjustment,
    }, nil
}
```

#### For REFUND Credit Notes

```go
func (s *creditNoteService) getMaxCreditableAmountForRefund(ctx context.Context, invoice *invoice.Invoice) (*dto.GetMaxCreditableAmountResponse, error) {
    // Get all existing REFUND credit notes for this invoice
    existingRefundCredits, err := s.CreditNoteRepo.List(ctx, &types.CreditNoteFilter{
        InvoiceID:          invoice.ID,
        CreditNoteType:     string(types.CreditNoteTypeRefund),
        CreditNoteStatus:   []types.CreditNoteStatus{types.CreditNoteStatusIssued},
        QueryFilter: types.QueryFilter{
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return nil, err
    }

    // Calculate total existing refund credits
    totalExistingRefunds := decimal.Zero
    for _, credit := range existingRefundCredits {
        totalExistingRefunds = totalExistingRefunds.Add(credit.Total)
    }

    // For refund credits: Max = invoice.amount_paid - sum(existing_refund_credits)
    maxCreditable := invoice.AmountPaid.Sub(totalExistingRefunds)
    if maxCreditable.IsNegative() {
        maxCreditable = decimal.Zero
    }

    return &dto.GetMaxCreditableAmountResponse{
        InvoiceTotal:                 invoice.Total,
        InvoiceAmountDue:            invoice.AmountDue,
        InvoiceAmountPaid:           invoice.AmountPaid,
        InvoiceAmountRemaining:      invoice.AmountRemaining,
        AlreadyCreditedAmount:       totalExistingRefunds,
        MaxCreditableAmount:         maxCreditable,
        AvailableCreditableAmount:   maxCreditable,
        CreditNoteType:              types.CreditNoteTypeRefund,
    }, nil
}
```

### 3. Credit Note Processing by Type

#### ADJUSTMENT Credit Note Processing

```go
func (s *creditNoteService) ProcessAdjustmentCreditNote(ctx context.Context, creditNote *creditnote.CreditNote) error {
    return s.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Step 1: Update credit note status to ISSUED
        now := time.Now().UTC()
        creditNote.CreditNoteStatus = types.CreditNoteStatusIssued
        creditNote.IssuedAt = &now

        if err := s.CreditNoteRepo.Update(txCtx, creditNote); err != nil {
            return err
        }

        // Step 2: Get current invoice state
        invoice, err := s.InvoiceRepo.Get(txCtx, creditNote.InvoiceID)
        if err != nil {
            return err
        }

        // Step 3: Recalculate invoice amounts
        updatedInvoice, err := s.recalculateInvoiceAmountsWithAdjustment(txCtx, invoice)
        if err != nil {
            return err
        }

        // Step 4: Update payment status if conditions are met
        s.updatePaymentStatusAfterAdjustment(updatedInvoice)

        // Step 5: Save updated invoice
        if err := s.InvoiceRepo.Update(txCtx, updatedInvoice); err != nil {
            return err
        }

        // Step 6: Attempt to reapply customer balance to remaining amount
        if err := s.reapplyCustomerBalanceAfterAdjustment(txCtx, updatedInvoice); err != nil {
            // Log but don't fail the transaction
            s.Logger.Errorw("Failed to reapply customer balance", "error", err, "invoice_id", updatedInvoice.ID)
        }

        return nil
    })
}

func (s *creditNoteService) recalculateInvoiceAmountsWithAdjustment(ctx context.Context, invoice *invoice.Invoice) (*invoice.Invoice, error) {
    // Get all ISSUED adjustment credit notes for this invoice
    adjustmentCredits, err := s.CreditNoteRepo.List(ctx, &types.CreditNoteFilter{
        InvoiceID:          invoice.ID,
        CreditNoteType:     string(types.CreditNoteTypeAdjustment),
        CreditNoteStatus:   []types.CreditNoteStatus{types.CreditNoteStatusIssued},
        QueryFilter: types.QueryFilter{
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return nil, err
    }

    // Calculate total adjustment credits
    totalAdjustmentCredits := decimal.Zero
    for _, credit := range adjustmentCredits {
        totalAdjustmentCredits = totalAdjustmentCredits.Add(credit.Total)
    }

    // Recalculate invoice amounts
    // amount_due = total - sum(adjustment_credits)
    invoice.AmountDue = invoice.Total.Sub(totalAdjustmentCredits)
    if invoice.AmountDue.IsNegative() {
        invoice.AmountDue = decimal.Zero
    }

    // amount_remaining = amount_due - amount_paid
    invoice.AmountRemaining = invoice.AmountDue.Sub(invoice.AmountPaid)
    if invoice.AmountRemaining.IsNegative() {
        invoice.AmountRemaining = decimal.Zero
    }

    return invoice, nil
}

func (s *creditNoteService) updatePaymentStatusAfterAdjustment(invoice *invoice.Invoice) {
    // Case 1: If amount_due is zero (fully adjusted), mark as succeeded
    if invoice.AmountDue.IsZero() {
        invoice.PaymentStatus = types.PaymentStatusSucceeded
        if invoice.PaidAt == nil {
            now := time.Now().UTC()
            invoice.PaidAt = &now
        }
        return
    }

    // Case 2: If amount_remaining is zero and some payment was made, mark as succeeded
    if invoice.AmountRemaining.IsZero() && invoice.AmountPaid.GreaterThan(decimal.Zero) {
        invoice.PaymentStatus = types.PaymentStatusSucceeded
        if invoice.PaidAt == nil {
            now := time.Now().UTC()
            invoice.PaidAt = &now
        }
        return
    }

    // Case 3: Keep current status if still has remaining amount
}
```

#### REFUND Credit Note Processing

```go
func (s *creditNoteService) ProcessRefundCreditNote(ctx context.Context, creditNote *creditnote.CreditNote) error {
    return s.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Step 1: Update credit note status to ISSUED
        now := time.Now().UTC()
        creditNote.CreditNoteStatus = types.CreditNoteStatusIssued
        creditNote.IssuedAt = &now

        if err := s.CreditNoteRepo.Update(txCtx, creditNote); err != nil {
            return err
        }

        // Step 2: Add amount to customer wallet
        if err := s.addRefundToCustomerWallet(txCtx, creditNote); err != nil {
            return err
        }

        // Step 3: Update invoice payment status
        if err := s.updateInvoicePaymentStatusAfterRefund(txCtx, creditNote); err != nil {
            return err
        }

        return nil
    })
}

func (s *creditNoteService) addRefundToCustomerWallet(ctx context.Context, creditNote *creditnote.CreditNote) error {
    // Create wallet transaction for the refund
    walletService := NewWalletService(s.ServiceParams)

    operation := &wallet.WalletOperation{
        CustomerID:        creditNote.CustomerID,
        Currency:          creditNote.Currency,
        Type:              types.TransactionTypeCredit,
        CreditAmount:      creditNote.Total,
        Description:       fmt.Sprintf("Credit note refund: %s", lo.FromPtr(creditNote.CreditNoteNumber)),
        TransactionReason: types.TransactionReasonCreditNote,
        ReferenceType:     types.WalletTxReferenceTypeCreditNote,
        ReferenceID:       creditNote.ID,
        Metadata: types.Metadata{
            "credit_note_id": creditNote.ID,
            "invoice_id":     creditNote.InvoiceID,
            "credit_type":    string(creditNote.CreditNoteType),
        },
    }

    return walletService.CreditWallet(ctx, operation)
}

func (s *creditNoteService) updateInvoicePaymentStatusAfterRefund(ctx context.Context, creditNote *creditnote.CreditNote) error {
    invoice, err := s.InvoiceRepo.Get(ctx, creditNote.InvoiceID)
    if err != nil {
        return err
    }

    // Get all ISSUED refund credit notes for this invoice
    refundCredits, err := s.CreditNoteRepo.List(ctx, &types.CreditNoteFilter{
        InvoiceID:          invoice.ID,
        CreditNoteType:     string(types.CreditNoteTypeRefund),
        CreditNoteStatus:   []types.CreditNoteStatus{types.CreditNoteStatusIssued},
        QueryFilter: types.QueryFilter{
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return err
    }

    // Calculate total refunded amount
    totalRefunded := decimal.Zero
    for _, credit := range refundCredits {
        totalRefunded = totalRefunded.Add(credit.Total)
    }

    // Update payment status based on refund amount vs amount paid
    if totalRefunded.Equal(invoice.AmountPaid) && totalRefunded.GreaterThan(decimal.Zero) {
        invoice.PaymentStatus = types.PaymentStatusRefunded
    } else if totalRefunded.GreaterThan(decimal.Zero) && totalRefunded.LessThan(invoice.AmountPaid) {
        invoice.PaymentStatus = types.PaymentStatusPartiallyRefunded
    }
    // If totalRefunded is zero, keep current status

    return s.InvoiceRepo.Update(ctx, invoice)
}
```

## Detailed Payment Status Scenarios

### Scenario 1: PENDING Payment Status → ADJUSTMENT Credit Note

**Initial State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: PENDING
- Total: $100.00
- Amount Due: $100.00
- Amount Paid: $0.00
- Amount Remaining: $100.00
```

**Customer Request:** $30 credit note

**Process Flow:**

1. Validate invoice status ✓
2. Determine credit type: ADJUSTMENT
3. Check max creditable: $100.00 - $0.00 = $100.00 ✓
4. Create adjustment credit note: $30.00
5. Recalculate invoice amounts:
   - Amount Due: $100.00 - $30.00 = $70.00
   - Amount Remaining: $70.00 - $0.00 = $70.00
6. Payment status remains PENDING

**Final State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: PENDING
- Total: $100.00
- Amount Due: $70.00
- Amount Paid: $0.00
- Amount Remaining: $70.00

Credit Note:
- Type: ADJUSTMENT
- Status: ISSUED
- Amount: $30.00
```

### Scenario 2: SUCCEEDED Payment Status → REFUND Credit Note

**Initial State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: SUCCEEDED
- Total: $100.00
- Amount Due: $100.00
- Amount Paid: $100.00
- Amount Remaining: $0.00
```

**Customer Request:** $30 credit note

**Process Flow:**

1. Validate invoice status ✓
2. Determine credit type: REFUND
3. Check max creditable: $100.00 - $0.00 = $100.00 ✓
4. Create refund credit note: $30.00
5. Add $30.00 to customer wallet
6. Update payment status: PARTIALLY_REFUNDED

**Final State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: PARTIALLY_REFUNDED
- Total: $100.00
- Amount Due: $100.00 (unchanged)
- Amount Paid: $100.00 (unchanged)
- Amount Remaining: $0.00 (unchanged)

Credit Note:
- Type: REFUND
- Status: ISSUED
- Amount: $30.00

Customer Wallet: +$30.00
```

### Scenario 3: PARTIALLY_REFUNDED → Additional REFUND Credit Note

**Initial State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: PARTIALLY_REFUNDED
- Total: $100.00
- Amount Due: $100.00
- Amount Paid: $100.00
- Amount Remaining: $0.00

Existing Refund Credits: $20.00
```

**Customer Request:** $30 credit note

**Process Flow:**

1. Validate invoice status ✓
2. Determine credit type: REFUND
3. Check max creditable: $100.00 - $20.00 = $80.00 ✓
4. Validate requested amount: $30.00 ≤ $80.00 ✓
5. Create refund credit note: $30.00
6. Add $30.00 to customer wallet
7. Total refunded now: $20.00 + $30.00 = $50.00
8. Payment status remains: PARTIALLY_REFUNDED

**Final State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: PARTIALLY_REFUNDED
- Total: $100.00
- Amount Due: $100.00
- Amount Paid: $100.00
- Amount Remaining: $0.00

Total Refund Credits: $50.00
Customer Wallet: +$50.00 (total)
```

### Scenario 4: Complex ADJUSTMENT with Customer Balance

**Initial State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: PENDING
- Total: $100.00
- Amount Due: $100.00
- Amount Paid: $0.00
- Amount Remaining: $100.00

Customer Wallet Balance: $40.00
```

**Customer Request:** $60 credit note

**Process Flow:**

1. Create ADJUSTMENT credit note: $60.00
2. Recalculate invoice amounts:
   - Amount Due: $100.00 - $60.00 = $40.00
   - Amount Remaining: $40.00 - $0.00 = $40.00
3. Reapply customer balance: $40.00 available
4. Apply balance to remaining amount:
   - Amount Paid: $0.00 + $40.00 = $40.00
   - Amount Remaining: $40.00 - $40.00 = $0.00
5. Update payment status: SUCCEEDED

**Final State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: SUCCEEDED
- Total: $100.00
- Amount Due: $40.00
- Amount Paid: $40.00
- Amount Remaining: $0.00

Credit Note: $60.00 (ADJUSTMENT)
Customer Wallet Balance: $0.00 (used $40.00)
```

### Scenario 5: REFUNDED Status → Error Case

**Initial State:**

```
Invoice:
- Status: FINALIZED
- Payment Status: REFUNDED
- Total: $100.00
- Amount Due: $100.00
- Amount Paid: $100.00
- Amount Remaining: $0.00

Existing Refund Credits: $100.00
```

**Customer Request:** $10 credit note

**Process Flow:**

1. Validate invoice status ✓
2. Determine credit type: ERROR ✗
3. Return error: "Cannot create credit note for fully refunded invoice"

## Implementation Considerations

### Transaction Safety

```go
func (s *creditNoteService) CreateCreditNote(ctx context.Context, req *dto.CreateCreditNoteRequest) (*dto.CreditNoteResponse, error) {
    return s.DB.WithTx(ctx, func(txCtx context.Context) (*dto.CreditNoteResponse, error) {
        // Lock invoice for update to prevent race conditions
        invoice, err := s.InvoiceRepo.GetForUpdate(txCtx, req.InvoiceID)
        if err != nil {
            return nil, err
        }

        // Re-validate with locked invoice state
        if err := s.ValidateCreditNoteCreation(txCtx, req); err != nil {
            return nil, err
        }

        // Create and process credit note
        creditNote, err := s.createAndProcessCreditNote(txCtx, req, invoice)
        if err != nil {
            return nil, err
        }

        return dto.NewCreditNoteResponse(creditNote), nil
    })
}
```

### Error Handling

```go
var (
    ErrInvoiceNotFinalized = ierr.NewError("invoice must be finalized for credit note creation")
    ErrInvoiceFullyRefunded = ierr.NewError("cannot create credit note for fully refunded invoice")
    ErrExceedsMaxCreditable = ierr.NewError("credit amount exceeds maximum creditable amount")
    ErrInvalidPaymentStatus = ierr.NewError("invalid payment status for credit note creation")
)
```

This comprehensive flow ensures proper handling of all payment status scenarios while maintaining data consistency and audit trails.

## Database Queries for Amount Calculations

### Get Existing Credit Notes by Type

```sql
-- Get all ISSUED adjustment credit notes for max creditable calculation
SELECT id, total, credit_note_status, created_at
FROM credit_notes
WHERE invoice_id = ?
  AND credit_note_type = 'ADJUSTMENT'
  AND credit_note_status = 'ISSUED'
  AND status = 'published'
  AND tenant_id = ?
  AND environment_id = ?
ORDER BY created_at ASC;

-- Get all ISSUED refund credit notes for max creditable calculation
SELECT id, total, credit_note_status, created_at
FROM credit_notes
WHERE invoice_id = ?
  AND credit_note_type = 'REFUND'
  AND credit_note_status = 'ISSUED'
  AND status = 'published'
  AND tenant_id = ?
  AND environment_id = ?
ORDER BY created_at ASC;
```

### Aggregate Queries for Amount Calculations

```sql
-- Calculate total adjustment credits for invoice amount_due calculation
SELECT COALESCE(SUM(total), 0) as total_adjustment_credits
FROM credit_notes
WHERE invoice_id = ?
  AND credit_note_type = 'ADJUSTMENT'
  AND credit_note_status = 'ISSUED'
  AND status = 'published'
  AND tenant_id = ?
  AND environment_id = ?;

-- Calculate total refund credits for payment status determination
SELECT COALESCE(SUM(total), 0) as total_refund_credits
FROM credit_notes
WHERE invoice_id = ?
  AND credit_note_type = 'REFUND'
  AND credit_note_status = 'ISSUED'
  AND status = 'published'
  AND tenant_id = ?
  AND environment_id = ?;
```

## Implementation Helper Functions

### Amount Calculation Utilities

```go
// CalculateInvoiceAmountDue calculates the current amount due considering all adjustments
func (s *creditNoteService) CalculateInvoiceAmountDue(ctx context.Context, invoiceID string) (decimal.Decimal, error) {
    invoice, err := s.InvoiceRepo.Get(ctx, invoiceID)
    if err != nil {
        return decimal.Zero, err
    }

    totalAdjustments, err := s.getTotalAdjustmentCredits(ctx, invoiceID)
    if err != nil {
        return decimal.Zero, err
    }

    amountDue := invoice.Total.Sub(totalAdjustments)
    if amountDue.IsNegative() {
        amountDue = decimal.Zero
    }

    return amountDue, nil
}

// GetTotalAdjustmentCredits gets the sum of all issued adjustment credit notes
func (s *creditNoteService) getTotalAdjustmentCredits(ctx context.Context, invoiceID string) (decimal.Decimal, error) {
    credits, err := s.CreditNoteRepo.List(ctx, &types.CreditNoteFilter{
        InvoiceID:        invoiceID,
        CreditNoteType:   string(types.CreditNoteTypeAdjustment),
        CreditNoteStatus: []types.CreditNoteStatus{types.CreditNoteStatusIssued},
        QueryFilter: types.QueryFilter{
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return decimal.Zero, err
    }

    total := decimal.Zero
    for _, credit := range credits {
        total = total.Add(credit.Total)
    }

    return total, nil
}

// GetTotalRefundCredits gets the sum of all issued refund credit notes
func (s *creditNoteService) getTotalRefundCredits(ctx context.Context, invoiceID string) (decimal.Decimal, error) {
    credits, err := s.CreditNoteRepo.List(ctx, &types.CreditNoteFilter{
        InvoiceID:        invoiceID,
        CreditNoteType:   string(types.CreditNoteTypeRefund),
        CreditNoteStatus: []types.CreditNoteStatus{types.CreditNoteStatusIssued},
        QueryFilter: types.QueryFilter{
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return decimal.Zero, err
    }

    total := decimal.Zero
    for _, credit := range credits {
        total = total.Add(credit.Total)
    }

    return total, nil
}
```

### Payment Status Management

```go
// UpdatePaymentStatusBasedOnAmounts updates payment status based on current amounts
func (s *creditNoteService) UpdatePaymentStatusBasedOnAmounts(invoice *invoice.Invoice) {
    // Case 1: Amount due is zero (fully adjusted) - mark as succeeded
    if invoice.AmountDue.IsZero() {
        invoice.PaymentStatus = types.PaymentStatusSucceeded
        if invoice.PaidAt == nil {
            now := time.Now().UTC()
            invoice.PaidAt = &now
        }
        return
    }

    // Case 2: Amount remaining is zero and payment was made - mark as succeeded
    if invoice.AmountRemaining.IsZero() && invoice.AmountPaid.GreaterThan(decimal.Zero) {
        invoice.PaymentStatus = types.PaymentStatusSucceeded
        if invoice.PaidAt == nil {
            now := time.Now().UTC()
            invoice.PaidAt = &now
        }
        return
    }

    // Case 3: Has remaining amount - keep appropriate pending status
    if invoice.AmountRemaining.GreaterThan(decimal.Zero) {
        // Keep current status (PENDING, PROCESSING, FAILED)
        return
    }
}

// UpdatePaymentStatusAfterRefund updates payment status after refund credit note
func (s *creditNoteService) UpdatePaymentStatusAfterRefund(ctx context.Context, invoice *invoice.Invoice) error {
    totalRefunds, err := s.getTotalRefundCredits(ctx, invoice.ID)
    if err != nil {
        return err
    }

    if totalRefunds.Equal(invoice.AmountPaid) && totalRefunds.GreaterThan(decimal.Zero) {
        invoice.PaymentStatus = types.PaymentStatusRefunded
    } else if totalRefunds.GreaterThan(decimal.Zero) && totalRefunds.LessThan(invoice.AmountPaid) {
        invoice.PaymentStatus = types.PaymentStatusPartiallyRefunded
    }
    // If no refunds, keep current status

    return nil
}
```

## Testing Scenarios Matrix

| Initial Payment Status | Credit Amount        | Expected Credit Type | Expected Final Payment Status | Amount Due Change | Amount Paid Change | Notes                     |
| ---------------------- | -------------------- | -------------------- | ----------------------------- | ----------------- | ------------------ | ------------------------- |
| PENDING                | $30 of $100          | ADJUSTMENT           | PENDING                       | $100 → $70        | No change          | Standard adjustment       |
| PENDING                | $100 of $100         | ADJUSTMENT           | SUCCEEDED                     | $100 → $0         | No change          | Fully adjusted            |
| PROCESSING             | $50 of $100          | ADJUSTMENT           | PROCESSING                    | $100 → $50        | No change          | Partial adjustment        |
| FAILED                 | $25 of $100          | ADJUSTMENT           | FAILED                        | $100 → $75        | No change          | Failed payment, adjusted  |
| SUCCEEDED              | $30 of $100          | REFUND               | PARTIALLY_REFUNDED            | No change         | No change          | Partial refund to wallet  |
| SUCCEEDED              | $100 of $100         | REFUND               | REFUNDED                      | No change         | No change          | Full refund to wallet     |
| PARTIALLY_REFUNDED     | $20 of $80 remaining | REFUND               | PARTIALLY_REFUNDED            | No change         | No change          | Additional refund         |
| PARTIALLY_REFUNDED     | $80 of $80 remaining | REFUND               | REFUNDED                      | No change         | No change          | Complete remaining refund |
| REFUNDED               | Any amount           | ERROR                | N/A                           | N/A               | N/A                | Should return error       |

## API Response Examples

### Get Max Creditable Amount - ADJUSTMENT

```json
{
  "invoice_id": "inv_123",
  "credit_note_type": "ADJUSTMENT",
  "invoice_total": "100.00",
  "invoice_amount_due": "100.00",
  "invoice_amount_paid": "0.00",
  "invoice_amount_remaining": "100.00",
  "already_credited_amount": "0.00",
  "max_creditable_amount": "100.00",
  "available_creditable_amount": "100.00"
}
```

### Get Max Creditable Amount - REFUND

```json
{
  "invoice_id": "inv_123",
  "credit_note_type": "REFUND",
  "invoice_total": "100.00",
  "invoice_amount_due": "100.00",
  "invoice_amount_paid": "100.00",
  "invoice_amount_remaining": "0.00",
  "already_credited_amount": "20.00",
  "max_creditable_amount": "80.00",
  "available_creditable_amount": "80.00"
}
```

This comprehensive implementation covers all payment status scenarios with detailed amount calculations and proper data consistency.
