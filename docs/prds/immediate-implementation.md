# Immediate Implementation Plan (Next 4-5 Days)

## Core Components to Implement

### 1. Invoice Generation Service Enhancements

#### Database Schema Updates
```sql
ALTER TABLE invoices
ADD COLUMN billing_sequence INTEGER,
ADD COLUMN invoice_number VARCHAR(50),
ADD COLUMN idempotency_key VARCHAR(50),

-- Constraints
CREATE UNIQUE INDEX idx_tenant_invoice_number ON invoices (tenant_id, invoice_number);
CREATE UNIQUE INDEX idx_subscription_period ON invoices (subscription_id, period_start, period_end) WHERE status != 'VOIDED';
CREATE UNIQUE INDEX idx_idempotency ON invoices (idempotency_key) WHERE idempotency_key IS NOT NULL;
```

#### Key Fields Explanation

1. **billing_sequence**
   - Purpose: Tracks the chronological order of invoices for a subscription
   - Use cases:
     - Ensures proper ordering when multiple invoices exist for same period
     - Helps in audit trail for invoice regeneration
     - Enables validation of billing period coverage
   - Implementation:
     - Auto-incrementing per subscription
     - Reset on subscription renewal
     - Used in period coverage validation

2. **invoice_number**
   - Format: INV-{YYYYMM}-{5-digit-sequence}
   - Example: INV-202501-00001
   - Implementation:
     - Use database sequence for atomic increment
     - Reset sequence monthly. Can there be a better way?
     - Prefix with current month/year - Need to be implemented as a template so that can be changed easily for a given tenant if required

3. **idempotency_key**
   - Purpose: Ensures idempotency for invoice generation
   - Use cases:
     - Prevents duplicate generation of invoices
     - Helps in audit trail for invoice regeneration

### 2. BillingEngine Interface Implementation

```go
type BillingEngine interface {
    // GenerateInvoice handles invoice generation for a period
    GenerateInvoice(ctx context.Context, req *InvoiceGenerationRequest) (*Invoice, error)
    
    // PreviewInvoice calculates charges without persistence
    PreviewInvoice(ctx context.Context, req *InvoiceGenerationRequest) (*InvoicePreview, error)
    
    // RefreshInvoice recalculates an existing draft invoice
    RefreshInvoice(ctx context.Context, invoiceID string) (*Invoice, error)
}

type InvoiceGenerationRequest struct {
    SubscriptionID string
    PeriodStart    time.Time
    PeriodEnd      time.Time
    IdempotencyKey string
    Options        *InvoiceOptions
}

type InvoiceOptions struct {
    Preview        bool
    SkipPersist   bool
    TimeZone      string
}
```

### 3. Period Management Implementation

```go
type PeriodManager interface {
    // ValidatePeriod ensures period boundaries are valid
    ValidatePeriod(ctx context.Context, sub *Subscription, start, end time.Time) error
    
    // GetNextSequence returns next billing sequence for subscription
    GetNextSequence(ctx context.Context, subscriptionID string) (int, error)
    
    // ValidateCoverage checks for gaps in billing periods
    ValidateCoverage(ctx context.Context, sub *Subscription, start, end time.Time) error
}
```

## Implementation Steps (Day by Day)

### Day 1: Database & Core Interface
1. Create database migration for new fields
2. Implement invoice number generation logic
3. Set up billing sequence management
4. Create base BillingEngine interface

### Day 2: Invoice Generation
1. Implement GenerateInvoice method
2. Add idempotency handling
3. Create atomic transaction wrapper
4. Implement error handling

### Day 3: Preview & Refresh
1. Implement PreviewInvoice method
2. Add RefreshInvoice for draft invoices
3. Create shared calculation logic
4. Add validation checks

### Day 4: Period Management
1. Implement PeriodManager interface
2. Add timezone handling logic
3. Create period validation functions
4. Add coverage validation

### Day 5: Testing & Integration
1. Write unit tests for all components
2. Add integration tests
3. Create migration script
4. Update existing cron jobs

## Testing Scenarios

### Critical Test Cases
1. Multiple period transitions
2. Concurrent invoice generation
3. Timezone edge cases
4. Draft invoice refresh
5. Preview calculation accuracy

### Validation Tests
1. Period boundary validation
2. Sequence number generation
3. Invoice number uniqueness
4. Idempotency key handling

## Monitoring & Logging

### Key Metrics
1. Invoice generation success rate
2. Processing time per invoice
3. Preview calculation time
4. Error rates by category

### Log Points
```go
// Example logging structure
log.With(
    "subscription_id", req.SubscriptionID,
    "period_start", req.PeriodStart,
    "period_end", req.PeriodEnd,
    "billing_sequence", sequence,
    "invoice_number", invoiceNumber,
).Info("generating invoice")
```

## Rollout Plan

### Phase 1 (Day 1-2)
1. Deploy database changes
2. Add new fields to models
3. Update repository layer

### Phase 2 (Day 3-4)
1. Deploy new billing engine
2. Enable preview functionality
3. Add refresh capability

### Phase 3 (Day 5)
1. Enable new invoice generation
2. Update cron jobs
3. Monitor metrics
