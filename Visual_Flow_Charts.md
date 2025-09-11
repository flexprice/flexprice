# Visual Flow Charts: GetUsageBySubscription vs V2

## GetUsageBySubscription (V1) - Visual Flow Chart

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           GetUsageBySubscription (V1)                          │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 1. Initialize Services                                                          │
│    ├── NewEventService()                                                       │
│    └── NewPriceService()                                                       │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 2. Data Retrieval                                                               │
│    ├── s.SubRepo.GetWithLineItems()                                            │
│    └── s.CustomerRepo.Get()                                                     │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 3. Time Range Processing                                                        │
│    ├── Calculate usageStartTime                                                 │
│    ├── Calculate usageEndTime                                                   │
│    └── Handle lifetime usage logic                                              │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 4. Price Collection                                                             │
│    ├── Collect priceIDs from lineItems                                         │
│    └── priceService.GetPrices() [Single call]                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 5. Data Mapping                                                                 │
│    ├── Build priceMap                                                          │
│    ├── Build meterMap                                                          │
│    └── Build meterDisplayNames                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 6. Performance Optimization                                                     │
│    └── s.EventRepo.GetDistinctEventNames() [Single query]                      │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 7. Meter Request Building                                                       │
│    ├── Create eventNameExists map                                              │
│    ├── For each lineItem:                                                      │
│    │   ├── Check if usage type                                                 │
│    │   ├── Check if meter exists                                               │
│    │   ├── Check event name optimization                                       │
│    │   ├── Create GetUsageByMeterRequest                                       │
│    │   └── Add filters from meter                                              │
│    └── Build meterUsageRequests array                                          │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 8. PARALLEL USAGE QUERIES                                                       │
│    └── eventService.BulkGetUsageByMeter() [MULTIPLE PARALLEL QUERIES]          │
│        │                                                                        │
│        ├── Goroutine 1: GetUsageByMeter() ──┐                                  │
│        ├── Goroutine 2: GetUsageByMeter() ──┤                                  │
│        ├── Goroutine 3: GetUsageByMeter() ──┤                                  │
│        ├── Goroutine 4: GetUsageByMeter() ──┤                                  │
│        ├── Goroutine 5: GetUsageByMeter() ──┤                                  │
│        └── ... (up to 5 concurrent) ────────┤                                  │
│                                              │                                  │
│        Each Goroutine:                       │                                  │
│        ├── s.GetUsageByMeter()               │                                  │
│        │   ├── s.meterRepo.GetMeter()        │                                  │
│        │   ├── Build GetUsageRequest         │                                  │
│        │   └── s.GetUsage()                  │                                  │
│        │       ├── GetAggregator()           │                                  │
│        │       ├── Build query               │                                  │
│        │       └── r.store.GetConn().Query() │                                  │
│        └── Return AggregationResult          │                                  │
│                                              │                                  │
│        └── Aggregate all results ────────────┘                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 9. Cost Calculation & Processing                                                │
│    ├── For each meterUsageRequest:                                             │
│    │   ├── Get usage from usageMap                                             │
│    │   ├── Get price from priceMap                                             │
│    │   ├── Handle bucketed max logic                                           │
│    │   ├── priceService.CalculateCost()                                       │
│    │   └── createChargeResponse()                                              │
│    └── Calculate totalCost                                                     │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 10. Commitment Logic                                                            │
│     ├── Check hasCommitment                                                     │
│     ├── Calculate commitmentAmount                                              │
│     ├── Calculate overageFactor                                                 │
│     ├── Apply commitment logic to charges                                       │
│     └── Update response fields                                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 11. Response Building                                                           │
│     ├── Set response.Amount                                                    │
│     ├── Set response.Currency                                                  │
│     ├── Set response.DisplayAmount                                             │
│     ├── Set response.StartTime/EndTime                                         │
│     ├── Set response.Charges                                                   │
│     ├── Set commitment fields                                                  │
│     └── Return response                                                        │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## GetUsageBySubscriptionV2 - Visual Flow Chart

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                        GetUsageBySubscriptionV2                                │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 1. Initialize Services                                                          │
│    └── NewPriceService() [No EventService needed]                              │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 2. Data Retrieval                                                               │
│    ├── s.SubRepo.GetWithLineItems()                                            │
│    └── s.CustomerRepo.Get()                                                     │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 3. Time Range Processing                                                        │
│    ├── Calculate usageStartTime                                                 │
│    ├── Calculate usageEndTime                                                   │
│    └── Handle lifetime usage logic                                              │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 4. Price Collection & Mapping                                                   │
│    ├── Collect priceIDs from lineItems                                         │
│    ├── Build meterToPriceMap                                                   │
│    ├── priceService.GetPrices() [Single call]                                  │
│    ├── Build priceMap                                                          │
│    ├── Build meterMap                                                          │
│    └── Build meterDisplayNames                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 5. Feature Mapping                                                              │
│    ├── NewFeatureService()                                                     │
│    ├── Create featureFilter                                                    │
│    ├── featureService.GetFeatures() [Single call]                              │
│    └── Build featureToMeterMap                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 6. SINGLE OPTIMIZED QUERY                                                       │
│    └── s.ProcessedEventRepo.GetUsageBySubscriptionV2()                         │
│        │                                                                        │
│        ├── Build optimized SQL query                                           │
│        ├── r.store.GetConn().Query() [SINGLE CLICKHOUSE QUERY]                 │
│        ├── Process results into UsageByFeatureResult                           │
│        └── Return map[string]*UsageByFeatureResult                             │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 7. Cost Calculation & Processing                                                │
│    ├── For each feature result:                                                │
│    │   ├── Get meterID from featureToMeterMap                                  │
│    │   ├── Get priceID from meterToPriceMap                                    │
│    │   ├── Get price from priceMap                                             │
│    │   ├── Get meter from meterMap                                             │
│    │   ├── Calculate quantity based on aggregation type:                      │
│    │   │   ├── SUM → usageResult.SumTotal                                      │
│    │   │   ├── MAX → usageResult.MaxTotal                                     │
│    │   │   ├── COUNT → usageResult.CountDistinctIDs                           │
│    │   │   ├── COUNT_UNIQUE → usageResult.CountUniqueQty                      │
│    │   │   └── LATEST → usageResult.LatestQty                                 │
│    │   ├── Apply multiplier if needed                                          │
│    │   ├── priceService.CalculateCost()                                        │
│    │   └── Create SubscriptionUsageByMetersResponse                            │
│    └── Calculate totalCost                                                     │
└─────────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│ 8. Response Building                                                           │
│    ├── Sort charges by meter display name                                      │
│    ├── Set response.Amount                                                     │
│    ├── Set response.Currency                                                   │
│    ├── Set response.DisplayAmount                                              │
│    ├── Set response.StartTime/EndTime                                          │
│    ├── Set response.Charges                                                    │
│    └── Return response                                                         │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Side-by-Side Comparison

