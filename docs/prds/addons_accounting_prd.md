# Add-ons Accounting Flow PRD (Invoice-Level Integration)

## Overview

This PRD outlines a streamlined addon accounting approach that works at the invoice level and usage aggregation level, keeping the existing event ingestion system unchanged. The flow integrates add-ons during invoice generation and usage calculation phases, maintaining the current system's stability while adding addon capabilities.

## Problem Statement

The current system needs addon integration that:

1. **Preserves Event System**: Keeps existing event ingestion unchanged to avoid disrupting core functionality
2. **Invoice-Level Integration**: Handles addon charges during invoice generation phase
3. **Usage Aggregation**: Calculates usage against combined plan + addon entitlements
4. **Mid-Cycle Changes**: Handles addon purchase/cancellation during billing cycles
5. **Proration Handling**: Manages proration for addon changes without affecting existing billing

## Functional Requirements

### 1. Event Ingestion Flow (Unchanged)

#### 1.1 Preserve Existing Event Processing

**Philosophy**: Keep the existing event ingestion system completely unchanged to maintain stability and avoid disrupting core functionality.

**Current Flow Remains**:

- Event validation
- Usage recording
- Meter aggregation
- Standard threshold monitoring

**No Changes Required**:

- No addon attribution at event level
- No source tracking during ingestion
- No real-time entitlement checking
- Existing event processing logic preserved

#### 1.2 Usage Aggregation Enhancement

**Enhanced at Usage Calculation Level**:

- Calculate combined entitlements during `GetUsageBySubscription`
- Apply addon limits during usage summary generation
- Handle overage detection at billing time
- Maintain backward compatibility

### 2. Enhanced Usage Calculation (Invoice-Level Integration)

#### 2.1 Usage Calculation with Addon Support

**Enhanced in `GetUsageBySubscription` and `CalculateUsageCharges`**

**Input**: Subscription with plan + addon line items
**Output**: Usage calculation with addon-aware overage detection

```go
// Enhanced usage response with addon support
type UsageCalculationWithAddons struct {
    PlanUsage           []*SubscriptionUsageByMetersResponse
    AddonUsage          []*SubscriptionUsageByMetersResponse
    OverageUsage        []*SubscriptionUsageByMetersResponse
    CombinedEntitlements map[string]*CombinedEntitlement
}

type CombinedEntitlement struct {
    FeatureID       string
    TotalLimit      *int64
    PlanLimit       *int64
    AddonLimits     []AddonLimit
    CurrentUsage    decimal.Decimal
    OverageAmount   decimal.Decimal
}

type AddonLimit struct {
    AddonID     string
    AddonName   string
    Limit       *int64
}
```

#### 2.2 Invoice-Level Addon Integration

**Integration Points**:

1. **GetUsageBySubscription Enhancement**:

   - Calculate combined entitlements from plan + addon line items
   - Apply usage against combined limits
   - Generate overage charges for excess usage

2. **CalculateUsageCharges Enhancement**:

   - Process both plan and addon line items
   - Create separate invoice line items for addon charges
   - Handle overage attribution to specific addons

3. **GetCustomerEntitlements Enhancement**:
   - Merge plan and addon entitlements
   - Return combined limits with source attribution
   - Maintain backward compatibility for existing clients

**Entitlement Merging Strategy**:

- **Sum Limits**: Add plan + addon limits for metered features
- **Enable Features**: Feature enabled if plan OR addon enables it
- **Overage Logic**: Apply overage only after combined limits exceeded

### 3. Mid-Cycle Addon Management

#### 3.1 Addon Purchase During Billing Cycle

**Scenario**: Customer purchases addon mid-cycle
**Handling Strategy**:

1. **Immediate Entitlement**: Addon entitlements apply immediately
2. **Prorated Billing**: Fixed charges prorated from purchase date
3. **Usage Inclusion**: Usage benefits apply from purchase date forward
4. **Invoice Generation**: Create separate proration invoice or add to next invoice

#### 3.2 Addon Cancellation During Billing Cycle

**Scenario**: Customer cancels addon mid-cycle
**Handling Strategy**:

1. **Entitlement Removal**: Remove addon entitlements from effective date
2. **Prorated Credit**: Credit unused portion of fixed charges
3. **Usage Cutoff**: Usage benefits end on cancellation date
4. **Credit Application**: Apply credits to current or next invoice

#### 3.3 Invoice Generation During Addon Changes

**Complex Scenario**: Invoice generation while addons are being modified
**Handling Strategy**:

1. **Snapshot Approach**: Use addon state at invoice generation time
2. **Separate Line Items**: Create distinct line items for each addon period
3. **Proration Handling**: Add proration line items for partial periods
4. **Clear Attribution**: Ensure each charge is clearly attributed to plan/addon

### 4. Enhanced Billing Calculation

#### 4.1 Enhanced CalculateFixedCharges

**Current Function Enhancement**:

```go
func (s *billingService) CalculateFixedCharges(
    ctx context.Context,
    sub *subscription.Subscription,
    periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // ENHANCED LOGIC:
    // 1. Process plan line items (existing logic)
    // 2. Process addon line items (NEW)
    // 3. Handle addon proration (NEW)
    // 4. Create separate line items for each addon (NEW)
}
```

#### 4.2 Enhanced CalculateUsageCharges

**Current Function Enhancement**:

```go
func (s *billingService) CalculateUsageCharges(
    ctx context.Context,
    sub *subscription.Subscription,
    usage *dto.GetUsageBySubscriptionResponse,
    periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // ENHANCED LOGIC:
    // 1. Get combined entitlements (plan + addons) (NEW)
    // 2. Calculate usage against combined limits (MODIFIED)
    // 3. Create separate overage line items for addons (NEW)
    // 4. Maintain backward compatibility (PRESERVED)
}
```

### 5. Invoice Generation Flow

#### 5.1 Invoice Line Item Structure

**Line Item Types**:

- **Plan Fixed Charges**: Regular plan charges
- **Addon Fixed Charges**: Addon subscription charges
- **Plan Usage Charges**: Usage charges for plan entitlements
- **Addon Usage Charges**: Usage charges for addon entitlements
- **Proration Charges**: Prorated amounts for addon changes
- **Overage Charges**: Charges for usage beyond combined limits

#### 5.2 Invoice Attribution

**Attribution Rules**:

- **Plan Line Items**: Include `plan_id` and `plan_display_name`
- **Addon Line Items**: Include `addon_id` and `addon_display_name`
- **Usage Line Items**: Include source attribution (plan vs addon)
- **Proration Line Items**: Include addon reference for proration
- **Overage Line Items**: Include source attribution for overage

## Data Flow Architecture (Invoice-Level Integration)

### 1. Event Ingestion Pipeline (Unchanged)

```
Customer App → Usage Event → Event Validation → Usage Recording →
Standard Aggregation → Existing Threshold Monitoring
```

### 2. Enhanced Usage Calculation Pipeline

```
GetUsageBySubscription → Get Plan + Addon Line Items →
Calculate Combined Entitlements → Apply Usage Against Combined Limits →
Generate Overage for Excess Usage
```

### 3. Enhanced Billing Pipeline

```
CalculateFixedCharges → Process Plan + Addon Line Items →
Apply Proration → CalculateUsageCharges → Process Combined Usage →
Create Separate Line Items → Invoice Generation
```

### 4. Invoice Generation Pipeline (Enhanced)

```
Enhanced Billing Calculation → Plan + Addon Line Items →
Invoice Assembly → Invoice Finalization → Payment Processing
```

## Technical Implementation (Enhanced Existing Services)

### 1. Enhanced Subscription Service

```go
// Enhanced GetUsageBySubscription to handle addons
func (s *subscriptionService) GetUsageBySubscription(
    ctx context.Context,
    req *dto.GetUsageBySubscriptionRequest,
) (*dto.GetUsageBySubscriptionResponse, error) {
    // EXISTING: Get usage for plan line items
    // NEW: Get addon line items
    // NEW: Calculate combined entitlements
    // NEW: Apply usage against combined limits
    // NEW: Generate overage charges for excess usage
}

// Enhanced GetCustomerEntitlements to include addons
func (s *billingService) GetCustomerEntitlements(
    ctx context.Context,
    customerID string,
    req *dto.GetCustomerEntitlementsRequest,
) (*dto.CustomerEntitlementsResponse, error) {
    // EXISTING: Get plan entitlements
    // NEW: Get addon entitlements
    // NEW: Merge entitlements with source attribution
    // PRESERVED: Backward compatibility
}
```

### 2. Enhanced Billing Service

```go
// Enhanced CalculateFixedCharges to handle addon line items
func (s *billingService) CalculateFixedCharges(
    ctx context.Context,
    sub *subscription.Subscription,
    periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // EXISTING: Process plan line items
    // NEW: Process addon line items from subscription_addons
    // NEW: Handle addon proration for mid-cycle changes
    // NEW: Create separate line items with addon attribution
}

// Enhanced CalculateUsageCharges to use combined entitlements
func (s *billingService) CalculateUsageCharges(
    ctx context.Context,
    sub *subscription.Subscription,
    usage *dto.GetUsageBySubscriptionResponse,
    periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // MODIFIED: Get combined entitlements (plan + addons)
    // MODIFIED: Calculate usage against combined limits
    // NEW: Create separate overage line items for addons
    // PRESERVED: Existing functionality for plan-only subscriptions
}
```

