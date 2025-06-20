# Credit Note Functionality PRD

## Executive Summary

This PRD outlines the implementation of comprehensive credit note functionality for FlexPrice, enabling customers to issue credits for duplicate, fraudulent, or erroneous charges, handle order changes, and manage subscription cancellations with proper audit trails and customer balance integration.

## Problem Statement

FlexPrice currently lacks:

- Ability to issue credits for billing errors or disputes
- Mechanism for handling subscription cancellations with pro-rated refunds
- System for managing order changes requiring partial credits
- Proper audit trail for invoice adjustments
- Integration with customer balance for credit management

## Solution Overview

Implement a comprehensive credit note system with two primary types:

1. **Adjustment Credit Notes**: For unpaid invoices (reduces amount due)
2. **Refund Credit Notes**: For paid invoices (adds to customer balance)

## Core Requirements

### Functional Requirements

#### CR-001: Credit Note Creation

- System MUST allow creating credit notes for eligible invoices
- System MUST support line-item level credits with partial amounts
- System MUST validate credit amounts don't exceed available creditable amounts
- System MUST generate unique credit note numbers

#### CR-002: Credit Note Types

- System MUST determine credit note type based on invoice payment status
- System MUST apply adjustment credits to invoice amounts
- System MUST apply refund credits to customer wallet balance

#### CR-003: Invoice Eligibility

- System MUST only allow credit notes for FINALIZED or PAID invoices
- System MUST prevent credit notes for externally synced invoices
- System MUST validate invoice status before credit note creation

#### CR-004: Customer Balance Integration

- System MUST integrate with existing wallet system for refund credits
- System MUST handle complex balance interactions for adjustment credits
- System MUST maintain balance consistency across operations

### Business Rules

#### BR-001: Credit Limits

- Multiple credit notes MAY be applied to single invoice up to invoice total
- Credit note amounts MUST NOT exceed remaining creditable amount per line item
- Total credit notes for invoice MUST NOT exceed original invoice amount

#### BR-002: Status Transitions

- Credit notes MUST start in DRAFT status
- Credit notes MUST transition to ISSUED when applied
- Only adjustment credit notes MAY be voided
- Refund credit notes CANNOT be voided once issued

#### BR-003: Customer Balance Handling

- Adjustment credits MUST revert existing balance applications
- Adjustment credits MUST reapply available balance after credit application
- Refund credits MUST add amounts to appropriate currency wallet

## Overview

This PRD outlines the implementation of a comprehensive credit note functionality for FlexPrice, similar to what's offered by Stripe, Orb, Togai, and Lago. Credit notes are essential documents used to decrease the amount due or issue a credit for already issued invoices.

## Goals

### Primary Goals

1. **Invoice Adjustments**: Enable adjustment credit notes for issued invoices (not yet paid)
2. **Customer Balance Credits**: Enable refund credit notes for paid invoices that add to customer balance
3. **Line Item Granularity**: Allow credit notes at the line item level with partial amounts
4. **Audit Trail**: Maintain complete audit trail of all credit note operations
5. **Customer Balance Integration**: Seamlessly integrate with existing wallet/customer balance system

### Secondary Goals

1. **PDF Generation**: Generate PDF documents for credit notes
2. **Email Notifications**: Send email notifications for credit note creation/voiding
3. **API Compatibility**: Maintain API compatibility with industry standards
4. **Multi-currency Support**: Support credit notes across different currencies
5. **Webhook Events**: Emit webhook events for credit note operations

## Use Cases

### 1. Duplicate Invoice Credit

**Scenario**: Customer was charged twice for the same service

- **Invoice Status**: FINALIZED or PAID
- **Action**: Issue adjustment credit note (if unpaid) or refund credit note (if paid)
- **Result**: Reduce amount due or add to customer balance

### 2. Service Quality Issues

**Scenario**: Customer complains about service quality and requests partial refund

- **Invoice Status**: FINALIZED or PAID
- **Action**: Issue partial credit note for specific line items
- **Result**: Partial credit applied to invoice or customer balance

### 3. Subscription Cancellation

**Scenario**: Customer cancels mid-billing period and deserves pro-rated credit

- **Invoice Status**: PAID
- **Action**: Automatically generate refund credit note for unused period
- **Result**: Add pro-rated amount to customer balance

### 4. Order Changes

**Scenario**: Customer reduces quantity or cancels part of their order

- **Invoice Status**: FINALIZED (not yet paid)
- **Action**: Issue adjustment credit note to reduce amount due
- **Result**: Invoice amount due is reduced

### 5. Billing Error Correction

**Scenario**: Wrong pricing or meter readings resulted in overcharging

- **Invoice Status**: Any eligible status
- **Action**: Issue credit note for the difference
- **Result**: Correct the billing amount

## Core Concepts

### Credit Note Types

#### 1. Adjustment Credit Note

- **Applied to**: Invoices with status `FINALIZED` (not yet paid)
- **Effect**: Reduces the invoice's `amount_due` and `amount_remaining`
- **Customer Balance**: No direct impact on customer balance
- **PDF**: Regenerates invoice PDF to show the credit note adjustment

#### 2. Refund Credit Note

- **Applied to**: Invoices with status `PAID` (payment_status = SUCCEEDED)
- **Effect**: Adds credit amount to customer's wallet balance
- **Invoice Impact**: No change to invoice amounts (already paid)
- **PDF**: Separate credit note PDF generated

### Credit Note Status

```go
type CreditNoteStatus string

const (
    CreditNoteStatusDraft     CreditNoteStatus = "DRAFT"
    CreditNoteStatusIssued    CreditNoteStatus = "ISSUED"
    CreditNoteStatusVoided    CreditNoteStatus = "VOIDED"
)
```

### Credit Note Reasons

