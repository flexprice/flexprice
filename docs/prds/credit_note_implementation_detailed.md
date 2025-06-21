# Credit Note Implementation - Detailed Flow and Amount Calculations

## Executive Summary

This document provides a comprehensive guide for implementing credit note functionality in FlexPrice, covering all payment status scenarios and the detailed flow of amount calculations for both adjustment and refund credit notes.

## Payment Status Scenarios and Credit Note Types

### Payment Status Decision Matrix

| Payment Status     | Credit Note Type | Invoice Amount Updates | Customer Balance Impact | Allowed |
| ------------------ | ---------------- | ---------------------- | ----------------------- | ------- |
| PENDING            | ADJUSTMENT       | ✓ Reduce amount_due    | ✗ No direct impact      | ✓       |
| PROCESSING         | ADJUSTMENT       | ✓ Reduce amount_due    | ✗ No direct impact      | ✓       |
| SUCCEEDED          | REFUND           | ✗ No invoice changes   | ✓ Add to balance        | ✓       |
| FAILED             | ADJUSTMENT       | ✓ Reduce amount_due    | ✗ No direct impact      | ✓       |
| REFUNDED           | N/A              | ✗ No changes allowed   | ✗ No changes            | ✗       |
| PARTIALLY_REFUNDED | REFUND           | ✗ No invoice changes   | ✓ Add remaining balance | ✓       |

## Detailed Implementation Flow

### 1. Initial Validation Phase

```go
func (s *creditNoteService) ValidateCreditNoteCreation(ctx context.Context, req *dto.CreateCreditNoteRequest) error {
    // Step 1: Get and validate invoice
    invoice, err := s.InvoiceRepo.Get(ctx, req.InvoiceID)
    if err != nil {
        return err
    }

    // Step 2: Check invoice status
    if invoice.InvoiceStatus != types.InvoiceStatusFinalized {
        return ierr.NewError("invoice must be finalized")
    }

    // Step 3: Check payment status eligibility
    switch invoice.PaymentStatus {
    case types.PaymentStatusRefunded:
        return ierr.NewError("cannot create credit note for fully refunded invoice")
    case types.PaymentStatusPending, types.PaymentStatusProcessing, types.PaymentStatusFailed:
        // These statuses allow ADJUSTMENT credit notes
    case types.PaymentStatusSucceeded, types.PaymentStatusPartiallyRefunded:
        // These statuses allow REFUND credit notes
    default:
        return ierr.NewError("invalid payment status for credit note creation")
    }

    return nil
}
```

### 2. Maximum Creditable Amount Calculation

#### For ADJUSTMENT Credit Notes (Unpaid Invoices)

```go
func (s *creditNoteService) GetMaxCreditableAmountForAdjustment(ctx context.Context, invoiceID string) (*dto.GetMaxCreditableAmountResponse, error) {
    invoice, err := s.InvoiceRepo.Get(ctx, invoiceID)
    if err != nil {
        return nil, err
    }

    // Get existing ADJUSTMENT credit notes
    existingAdjustmentCredits, err := s.CreditNoteRepo.GetAdjustmentCreditsByInvoiceID(ctx, invoiceID)
    if err != nil {
        return nil, err
    }

    // Calculate total existing adjustment credits
    totalExistingAdjustments := decimal.Zero
    for _, credit := range existingAdjustmentCredits {
        if credit.CreditNoteStatus == types.CreditNoteStatusIssued {
            totalExistingAdjustments = totalExistingAdjustments.Add(credit.Total)
        }
    }

    // Maximum creditable = invoice.total - existing_adjustment_credits
    maxCreditable := invoice.Total.Sub(totalExistingAdjustments)
    if maxCreditable.IsNegative() {
        maxCreditable = decimal.Zero
    }

    return &dto.GetMaxCreditableAmountResponse{
        InvoiceTotal:              invoice.Total,
        InvoiceAmountDue:          invoice.AmountDue,
        ExistingAdjustmentCredits: totalExistingAdjustments,
        MaxCreditableAmount:       maxCreditable,
        AvailableCreditableAmount: maxCreditable,
    }, nil
}
```

