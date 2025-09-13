# GetUsageBySubscription vs GetUsageBySubscriptionV2 Comparison

## Overview

This document provides a detailed comparison between the original `GetUsageBySubscription` function and the new optimized `GetUsageBySubscriptionV2` function.

## Key Differences

### 1. Database Query Strategy

| Aspect | Original (V1) | Optimized (V2) |
|--------|---------------|----------------|
| **Query Count** | Multiple parallel queries (1 per meter) | Single optimized query |
| **Query Type** | Individual meter queries via `BulkGetUsageByMeter` | Direct `events_processed` table query |
| **Database Load** | High (concurrent queries) | Low (single query) |
| **Risk of Overload** | High (can overwhelm ClickHouse) | Low (controlled load) |

### 2. Query Implementation

#### Original V1 Query Pattern
```go
// Multiple queries executed in parallel via BulkGetUsageByMeter
for each meter {
    query = "SELECT aggregation FROM events WHERE event_name = ? AND ..."
    // Executed concurrently with goroutines
}
```

#### V2 Single Query
```sql
SELECT 
    feature_id,
    sum(qty_total)                     AS sum_total,
    max(qty_total)                     AS max_total,
    count(DISTINCT id)                 AS count_distinct_ids,
    count(DISTINCT unique_hash)        AS count_unique_qty,
    argMax(qty_total, "timestamp")     AS latest_qty
FROM events_processed
WHERE 
    subscription_id = ?
    AND external_customer_id = ?
    AND environment_id = ?
    AND tenant_id = ?
    AND "timestamp" >= ?
    AND "timestamp" < ?
GROUP BY feature_id
```

### 3. Performance Optimizations

| Feature | V1 | V2 |
|---------|----|----|
| **Event Name Filtering** | ✅ Pre-filters meters with no events | ❌ Not needed (single query) |
| **Parallel Processing** | ✅ Uses goroutines for concurrent queries | ❌ Single query execution |
| **Database Connections** | High (multiple concurrent) | Low (single connection) |
| **Memory Usage** | Higher (multiple result sets) | Lower (single result set) |
| **Query Planning** | Multiple query plans | Single optimized plan |

### 4. Data Processing Flow

#### V1 Flow
```
1. Get subscription + line items
2. Get customer
3. Collect price IDs
4. Fetch prices with meters
5. Get distinct event names (optimization)
6. Build meter usage requests
7. Execute BulkGetUsageByMeter (parallel queries)
8. Process results from multiple queries
9. Calculate costs
10. Apply commitment logic
11. Return response
```

#### V2 Flow
```
1. Get subscription + line items
2. Get customer
3. Collect price IDs + meter mappings
4. Fetch prices with meters
5. Get features for meters (feature mapping)
6. Execute single optimized query
7. Process single result set
8. Calculate costs
9. Return response
```

### 5. Code Complexity

| Aspect | V1 | V2 |
|--------|----|----|
| **Lines of Code** | ~300+ lines | ~200 lines |
| **Complexity** | High (parallel processing, error handling) | Medium (straightforward processing) |
| **Error Handling** | Complex (multiple query failures) | Simple (single query failure) |
| **Maintenance** | Difficult (concurrent logic) | Easier (linear processing) |

### 6. Error Handling

#### V1 Error Scenarios
- Individual meter query failures
- Partial failures in parallel execution
- Timeout handling for concurrent queries
- Complex error aggregation

#### V2 Error Scenarios
- Single query failure
- Simple error propagation
- No partial failure scenarios

### 7. Memory Usage

| Component | V1 | V2 |
|-----------|----|----|
| **Query Results** | Multiple result sets in memory | Single result set |
| **Goroutines** | 5+ concurrent goroutines | No goroutines |
| **Channel Buffers** | Multiple channel operations | No channels |
| **Result Aggregation** | Complex map merging | Simple map processing |

### 8. Response Format Compatibility

| Field | V1 | V2 | Notes |
|-------|----|----|-------|
| `Amount` | ✅ | ✅ | Same calculation |
| `Currency` | ✅ | ✅ | Same source |
| `DisplayAmount` | ✅ | ✅ | Same formatting |
| `StartTime` | ✅ | ✅ | Same logic |
| `EndTime` | ✅ | ✅ | Same logic |
| `Charges` | ✅ | ✅ | Same structure |
| `CommitmentAmount` | ✅ | ❌ | Not implemented in V2 |
| `OverageFactor` | ✅ | ❌ | Not implemented in V2 |
| `CommitmentUtilized` | ✅ | ❌ | Not implemented in V2 |
| `OverageAmount` | ✅ | ❌ | Not implemented in V2 |
| `HasOverage` | ✅ | ❌ | Not implemented in V2 |

### 9. Aggregation Support

| Aggregation Type | V1 | V2 | Implementation |
|------------------|----|----|----------------|
| `SUM` | ✅ | ✅ | `sum(qty_total)` |
| `SUM_WITH_MULTIPLIER` | ✅ | ✅ | `sum(qty_total) * multiplier` |
| `MAX` | ✅ | ✅ | `max(qty_total)` |
| `COUNT` | ✅ | ✅ | `count(DISTINCT id)` |
| `COUNT_UNIQUE` | ✅ | ✅ | `count(DISTINCT unique_hash)` |
| `LATEST` | ✅ | ✅ | `argMax(qty_total, timestamp)` |
| `AVG` | ✅ | ❌ | Not implemented in V2 |

### 10. Missing Features in V2

The V2 implementation currently lacks these V1 features:
- **Commitment Logic**: No commitment amount handling
- **Overage Logic**: No overage factor calculations
- **Bucketed Max Support**: No bucketed max meter handling
- **Advanced Filtering**: No meter-level filtering support

### 11. Performance Characteristics

#### V1 Performance
- **Latency**: Variable (depends on slowest query)
- **Throughput**: Limited by ClickHouse concurrent query limits
- **Resource Usage**: High (multiple connections, goroutines)
- **Scalability**: Poor (can overwhelm database)

#### V2 Performance
- **Latency**: Consistent (single query execution)
- **Throughput**: High (single optimized query)
- **Resource Usage**: Low (single connection, no goroutines)
- **Scalability**: Excellent (controlled database load)

### 12. Use Case Recommendations

#### Use V1 When:
- You need commitment/overage logic
- You have bucketed max meters
- You need advanced meter filtering
- You have complex meter configurations

#### Use V2 When:
- You want maximum performance
- You have many meters (100+)
- ClickHouse is under load
- You need consistent response times
- You don't need commitment/overage features

### 13. Migration Considerations

#### Breaking Changes
- Missing commitment/overage fields in response
- Different error handling behavior
- No bucketed max support

#### Non-Breaking Changes
- Same core response structure
- Same calculation logic for basic usage
- Same input parameters

### 14. Future Enhancements for V2

To make V2 feature-complete, consider adding:
1. Commitment amount logic
2. Overage factor calculations
3. Bucketed max meter support
4. Advanced filtering capabilities
5. AVG aggregation support

## Conclusion

The V2 implementation provides significant performance improvements through a single optimized query but sacrifices some advanced features. It's ideal for high-performance scenarios where basic usage calculation is sufficient, while V1 remains better for complex billing scenarios requiring commitment and overage logic.
