# Subscription Proration and Workflow Implementation

## 1. Introduction

This document outlines a phased, pragmatic approach to implementing subscription prorations and workflows in FlexPrice. Proration is a critical billing component that ensures customers are charged accurately when their subscriptions change mid-billing cycle. This guide provides technical implementation details for handling various subscription change scenarios.

## 2. Core Concepts

### 2.1 Proration Fundamentals

Proration is the process of calculating charges proportionally based on the time a service was used within a billing period. When a subscription changes mid-cycle, we need to:

1. Calculate a credit for the unused portion of the current subscription
2. Calculate a charge for the new subscription for the remainder of the billing period
3. Combine these as line items on a new or existing invoice

### 2.2 Key Proration Scenarios

- **Subscription Upgrade**: Moving to a higher-priced plan mid-cycle (processed immediately)
- **Subscription Downgrade**: Moving to a lower-priced plan mid-cycle (can be immediate or at period end)
- **Quantity Change**: Adjusting the quantity of a subscription
- **Subscription Cancellation**: Early termination and refund calculations
- **Subscribing to an add-on**: Adding new line items to the subscription mid-cycle

### 2.3 Proration Coefficient Strategies

The foundation of proration calculations is the proration coefficient. We'll support two strategies:

#### 2.3.1 Day-Based Proration (Default)

```
proration_coefficient = remaining_days / total_days_in_period
```

This calculates proration based on calendar days, which is more intuitive for customers and matches typical billing practices.

#### 2.3.2 Second-Based Proration (Future Enhancement)

```
proration_coefficient = remaining_seconds / total_seconds_in_period
```

This provides more precise proration for time-sensitive services and can be enabled as a configuration option in the future.

For both strategies, the proration calculations follow the same pattern:

For a credit:
```
credit_amount = original_price * proration_coefficient
```

For a charge:
```
charge_amount = new_price * proration_coefficient
```

## 3. Phased Implementation Approach

### 3.1 Phase 1: Core Proration Engine

The first step is to implement a focused `ProrationService` that handles basic proration calculations:

```go
type ProrationService interface {
    // CalculateProration calculates the proration for changing a subscription line item
    // Returns ProrationResult containing credit and charge items
    // Can be used both for preview and actual application
    CalculateProration(ctx context.Context, 
        subscription *subscription.Subscription,
        params ProrationParams) (*ProrationResult, error)
        
    // ApplyProration applies the calculated proration to the subscription
    // This creates the appropriate invoices or credits based on the proration result
    ApplyProration(ctx context.Context,
        subscription *subscription.Subscription,
        prorationResult *ProrationResult,
        prorationBehavior ProrationBehavior) error
}

type ProrationAction string

const (
    ProrationActionUpgrade      ProrationAction = "upgrade"
    ProrationActionDowngrade    ProrationAction = "downgrade"
    ProrationActionQuantityChange ProrationAction = "quantity_change"
    ProrationActionCancellation ProrationAction = "cancellation"
    ProrationActionAddItem      ProrationAction = "add_item"
    ProrationActionRemoveItem   ProrationAction = "remove_item"
)

type ProrationParams struct {
    LineItemID       string           // ID of the line item being changed (can be empty for add_item action)
    OldPriceID       *string          // Old price ID (nil for add_item)
    NewPriceID       *string          // New price ID (nil for cancellation or remove_item)
    OldQuantity      *decimal.Decimal // Old quantity (nil for add_item)
    NewQuantity      decimal.Decimal  // New quantity
    ProrationDate    time.Time        // When the proration takes effect
    ProrationBehavior ProrationBehavior
    ProrationStrategy ProrationStrategy // Day-based or second-based
    Action           ProrationAction  // Type of change being performed
}

type ProrationBehavior string

const (
    // CreateProrations will create prorated line items (default)
    ProrationBehaviorCreateProrations ProrationBehavior = "create_prorations"
    
    // AlwaysInvoice will immediately invoice prorated amounts
    ProrationBehaviorAlwaysInvoice ProrationBehavior = "always_invoice"
    
    // None will not perform any prorations
    ProrationBehaviorNone ProrationBehavior = "none"
)

type ProrationStrategy string

const (
    // DayBased calculates proration based on days (default)
    ProrationStrategyDayBased ProrationStrategy = "day_based"
    
    // SecondBased calculates proration based on seconds
    ProrationStrategySecondBased ProrationStrategy = "second_based"
)

type ProrationResult struct {
    Credits          []ProrationLineItem    // Credit line items
    Charges          []ProrationLineItem    // Charge line items
    NetAmount        decimal.Decimal        // Net amount (positive means customer owes, negative means refund/credit)
    Currency         string                 // Currency code
    Action           ProrationAction        // The type of action that generated this proration
    ProrationDate    time.Time              // Effective date for the proration
    LineItemID       string                 // ID of the affected line item (empty for new items)
    IsPreview        bool                   // Whether this is a preview or actual proration
}

type ProrationLineItem struct {
    Description string           // Description of the line item
    Amount      decimal.Decimal  // Amount (always positive)
    Type        types.LineItemType // Credit or Charge
    PriceID     string           // Associated price ID
    Quantity    decimal.Decimal  // Quantity
    UnitAmount  decimal.Decimal  // Unit amount
    PeriodStart time.Time        // Start of the applicable period
    PeriodEnd   time.Time        // End of the applicable period
}
```