#### For REFUND Credit Notes (Paid Invoices)

```go
func (s *creditNoteService) GetMaxCreditableAmountForRefund(ctx context.Context, invoiceID string) (*dto.GetMaxCreditableAmountResponse, error) {
    invoice, err := s.InvoiceRepo.Get(ctx, invoiceID)
    if err != nil {
        return nil, err
    }

    // Get existing REFUND credit notes
    existingRefundCredits, err := s.CreditNoteRepo.GetRefundCreditsByInvoiceID(ctx, invoiceID)
    if err != nil {
        return nil, err
    }

    // Calculate total existing refund credits
    totalExistingRefunds := decimal.Zero
    for _, credit := range existingRefundCredits {
        if credit.CreditNoteStatus == types.CreditNoteStatusIssued {
            totalExistingRefunds = totalExistingRefunds.Add(credit.Total)
        }
    }

    // For refund credit notes, max creditable = amount_paid - existing_refunds
    maxCreditable := invoice.AmountPaid.Sub(totalExistingRefunds)
    if maxCreditable.IsNegative() {
        maxCreditable = decimal.Zero
    }

    return &dto.GetMaxCreditableAmountResponse{
        InvoiceTotal:           invoice.Total,
        InvoiceAmountPaid:      invoice.AmountPaid,
        ExistingRefundCredits:  totalExistingRefunds,
        MaxCreditableAmount:    maxCreditable,
        AvailableCreditableAmount: maxCreditable,
    }, nil
}
```

### 3. Credit Note Type Determination

```go
func (s *creditNoteService) DetermineCreditNoteType(ctx context.Context, invoice *invoice.Invoice) types.CreditNoteType {
    switch invoice.PaymentStatus {
    case types.PaymentStatusPending, types.PaymentStatusProcessing, types.PaymentStatusFailed:
        return types.CreditNoteTypeAdjustment
    case types.PaymentStatusSucceeded, types.PaymentStatusPartiallyRefunded:
        return types.CreditNoteTypeRefund
    default:
        // This should be caught in validation, but return adjustment as fallback
        return types.CreditNoteTypeAdjustment
    }
}
```

### 4. ADJUSTMENT Credit Note Processing

