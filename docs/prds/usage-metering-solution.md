# Flexprice Usage Metering System: Proposed Solutions

## Executive Summary

Based on our analysis of the current Flexprice usage metering system, we've identified several areas for improvement to achieve millisecond-level responsiveness while handling millions of events. This document outlines a comprehensive solution approach with immediate and long-term improvements.

## Solution Areas

### 1. Optimized Event Ingestion & Processing

#### 1.1 Batch Processing Optimization

**Problem:** Individual event processing creates high CPU/IO overhead.

**Solution:**
- Implement efficient batch ingestion at all layers:
  ```go
  func (s *eventService) BulkCreateEvents(ctx context.Context, events *dto.BulkIngestEventRequest) error {
      // Process events in optimized batches of configurable size
      batchSize := 1000
      totalEvents := len(events.Events)
      
      for i := 0; i < totalEvents; i += batchSize {
          end := min(i+batchSize, totalEvents)
          batch := events.Events[i:end]
          
          // Process batch with single Kafka transaction
          if err := s.publishEventBatch(ctx, batch); err != nil {
              return err
          }
      }
      return nil
  }
  ```

#### 1.2 Kafka Optimization

**Problem:** Default Kafka settings may not be optimized for high throughput.

**Solution:**
- Increase partition count for event topics
- Optimize producer/consumer configurations:
  ```go
  // Producer config
  config := sarama.NewConfig()
  config.Producer.Compression = sarama.CompressionSnappy
  config.Producer.Flush.Frequency = 100 * time.Millisecond
  config.Producer.Flush.MaxMessages = 1000
  config.Producer.Return.Successes = true
  
  // Consumer config
  config.Consumer.Fetch.Min = 1 * 1024 * 1024  // 1MB minimum fetch
  config.Consumer.Fetch.Default = 10 * 1024 * 1024  // 10MB default fetch
  config.Consumer.MaxProcessingTime = 500 * time.Millisecond
  ```

### 2. ClickHouse Optimization

#### 2.1 Table Optimization

**Problem:** Events table grows indefinitely without proper partitioning.

**Solution:**
- Implement time-based partitioning and TTL:
  ```sql
  ALTER TABLE events
  MODIFY TTL timestamp + INTERVAL 90 DAY DELETE;
  ```

- Optimize table schema with proper sorting keys:
  ```sql
  ALTER TABLE events
  MODIFY ORDER BY (tenant_id, event_name, external_customer_id, timestamp);
  ```

#### 2.2 Query Optimization

**Problem:** Complex SQL with nested CTEs causes performance issues.

**Solution:**
- Refactor query builder to generate more efficient SQL:
  ```go
  func (qb *QueryBuilder) WithAggregation(ctx context.Context, aggType types.AggregationType, propertyName string) *QueryBuilder {
      // Simplified direct aggregation without excessive CTEs
      var aggClause string
      switch aggType {
      case types.AggregationCount:
          aggClause = "COUNT(*)"
      case types.AggregationSum:
          aggClause = fmt.Sprintf("SUM(JSONExtractString(properties, '%s'))", propertyName)
      // Other aggregation types...
      }
      
      // More efficient query structure
      qb.finalQuery = fmt.Sprintf(`
          SELECT 
              %s as value
          FROM events
          WHERE %s
          GROUP BY tenant_id, event_name
      `, aggClause, qb.baseConditions)
      
      return qb
  }
  ```

#### 2.3 Materialized Views

**Problem:** Repeated calculations on raw data for common aggregations.

**Solution:**
- Implement materialized views for common aggregation patterns:
  ```sql
  CREATE MATERIALIZED VIEW event_daily_counts
  ENGINE = SummingMergeTree()
  PARTITION BY toYYYYMM(day)
  ORDER BY (tenant_id, event_name, external_customer_id, day)
  AS SELECT
      tenant_id,
      event_name,
      external_customer_id,
      toDate(timestamp) as day,
      count() as event_count
  FROM events
  GROUP BY tenant_id, event_name, external_customer_id, day;
  ```

### 3. Caching Strategy

#### 3.1 Multi-Level Caching

**Problem:** Repeated expensive calculations for wallet balances.

**Solution:**
- Implement Redis-based caching for:
  1. Meter configurations
  2. Frequently accessed usage stats
  3. Wallet balances

```go
// Example implementation
type CachedEventService struct {
    eventService EventService
    redisClient  *redis.Client
    cacheTTL     time.Duration
}

func (s *CachedEventService) GetUsageByMeter(ctx context.Context, req *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
    cacheKey := fmt.Sprintf("usage:%s:%s:%s", req.MeterID, req.ExternalCustomerID, req.StartTime.Format(time.RFC3339))
    
    // Try to get from cache
    if cachedData, err := s.redisClient.Get(ctx, cacheKey).Result(); err == nil {
        var result events.AggregationResult
        if err := json.Unmarshal([]byte(cachedData), &result); err == nil {
            return &result, nil
        }
    }
    
    // Cache miss - call the actual service
    result, err := s.eventService.GetUsageByMeter(ctx, req)
    if err != nil {
        return nil, err
    }
    
    // Cache the result
    if cached, err := json.Marshal(result); err == nil {
        s.redisClient.Set(ctx, cacheKey, cached, s.cacheTTL)
    }
    
    return result, nil
}
```

#### 3.2 Cache Invalidation Strategy

**Problem:** Ensuring cached data is consistent with actual usage.

**Solution:**
- Implement time-based expiration for usage caches
- Event-based invalidation for critical updates:
  ```go
  func (s *eventService) invalidateUsageCaches(ctx context.Context, meterID, customerID string) {
      pattern := fmt.Sprintf("usage:%s:%s:*", meterID, customerID)
      keys, _ := s.redisClient.Keys(ctx, pattern).Result()
      if len(keys) > 0 {
          s.redisClient.Del(ctx, keys...)
      }
  }
  ```

