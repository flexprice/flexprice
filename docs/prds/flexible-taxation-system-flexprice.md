# Flexible Taxation System for Flexprice

## 1. Introduction

This document outlines the design and implementation approach for a flexible taxation system in the Flexprice platform. The system supports hierarchical tax rules, multiple tax rates per line item, audit-proof tax snapshots, and efficient storage/computation strategies.

## 2. Requirements

1. **Flexibility**: Define and manage tax rates at different entity levels
2. **Hierarchy**: Support clear precedence rules: Line-item > Invoice > Customer > Plan > Tenant
3. **Multiple Taxes**: Allow multiple taxes on the same invoice line item (e.g., 9% CGST + 9% SGST)
4. **Historical Integrity**: Keep record of applied tax rates for audit purposes
5. **Management**: Edit, update, or archive tax rates without affecting historical data
6. **Efficient Storage**: Optimize tax data storage across various entities
7. **Calculation Timing**: Define when tax calculations occur in the invoice lifecycle

## 3. Data Model

### 3.1 New Tables

#### `tax_rates`

| Column        | Type          | Description                               |
| ------------- | ------------- | ----------------------------------------- |
| `id`          | UUID          | Primary key                               |
| `name`        | TEXT          | Human-readable name (e.g., "Central GST") |
| `code`        | TEXT          | Short code (e.g., "CGST")                 |
| `percentage`  | NUMERIC(9,6)  | Tax rate (e.g., 0.090000 for 9%)          |
| `fixed_value` | NUMERIC(18,6) | Fixed tax amount (e.g., 10.00 for 10.00)  |
| `is_compound` | BOOLEAN       | Whether tax is applied after other taxes  |
| `valid_from`  | TIMESTAMPTZ   | Start date of validity                    |
| `valid_to`    | TIMESTAMPTZ   | End date of validity                      |
| `created_at`  | TIMESTAMPTZ   | Creation timestamp                        |
| `archived_at` | TIMESTAMPTZ   | Soft delete timestamp                     |

#### `tax_rate_config`

| Column        | Type        | Description                                          |
| ------------- | ----------- | ---------------------------------------------------- |
| `id`          | UUID        | Primary key                                          |
| `scope`       | ENUM        | 'TENANT', 'CUSTOMER', 'PLAN', 'INVOICE', 'LINE_ITEM' |
| `scope_id`    | UUID        | ID of the entity this tax applies to                 |
| `tax_rate_id` | UUID        | Reference to tax_rates.id                            |
| `priority`    | SMALLINT    | Precedence value (lower = higher priority)           |
| `is_default`  | BOOLEAN     | Whether this is the default for the scope            |
| `created_at`  | TIMESTAMPTZ | Creation timestamp                                   |
| `archived_at` | TIMESTAMPTZ | Soft delete timestamp                                |

#### `invoice_taxes`

| Column        | Type          | Description                              |
| ------------- | ------------- | ---------------------------------------- |
| `id`          | UUID          | Primary key                              |
| `invoice_id`  | UUID          | Reference to invoices.id                 |
| `tax_rate_id` | UUID          | Reference to tax_rates.id                |
| `percentage`  | NUMERIC(9,6)  | Snapshot of tax rate when applied        |
| `fixed_value` | NUMERIC(18,6) | Snapshot of fixed tax value when applied |
| `amount`      | NUMERIC(18,6) | Total tax amount for this invoice        |

#### `invoice_line_item_taxes`

| Column         | Type          | Description                              |
| -------------- | ------------- | ---------------------------------------- |
| `id`           | UUID          | Primary key                              |
| `line_item_id` | UUID          | Reference to invoice_line_items.id       |
| `tax_rate_id`  | UUID          | Reference to tax_rates.id                |
| `percentage`   | NUMERIC(9,6)  | Snapshot of tax rate when applied        |
| `fixed_value`  | NUMERIC(18,6) | Snapshot of fixed tax value when applied |
| `amount`       | NUMERIC(18,6) | Tax amount for this line item            |
| `order`        | SMALLINT      | Order for applying compound taxes        |

