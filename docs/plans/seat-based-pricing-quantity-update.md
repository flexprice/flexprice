# Seat-Based Pricing: Edit Fixed Charges Quantity with Proration

## Context

FlexPrice needs the capability to edit the quantity of fixed charges (seat-based pricing) on subscription line items with an optional proration feature. This is for scenarios like:
- Customer adds more seats mid-billing period
- Customer reduces seats mid-billing period
- Billing team needs to adjust quantities with or without prorated charges/credits

The proration logic already exists in the codebase (`ProrationActionQuantityChange`), but there's no API endpoint to trigger it. The `UpdateSubscriptionLineItemRequest` currently lacks a `quantity` field.

**Slack Thread**: https://flexpriceio.slack.com/archives/C0870JC1M6H/p1772180958991769

---

## Implementation Plan

### 1. Update DTO (`internal/api/dto/subscription_line_item.go`)

**Add fields to `UpdateSubscriptionLineItemRequest`:**

```go
type UpdateSubscriptionLineItemRequest struct {
    // ... existing fields ...

    // Quantity is the new quantity for the line item (seats, units, etc.)
    // Only applicable for PRICE_TYPE_FIXED prices
    Quantity *decimal.Decimal `json:"quantity,omitempty" swaggertype:"string"`

    // ProrationBehavior controls how mid-period quantity changes are handled
    // - "create_prorations": Generate prorated credits/charges (default when quantity changes)
    // - "none": Just update quantity without proration adjustments
    ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`
}
```

**Update `ShouldCreateNewLineItem()` method:**
- Quantity changes should NOT trigger a new line item (update in-place with proration)

**Update `Validate()` method:**
- Validate quantity is positive if provided
- Validate proration_behavior is valid if provided

---

### 2. Update Service Layer (`internal/service/subscription_line_item.go`)

**Modify `UpdateSubscriptionLineItem` method to handle quantity changes:**

```go
// In the else branch (metadata/commitment updates path), add quantity handling:

// Handle quantity change
if req.Quantity != nil {
    // 1. Validate: Only FIXED prices can have quantity updates
    if existingLineItem.PriceType != types.PRICE_TYPE_FIXED {
        return error: "quantity updates only allowed for fixed price line items"
    }

    // 2. Validate: New quantity must be different and positive
    if req.Quantity.Equal(existingLineItem.Quantity) {
        return error: "new quantity must be different from current"
    }
    if req.Quantity.LessThanOrEqual(decimal.Zero) {
        return error: "quantity must be positive"
    }

    // 3. Validate MinQuantity from price if applicable
    price, err := priceService.GetPrice(ctx, existingLineItem.PriceID)
    if price.MinQuantity != nil && req.Quantity.LessThan(*price.MinQuantity) {
        return error: "quantity below minimum"
    }

    // 4. Determine proration behavior (default to create_prorations)
    prorationBehavior := req.ProrationBehavior
    if prorationBehavior == "" {
        prorationBehavior = types.ProrationBehaviorCreateProrations
    }

    // 5. Calculate proration if needed
    if prorationBehavior == types.ProrationBehaviorCreateProrations {
        prorationResult, err := s.calculateQuantityChangeProration(ctx, sub, existingLineItem, price, req.Quantity, effectiveFrom)
        // Store proration result for response/invoice generation
    }

    // 6. Update the quantity
    existingLineItem.Quantity = *req.Quantity
}
```

**Add new helper method `calculateQuantityChangeProration`:**

```go
func (s *subscriptionService) calculateQuantityChangeProration(
    ctx context.Context,
    sub *subscription.Subscription,
    lineItem *subscription.SubscriptionLineItem,
    price *price.Price,
    newQuantity *decimal.Decimal,
    effectiveDate time.Time,
) (*proration.ProrationResult, error) {
    prorationService := NewProrationService(s.ServiceParams)

    params := proration.ProrationParams{
        SubscriptionID:     sub.ID,
        LineItemID:         lineItem.ID,
        PlanPayInAdvance:   price.InvoiceCadence == types.InvoiceCadenceAdvance,
        CurrentPeriodStart: sub.CurrentPeriodStart,
        CurrentPeriodEnd:   sub.CurrentPeriodEnd,
        Action:             types.ProrationActionQuantityChange,
        OldPriceID:         lineItem.PriceID,
        OldQuantity:        lineItem.Quantity,
        OldPricePerUnit:    price.Amount,
        NewPriceID:         lineItem.PriceID,  // Same price
        NewQuantity:        *newQuantity,
        NewPricePerUnit:    price.Amount,      // Same price
        ProrationDate:      effectiveDate,
        ProrationBehavior:  types.ProrationBehaviorCreateProrations,
        CustomerTimezone:   sub.CustomerTimezone,
        ProrationStrategy:  types.StrategyDayBased,
        Currency:           price.Currency,
        PlanDisplayName:    lineItem.PlanDisplayName,
    }

    return prorationService.CalculateProration(ctx, params)
}
```

**Add invoice generation for proration:**

```go
func (s *subscriptionService) generateProrationInvoice(
    ctx context.Context,
    sub *subscription.Subscription,
    lineItem *subscription.SubscriptionLineItem,
    prorationResult *proration.ProrationResult,
) (*invoice.Invoice, error) {
    // Similar to subscription_change.go flow:
    // 1. Create invoice with proration line items
    // 2. Handle both credit and charge items
    // 3. Return generated invoice

    invoiceService := NewInvoiceService(s.ServiceParams)

    // Build invoice line items from proration result
    var invoiceLineItems []dto.CreateInvoiceLineItemRequest

    for _, creditItem := range prorationResult.CreditItems {
        invoiceLineItems = append(invoiceLineItems, dto.CreateInvoiceLineItemRequest{
            Description: creditItem.Description,
            Amount:      creditItem.Amount,
            PriceID:     creditItem.PriceID,
            Quantity:    creditItem.Quantity,
            PeriodStart: &creditItem.StartDate,
            PeriodEnd:   &creditItem.EndDate,
        })
    }

    for _, chargeItem := range prorationResult.ChargeItems {
        invoiceLineItems = append(invoiceLineItems, dto.CreateInvoiceLineItemRequest{
            Description: chargeItem.Description,
            Amount:      chargeItem.Amount,
            PriceID:     chargeItem.PriceID,
            Quantity:    chargeItem.Quantity,
            PeriodStart: &chargeItem.StartDate,
            PeriodEnd:   &chargeItem.EndDate,
        })
    }

    // Create proration invoice
    return invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
        CustomerID:     sub.CustomerID,
        SubscriptionID: &sub.ID,
        Currency:       sub.Currency,
        LineItems:      invoiceLineItems,
        InvoiceType:    types.InvoiceTypeSubscription,
    })
}
```

---

### 3. Update Response DTO

**Extend `SubscriptionLineItemResponse` to include proration details when applicable:**

```go
type SubscriptionLineItemResponse struct {
    *subscription.SubscriptionLineItem

    // ProrationResult contains details about any proration calculated for this update
    // Only populated when quantity changes with proration enabled
    ProrationResult *ProrationResultResponse `json:"proration_result,omitempty"`
}

