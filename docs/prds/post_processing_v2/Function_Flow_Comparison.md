# Function-Level Flow Comparison: GetUsageBySubscription vs V2

## GetUsageBySubscription (Original V1) - Function Call Flow

```
GetUsageBySubscription()
├── 1. Initialize Services
│   ├── NewEventService()
│   └── NewPriceService()
│
├── 2. Data Retrieval
│   ├── s.SubRepo.GetWithLineItems()
│   └── s.CustomerRepo.Get()
│
├── 3. Time Range Processing
│   ├── Calculate usageStartTime
│   ├── Calculate usageEndTime
│   └── Handle lifetime usage logic
│
├── 4. Price Collection
│   ├── Collect priceIDs from lineItems
│   └── priceService.GetPrices() [Single call with all price IDs]
│
├── 5. Data Mapping
│   ├── Build priceMap
│   ├── Build meterMap
│   └── Build meterDisplayNames
│
├── 6. Performance Optimization
│   └── s.EventRepo.GetDistinctEventNames() [Single query]
│
├── 7. Meter Request Building
│   ├── Create eventNameExists map
│   ├── For each lineItem:
│   │   ├── Check if usage type
│   │   ├── Check if meter exists
│   │   ├── Check event name optimization
│   │   ├── Create GetUsageByMeterRequest
│   │   └── Add filters from meter
│   └── Build meterUsageRequests array
│
├── 8. Parallel Usage Queries
│   └── eventService.BulkGetUsageByMeter() [MULTIPLE PARALLEL QUERIES]
│       ├── For each meter (in parallel):
│       │   ├── s.GetUsageByMeter()
│       │   │   ├── s.meterRepo.GetMeter() [if needed]
│       │   │   ├── Build GetUsageRequest
│       │   │   ├── s.GetUsage() [INDIVIDUAL CLICKHOUSE QUERY]
│       │   │   │   ├── GetAggregator()
│       │   │   │   ├── Build query with QueryBuilder
│       │   │   │   ├── r.store.GetConn().Query() [CLICKHOUSE QUERY]
│       │   │   │   └── Process results
│       │   │   └── Handle historic usage (if needed)
│       │   └── Return AggregationResult
│       └── Aggregate all results into usageMap
│
├── 9. Cost Calculation & Processing
│   ├── For each meterUsageRequest:
│   │   ├── Get usage from usageMap
│   │   ├── Get price from priceMap
│   │   ├── Handle bucketed max logic
│   │   ├── priceService.CalculateCost() or CalculateBucketedCost()
│   │   ├── createChargeResponse()
│   │   └── Add to usageCharges
│   └── Calculate totalCost
│
├── 10. Commitment Logic
│   ├── Check hasCommitment
│   ├── Calculate commitmentAmount
│   ├── Calculate overageFactor
│   ├── Apply commitment logic to charges
│   └── Update response fields
│
└── 11. Response Building
    ├── Set response.Amount
    ├── Set response.Currency
    ├── Set response.DisplayAmount
    ├── Set response.StartTime/EndTime
    ├── Set response.Charges
    ├── Set commitment fields
    └── Return response
```

## GetUsageBySubscriptionV2 - Function Call Flow

```
GetUsageBySubscriptionV2()
├── 1. Initialize Services
│   └── NewPriceService() [No EventService needed]
│
├── 2. Data Retrieval
│   ├── s.SubRepo.GetWithLineItems()
│   └── s.CustomerRepo.Get()
│
├── 3. Time Range Processing
│   ├── Calculate usageStartTime
│   ├── Calculate usageEndTime
│   └── Handle lifetime usage logic
│
├── 4. Price Collection & Mapping
│   ├── Collect priceIDs from lineItems
│   ├── Build meterToPriceMap
│   ├── priceService.GetPrices() [Single call with all price IDs]
│   ├── Build priceMap
│   ├── Build meterMap
│   └── Build meterDisplayNames
│
├── 5. Feature Mapping
│   ├── NewFeatureService()
│   ├── Create featureFilter
│   ├── featureService.GetFeatures() [Single call for all meters]
│   └── Build featureToMeterMap
│
├── 6. Single Optimized Query
│   └── s.ProcessedEventRepo.GetUsageBySubscriptionV2() [SINGLE CLICKHOUSE QUERY]
│       ├── Build optimized SQL query
│       ├── r.store.GetConn().Query() [CLICKHOUSE QUERY]
│       ├── Process results into UsageByFeatureResult
│       └── Return map[string]*UsageByFeatureResult
│
├── 7. Cost Calculation & Processing
│   ├── For each feature result:
│   │   ├── Get meterID from featureToMeterMap
│   │   ├── Get priceID from meterToPriceMap
│   │   ├── Get price from priceMap
│   │   ├── Get meter from meterMap
│   │   ├── Calculate quantity based on aggregation type:
│   │   │   ├── SUM/SUM_WITH_MULTIPLIER → usageResult.SumTotal
│   │   │   ├── MAX → usageResult.MaxTotal
│   │   │   ├── COUNT → usageResult.CountDistinctIDs
│   │   │   ├── COUNT_UNIQUE → usageResult.CountUniqueQty
│   │   │   └── LATEST → usageResult.LatestQty
│   │   ├── Apply multiplier if needed
│   │   ├── priceService.CalculateCost()
│   │   ├── Create SubscriptionUsageByMetersResponse
│   │   └── Add to usageCharges
│   └── Calculate totalCost
│
├── 8. Response Building
│   ├── Sort charges by meter display name
│   ├── Set response.Amount
│   ├── Set response.Currency
│   ├── Set response.DisplayAmount
│   ├── Set response.StartTime/EndTime
│   ├── Set response.Charges
│   └── Return response
```

