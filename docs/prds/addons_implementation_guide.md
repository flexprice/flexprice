# Add-ons Implementation Guide

## Overview

This implementation guide provides detailed function-level breakdowns for integrating add-ons into the existing FlexPrice subscription billing system. The guide follows the existing architecture patterns and provides specific implementation details for each major function.

## 🔧 Function-Level Breakdown

### 1. Add-on Catalog Setup

#### 1.1 CreateAddon Function

**Function Name**: `CreateAddon`  
**Responsibility**: Create a new addon with associated prices and entitlements  
**Location**: `internal/service/addon.go`

```go
func (s *addonService) CreateAddon(ctx context.Context, req *dto.CreateAddonRequest) (*dto.AddonResponse, error) {
    // Input: CreateAddonRequest with name, type, prices, entitlements
    // Output: AddonResponse with created addon details

    // 1. Validate addon data (name, type, dependencies)
    // 2. Create addon record in database
    // 3. Create associated prices (if provided)
    // 4. Create associated entitlements (if provided)
    // 5. Return response with expanded data
}
```

**When Called**:

- Admin creates new addon in catalog
- API endpoint: `POST /api/v1/addons`

**Dependencies**:

- `AddonRepository.Create()`
- `PriceService.CreatePrice()` (for addon prices)
- `EntitlementService.CreateEntitlement()` (for addon entitlements)

### 2. Subscription Creation (with Add-ons)

#### 2.1 Enhanced CreateSubscription Function

**Function Name**: `CreateSubscription` (Enhanced)  
**Responsibility**: Create subscription with optional addons  
**Location**: `internal/service/subscription.go`

```go
func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
    // Input: CreateSubscriptionRequest with optional Addons field
    // Output: SubscriptionResponse with addon information

    // EXISTING BEHAVIOR (for plan-only subs):
    // 1. Validate customer and plan
    // 2. Create subscription with plan line items
    // 3. Handle credit grants
    // 4. Create invoice for advance charges

    // NEW BEHAVIOR (with addons):
    // 1. Validate addon compatibility with selected plan
    // 2. Validate addon dependencies and conflicts
    // 3. Create subscription with plan line items
    // 4. Create subscription_addons records
    // 5. Create addon line items
    // 6. Handle credit grants (plan + addon)
    // 7. Create invoice with plan + addon charges
    // 8. Send webhook events for addon assignments
}
```

**When Called**:

- Customer creates subscription with addons
- API endpoint: `POST /api/v1/subscriptions`

**New Dependencies**:

- `AddonService.ValidateAddonCompatibility()`
- `SubscriptionAddonRepository.Create()`
- `AddonEntitlementService.MergeWithPlanEntitlements()`

#### 2.2 ValidateAddonCompatibility Function

**Function Name**: `ValidateAddonCompatibility`  
**Responsibility**: Validate addon can be assigned to subscription  
**Location**: `internal/service/addon.go`

```go
func (s *addonService) ValidateAddonCompatibility(ctx context.Context, addonID string, subscription *subscription.Subscription) error {
    // Input: addonID, subscription
    // Output: error if incompatible

    // 1. Check addon exists and is active
    // 2. Validate addon type (single-instance vs multi-instance)
    // 3. Check for existing addon assignment (single-instance)
    // 4. Validate price compatibility (currency, billing period)
    // 5. Validate feature dependencies
    // 6. Check usage limit conflicts
}
```

### 3. Post-Creation Add-on Purchase

#### 3.1 AddAddonToSubscription Function

**Function Name**: `AddAddonToSubscription`  
**Responsibility**: Add addon to existing subscription  
**Location**: `internal/service/subscription.go`

```go
func (s *subscriptionService) AddAddonToSubscription(ctx context.Context, subscriptionID string, req *dto.AddAddonRequest) error {
    // Input: subscriptionID, AddAddonRequest
    // Output: error if failed

    // 1. Validate addon exists and is available
    // 2. Check if addon is already assigned (single-instance)
    // 3. Validate price compatibility with subscription
    // 4. Validate feature dependencies
    // 5. Create subscription addon record
    // 6. Create subscription line items for addon prices
    // 7. Calculate proration if mid-cycle
    // 8. Update entitlements with addon contributions
    // 9. Trigger billing recalculation
    // 10. Send webhook events
}
```

**When Called**:

- Customer adds addon mid-cycle
- API endpoint: `POST /api/v1/subscriptions/{id}/addons`

#### 3.2 CalculateAddonProration Function

**Function Name**: `CalculateAddonProration`  
**Responsibility**: Calculate prorated amount for addon changes  
**Location**: `internal/service/billing.go`