### 3. New Addon Management Service

```go
type AddonService interface {
    // Addon lifecycle management
    AddAddonToSubscription(ctx context.Context, subscriptionID string, req *dto.AddAddonRequest) error
    RemoveAddonFromSubscription(ctx context.Context, subscriptionID string, addonID string) error
    UpdateAddonQuantity(ctx context.Context, subscriptionID string, addonID string, quantity int) error

    // Proration calculations
    CalculateAddonProration(ctx context.Context, subscription *subscription.Subscription, addon *SubscriptionAddon, changeDate time.Time) (decimal.Decimal, error)

    // Addon-specific operations
    GetSubscriptionAddons(ctx context.Context, subscriptionID string) ([]*SubscriptionAddon, error)
    ValidateAddonCompatibility(ctx context.Context, subscriptionID string, addonID string) error
}
```

## Edge Cases and Error Handling

### 1. Usage Limit Conflicts

**Scenario**: Addon and plan have different usage limits for same feature
**Resolution**:

- Sum the limits for metered features
- Use the higher limit for boolean features
- Apply precedence rules for static features

### 2. Reset Period Mismatches

**Scenario**: Plan and addon have different usage reset periods
**Resolution**:

- Enforce same reset period for metered features
- Return error if reset periods don't match
- Provide migration path for existing subscriptions

### 3. Proration Edge Cases

**Scenario**: Addon added/removed mid-cycle with complex pricing
**Resolution**:

- Calculate daily rates for all pricing models
- Apply proration behavior consistently
- Handle tiered pricing proration correctly

### 4. Overage Calculation Conflicts

**Scenario**: Usage exceeds combined limits with different overage rates
**Resolution**:

- Apply plan overage rates first
- Apply addon overage rates for addon-specific usage
- Track overage by source for proper attribution

## Performance Considerations

### 1. Usage Aggregation Performance

**Optimizations**:

- Cache combined entitlements for active subscriptions
- Batch process usage events
- Use database indexes for usage queries
- Implement usage aggregation at database level

### 2. Billing Calculation Performance

**Optimizations**:

- Pre-calculate fixed charges for addons
- Cache entitlement calculations
- Use parallel processing for usage calculations
- Implement incremental billing calculations

### 3. Invoice Generation Performance

**Optimizations**:

- Generate line items in batches
- Use database transactions for consistency
- Implement async invoice processing
- Cache invoice templates

## Monitoring and Analytics

### 1. Usage Analytics

**Metrics**:

- Usage by source (plan vs addon)
- Addon adoption rates
- Usage pattern analysis
- Overage frequency and amounts

### 2. Billing Analytics

**Metrics**:

- Addon revenue contribution
- Proration frequency and amounts
- Invoice complexity metrics
- Billing accuracy rates

### 3. System Performance

**Metrics**:

- Event processing latency
- Entitlement calculation time
- Invoice generation time
- Database query performance

## Success Metrics

### 1. Functional Metrics

- **Event Processing Accuracy**: 99.9%+ event processing accuracy
- **Entitlement Calculation Accuracy**: 100% entitlement aggregation accuracy
- **Invoice Generation Accuracy**: 99.9%+ invoice accuracy
- **Proration Accuracy**: 100% proration calculation accuracy

### 2. Performance Metrics

- **Event Processing Latency**: <100ms average processing time
- **Entitlement Calculation Time**: <200ms average calculation time
- **Invoice Generation Time**: <500ms average generation time
- **System Throughput**: 10,000+ events per second

### 3. Business Metrics

- **Addon Revenue**: 15%+ additional revenue from addons
- **Customer Satisfaction**: 20%+ reduction in billing-related support tickets
- **System Reliability**: 99.9%+ system uptime
- **Data Consistency**: Zero data inconsistencies in billing calculations

## Conclusion

This PRD provides a comprehensive framework for the addon accounting flow, ensuring accurate usage tracking, proper entitlement aggregation, and correct invoice generation. The system maintains backward compatibility while adding significant flexibility for complex billing scenarios.

The implementation prioritizes accuracy, performance, and scalability while providing detailed analytics and monitoring capabilities. The flow integrates seamlessly with the existing billing infrastructure while adding the necessary complexity to handle addon-specific requirements.