```go
type CreditNoteReason string

const (
    CreditNoteReasonDuplicate      CreditNoteReason = "DUPLICATE"
    CreditNoteReasonFraudulent     CreditNoteReason = "FRAUDULENT"
    CreditNoteReasonOrderChange    CreditNoteReason = "ORDER_CHANGE"
    CreditNoteReasonUnsatisfactory CreditNoteReason = "UNSATISFACTORY"
    CreditNoteReasonService        CreditNoteReason = "SERVICE_ISSUE"
    CreditNoteReasonBillingError   CreditNoteReason = "BILLING_ERROR"
    CreditNoteReasonGoodwill       CreditNoteReason = "GOODWILL"
    CreditNoteReasonSubscriptionCancellation CreditNoteReason = "SUBSCRIPTION_CANCELLATION"
)
```

### Credit Note Type

```go
type CreditNoteType string

const (
    CreditNoteTypeAdjustment CreditNoteType = "ADJUSTMENT"
    CreditNoteTypeRefund     CreditNoteType = "REFUND"
)
```

## System Architecture

### Database Schema

#### Invoice Table Updates

First, we need to update the existing invoice table to support credit note calculations:

```sql
-- Add new fields to invoices table to support credit note functionality
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS subtotal NUMERIC(20,8) DEFAULT 0;
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS total NUMERIC(20,8) DEFAULT 0;
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS amount_due NUMERIC(20,8) DEFAULT 0;
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS amount_paid NUMERIC(20,8) DEFAULT 0;
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS amount_remaining NUMERIC(20,8) DEFAULT 0;

-- Create index for efficient credit note amount calculations
CREATE INDEX IF NOT EXISTS idx_invoices_amounts ON invoices(tenant_id, environment_id, amount_due, amount_remaining);
```

**Invoice Field Definitions:**

- `subtotal`: Sum of all line items amount (before tax and discount)
- `total`: Sum of all line items amount + tax - discount
- `amount_due`: total - sum of all adjustment credit notes amount
- `amount_paid`: Sum of all payments made against the invoice
- `amount_remaining`: amount_due - amount_paid

#### Credit Notes Table

```sql
CREATE TABLE credit_notes (
    id VARCHAR(50) PRIMARY KEY,
    tenant_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) DEFAULT 'published',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    created_by VARCHAR(50),
    updated_by VARCHAR(50),
    environment_id VARCHAR(50) DEFAULT '',

    -- Credit Note Specific Fields
    credit_note_number VARCHAR(50),
    invoice_id VARCHAR(50) NOT NULL REFERENCES invoices(id),
    customer_id VARCHAR(50) NOT NULL,
    credit_note_status VARCHAR(50) DEFAULT 'DRAFT',
    credit_note_type VARCHAR(50) NOT NULL, -- ADJUSTMENT, REFUND
    reason VARCHAR(50) NOT NULL,
    memo TEXT,

    -- Amounts
    currency VARCHAR(10) NOT NULL,
    subtotal NUMERIC(20,8) DEFAULT 0,
    total NUMERIC(20,8) DEFAULT 0,
    minimum_amount_refunded NUMERIC(20,8) DEFAULT 0,

    -- Dates
    issued_at TIMESTAMP,
    voided_at TIMESTAMP,

    -- PDF and External
    credit_note_pdf_url VARCHAR(500),
    external_sync_status VARCHAR(50) DEFAULT 'NOT_SYNCED',

    -- Metadata
    metadata JSONB,
    version INTEGER DEFAULT 1,

    -- Indexes
    INDEX idx_credit_notes_tenant_env_invoice (tenant_id, environment_id, invoice_id),
    INDEX idx_credit_notes_tenant_env_customer (tenant_id, environment_id, customer_id),
    INDEX idx_credit_notes_tenant_env_status (tenant_id, environment_id, credit_note_status),
    INDEX idx_credit_notes_number_unique (tenant_id, environment_id, credit_note_number) UNIQUE
);
```

#### Credit Note Line Items Table

```sql
CREATE TABLE credit_note_line_items (
    id VARCHAR(50) PRIMARY KEY,
    tenant_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) DEFAULT 'published',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    created_by VARCHAR(50),
    updated_by VARCHAR(50),
    environment_id VARCHAR(50) DEFAULT '',

    -- Relationships
    credit_note_id VARCHAR(50) NOT NULL REFERENCES credit_notes(id),
    invoice_line_item_id VARCHAR(50) NOT NULL REFERENCES invoice_line_items(id),

    -- Line Item Details
    name VARCHAR(255),
    amount NUMERIC(20,8) NOT NULL,
    quantity NUMERIC(20,8) DEFAULT 0,
    currency VARCHAR(10) NOT NULL,

    -- Tax Information
    tax_amounts JSONB, -- Array of tax amounts

    -- Item Reference
    item_id VARCHAR(50), -- Generic reference to price/product

    -- Metadata
    metadata JSONB,

    INDEX idx_credit_note_line_items_credit_note (credit_note_id),
    INDEX idx_credit_note_line_items_invoice_line_item (invoice_line_item_id)
);
```

### Domain Models

#### Updated Invoice Model

