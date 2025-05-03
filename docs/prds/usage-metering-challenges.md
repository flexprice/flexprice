# Flexprice Usage Metering System: Architecture, Flow and Challenges

## System Overview

Flexprice is built to provide a scalable usage-based billing infrastructure. The core functionality revolves around:

1. Ingesting usage events from client systems
2. Processing and aggregating those events according to defined meters
3. Computing costs based on pricing configurations
4. Maintaining real-time customer wallet balances
5. Supporting high-throughput event processing with millisecond-level response times

## System Architecture & Data Flow

### 1. Meter Definition

**Component:** `internal/domain/meter/model.go`

Meters define:
- Which events to track (`EventName`)
- How to filter events (`Filters`)
- How to aggregate data (`Aggregation`)
- Whether to reset usage periodically (`ResetUsage`)

```go
type Meter struct {
    ID          string
    EventName   string
    Name        string
    Aggregation Aggregation
    Filters     []Filter
    ResetUsage  types.ResetUsage
    ...
}
```

Supported aggregation types (`internal/types/aggregation.go`):
- COUNT: Simple count of events
- SUM: Summing a numeric property value
- AVG: Averaging a numeric property value 
- COUNT_UNIQUE: Count distinct values of a property

### 2. Event Ingestion

**Flow:**
1. Clients send events to Flexprice API
2. Events are validated and published to Kafka
3. A consumer (`handleEventConsumption` in `cmd/server/main.go`) processes events from Kafka
4. Events are persisted to Clickhouse (`events` table)

**Event Structure** (`internal/domain/events/model.go`):
```go
type Event struct {
    ID                 string
    TenantID           string
    EventName          string
    Properties         map[string]interface{}
    Source             string
    Timestamp          time.Time
    IngestedAt         time.Time
    CustomerID         string
    ExternalCustomerID string
}
```

**Clickhouse Schema** (`migrations/clickhouse/000001_create_events_table.up.sql`):
```sql
CREATE TABLE IF NOT EXISTS events (
    id String,
    tenant_id String,
    external_customer_id String,
    customer_id String,
    event_name String,
    source String,
    timestamp DateTime64(3),
    ingested_at DateTime64(3) DEFAULT now(),
    properties String,
    ...
) ENGINE = ReplacingMergeTree(timestamp)
...
```

### 3. Usage Calculation

**Component:** `internal/service/event.go`

The `EventService` provides methods to:
- Create events
- Query usage based on meters
- Generate usage reports

Key methods:
- `GetUsage`: Query raw usage based on parameters
- `GetUsageByMeter`: Get usage based on meter configuration
- `GetUsageByMeterWithFilters`: Get usage with complex filter criteria

For usage calculation:
1. System retrieves meter configuration
2. Constructs ClickHouse query based on meter settings and filters
3. Executes query to calculate aggregations
4. Returns usage data for billing calculations

### 4. Charge Computation

**Components:**
- `internal/domain/plan/model.go`: Defines subscription plans
- `internal/domain/price/model.go`: Defines pricing structures

The system supports different pricing models:
- Fixed pricing
- Tiered pricing
- Volume-based pricing
- Package pricing

Usage is transformed into billable amounts based on:
1. Meter readings (usage quantity)
2. Price configuration (unit price, tiers, etc.)
3. Plan specifications

### 5. Wallet Balance Tracking

**Component:** `internal/service/wallet.go`

The wallet system:
- Maintains customer balances
- Tracks credits and debits
- Calculates real-time balances considering:
  - Current wallet balance
  - Unpaid invoices
  - Current period usage (not yet invoiced)

Key method:
- `GetWalletBalance`: Calculates real-time balance by:
  1. Retrieving wallet information
  2. Fetching unpaid invoice amounts
  3. Calculating current period usage
  4. Subtracting unpaid invoices and usage from balance

## Challenges & Shortcomings

### 1. Scalability Challenges

- **Event Ingestion Bottlenecks**:
  - High volume of incoming events can overwhelm Kafka
  - Serialization/deserialization overhead

- **Storage Growth**:
  - ClickHouse table size grows continuously
  - No apparent partitioning or TTL strategy

- **Query Performance**:
  - As data grows, queries may slow down
  - Complex aggregations on large datasets

### 2. Real-Time Processing Challenges

- **Wallet Balance Calculation**:
  - Requires calculating usage in real-time
  - Multiple database calls for each balance check
  - No apparent caching strategy

- **Latency in Balance Updates**:
  - Time between event ingestion and balance reflection

### 3. Query Efficiency

- **Complex ClickHouse Queries**:
  - `query_builder.go` constructs complex SQL
  - Multi-level CTEs and nested queries
  - May have performance bottlenecks at scale

- **Resource Intensive Aggregations**:
  - Some aggregations (e.g., COUNT_UNIQUE) can be resource-intensive
  - No apparent sampling or approximation for large datasets

### 4. Architectural Concerns

- **Tight Coupling**:
  - Wallet balance directly dependent on usage calculations
  - Changes in one component can affect others

- **No Apparent Pre-Aggregation**:
  - Every query computes from raw events
  - No materialized views or pre-computed aggregates

- **Error Handling**:
  - Limited information on handling failed event processing
  - Recovery mechanisms not clearly defined

### 5. Operational Challenges

- **Monitoring & Observability**:
  - No clear visibility into system bottlenecks
  - Difficult to detect and diagnose performance issues

- **Resilience**:
  - Single point of failures in critical paths
  - Limited information on retry mechanisms

## Recommendations for Improvement

1. **Event Streaming Optimizations**:
   - Optimize Kafka configuration for high throughput
   - Consider batch processing for improved efficiency

2. **Data Management Strategies**:
   - Implement data retention policies
   - Add time-based partitioning in ClickHouse
   - Consider downsampling historical data

3. **Performance Enhancements**:
   - Implement materialized views for common aggregations
   - Add caching layer for frequently accessed data
   - Optimize query patterns

4. **Architectural Improvements**:
   - Decouple wallet balance from real-time usage
   - Implement event-driven architecture for balance updates
   - Consider CQRS pattern to separate read/write operations

5. **Operational Enhancements**:
   - Add comprehensive monitoring and alerting
   - Implement circuit breakers for critical components
   - Create detailed performance dashboards 