#### 3.1.1 Core Calculation Logic

The proration calculation implementation will support both day-based and second-based strategies:

```go
func (s *prorationService) calculateProportionalAmount(
    amount decimal.Decimal,
    startDate, endDate, prorationDate time.Time,
    strategy ProrationStrategy,
) decimal.Decimal {
    var coefficient decimal.Decimal
    
    switch strategy {
    case ProrationStrategySecondBased:
    // Total period duration in seconds
        totalDuration := endDate.Sub(startDate).Seconds()
    
    // Remaining period duration in seconds
        remainingDuration := endDate.Sub(prorationDate).Seconds()
        
        // Calculate proration coefficient
        coefficient = decimal.NewFromFloat(remainingDuration / totalDuration)
        
    case ProrationStrategyDayBased, "": // Default to day-based
        // Calculate days in the period
        totalDays := decimal.NewFromInt(int64(daysBetween(startDate, endDate)))
        
        // Calculate remaining days
        remainingDays := decimal.NewFromInt(int64(daysBetween(prorationDate, endDate)))
    
    // Calculate proration coefficient
        coefficient = remainingDays.Div(totalDays)
    }
    
    // Apply coefficient to amount
    return amount.Mul(coefficient).Round(2)
}

// daysBetween calculates the number of calendar days between two dates
func daysBetween(start, end time.Time) int {
    // Convert to same timezone to avoid timezone issues
    startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
    endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
    
    // Calculate difference in days
    return int(endDate.Sub(startDate).Hours() / 24) + 1 // +1 to include both start and end days
}
```

#### 3.1.2 Proration Calculation Implementation

The main calculation method will validate inputs and handle different proration scenarios:

```go
func (s *prorationService) CalculateProration(
    ctx context.Context, 
    subscription *subscription.Subscription,
    params ProrationParams,
) (*ProrationResult, error) {
    // Validate inputs
    if err := s.validateProrationParams(ctx, subscription, params); err != nil {
        return nil, err
    }
    
    // Set default strategy if not specified
    if params.ProrationStrategy == "" {
        params.ProrationStrategy = ProrationStrategyDayBased
    }
    
    // Initialize result
    result := &ProrationResult{
        Credits:       []ProrationLineItem{},
        Charges:       []ProrationLineItem{},
        Currency:      subscription.Currency,
        Action:        params.Action,
        ProrationDate: params.ProrationDate,
        LineItemID:    params.LineItemID,
        IsPreview:     false, // Will be set to true if this is used for preview
    }
    
    // Handle different actions
    switch params.Action {
    case ProrationActionUpgrade, ProrationActionDowngrade, ProrationActionQuantityChange:
        if err := s.calculateChangePriceOrQuantity(ctx, subscription, params, result); err != nil {
            return nil, err
        }
    
    case ProrationActionAddItem:
        if err := s.calculateAddItem(ctx, subscription, params, result); err != nil {
            return nil, err
        }
        
    case ProrationActionRemoveItem:
        if err := s.calculateRemoveItem(ctx, subscription, params, result); err != nil {
            return nil, err
        }
        
    case ProrationActionCancellation:
        if err := s.calculateCancellation(ctx, subscription, params, result); err != nil {
            return nil, err
        }
        
    default:
        return nil, ierr.NewError("unsupported proration action").Mark(ierr.ErrValidation)
    }
    
    // Calculate net amount
    var netAmount decimal.Decimal
    for _, charge := range result.Charges {
        netAmount = netAmount.Add(charge.Amount)
    }
    
    for _, credit := range result.Credits {
        netAmount = netAmount.Sub(credit.Amount)
    }
    
    result.NetAmount = netAmount
    
    return result, nil
}
```

