# Subscription Invoice Creation PRD

## Overview

This document outlines the design and implementation for a composable, scalable, and extensible subscription invoice creation system. The system will handle various billing scenarios based on invoice cadence (advanced or arrear) and price types (fixed or usage).

## Problem Statement

The current subscription billing system needs to handle multiple scenarios for invoice creation:

1. **Fixed Charge Advanced Billing**: Create an invoice with the full subscription line item amount as soon as the current period starts (i.e., when subscription is created or a period changes).
2. **Fixed Charge Arrear Billing**: Charge when processing subscription period updates (i.e., when the current period ends).
3. **Usage Charge Arrear Billing**: Calculate usage for the entire period when processing subscription period updates.
4. **Usage Charge Advanced Billing**: Handle various invoicing strategies:
   - Invoice each fee upon event reception (default)
   - Regroup all fees in a single invoice at the end of the period
   - Do not invoice any fees

Additionally, the system needs to support preview invoice generation to give customers a sneak peek of their next invoice.

## Goals

1. Create a composable and extensible invoice creation system
2. Support all billing scenarios (fixed/usage charges with advanced/arrear billing)
3. Implement preview invoice generation
4. Handle subscription period transitions correctly
5. Ensure proper handling of paused subscriptions
6. Maintain backward compatibility with existing code

## Non-Goals

1. Changing the existing data models
2. Modifying the payment processing system
3. Implementing new billing models beyond the current requirements

## Technical Design

### Core Components

1. **InvoiceStrategy Interface**: A strategy pattern to encapsulate different invoicing behaviors
2. **BillingEligibilityService**: Determines which line items are eligible for billing
3. **InvoicePreviewService**: Generates preview invoices
4. **Enhanced BillingService**: Improved service with strategy-based invoice creation

### Detailed Design

#### 1. InvoiceStrategy Interface

```go
// InvoiceStrategy defines how line items should be invoiced
type InvoiceStrategy interface {
    // IsEligibleForInvoicing determines if a line item should be invoiced at the current time
    IsEligibleForInvoicing(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                          periodStart, periodEnd, now time.Time) bool
    
    // IsEligibleForPreview determines if a line item should be included in a preview invoice
    IsEligibleForPreview(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                        periodStart, periodEnd, now time.Time) bool
    
    // CalculateAmount calculates the amount to be invoiced
    CalculateAmount(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                   usage *dto.GetUsageBySubscriptionResponse, 
                   periodStart, periodEnd time.Time) (decimal.Decimal, error)
}
```

#### 2. Concrete Strategy Implementations

```go
// FixedAdvancedStrategy handles fixed charges with advanced billing
type FixedAdvancedStrategy struct {
    priceService PriceService
}

// FixedArrearStrategy handles fixed charges with arrear billing
type FixedArrearStrategy struct {
    priceService PriceService
}

// UsageArrearStrategy handles usage charges with arrear billing
type UsageArrearStrategy struct {
    usageService UsageService
}

// UsageAdvancedStrategy handles usage charges with advanced billing
type UsageAdvancedStrategy struct {
    usageService UsageService
    // Configuration for invoicing strategy (per event, end of period, etc.)
    invoicingMode UsageInvoicingMode
}
```

#### 3. Strategy Factory

```go
// InvoiceStrategyFactory creates the appropriate strategy based on line item properties
type InvoiceStrategyFactory struct {
    priceService PriceService
    usageService UsageService
}

func (f *InvoiceStrategyFactory) CreateStrategy(lineItem *subscription.SubscriptionLineItem) InvoiceStrategy {
    if lineItem.PriceType == types.PRICE_TYPE_FIXED {
        if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
            return &FixedAdvancedStrategy{priceService: f.priceService}
        }
        return &FixedArrearStrategy{priceService: f.priceService}
    } else { // PRICE_TYPE_USAGE
        if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
            return &UsageAdvancedStrategy{usageService: f.usageService}
        }
        return &UsageArrearStrategy{usageService: f.usageService}
    }
}
```

#### 4. Enhanced BillingService

The existing BillingService will be enhanced to use the strategy pattern:

```go
func (s *billingService) GetLineItemsToBeInvoiced(
    ctx context.Context,
    sub *subscription.Subscription,
    now time.Time,
) ([]*subscription.SubscriptionLineItem, error) {
    lineItemsToBeInvoiced := make([]*subscription.SubscriptionLineItem, 0)
    
    // Check subscription status, etc. (existing code)
    
    // Create strategy factory
    strategyFactory := NewInvoiceStrategyFactory(s.PriceService, s.UsageService)
    
    // Check existing invoices (existing code)
    
    for _, lineItem := range sub.LineItems {
        // Get appropriate strategy for this line item
        strategy := strategyFactory.CreateStrategy(lineItem)
        
        // Check if eligible for invoicing
        if strategy.IsEligibleForInvoicing(ctx, lineItem, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, now) {
            // Check if already invoiced (existing code)
            lineItemsToBeInvoiced = append(lineItemsToBeInvoiced, lineItem)
        }
    }
    
    return lineItemsToBeInvoiced, nil
}
```

#### 5. Preview Invoice Generation