```
V1 (Original)                           │ V2 (Optimized)
────────────────────────────────────────┼────────────────────────────────────────
┌─ Initialize Services ─┐                │ ┌─ Initialize Services ─┐
│ • NewEventService()   │                │ │ • NewPriceService()   │
│ • NewPriceService()   │                │ └───────────────────────┘
└───────────────────────┘                │
┌─ Data Retrieval ──────┐                │ ┌─ Data Retrieval ──────┐
│ • GetWithLineItems()  │                │ │ • GetWithLineItems()  │
│ • CustomerRepo.Get()  │                │ │ • CustomerRepo.Get()  │
└───────────────────────┘                │ └───────────────────────┘
┌─ Time Processing ─────┐                │ ┌─ Time Processing ─────┐
│ • Calculate times     │                │ │ • Calculate times     │
└───────────────────────┘                │ └───────────────────────┘
┌─ Price Collection ────┐                │ ┌─ Price Collection ────┐
│ • Collect priceIDs    │                │ │ • Collect priceIDs    │
│ • GetPrices()         │                │ │ • Build mappings      │
└───────────────────────┘                │ │ • GetPrices()         │
┌─ Data Mapping ────────┐                │ └───────────────────────┘
│ • Build priceMap      │                │ ┌─ Feature Mapping ─────┐
│ • Build meterMap      │                │ │ • NewFeatureService() │
│ • Build displayNames  │                │ │ • GetFeatures()       │
└───────────────────────┘                │ │ • Build featureMap    │
┌─ Optimization ────────┐                │ └───────────────────────┘
│ • GetDistinctEvents() │                │
└───────────────────────┘                │
┌─ Request Building ────┐                │
│ • Build meterRequests │                │
└───────────────────────┘                │
┌─ PARALLEL QUERIES ────┐                │ ┌─ SINGLE QUERY ────────┐
│ • BulkGetUsageByMeter │                │ │ • GetUsageBySubV2()   │
│ • Multiple goroutines │                │ │ • Single ClickHouse   │
│ • Concurrent execution│                │ │ • Optimized SQL       │
└───────────────────────┘                │ └───────────────────────┘
┌─ Cost Calculation ────┐                │ ┌─ Cost Calculation ────┐
│ • Process each meter  │                │ │ • Process each feature│
│ • Handle bucketed max │                │ │ • Simple aggregation  │
│ • Calculate costs     │                │ │ • Calculate costs     │
└───────────────────────┘                │ └───────────────────────┘
┌─ Commitment Logic ────┐                │
│ • Check commitment    │                │
│ • Calculate overage   │                │
│ • Apply logic         │                │
└───────────────────────┘                │
┌─ Response Building ───┐                │ ┌─ Response Building ───┐
│ • Set all fields      │                │ │ • Set basic fields    │
│ • Include commitment  │                │ │ • No commitment       │
└───────────────────────┘                │ └───────────────────────┘
```

## Key Visual Differences

### 1. Query Execution
- **V1**: Multiple parallel branches (goroutines) executing simultaneously
- **V2**: Single linear execution path

### 2. Database Load
- **V1**: Multiple concurrent ClickHouse connections
- **V2**: Single ClickHouse connection

### 3. Complexity
- **V1**: Complex branching with parallel processing
- **V2**: Simple linear flow

### 4. Error Handling
- **V1**: Multiple error points (each goroutine)
- **V2**: Single error point (main execution)

### 5. Memory Usage
- **V1**: High (multiple concurrent operations)
- **V2**: Low (sequential operations)

The visual flow charts clearly show that V2 eliminates the complex parallel processing of V1, resulting in a much simpler and more efficient execution path.