```go
// Invoice represents an updated invoice domain model with credit note support
type Invoice struct {
    ID                    string                 `json:"id"`
    InvoiceNumber         *string                `json:"invoice_number"`
    CustomerID            string                 `json:"customer_id"`
    InvoiceStatus         types.InvoiceStatus    `json:"invoice_status"`
    PaymentStatus         types.PaymentStatus    `json:"payment_status"`

    // Enhanced Amount Fields for Credit Note Support
    Subtotal              decimal.Decimal        `json:"subtotal"`          // Sum of all line items amount
    Total                 decimal.Decimal        `json:"total"`             // Subtotal + tax - discount
    AmountDue             decimal.Decimal        `json:"amount_due"`        // Total - sum of adjustment credit notes
    AmountPaid            decimal.Decimal        `json:"amount_paid"`       // Sum of all payments
    AmountRemaining       decimal.Decimal        `json:"amount_remaining"`  // AmountDue - AmountPaid

    // Legacy fields (for backward compatibility)
    Amount                decimal.Decimal        `json:"amount"`            // Deprecated: use Total instead
    Currency              string                 `json:"currency"`

    // Dates
    IssuedAt              *time.Time             `json:"issued_at,omitempty"`
    DueDate               *time.Time             `json:"due_date,omitempty"`

    // Related entities
    LineItems             []*InvoiceLineItem     `json:"line_items,omitempty"`
    CreditNotes           []*CreditNote          `json:"credit_notes,omitempty"`
    Payments              []*Payment             `json:"payments,omitempty"`

    // Common fields
    Metadata              types.Metadata         `json:"metadata,omitempty"`
    Version               int                    `json:"version"`
    EnvironmentID         string                 `json:"environment_id"`
    types.BaseModel
}
```

#### Credit Note Model

```go
// CreditNote represents a credit note domain model
type CreditNote struct {
    ID                    string                 `json:"id"`
    CreditNoteNumber      *string                `json:"credit_note_number"`
    InvoiceID             string                 `json:"invoice_id"`
    CustomerID            string                 `json:"customer_id"`
    CreditNoteStatus      types.CreditNoteStatus `json:"credit_note_status"`
    CreditNoteType        types.CreditNoteType   `json:"credit_note_type"`
    Reason                types.CreditNoteReason `json:"reason"`
    Memo                  string                 `json:"memo,omitempty"`

    // Amounts
    Currency              string                 `json:"currency"`
    Subtotal              decimal.Decimal        `json:"subtotal"`
    Total                 decimal.Decimal        `json:"total"`
    MinimumAmountRefunded decimal.Decimal        `json:"minimum_amount_refunded"`

    // Dates
    IssuedAt              *time.Time             `json:"issued_at,omitempty"`
    VoidedAt              *time.Time             `json:"voided_at,omitempty"`

    // PDF and Sync
    CreditNotePDFURL      *string                `json:"credit_note_pdf_url,omitempty"`
    ExternalSyncStatus    string                 `json:"external_sync_status"`

    // Related entities
    Invoice               *Invoice               `json:"invoice,omitempty"`
    LineItems             []*CreditNoteLineItem  `json:"line_items,omitempty"`

    // Common fields
    Metadata              types.Metadata         `json:"metadata,omitempty"`
    Version               int                    `json:"version"`
    EnvironmentID         string                 `json:"environment_id"`
    types.BaseModel
}
```

#### Credit Note Line Item Model

```go
// CreditNoteLineItem represents a line item in a credit note
type CreditNoteLineItem struct {
    ID                   string                      `json:"id"`
    CreditNoteID         string                      `json:"credit_note_id"`
    InvoiceLineItemID    string                      `json:"invoice_line_item_id"`
    Name                 string                      `json:"name"`
    Amount               decimal.Decimal             `json:"amount"`
    Quantity             decimal.Decimal             `json:"quantity"`
    Currency             string                      `json:"currency"`
    TaxAmounts           []TaxAmount                 `json:"tax_amounts,omitempty"`
    ItemID               *string                     `json:"item_id,omitempty"`

    // Related entities
    InvoiceLineItem      *InvoiceLineItem            `json:"invoice_line_item,omitempty"`

    // Common fields
    Metadata             types.Metadata              `json:"metadata,omitempty"`
    EnvironmentID        string                      `json:"environment_id"`
    types.BaseModel
}

// TaxAmount represents tax information for a line item
type TaxAmount struct {
    TaxRateDescription  string          `json:"tax_rate_description"`
    TaxRatePercentage   string          `json:"tax_rate_percentage"`
    Amount              decimal.Decimal `json:"amount"`
}
```

## API Design

### Create Credit Note

```
POST /v1/credit_notes
```

#### Request Body

```json
{
  "line_items": [
    {
      "invoice_line_item_id": "4khy3nwzktxv7",
      "amount": "100.00",
      "quantity": "1.0"
    }
  ],
  "reason": "duplicate",
  "memo": "An optional memo for my credit note."
}
```

#### Response

```json
{
  "id": "cn_1234567890",
  "created_at": "2023-11-07T05:31:56Z",
  "voided_at": null,
  "credit_note_number": "CN-202311-00001",
  "invoice_id": "inv_1234567890",
  "memo": "An optional memo for my credit note.",
  "reason": "duplicate",
  "type": "adjustment",
  "subtotal": "100.00",
  "total": "100.00",
  "customer": {
    "id": "cust_1234567890",
    "external_customer_id": "ext_cust_123"
  },
  "credit_note_pdf": "https://files.flexprice.com/credit_notes/cn_1234567890.pdf",
  "minimum_amount_refunded": "0.00",
  "discounts": [],
  "maximum_amount_adjustment": null,
  "invoice": {
    "id": "inv_1234567890",
    "invoice_number": "INV-202311-00001",
    "subtotal": "1000.00",
    "total": "1200.00",
    "amount_due": "1100.00",
    "amount_paid": "0.00",
    "amount_remaining": "1100.00",
    "currency": "USD",
    "invoice_status": "finalized",
    "payment_status": "pending"
  },
  "line_items": [
    {
      "id": "cnli_1234567890",
      "name": "API Calls",
      "subtotal": "100.00",
      "amount": "100.00",
      "quantity": 1,
      "discounts": [],
      "tax_amounts": [
        {
          "tax_rate_description": "VAT",
          "tax_rate_percentage": "20.0",
          "amount": "20.00"
        }
      ],
      "item_id": "price_123"
    }
  ]
}
```

#### Updated Invoice API Response

All invoice API responses now include the enhanced amount fields:

```json
{
  "id": "inv_1234567890",
  "invoice_number": "INV-202311-00001",
  "customer_id": "cust_1234567890",
  "invoice_status": "finalized",
  "payment_status": "pending",

  "subtotal": "1000.00",
  "total": "1200.00",
  "amount_due": "1100.00",
  "amount_paid": "0.00",
  "amount_remaining": "1100.00",

  "amount": "1200.00",
  "currency": "USD",
  "issued_at": "2023-11-07T05:30:00Z",
  "due_date": "2023-12-07T05:30:00Z",

  "line_items": [...],
  "credit_notes": [
    {
      "id": "cn_1234567890",
      "credit_note_number": "CN-202311-00001",
      "type": "adjustment",
      "total": "100.00",
      "issued_at": "2023-11-07T05:31:56Z",
      "status": "issued"
    }
  ],
  "payments": [...]
}
```

### List Credit Notes

```
GET /v1/credit_notes?invoice_id={invoice_id}&customer_id={customer_id}&status={status}
```

### Get Credit Note

```
GET /v1/credit_notes/{id}
```

### Void Credit Note

```
POST /v1/credit_notes/{id}/void
```

#### Request Body

```json
{
  "reason": "Applied incorrectly"
}
```

## Business Logic

### Invoice Amount Calculation Service

Before implementing credit notes, we need a robust invoice amount calculation service:

```go
// InvoiceAmountCalculator handles all invoice amount calculations
type InvoiceAmountCalculator struct {
    InvoiceRepo           repository.InvoiceRepository
    CreditNoteRepo        repository.CreditNoteRepository
    PaymentRepo           repository.PaymentRepository
}

// RecalculateInvoiceAmounts recalculates all amount fields for an invoice
func (calc *InvoiceAmountCalculator) RecalculateInvoiceAmounts(ctx context.Context, invoiceID string) (*Invoice, error) {
    invoice, err := calc.InvoiceRepo.Get(ctx, invoiceID)
    if err != nil {
        return nil, err
    }

    // 1. Calculate subtotal from line items
    invoice.Subtotal = calc.calculateSubtotal(ctx, invoice.LineItems)

    // 2. Calculate total (subtotal + tax - discount)
    invoice.Total = calc.calculateTotal(ctx, invoice)

    // 3. Calculate amount_due (total - adjustment credit notes)
    adjustmentCreditTotal, err := calc.getAdjustmentCreditNotesTotal(ctx, invoiceID)
    if err != nil {
        return nil, err
    }
    invoice.AmountDue = invoice.Total.Sub(adjustmentCreditTotal)

    // 4. Calculate amount_paid from payments
    paidAmount, err := calc.getSuccessfulPaymentsTotal(ctx, invoiceID)
    if err != nil {
        return nil, err
    }
    invoice.AmountPaid = paidAmount

    // 5. Calculate amount_remaining
    invoice.AmountRemaining = invoice.AmountDue.Sub(invoice.AmountPaid)

    return invoice, nil
}

func (calc *InvoiceAmountCalculator) calculateSubtotal(ctx context.Context, lineItems []*InvoiceLineItem) decimal.Decimal {
    subtotal := decimal.Zero
    for _, item := range lineItems {
        subtotal = subtotal.Add(item.Amount)
    }
    return subtotal
}

func (calc *InvoiceAmountCalculator) calculateTotal(ctx context.Context, invoice *Invoice) decimal.Decimal {
    // Start with subtotal
    total := invoice.Subtotal

    // Add tax amounts
    for _, lineItem := range invoice.LineItems {
        for _, taxAmount := range lineItem.TaxAmounts {
            total = total.Add(taxAmount.Amount)
        }
    }

    // Subtract discounts (if applicable)
    // TODO: Add discount calculation when discount feature is implemented

    return total
}

func (calc *InvoiceAmountCalculator) getAdjustmentCreditNotesTotal(ctx context.Context, invoiceID string) (decimal.Decimal, error) {
    creditNotes, err := calc.CreditNoteRepo.GetByInvoiceID(ctx, invoiceID)
    if err != nil {
        return decimal.Zero, err
    }

    total := decimal.Zero
    for _, cn := range creditNotes {
        if cn.CreditNoteType == types.CreditNoteTypeAdjustment &&
           cn.CreditNoteStatus == types.CreditNoteStatusIssued {
            total = total.Add(cn.Total)
        }
    }
    return total, nil
}

func (calc *InvoiceAmountCalculator) getSuccessfulPaymentsTotal(ctx context.Context, invoiceID string) (decimal.Decimal, error) {
    payments, err := calc.PaymentRepo.GetByInvoiceID(ctx, invoiceID)
    if err != nil {
        return decimal.Zero, err
    }

    total := decimal.Zero
    for _, payment := range payments {
        if payment.PaymentStatus == types.PaymentStatusSucceeded {
            total = total.Add(payment.Amount)
        }
    }
    return total, nil
}
```

### Credit Note Creation Flow

#### 1. Validation Phase