```go
func (s *billingService) PrepareSubscriptionInvoiceRequest(
    ctx context.Context,
    sub *subscription.Subscription,
    periodStart,
    periodEnd time.Time,
    isPreview bool,
) (*dto.CreateInvoiceRequest, error) {
    // Create strategy factory
    strategyFactory := NewInvoiceStrategyFactory(s.PriceService, s.UsageService)
    
    // Get usage data (existing code)
    
    // For preview invoices, we need to determine which line items to include
    lineItemsToInclude := make([]*subscription.SubscriptionLineItem, 0)
    
    for _, lineItem := range sub.LineItems {
        strategy := strategyFactory.CreateStrategy(lineItem)
        
        if isPreview {
            // For preview, use the preview eligibility check
            if strategy.IsEligibleForPreview(ctx, lineItem, periodStart, periodEnd, time.Now()) {
                lineItemsToInclude = append(lineItemsToInclude, lineItem)
            }
        } else {
            // For actual invoices, use the normal eligibility check
            if strategy.IsEligibleForInvoicing(ctx, lineItem, periodStart, periodEnd, time.Now()) {
                lineItemsToInclude = append(lineItemsToInclude, lineItem)
            }
        }
    }
    
    // Calculate charges for included line items
    // Create invoice request (existing code with modifications)
    
    return req, nil
}
```

### Strategy Implementation Details

#### Fixed Advanced Strategy

```go
func (s *FixedAdvancedStrategy) IsEligibleForInvoicing(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                     periodStart, periodEnd, now time.Time) bool {
    // Invoice as soon as the period starts
    return now.After(periodStart) || now.Equal(periodStart)
}

func (s *FixedAdvancedStrategy) IsEligibleForPreview(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                   periodStart, periodEnd, now time.Time) bool {
    // Include in preview for the next period
    return true
}
```

#### Fixed Arrear Strategy

```go
func (s *FixedArrearStrategy) IsEligibleForInvoicing(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                   periodStart, periodEnd, now time.Time) bool {
    // Invoice at the end of the period
    return now.After(periodEnd) || now.Equal(periodEnd)
}

func (s *FixedArrearStrategy) IsEligibleForPreview(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                 periodStart, periodEnd, now time.Time) bool {
    // Include in preview
    return true
}
```

#### Usage Arrear Strategy

```go
func (s *UsageArrearStrategy) IsEligibleForInvoicing(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                   periodStart, periodEnd, now time.Time) bool {
    // Invoice at the end of the period
    return now.After(periodEnd) || now.Equal(periodEnd)
}

func (s *UsageArrearStrategy) IsEligibleForPreview(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                 periodStart, periodEnd, now time.Time) bool {
    // Include in preview
    return true
}
```

#### Usage Advanced Strategy

```go
func (s *UsageAdvancedStrategy) IsEligibleForInvoicing(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                     periodStart, periodEnd, now time.Time) bool {
    switch s.invoicingMode {
    case UsageInvoicingModePerEvent:
        // Invoice each fee upon event reception
        return true
    case UsageInvoicingModeEndOfPeriod:
        // Regroup all fees at the end of the period
        return now.After(periodEnd) || now.Equal(periodEnd)
    case UsageInvoicingModeNoInvoice:
        // Do not invoice any fees
        return false
    default:
        // Default to per-event invoicing
        return true
    }
}

func (s *UsageAdvancedStrategy) IsEligibleForPreview(ctx context.Context, lineItem *subscription.SubscriptionLineItem, 
                                                   periodStart, periodEnd, now time.Time) bool {
    // Skip usage advanced charges from preview
    return false
}
```

## Implementation Plan

### Phase 1: Core Strategy Pattern

1. Define the InvoiceStrategy interface and concrete implementations
2. Create the InvoiceStrategyFactory
3. Refactor BillingService.GetLineItemsToBeInvoiced to use strategies
4. Add unit tests for each strategy

### Phase 2: Preview Invoice Enhancement

1. Refactor BillingService.PrepareSubscriptionInvoiceRequest to use strategies
2. Implement IsEligibleForPreview in each strategy
3. Add unit tests for preview invoice generation

### Phase 3: Usage Advanced Billing Strategies

1. Implement different invoicing modes for UsageAdvancedStrategy
2. Add configuration options for usage invoicing
3. Update documentation and add examples

### Phase 4: Integration and Testing

1. Integrate with existing subscription period processing
2. Add integration tests for all billing scenarios
3. Verify backward compatibility

## Nuances and Edge Cases

### Subscription Pauses

When a subscription is paused, we need to handle billing appropriately:

1. For fixed advanced charges, we should not invoice for periods when the subscription is paused
2. For usage charges, we should only calculate usage for active periods

### Subscription Cancellations

When a subscription is canceled:

1. For fixed advanced charges, we may need to issue refunds or credits for unused portions
2. For arrear charges, we should still invoice for usage up to the cancellation date

### Proration

When subscription changes occur mid-period:

1. Fixed charges may need to be prorated
2. Usage charges should be calculated correctly for partial periods

### Currency Changes

If a subscription's currency changes:

1. Ensure all line items use the correct currency
2. Handle any necessary conversions

## Metrics and Monitoring

1. Track invoice creation success/failure rates
2. Monitor time taken to generate invoices
3. Track usage calculation performance
4. Monitor preview invoice accuracy

## Future Enhancements

1. Support for more complex billing models (e.g., tiered pricing)
2. Enhanced preview capabilities with projected usage
3. Support for custom billing rules per customer
4. Integration with more payment providers

## Conclusion

This design provides a flexible, extensible framework for subscription invoice creation that handles all the required billing scenarios. By using the strategy pattern, we can easily add new billing strategies in the future without modifying existing code. The separation of invoicing eligibility from amount calculation also allows for more precise control over when and how charges are invoiced. 