#### 3.1.3 Validation Logic

The service will include validation logic to ensure proration is only applied in appropriate scenarios:

```go
func (s *prorationService) validateProrationParams(
    ctx context.Context,
    subscription *subscription.Subscription,
    params ProrationParams,
) error {
    // Check subscription is valid
    if subscription == nil {
        return ierr.NewError("subscription cannot be nil").Mark(ierr.ErrValidation)
    }
    
    // Validate line item exists (except for add_item)
    if params.Action != ProrationActionAddItem && params.LineItemID != "" {
        var foundLineItem bool
        for _, item := range subscription.LineItems {
            if item.ID == params.LineItemID {
                foundLineItem = true
                
                // Validate price type is fixed
                price, err := s.PriceRepo.Get(ctx, item.PriceID)
    if err != nil {
                    return err
                }
                
                if price.PriceType != types.PRICE_TYPE_FIXED {
                    return ierr.NewError("proration only supported for fixed prices").Mark(ierr.ErrValidation)
                }
                
                // Validate billing cadence is recurring
                if price.BillingCadence != types.BILLING_CADENCE_RECURRING {
                    return ierr.NewError("proration only supported for recurring billing cadence").Mark(ierr.ErrValidation)
                }
                
                break
            }
        }
        
        if !foundLineItem {
            return ierr.NewError("line item not found").
                WithHintf("Line item %s not found in subscription %s", params.LineItemID, subscription.ID).
                Mark(ierr.ErrNotFound)
        }
    }
    
    // Action-specific validation
    switch params.Action {
    case ProrationActionUpgrade, ProrationActionDowngrade:
        if params.NewPriceID == nil {
            return ierr.NewError("new price ID is required for upgrades/downgrades").Mark(ierr.ErrValidation)
        }
        
        // Validate new price if provided
        if params.NewPriceID != nil {
            newPrice, err := s.PriceRepo.Get(ctx, *params.NewPriceID)
    if err != nil {
                return err
            }
            
            if newPrice.PriceType != types.PRICE_TYPE_FIXED {
                return ierr.NewError("proration only supported for fixed prices").Mark(ierr.ErrValidation)
            }
            
            if newPrice.BillingCadence != types.BILLING_CADENCE_RECURRING {
                return ierr.NewError("proration only supported for recurring billing cadence").Mark(ierr.ErrValidation)
            }
        }
        
    case ProrationActionQuantityChange:
        if params.OldQuantity == nil {
            return ierr.NewError("old quantity is required for quantity changes").Mark(ierr.ErrValidation)
        }
        
    case ProrationActionAddItem:
        if params.NewPriceID == nil {
            return ierr.NewError("new price ID is required for add item").Mark(ierr.ErrValidation)
        }
        
        // Validate new price
        newPrice, err := s.PriceRepo.Get(ctx, *params.NewPriceID)
        if err != nil {
            return err
        }
        
        if newPrice.PriceType != types.PRICE_TYPE_FIXED {
            return ierr.NewError("proration only supported for fixed prices").Mark(ierr.ErrValidation)
        }
        
        if newPrice.BillingCadence != types.BILLING_CADENCE_RECURRING {
            return ierr.NewError("proration only supported for recurring billing cadence").Mark(ierr.ErrValidation)
        }
    }
    
    return nil
}
```

### 3.2 Phase 2: Preview Functionality

Once the core proration engine is implemented, we'll integrate it with the existing preview functionality:

#### 3.2.1 Invoice Preview Integration

Extend the existing `GetPreviewInvoice` method to handle proration previews:

```go
func (s *invoiceService) GetPreviewInvoice(
    ctx context.Context,
    req dto.GetPreviewInvoiceRequest,
) (*dto.InvoiceResponse, error) {
    // Handle existing preview logic
    
    // If this is a proration preview request
    if req.ProrationPreview {
        sub, err := s.SubRepo.Get(ctx, req.SubscriptionID)
    if err != nil {
            return nil, err
        }
        
        // Map the request to proration params
        prorationParams := ProrationParams{
            LineItemID:        req.LineItemID,
            OldPriceID:        req.OldPriceID,
            NewPriceID:        req.NewPriceID,
            OldQuantity:       req.OldQuantity,
            NewQuantity:       req.NewQuantity,
            ProrationDate:     req.ProrationDate,
            ProrationBehavior: ProrationBehavior(req.ProrationBehavior),
            ProrationStrategy: ProrationStrategy(req.ProrationStrategy),
            Action:            ProrationAction(req.ProrationAction),
        }
        
        // Get proration calculation
        prorationResult, err := s.ProrationService.CalculateProration(
            ctx,
            sub,
            prorationParams,
        )
        if err != nil {
            return nil, err
        }
        
        // Mark as preview
        prorationResult.IsPreview = true
        
        // Convert proration result to invoice line items
        lineItems := s.convertProrationResultToLineItems(prorationResult)
        
        // Create a preview invoice with the line items
        previewInvoice := &dto.InvoiceResponse{
            CustomerID:     sub.CustomerID,
            SubscriptionID: &sub.ID,
            Currency:       sub.Currency,
            AmountDue:      prorationResult.NetAmount,
            LineItems:      lineItems,
            IsPreview:      true,
            // ... other fields
        }
        
        return previewInvoice, nil
    }
    
    // Handle other preview cases
    // ...
}
```

#### 3.2.2 Billing Service Integration

Extend the billing service to handle proration calculations, leveraging the existing `BillingCalculationResult` structure:

```go
func (s *billingService) CalculateProrationCharges(
    ctx context.Context,
    sub *subscription.Subscription,
    prorationResult *ProrationResult,
) (*BillingCalculationResult, error) {
    // Convert proration line items to invoice line items
    fixedCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
    
    // Add credit line items
    for _, credit := range prorationResult.Credits {
        fixedCharges = append(fixedCharges, dto.CreateInvoiceLineItemRequest{
            Description: credit.Description,
            Amount:      credit.Amount,
            PriceID:     credit.PriceID,
            Quantity:    credit.Quantity,
            UnitAmount:  credit.UnitAmount,
            PeriodStart: &credit.PeriodStart,
            PeriodEnd:   &credit.PeriodEnd,
            Type:        credit.Type,
        })
    }
    
    // Add charge line items
    for _, charge := range prorationResult.Charges {
        fixedCharges = append(fixedCharges, dto.CreateInvoiceLineItemRequest{
            Description: charge.Description,
            Amount:      charge.Amount,
            PriceID:     charge.PriceID,
            Quantity:    charge.Quantity,
            UnitAmount:  charge.UnitAmount,
            PeriodStart: &charge.PeriodStart,
            PeriodEnd:   &charge.PeriodEnd,
            Type:        charge.Type,
        })
    }
    
    return &BillingCalculationResult{
        FixedCharges: fixedCharges,
        TotalAmount:  prorationResult.NetAmount,
        Currency:     prorationResult.Currency,
    }, nil
}
```

## 4. Integration with Existing Services

### 4.1 Leveraging BillingService

The proration implementation will leverage the existing `BillingService` interface for calculating charges:

```go
// From the existing BillingService interface
type BillingService interface {
    // Existing methods...
    
    // CalculateFixedCharges calculates all fixed charges for a subscription
    CalculateFixedCharges(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)
    
    // CalculateAllCharges calculates both fixed and usage charges
    CalculateAllCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) (*BillingCalculationResult, error)
    
    // New method for proration
    CalculateProrationCharges(ctx context.Context, sub *subscription.Subscription, prorationResult *ProrationResult) (*BillingCalculationResult, error)
}
```

### 4.2 Leveraging InvoiceService

We'll extend the existing `InvoiceService` interface for handling proration invoices:

```go
// Extend GetPreviewInvoiceRequest to support proration previews
type GetPreviewInvoiceRequest struct {
    // Existing fields...
    
    // Proration preview fields
    ProrationPreview   bool
    LineItemID         string
    OldPriceID         *string
    NewPriceID         *string
    OldQuantity        *decimal.Decimal
    NewQuantity        decimal.Decimal
    ProrationDate      time.Time
    ProrationBehavior  string
    ProrationStrategy  string
    ProrationAction    string
}
```

## 5. Immediate Next Steps

