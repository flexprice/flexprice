# Sum with Window Aggregation and Commitment-Based Slab Tiered Pricing

## Overview
This document outlines the implementation of two new features:
1. **Sum with Window Aggregation**: Aggregate usage in time-based windows (buckets) for more granular metering
2. **Commitment-Based Slab Tiered Pricing**: Apply minimum commitment amounts with slab-based tier progression

## 1. Sum with Window Aggregation

### Problem Statement
Current aggregation types (COUNT, SUM, MAX, etc.) aggregate over the entire billing period. We need the ability to aggregate usage within smaller time windows (e.g., per minute, per hour) and then apply pricing logic to those windowed values.

### Use Case
**Feature**: NVIDIA H100 GPU usage tracking
- Track GPU instances per minute
- Each minute's usage is aggregated independently
- Pricing applied to aggregated values per window

**Example**:
```
Minute 1: 12 GPU instances → 12 units
Minute 2: 20 GPU instances → 20 units
Minute 3: 25 GPU instances → 25 units
```

### Technical Design

#### 1.1 Type Addition
Add new aggregation type to `internal/types/aggregation.go`:

```go
const (
    // Existing types...
    AggregationMax               AggregationType = "MAX"
    AggregationWeightedSum       AggregationType = "WEIGHTED_SUM"
    AggregationSumWithWindow     AggregationType = "SUM_WITH_WINDOW"  // New
)
```

Update validation:
```go
func (t AggregationType) Validate() bool {
    switch t {
    case AggregationCount,
        AggregationSum,
        // ... other types
        AggregationSumWithWindow:  // Add here
        return true
    default:
        return false
    }
}
```

#### 1.2 Schema Changes
The existing `MeterAggregation` struct in `ent/schema/meter.go` already supports `BucketSize`:

```go
type MeterAggregation struct {
    Type       types.AggregationType `json:"type"`
    Field      string                `json:"field,omitempty"`
    Multiplier *decimal.Decimal      `json:"multiplier,omitempty"`
    BucketSize types.WindowSize      `json:"bucket_size,omitempty"`  // Already exists
}
```

**Configuration Example**:
```json
{
  "type": "SUM_WITH_WINDOW",
  "field": "gpu_count",
  "bucket_size": "MINUTE"
}
```

#### 1.3 Aggregator Implementation
Create new aggregator in `internal/repository/clickhouse/aggregators.go`:

```go
// SumWithWindowAggregator aggregates sum within time windows
type SumWithWindowAggregator struct {
    *BaseAggregator
    windowSize types.WindowSize
}

func NewSumWithWindowAggregator(windowSize types.WindowSize) *SumWithWindowAggregator {
    return &SumWithWindowAggregator{
        BaseAggregator: NewBaseAggregator(),
        windowSize:     windowSize,
    }
}

func (a *SumWithWindowAggregator) GetType() types.AggregationType {
    return types.AggregationSumWithWindow
}

func (a *SumWithWindowAggregator) BuildQuery(ctx context.Context, params QueryParams) string {
    // Build ClickHouse query with time-window bucketing
    windowInterval := a.getWindowInterval()
    
    return fmt.Sprintf(`
        SELECT 
            toStartOfInterval(event_time, INTERVAL %s) as bucket_start,
            SUM(CAST(JSONExtractString(properties, '%s') AS Decimal(20,8))) as bucket_value
        FROM events
        WHERE %s
        GROUP BY bucket_start
        ORDER BY bucket_start
    `, windowInterval, params.PropertyName, params.WhereClause)
}

func (a *SumWithWindowAggregator) getWindowInterval() string {
    switch a.windowSize {
    case types.WindowSizeMinute:
        return "1 MINUTE"
    case types.WindowSize15Min:
        return "15 MINUTE"
    case types.WindowSizeHour:
        return "1 HOUR"
    case types.WindowSizeDay:
        return "1 DAY"
    // ... other window sizes
    default:
        return "1 HOUR"
    }
}
```