## Detailed Function Call Comparison

### Database Queries

| Function | V1 Queries | V2 Queries |
|----------|------------|------------|
| **Subscription Data** | `s.SubRepo.GetWithLineItems()` | `s.SubRepo.GetWithLineItems()` |
| **Customer Data** | `s.CustomerRepo.Get()` | `s.CustomerRepo.Get()` |
| **Price Data** | `priceService.GetPrices()` | `priceService.GetPrices()` |
| **Event Optimization** | `s.EventRepo.GetDistinctEventNames()` | ❌ Not needed |
| **Feature Data** | ❌ Not used | `featureService.GetFeatures()` |
| **Usage Data** | `eventService.BulkGetUsageByMeter()` | `s.ProcessedEventRepo.GetUsageBySubscriptionV2()` |
| **Individual Meter Queries** | `s.GetUsageByMeter()` × N meters | ❌ Not needed |
| **ClickHouse Queries** | `r.store.GetConn().Query()` × N meters | `r.store.GetConn().Query()` × 1 |

### Service Initialization

| Service | V1 | V2 |
|---------|----|----|
| **EventService** | ✅ `NewEventService()` | ❌ Not needed |
| **PriceService** | ✅ `NewPriceService()` | ✅ `NewPriceService()` |
| **FeatureService** | ❌ Not used | ✅ `NewFeatureService()` |

### Data Processing Functions

| Function | V1 | V2 |
|----------|----|----|
| **Meter Request Building** | ✅ Complex loop with optimizations | ❌ Not needed |
| **Parallel Query Execution** | ✅ `BulkGetUsageByMeter()` | ❌ Not needed |
| **Individual Meter Processing** | ✅ `GetUsageByMeter()` × N | ❌ Not needed |
| **Feature Mapping** | ❌ Not used | ✅ `GetFeatures()` |
| **Single Query Processing** | ❌ Not used | ✅ `GetUsageBySubscriptionV2()` |

### Cost Calculation Functions

| Function | V1 | V2 |
|----------|----|----|
| **Basic Cost Calculation** | ✅ `priceService.CalculateCost()` | ✅ `priceService.CalculateCost()` |
| **Bucketed Cost Calculation** | ✅ `priceService.CalculateBucketedCost()` | ❌ Not implemented |
| **Charge Response Creation** | ✅ `createChargeResponse()` | ✅ Inline creation |
| **Commitment Logic** | ✅ Complex commitment processing | ❌ Not implemented |
| **Overage Logic** | ✅ Overage factor calculations | ❌ Not implemented |

### Response Building Functions

| Function | V1 | V2 |
|----------|----|----|
| **Charge Sorting** | ✅ By meter display name | ✅ By meter display name |
| **Response Field Setting** | ✅ All fields including commitment | ✅ Basic fields only |
| **Commitment Field Setting** | ✅ Commitment/Overage fields | ❌ Not implemented |

## Key Differences in Function Calls

### 1. Query Strategy
- **V1**: Multiple parallel ClickHouse queries via `BulkGetUsageByMeter()`
- **V2**: Single optimized ClickHouse query via `GetUsageBySubscriptionV2()`

### 2. Service Dependencies
- **V1**: Requires `EventService` for parallel processing
- **V2**: Requires `FeatureService` for feature mapping

### 3. Data Processing
- **V1**: Complex parallel processing with goroutines
- **V2**: Simple sequential processing

### 4. Error Handling
- **V1**: Complex error handling for multiple concurrent operations
- **V2**: Simple error handling for single operations

### 5. Memory Usage
- **V1**: High (multiple result sets, goroutines, channels)
- **V2**: Low (single result set, no concurrency)

## Performance Impact

### V1 Performance Characteristics
- **Concurrent Operations**: 5+ goroutines
- **Database Connections**: Multiple concurrent
- **Query Count**: 1 + N meters
- **Memory Usage**: High (parallel processing)
- **Error Complexity**: High (partial failures)

### V2 Performance Characteristics
- **Concurrent Operations**: 0 goroutines
- **Database Connections**: Single
- **Query Count**: 3 (subscription, customer, usage)
- **Memory Usage**: Low (sequential processing)
- **Error Complexity**: Low (single point of failure)

This function-level comparison shows that V2 significantly simplifies the execution flow while maintaining the same core functionality for basic usage scenarios.