```go
func (s *billingService) CalculateAddonProration(ctx context.Context, subscription *subscription.Subscription, addon *SubscriptionAddon, changeDate time.Time) (decimal.Decimal, error) {
    // Input: subscription, addon, change date
    // Output: prorated amount

    // 1. Calculate remaining days in billing period
    // 2. Calculate daily rate for addon
    // 3. Apply proration behavior (create_prorations vs none)
    // 4. Return prorated amount
}
```

### 4. Entitlement Calculation

#### 4.1 Enhanced GetCustomerEntitlements Function

**Function Name**: `GetCustomerEntitlements` (Enhanced)  
**Responsibility**: Aggregate entitlements from plan + addons  
**Location**: `internal/service/billing.go`

```go
func (s *billingService) GetCustomerEntitlements(ctx context.Context, customerID string, req *dto.GetCustomerEntitlementsRequest) (*dto.CustomerEntitlementsResponse, error) {
    // EXISTING BEHAVIOR (plan-only):
    // 1. Get active subscriptions
    // 2. Get plan entitlements
    // 3. Aggregate by feature

    // NEW BEHAVIOR (with addons):
    // 1. Get active subscriptions
    // 2. Get plan entitlements
    // 3. Get addon entitlements
    // 4. Merge entitlements intelligently
    // 5. Return aggregated entitlements with sources
}
```

#### 4.2 MergeEntitlements Function

**Function Name**: `MergeEntitlements`  
**Responsibility**: Merge plan and addon entitlements  
**Location**: `internal/service/billing.go`

```go
func mergeEntitlements(planEntitlements []*entitlement.Entitlement, addonEntitlements []*entitlement.Entitlement) []*entitlement.Entitlement {
    // Input: plan entitlements, addon entitlements
    // Output: merged entitlements

    // For metered features: sum usage limits
    // For boolean features: enable if any source enables
    // For static features: collect all unique values
    // Handle conflicts and overrides
}
```

### 5. Usage Metering + Limits

#### 5.1 Enhanced GetUsageBySubscription Function

**Function Name**: `GetUsageBySubscription` (Enhanced)  
**Responsibility**: Calculate usage against combined entitlements  
**Location**: `internal/service/subscription.go`

```go
func (s *subscriptionService) GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error) {
    // EXISTING BEHAVIOR (plan-only):
    // 1. Get subscription line items
    // 2. Calculate usage for each meter

    // NEW BEHAVIOR (with addons):
    // 1. Get subscription line items (plan + addon)
    // 2. Get combined entitlements (plan + addon)
    // 3. Calculate usage against combined limits
    // 4. Apply overage charges based on combined limits
    // 5. Track usage by source (plan vs addon)
}
```

#### 5.2 TrackAddonUsage Function

**Function Name**: `TrackAddonUsage`  
**Responsibility**: Track usage against addon limits  
**Location**: `internal/service/addon.go`

```go
func (s *addonService) TrackAddonUsage(ctx context.Context, subscriptionID string, addonID string, usage decimal.Decimal) error {
    // Input: subscriptionID, addonID, usage amount
    // Output: error if tracking failed

    // 1. Get addon usage limits
    // 2. Check if usage exceeds limits
    // 3. Update usage tracking
    // 4. Send notifications for thresholds
    // 5. Trigger billing if limits exceeded
}
```

### 6. Billing and Invoicing

#### 6.1 Enhanced CalculateFixedCharges Function

**Function Name**: `CalculateFixedCharges` (Enhanced)  
**Responsibility**: Calculate fixed charges for plan + addons  
**Location**: `internal/service/billing.go`

```go
func (s *billingService) CalculateFixedCharges(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // EXISTING BEHAVIOR (plan-only):
    // 1. Process plan line items
    // 2. Calculate fixed charges

    // NEW BEHAVIOR (with addons):
    // 1. Process plan line items
    // 2. Process addon line items
    // 3. Handle proration for addon changes
    // 4. Apply discounts and credits
    // 5. Return fixed charges with proper attribution
}
```

#### 6.2 Enhanced CalculateUsageCharges Function

**Function Name**: `CalculateUsageCharges` (Enhanced)  
**Responsibility**: Calculate usage charges with addon contributions  
**Location**: `internal/service/billing.go`

```go
func (s *billingService) CalculateUsageCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // EXISTING BEHAVIOR (plan-only):
    // 1. Get plan entitlements
    // 2. Calculate usage charges

    // NEW BEHAVIOR (with addons):
    // 1. Get combined entitlements (plan + addon)
    // 2. Calculate usage against combined limits
    // 3. Apply overage charges based on combined limits
    // 4. Track usage by source for analytics
    // 5. Return usage charges with proper attribution
}
```

### 7. Add-on Cancellation / Modification