Update `GetAggregator` function:
```go
func GetAggregator(aggregationType types.AggregationType, bucketSize types.WindowSize) events.Aggregator {
    switch aggregationType {
    case types.AggregationSumWithWindow:
        return NewSumWithWindowAggregator(bucketSize)
    // ... existing cases
    }
}
```

#### 1.4 Repository Updates
Update `internal/repository/clickhouse/event.go` to handle windowed aggregation:

```go
func (r *clickhouseEventRepo) GetAggregatedUsage(ctx context.Context, params UsageParams) (*AggregationResult, error) {
    // Validate bucket_size for SUM_WITH_WINDOW
    if params.AggregationType == types.AggregationSumWithWindow {
        if params.BucketSize == "" {
            return nil, ierr.NewError("bucket_size required for SUM_WITH_WINDOW").
                Mark(ierr.ErrValidation)
        }
    }
    
    aggregator := GetAggregator(params.AggregationType, params.BucketSize)
    // ... rest of implementation
}
```

#### 1.5 Service Layer Updates
Update `internal/service/feature_usage_tracking.go`:

```go
func (s *featureUsageTrackingService) processUsageItem(ctx context.Context, item *events.DetailedUsageAnalytic, meter *meter.Meter, price *price.Price) {
    switch meter.Aggregation.Type {
    case types.AggregationSumWithWindow:
        // Each point represents a window's sum
        for i, point := range item.Points {
            windowValue := s.getCorrectUsageValueForPoint(point, meter.Aggregation.Type)
            pointCost := priceService.CalculateCost(ctx, price, windowValue)
            // Store window-level metadata
        }
    // ... other cases
    }
}
```

#### 1.6 API Updates
Update `internal/api/dto/events.go` to support the new aggregation type in API requests:

```go
type AggregateUsageRequest struct {
    EventName          string                `form:"event_name" json:"event_name" binding:"required"`
    AggregationType    types.AggregationType `form:"aggregation_type" json:"aggregation_type" binding:"required"`
    PropertyName       string                `form:"property_name" json:"property_name"`
    BucketSize         types.WindowSize      `form:"bucket_size" json:"bucket_size,omitempty"`
    // ... other fields
}

func (r *AggregateUsageRequest) Validate() error {
    if r.AggregationType == types.AggregationSumWithWindow && r.BucketSize == "" {
        return ierr.NewError("bucket_size is required for SUM_WITH_WINDOW aggregation").
            Mark(ierr.ErrValidation)
    }
    // ... rest of validation
}
```

### 1.7 Invoice Line Item Metadata
Store window-level details in invoice line item metadata:

```go
metadata := map[string]string{
    "aggregation_type": "SUM_WITH_WINDOW",
    "bucket_size":      "MINUTE",
    "window_count":     "3",  // number of windows in billing period
    "window_breakdown": `[
        {"start": "2024-01-01T00:00:00Z", "end": "2024-01-01T00:01:00Z", "value": 12, "cost": 12},
        {"start": "2024-01-01T00:01:00Z", "end": "2024-01-01T00:02:00Z", "value": 20, "cost": 20},
        {"start": "2024-01-01T00:02:00Z", "end": "2024-01-01T00:03:00Z", "value": 25, "cost": 30}
    ]`,
}
```

## 2. Commitment-Based Slab Tiered Pricing

### Problem Statement
Need to support minimum commitment amounts where:
1. Customer commits to a minimum spend (e.g., $20/month)
2. Usage is charged using slab-based tiers
3. Minimum commitment is guaranteed even if actual usage is lower

### Use Case
**Feature**: GPU commitment pricing
- Commitment: 20 instances minimum
- Price tiers (SLAB mode):
  - 0-20 instances: $1/instance
  - 20+ instances: $2/instance

**Examples**:
```
Scenario 1: 12 instances → Bill $20 (minimum commitment)
Scenario 2: 20 instances → Bill $20 (exactly at commitment)
Scenario 3: 25 instances → Bill $30 (20×$1 + 5×$2)
```

