# Subscription Proration and Workflow Implementation

## 1. Introduction

This document outlines a phased, domain-driven approach to implementing subscription prorations and workflows in FlexPrice. Proration is a critical billing component that ensures customers are charged accurately when their subscriptions change mid-billing cycle. This guide provides technical implementation details for handling various subscription change scenarios with a focus on maintainability, testability, and scalability. The design incorporates industry best practices while addressing common edge cases and complex billing scenarios.

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

#### 2.3.3 Timezone-Aware Calculations

Proration calculations must account for the customer's timezone to ensure accurate billing periods:

```go
func calculateRemainingDays(terminationDate time.Time, periodEndDate time.Time, customerTimezone string) int {
    // Convert dates to customer's timezone
    loc, _ := time.LoadLocation(customerTimezone)
    terminationInTZ := terminationDate.In(loc)
    periodEndInTZ := periodEndDate.In(loc)
    
    // Calculate days difference
    return daysBetween(terminationInTZ, periodEndInTZ)
}
```

For both strategies, the proration calculations follow the same pattern:

For a credit:
```
credit_amount = original_price * proration_coefficient
```

For a charge:
```
charge_amount = new_price * proration_coefficient
```

#### 2.3.4 Multiple Changes Protection

When multiple subscription changes occur in a single billing period, we must ensure the credited amount never exceeds the original payment:

```go
// Cap credit amount to the original subscription fee
if creditAmount.GreaterThan(originalSubscriptionFee) {
    creditAmount = originalSubscriptionFee
}

// Account for previous credits on this subscription fee
creditAmount = creditAmount.Sub(previousCreditsIssued)
if creditAmount.LessThanOrEqual(decimal.Zero) {
    return nil // No further credit if already fully credited
}
```

## 3. Domain-Driven Architecture

After careful evaluation, we've developed an architecture focused on maintainability, testability, and clear separation of concerns. The key innovation is modeling subscription changes as discrete operations that follow a consistent lifecycle.

### 3.1 Core Design Principles

1. **Separation of Concerns**: Clear distinction between planning and execution phases
2. **Strategy Pattern**: Polymorphic operations instead of complex conditionals
3. **Single Execution Path**: Unified code flow for both preview and actual execution
4. **Composability**: Complex operations built from simple, reusable components
5. **Termination Context**: Explicitly tracking why a subscription is being terminated
6. **Billing Mode Awareness**: Different workflows for pay-in-advance vs. pay-in-arrears models

### 3.2 Architecture Overview

The architecture introduces the **SubscriptionChangeOperation** concept, representing a single atomic change to a subscription. The workflow follows these steps:

1. **Parse Request**: Convert API request into a list of operations
2. **Plan Operations**: Calculate the details and impacts of each operation
3. **Preview** (optional): Return the planned changes without executing them
4. **Execute**: Apply the planned changes if this is not a preview

This approach ensures that preview and actual execution share nearly 100% of their logic.

#### 3.2.1 Core Interfaces

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
    PendingSubscription *subscription.Subscription // For delayed downgrades
    Errors []error
}

// SubscriptionUpdateOrchestrator manages the end-to-end process of updating subscriptions
type SubscriptionUpdateOrchestrator interface {
    // ProcessUpdate handles the entire update workflow
    ProcessUpdate(ctx context.Context, subID string, req dto.UpdateSubscriptionRequest, isPreview bool) 
        (*dto.UpdateSubscriptionResponse, error)
}

// TerminationReason represents why a subscription is being terminated
type TerminationReason string

const (
    TerminationReasonUpgrade      TerminationReason = "upgrade"
    TerminationReasonDowngrade    TerminationReason = "downgrade"
    TerminationReasonCancellation TerminationReason = "cancellation"
    TerminationReasonExpiration   TerminationReason = "expiration"
)

// BillingMode represents when a subscription is billed
type BillingMode string

const (
    BillingModeInAdvance BillingMode = "in_advance"
    BillingModeInArrears BillingMode = "in_arrears"
)

// ScheduleType determines when subscription changes take effect
type ScheduleType string