### 4. Real-Time Wallet Balance Optimization

#### 4.1 Estimated Real-Time Balance

**Problem:** Calculating exact usage for every balance request is expensive.

**Solution:**
- Implement estimation-based real-time balance:
  ```go
  func (s *walletService) GetEstimatedWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
      w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
      if err != nil {
          return nil, err
      }
      
      // Get current cached usage or last known value
      cachedUsage, _ := s.getCachedCurrentPeriodUsage(ctx, w.CustomerID, w.Currency)
      
      // Get last updated timestamp
      lastUpdated, _ := s.getCachedLastUpdateTime(ctx, walletID)
      
      // Use time-weighted projection for usage since last calculation
      timeSinceUpdate := time.Since(lastUpdated)
      projectedAdditionalUsage := s.projectUsageSinceLastUpdate(ctx, w.CustomerID, lastUpdated)
      
      // Calculate estimated balance
      estimatedBalance := w.Balance.
          Sub(cachedUsage).
          Sub(projectedAdditionalUsage)
      
      return &dto.WalletBalanceResponse{
          Wallet:                w,
          RealTimeBalance:       estimatedBalance,
          RealTimeCreditBalance: estimatedBalance.Div(w.ConversionRate),
          BalanceUpdatedAt:      time.Now().UTC(),
          EstimatedFlag:         true,
          LastFullCalculation:   lastUpdated,
      }, nil
  }
  ```

#### 4.2 Background Balance Calculation

**Problem:** Synchronous calculation slows down API responses.

**Solution:**
- Move expensive calculations to background jobs:
  ```go
  func (s *walletService) ScheduleWalletBalanceUpdate(customerID string) {
      // Queue job for background processing
      job := &WalletBalanceUpdateJob{
          CustomerID:  customerID,
          ScheduledAt: time.Now().UTC(),
      }
      s.backgroundJobQueue.Enqueue(job)
  }
  
  // Background worker
  func (w *WalletBalanceWorker) ProcessJobs() {
      for job := range w.jobQueue {
          // Calculate full wallet balance
          balances, err := w.calculateCustomerWalletBalances(context.Background(), job.CustomerID)
          if err != nil {
              w.logger.Errorw("failed to calculate wallet balance", "error", err)
              continue
          }
          
          // Update cache with precise values
          for _, balance := range balances {
              w.cacheBalanceResult(balance)
          }
      }
  }
  ```

### 5. Architectural Improvements

#### 5.1 Event-Driven Metering

**Problem:** Tight coupling between components.

**Solution:**
- Implement event-driven architecture for usage metering:
  ```go
  // Event definitions
  type EventProcessed struct {
      CustomerID string
      MeterID    string
      EventName  string
      Timestamp  time.Time
  }
  
  type UsageUpdated struct {
      CustomerID string
      MeterID    string
      NewValue   decimal.Decimal
      Timestamp  time.Time
  }
  
  type WalletBalanceChanged struct {
      WalletID        string
      NewBalance      decimal.Decimal
      ChangeAmount    decimal.Decimal
      ChangeReason    string
      Timestamp       time.Time
  }
  
  // Event handlers
  func (s *eventService) handleEventProcessed(ctx context.Context, event EventProcessed) {
      // Trigger usage recalculations
      s.eventBus.Publish(UsageUpdated{
          CustomerID: event.CustomerID,
          MeterID:    event.MeterID,
          // Other fields...
      })
  }
  
  func (s *walletService) handleUsageUpdated(ctx context.Context, event UsageUpdated) {
      // Update wallet balance cache
      // Schedule background balance recalculation if needed
  }
  ```

#### 5.2 CQRS Pattern Implementation

**Problem:** Read and write operations use the same models and pathways.

**Solution:**
- Separate read and write models:
  ```go
  // Write model - for processing events
  type EventCommand struct {
      ID                 string
      TenantID           string
      EventName          string
      Properties         map[string]interface{}
      // Other fields...
  }
  
  // Read model - optimized for queries
  type EventReadModel struct {
      ID                 string
      TenantID           string
      EventName          string
      
      // Pre-extracted common properties for faster queries
      PropertyValue1     string  // Common property extracted
      PropertyValue2     int     // Common property extracted
      
      // Rest of properties
      Properties         map[string]interface{}
      // Other fields...
  }
  ```

### 6. Implementation Roadmap

#### Phase 1: Immediate Optimizations (1-2 weeks)
- Implement basic caching for meter configs and wallet balances
- Optimize Kafka configuration for higher throughput
- Add ClickHouse query optimization for most common patterns

#### Phase 2: Core Performance Improvements (2-4 weeks)
- Implement materialized views in ClickHouse
- Develop background processing for wallet balance calculations
- Add TTL and partitioning for ClickHouse tables

#### Phase 3: Architectural Enhancements (4-8 weeks)
- Implement event-driven architecture
- Develop CQRS pattern for optimized reads
- Build comprehensive monitoring and alerting

## Performance Metrics & Targets

| Metric | Current | Target | Improvement |
|--------|---------|--------|-------------|
| Event Ingestion Rate | ~1K/sec | 100K/sec | 100x |
| Wallet Balance Calculation | ~500ms | <50ms | 10x |
| Usage Query Response Time | ~200ms | <50ms | 4x |
| Peak Sustainable Load | Unknown | 1M events/min | - |

## Conclusion

The proposed solutions address the key challenges in the Flexprice usage metering system while maintaining the existing functionality. By implementing these improvements in phases, we can achieve significant performance gains with minimal disruption to the current system. 