### Technical Design

#### 2.1 Schema Changes
Add commitment field to `ent/schema/price.go`:

```go
type Price struct {
    ent.Schema
}

func (Price) Fields() []ent.Field {
    return []ent.Field{
        // ... existing fields
        field.String("id").Unique().Immutable(),
        field.Enum("billing_model").Values("FLAT_FEE", "PACKAGE", "TIERED"),
        field.JSON("tiers", []types.PriceTier{}).Optional(),
        field.Enum("tier_mode").Values("VOLUME", "SLAB").Optional(),
        
        // New field
        field.Other("commitment_quantity", &decimal.Decimal{}).
            SchemaType(map[string]string{
                "postgres": "decimal(20,8)",
            }).
            Optional().
            Nillable().
            Comment("Minimum commitment quantity for tiered pricing"),
    }
}
```

#### 2.2 Type Updates
Update `internal/types/price.go` to add commitment validation:

```go
// PriceTier already exists, no changes needed
type PriceTier struct {
    UpTo       *uint64         `json:"up_to"`
    UnitAmount decimal.Decimal `json:"unit_amount"`
    FlatAmount *decimal.Decimal `json:"flat_amount,omitempty"`
}

// Add commitment validation helper
func ValidateCommitmentTier(commitmentQty *decimal.Decimal, tiers []PriceTier, tierMode BillingTier) error {
    if commitmentQty == nil || commitmentQty.IsZero() {
        return nil
    }
    
    if tierMode != BILLING_TIER_SLAB {
        return ierr.NewError("commitment_quantity only supported with SLAB tier mode").
            Mark(ierr.ErrValidation)
    }
    
    if len(tiers) == 0 {
        return ierr.NewError("tiers required when using commitment_quantity").
            Mark(ierr.ErrValidation)
    }
    
    return nil
}
```

#### 2.3 Domain Model Updates
Update `internal/domain/price/model.go`:

```go
type Price struct {
    ID                  string
    Name                string
    BillingModel        types.BillingModel
    TierMode            types.BillingTier
    Tiers               []types.PriceTier
    CommitmentQuantity  *decimal.Decimal  // New field
    // ... other fields
    types.BaseModel
}

func FromEnt(e *ent.Price) *Price {
    if e == nil {
        return nil
    }
    return &Price{
        ID:                 e.ID,
        BillingModel:       types.BillingModel(e.BillingModel),
        TierMode:           types.BillingTier(lo.FromPtr(e.TierMode)),
        Tiers:              e.Tiers,
        CommitmentQuantity: e.CommitmentQuantity,  // New
        // ... other fields
    }
}
```

#### 2.4 Price Calculation Logic
Update `internal/service/price.go` to handle commitment:

```go
func (s *priceService) CalculateCost(ctx context.Context, price *price.Price, quantity decimal.Decimal) decimal.Decimal {
    if price.BillingModel != types.BILLING_MODEL_TIERED {
        return s.calculateSingletonCost(ctx, price, quantity)
    }
    
    // Calculate actual cost using slab tiers
    actualCost := s.calculateTieredCost(ctx, price, quantity)
    
    // Apply commitment minimum if specified
    if price.CommitmentQuantity != nil && !price.CommitmentQuantity.IsZero() {
        commitmentCost := s.calculateTieredCost(ctx, price, *price.CommitmentQuantity)
        
        // Return the maximum of actual cost and commitment cost
        if actualCost.LessThan(commitmentCost) {
            s.Logger.WithContext(ctx).Infof(
                "applying commitment minimum: actual=%s, commitment=%s, price_id=%s",
                actualCost.String(), commitmentCost.String(), price.ID,
            )
            return commitmentCost
        }
    }
    
    return actualCost
}

func (s *priceService) CalculateCostWithBreakup(ctx context.Context, price *price.Price, quantity decimal.Decimal, round bool) dto.CostBreakup {
    result := dto.CostBreakup{}
    
    if price.BillingModel == types.BILLING_MODEL_TIERED && price.TierMode == types.BILLING_TIER_SLAB {
        // Calculate actual usage cost and tiers used
        result.FinalCost = s.calculateTieredCost(ctx, price, quantity)
        result.TierBreakdown = s.calculateSlabTierBreakdown(ctx, price, quantity)
        
        // Check commitment
        if price.CommitmentQuantity != nil && !price.CommitmentQuantity.IsZero() {
            commitmentCost := s.calculateTieredCost(ctx, price, *price.CommitmentQuantity)
            
            result.CommitmentQuantity = price.CommitmentQuantity
            result.CommitmentCost = &commitmentCost
            
            if result.FinalCost.LessThan(commitmentCost) {
                result.CommitmentApplied = true
                result.FinalCost = commitmentCost
            }
        }
    }
    
    return result
}

// Helper to calculate slab tier breakdown
func (s *priceService) calculateSlabTierBreakdown(ctx context.Context, price *price.Price, quantity decimal.Decimal) []dto.TierCostBreakdown {
    breakdown := []dto.TierCostBreakdown{}
    
    remainingQuantity := quantity
    tierStartQuantity := decimal.Zero
    
    for i, tier := range price.Tiers {
        var tierQuantity = remainingQuantity
        if tier.UpTo != nil {
            upTo := decimal.NewFromUint64(*tier.UpTo)
            tierCapacity := upTo.Sub(tierStartQuantity)
            
            if remainingQuantity.GreaterThan(tierCapacity) {
                tierQuantity = tierCapacity
            }
            tierStartQuantity = upTo
        }
        
        tierCost := tier.CalculateTierAmount(tierQuantity, price.Currency)
        
        breakdown = append(breakdown, dto.TierCostBreakdown{
            TierIndex:    i,
            TierUpTo:     tier.UpTo,
            UnitAmount:   tier.UnitAmount,
            Quantity:     tierQuantity,
            Cost:         tierCost,
        })
        
        remainingQuantity = remainingQuantity.Sub(tierQuantity)
        
        if remainingQuantity.LessThanOrEqual(decimal.Zero) {
            break
        }
    }
    
    return breakdown
}
```

#### 2.5 DTO Updates
Add commitment fields to `internal/api/dto/price.go`:

```go
type CostBreakup struct {
    EffectiveUnitCost   decimal.Decimal       `json:"effective_unit_cost"`
    FinalCost           decimal.Decimal       `json:"final_cost"`
    TierBreakdown       []TierCostBreakdown   `json:"tier_breakdown,omitempty"`
    CommitmentQuantity  *decimal.Decimal      `json:"commitment_quantity,omitempty"`
    CommitmentCost      *decimal.Decimal      `json:"commitment_cost,omitempty"`
    CommitmentApplied   bool                  `json:"commitment_applied"`
}

type TierCostBreakdown struct {
    TierIndex   int              `json:"tier_index"`
    TierUpTo    *uint64          `json:"tier_up_to"`
    UnitAmount  decimal.Decimal  `json:"unit_amount"`
    Quantity    decimal.Decimal  `json:"quantity"`
    Cost        decimal.Decimal  `json:"cost"`
}

type CreatePriceRequest struct {
    Name               string                `json:"name" binding:"required"`
    BillingModel       types.BillingModel    `json:"billing_model" binding:"required"`
    TierMode           *types.BillingTier    `json:"tier_mode,omitempty"`
    Tiers              []types.PriceTier     `json:"tiers,omitempty"`
    CommitmentQuantity *decimal.Decimal      `json:"commitment_quantity,omitempty"`
    // ... other fields
}

func (r *CreatePriceRequest) Validate() error {
    // ... existing validation
    
    // Validate commitment
    if r.CommitmentQuantity != nil {
        if r.BillingModel != types.BILLING_MODEL_TIERED {
            return ierr.NewError("commitment_quantity only supported for TIERED billing model").
                Mark(ierr.ErrValidation)
        }
        
        if r.TierMode == nil || *r.TierMode != types.BILLING_TIER_SLAB {
            return ierr.NewError("commitment_quantity requires SLAB tier mode").
                Mark(ierr.ErrValidation)
        }
        
        if r.CommitmentQuantity.LessThanOrEqual(decimal.Zero) {
            return ierr.NewError("commitment_quantity must be greater than zero").
                Mark(ierr.ErrValidation)
        }
    }
    
    return nil
}
```