const (
    ScheduleTypeImmediate ScheduleType = "immediate"
    ScheduleTypePeriodEnd ScheduleType = "period_end"
    ScheduleTypeSpecificDate ScheduleType = "specific_date"
)
```

### 3.3 Operation Types

We'll implement different operation types as concrete implementations of the `SubscriptionChangeOperation` interface:

1. **AddLineItemOperation**: Adds a new line item to the subscription
2. **RemoveLineItemOperation**: Removes an existing line item
3. **UpdateLineItemOperation**: Changes a line item's price or quantity
4. **UpdateMetadataOperation**: Updates subscription metadata
5. **ChangeCancellationOperation**: Changes cancellation settings

Each operation follows the same pattern:
- Implements `Plan()` to calculate the impact
- Implements `Execute()` to apply the change if not a preview

## 4. Proration Service Implementation

The core proration functionality will be implemented through a dedicated service:

```go
type ProrationService interface {
    // CalculateProration calculates the proration for changing a subscription line item
    // Returns ProrationResult containing credit and charge items
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
    LineItemID           string           // ID of the line item being changed (can be empty for add_item action)
    OldPriceID           string           // Old price ID (empty for add_item)
    NewPriceID           string           // New price ID (empty for cancellation or remove_item)
    OldQuantity          decimal.Decimal  // Old quantity (zero for add_item)
    NewQuantity          decimal.Decimal  // New quantity
    ProrationDate        time.Time        // When the proration takes effect
    ProrationBehavior    ProrationBehavior
    ProrationStrategy    ProrationStrategy // Day-based or second-based
    Action               ProrationAction   // Type of change being performed
    BillingMode          BillingMode       // Whether subscription is billed in advance or arrears
    LastAmountPaid       decimal.Decimal   // Amount previously paid for the current period
    PreviousCreditsIssued decimal.Decimal  // Sum of credits already issued for this period
    CustomerTimezone     string           // Customer's timezone for accurate date calculations
    TerminationReason    TerminationReason // Why subscription is being terminated (for credit issuance logic)
    ScheduleType         ScheduleType      // When the change should take effect
    ScheduleDate         time.Time         // Specific date for scheduled changes (if applicable)
    HasScheduleDate      bool              // Whether a schedule date is specified
}

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
```

## 5. Implementation Approach

### 5.1 Factory Pattern for Operations

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

### 5.2 Orchestrator Implementation

The orchestrator coordinates the entire workflow:

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

### 5.3 Sample Operation Implementation

Here's how an operation for adding a line item would be implemented:

```go
type AddLineItemOperation struct {
    ProrationParams *proration.ProrationParams
    SubRepo         subscription.SubscriptionRepo
    PriceService    PriceService
    ProrationService ProrationService
}

func (o *AddLineItemOperation) Plan(
    ctx context.Context,
    sub *subscription.Subscription,
) (*OperationPlan, error) {
    // Validate operation
    price, err := o.PriceService.GetPrice(ctx, o.ProrationParams.NewPriceID)
    if err != nil {
        return nil, err
    }
    
    // Calculate proration
    prorationResult, err := o.ProrationService.CalculateProration(ctx, sub, o.ProrationParams)
    if err != nil {
        return nil, err
    }
    
    // Create a copy of the subscription with the new item
    updatedSub := sub.Clone()
    newLineItem := &subscription.SubscriptionLineItem{
        ID:             uuid.NewString(), // Temporary ID for preview
        PriceID:        o.ProrationParams.NewPriceID,
        Quantity:       o.ProrationParams.NewQuantity,
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
    
    // If this change should be scheduled for future (like period end),
    // we need to handle it differently
    if o.ProrationParams.ScheduleType == ScheduleTypeSpecificDate ||
       o.ProrationParams.ScheduleType == ScheduleTypePeriodEnd {
        return o.scheduleChange(ctx, sub, plan)
    }
    
    // Create the new line item in database
    newLineItem := &subscription.SubscriptionLineItem{
        SubscriptionID: sub.ID,
        PriceID:        o.ProrationParams.NewPriceID,
        Quantity:       o.ProrationParams.NewQuantity,
        // Other fields...
    }
    
    if err := o.SubRepo.AddLineItem(ctx, newLineItem); err != nil {
        return err
    }
    
    // Apply proration if needed
    if plan.ProrationResult != nil && o.ProrationParams.ProrationBehavior != types.ProrationBehaviorNone {
        if err := o.ProrationService.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams); err != nil {
            return err
        }
    }
    
    // Send webhook notification
    o.notifySubscriptionUpdated(ctx, sub)
    
    return nil
}