```go
func (s *creditNoteService) ProcessAdjustmentCreditNote(ctx context.Context, creditNote *creditnote.CreditNote) error {
    return s.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Step 1: Update credit note status
        creditNote.CreditNoteStatus = types.CreditNoteStatusIssued
        creditNote.IssuedAt = lo.ToPtr(time.Now().UTC())

        if err := s.CreditNoteRepo.Update(txCtx, creditNote); err != nil {
            return err
        }

        // Step 2: Get current invoice state
        invoice, err := s.InvoiceRepo.Get(txCtx, creditNote.InvoiceID)
        if err != nil {
            return err
        }

        // Step 3: Recalculate invoice amounts
        updatedInvoice, err := s.RecalculateInvoiceAmountsWithAdjustment(txCtx, invoice)
        if err != nil {
            return err
        }

        // Step 4: Update payment status if fully covered
        s.UpdatePaymentStatusAfterAdjustment(txCtx, updatedInvoice)

        // Step 5: Save updated invoice
        if err := s.InvoiceRepo.Update(txCtx, updatedInvoice); err != nil {
            return err
        }

        // Step 6: Reapply customer balance if available
        if err := s.ReapplyCustomerBalanceAfterAdjustment(txCtx, updatedInvoice); err != nil {
            return err
        }

        return nil
    })
}

func (s *creditNoteService) RecalculateInvoiceAmountsWithAdjustment(ctx context.Context, invoice *invoice.Invoice) (*invoice.Invoice, error) {
    // Get all ISSUED adjustment credit notes for this invoice
    adjustmentCredits, err := s.CreditNoteRepo.GetAdjustmentCreditsByInvoiceID(ctx, invoice.ID)
    if err != nil {
        return nil, err
    }

    totalAdjustmentCredits := decimal.Zero
    for _, credit := range adjustmentCredits {
        if credit.CreditNoteStatus == types.CreditNoteStatusIssued {
            totalAdjustmentCredits = totalAdjustmentCredits.Add(credit.Total)
        }
    }

    // Recalculate amounts
    invoice.AmountDue = invoice.Total.Sub(totalAdjustmentCredits)
    if invoice.AmountDue.IsNegative() {
        invoice.AmountDue = decimal.Zero
    }

    invoice.AmountRemaining = invoice.AmountDue.Sub(invoice.AmountPaid)
    if invoice.AmountRemaining.IsNegative() {
        invoice.AmountRemaining = decimal.Zero
    }

    return invoice, nil
}

func (s *creditNoteService) UpdatePaymentStatusAfterAdjustment(ctx context.Context, invoice *invoice.Invoice) {
    // If amount_remaining is zero and some payment was made, mark as succeeded
    if invoice.AmountRemaining.IsZero() && invoice.AmountPaid.GreaterThan(decimal.Zero) {
        invoice.PaymentStatus = types.PaymentStatusSucceeded
        if invoice.PaidAt == nil {
            invoice.PaidAt = lo.ToPtr(time.Now().UTC())
        }
    }
    // If amount_due is zero (fully adjusted), mark as succeeded even without payment
    if invoice.AmountDue.IsZero() {
        invoice.PaymentStatus = types.PaymentStatusSucceeded
        if invoice.PaidAt == nil {
            invoice.PaidAt = lo.ToPtr(time.Now().UTC())
        }
    }
}
```

### 5. REFUND Credit Note Processing

```go
func (s *creditNoteService) ProcessRefundCreditNote(ctx context.Context, creditNote *creditnote.CreditNote) error {
    return s.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Step 1: Update credit note status
        creditNote.CreditNoteStatus = types.CreditNoteStatusIssued
        creditNote.IssuedAt = lo.ToPtr(time.Now().UTC())

        if err := s.CreditNoteRepo.Update(txCtx, creditNote); err != nil {
            return err
        }

        // Step 2: Add amount to customer wallet
        if err := s.AddToCustomerWallet(txCtx, creditNote); err != nil {
            return err
        }

        // Step 3: Update invoice payment status if needed
        if err := s.UpdateInvoicePaymentStatusAfterRefund(txCtx, creditNote); err != nil {
            return err
        }

        return nil
    })
}

func (s *creditNoteService) AddToCustomerWallet(ctx context.Context, creditNote *creditnote.CreditNote) error {
    // Get or create customer wallet for the currency
    wallet, err := s.WalletService.GetOrCreateWallet(ctx, creditNote.CustomerID, creditNote.Currency)
    if err != nil {
        return err
    }

    // Create wallet transaction
    transaction := &wallettransaction.WalletTransaction{
        ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
        WalletID:          wallet.ID,
        TransactionType:   types.TransactionTypeCredit,
        Amount:            creditNote.Total,
        Currency:          creditNote.Currency,
        Description:       fmt.Sprintf("Credit note refund: %s", lo.FromPtr(creditNote.CreditNoteNumber)),
        TransactionReason: types.TransactionReasonCreditNote,
        ReferenceType:     types.WalletTxReferenceTypeCreditNote,
        ReferenceID:       creditNote.ID,
        BaseModel:         types.GetDefaultBaseModel(ctx),
    }

    return s.WalletService.CreditWallet(ctx, wallet.ID, transaction)
}

func (s *creditNoteService) UpdateInvoicePaymentStatusAfterRefund(ctx context.Context, creditNote *creditnote.CreditNote) error {
    invoice, err := s.InvoiceRepo.Get(ctx, creditNote.InvoiceID)
    if err != nil {
        return err
    }

    // Calculate total refund credits for this invoice
    refundCredits, err := s.CreditNoteRepo.GetRefundCreditsByInvoiceID(ctx, invoice.ID)
    if err != nil {
        return err
    }

    totalRefunded := decimal.Zero
    for _, credit := range refundCredits {
        if credit.CreditNoteStatus == types.CreditNoteStatusIssued {
            totalRefunded = totalRefunded.Add(credit.Total)
        }
    }

    // Update payment status based on refund amount
    if totalRefunded.Equal(invoice.AmountPaid) {
        invoice.PaymentStatus = types.PaymentStatusRefunded
    } else if totalRefunded.GreaterThan(decimal.Zero) {
        invoice.PaymentStatus = types.PaymentStatusPartiallyRefunded
    }

    return s.InvoiceRepo.Update(ctx, invoice)
}
```