```go
func (s *creditNoteService) ValidateCreditNoteCreation(ctx context.Context, req *CreateCreditNoteRequest) error {
    // 1. Validate invoice exists and is eligible
    invoice, err := s.InvoiceRepo.Get(ctx, req.InvoiceID)
    if err != nil {
        return err
    }

    // 2. Check invoice status eligibility
    eligibleStatuses := []types.InvoiceStatus{
        types.InvoiceStatusFinalized,
    }
    if invoice.PaymentStatus == types.PaymentStatusSucceeded {
        // For paid invoices, we can issue refund credit notes
        eligibleStatuses = append(eligibleStatuses, types.InvoiceStatusFinalized)
    }

    if !contains(eligibleStatuses, invoice.InvoiceStatus) {
        return ierr.NewError("invoice not eligible for credit note")
    }

    // 3. Check if invoice is synced to external provider
    if invoice.ExternalSyncStatus == "SYNCED" {
        return ierr.NewError("cannot issue credit note for externally synced invoice")
    }

    // 4. Validate line items exist and amounts are valid
    totalCreditAmount := decimal.Zero
    for _, lineItem := range req.LineItems {
        invoiceLineItem, err := s.InvoiceLineItemRepo.Get(ctx, lineItem.InvoiceLineItemID)
        if err != nil {
            return err
        }

        if invoiceLineItem.InvoiceID != invoice.ID {
            return ierr.NewError("line item does not belong to specified invoice")
        }

        // Check if amount exceeds what's available for credit
        maxCreditableAmount := s.getMaxCreditableAmount(ctx, invoiceLineItem.ID)
        if lineItem.Amount.GreaterThan(maxCreditableAmount) {
            return ierr.NewError("credit amount exceeds maximum creditable amount")
        }

        totalCreditAmount = totalCreditAmount.Add(lineItem.Amount)
    }

    // 5. Validate total credit amount doesn't exceed available creditable amount
    maxInvoiceCredit := s.getMaxInvoiceCreditAmount(ctx, invoice.ID)
    if totalCreditAmount.GreaterThan(maxInvoiceCredit) {
        return ierr.NewError("total credit amount exceeds maximum creditable for invoice")
    }

    return nil
}
```

#### 2. Credit Note Type Determination

```go
func (s *creditNoteService) determineCreditNoteType(ctx context.Context, invoice *Invoice) types.CreditNoteType {
    if invoice.PaymentStatus == types.PaymentStatusSucceeded {
        return types.CreditNoteTypeRefund
    }
    return types.CreditNoteTypeAdjustment
}
```

#### 3. Credit Note Creation

```go
func (s *creditNoteService) CreateCreditNote(ctx context.Context, req *CreateCreditNoteRequest) (*CreditNote, error) {
    return s.DB.WithTx(ctx, func(ctx context.Context) (*CreditNote, error) {
        // 1. Validate request
        if err := s.ValidateCreditNoteCreation(ctx, req); err != nil {
            return nil, err
        }

        // 2. Get invoice
        invoice, err := s.InvoiceRepo.Get(ctx, req.InvoiceID)
        if err != nil {
            return nil, err
        }

        // 3. Determine credit note type
        creditNoteType := s.determineCreditNoteType(ctx, invoice)

        // 4. Generate credit note number
        creditNoteNumber, err := s.generateCreditNoteNumber(ctx)
        if err != nil {
            return nil, err
        }

        // 5. Create credit note
        creditNote := &CreditNote{
            ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE),
            CreditNoteNumber: &creditNoteNumber,
            InvoiceID:        invoice.ID,
            CustomerID:       invoice.CustomerID,
            CreditNoteStatus: types.CreditNoteStatusDraft,
            CreditNoteType:   creditNoteType,
            Reason:           req.Reason,
            Memo:             req.Memo,
            Currency:         invoice.Currency,
            EnvironmentID:    invoice.EnvironmentID,
            BaseModel:        types.GetDefaultBaseModel(ctx),
        }

        // 6. Process line items
        var totalAmount decimal.Decimal
        for _, lineItemReq := range req.LineItems {
            lineItem := &CreditNoteLineItem{
                ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_NOTE_LINE_ITEM),
                CreditNoteID:      creditNote.ID,
                InvoiceLineItemID: lineItemReq.InvoiceLineItemID,
                Amount:            lineItemReq.Amount,
                Quantity:          lineItemReq.Quantity,
                Currency:          invoice.Currency,
                EnvironmentID:     invoice.EnvironmentID,
                BaseModel:         types.GetDefaultBaseModel(ctx),
            }

            // Calculate tax if applicable
            if err := s.calculateLineTax(ctx, lineItem, invoice); err != nil {
                return nil, err
            }

            creditNote.LineItems = append(creditNote.LineItems, lineItem)
            totalAmount = totalAmount.Add(lineItem.Amount)
        }

        creditNote.Subtotal = totalAmount
        creditNote.Total = totalAmount // Add tax calculation here if needed

        // 7. Save credit note
        if err := s.CreditNoteRepo.Create(ctx, creditNote); err != nil {
            return nil, err
        }

        // 8. Issue the credit note (transition to ISSUED status)
        if err := s.issueCreditNote(ctx, creditNote); err != nil {
            return nil, err
        }

        return creditNote, nil
    })
}
```

#### 4. Credit Note Issuance

```go
func (s *creditNoteService) issueCreditNote(ctx context.Context, creditNote *CreditNote) error {
    return s.DB.WithTx(ctx, func(ctx context.Context) error {
        // 1. Transition status
        now := time.Now().UTC()
        creditNote.CreditNoteStatus = types.CreditNoteStatusIssued
        creditNote.IssuedAt = &now

        // 2. Update credit note in database first
        if err := s.CreditNoteRepo.Update(ctx, creditNote); err != nil {
            return err
        }

        // 3. Apply credit based on type and update invoice amounts
        if creditNote.CreditNoteType == types.CreditNoteTypeAdjustment {
            if err := s.applyAdjustmentCreditWithInvoiceUpdate(ctx, creditNote); err != nil {
                return err
            }
        } else if creditNote.CreditNoteType == types.CreditNoteTypeRefund {
            if err := s.applyRefundCredit(ctx, creditNote); err != nil {
                return err
            }
        }

        return nil
    })

    // 4. Generate PDF (outside transaction to avoid long-running ops in tx)
    if err := s.generateCreditNotePDF(ctx, creditNote); err != nil {
        s.Logger.Errorw("Failed to generate credit note PDF", "error", err, "credit_note_id", creditNote.ID)
        // Don't fail the entire operation for PDF generation failure
    }

    // 5. Send notifications
    go s.sendCreditNoteNotification(context.Background(), creditNote)

    // 6. Emit webhook event
    s.publishWebhookEvent(ctx, types.WebhookEventCreditNoteIssued, creditNote.ID)

    return nil
}
```