#### 7.1 RemoveAddonFromSubscription Function

**Function Name**: `RemoveAddonFromSubscription`  
**Responsibility**: Remove addon from subscription  
**Location**: `internal/service/subscription.go`

```go
func (s *subscriptionService) RemoveAddonFromSubscription(ctx context.Context, subscriptionID string, addonID string, req *dto.RemoveAddonRequest) error {
    // Input: subscriptionID, addonID, removal request
    // Output: error if removal failed

    // 1. Validate addon is assigned to subscription
    // 2. Calculate proration for unused period
    // 3. Set end date for addon
    // 4. Remove from active entitlements
    // 5. Recalculate billing with proration
    // 6. Generate credit/charge for unused period
    // 7. Send webhook events
    // 8. Update usage tracking
}
```

#### 7.2 UpdateSubscriptionAddon Function

**Function Name**: `UpdateSubscriptionAddon`  
**Responsibility**: Update addon quantity or price  
**Location**: `internal/service/subscription.go`

```go
func (s *subscriptionService) UpdateSubscriptionAddon(ctx context.Context, subscriptionID string, addonID string, req *dto.UpdateAddonRequest) error {
    // Input: subscriptionID, addonID, update request
    // Output: error if update failed

    // 1. Validate update request
    // 2. Calculate proration for changes
    // 3. Update addon quantity/price
    // 4. Update line items
    // 5. Update entitlements
    // 6. Recalculate billing
    // 7. Send webhook events
}
```

## 🔁 Add-on Lifecycle Phases

### Phase 1: Add-on Catalog Setup

```go
// 1. Create addon
addon, err := addonService.CreateAddon(ctx, &dto.CreateAddonRequest{
    Name: "Premium Support",
    Type: "single_instance",
    Prices: []dto.CreatePriceRequest{...},
    Entitlements: []dto.CreateEntitlementRequest{...},
})

// 2. Create addon prices
price, err := priceService.CreatePrice(ctx, &dto.CreatePriceRequest{
    AddonID: addon.ID,
    Amount: 50.00,
    Currency: "usd",
    BillingPeriod: "monthly",
})
```

### Phase 2: Subscription Creation (with Add-ons)

```go 
// 1. Create subscription with addons
subscription, err := subscriptionService.CreateSubscription(ctx, &dto.CreateSubscriptionRequest{
    CustomerID: "cust_123",
    PlanID: "plan_pro",
    Addons: []dto.SubscriptionAddonRequest{
        {
            AddonID: "addon_premium_support",
            PriceID: "price_premium_monthly",
            Quantity: 1,
        },
    },
})
```

### Phase 3: Post-Creation Add-on Purchase

```go
// 1. Add addon mid-cycle
err := subscriptionService.AddAddonToSubscription(ctx, "sub_123", &dto.AddAddonRequest{
    AddonID: "addon_analytics",
    PriceID: "price_analytics_monthly",
    Quantity: 1,
    ProrationBehavior: "create_prorations",
})
```

### Phase 4: Entitlement Calculation

```go
// 1. Get combined entitlements
entitlements, err := billingService.GetCustomerEntitlements(ctx, "cust_123", &dto.GetCustomerEntitlementsRequest{})

// 2. Entitlements include both plan and addon sources
for _, feature := range entitlements.Features {
    // Feature: API Calls
    // Total Limit: 10,000 (5,000 from plan + 5,000 from addon)
    // Sources: [plan, addon]
}
```

### Phase 5: Usage Metering + Limits

```go
// 1. Track usage against combined limits
err := addonService.TrackAddonUsage(ctx, "sub_123", "addon_extra_api", decimal.NewFromInt(1000))

// 2. Check usage against combined entitlements
usage, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
    SubscriptionID: "sub_123",
})
```

### Phase 6: Billing and Invoicing

```go
// 1. Calculate charges including addons
charges, err := billingService.CalculateFixedCharges(ctx, subscription, periodStart, periodEnd)

// 2. Generate invoice with separate line items
invoice, err := invoiceService.CreateInvoice(ctx, &dto.CreateInvoiceRequest{
    CustomerID: "cust_123",
    LineItems: []dto.CreateInvoiceLineItemRequest{
        // Plan line item
        {PlanID: "plan_pro", Amount: 29.99},
        // Addon line item
        {AddonID: "addon_premium_support", Amount: 50.00},
    },
})
```

### Phase 7: Add-on Cancellation / Modification

```go
// 1. Remove addon
err := subscriptionService.RemoveAddonFromSubscription(ctx, "sub_123", "addon_analytics", &dto.RemoveAddonRequest{
    EffectiveDate: time.Now(),
    ProrationBehavior: "create_prorations",
})

// 2. Update addon quantity
err := subscriptionService.UpdateSubscriptionAddon(ctx, "sub_123", "addon_extra_api", &dto.UpdateAddonRequest{
    Quantity: 2,
    ProrationBehavior: "create_prorations",
})
```