type ProrationResultResponse struct {
    NetAmount    decimal.Decimal `json:"net_amount"`
    CreditAmount decimal.Decimal `json:"credit_amount"`
    ChargeAmount decimal.Decimal `json:"charge_amount"`
    Description  string          `json:"description"`
}

type UpdateSubscriptionLineItemResponse struct {
    *SubscriptionLineItemResponse

    // Invoice generated for proration (if applicable)
    Invoice *InvoiceResponse `json:"invoice,omitempty"`

    // ProrationResult contains details about proration calculated
    ProrationResult *ProrationResultResponse `json:"proration_result,omitempty"`
}
```

---

### 4. Invoice Generation (Immediate)

When proration is enabled, generate an immediate invoice similar to the subscription change (upgrade/downgrade) flow:

1. **Calculate proration** using existing proration service
2. **Generate immediate invoice** with proration line items:
   - For quantity increase: Charge for additional seats for remaining period
   - For quantity decrease: Credit for reduced seats for remaining period
3. **Invoice generation respects billing config**:
   - For `InvoiceCadenceAdvance` (pay in advance): Generate immediate invoice
   - For `InvoiceCadenceArrears` (pay in arrears): Add to end-of-period invoice

**Future Enhancement**: Add option to schedule quantity change at period end (next invoice) instead of immediate.

**Reference Implementation**: See `subscription_change.go` for how upgrade/downgrade handles invoice generation.

---

### 5. Files to Modify

| File | Changes |
|------|---------|
| `internal/api/dto/subscription_line_item.go` | Add `Quantity`, `ProrationBehavior` fields to UpdateRequest; Add `UpdateSubscriptionLineItemResponse` |
| `internal/service/subscription_line_item.go` | Add quantity change handling with proration calculation and invoice generation |
| `internal/api/v1/subscription.go` | Update handler to return extended response with invoice/proration details |
| `internal/service/subscription.go` | Add `UpdateSubscriptionLineItemWithProration` method signature to interface |

---

### 6. Validation Rules

1. **Quantity updates only for FIXED prices** - Usage-based prices track quantity via meters
2. **Quantity must be positive** - Zero or negative not allowed
3. **Quantity must be different** - No-op updates rejected
4. **MinQuantity enforcement** - Must respect price.MinQuantity if set
5. **Active subscription only** - Only active subscriptions can have quantity updates
6. **Active line item only** - Only active (non-terminated) line items can be updated

---

### 7. API Example

**Request:**
```json
PUT /subscriptions/lineitems/{id}
{
    "quantity": "15",
    "proration_behavior": "create_prorations"
}
```

**Response:**
```json
{
    "line_item": {
        "id": "sli_xxx",
        "subscription_id": "sub_xxx",
        "quantity": "15",
        "price_type": "fixed",
        "currency": "USD"
    },
    "proration_result": {
        "net_amount": "50.00",
        "credit_amount": "0.00",
        "charge_amount": "50.00",
        "description": "Prorated charge for quantity change (10 -> 15 seats)"
    },
    "invoice": {
        "id": "inv_xxx",
        "status": "draft",
        "amount_due": "50.00",
        "currency": "USD",
        "line_items": [
            {
                "description": "Prorated charge for quantity change",
                "amount": "50.00"
            }
        ]
    }
}
```

---

## Verification Plan

1. **Unit Tests:**
   - Test quantity update with proration enabled
   - Test quantity update with proration disabled
   - Test quantity increase mid-period proration calculation
   - Test quantity decrease mid-period proration calculation
   - Test validation: only FIXED prices
   - Test validation: positive quantity
   - Test validation: MinQuantity enforcement

2. **Integration Tests:**
   - Create subscription with fixed price line item
   - Update quantity mid-period
   - Verify proration amounts are correct
   - Verify invoice items are generated

3. **Manual Testing:**
   - Create a subscription with a seat-based plan
   - Update quantity via API
   - Check proration in response
   - Verify next invoice reflects the changes