### Customer Balance Integration

#### Adjustment Credit Notes

```go
func (s *creditNoteService) applyAdjustmentCreditWithInvoiceUpdate(ctx context.Context, creditNote *CreditNote) error {
    // Use the invoice amount calculator to properly recalculate all amounts
    invoice, err := s.InvoiceAmountCalculator.RecalculateInvoiceAmounts(ctx, creditNote.InvoiceID)
    if err != nil {
        return err
    }

    // Handle customer balance re-application for adjustment credit notes
    // When we reduce amount_due via credit note, we may need to reapply customer balance
    if err := s.reapplyCustomerBalanceAfterAdjustment(ctx, invoice); err != nil {
        return err
    }

    // Update invoice with recalculated amounts
    if err := s.InvoiceRepo.Update(ctx, invoice); err != nil {
        return err
    }

    // 5. Regenerate invoice PDF with credit note
    go s.regenerateInvoicePDFWithCredit(context.Background(), invoice.ID)

    return nil
}

func (s *creditNoteService) reapplyCustomerBalanceAfterAdjustment(ctx context.Context, invoice *Invoice) error {
    // Get available customer balance for this currency
    availableBalance := s.getCustomerBalanceForCurrency(ctx, invoice.CustomerID, invoice.Currency)

    if availableBalance.LessThanOrEqual(decimal.Zero) {
        return nil // No balance to apply
    }

    // Calculate how much additional balance can be applied
    currentUnpaidAmount := invoice.AmountRemaining
    maxApplicableBalance := decimal.Min(availableBalance, currentUnpaidAmount)

    if maxApplicableBalance.GreaterThan(decimal.Zero) {
        // Apply additional balance
        if err := s.deductFromCustomerBalance(ctx, invoice.CustomerID, maxApplicableBalance, invoice.Currency); err != nil {
            return err
        }

        // Update invoice amounts
        invoice.AmountPaid = invoice.AmountPaid.Add(maxApplicableBalance)
        invoice.AmountRemaining = invoice.AmountDue.Sub(invoice.AmountPaid)

        // Create wallet transaction record for this balance application
        if err := s.recordBalanceApplication(ctx, invoice.ID, maxApplicableBalance, invoice.Currency); err != nil {
            return err
        }
    }

    return nil
}
```

#### Refund Credit Notes

```go
func (s *creditNoteService) applyRefundCredit(ctx context.Context, creditNote *CreditNote) error {
    // For refund credit notes, add amount to customer balance
    return s.addToCustomerBalance(ctx, creditNote.CustomerID, creditNote.Total, creditNote.Currency)
}

func (s *creditNoteService) addToCustomerBalance(ctx context.Context, customerID string, amount decimal.Decimal, currency string) error {
    // Get customer's wallet for this currency
    wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
    if err != nil {
        return err
    }

    var targetWallet *wallet.Wallet
    for _, w := range wallets {
        if w.Currency == currency && w.WalletStatus == types.WalletStatusActive {
            targetWallet = w
            break
        }
    }

    if targetWallet == nil {
        // Create a new wallet for this currency
        targetWallet = &wallet.Wallet{
            ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET),
            CustomerID:     customerID,
            Currency:       currency,
            WalletStatus:   types.WalletStatusActive,
            WalletType:     types.WalletTypePrePaid,
            ConversionRate: decimal.NewFromInt(1),
            EnvironmentID:  types.GetEnvironmentID(ctx),
            BaseModel:      types.GetDefaultBaseModel(ctx),
        }

        if err := s.WalletRepo.CreateWallet(ctx, targetWallet); err != nil {
            return err
        }
    }

    // Add credit to wallet
    creditOp := &wallet.WalletOperation{
        WalletID:          targetWallet.ID,
        Type:              types.TransactionTypeCredit,
        CreditAmount:      amount,
        Description:       fmt.Sprintf("Credit note refund: %s", creditNote.CreditNoteNumber),
        TransactionReason: types.TransactionReasonCreditNote,
        ReferenceType:     types.WalletTxReferenceTypeCreditNote,
        ReferenceID:       creditNote.ID,
    }

    return s.WalletService.CreditWallet(ctx, creditOp)
}
```

### Credit Note Voiding

```go
func (s *creditNoteService) VoidCreditNote(ctx context.Context, id string, reason string) error {
    return s.DB.WithTx(ctx, func(ctx context.Context) error {
        // 1. Get credit note
        creditNote, err := s.CreditNoteRepo.Get(ctx, id)
        if err != nil {
            return err
        }

        // 2. Validate credit note can be voided
        if creditNote.CreditNoteStatus != types.CreditNoteStatusIssued {
            return ierr.NewError("credit note must be issued to be voided")
        }

        if creditNote.CreditNoteType == types.CreditNoteTypeRefund {
            return ierr.NewError("refund credit notes cannot be voided")
        }

        // 3. Update credit note status first
        now := time.Now().UTC()
        creditNote.CreditNoteStatus = types.CreditNoteStatusVoided
        creditNote.VoidedAt = &now
        creditNote.Metadata["void_reason"] = reason

        if err := s.CreditNoteRepo.Update(ctx, creditNote); err != nil {
            return err
        }

        // 4. Recalculate invoice amounts after voiding
        if creditNote.CreditNoteType == types.CreditNoteTypeAdjustment {
            invoice, err := s.InvoiceAmountCalculator.RecalculateInvoiceAmounts(ctx, creditNote.InvoiceID)
            if err != nil {
                return err
            }

            // Reapply customer balance after recalculation
            if err := s.reapplyCustomerBalanceAfterAdjustment(ctx, invoice); err != nil {
                return err
            }

            // Update invoice with recalculated amounts
            if err := s.InvoiceRepo.Update(ctx, invoice); err != nil {
                return err
            }
        }

        return nil
    })

    // 5. Emit webhook event (outside transaction)
    s.publishWebhookEvent(ctx, types.WebhookEventCreditNoteVoided, id)

    return nil
}
```