### 3.2 Updates to Existing Tables

#### `invoices` (new columns)

| Column          | Type          | Description                                 |
| --------------- | ------------- | ------------------------------------------- |
| `total_tax`     | NUMERIC(18,6) | Sum of all tax amounts                      |
| `total_gross`   | NUMERIC(18,6) | total_net + total_tax                       |

Sample `applied_taxes` JSONB format:

```json
[
  { "code": "CGST", "percentage": 0.09, "amount": 45.0 },
  { "code": "SGST", "percentage": 0.09, "amount": 45.0 }
]
```

### 3.3 Indexes

```sql
CREATE INDEX ON tax_rate_config(scope, scope_id, archived_at);
CREATE INDEX ON invoice_taxes(invoice_id);
CREATE INDEX ON invoice_line_item_taxes(line_item_id);
```

## 4. Tax Hierarchy & Resolution

### 4.1 Precedence Rules

| Priority | Scope     | Example Use Case                              |
| -------- | --------- | --------------------------------------------- |
| 0        | Line-item | Specific product tax exemption or luxury duty |
| 1        | Invoice   | One-off manual tax override                   |
| 2        | Customer  | Customer-specific tax treatment               |
| 3        | Subscription | Plan with built-in tax rate                   |
| 4        | Tenant    | Global default tax policy                     |

### 4.2 Resolution Algorithm

```go
func ResolveTaxRates(ctx context.Context, tenantID, customerID, planID uuid.UUID,
                     invoiceID uuid.UUID, lineItemID uuid.UUID) []TaxRate {
    // 1. Get all active tax assignments
    assignments := repo.FindActiveAssignments(
        WithTenant(tenantID),
        WithCustomer(customerID),
        WithPlan(planID),
        WithInvoice(invoiceID),
        WithLineItem(lineItemID),
    )

    // 2. Sort by priority (lower number = higher priority)
    sort.SliceStable(assignments, func(i, j int) bool {
        return assignments[i].Priority < assignments[j].Priority
    })

    // 3. De-duplicate by tax_rate_id, keeping highest priority
    seen := make(map[uuid.UUID]bool)
    result := []TaxRate{}

    for _, a := range assignments {
        if _, exists := seen[a.TaxRateID]; !exists {
            taxRate := repo.GetTaxRate(a.TaxRateID)
            if taxRate != nil && taxRate.ArchivedAt == nil {
                result = append(result, *taxRate)
                seen[a.TaxRateID] = true
            }
        }
    }

    return result
}
```

## 5. Multiple Taxes & Compound Taxes

### 5.1 Supporting Multiple Taxes

Each line item can have multiple tax rates applied. These are stored as separate rows in `invoice_line_item_taxes`.

### 5.2 Compound vs. Non-Compound Taxes