#### 2.6 Invoice Line Item Metadata
Store commitment details in metadata:

```go
metadata := map[string]string{
    "billing_model":         "TIERED",
    "tier_mode":             "SLAB",
    "actual_quantity":       "12",
    "actual_cost":           "12.00",
    "commitment_quantity":   "20",
    "commitment_cost":       "20.00",
    "commitment_applied":    "true",
    "tier_breakdown":        `[
        {"tier": 0, "up_to": 20, "unit_amount": 1.00, "quantity": 12, "cost": 12.00}
    ]`,
}
```

## 3. Combined Use Case Example

### Configuration
**Meter**:
```json
{
  "event_name": "gpu_usage",
  "aggregation": {
    "type": "SUM_WITH_WINDOW",
    "field": "instance_count",
    "bucket_size": "MINUTE"
  }
}
```

**Price**:
```json
{
  "billing_model": "TIERED",
  "tier_mode": "SLAB",
  "commitment_quantity": 20,
  "tiers": [
    {"up_to": 20, "unit_amount": 1.0},
    {"up_to": null, "unit_amount": 2.0}
  ]
}
```

### Scenario: 3-Minute Billing Period
**Events**:
- Minute 1: 12 instances → Cost: $12
- Minute 2: 20 instances → Cost: $20
- Minute 3: 25 instances → Cost: $30 (20×$1 + 5×$2)

**Total**: $62 actual usage cost

**Invoice Line Item**:
```json
{
  "description": "GPU Usage",
  "quantity": 57,  // 12 + 20 + 25
  "amount": 62.00,
  "metadata": {
    "aggregation_type": "SUM_WITH_WINDOW",
    "bucket_size": "MINUTE",
    "commitment_quantity": "20",
    "commitment_cost_per_window": "20.00",
    "window_breakdown": "[{minute:1, qty:12, cost:12}, {minute:2, qty:20, cost:20}, {minute:3, qty:25, cost:30}]",
    "tier_mode": "SLAB",
    "commitment_applied": "false"
  }
}
```

**Note**: Commitment is checked per billing period (not per window). Since total cost ($62) > commitment cost per window × 3 windows, commitment is not applied.

## 4. Migration Plan

### Phase 1: Foundation (Week 1)
- [ ] Add `AggregationSumWithWindow` type
- [ ] Implement `SumWithWindowAggregator`
- [ ] Add unit tests for new aggregation type
- [ ] Update API documentation

### Phase 2: Commitment Pricing (Week 2)
- [ ] Add `commitment_quantity` field to price schema
- [ ] Run database migration
- [ ] Update price calculation logic
- [ ] Add commitment validation
- [ ] Update API endpoints

### Phase 3: Integration (Week 3)
- [ ] Update feature usage tracking service
- [ ] Update invoice generation logic
- [ ] Add invoice line item metadata
- [ ] Integration testing

### Phase 4: Testing & Documentation (Week 4)
- [ ] End-to-end testing with combined use cases
- [ ] Performance testing with large datasets
- [ ] Update API documentation
- [ ] Create user guide with examples

## 5. Testing Strategy

### 5.1 Unit Tests
- Test `SUM_WITH_WINDOW` aggregation with various window sizes
- Test commitment calculation with different tier configurations
- Test edge cases (zero usage, exact commitment, below commitment)