### Transaction Management and Data Consistency

#### Critical Transaction Requirements

All operations that affect invoice amounts MUST be performed within database transactions to ensure data consistency:

```go
// InvoiceTransactionManager handles all operations that affect invoice amounts
type InvoiceTransactionManager struct {
    DB                    database.DB
    InvoiceRepo           repository.InvoiceRepository
    CreditNoteRepo        repository.CreditNoteRepository
    PaymentRepo           repository.PaymentRepository
    WalletService         service.WalletService
    AmountCalculator      *InvoiceAmountCalculator
}

// ExecuteInvoiceAmountUpdate executes operations that update invoice amounts within a transaction
func (tm *InvoiceTransactionManager) ExecuteInvoiceAmountUpdate(ctx context.Context, invoiceID string, operation func(context.Context) error) error {
    return tm.DB.WithTx(ctx, func(txCtx context.Context) error {
        // 1. Execute the operation (credit note creation, payment, etc.)
        if err := operation(txCtx); err != nil {
            return err
        }

        // 2. Recalculate all invoice amounts
        invoice, err := tm.AmountCalculator.RecalculateInvoiceAmounts(txCtx, invoiceID)
        if err != nil {
            return err
        }

        // 3. Update invoice with new amounts
        if err := tm.InvoiceRepo.Update(txCtx, invoice); err != nil {
            return err
        }

        // 4. Validate data consistency
        if err := tm.validateInvoiceAmountConsistency(txCtx, invoice); err != nil {
            return ierr.NewError("invoice amount consistency check failed: %v", err)
        }

        return nil
    })
}

// validateInvoiceAmountConsistency ensures all amount fields are mathematically correct
func (tm *InvoiceTransactionManager) validateInvoiceAmountConsistency(ctx context.Context, invoice *Invoice) error {
    // Validate: amount_due = total - sum(adjustment_credit_notes)
    adjustmentCredits, err := tm.AmountCalculator.getAdjustmentCreditNotesTotal(ctx, invoice.ID)
    if err != nil {
        return err
    }

    expectedAmountDue := invoice.Total.Sub(adjustmentCredits)
    if !invoice.AmountDue.Equal(expectedAmountDue) {
        return fmt.Errorf("amount_due mismatch: expected %s, got %s", expectedAmountDue, invoice.AmountDue)
    }

    // Validate: amount_remaining = amount_due - amount_paid
    expectedAmountRemaining := invoice.AmountDue.Sub(invoice.AmountPaid)
    if !invoice.AmountRemaining.Equal(expectedAmountRemaining) {
        return fmt.Errorf("amount_remaining mismatch: expected %s, got %s", expectedAmountRemaining, invoice.AmountRemaining)
    }

    // Validate: amount_paid matches sum of successful payments
    actualPayments, err := tm.AmountCalculator.getSuccessfulPaymentsTotal(ctx, invoice.ID)
    if err != nil {
        return err
    }

    if !invoice.AmountPaid.Equal(actualPayments) {
        return fmt.Errorf("amount_paid mismatch: expected %s, got %s", actualPayments, invoice.AmountPaid)
    }

    return nil
}
```

#### Concurrent Operation Handling

```go
// HandleConcurrentInvoiceOperations prevents race conditions when multiple operations affect the same invoice
func (tm *InvoiceTransactionManager) HandleConcurrentInvoiceOperations(ctx context.Context, invoiceID string, operation func(context.Context) error) error {
    // Use row-level locking to prevent concurrent modifications
    return tm.DB.WithTx(ctx, func(txCtx context.Context) error {
        // Lock the invoice row for update
        invoice, err := tm.InvoiceRepo.GetForUpdate(txCtx, invoiceID)
        if err != nil {
            return err
        }

        // Store original version for optimistic locking
        originalVersion := invoice.Version

        // Execute the operation
        if err := operation(txCtx); err != nil {
            return err
        }

        // Recalculate amounts
        updatedInvoice, err := tm.AmountCalculator.RecalculateInvoiceAmounts(txCtx, invoiceID)
        if err != nil {
            return err
        }

        // Increment version for optimistic locking
        updatedInvoice.Version = originalVersion + 1

        // Update with version check
        if err := tm.InvoiceRepo.UpdateWithVersionCheck(txCtx, updatedInvoice, originalVersion); err != nil {
            return err
        }

        return nil
    })
}
```

## Implementation Phases

### Phase 1: Invoice Model Enhancement (Week 1)

1. **Database Migration**: Add new amount fields to invoices table

   - Add `subtotal`, `total`, `amount_due`, `amount_paid`, `amount_remaining` columns
   - Create indexes for efficient amount calculations
   - Write migration scripts with proper rollback support

2. **Invoice Model Updates**: Update Invoice domain model and repository

   - Add new amount fields to Invoice struct
   - Update all invoice creation/update logic to populate these fields
   - Implement backward compatibility with existing `amount` field

3. **Invoice Amount Calculator**: Implement InvoiceAmountCalculator service
   - Core calculation logic for all amount fields
   - Integration with existing line items, payments, and tax calculations
   - Data consistency validation methods

### Phase 2: Core Credit Note Foundation (Week 2-3)

1. **Database Schema**: Create credit_notes and credit_note_line_items tables
2. **Domain Models**: Implement CreditNote and CreditNoteLineItem domain models
3. **Types**: Add credit note enums and types to the types package
4. **Basic Repository**: Implement CRUD operations for credit notes
5. **Transaction Manager**: Implement InvoiceTransactionManager for data consistency

### Phase 3: Business Logic Implementation (Week 4-5)