### 5.1 Phase 1 Implementation Tasks

1. Create the `ProrationService` interface and basic implementation
2. Implement the core proration calculation logic
3. Implement validation logic for different proration scenarios
4. Write unit tests for proration calculations

### 5.2 Phase 2 Implementation Tasks

1. Extend `InvoiceService.GetPreviewInvoice` to handle proration previews
2. Add `CalculateProrationCharges` to the billing service
3. Create API endpoints for previewing subscription changes

## 6. Future Phases

For future phases, we'll need to implement:

1. Subscription workflow actions (upgrade, downgrade, etc.)
2. Pending changes and effective dates
3. Special case handling (trials, cross-currency, etc.)
4. Credit management

These will be detailed in a separate document once the core proration engine is complete.

## 7. Conclusion

This phased approach to implementing subscription proration focuses on:

1. Building a solid, flexible proration calculation engine first
2. Integrating with existing preview functionality to validate calculations
3. Handling different scenarios correctly with proper validation

By focusing on the proration engine and preview functionality first, we can ensure accurate calculations before implementing the more complex subscription workflow actions.

## 8. Implementation Approach

Based on our implementation experience, we've adopted the following approach for subscription updates and proration:

### 8.1 Modular Design

We've structured the subscription update functionality into several modular components:

1. **Core Subscription Update Methods**:
   - `UpdateSubscription`: Handles all types of subscription changes in a single API call
   - `PreviewSubscriptionUpdate`: Previews the impact of subscription changes without applying them

2. **Helper Methods for Different Operations**:
   - `processDeleteItem` / `previewDeleteItem`: Handle removal of line items
   - `processUpdateItem` / `previewUpdateItem`: Handle updates to existing line items
   - `processAddItem` / `previewAddItem`: Handle addition of new line items
   - `updateLineItemPrice`: Updates a line item with a new price

3. **Proration Service**:
   - `CalculateProration`: Calculates proration for different types of subscription changes
   - `ApplyProration`: Applies the calculated proration by creating invoices or credits

### 8.2 Transaction Flow

The subscription update process follows this flow:

1. **Validation**: Validate the update request
2. **Retrieval**: Get the subscription with its line items
3. **Processing**: Process each item in the request within a transaction:
   - For deletions: Calculate proration, apply it, and mark the item for removal
   - For updates: Calculate proration, apply it, and update the item
   - For additions: Calculate proration, apply it, and add the new item
4. **Finalization**: Update the subscription in the database
5. **Response**: Return the updated subscription

### 8.3 Proration Handling

Proration is handled through a dedicated service that:

1. Calculates credits for unused portions of existing subscriptions
2. Calculates charges for new or changed subscriptions
3. Creates invoices or credits based on the proration behavior
4. Supports different proration strategies (day-based or second-based)

### 8.4 Preview Functionality

The preview functionality follows the same logic as the actual update but:

1. Does not modify the subscription
2. Returns a preview invoice showing the financial impact
3. Marks all proration results as previews

### 8.5 Future Improvements

Based on our implementation, we've identified these areas for future improvement:

1. **Credit Management**: Implement a more robust system for handling negative amounts
2. **Effective Dates**: Add support for scheduling changes to take effect at a future date
3. **Bulk Operations**: Optimize for handling large numbers of subscription changes
4. **Retry Mechanism**: Implement a retry mechanism for failed proration payments
5. **Notification System**: Add notifications for subscription changes and proration charges

This modular approach allows for easy extension and maintenance of the subscription update functionality while ensuring accurate proration calculations.

## 9. Subscription Update Workflows - Revised Approach

After critical evaluation, we're revising our approach to subscription updates to create a more maintainable, less duplicative architecture that will be easier for new engineers to understand and extend.

### 9.1 Core Design Principles

1. **Separation of Concerns**: Clearly separate operation planning from execution
2. **Strategy Pattern**: Use strategy interfaces for different operation types
3. **Single Execution Path**: Use the same code path for both preview and actual execution
4. **Composability**: Build complex operations from simple, reusable components
5. **Minimized Conditionals**: Avoid complex if-else logic by using polymorphism

### 9.2 Architecture Overview

We'll introduce a new concept: **SubscriptionChangeOperation**, which represents a single atomic change to a subscription. The workflow follows these steps:

1. **Parse Request**: Convert API request into a list of operations
2. **Plan Operations**: Calculate the details and impacts of each operation
3. **Preview** (optional): Return the planned changes without executing them
4. **Execute**: Apply the planned changes if this is not a preview

This approach ensures that preview and actual execution share nearly 100% of their logic.

#### 9.2.1 Core Interfaces

```go
// SubscriptionChangeOperation represents a single atomic change to a subscription
type SubscriptionChangeOperation interface {
    // Plan calculates the details and impact of this operation without executing it
    Plan(ctx context.Context, sub *subscription.Subscription) (*OperationPlan, error)
    
    // Execute applies the planned changes to the subscription
    // If isPreview is true, it won't persist any changes
    Execute(ctx context.Context, sub *subscription.Subscription, plan *OperationPlan, isPreview bool) error
}

// OperationPlan contains the details of a planned subscription change
type OperationPlan struct {
    ProrationResult *proration.ProrationResult
    InvoiceLineItems []dto.CreateInvoiceLineItemRequest
    UpdatedSubscription *subscription.Subscription
    Errors []error
}

// SubscriptionUpdateOrchestrator manages the end-to-end process of updating subscriptions
type SubscriptionUpdateOrchestrator interface {
    // ProcessUpdate handles the entire update workflow
    ProcessUpdate(ctx context.Context, subID string, req dto.UpdateSubscriptionRequest, isPreview bool) 
        (*dto.UpdateSubscriptionResponse, error)
}
```

### 9.3 Operation Types

We'll implement different operation types as concrete implementations of the `SubscriptionChangeOperation` interface:

1. **AddLineItemOperation**: Adds a new line item to the subscription
2. **RemoveLineItemOperation**: Removes an existing line item
3. **UpdateLineItemOperation**: Changes a line item's price or quantity
4. **UpdateMetadataOperation**: Updates subscription metadata
5. **ChangeCancellationOperation**: Changes cancellation settings

Each operation follows the same pattern:
- Implements `Plan()` to calculate the impact
- Implements `Execute()` to apply the change if not a preview

### 9.4 Implementation Details

#### 9.4.1 Subscription Update Orchestrator

The orchestrator is responsible for parsing the request, creating and planning operations, and then either executing them or returning a preview:

```go
func (o *subscriptionUpdateOrchestrator) ProcessUpdate(
    ctx context.Context, 
    subID string, 
    req dto.UpdateSubscriptionRequest, 
    isPreview bool,
) (*dto.UpdateSubscriptionResponse, error) {
    // Get the subscription
    sub, err := o.SubRepo.GetWithLineItems(ctx, subID)
    if err != nil {
        return nil, err
    }
    
    // Parse request into operations
    operations, err := o.createOperationsFromRequest(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // Plan all operations
    plans, err := o.planOperations(ctx, sub, operations)
    if err != nil {
        return nil, err
    }
    
    // For preview mode, just return the planned changes
    if isPreview {
        return o.createPreviewResponse(sub, plans)
    }
    
    // Use consistent transaction pattern for actual execution
    var updatedSub *subscription.Subscription
    var invoice *dto.InvoiceResponse
    
    err = o.DB.WithTx(ctx, func(ctx context.Context) error {
        // Execute the operations in transaction context
        var execErr error
        updatedSub, invoice, execErr = o.executeOperations(ctx, sub, operations, plans)
        return execErr
    })
    
    if err != nil {
        return nil, err
    }
    
    // Create response
    return o.createResponse(updatedSub, invoice, plans)
}
```

#### 9.4.2 Operation Planning

Planning calculates the impact of an operation without executing it:

```go
func (o *subscriptionUpdateOrchestrator) planOperations(
    ctx context.Context,
    sub *subscription.Subscription,
    operations []SubscriptionChangeOperation,
) ([]*OperationPlan, error) {
    plans := make([]*OperationPlan, 0, len(operations))
    
    // Create a working copy of the subscription for planning
    workingSub := sub.Clone()
    
    for _, op := range operations {
        plan, err := op.Plan(ctx, workingSub)
        if err != nil {
            return nil, err
        }
        
        plans = append(plans, plan)
        
        // Update the working subscription for next operations
        // This ensures operations are planned in the context of previous ones
        if plan.UpdatedSubscription != nil {
            workingSub = plan.UpdatedSubscription
        }
    }
    
    return plans, nil
}
```