### 6. Customer Balance Reapplication (for Adjustment Credits)

```go
func (s *creditNoteService) ReapplyCustomerBalanceAfterAdjustment(ctx context.Context, invoice *invoice.Invoice) error {
    // Only reapply if there's still an amount remaining
    if invoice.AmountRemaining.LessThanOrEqual(decimal.Zero) {
        return nil
    }

    // Get available customer balance for this currency
    availableBalance, err := s.WalletService.GetAvailableBalance(ctx, invoice.CustomerID, invoice.Currency)
    if err != nil {
        return err
    }

    if availableBalance.LessThanOrEqual(decimal.Zero) {
        return nil // No balance to apply
    }

    // Calculate how much balance can be applied
    applicableAmount := decimal.Min(availableBalance, invoice.AmountRemaining)

    if applicableAmount.GreaterThan(decimal.Zero) {
        // Apply balance to invoice
        if err := s.ApplyCustomerBalanceToInvoice(ctx, invoice, applicableAmount); err != nil {
            return err
        }

        // Update invoice amounts
        invoice.AmountPaid = invoice.AmountPaid.Add(applicableAmount)
        invoice.AmountRemaining = invoice.AmountDue.Sub(invoice.AmountPaid)

        // Update payment status if fully paid
        if invoice.AmountRemaining.IsZero() {
            invoice.PaymentStatus = types.PaymentStatusSucceeded
            if invoice.PaidAt == nil {
                invoice.PaidAt = lo.ToPtr(time.Now().UTC())
            }
        }

        // Save updated invoice
        return s.InvoiceRepo.Update(ctx, invoice)
    }

    return nil
}
```

## Payment Status Specific Scenarios

### Scenario 1: PENDING Payment Status

**Initial State:**

- Invoice: FINALIZED, amount_due = $100, amount_paid = $0, payment_status = PENDING
- Customer requests $30 credit note

**Process:**

1. Create ADJUSTMENT credit note for $30
2. Update invoice: amount_due = $70, amount_remaining = $70
3. Payment status remains PENDING
4. When payment of $70 is processed later, status becomes SUCCEEDED

### Scenario 2: SUCCEEDED Payment Status

**Initial State:**

- Invoice: FINALIZED, amount_due = $100, amount_paid = $100, payment_status = SUCCEEDED
- Customer requests $30 credit note

**Process:**

1. Create REFUND credit note for $30
2. Add $30 to customer wallet balance
3. Update invoice payment_status = PARTIALLY_REFUNDED
4. Invoice amounts remain unchanged (already paid)

### Scenario 3: PARTIALLY_REFUNDED Payment Status

**Initial State:**

- Invoice: FINALIZED, amount_due = $100, amount_paid = $100, payment_status = PARTIALLY_REFUNDED
- Previous refund credit notes: $20
- Customer requests additional $30 credit note

**Process:**

1. Validate: Total refunds ($20 + $30 = $50) ≤ amount_paid ($100) ✓
2. Create REFUND credit note for $30
3. Add $30 to customer wallet balance
4. Payment status remains PARTIALLY_REFUNDED
5. Total refunded: $50

### Scenario 4: Complex Adjustment with Balance Reapplication

**Initial State:**