1. **Credit Note Service**: Implement core business logic for credit note creation
2. **Validation Logic**: Implement comprehensive validation rules
3. **Amount Recalculation**: Integrate credit notes with invoice amount calculations
4. **Customer Balance Integration**: Enhance wallet integration for refund credit notes
5. **Concurrent Operation Handling**: Implement row-level locking and version control

### Phase 4: API Layer (Week 6)

1. **REST Endpoints**: Implement API endpoints for credit note operations
2. **Request/Response DTOs**: Create API request and response structures
3. **Enhanced Invoice APIs**: Update invoice APIs to include new amount fields
4. **API Validation**: Implement request validation
5. **API Documentation**: Update Swagger documentation

### Phase 5: Advanced Features (Week 7-8)

1. **PDF Generation**: Implement credit note PDF generation
2. **Email Notifications**: Implement email notifications for credit note events
3. **Webhook Events**: Implement webhook events for credit note operations
4. **Number Generation**: Implement credit note number generation system
5. **Data Migration**: Migrate existing invoices to populate new amount fields

### Phase 6: Testing & Validation (Week 9)

1. **Unit Tests**: Comprehensive unit tests for all components
2. **Integration Tests**: End-to-end integration tests with transaction validation
3. **API Tests**: API endpoint testing including concurrent operation scenarios
4. **Performance Tests**: Test invoice amount calculation performance
5. **Data Consistency Tests**: Validate amount field consistency across operations

### Phase 7: Deployment & Monitoring (Week 10)

1. **Production Deployment**: Deploy with proper rollback strategies
2. **Monitoring Setup**: Implement monitoring for amount calculation accuracy
3. **Performance Monitoring**: Monitor invoice operation performance
4. **Documentation**: Complete API and user documentation
5. **Training**: Train support team on credit note functionality

## Risk Assessment

### High Risk

1. **Customer Balance Complexity**: The interaction between credit notes and existing customer balance is complex and error-prone
2. **Data Consistency**: Ensuring data consistency across invoice adjustments and wallet transactions
3. **Performance Impact**: Credit note operations involve multiple table updates and calculations

### Medium Risk

1. **PDF Generation**: Credit note PDF generation may have performance implications
2. **External Sync**: Handling credit notes for externally synced invoices
3. **Concurrent Operations**: Race conditions during simultaneous credit note and payment operations

### Low Risk

1. **API Compatibility**: Maintaining backward compatibility with existing APIs
2. **Email Delivery**: Email notification failures should not block credit note creation
3. **Webhook Reliability**: Webhook delivery failures should be handled gracefully

## Testing Strategy

### Unit Tests

- Credit note creation validation
- Credit note type determination logic
- Amount calculation logic
- Customer balance integration logic

### Integration Tests

- End-to-end credit note creation flow
- Invoice amount adjustment verification
- Customer balance credit verification
- PDF generation testing

### API Tests

- Create credit note API endpoints
- List and retrieve credit note endpoints
- Void credit note endpoint
- Error handling scenarios

### Performance Tests

- Credit note creation performance under load
- Database query optimization verification
- PDF generation performance testing

## Monitoring & Metrics

### Key Metrics

1. **Credit Note Volume**: Track number of credit notes created per day/week/month
2. **Credit Note Amounts**: Track total amounts credited over time
3. **Credit Note Types**: Distribution between adjustment and refund credit notes
4. **Processing Time**: Average time to process credit note creation
5. **Error Rates**: Credit note creation failure rates

### Alerts

1. **High Credit Note Volume**: Alert when credit note creation exceeds normal thresholds
2. **Processing Failures**: Alert on credit note creation failures
3. **Customer Balance Discrepancies**: Alert on customer balance calculation errors
4. **PDF Generation Failures**: Alert on credit note PDF generation failures

## Success Criteria

### Functional Requirements

- [ ] Create adjustment credit notes for unpaid invoices
- [ ] Create refund credit notes for paid invoices
- [ ] Support line-item level credit notes with partial amounts
- [ ] Integrate seamlessly with customer balance/wallet system
- [ ] Generate credit note PDFs
- [ ] Support credit note voiding (adjustment credit notes only)
- [ ] Emit webhook events for credit note operations
- [ ] Send email notifications for credit note creation

### Non-Functional Requirements

- [ ] Credit note creation completes within 5 seconds
- [ ] Support concurrent credit note operations
- [ ] Maintain data consistency across all operations
- [ ] 99.9% uptime for credit note operations
- [ ] API response times under 500ms for read operations

### Business Requirements

- [ ] Reduce customer support workload for billing disputes
- [ ] Improve customer satisfaction with billing corrections
- [ ] Provide complete audit trail for all billing adjustments
- [ ] Enable automated pro-rated credits for subscription cancellations
- [ ] Maintain compliance with accounting standards

## Future Enhancements

### Advanced Features

1. **Automated Credit Notes**: Automatic credit note generation for subscription cancellations
2. **Credit Note Templates**: Pre-defined templates for common credit note scenarios
3. **Approval Workflows**: Multi-step approval process for large credit amounts
4. **Bulk Credit Notes**: Ability to create credit notes for multiple invoices at once
5. **Tax Recalculation**: Advanced tax handling for credit notes with different tax rates

### Integration Enhancements

1. **External Provider Sync**: Sync credit notes with external billing providers
2. **Accounting System Integration**: Direct integration with accounting systems
3. **Customer Portal**: Self-service credit note requests for customers
4. **Mobile App Support**: Mobile-optimized credit note management

### Analytics & Reporting

1. **Credit Note Analytics**: Detailed analytics on credit note patterns
2. **Financial Impact Reports**: Reports on financial impact of credit notes
3. **Customer Behavior Analysis**: Analysis of customers who receive credit notes
4. **Churn Prevention**: Use credit note data for churn prevention strategies

This comprehensive PRD provides a solid foundation for implementing credit note functionality that will meet the needs of FlexPrice customers while maintaining the high standards of the existing platform.