#### 9.4.3 Example Operation Implementation

Here's how an operation for adding a line item would be implemented, leveraging existing `ProrationParams`:

```go
type AddLineItemOperation struct {
    // Reuse existing ProrationParams structure
    ProrationParams *proration.ProrationParams
    
    // Add only additional fields not covered by ProrationParams
    SubRepo         subscription.SubscriptionRepo
    PriceService    PriceService
}

func NewAddLineItemOperation(
    priceID string, 
    quantity decimal.Decimal,
    prorationDate time.Time,
    prorationBehavior types.ProrationBehavior,
    prorationStrategy types.ProrationStrategy,
    subRepo subscription.SubscriptionRepo,
    prorationService ProrationService,
    priceService PriceService,
) *AddLineItemOperation {
    return &AddLineItemOperation{
        ProrationParams: &proration.ProrationParams{
            NewPriceID:        &priceID,
            NewQuantity:       &quantity,
            ProrationDate:     prorationDate,
            ProrationBehavior: prorationBehavior,
            ProrationStrategy: prorationStrategy,
            Action:            types.ProrationActionAddItem,
        },
        SubRepo:         subRepo,
        PriceService:    priceService,
    }
}

func (o *AddLineItemOperation) Plan(
    ctx context.Context,
    sub *subscription.Subscription,
) (*OperationPlan, error) {
    // Validate operation
    price, err := o.PriceService.GetPrice(ctx, *o.ProrationParams.NewPriceID)
    if err != nil {
        return nil, err
    }
    
    // Calculate proration using the wrapped ProrationParams
    prorationResult, err := o.ProrationService.CalculateProration(ctx, sub, o.ProrationParams)
    if err != nil {
        return nil, err
    }
    
    // Create a copy of the subscription with the new item
    updatedSub := sub.Clone()
    newLineItem := &subscription.SubscriptionLineItem{
        ID:             uuid.NewString(), // Temporary ID for preview
        PriceID:        *o.ProrationParams.NewPriceID,
        Quantity:       *o.ProrationParams.NewQuantity,
        InvoiceCadence: price.InvoiceCadence,
        // Other fields...
    }
    updatedSub.LineItems = append(updatedSub.LineItems, newLineItem)
    
    // Return the plan
    return &OperationPlan{
        ProrationResult:     prorationResult,
        UpdatedSubscription: updatedSub,
    }, nil
}

func (o *AddLineItemOperation) Execute(
    ctx context.Context,
    sub *subscription.Subscription,
    plan *OperationPlan,
    isPreview bool,
) error {
    if isPreview {
        // No actual execution needed for preview
        return nil
    }
    
    // Create the new line item in database
    newLineItem := &subscription.SubscriptionLineItem{
        SubscriptionID: sub.ID,
        PriceID:        *o.ProrationParams.NewPriceID,
        Quantity:       *o.ProrationParams.NewQuantity,
        // Other fields...
    }
    
    if err := o.SubRepo.AddLineItem(ctx, newLineItem); err != nil {
        return err
    }
    
    // Apply proration if needed
    if plan.ProrationResult != nil && o.ProrationParams.ProrationBehavior != types.ProrationBehaviorNone {
        if err := o.ProrationService.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams.ProrationBehavior); err != nil {
            return err
        }
    }
    
    return nil
}
```

Similarly, other operations like `RemoveLineItemOperation` and `UpdateLineItemOperation` would follow the same pattern of wrapping the `ProrationParams` structure.

### 9.5 Factory Pattern for Operations

To make the creation of operations cleaner, we'll use factory methods:

```go
// OperationFactory creates subscription change operations
type OperationFactory interface {
    // CreateFromRequest creates operations from an API request
    CreateFromRequest(ctx context.Context, req dto.UpdateSubscriptionRequest) ([]SubscriptionChangeOperation, error)
    
    // CreateAddLineItemOperation creates an operation to add a line item
    CreateAddLineItemOperation(priceID string, quantity decimal.Decimal, prorationOpts *proration.ProrationParams) SubscriptionChangeOperation
    
    // CreateRemoveLineItemOperation creates an operation to remove a line item
    CreateRemoveLineItemOperation(lineItemID string, prorationOpts *proration.ProrationParams) SubscriptionChangeOperation
    
    // CreateCancellationOperation creates an operation to cancel a subscription
    CreateCancellationOperation(cancelAtPeriodEnd bool, prorationOpts *proration.ProrationParams) SubscriptionChangeOperation
    
    // Other factory methods...
}
```