// scheduleChange creates a pending change that will be applied at the specified time
func (o *AddLineItemOperation) scheduleChange(
    ctx context.Context, 
    sub *subscription.Subscription,
    plan *OperationPlan,
) error {
    var effectiveDate time.Time
    
    if o.ProrationParams.ScheduleType == ScheduleTypeSpecificDate && o.ProrationParams.HasScheduleDate {
        effectiveDate = o.ProrationParams.ScheduleDate
    } else { // ScheduleTypePeriodEnd
        effectiveDate = sub.CurrentPeriodEnd
    }
    
    // Create a pending change
    pendingChange := &subscription.PendingChange{
        SubscriptionID: sub.ID,
        Type:           "add_line_item",
        EffectiveDate:  effectiveDate,
        Data: map[string]interface{}{
            "price_id": o.ProrationParams.NewPriceID,
            "quantity": o.ProrationParams.NewQuantity,
        },
    }
    
    return o.SubRepo.AddPendingChange(ctx, pendingChange)
}

func (o *AddLineItemOperation) notifySubscriptionUpdated(ctx context.Context, sub *subscription.Subscription) {
    // Send webhook notification about subscription update
    // Implementation details omitted for brevity
}
```

## 6. Integration with Existing Services

### 6.1 Service Layer Integration

The subscription service will expose a clean API for both previewing and applying changes:

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

### 6.2 Special Operations

Cancellation and resumption are special cases that leverage the same architecture:

```go
func (s *subscriptionService) CancelSubscription(ctx context.Context, subID string, req dto.CancelSubscriptionRequest) (*dto.SubscriptionResponse, error) {
    // Get the subscription to determine its billing mode
    sub, err := s.SubRepo.GetWithLineItems(ctx, subID)
    if err != nil {
        return nil, err
    }
    
    // Prepare proration parameters with enhanced context
    prorationOpts := req.ProrationOpts
    if prorationOpts == nil {
        prorationOpts = &proration.ProrationParams{}
    }
    
    // Set termination reason and billing mode
    prorationOpts.TerminationReason = TerminationReasonCancellation
    prorationOpts.BillingMode = determineBillingMode(sub)
    prorationOpts.LastAmountPaid = getLastPaidAmount(sub)
    prorationOpts.CustomerTimezone = sub.Customer.Timezone
    
    // Determine when the cancellation should take effect
    if req.CancelAtPeriodEnd {
        prorationOpts.ScheduleType = ScheduleTypePeriodEnd
    } else {
        prorationOpts.ScheduleType = ScheduleTypeImmediate
    }
    
    // Create a specialized cancellation operation
    cancelOp := s.OperationFactory.CreateCancellationOperation(prorationOpts)
    
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

// determineBillingMode examines the subscription to determine if it's billed in advance or arrears
func determineBillingMode(sub *subscription.Subscription) BillingMode {
    if sub.Plan.PayInAdvance {
        return BillingModeInAdvance
    }
    return BillingModeInArrears
}

// getLastPaidAmount retrieves the amount that was paid for the current billing period
func getLastPaidAmount(sub *subscription.Subscription) decimal.Decimal {
    // Implementation would look at subscription fees on the latest invoice
    // Simplified for this example
    return sub.TotalAmount
}
```

## 7. Phased Implementation Plan

We'll implement this solution incrementally, starting with the simplest operations:

1. **Phase 1: Core Infrastructure and Cancellation**
   - Implement the core interfaces (`SubscriptionChangeOperation`, `OperationPlan`, `SubscriptionUpdateOrchestrator`)
   - Implement the `CancellationOperation` as the first operation type
   - Build the orchestrator with support for a single operation
   - Implement the `CancelSubscription` method using this architecture
   - Ensure proper handling of billing modes (in-advance vs. in-arrears)
   - Implement protection for multiple changes within a period

2. **Phase 2: Basic Line Item Operations**
   - Implement `UpdateLineItemOperation` for changing quantity (simpler than price changes)
   - Extend the orchestrator to handle multiple operations
   - Implement timezone-aware proration calculations
   - Add support for the basic update subscription endpoint

3. **Phase 3: Advanced Line Item Operations**
   - Implement `AddLineItemOperation` and `RemoveLineItemOperation`
   - Implement price change functionality in `UpdateLineItemOperation`
   - Complete the full update subscription endpoint
   - Add support for scheduled changes (immediate, period end, specific date)

4. **Phase 4: Advanced Features**
   - Implement metadata operations
   - Add comprehensive support for pending changes
   - Implement subscription pause/resume functionality
   - Add support for trial periods and their impact on proration
   - Implement support for calendar vs. anniversary billing

## 8. Edge Cases and Special Considerations

### 8.1 Multiple Changes Within a Period

When a customer makes multiple subscription changes within a single billing period, we need to:

1. Track the total amount paid for the current period
2. Track credits already issued for this period
3. Cap credits to prevent over-crediting
4. Ensure all changes are properly sequenced

Implementation example:

```go
func (s *prorationService) CapCreditAmount(
    creditAmount decimal.Decimal, 
    originalAmount decimal.Decimal,
    previousCredits decimal.Decimal,
) decimal.Decimal {
    // Cap at original amount
    if creditAmount.GreaterThan(originalAmount) {
        creditAmount = originalAmount
    }
    
    // Reduce by previous credits
    creditAmount = creditAmount.Sub(previousCredits)
    
    // Ensure non-negative
    if creditAmount.LessThanOrEqual(decimal.Zero) {
        return decimal.Zero
    }
    
    return creditAmount
}
```

### 8.2 Downgrade vs. Cancellation Logic

Downgrades and cancellations require different handling:

1. Downgrades typically create a pending subscription activated at period end
2. Cancellations either terminate immediately or schedule termination at period end
3. Proper webhook notifications are sent for each transition state

### 8.3 Trial Periods

Trial periods introduce special considerations:

1. No proration needed for plan changes during trial
2. When upgrading from trial to paid, we calculate from the trial end date
3. Cancellation during trial should not generate credits

### 8.4 Calendar vs. Anniversary Billing

Supporting both billing models requires:

1. Different calculations for next billing date
2. Special handling of billing cycle alignment
3. Distinct proration coefficient calculations

## 9. Benefits of the Architecture

This architecture provides several key advantages:

1. **DRY Code**: Preview and actual execution share 90%+ of their code
2. **Maintainability**: Each operation is a self-contained unit with clear responsibilities
3. **Testability**: Operations can be unit-tested in isolation
4. **Extensibility**: New operation types can be added without changing the orchestrator
5. **Reduced Complexity**: Strategy pattern eliminates complex if-else chains
6. **Clear Workflow**: Distinct phases make the overall process easier to understand
7. **Improved Onboarding**: New team members can understand the system more quickly
8. **Transaction Safety**: Consistent transaction handling across all operations
9. **Edge Case Handling**: Robust handling of multiple changes, timezone differences, and billing modes
10. **Change Scheduling**: Built-in support for immediate, period-end, and date-specific changes

## 10. Testing Strategy

To ensure the proration system works correctly in all scenarios, we'll implement a comprehensive testing strategy:

### 10.1 Unit Tests

- Test individual operations in isolation
- Test proration calculations with various date/time scenarios
- Verify edge cases like multiple upgrades

Example test for multiple upgrades:

```go
func TestMultipleUpgrades(t *testing.T) {
    // Setup subscription with Plan A
    // Upgrade to Plan B mid-period
    // Verify credit amount for unused portion of Plan A
    // Upgrade to Plan C shortly after
    // Verify credit amount doesn't exceed original Plan B payment
    // Verify final state is correctly upgraded to Plan C
}
```

### 10.2 Integration Tests

- Test the full orchestration flow
- Verify transaction handling and rollbacks
- Test scheduled changes and their activation

### 10.3 Timezone Testing

- Test proration with customers in different timezones
- Verify billing period calculations across date boundaries
- Test daylight saving time transitions

## 11. Conclusion

This domain-driven approach to subscription proration focuses on:

1. Building a clean, maintainable architecture with clear separation of concerns
2. Using polymorphism to handle different operation types consistently
3. Sharing code between preview and execution paths
4. Implementing features incrementally, starting with the simplest cases
5. Handling advanced edge cases like multiple changes and timezone differences

The result is a system that strikes the right balance between flexibility, performance, and maintainability, while ensuring accurate proration calculations in all scenarios. By incorporating best practices from industry leaders while maintaining a domain-driven design approach, this implementation provides a robust foundation for subscription management that can adapt to evolving business requirements.