## 🧠 Codebase Deep Analysis

### Functions Requiring Add-on Integration

#### 1. CreateSubscription (Major Refactor)

**Current Behavior**: Creates subscription with plan line items only  
**Add-on Integration Points**:

- Add `Addons` field to `CreateSubscriptionRequest`
- Validate addon compatibility with plan
- Create `subscription_addons` records
- Create addon line items
- Merge entitlements from plan + addons
- Send webhook events for addon assignments

#### 2. GetCustomerEntitlements (Major Enhancement)

**Current Behavior**: Aggregates entitlements from plan only  
**Add-on Integration Points**:

- Query addon entitlements in addition to plan entitlements
- Implement `mergeEntitlements()` function
- Add addon sources to response
- Handle addon-specific entitlement rules

#### 3. CalculateFixedCharges (Enhancement)

**Current Behavior**: Calculates charges from plan line items only  
**Add-on Integration Points**:

- Process addon line items
- Handle addon proration
- Separate line items by source (plan vs addon)
- Apply addon-specific pricing rules

#### 4. CalculateUsageCharges (Enhancement)

**Current Behavior**: Calculates usage against plan entitlements only  
**Add-on Integration Points**:

- Get combined entitlements (plan + addon)
- Calculate usage against combined limits
- Track usage by source
- Apply overage charges based on combined limits

#### 5. GetUsageBySubscription (Enhancement)

**Current Behavior**: Returns usage for plan meters only  
**Add-on Integration Points**:

- Include addon usage in response
- Track usage by source (plan vs addon)
- Apply addon usage limits
- Handle addon-specific usage rules

#### 6. CreateInvoice (Enhancement)

**Current Behavior**: Creates invoice from plan line items only  
**Add-on Integration Points**:

- Include addon line items in invoice
- Handle addon proration line items
- Apply addon-specific discounts
- Separate line items by source

#### 7. PreviewPriceQuote (New Function)

**Function Name**: `PreviewPriceQuote`  
**Responsibility**: Preview pricing for subscription with addons  
**Location**: `internal/service/subscription.go`

```go
func (s *subscriptionService) PreviewPriceQuote(ctx context.Context, req *dto.PreviewPriceQuoteRequest) (*dto.PriceQuoteResponse, error) {
    // Input: plan + addons configuration
    // Output: detailed price breakdown

    // 1. Calculate plan charges
    // 2. Calculate addon charges
    // 3. Calculate proration for mid-cycle changes
    // 4. Apply discounts and credits
    // 5. Return detailed breakdown
}
```

#### 8. FetchActiveEntitlements (Enhancement)

**Current Behavior**: Returns entitlements from plan only  
**Add-on Integration Points**:

- Query addon entitlements
- Merge with plan entitlements
- Apply addon-specific rules
- Return combined entitlements

## ✅ Expected Output

### 1. PRD with All Functional/Business/Data Concerns

The PRD has been completed and includes:

- ✅ Complete data model design
- ✅ API specifications
- ✅ Business logic flows
- ✅ Edge case handling
- ✅ Migration strategy

### 2. Implementation Guide

This guide provides:

- ✅ Function-level breakdowns
- ✅ Input/output specifications
- ✅ When and where functions are called
- ✅ Behavior with and without addons
- ✅ New/updated dependencies

### 3. Edge Case Coverage

The implementation handles:

- ✅ Multi-instance addons
- ✅ Addon + plan overlapping entitlements
- ✅ Addon mid-cycle proration
- ✅ Cancellation/refund flows
- ✅ Usage limit conflicts
- ✅ Feature dependencies

## 🔒 Implementation Strategy

### Phase 1: Core Infrastructure (Week 1-2)

1. Create addon domain models and repositories
2. Implement basic addon service
3. Add database migrations
4. Create addon management APIs

### Phase 2: Subscription Integration (Week 3-4)

1. Enhance subscription service with addon support
2. Update billing service for addon calculations
3. Implement entitlement aggregation
4. Add addon lifecycle management

### Phase 3: Billing Integration (Week 5-6)

1. Update invoice generation for addons
2. Implement proration logic
3. Add usage tracking for addons
4. Create addon analytics

### Phase 4: Testing & Validation (Week 7-8)

1. Unit tests for all new functionality
2. Integration tests for billing scenarios
3. Performance testing
4. Data migration validation

This implementation guide provides a comprehensive roadmap for integrating add-ons into the existing FlexPrice system while maintaining the clean architecture and following established patterns.