### 5.2 Integration Tests
- Test full billing flow with windowed aggregation
- Test commitment pricing across multiple billing periods
- Test combined scenarios (windows + commitment)

### 5.3 Test Cases
```go
// Test: SUM_WITH_WINDOW with MINUTE bucket
// Expected: Each minute aggregated independently

// Test: Commitment below usage
// Input: commitment=20, actual=12
// Expected: Bill $20 (commitment minimum)

// Test: Commitment at exact usage
// Input: commitment=20, actual=20
// Expected: Bill $20

// Test: Usage exceeds commitment
// Input: commitment=20, actual=25, tiers=[{0-20: $1}, {20+: $2}]
// Expected: Bill $30 (20×$1 + 5×$2)
```

## 6. API Examples

### Create Meter with Window Aggregation
```bash
POST /v1/meters
{
  "event_name": "gpu_usage",
  "name": "GPU Instance Usage",
  "aggregation": {
    "type": "SUM_WITH_WINDOW",
    "field": "instance_count",
    "bucket_size": "MINUTE"
  }
}
```

### Create Price with Commitment
```bash
POST /v1/prices
{
  "name": "GPU Commitment Pricing",
  "billing_model": "TIERED",
  "tier_mode": "SLAB",
  "commitment_quantity": 20,
  "tiers": [
    {"up_to": 20, "unit_amount": 1.00},
    {"up_to": null, "unit_amount": 2.00}
  ],
  "currency": "USD",
  "price_type": "USAGE"
}
```

## 7. Backward Compatibility

### Existing Features Unaffected
- All existing aggregation types continue to work
- Existing tiered pricing (without commitment) unchanged
- `commitment_quantity` is optional, defaults to null
- Invoice generation logic handles both old and new formats

### Migration Path
1. Existing meters/prices remain unchanged
2. New features opt-in via API
3. No breaking changes to existing APIs
4. Graceful handling of null commitment values

## 8. Performance Considerations

### ClickHouse Query Optimization
- Window aggregation uses `toStartOfInterval` for efficient bucketing
- Proper indexing on `event_time` column
- Aggregation pushed down to ClickHouse level

### Caching Strategy
- Cache commitment calculations for repeated billing periods
- Cache tier calculations for common quantities
- Invalidate cache on price updates

## 9. Monitoring & Observability

### Key Metrics
- Window aggregation query performance
- Commitment application rate
- Cost calculation accuracy
- Invoice generation success rate

### Logging
```go
s.Logger.WithContext(ctx).Infof(
    "window aggregation: meter=%s, bucket_size=%s, windows=%d, total_cost=%s",
    meter.ID, meter.Aggregation.BucketSize, windowCount, totalCost,
)

s.Logger.WithContext(ctx).Infof(
    "commitment applied: price=%s, actual=%s, commitment=%s",
    price.ID, actualCost, commitmentCost,
)
```

## 10. Documentation Updates

### Required Updates
- [ ] API Reference (Swagger/OpenAPI)
- [ ] Pricing Models Guide
- [ ] Meter Configuration Guide
- [ ] Invoice Line Item Metadata Reference
- [ ] Migration Guide for Existing Customers

## 11. Acceptance Criteria

### Sum with Window Aggregation
- [x] New aggregation type `SUM_WITH_WINDOW` supported
- [x] Window sizes (MINUTE, HOUR, DAY, etc.) configurable
- [x] Each window aggregated independently
- [x] Invoice metadata shows per-window breakdown
- [x] Backward compatible with existing aggregations

### Commitment-Based Slab Pricing
- [x] `commitment_quantity` field added to price schema
- [x] Commitment enforced as minimum bill amount
- [x] Works with SLAB tier mode
- [x] Invoice metadata shows commitment details
- [x] Backward compatible (commitment optional)

### Combined Functionality
- [x] Window aggregation + commitment pricing work together
- [x] Per-window costs calculated correctly
- [x] Commitment applied to total billing period
- [x] Transparent in invoice line items