- Invoice: FINALIZED, amount_due = $100, amount_paid = $0, payment_status = PENDING
- Customer has $40 wallet balance
- Customer requests $60 credit note

**Process:**

1. Create ADJUSTMENT credit note for $60
2. Update invoice: amount_due = $40, amount_remaining = $40
3. Reapply customer balance: $40 from wallet
4. Final state: amount_due = $40, amount_paid = $40, amount_remaining = $0
5. Update payment_status = SUCCEEDED

## Error Handling and Edge Cases

### Edge Case 1: Multiple Concurrent Credit Notes

```go
func (s *creditNoteService) HandleConcurrentCreditNotes(ctx context.Context, invoiceID string, operation func(context.Context) error) error {
    return s.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Lock invoice row for update to prevent race conditions
        invoice, err := s.InvoiceRepo.GetForUpdate(txCtx, invoiceID)
        if err != nil {
            return err
        }

        // Execute operation with locked invoice
        return operation(txCtx)
    })
}
```

### Edge Case 2: Credit Note Exceeding Available Amount

```go
func (s *creditNoteService) ValidateCreditAmountAgainstCurrentState(ctx context.Context, req *dto.CreateCreditNoteRequest) error {
    // Get current invoice state with all existing credit notes
    invoice, err := s.InvoiceRepo.GetWithCreditNotes(ctx, req.InvoiceID)
    if err != nil {
        return err
    }

    // Calculate current maximum creditable amount
    maxCreditable, err := s.GetCurrentMaxCreditableAmount(ctx, invoice)
    if err != nil {
        return err
    }

    requestedAmount := s.calculateTotalRequestedAmount(req.LineItems)

    if requestedAmount.GreaterThan(maxCreditable.AvailableCreditableAmount) {
        return ierr.NewError("credit amount exceeds currently available creditable amount").
            WithReportableDetails(map[string]any{
                "requested_amount": requestedAmount,
                "available_amount": maxCreditable.AvailableCreditableAmount,
                "existing_credits": maxCreditable.ExistingCredits,
            })
    }

    return nil
}
```

## Database Schema Considerations

### Credit Note Queries for Amount Calculations

```sql
-- Get all adjustment credit notes for an invoice
SELECT cn.id, cn.total, cn.credit_note_status
FROM credit_notes cn
WHERE cn.invoice_id = ?
  AND cn.credit_note_type = 'ADJUSTMENT'
  AND cn.status = 'published'
  AND cn.tenant_id = ?
  AND cn.environment_id = ?;

-- Get all refund credit notes for an invoice
SELECT cn.id, cn.total, cn.credit_note_status
FROM credit_notes cn
WHERE cn.invoice_id = ?
  AND cn.credit_note_type = 'REFUND'
  AND cn.status = 'published'
  AND cn.tenant_id = ?
  AND cn.environment_id = ?;

-- Calculate total adjustment credits for invoice amount_due calculation
SELECT COALESCE(SUM(cn.total), 0) as total_adjustment_credits
FROM credit_notes cn
WHERE cn.invoice_id = ?
  AND cn.credit_note_type = 'ADJUSTMENT'
  AND cn.credit_note_status = 'ISSUED'
  AND cn.status = 'published'
  AND cn.tenant_id = ?
  AND cn.environment_id = ?;
```

### Indexes for Performance

```sql
-- Index for credit note amount calculations
CREATE INDEX idx_credit_notes_invoice_type_status ON credit_notes(
    tenant_id, environment_id, invoice_id, credit_note_type, credit_note_status
) WHERE status = 'published';

-- Index for customer wallet balance queries
CREATE INDEX idx_wallet_transactions_wallet_type ON wallet_transactions(
    wallet_id, transaction_type, status
) WHERE status = 'published';
```

## Testing Scenarios

### Unit Test Cases

