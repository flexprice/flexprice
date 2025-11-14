# 📊 Cost Sheet Revenue Analytics - Implementation Plan

## 🎯 Objective

Build a comprehensive analytics layer that provides cost-related metrics (Total Cost, Breakdown per meter) and combines them with existing revenue analytics to compute derived metrics like **ROI, Margin, and Revenue Breakdown**.

---

## 📋 Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Current System Analysis](#current-system-analysis)
3. [Implementation Phases](#implementation-phases)
4. [Detailed Component Design](#detailed-component-design)
5. [Database Schema Changes](#database-schema-changes)
6. [API Design](#api-design)
7. [Testing Strategy](#testing-strategy)
8. [Performance Considerations](#performance-considerations)
9. [Rollout Strategy](#rollout-strategy)
10. [Risk Assessment](#risk-assessment)

---

## 🏗️ Architecture Overview

### High-Level Flow

```
┌─────────────────┐
│  ClickHouse     │
│  Events Table   │──┐
└─────────────────┘  │
                     │
┌─────────────────┐  │    ┌──────────────────────┐
│  ClickHouse     │  │    │  Cost Sheet          │
│  Events         │──┼───>│  Analytics Service   │
│  Processed      │  │    │  (NEW)               │
└─────────────────┘  │    └──────────────────────┘
                     │              │
┌─────────────────┐  │              │
│  PostgreSQL     │  │              │ Cost Calculation
│  Cost Sheet v2  │──┘              │ via Billing Service
│  Prices         │                 │
└─────────────────┘                 v
                            ┌──────────────────────┐
                            │  Cost Breakdown      │
                            │  - Total Cost        │
                            │  - Cost per Meter    │
                            │  - Cost per Feature  │
                            └──────────────────────┘
                                      │
                                      │
                                      v
┌─────────────────┐         ┌──────────────────────┐
│  Existing       │         │  Combined Analytics  │
│  Function       │────────>│  API (NEW)           │
│  Analytics API  │         │                      │
└─────────────────┘         └──────────────────────┘
                                      │
                                      v
                            ┌──────────────────────┐
                            │  Final Response      │
                            │  - Revenue           │
                            │  - Total Cost        │
                            │  - Margin            │
                            │  - ROI               │
                            │  - Breakdowns        │
                            └──────────────────────┘
```

---

## 🔍 Current System Analysis

### Existing Components

#### 1. **Function Analytics API**
- **Location**: `internal/service/feature_usage_tracking.go`
- **Key Methods**:
  - `GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest)`
- **Data Source**: ClickHouse `events_processed` and `feature_usage` tables
- **Features**:
  - Aggregates usage data by customer
  - Supports filtering by feature, source, time range
  - Provides time-series breakdowns
  - Computes revenue from billing data

#### 2. **ClickHouse Tables**

##### Events Table (`flexprice.events`)
```sql
- id String
- tenant_id String
- external_customer_id String
- environment_id String
- event_name String
- customer_id Nullable(String)
- source Nullable(String)
- timestamp DateTime64(3)
- ingested_at DateTime64(3)
- properties String
```

##### Events Processed Table (`flexprice.events_processed`)
```sql
- id, tenant_id, environment_id, external_customer_id, event_name
- customer_id, subscription_id, sub_line_item_id
- price_id, meter_id, feature_id, period_id
- timestamp, ingested_at, processed_at
- source, properties
- unique_hash
- qty_total, qty_billable, qty_free_applied
- tier_snapshot, unit_cost, cost
- currency
- version, sign, final_lag_ms
```

**Key Insight**: We must use raw events + billing services for all cost calculations to ensure accuracy and consistency with the existing billing system.

#### 3. **Billing Services**
- **Location**: `internal/service/billing.go`, `internal/service/price.go`
- **Key Methods**:
  - `CalculateCost(ctx, price, quantity)` - Calculate cost for a price and quantity
  - `CalculateCostWithBreakup(ctx, price, quantity, round)` - Detailed cost breakdown
  - `CalculateUsageCharges(ctx, subscription, usage, periodStart, periodEnd)` - Calculate usage-based charges
- **Billing Models Supported**:
  - Flat Fee
  - Package
  - Tiered (Volume & Slab modes)

#### 4. **Cost Sheet V2 Implementation** (Already in place)
- **Location**: `internal/service/costsheet_v2.go`
- **Features**:
  - CRUD operations for Cost Sheets
  - Association with Prices (via `entity_type` = `COSTSHEET_V2`)
  - Metadata support
- **Status**: ✅ Already implemented and staged

---

## 📐 Implementation Phases

### Phase 1: Cost Sheet Analytics Service (Core) 🎯
**Duration**: 4-5 days  
**Complexity**: High

#### Objectives:
- Create a new service to compute cost analytics using hybrid approach
- Implement raw events processing for complex pricing models (tiered, max, weighted sum)
- Leverage existing `events_processed` for simple pricing models
- Support all meter aggregation types (SUM, MAX, COUNT_UNIQUE, WEIGHTED_SUM, etc.)
- Provide granular cost breakdown per meter/feature

#### Deliverables:
1. `CostsheetAnalyticsService` interface and implementation
2. Cost calculation strategies (raw events vs processed events)
3. Meter aggregation logic for all aggregation types
4. DTOs for cost analytics requests and responses
5. Repository methods for both raw and processed event queries
6. Integration with existing billing services
7. Unit tests for cost calculation logic

---

### Phase 2: Combined Analytics API (Integration) 🔗
**Duration**: 2-3 days  
**Complexity**: Medium

#### Objectives:
- Integrate Cost Sheet Analytics with existing Function Analytics
- Compute derived metrics (ROI, Margin)
- Provide unified API response

#### Deliverables:
1. New API endpoint `/analytics/combined` or similar
2. Service layer integration logic
3. Response DTOs with revenue, cost, and derived metrics
4. API documentation (Swagger)

---

### Phase 3: Time-Series Support & Advanced Features ⏱️
**Duration**: 2-3 days  
**Complexity**: Medium

#### Objectives:
- Add time-series support to cost analytics (hourly, daily, weekly breakdowns)
- Support grouping by multiple dimensions (meter, feature, source)
- Add caching layer for performance

#### Deliverables:
1. Time-series cost analytics
2. Multi-dimensional grouping support
3. Caching implementation (if needed)
4. Performance benchmarks

---

### Phase 4: Testing & Documentation 📝
**Duration**: 2 days  
**Complexity**: Low

#### Objectives:
- Comprehensive integration tests
- Load testing
- API documentation
- User guide

#### Deliverables:
1. Integration tests
2. Load test results
3. API documentation
4. User guide / README

---

## 🛠️ Detailed Component Design

### 1. Cost Sheet Analytics Service

#### Interface Definition

```go
// Location: internal/interfaces/service.go

type CostsheetAnalyticsService interface {
    // GetCostAnalytics retrieves cost analytics for a customer
    GetCostAnalytics(ctx context.Context, req *dto.GetCostAnalyticsRequest) (*dto.GetCostAnalyticsResponse, error)
    
    // GetCostAnalyticsWithBreakdown retrieves cost analytics with detailed breakdown
    GetCostAnalyticsWithBreakdown(ctx context.Context, req *dto.GetCostAnalyticsRequest) (*dto.GetCostAnalyticsBreakdownResponse, error)
    
    // GetCombinedAnalytics combines cost and revenue analytics
    GetCombinedAnalytics(ctx context.Context, req *dto.GetCombinedAnalyticsRequest) (*dto.GetCombinedAnalyticsResponse, error)
}

// Internal helper interface for cost calculation strategies
type CostCalculationStrategy interface {
    // CalculateCosts calculates costs for a set of events and meters
    CalculateCosts(ctx context.Context, events []*events.Event, meters []*meter.Meter, prices map[string]*price.Price) ([]CostAnalyticItem, error)
    
    // SupportsComplexPricing returns true if this strategy can handle complex pricing models
    SupportsComplexPricing() bool
}
```

#### Request DTOs

```go
// Location: internal/api/dto/costsheet_analytics.go

type GetCostAnalyticsRequest struct {
    // Required fields
    StartTime          time.Time `json:"start_time" validate:"required"`
    EndTime            time.Time `json:"end_time" validate:"required"`
    
    // Optional filters - at least one of these should be provided
    CostsheetV2ID      string   `json:"costsheet_v2_id,omitempty"`
    ExternalCustomerID string   `json:"external_customer_id,omitempty"` // Optional - for specific customer
    CustomerIDs        []string `json:"customer_ids,omitempty"`         // Optional - for multiple customers
    
    // Additional filters
    MeterIDs       []string              `json:"meter_ids,omitempty"`
    Sources        []string              `json:"sources,omitempty"`
    WindowSize     types.WindowSize      `json:"window_size,omitempty"` // For time-series
    GroupBy        []string              `json:"group_by,omitempty"`    // "meter_id", "source", "customer_id"
    PropertyFilters map[string][]string  `json:"property_filters,omitempty"`
    
    // Additional options
    IncludeTimeSeries bool `json:"include_time_series,omitempty"`
    IncludeBreakdown  bool `json:"include_breakdown,omitempty"`
}

type GetCombinedAnalyticsRequest struct {
    GetCostAnalyticsRequest
    // Revenue analytics options can be inherited or specified separately
    IncludeRevenue bool `json:"include_revenue"`
}
```

#### Response DTOs

```go
type CostAnalyticItem struct {
    MeterID     string          `json:"meter_id"`
    MeterName   string          `json:"meter_name,omitempty"`
    Source      string          `json:"source,omitempty"`
    CustomerID  string          `json:"customer_id,omitempty"`
    ExternalCustomerID string   `json:"external_customer_id,omitempty"`
    Properties  map[string]string `json:"properties,omitempty"`
    
    // Aggregated metrics
    TotalCost     decimal.Decimal `json:"total_cost"`
    TotalQuantity decimal.Decimal `json:"total_quantity"`
    TotalEvents   int64           `json:"total_events"`
    
    // Breakdown
    CostByPeriod []CostPoint `json:"cost_by_period,omitempty"` // Time-series
    
    // Metadata
    Currency      string          `json:"currency"`
    PriceID       string          `json:"price_id,omitempty"`
    CostsheetV2ID string          `json:"costsheet_v2_id,omitempty"`
}

type CostPoint struct {
    Timestamp time.Time       `json:"timestamp"`
    Cost      decimal.Decimal `json:"cost"`
    Quantity  decimal.Decimal `json:"quantity"`
    EventCount int64          `json:"event_count"`
}

type GetCostAnalyticsResponse struct {
    CustomerID         string              `json:"customer_id"`
    ExternalCustomerID string              `json:"external_customer_id"`
    StartTime          time.Time           `json:"start_time"`
    EndTime            time.Time           `json:"end_time"`
    Currency           string              `json:"currency"`
    
    // Summary
    TotalCost      decimal.Decimal     `json:"total_cost"`
    TotalQuantity  decimal.Decimal     `json:"total_quantity"`
    TotalEvents    int64               `json:"total_events"`
    
    // Detailed breakdown
    CostAnalytics  []CostAnalyticItem  `json:"cost_analytics"`
    
    // Time-series (if requested)
    CostTimeSeries []CostPoint         `json:"cost_time_series,omitempty"`
}

type GetCombinedAnalyticsResponse struct {
    // Cost metrics
    CostAnalytics *GetCostAnalyticsResponse `json:"cost_analytics"`
    
    // Revenue metrics (from existing analytics)
    RevenueAnalytics *GetUsageAnalyticsResponse `json:"revenue_analytics,omitempty"`
    
    // Derived metrics
    TotalRevenue decimal.Decimal `json:"total_revenue"`
    TotalCost    decimal.Decimal `json:"total_cost"`
    Margin       decimal.Decimal `json:"margin"`        // Revenue - Cost
    MarginPercent decimal.Decimal `json:"margin_percent"` // (Margin / Revenue) * 100
    ROI          decimal.Decimal `json:"roi"`            // (Revenue - Cost) / Cost
    ROIPercent   decimal.Decimal `json:"roi_percent"`    // ROI * 100
    
    Currency     string          `json:"currency"`
    StartTime    time.Time       `json:"start_time"`
    EndTime      time.Time       `json:"end_time"`
}
```

### 2. Repository Layer

#### ClickHouse Query Patterns

```go
// Location: internal/repository/clickhouse/costsheet_analytics.go

type CostsheetAnalyticsRepository interface {
    // GetCostAnalytics retrieves cost analytics from events_processed
    GetCostAnalytics(ctx context.Context, params *CostAnalyticsParams) ([]CostAnalyticItem, error)
    
    // GetCostTimeSeries retrieves time-series cost data
    GetCostTimeSeries(ctx context.Context, params *CostAnalyticsParams, analytic *CostAnalyticItem) ([]CostPoint, error)
}

type CostAnalyticsParams struct {
    TenantID           string
    EnvironmentID      string
    CustomerID         string
    ExternalCustomerID string
    
    // Filters
    CostsheetV2IDs []string
    MeterIDs       []string
    FeatureIDs     []string
    Sources        []string
    StartTime      time.Time
    EndTime        time.Time
    
    // Grouping & Aggregation
    GroupBy        []string // "meter_id", "feature_id", "source", "properties.<field>"
    WindowSize     types.WindowSize
    PropertyFilters map[string][]string
    
    // Pagination
    Limit  int
    Offset int
}
```

#### Key ClickHouse Queries

**Raw Events Query (used by existing BulkGetUsageByMeter)**
```sql
-- This query is already implemented in the existing event service
-- We'll reuse BulkGetUsageByMeter which handles:
-- 1. Fetching raw events from ClickHouse
-- 2. Applying meter aggregation logic
-- 3. Grouping by time periods if needed
-- 4. Handling all aggregation types (SUM, MAX, COUNT_UNIQUE, etc.)

-- The query pattern (handled internally by BulkGetUsageByMeter):
SELECT 
    id,
    event_name,
    external_customer_id,
    customer_id,
    source,
    timestamp,
    properties
FROM events
WHERE tenant_id = ?
  AND environment_id = ?
  [AND external_customer_id IN (?)] -- Optional customer filter
  AND timestamp >= ?
  AND timestamp <= ?
  [AND event_name IN (?)] -- Filter by meter event names
  [AND source IN (?)] -- Optional source filter
ORDER BY timestamp ASC
```

```sql
-- Time-series cost query
SELECT 
    toStartOfHour(timestamp) AS window_time, -- Adjust based on window_size
    SUM(cost * sign) AS cost,
    SUM(qty_total * sign) AS quantity,
    COUNT(DISTINCT id) AS event_count
FROM events_processed
WHERE tenant_id = ?
  AND environment_id = ?
  AND customer_id = ?
  AND timestamp >= ?
  AND timestamp <= ?
  AND meter_id = ? -- From analytic item
  AND feature_id = ?
  AND sign != 0
GROUP BY window_time
ORDER BY window_time ASC
```

### 3. Service Implementation

```go
// Location: internal/service/costsheet_analytics.go

type costsheetAnalyticsService struct {
    ServiceParams
}

func NewCostsheetAnalyticsService(params ServiceParams) CostsheetAnalyticsService {
    return &costsheetAnalyticsService{
        ServiceParams: params,
    }
}

// Main implementation flow (modeled after GetUsageBySubscription)
func (s *costsheetAnalyticsService) GetCostAnalytics(
    ctx context.Context, 
    req *dto.GetCostAnalyticsRequest,
) (*dto.GetCostAnalyticsResponse, error) {
    // 1. Validate request - at least one filter must be provided
    if err := s.validateRequest(req); err != nil {
        return nil, err
    }
    
    // 2. Fetch costsheet and associated prices (like subscription line items)
    costsheet, prices, err := s.fetchCostsheetData(ctx, req.CostsheetV2ID)
    if err != nil {
        return nil, err
    }
    
    // 3. Get customers based on request filters
    customers, err := s.fetchCustomers(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // 4. Build meter usage requests for each customer-price combination
    meterUsageRequests := s.buildMeterUsageRequests(ctx, customers, prices, req)
    
    // 5. Use existing BulkGetUsageByMeter (same as subscription billing)
    eventService := NewEventService(s.ServiceParams)
    usageMap, err := eventService.BulkGetUsageByMeter(ctx, meterUsageRequests)
    if err != nil {
        return nil, err
    }
    
    // 6. Calculate costs using price service (same logic as subscription)
    costAnalytics := s.calculateCostsFromUsage(ctx, usageMap, prices, customers, req)
    
    // 7. Optionally add time-series data
    if req.IncludeTimeSeries {
        costAnalytics = s.addTimeSeriesData(ctx, req, costAnalytics)
    }
    
    // 8. Build response
    return s.buildResponse(req, costAnalytics), nil
}

// buildMeterUsageRequests creates usage requests for each customer-price combination
func (s *costsheetAnalyticsService) buildMeterUsageRequests(
    ctx context.Context,
    customers []*customer.Customer,
    prices []*price.Price,
    req *dto.GetCostAnalyticsRequest,
) []*dto.GetUsageByMeterRequest {
    var meterUsageRequests []*dto.GetUsageByMeterRequest
    
    // For each customer and each price, create a usage request
    for _, customer := range customers {
        for _, price := range prices {
            // Skip if meter ID is not set
            if price.MeterID == "" {
                continue
            }
            
            // Apply meter ID filter if specified
            if len(req.MeterIDs) > 0 && !contains(req.MeterIDs, price.MeterID) {
                continue
            }
            
            usageRequest := &dto.GetUsageByMeterRequest{
                MeterID:            price.MeterID,
                PriceID:            price.ID,
                ExternalCustomerID: customer.ExternalID,
                StartTime:          req.StartTime,
                EndTime:            req.EndTime,
                Filters:            make(map[string][]string),
            }
            
            // Apply property filters if specified
            for key, values := range req.PropertyFilters {
                usageRequest.Filters[key] = values
            }
            
            meterUsageRequests = append(meterUsageRequests, usageRequest)
        }
    }
    
    return meterUsageRequests
}

// calculateCostsFromUsage processes usage results and calculates costs
func (s *costsheetAnalyticsService) calculateCostsFromUsage(
    ctx context.Context,
    usageMap map[string]*events.AggregationResult,
    prices []*price.Price,
    customers []*customer.Customer,
    req *dto.GetCostAnalyticsRequest,
) []CostAnalyticItem {
    priceService := NewPriceService(s.ServiceParams)
    var costAnalytics []CostAnalyticItem
    
    // Build price map for quick lookup
    priceMap := make(map[string]*price.Price)
    for _, p := range prices {
        priceMap[p.ID] = p
    }
    
    // Build customer map for quick lookup
    customerMap := make(map[string]*customer.Customer)
    for _, c := range customers {
        customerMap[c.ID] = c
    }
    
    // Process each usage result
    for priceID, usage := range usageMap {
        price, exists := priceMap[priceID]
        if !exists {
            continue
        }
        
        // Find customer for this usage (from external_customer_id in usage)
        var customer *customer.Customer
        for _, c := range customers {
            if c.ExternalID == usage.ExternalCustomerID {
                customer = c
                break
            }
        }
        if customer == nil {
            continue
        }
        
        // Calculate cost (same logic as GetUsageBySubscription)
        var cost decimal.Decimal
        var quantity decimal.Decimal
        
        // Handle bucketed max meters
        if usage.MeterID != "" {
            meter, err := s.MeterRepo.GetMeter(ctx, usage.MeterID)
            if err == nil && meter.IsBucketedMaxMeter() {
                // For bucketed max, use array of values
                bucketedValues := make([]decimal.Decimal, len(usage.Results))
                for i, result := range usage.Results {
                    bucketedValues[i] = result.Value
                }
                cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)
                
                // Calculate quantity as sum of all bucket maxes
                quantity = decimal.Zero
                for _, bucketValue := range bucketedValues {
                    quantity = quantity.Add(bucketValue)
                }
            } else {
                // For all other cases, use single value
                quantity = usage.Value
                cost = priceService.CalculateCost(ctx, price, quantity)
            }
        } else {
            quantity = usage.Value
            cost = priceService.CalculateCost(ctx, price, quantity)
        }
        
        costAnalytic := CostAnalyticItem{
            MeterID:            usage.MeterID,
            MeterName:          "", // Will be enriched later
            Source:             "", // Can be extracted from usage if needed
            CustomerID:         customer.ID,
            ExternalCustomerID: customer.ExternalID,
            TotalCost:          cost,
            TotalQuantity:      quantity,
            TotalEvents:        int64(len(usage.Results)),
            Currency:           price.Currency,
            PriceID:            price.ID,
            CostsheetV2ID:      price.EntityID, // Assuming price is linked to costsheet
        }
        
        costAnalytics = append(costAnalytics, costAnalytic)
    }
    
    return costAnalytics
}

// aggregateUsageForMeter applies meter-specific aggregation logic
func (s *costsheetAnalyticsService) aggregateUsageForMeter(
    events []*events.Event,
    meter *meter.Meter,
) *UsageAggregation {
    aggregation := &UsageAggregation{
        TotalQuantity: decimal.Zero,
        MaxQuantity:   decimal.Zero,
        UniqueValues:  make(map[string]bool),
        EventCount:    len(events),
    }
    
    switch meter.Aggregation.Type {
    case types.AggregationSum, types.AggregationSumWithMultiplier:
        for _, event := range events {
            if val, ok := event.Properties[meter.Aggregation.Field]; ok {
                if decVal, err := decimal.NewFromString(fmt.Sprintf("%v", val)); err == nil {
                    if meter.Aggregation.Multiplier != nil {
                        decVal = decVal.Mul(*meter.Aggregation.Multiplier)
                    }
                    aggregation.TotalQuantity = aggregation.TotalQuantity.Add(decVal)
                }
            }
        }
        
    case types.AggregationMax:
        for _, event := range events {
            if val, ok := event.Properties[meter.Aggregation.Field]; ok {
                if decVal, err := decimal.NewFromString(fmt.Sprintf("%v", val)); err == nil {
                    if decVal.GreaterThan(aggregation.MaxQuantity) {
                        aggregation.MaxQuantity = decVal
                    }
                }
            }
        }
        aggregation.TotalQuantity = aggregation.MaxQuantity
        
    case types.AggregationCount:
        aggregation.TotalQuantity = decimal.NewFromInt(int64(len(events)))
        
    case types.AggregationCountUnique:
        for _, event := range events {
            if val, ok := event.Properties[meter.Aggregation.Field]; ok {
                aggregation.UniqueValues[fmt.Sprintf("%v", val)] = true
            }
        }
        aggregation.TotalQuantity = decimal.NewFromInt(int64(len(aggregation.UniqueValues)))
        
    case types.AggregationWeightedSum:
        // Implement weighted sum logic
        aggregation.TotalQuantity = s.calculateWeightedSum(events, meter)
    }
    
    return aggregation
}

// calculateCostForUsage calculates cost based on meter aggregation and price model
func (s *costsheetAnalyticsService) calculateCostForUsage(
    ctx context.Context,
    price *price.Price,
    meter *meter.Meter,
    usage *UsageAggregation,
) decimal.Decimal {
    priceService := NewPriceService(s.ServiceParams)
    
    switch meter.Aggregation.Type {
    case types.AggregationMax:
        if meter.IsBucketedMaxMeter() {
            // For bucketed max, we need time-series data
            // This is a simplified version - in reality, we'd need to fetch bucketed data
            bucketedValues := []decimal.Decimal{usage.MaxQuantity}
            return priceService.CalculateBucketedCost(ctx, price, bucketedValues)
        } else {
            return priceService.CalculateCost(ctx, price, usage.MaxQuantity)
        }
        
    default:
        return priceService.CalculateCost(ctx, price, usage.TotalQuantity)
    }
}

type UsageAggregation struct {
    TotalQuantity decimal.Decimal
    MaxQuantity   decimal.Decimal
    UniqueValues  map[string]bool
    EventCount    int
}

func (s *costsheetAnalyticsService) GetCombinedAnalytics(
    ctx context.Context,
    req *dto.GetCombinedAnalyticsRequest,
) (*dto.GetCombinedAnalyticsResponse, error) {
    // 1. Fetch cost analytics
    costReq := req.GetCostAnalyticsRequest
    costAnalytics, err := s.GetCostAnalytics(ctx, &costReq)
    if err != nil {
        return nil, err
    }
    
    // 2. Fetch revenue analytics (use existing service)
    var revenueAnalytics *dto.GetUsageAnalyticsResponse
    if req.IncludeRevenue {
        revenueReq := s.buildRevenueRequest(req)
        featureUsageService := NewFeatureUsageTrackingService(s.ServiceParams)
        revenueAnalytics, err = featureUsageService.GetDetailedUsageAnalytics(ctx, revenueReq)
        if err != nil {
            s.Logger.Warnw("failed to fetch revenue analytics", "error", err)
            // Continue without revenue data
        }
    }
    
    // 3. Compute derived metrics
    response := &dto.GetCombinedAnalyticsResponse{
        CostAnalytics:    costAnalytics,
        RevenueAnalytics: revenueAnalytics,
        TotalCost:        costAnalytics.TotalCost,
        Currency:         costAnalytics.Currency,
        StartTime:        costAnalytics.StartTime,
        EndTime:          costAnalytics.EndTime,
    }
    
    if revenueAnalytics != nil {
        response.TotalRevenue = s.calculateTotalRevenue(revenueAnalytics)
        response.Margin = response.TotalRevenue.Sub(response.TotalCost)
        
        if !response.TotalRevenue.IsZero() {
            response.MarginPercent = response.Margin.Div(response.TotalRevenue).Mul(decimal.NewFromInt(100))
        }
        
        if !response.TotalCost.IsZero() {
            response.ROI = response.Margin.Div(response.TotalCost)
            response.ROIPercent = response.ROI.Mul(decimal.NewFromInt(100))
        }
    }
    
    return response, nil
}
```

### 4. API Handler

```go
// Location: internal/api/v1/costsheet_analytics.go

type CostsheetAnalyticsHandler struct {
    costsheetAnalyticsService service.CostsheetAnalyticsService
    config                    *config.Configuration
    Logger                    *logger.Logger
}

// @Summary Get cost analytics for a customer
// @Description Retrieve cost analytics with breakdown by meter, feature, and time
// @Tags Cost Analytics
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetCostAnalyticsRequest true "Cost analytics request"
// @Success 200 {object} dto.GetCostAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /analytics/cost [post]
func (h *CostsheetAnalyticsHandler) GetCostAnalytics(c *gin.Context) {
    ctx := c.Request.Context()
    
    var req dto.GetCostAnalyticsRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(ierr.WithError(err).
            WithHint("Please check the request payload").
            Mark(ierr.ErrValidation))
        return
    }
    
    response, err := h.costsheetAnalyticsService.GetCostAnalytics(ctx, &req)
    if err != nil {
        c.Error(err)
        return
    }
    
    c.JSON(http.StatusOK, response)
}

// @Summary Get combined revenue and cost analytics
// @Description Retrieve combined analytics with ROI, margin, and detailed breakdowns
// @Tags Analytics
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetCombinedAnalyticsRequest true "Combined analytics request"
// @Success 200 {object} dto.GetCombinedAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /analytics/combined [post]
func (h *CostsheetAnalyticsHandler) GetCombinedAnalytics(c *gin.Context) {
    ctx := c.Request.Context()
    
    var req dto.GetCombinedAnalyticsRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(ierr.WithError(err).
            WithHint("Please check the request payload").
            Mark(ierr.ErrValidation))
        return
    }
    
    response, err := h.costsheetAnalyticsService.GetCombinedAnalytics(ctx, &req)
    if err != nil {
        c.Error(err)
        return
    }
    
    c.JSON(http.StatusOK, response)
}
```

---

## 🗄️ Database Schema Changes

### No Schema Changes Required! ✅

The existing schema is sufficient:
- **`events_processed`** already contains `cost` and `unit_cost` fields
- **`costsheet_v2`** table is already created (in staged changes)
- **Prices table** already supports `entity_type` = `COSTSHEET_V2`

### Optional: Future Optimizations

If performance becomes an issue, consider:

1. **Materialized View** for cost aggregations:
```sql
CREATE MATERIALIZED VIEW cost_analytics_mv
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (tenant_id, environment_id, customer_id, meter_id, feature_id, timestamp)
AS SELECT
    tenant_id,
    environment_id,
    customer_id,
    meter_id,
    feature_id,
    source,
    currency,
    toStartOfHour(timestamp) as timestamp,
    sum(cost * sign) as total_cost,
    sum(qty_total * sign) as total_quantity,
    count(distinct id) as event_count
FROM events_processed
WHERE sign != 0
GROUP BY tenant_id, environment_id, customer_id, meter_id, feature_id, source, currency, timestamp;
```

2. **Caching Layer** in Redis for frequently accessed analytics

---

## 🔌 API Design

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/analytics/cost` | Get cost analytics for a customer |
| POST | `/v1/analytics/combined` | Get combined revenue + cost analytics with derived metrics |
| POST | `/v1/analytics/cost/breakdown` | Get detailed cost breakdown by dimensions |

### Request Examples

#### 1. Basic Cost Analytics

```json
POST /v1/analytics/cost

{
  "costsheet_v2_id": "costsheet_123",
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-31T23:59:59Z",
  "group_by": ["meter_id", "customer_id"],
  "include_breakdown": true
}
```

#### 2. Combined Analytics (Revenue + Cost)

```json
POST /v1/analytics/combined

{
  "costsheet_v2_id": "costsheet_123",
  "external_customer_id": "customer_123", 
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-31T23:59:59Z",
  "include_revenue": true,
  "include_time_series": true,
  "window_size": "DAY",
  "group_by": ["meter_id"]
}
```

#### 3. Cost Analytics with Time-Series

```json
POST /v1/analytics/cost

{
  "costsheet_v2_id": "costsheet_123",
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-31T23:59:59Z",
  "window_size": "HOUR",
  "include_time_series": true,
  "meter_ids": ["meter_api_calls", "meter_storage"]
}
```

### Response Example

```json
{
  "cost_analytics": {
    "customer_id": "cust_internal_123",
    "external_customer_id": "customer_123",
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-31T23:59:59Z",
    "currency": "USD",
    "total_cost": "1250.50",
    "total_quantity": "500000",
    "total_events": 45000,
    "cost_analytics": [
      {
        "meter_id": "meter_api_calls",
        "meter_name": "API Calls",
        "feature_id": "feature_api",
        "feature_name": "API Access",
        "total_cost": "850.30",
        "total_quantity": "425000",
        "total_events": 42500,
        "currency": "USD"
      },
      {
        "meter_id": "meter_storage",
        "meter_name": "Storage Usage",
        "feature_id": "feature_storage",
        "feature_name": "Cloud Storage",
        "total_cost": "400.20",
        "total_quantity": "75000",
        "total_events": 2500,
        "currency": "USD"
      }
    ]
  },
  "revenue_analytics": {
    "total_revenue": "2500.00",
    // ... existing revenue analytics structure
  },
  "total_revenue": "2500.00",
  "total_cost": "1250.50",
  "margin": "1249.50",
  "margin_percent": "49.98",
  "roi": "0.9996",
  "roi_percent": "99.96",
  "currency": "USD",
  "start_time": "2024-01-01T00:00:00Z",
  "end_time": "2024-01-31T23:59:59Z"
}
```

---

## 🔄 Cost Calculation Strategy Details

### Pricing Model Approach

| Pricing Model | Aggregation Types | Calculation Source | Strategy |
|---------------|-------------------|-------------------|----------|
| **All Models** | All Types | Raw Events Only | 🔄 Dynamic Calculation |

**Rationale**: Use raw events for all calculations to ensure consistency with billing system and avoid discrepancies.

### Cost Calculation Pattern (Based on GetUsageBySubscription)

```go
func (s *costsheetAnalyticsService) GetCostAnalytics(
    ctx context.Context, 
    req *dto.GetCostAnalyticsRequest,
) (*dto.GetCostAnalyticsResponse, error) {
    // 1. Validate request - at least one filter must be provided
    if err := s.validateRequest(req); err != nil {
        return nil, err
    }
    
    // 2. Fetch costsheet and associated prices (similar to subscription line items)
    costsheet, prices, err := s.fetchCostsheetData(ctx, req.CostsheetV2ID)
    if err != nil {
        return nil, err
    }
    
    // 3. Get customers (if specific customers requested)
    customers, err := s.fetchCustomers(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // 4. Build meter usage requests (similar to GetUsageBySubscription pattern)
    meterUsageRequests := s.buildMeterUsageRequests(ctx, customers, prices, req)
    
    // 5. Bulk get usage by meter (reuse existing BulkGetUsageByMeter)
    eventService := NewEventService(s.ServiceParams)
    usageMap, err := eventService.BulkGetUsageByMeter(ctx, meterUsageRequests)
    if err != nil {
        return nil, err
    }
    
    // 6. Calculate costs using price service (same as subscription billing)
    costAnalytics := s.calculateCostsFromUsage(ctx, usageMap, prices, customers)
    
    // 7. Build response
    return s.buildResponse(req, costAnalytics), nil
}
```

### Raw Events Processing Flow

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Raw Events    │───▶│  Group by Meter  │───▶│   Aggregate     │
│   (ClickHouse)  │    │   & Time Period  │    │   by Type       │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                                         │
                                                         ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Final Cost    │◀───│  Apply Pricing   │◀───│  Usage Results  │
│   Analytics     │    │   Model Logic    │    │  (per meter)    │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

### Aggregation Type Implementations

#### 1. SUM Aggregation
```go
func (s *costsheetAnalyticsService) aggregateSum(events []*events.Event, meter *meter.Meter) decimal.Decimal {
    total := decimal.Zero
    for _, event := range events {
        if val, ok := event.Properties[meter.Aggregation.Field]; ok {
            if decVal, err := decimal.NewFromString(fmt.Sprintf("%v", val)); err == nil {
                total = total.Add(decVal)
            }
        }
    }
    return total
}
```

#### 2. MAX Aggregation (Simple)
```go
func (s *costsheetAnalyticsService) aggregateMax(events []*events.Event, meter *meter.Meter) decimal.Decimal {
    max := decimal.Zero
    for _, event := range events {
        if val, ok := event.Properties[meter.Aggregation.Field]; ok {
            if decVal, err := decimal.NewFromString(fmt.Sprintf("%v", val)); err == nil {
                if decVal.GreaterThan(max) {
                    max = decVal
                }
            }
        }
    }
    return max
}
```

#### 3. MAX Aggregation (Bucketed)
```go
func (s *costsheetAnalyticsService) aggregateBucketedMax(
    events []*events.Event, 
    meter *meter.Meter,
    bucketSize types.WindowSize,
) []decimal.Decimal {
    // Group events by time buckets
    buckets := s.groupEventsByTimeBucket(events, bucketSize)
    
    var bucketedValues []decimal.Decimal
    for _, bucketEvents := range buckets {
        maxInBucket := s.aggregateMax(bucketEvents, meter)
        bucketedValues = append(bucketedValues, maxInBucket)
    }
    
    return bucketedValues
}
```

#### 4. COUNT_UNIQUE Aggregation
```go
func (s *costsheetAnalyticsService) aggregateCountUnique(events []*events.Event, meter *meter.Meter) decimal.Decimal {
    uniqueValues := make(map[string]bool)
    
    for _, event := range events {
        if val, ok := event.Properties[meter.Aggregation.Field]; ok {
            uniqueValues[fmt.Sprintf("%v", val)] = true
        }
    }
    
    return decimal.NewFromInt(int64(len(uniqueValues)))
}
```

#### 5. WEIGHTED_SUM Aggregation
```go
func (s *costsheetAnalyticsService) aggregateWeightedSum(events []*events.Event, meter *meter.Meter) decimal.Decimal {
    total := decimal.Zero
    
    for _, event := range events {
        // Get value
        val, valOk := event.Properties[meter.Aggregation.Field]
        // Get weight
        weight, weightOk := event.Properties[meter.Aggregation.WeightField]
        
        if valOk && weightOk {
            if decVal, err := decimal.NewFromString(fmt.Sprintf("%v", val)); err == nil {
                if decWeight, err := decimal.NewFromString(fmt.Sprintf("%v", weight)); err == nil {
                    weighted := decVal.Mul(decWeight)
                    total = total.Add(weighted)
                }
            }
        }
    }
    
    return total
}
```

### Performance Optimizations

#### 1. Batch Processing
```go
func (s *costsheetAnalyticsService) processCostAnalyticsBatch(
    ctx context.Context,
    requests []*dto.GetCostAnalyticsRequest,
) ([]*dto.GetCostAnalyticsResponse, error) {
    // Process multiple customers in parallel
    results := make([]*dto.GetCostAnalyticsResponse, len(requests))
    
    // Use worker pool for concurrent processing
    semaphore := make(chan struct{}, 10) // Limit to 10 concurrent requests
    
    var wg sync.WaitGroup
    for i, req := range requests {
        wg.Add(1)
        go func(index int, request *dto.GetCostAnalyticsRequest) {
            defer wg.Done()
            semaphore <- struct{}{} // Acquire
            defer func() { <-semaphore }() // Release
            
            result, err := s.GetCostAnalytics(ctx, request)
            if err != nil {
                s.Logger.Errorw("batch processing failed", "error", err, "customer", request.ExternalCustomerID)
                return
            }
            results[index] = result
        }(i, req)
    }
    
    wg.Wait()
    return results, nil
}
```

#### 2. Query Optimization
```go
func (s *costsheetAnalyticsService) optimizeEventQuery(
    params *events.FindEventsParams,
    meters []*meter.Meter,
) *events.FindEventsParams {
    // Only fetch event names that are relevant to our meters
    eventNames := make([]string, 0, len(meters))
    for _, meter := range meters {
        eventNames = append(eventNames, meter.EventName)
    }
    params.EventNames = eventNames
    
    // Optimize property filters based on meter aggregation fields
    requiredProperties := make([]string, 0)
    for _, meter := range meters {
        if meter.Aggregation.Field != "" {
            requiredProperties = append(requiredProperties, meter.Aggregation.Field)
        }
        if meter.Aggregation.WeightField != "" {
            requiredProperties = append(requiredProperties, meter.Aggregation.WeightField)
        }
    }
    params.RequiredProperties = requiredProperties
    
    return params
}
```

---

## 🧪 Testing Strategy

### 1. Unit Tests

- ✅ Cost calculation logic
- ✅ Analytics parameter building
- ✅ Response transformation
- ✅ Metadata enrichment
- ✅ Derived metrics computation (ROI, Margin)

### 2. Integration Tests

- ✅ ClickHouse query execution
- ✅ Service layer integration
- ✅ API endpoint testing
- ✅ Error handling scenarios
- ✅ Edge cases (zero cost, zero revenue, etc.)

### 3. Performance Tests

- ✅ Query performance with large datasets (1M+ events)
- ✅ Time-series aggregation performance
- ✅ Concurrent request handling
- ✅ Cache effectiveness (if implemented)

### 4. Test Data Setup

```go
// Setup test events in ClickHouse
func setupTestCostData(t *testing.T) {
    events := []ProcessedEvent{
        {
            ID: "event_1",
            TenantID: "tenant_1",
            CustomerID: "cust_1",
            MeterID: "meter_api",
            FeatureID: "feature_api",
            Cost: decimal.NewFromFloat(10.50),
            QtyTotal: decimal.NewFromInt(100),
            Timestamp: time.Now().Add(-1 * time.Hour),
            // ... other fields
        },
        // ... more test events
    }
    
    // Insert into ClickHouse
    // ...
}
```

---

## ⚡ Performance Considerations

### 1. Query Optimization

- **Leverage ClickHouse Indexes**: Use bloom filters on `meter_id`, `feature_id`, `source`
- **Partition Pruning**: Filter by `timestamp` to leverage monthly partitions
- **Limit Data Scanned**: Use appropriate time ranges and customer filters

### 2. Caching Strategy

```go
// Cache key pattern
// costanalytics:{tenant_id}:{customer_id}:{start_time}:{end_time}:{hash(filters)}

func (s *costsheetAnalyticsService) GetCostAnalytics(ctx context.Context, req *dto.GetCostAnalyticsRequest) (*dto.GetCostAnalyticsResponse, error) {
    // 1. Check cache
    cacheKey := s.buildCacheKey(req)
    if cached, found := s.Cache.Get(cacheKey); found {
        return cached.(*dto.GetCostAnalyticsResponse), nil
    }
    
    // 2. Fetch from database
    response, err := s.fetchCostAnalytics(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // 3. Cache result (TTL: 5 minutes)
    s.Cache.Set(cacheKey, response, 5*time.Minute)
    
    return response, nil
}
```

### 3. Pagination

For large result sets, implement cursor-based pagination:

```go
type GetCostAnalyticsRequest struct {
    // ... existing fields
    Limit  int    `json:"limit,omitempty"`
    Cursor string `json:"cursor,omitempty"` // Base64 encoded pagination cursor
}

type GetCostAnalyticsResponse struct {
    // ... existing fields
    NextCursor string `json:"next_cursor,omitempty"`
    HasMore    bool   `json:"has_more"`
}
```

### 4. Batch Processing

For bulk analytics (multiple customers), implement batch processing:

```go
func (s *costsheetAnalyticsService) GetBatchCostAnalytics(
    ctx context.Context,
    requests []*dto.GetCostAnalyticsRequest,
) ([]*dto.GetCostAnalyticsResponse, error) {
    // Process in parallel using goroutines
    // ...
}
```

---

## 🚀 Rollout Strategy

### Phase 1: Internal Testing
- **Week 1**: Deploy to dev environment
- Internal testing with sample data
- Performance benchmarking

### Phase 2: Beta Release
- **Week 2**: Deploy to staging
- Select beta customers for testing
- Collect feedback

### Phase 3: Production Rollout
- **Week 3**: Gradual production rollout
- Monitor performance metrics
- Set up alerts and monitoring

### Phase 4: Feature Flag
- Use feature flags to control access
- Gradual rollout to all customers
- Monitor usage and performance

---

## ⚠️ Risk Assessment

### Risk 1: ClickHouse Query Performance
**Probability**: Medium  
**Impact**: High  
**Mitigation**:
- Implement query timeouts
- Add caching layer
- Use materialized views for frequently accessed data
- Monitor query performance with alerting

### Risk 2: Data Consistency
**Probability**: Low  
**Impact**: High  
**Mitigation**:
- Validate data integrity during processing
- Implement retry logic for failed queries
- Add data quality checks

### Risk 3: Cost Calculation Accuracy
**Probability**: Low  
**Impact**: Critical  
**Mitigation**:
- Comprehensive unit tests
- Validation against existing billing data
- Regular audits and reconciliation

### Risk 4: API Rate Limits
**Probability**: Medium  
**Impact**: Medium  
**Mitigation**:
- Implement rate limiting
- Add request throttling
- Provide batch API endpoints

---

## 📊 Success Metrics

1. **Performance**:
   - API response time < 500ms (p95)
   - ClickHouse query time < 200ms (p95)
   - Cache hit rate > 60%

2. **Accuracy**:
   - Cost calculation accuracy: 100%
   - Data consistency: 99.99%

3. **Adoption**:
   - API usage growth: 20% month-over-month
   - Customer satisfaction score: > 4.5/5

4. **Reliability**:
   - API uptime: 99.9%
   - Error rate: < 0.1%

---

## 📚 Dependencies

### Internal Dependencies
- ✅ Cost Sheet V2 implementation (already staged)
- ✅ ClickHouse `events_processed` table
- ✅ Existing billing services
- ✅ Feature Usage Tracking Service

### External Dependencies
- ClickHouse (v21.3+)
- PostgreSQL (for metadata)
- Redis (optional, for caching)

---

## 🗓️ Timeline Summary

| Phase | Duration | Start | End |
|-------|----------|-------|-----|
| Phase 1: Core Service | 4-5 days | Day 1 | Day 5 |
| Phase 2: Integration | 2-3 days | Day 6 | Day 8 |
| Phase 3: Advanced Features | 2-3 days | Day 9 | Day 11 |
| Phase 4: Testing & Docs | 2-3 days | Day 12 | Day 14 |
| **Total** | **10-14 days** | | |

---

## 📝 Implementation Checklist

### Phase 1: Core Service
- [ ] Create `CostsheetAnalyticsService` interface
- [ ] Implement cost calculation strategies (raw vs processed events)
- [ ] Implement `CostAnalyticsRepository` for both raw and processed events
- [ ] Create meter aggregation logic for all aggregation types
- [ ] Implement cost calculation using billing services
- [ ] Create DTOs for requests and responses
- [ ] Add comprehensive unit tests for all aggregation types
- [ ] Add integration tests with billing services

### Phase 2: Integration
- [ ] Create API handlers
- [ ] Implement combined analytics endpoint
- [ ] Add Swagger documentation
- [ ] Integrate with existing analytics API
- [ ] Add integration tests

### Phase 3: Advanced Features
- [ ] Implement time-series support
- [ ] Add multi-dimensional grouping
- [ ] Implement caching layer
- [ ] Add pagination support
- [ ] Performance benchmarking

### Phase 4: Testing & Documentation
- [ ] Comprehensive integration tests
- [ ] Load testing
- [ ] API documentation
- [ ] User guide
- [ ] Deployment documentation

---

## 🎓 Key Insights & Decisions

### 1. Raw Events Only Strategy
**Decision**: Use raw events + billing services for all cost calculations, following the same pattern as `GetUsageBySubscription`.  
**Rationale**: Ensures consistency with existing billing system and avoids any discrepancies between cost analytics and actual billing.

### 2. Modular Service Design
**Decision**: Create separate `CostsheetAnalyticsService` that can be used independently or combined with revenue analytics.  
**Rationale**: Provides flexibility and maintains separation of concerns.

### 3. Optional Revenue Integration
**Decision**: Make revenue analytics optional in combined endpoint.  
**Rationale**: Some users may only want cost analytics without revenue data.

### 4. Consistent API Design
**Decision**: Follow existing analytics API patterns (request/response structures, error handling).  
**Rationale**: Maintains consistency across the codebase and reduces learning curve.

---

## 📧 Questions & Clarifications Needed

1. **Costsheet-Price Association**: How are prices associated with costsheets? Through `entity_type` and `entity_id` in the prices table?
   
2. **Multi-Currency Support**: How should we handle analytics when a customer has costs in multiple currencies?

3. **Historical Data**: Should we support analytics for historical data before costsheets were implemented?

4. **Access Control**: Who should have access to cost analytics? Same permissions as revenue analytics?

5. **Export Functionality**: Should we support exporting analytics data (CSV, PDF)?

---

## 🔗 Related Documentation

- [Event Post-Processing V2](docs/prds/event_post_processing_v2.md)
- [Analytics Function Refactor](docs/prds/refactor-analytics-function.md)
- [Cost Sheet PRD](cost_sheet.md)
- [ClickHouse Repository README](internal/repository/clickhouse/README.md)

---

## 👥 Stakeholders

- **Engineering Team**: Implementation and technical review
- **Product Team**: Feature validation and prioritization
- **QA Team**: Testing and quality assurance
- **DevOps Team**: Deployment and monitoring setup
- **Customer Success**: Beta testing and feedback collection

---

**Document Version**: 1.0  
**Last Updated**: 2024-10-26  
**Author**: AI Assistant  
**Status**: Draft - Awaiting Review

