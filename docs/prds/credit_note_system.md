# Credit Note System Implementation - Product Requirements Document

## Table of Contents

1. [Overview](#overview)
2. [Credit Notes Fundamentals](#credit-notes-fundamentals)
3. [Data Model Design](#data-model-design)
4. [Business Rules and Requirements](#business-rules-and-requirements)
5. [Service Layer Implementation](#service-layer-implementation)
6. [API Design](#api-design)
7. [Repository Layer](#repository-layer)
8. [Integration Points](#integration-points)
9. [Migration Plan](#migration-plan)

## Overview

### Goals

Implement a comprehensive credit note system in FlexPrice that allows for:

- Issuing credit notes against finalized invoices
- Supporting both account credits and cash refunds
- Partial credit notes for specific invoice line items
- Integration with payment processing for refunds
- Multi-tenant support with proper isolation

### Key Features

- **Dual Credit Types**: Account credits vs cash refunds
- **Granular Control**: Credit individual line items or entire invoices
- **Simple Status Management**: Clear lifecycle tracking
- **Payment Integration**: Automatic refund processing through payment providers
- **Financial Integrity**: Comprehensive validation and audit trails

### Goals

Implement a comprehensive credit note system in FlexPrice that allows for:

- Issuing credit notes against finalized invoices
- Supporting both account credits and cash refunds
- Partial credit notes for specific invoice line items
- Integration with payment processing for refunds
- Multi-tenant support with proper isolation
- Audit trail and compliance tracking

### Key Features

- **Dual Credit Types**: Account credits vs cash refunds
- **Granular Control**: Credit individual line items or entire invoices
- **Status Management**: Separate lifecycle tracking for credits and refunds
- **Payment Integration**: Automatic refund processing through payment providers
- **Financial Integrity**: Comprehensive validation and audit trails
- **Multi-tenant**: Tenant-isolated credit note management

## Credit Notes Fundamentals

### What are Credit Notes?

Credit notes are financial documents that reduce the amount owed by a customer, issued against an original invoice. They serve two primary purposes:

1. **Account Credits**: Add credit balance to customer's account for future use
2. **Cash Refunds**: Return money directly to customer's original payment method

### Types of Credit Notes

1. **Full Credit Note**: Credits the entire invoice amount
2. **Partial Credit Note**: Credits specific line items or partial amounts
3. **Adjustment Credit Note**: Corrects pricing errors or billing mistakes
4. **Refund Credit Note**: Processes actual cash refunds to customers

### Credit Note vs Invoice Relationship

```
Invoice (1) ←→ (0..n) Credit Notes
    ↓
Invoice Line Items (1) ←→ (0..n) Credit Note Items
```

Each credit note:

- References exactly one parent invoice
- Can credit multiple line items from that invoice
- Maintains separate tracking for credit and refund amounts
- Cannot exceed the remaining creditable amount on the invoice

## Credit Notes Fundamentals

### What are Credit Notes?

Credit notes are financial documents that reduce the amount owed by a customer, issued against an original invoice. They serve two primary purposes:

1. **Account Credits** (`credit_amount`): Add credit balance to customer's account for future use
2. **Cash Refunds** (`refund_amount`): Return money directly to customer's original payment method

### Why `credit_amount` and `refund_amount`?

Unlike invoices which track `amount_due` vs `amount_paid`, credit notes track the **type of compensation**:

- **Invoice Context**: What customer owes vs what they paid
- **Credit Note Context**: What type of credit customer receives (account credit vs cash back)

This design is optimal because:

- Clear semantic meaning
- Different processing logic for each type
- Better audit trail for accounting purposes

### Types of Credit Notes

1. **Full Credit Note**: Credits the entire invoice amount
2. **Partial Credit Note**: Credits specific line items or partial amounts
3. **Mixed Credit Note**: Combination of account credit and cash refund

## Data Model Design

### Core Entities

#### CreditNote Entity

```go
// internal/domain/creditnote/model.go
type CreditNote struct {
    ID          string    `json:"id"`
    CustomerID  string    `json:"customer_id"`
    InvoiceID   string    `json:"invoice_id"`

    // Sequential numbering
    Number      string    `json:"number"`

    // Core amounts - this is the optimal design
    CreditAmount  decimal.Decimal `json:"credit_amount"`  // Goes to customer account
    RefundAmount  decimal.Decimal `json:"refund_amount"`  // Cash back to customer
    TotalAmount   decimal.Decimal `json:"total_amount"`   // Sum of above

    Currency string `json:"currency"`

    // Simple status tracking
    CreditStatus CreditNoteStatus `json:"status"`

    // Reason and metadata
    Reason      CreditReason `json:"reason"`
    Description string       `json:"description"`

    // Timestamps
    IssuedAt    time.Time  `json:"issued_at"`
    ProcessedAt *time.Time `json:"processed_at,omitempty"`

    // Audit fields
    types.BaseModel
}

type CreditNoteStatus string

const (
    CreditNoteStatusPending   CreditNoteStatus = "pending"
    CreditNoteStatusProcessed CreditNoteStatus = "processed"
    CreditNoteStatusFailed    CreditNoteStatus = "failed"
    CreditNoteStatusCancelled CreditNoteStatus = "cancelled"
)

type CreditReason string

const (
    CreditReasonDuplicate             CreditReason = "duplicate"
    CreditReasonFraudulent            CreditReason = "fraudulent"
    CreditReasonRequestedByCustomer   CreditReason = "requested_by_customer"
    CreditReasonOrderCancellation     CreditReason = "order_cancellation"
    CreditReasonOrderReturn           CreditReason = "order_return"
    CreditReasonProductUnsatisfactory CreditReason = "product_unsatisfactory"
    CreditReasonOther                 CreditReason = "other"
)
```

#### CreditNoteItem Entity

```go
type CreditNoteItem struct {
    ID             string `json:"id"`
    CreditNoteID   string `json:"credit_note_id"`
    InvoiceItemID  string `json:"invoice_item_id"`

    // Amount breakdown for this item
    CreditAmount decimal.Decimal `json:"credit_amount"`
    RefundAmount decimal.Decimal `json:"refund_amount"`
    TotalAmount  decimal.Decimal `json:"total_amount"`

    // Item details
    Description string `json:"description"`
    Quantity    int64  `json:"quantity"`
    UnitAmount  decimal.Decimal `json:"unit_amount"`

    types.BaseModel
}
```

### Ent Schema Definitions

```go
// ent/schema/creditnote.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
    "github.com/flexprice/flexprice/ent/schema/mixin"
)

type CreditNote struct {
    ent.Schema
}

func (CreditNote) Mixin() []ent.Mixin {
    return []ent.Mixin{
        mixin.BaseMixin{},
        mixin.EnvironmentMixin{},
    }
}

func (CreditNote) Fields() []ent.Field {
    return []ent.Field{
        field.String("customer_id").NotEmpty(),
        field.String("invoice_id").NotEmpty(),
        field.String("number").Unique(),

        // Optimal amount design
        field.Other("credit_amount", &decimal.Decimal{}).
            SchemaType(map[string]string{
                dialect.Postgres: "decimal(20,4)",
            }).
            Default(decimal.Zero),

        field.Other("refund_amount", &decimal.Decimal{}).
            SchemaType(map[string]string{
                dialect.Postgres: "decimal(20,4)",
            }).
            Default(decimal.Zero),

        field.Other("total_amount", &decimal.Decimal{}).
            SchemaType(map[string]string{
                dialect.Postgres: "decimal(20,4)",
            }).
            Default(decimal.Zero),

        field.String("currency").Default("USD"),
        field.String("description").Optional(),

        // Simplified status
        field.Enum("status").Values("pending", "processed", "failed", "cancelled").Default("pending"),

        // Reason
        field.Enum("reason").Values(
            "duplicate", "fraudulent", "requested_by_customer",
            "order_cancellation", "order_return", "product_unsatisfactory", "other",
        ),

        // Timestamps
        field.Time("issued_at"),
        field.Time("processed_at").Optional(),
        field.String("created_by").NotEmpty(),
    }
}

func (CreditNote) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("customer", Customer.Type).
            Ref("credit_notes").
            Field("customer_id").
            Required().
            Unique(),

        edge.From("invoice", Invoice.Type).
            Ref("credit_notes").
            Field("invoice_id").
            Required().
            Unique(),

        edge.To("items", CreditNoteItem.Type),
    }
}

func (CreditNote) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("tenant_id", "customer_id"),
        index.Fields("tenant_id", "invoice_id"),
        index.Fields("tenant_id", "number").Unique(),
        index.Fields("status"),
        index.Fields("issued_at"),
    }
}
```

```go
// ent/schema/creditnoteitem.go
type CreditNoteItem struct {
    ent.Schema
}

func (CreditNoteItem) Mixin() []ent.Mixin {
    return []ent.Mixin{
        mixin.BaseMixin{},
    }
}

func (CreditNoteItem) Fields() []ent.Field {
    return []ent.Field{
        field.String("credit_note_id").NotEmpty(),
        field.String("invoice_item_id").NotEmpty(),

        field.Decimal("credit_amount").Default(0),
        field.Decimal("refund_amount").Default(0),
        field.Decimal("total_amount").Default(0),

        field.Text("description"),
        field.Int64("quantity").Default(1),
        field.Decimal("unit_amount").Default(0),
    }
}

func (CreditNoteItem) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("credit_note", CreditNote.Type).
            Ref("items").
            Field("credit_note_id").
            Required().
            Unique(),

        edge.From("invoice_item", InvoiceLineItem.Type).
            Ref("credit_note_items").
            Field("invoice_item_id").
            Required().
            Unique(),
    }
}
```

## Business Rules and Requirements

### Invoice Requirements for Credit Notes

1. **Invoice Status Requirements**:

   - Invoice must be `finalized` (not draft or pending)
   - Invoice must not be `cancelled` or `voided`

2. **Payment Requirements**:

   - For **credit amounts**: Invoice can be paid or unpaid
   - For **refund amounts**: Invoice must have been paid
   - Cannot refund more than the paid amount

3. **Amount Validation**:
   - Credit note total cannot exceed remaining creditable amount on invoice
   - Individual items cannot exceed their remaining creditable amount

### Credit Note Business Rules

1. **Sequential Numbering**:

   - Format: `CN-{invoice_number}-{sequence}`
   - Example: `CN-INV-2024-001-001`

2. **Amount Logic**:

   - `total_amount = credit_amount + refund_amount`
   - At least one of `credit_amount` or `refund_amount` must be > 0

3. **Status Lifecycle**:
   ```
   pending → processed (success)
   pending → failed (processing error)
   pending → cancelled (manual cancellation)
   ```

## Service Layer Implementation

### Credit Note Service Interface

```go
// internal/service/creditnote.go
package service

type CreditNoteInterface interface {
    // Core operations
    Create(ctx context.Context, req *CreateCreditNoteRequest) (*creditnote.CreditNote, error)
    GetByID(ctx context.Context, id string) (*creditnote.CreditNote, error)
    List(ctx context.Context, filter *ListCreditNotesRequest) (*ListCreditNotesResponse, error)

    // Business operations
    Process(ctx context.Context, id string) error
    Cancel(ctx context.Context, id string, reason string) error

    // Utility
    GetCreditableAmount(ctx context.Context, invoiceID string) (decimal.Decimal, error)
}

type CreateCreditNoteRequest struct {
    InvoiceID    string                        `json:"invoice_id" validate:"required"`
    CreditAmount decimal.Decimal               `json:"credit_amount" validate:"min=0"`
    RefundAmount decimal.Decimal               `json:"refund_amount" validate:"min=0"`
    Reason       creditnote.CreditReason       `json:"reason" validate:"required"`
    Description  string                        `json:"description"`
    Items        []CreateCreditNoteItemRequest `json:"items" validate:"required,min=1"`
}

type CreateCreditNoteItemRequest struct {
    InvoiceItemID string          `json:"invoice_item_id" validate:"required"`
    CreditAmount  decimal.Decimal `json:"credit_amount" validate:"min=0"`
    RefundAmount  decimal.Decimal `json:"refund_amount" validate:"min=0"`
    Quantity      int64           `json:"quantity" validate:"min=1"`
}
```

### Service Implementation

```go
func (s *creditNoteService) Create(ctx context.Context, req *CreateCreditNoteRequest) (*creditnote.CreditNote, error) {
    // Validate request
    if err := s.validate(ctx, req); err != nil {
        return nil, err
    }

    // Get invoice
    inv, err := s.invoiceRepo.GetByID(ctx, req.InvoiceID)
    if err != nil {
        return nil, err
    }

    // Generate credit note number
    number, err := s.generateNumber(ctx, inv)
    if err != nil {
        return nil, err
    }

    // Create credit note
    totalAmount := req.CreditAmount.Add(req.RefundAmount)
    creditNote := &creditnote.CreditNote{
        ID:           types.GenerateID(),
        TenantID:     types.GetTenantID(ctx),
        CustomerID:   inv.CustomerID,
        InvoiceID:    req.InvoiceID,
        Number:       number,
        CreditAmount: req.CreditAmount,
        RefundAmount: req.RefundAmount,
        TotalAmount:  totalAmount,
        Currency:     inv.Currency,
        Status:       creditnote.CreditNoteStatusPending,
        Reason:       req.Reason,
        Description:  req.Description,
        IssuedAt:     time.Now(),
        CreatedBy:    types.GetUserID(ctx),
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
    }

    // Create items
    items := make([]*creditnote.CreditNoteItem, len(req.Items))
    for i, itemReq := range req.Items {
        items[i] = &creditnote.CreditNoteItem{
            ID:            types.GenerateID(),
            CreditNoteID:  creditNote.ID,
            InvoiceItemID: itemReq.InvoiceItemID,
            CreditAmount:  itemReq.CreditAmount,
            RefundAmount:  itemReq.RefundAmount,
            TotalAmount:   itemReq.CreditAmount.Add(itemReq.RefundAmount),
            Quantity:      itemReq.Quantity,
            CreatedAt:     time.Now(),
            UpdatedAt:     time.Now(),
        }
    }

    // Save to database
    if err := s.repo.CreateWithItems(ctx, creditNote, items); err != nil {
        return nil, err
    }

    // Auto-process if configured
    go s.Process(context.Background(), creditNote.ID)

    return creditNote, nil
}

func (s *creditNoteService) Process(ctx context.Context, id string) error {
    creditNote, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return err
    }

    if creditNote.Status != creditnote.CreditNoteStatusPending {
        return ierr.NewError("credit note already processed").Mark(ierr.ErrBadRequest)
    }

    // Process credit amount (add to customer wallet)
    if creditNote.CreditAmount.GreaterThan(decimal.Zero) {
        if err := s.processCredit(ctx, creditNote); err != nil {
            s.updateStatus(ctx, id, creditnote.CreditNoteStatusFailed)
            return err
        }
    }

    // Process refund amount (refund via payment gateway)
    if creditNote.RefundAmount.GreaterThan(decimal.Zero) {
        if err := s.processRefund(ctx, creditNote); err != nil {
            s.updateStatus(ctx, id, creditnote.CreditNoteStatusFailed)
            return err
        }
    }

    // Mark as processed
    now := time.Now()
    err = s.repo.Update(ctx, id, &creditnote.UpdateCreditNote{
        Status:      &creditnote.CreditNoteStatusProcessed,
        ProcessedAt: &now,
    })

    return err
}

func (s *creditNoteService) validate(ctx context.Context, req *CreateCreditNoteRequest) error {
    // Must have at least one amount > 0
    if req.CreditAmount.IsZero() && req.RefundAmount.IsZero() {
        return ierr.NewError("either credit_amount or refund_amount must be greater than 0").Mark(ierr.ErrBadRequest)
    }

    // Get invoice and validate
    inv, err := s.invoiceRepo.GetByID(ctx, req.InvoiceID)
    if err != nil {
        return err
    }

    if inv.Status != types.InvoiceStatusFinalized {
        return ierr.NewError("credit notes can only be issued against finalized invoices").Mark(ierr.ErrBadRequest)
    }

    // Check if refund amount can be processed
    if req.RefundAmount.GreaterThan(decimal.Zero) && inv.PaymentStatus != types.PaymentStatusSucceeded {
        return ierr.NewError("refunds can only be processed for paid invoices").Mark(ierr.ErrBadRequest)
    }

    // Check creditable amount
    creditableAmount, err := s.GetCreditableAmount(ctx, req.InvoiceID)
    if err != nil {
        return err
    }

    totalAmount := req.CreditAmount.Add(req.RefundAmount)
    if totalAmount.GreaterThan(creditableAmount) {
        return ierr.NewError("credit note amount exceeds creditable amount").
            WithMetadata("creditable_amount", creditableAmount).
            WithMetadata("requested_amount", totalAmount).
            Mark(ierr.ErrBadRequest)
    }

    return nil
}
```

## API Design

### REST Endpoints

```go
// @Summary Create credit note
// @Description Create a new credit note for an invoice
// @Tags credit_notes
// @Accept json
// @Produce json
// @Param request body service.CreateCreditNoteRequest true "Credit note creation request"
// @Success 201 {object} creditnote.CreditNote
// @Router /credit_notes [post]
func (h *CreditNoteHandler) Create(c *gin.Context) {
    var req service.CreateCreditNoteRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    creditNote, err := h.creditNoteService.Create(c.Request.Context(), &req)
    if err != nil {
        handleServiceError(c, err)
        return
    }

    c.JSON(http.StatusCreated, creditNote)
}

// @Summary Get credit note
// @Description Get credit note by ID
// @Tags credit_notes
// @Produce json
// @Param id path string true "Credit note ID"
// @Success 200 {object} creditnote.CreditNote
// @Router /credit_notes/{id} [get]
func (h *CreditNoteHandler) GetByID(c *gin.Context) {
    id := c.Param("id")

    creditNote, err := h.creditNoteService.GetByID(c.Request.Context(), id)
    if err != nil {
        handleServiceError(c, err)
        return
    }

    c.JSON(http.StatusOK, creditNote)
}

// @Summary Process credit note
// @Description Process pending credit note
// @Tags credit_notes
// @Param id path string true "Credit note ID"
// @Success 200 {object} map[string]string
// @Router /credit_notes/{id}/process [post]
func (h *CreditNoteHandler) Process(c *gin.Context) {
    id := c.Param("id")

    if err := h.creditNoteService.Process(c.Request.Context(), id); err != nil {
        handleServiceError(c, err)
        return
    }

    c.JSON(http.StatusOK, gin.H{"message": "credit note processed successfully"})
}
```

## Repository Layer

```go
// internal/repository/ent/creditnote.go
func (r *creditNoteRepository) Create(ctx context.Context, cn *creditnote.CreditNote) error {
    _, err := r.client.CreditNote.
        Create().
        SetID(cn.ID).
        SetTenantID(cn.TenantID).
        SetCustomerID(cn.CustomerID).
        SetInvoiceID(cn.InvoiceID).
        SetNumber(cn.Number).
        SetCreditAmount(cn.CreditAmount).
        SetRefundAmount(cn.RefundAmount).
        SetTotalAmount(cn.TotalAmount).
        SetCurrency(cn.Currency).
        SetStatus(creditnote.Status(cn.Status)).
        SetReason(creditnote.Reason(cn.Reason)).
        SetNillableDescription(&cn.Description).
        SetIssuedAt(cn.IssuedAt).
        SetCreatedBy(cn.CreatedBy).
        Save(ctx)

    return err
}
```

## Integration Points

### Wallet Service Integration

```go
// For processing credit_amount
walletService.AddCredit(ctx, &AddCreditRequest{
    CustomerID: creditNote.CustomerID,
    Amount:     creditNote.CreditAmount,
    Currency:   creditNote.Currency,
    Source:     "credit_note",
    SourceID:   creditNote.ID,
})
```

### Payment Service Integration

```go
// For processing refund_amount
paymentService.ProcessRefund(ctx, &ProcessRefundRequest{
    CustomerID:   creditNote.CustomerID,
    InvoiceID:    creditNote.InvoiceID,
    Amount:       creditNote.RefundAmount,
    Currency:     creditNote.Currency,
    CreditNoteID: creditNote.ID,
})
```

## Migration Plan

### Phase 1: Foundation (Week 1-2)

- [ ] Create Ent schemas
- [ ] Generate database migrations
- [ ] Implement repository layer

### Phase 2: Core Service (Week 3-4)

- [ ] Implement service layer with simplified design
- [ ] Add validation logic
- [ ] Integrate with wallet and payment services

### Phase 3: API & Testing (Week 5-6)

- [ ] Implement REST API
- [ ] Add comprehensive tests
- [ ] Documentation

### Phase 4: Production (Week 7-8)

- [ ] Production deployment
- [ ] Monitoring setup
- [ ] Team training

## Key Benefits of This Design

1. **Clear Semantics**: `credit_amount` and `refund_amount` clearly indicate the type of compensation
2. **Simple Processing**: Single status field with clear lifecycle
3. **Flexible**: Supports pure credits, pure refunds, or mixed credit notes
4. **Maintainable**: Simplified design reduces complexity
5. **Audit-Friendly**: Clear distinction between credit types for accounting

Your intuition to use `credit_amount` and `refund_amount` is **correct and optimal** for a credit note system!