```go
func TestCreditNoteAmountCalculations(t *testing.T) {
    testCases := []struct {
        name               string
        invoiceTotal       decimal.Decimal
        invoiceAmountPaid  decimal.Decimal
        paymentStatus      types.PaymentStatus
        existingAdjustments decimal.Decimal
        existingRefunds    decimal.Decimal
        requestedCredit    decimal.Decimal
        expectedType       types.CreditNoteType
        expectedMaxCredit  decimal.Decimal
        shouldError        bool
    }{
        {
            name:              "PENDING - First Adjustment Credit",
            invoiceTotal:      decimal.NewFromInt(100),
            invoiceAmountPaid: decimal.Zero,
            paymentStatus:     types.PaymentStatusPending,
            existingAdjustments: decimal.Zero,
            existingRefunds:   decimal.Zero,
            requestedCredit:   decimal.NewFromInt(30),
            expectedType:      types.CreditNoteTypeAdjustment,
            expectedMaxCredit: decimal.NewFromInt(100),
            shouldError:       false,
        },
        {
            name:              "SUCCEEDED - First Refund Credit",
            invoiceTotal:      decimal.NewFromInt(100),
            invoiceAmountPaid: decimal.NewFromInt(100),
            paymentStatus:     types.PaymentStatusSucceeded,
            existingAdjustments: decimal.Zero,
            existingRefunds:   decimal.Zero,
            requestedCredit:   decimal.NewFromInt(30),
            expectedType:      types.CreditNoteTypeRefund,
            expectedMaxCredit: decimal.NewFromInt(100),
            shouldError:       false,
        },
        {
            name:              "PARTIALLY_REFUNDED - Additional Refund",
            invoiceTotal:      decimal.NewFromInt(100),
            invoiceAmountPaid: decimal.NewFromInt(100),
            paymentStatus:     types.PaymentStatusPartiallyRefunded,
            existingAdjustments: decimal.Zero,
            existingRefunds:   decimal.NewFromInt(20),
            requestedCredit:   decimal.NewFromInt(30),
            expectedType:      types.CreditNoteTypeRefund,
            expectedMaxCredit: decimal.NewFromInt(80), // 100 - 20
            shouldError:       false,
        },
        {
            name:              "REFUNDED - Should Error",
            invoiceTotal:      decimal.NewFromInt(100),
            invoiceAmountPaid: decimal.NewFromInt(100),
            paymentStatus:     types.PaymentStatusRefunded,
            existingAdjustments: decimal.Zero,
            existingRefunds:   decimal.NewFromInt(100),
            requestedCredit:   decimal.NewFromInt(10),
            expectedType:      types.CreditNoteTypeRefund,
            expectedMaxCredit: decimal.Zero,
            shouldError:       true,
        },
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

## Monitoring and Metrics

### Key Metrics to Track

1. **Credit Note Volume by Type**

   - Daily/Weekly adjustment vs refund credit notes
   - Average credit note amounts

2. **Invoice Amount Accuracy**

   - Validation that amount_due calculations are correct
   - Monitoring for negative amounts or inconsistencies

3. **Customer Balance Integration**

   - Successful wallet credit operations
   - Balance reapplication success rate

4. **Performance Metrics**
   - Credit note creation time
   - Database query performance for amount calculations

### Alerts

```go
// Alert when credit note amounts seem excessive
if creditNote.Total.GreaterThan(invoice.Total.Mul(decimal.NewFromFloat(0.5))) {
    s.Logger.Warnw("Large credit note created",
        "credit_note_id", creditNote.ID,
        "credit_amount", creditNote.Total,
        "invoice_total", invoice.Total,
        "percentage", creditNote.Total.Div(invoice.Total).Mul(decimal.NewFromInt(100)))
}

// Alert on calculation inconsistencies
if invoice.AmountRemaining.IsNegative() {
    s.Logger.Errorw("Negative amount_remaining detected",
        "invoice_id", invoice.ID,
        "amount_due", invoice.AmountDue,
        "amount_paid", invoice.AmountPaid,
        "amount_remaining", invoice.AmountRemaining)
}
```

This comprehensive implementation ensures that credit notes work correctly across all payment status scenarios while maintaining data consistency and providing proper audit trails.