- **Non-compound taxes** (default): Each tax is calculated on the base amount (e.g., CGST + SGST)
- **Compound taxes**: Taxes are applied sequentially, with each tax including the previous taxes (e.g., Canada's HST on top of PST)

### 5.3 Calculation Logic

```go
func CalculateLineTaxes(net decimal.Decimal, rates []TaxRate) ([]AppliedTax, decimal.Decimal) {
    taxBase := net
    totalTax := decimal.Zero
    appliedTaxes := []AppliedTax{}

    // Sort by order field if available
    sort.SliceStable(rates, func(i, j int) bool {
        return rates[i].Order < rates[j].Order
    })

    for _, rate := range rates {
        taxAmount := taxBase.Mul(rate.Percentage)

        appliedTaxes = append(appliedTaxes, AppliedTax{
            TaxRateID:  rate.ID,
            Code:       rate.Code,
            Percentage: rate.Percentage,
            Amount:     taxAmount,
        })

        totalTax = totalTax.Add(taxAmount)

        // If compound, add to the base for next tax calculation
        if rate.IsCompound {
            taxBase = taxBase.Add(taxAmount)
        }
    }

    return appliedTaxes, totalTax
}
```

## 6. CRUD Operations

### 6.1 Tax Rates Management

#### Create

```go
func CreateTaxRate(ctx context.Context, input TaxRateInput) (*TaxRate, error) {
    return repo.CreateTaxRate(ctx, &TaxRate{
        ID:          uuid.New(),
        Name:        input.Name,
        Code:        input.Code,
        Percentage:  input.Percentage,
        FixedValue:  input.FixedValue,
        IsCompound:  input.IsCompound,
        ValidFrom:   input.ValidFrom,
        ValidTo:     input.ValidTo,
        CreatedAt:   time.Now(),
    })
}
```

#### Update

To preserve historical integrity, update creates a new tax rate and archives the old one:

```go
func UpdateTaxRate(ctx context.Context, id uuid.UUID, input TaxRateInput) (*TaxRate, error) {
    // 1. Archive the old rate
    err := repo.ArchiveTaxRate(ctx, id)
    if err != nil {
        return nil, err
    }

    // 2. Create a new one with updated values
    newRate := &TaxRate{
        ID:          uuid.New(),
        Name:        input.Name,
        Code:        input.Code,
        Percentage:  input.Percentage,
        FixedValue:  input.FixedValue,
        IsCompound:  input.IsCompound,
        ValidFrom:   input.ValidFrom,
        ValidTo:     input.ValidTo,
        CreatedAt:   time.Now(),
    }

    // 3. Update all active assignments to point to the new rate
    err = repo.UpdateTaxAssignmentsForRate(ctx, id, newRate.ID)
    if err != nil {
        return nil, err
    }

    return repo.CreateTaxRate(ctx, newRate)
}
```

#### Archive/Delete

```go
func ArchiveTaxRate(ctx context.Context, id uuid.UUID) error {
    return repo.ArchiveTaxRate(ctx, id, time.Now())
}
```

### 6.2 Tax Assignments Management

#### Create/Update Assignment

```go
func AssignTaxRate(ctx context.Context, input TaxAssignmentInput) (*TaxAssignment, error) {
    return repo.CreateTaxAssignment(ctx, &TaxAssignment{
        ID:         uuid.New(),
        Scope:      input.Scope,
        ScopeID:    input.ScopeID,
        TaxRateID:  input.TaxRateID,
        Priority:   input.Priority,
        IsDefault:  input.IsDefault,
        CreatedAt:  time.Now(),
    })
}
```

#### Remove Assignment

```go
func RemoveTaxAssignment(ctx context.Context, id uuid.UUID) error {
    return repo.ArchiveTaxAssignment(ctx, id, time.Now())
}
```

## 7. Invoice Lifecycle Integration

### 7.1 Draft Creation

When creating a draft invoice, resolve applicable tax rates:

```go
func CreateDraftInvoice(ctx context.Context, input InvoiceInput) (*Invoice, error) {
    // Standard invoice creation logic...

    // For each line item, resolve and attach tax rates (percentages only)
    for _, item := range invoice.LineItems {
        taxRates := taxService.ResolveTaxRates(ctx,
            invoice.TenantID, invoice.CustomerID,
            item.PlanID, invoice.ID, item.ID)

        // Store temporarily for UI preview - no amounts yet
        item.TaxRates = taxRates
    }

    return invoice, nil
}
```

### 7.2 Invoice Finalization

When finalizing an invoice, re-resolve, calculate, and snapshot tax rates:

```go
func FinalizeInvoice(ctx context.Context, invoiceID uuid.UUID) (*Invoice, error) {
    tx, err := db.BeginTx(ctx)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback()

    invoice, err := repo.GetInvoice(ctx, invoiceID)
    if err != nil {
        return nil, err
    }

    // Re-resolve and apply tax rates
    totalTax := decimal.Zero
    appliedTaxesMap := make(map[string]AppliedTax)

    for _, item := range invoice.LineItems {
        // 1. Resolve applicable tax rates for this line item
        taxRates := taxService.ResolveTaxRates(ctx,
            invoice.TenantID, invoice.CustomerID,
            item.PlanID, invoice.ID, item.ID)

        // 2. Calculate tax amounts
        appliedTaxes, lineTotalTax := taxService.CalculateLineTaxes(item.NetAmount, taxRates)

        // 3. Store snapshots in invoice_line_item_taxes
        for i, applied := range appliedTaxes {
            _, err := repo.CreateInvoiceLineItemTax(ctx, &InvoiceLineItemTax{
                ID:         uuid.New(),
                LineItemID: item.ID,
                TaxRateID:  applied.TaxRateID,
                Percentage: applied.Percentage,
                Amount:     applied.Amount,
                Order:      int16(i),
            })
            if err != nil {
                return nil, err
            }

            // Aggregate for invoice level
            key := applied.Code
            if existing, found := appliedTaxesMap[key]; found {
                appliedTaxesMap[key] = AppliedTax{
                    TaxRateID:  existing.TaxRateID,
                    Code:       existing.Code,
                    Percentage: existing.Percentage,
                    Amount:     existing.Amount.Add(applied.Amount),
                }
            } else {
                appliedTaxesMap[key] = applied
            }
        }

        totalTax = totalTax.Add(lineTotalTax)
    }

    // 4. Create invoice_taxes entries for each unique tax
    for _, applied := range appliedTaxesMap {
        _, err := repo.CreateInvoiceTax(ctx, &InvoiceTax{
            ID:         uuid.New(),
            InvoiceID:  invoice.ID,
            TaxRateID:  applied.TaxRateID,
            Percentage: applied.Percentage,
            Amount:     applied.Amount,
        })
        if err != nil {
            return nil, err
        }
    }

    // 5. Update invoice with totals and applied_taxes JSON
    appliedTaxesJSON := []map[string]interface{}{}
    for _, tax := range appliedTaxesMap {
        appliedTaxesJSON = append(appliedTaxesJSON, map[string]interface{}{
            "code":       tax.Code,
            "percentage": tax.Percentage,
            "amount":     tax.Amount,
        })
    }

    invoice.TotalTax = totalTax
    invoice.TotalGross = invoice.TotalNet.Add(totalTax)
    invoice.AppliedTaxes = appliedTaxesJSON
    invoice.Status = "FINALIZED"

    err = repo.UpdateInvoice(ctx, invoice)
    if err != nil {
        return nil, err
    }

    err = tx.Commit()
    if err != nil {
        return nil, err
    }

    return invoice, nil
}
```

## 8. API Design

### 8.1 Tax Rates API

```
POST   /v1/tax-rates            Create tax rate
GET    /v1/tax-rates            List tax rates
GET    /v1/tax-rates/{id}       Get tax rate details
PATCH  /v1/tax-rates/{id}       Update tax rate (creates new version)
DELETE /v1/tax-rates/{id}       Archive tax rate
```

### 8.2 Tax Assignments API

```
POST   /v1/tax-assignments              Create assignment
GET    /v1/tax-assignments              List assignments
GET    /v1/tax-assignments/{id}         Get assignment details
PATCH  /v1/tax-assignments/{id}         Update assignment
DELETE /v1/tax-assignments/{id}         Archive assignment
GET    /v1/tax-assignments/by-scope     Get assignments by scope
```

### 8.3 Invoice Tax Overrides API

```
PATCH  /v1/invoices/{id}/taxes          Set invoice-level tax overrides
GET    /v1/invoices/{id}/taxes          Get applied taxes
```

## 9. Implementation in Flexprice Codebase

### 9.1 Directory Structure

```
internal/
  domain/
    tax/
      models.go               # Core domain models
      repository.go           # Data access layer
      service.go              # Business logic

  ent/
    schema/
      tax_rate.go
      tax_assignment.go
      invoice_tax.go
      invoice_line_item_tax.go

  api/
    v1/
      tax_rate.go
      tax_assignment.go

  service/
    tax/
      resolver.go             # Tax resolution logic
      calculator.go           # Tax calculation engine
      snapshot.go             # Creating tax snapshots
      service.go              # Service methods
```

### 9.2 Integration with Temporal Workflow

In the invoice finalization workflow:

```go
func (w *InvoiceWorkflow) FinalizeInvoice(ctx workflow.Context, req FinalizeRequest) error {
    // Existing finalization steps...

    // Add tax calculation step
    err = workflow.ExecuteActivity(ctx, w.activities.CalculateAndApplyTaxes, workflow.ActivityOptions{
        StartToCloseTimeout: 5 * time.Minute,
    }, req.InvoiceID).Get(ctx, nil)
    if err != nil {
        return err
    }

    // Continue with payment processing...

    return nil
}

// In activities implementation
func (a *InvoiceActivities) CalculateAndApplyTaxes(ctx context.Context, invoiceID uuid.UUID) error {
    return a.taxService.FinalizeInvoiceTaxes(ctx, invoiceID)
}
```

## 10. Example Scenarios

### 10.1 Indian GST Scenario

```
Setup:
- Tenant default: 18% GST (CGST 9% + SGST 9%)
- Customer: Standard domestic customer
- Line item: Taxable service worth ₹1,000

Resolution:
- Both CGST and SGST apply (non-compound)
- CGST: 9% of ₹1,000 = ₹90
- SGST: 9% of ₹1,000 = ₹90
- Total tax: ₹180
- Invoice total: ₹1,180

applied_taxes JSON on invoice:
[
  {"code":"CGST","percentage":0.09,"amount":90.00},
  {"code":"SGST","percentage":0.09,"amount":90.00}
]
```

### 10.2 International Customer Scenario

```
Setup:
- Tenant default: 18% GST (CGST 9% + SGST 9%)
- Customer override: 0% Export (priority 2)
- Line item: Service worth ₹1,000

Resolution:
- Customer export override applies (0%)
- Total tax: ₹0
- Invoice total: ₹1,000

applied_taxes JSON on invoice:
[
  {"code":"EXPORT","percentage":0.00,"amount":0.00}
]
```

### 10.3 Mixed Line Items Scenario

```
Setup:
- Tenant default: 18% GST
- Line item 1: Standard service ₹1,000
- Line item 2: Luxury item ₹2,000 with 28% GST override (priority 0)

Resolution:
- Line item 1: 18% of ₹1,000 = ₹180
- Line item 2: 28% of ₹2,000 = ₹560
- Total tax: ₹740
- Invoice total: ₹3,740

applied_taxes JSON on invoice:
[
  {"code":"GST","percentage":0.18,"amount":180.00},
  {"code":"LUX_GST","percentage":0.28,"amount":560.00}
]
```

## 11. Implementation Roadmap

1. **Database Migration**: Create the new tables and add columns to `invoices`
2. **Core Tax Service**: Implement tax domain models, repositories, and services
3. **API Layer**: Add endpoints for tax management
4. **Integration**: Hook into invoice creation and finalization flows
5. **Testing**: Unit and integration tests for tax resolution, calculation, and snapshot logic
6. **Rollout**: Feature flag for gradual release, backfill 0% default tax for existing tenants

## 12. Advantages of This Design

1. **Flexibility**: Can handle any tax scenario from simple flat rates to complex hierarchical rules
2. **Audit Proof**: Historical invoices remain immutable even if tax rates change
3. **Performance**: Pre-calculated totals and JSONB summary for quick access
4. **Multiple Taxes**: Supports any number of taxes per line item, with or without compounding
5. **Clear Precedence**: Explicit priority rules prevent ambiguity
6. **Future-proof**: Easy to extend with external tax services like Stripe Tax

## 13. Conclusion

This design provides a comprehensive solution for Flexprice's taxation requirements. It handles the hierarchy of tax rates, supports multiple taxes per line item, maintains historical integrity, and provides efficient storage and calculation strategies.

By implementing this system, Flexprice can offer customers flexible taxation options that work with complex international tax regulations while maintaining audit-friendly records.