### 9.6 Preview and Actual Execution Path

The key insight is that preview and actual execution will use almost identical code paths:

```go
// For preview
func (s *subscriptionService) PreviewSubscriptionUpdate(ctx context.Context, subID string, req dto.UpdateSubscriptionRequest) (*dto.UpdateSubscriptionResponse, error) {
    return s.Orchestrator.ProcessUpdate(ctx, subID, req, true)
}

// For actual update
func (s *subscriptionService) UpdateSubscription(ctx context.Context, subID string, req dto.UpdateSubscriptionRequest) (*dto.UpdateSubscriptionResponse, error) {
    return s.Orchestrator.ProcessUpdate(ctx, subID, req, false)
}
```

### 9.7 Benefits of This Approach

1. **DRY**: Preview and actual execution share 90%+ of their code
2. **Maintainable**: Each operation is a self-contained unit with clear responsibilities
3. **Testable**: Operations can be unit-tested in isolation
4. **Extensible**: New operation types can be added without changing the orchestrator
5. **Reduced Conditionals**: Strategy pattern eliminates complex if-else chains
6. **Clear Workflow**: Distinct phases make the overall process easier to understand
7. **Reuses Existing Structures**: By wrapping ProrationParams, we minimize duplication with existing code

### 9.8 Subscription Cancellation and Resumption

Cancellation and resumption are special cases of subscription updates that can leverage the same architecture:

```go
func (s *subscriptionService) CancelSubscription(ctx context.Context, subID string, req dto.CancelSubscriptionRequest) (*dto.SubscriptionResponse, error) {
    // Create a specialized cancellation operation
    cancelOp := s.OperationFactory.CreateCancellationOperation(req.CancelAtPeriodEnd, req.ProrationOpts)
    
    // Process as a regular update with this single operation
    updateReq := dto.UpdateSubscriptionRequest{
        // Minimal fields needed for the orchestrator
    }
    
    response, err := s.Orchestrator.ProcessUpdateWithOperations(ctx, subID, updateReq, []SubscriptionChangeOperation{cancelOp}, false)
    if err != nil {
        return nil, err
    }
    
    return &dto.SubscriptionResponse{
        Subscription: response.Subscription,
    }, nil
}
```

### 9.9 Revised Implementation Plan

We'll implement this solution incrementally, starting with the simplest operations to validate the architecture:

1. **Phase 1: Core Infrastructure and Cancellation**
   - Implement the core interfaces (`SubscriptionChangeOperation`, `OperationPlan`, `SubscriptionUpdateOrchestrator`)
   - Implement the `CancellationOperation` as the first operation type
   - Build the orchestrator with support for a single operation
   - Implement the `CancelSubscription` method using this architecture
   - This phase will validate the core architecture with minimal complexity

2. **Phase 2: Basic Line Item Operations**
   - Implement `UpdateLineItemOperation` for changing quantity (simpler than price changes)
   - Extend the orchestrator to handle multiple operations
   - Add support for the basic update subscription endpoint

3. **Phase 3: Advanced Line Item Operations**
   - Implement `AddLineItemOperation` and `RemoveLineItemOperation`
   - Implement price change functionality in `UpdateLineItemOperation`
   - Complete the full update subscription endpoint

4. **Phase 4: Advanced Features**
   - Implement metadata operations
   - Add support for pending changes and effective dates
   - Implement the resumption functionality

This phased approach allows us to validate the architecture with simpler cases first before building more complex functionality, ensuring the design scales well as we add features.

## 10. Conclusion

This revised architecture addresses the maintainability concerns with a pattern that:

1. **Eliminates Duplication**: By using the same code path for preview and execution
2. **Simplifies Logic**: By using strategy pattern instead of complex conditionals
3. **Improves Testability**: With small, focused components
4. **Enhances Maintainability**: Making it easier for new engineers to understand and modify
5. **Follows Best Practices**: Using well-known design patterns for flexibility
6. **Reuses Existing Code**: By wrapping ProrationParams rather than duplicating fields
7. **Uses Consistent Patterns**: Aligns with existing codebase patterns like WithTx

The result is a system that new team members can understand quickly, with clear boundaries between components and a predictable flow from request to execution. 