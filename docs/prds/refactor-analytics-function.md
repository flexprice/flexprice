# PRD: Refactor GetDetailedUsageAnalytics Function

## 1. Executive Summary

### Problem Statement
The `GetDetailedUsageAnalytics` function in `internal/service/feature_usage_tracking.go` has become severely unoptimized with:
- Excessive code complexity (125+ lines with 9+ steps)
- Multiple sequential database calls causing performance bottlenecks
- Deep nesting levels (3+ levels) making code hard to maintain
- Redundant data processing and loops
- Poor separation of concerns

### Solution Overview
Refactor the function to achieve:
- **50%+ code reduction** (target: ~60 lines main function)
- **3x performance improvement** through parallel data fetching
- **Maximum 2 nesting levels** for better readability
- **Zero redundancy** with single-pass data processing
- **Single responsibility** per helper function

## 2. Business Context

### Current Impact
- **Performance**: Sequential DB calls cause 200-500ms response times
- **Maintainability**: Complex nested logic makes bug fixes difficult
- **Scalability**: Poor performance under high load
- **Developer Experience**: Hard to understand and modify

### Success Metrics
- Response time reduction: 200-500ms → 50-150ms
- Code complexity reduction: 125 lines → 60 lines main function
- Nesting levels: 3+ → 2 maximum
- Database calls: 4+ sequential → 3 parallel
- Test coverage: Maintain 100% existing coverage

## 3. Technical Requirements

### 3.1 Functional Requirements

#### FR1: Maintain Existing API Contract
- **Requirement**: No changes to `GetUsageAnalyticsRequest` or `GetUsageAnalyticsResponse` DTOs
- **Acceptance Criteria**: All existing API consumers continue to work without changes
- **Priority**: P0 (Critical)

#### FR2: Preserve Business Logic
- **Requirement**: All existing business logic must be preserved exactly
- **Acceptance Criteria**: 
  - Currency validation logic unchanged
  - Cost calculation logic unchanged
  - Aggregation logic unchanged
  - Grouping logic unchanged
- **Priority**: P0 (Critical)

#### FR3: Improve Performance
- **Requirement**: Reduce response time by 60-70%
- **Acceptance Criteria**:
  - Parallel database calls instead of sequential
  - Single-pass data processing
  - Eliminate redundant loops
- **Priority**: P0 (Critical)

#### FR4: Reduce Code Complexity
- **Requirement**: Simplify code structure and improve maintainability
- **Acceptance Criteria**:
  - Maximum 2 nesting levels
  - Single responsibility per function
  - Clear separation of concerns
- **Priority**: P1 (High)

### 3.2 Non-Functional Requirements

#### NFR1: Performance
- **Target**: 50-150ms response time (vs current 200-500ms)
- **Method**: Parallel data fetching, optimized data structures
- **Measurement**: Load testing with 100 concurrent requests

#### NFR2: Maintainability
- **Target**: Maximum 2 nesting levels, single responsibility functions
- **Method**: Refactored architecture with clear separation
- **Measurement**: Code review and complexity analysis

#### NFR3: Reliability
- **Target**: Zero regression in functionality
- **Method**: Comprehensive testing and validation
- **Measurement**: 100% test coverage maintained

#### NFR4: Scalability
- **Target**: Handle 3x current load without performance degradation
- **Method**: Optimized database access patterns
- **Measurement**: Load testing with increased concurrent users

## 4. Technical Design

### 4.1 Current Architecture Issues

```
GetDetailedUsageAnalytics()
├── Step 1: Validate request
├── Step 2: Get customer (DB call 1)
├── Step 3: Get subscriptions (DB call 2)
├── Step 4: Validate currency
├── Step 5: Create params
├── Step 6: Convert subscriptions
├── Step 7: Get analytics (DB call 3)
├── Step 8: Enrich data (DB calls 4-6)
├── Step 9: Set currency
└── Step 10: Aggregate and return
```

**Problems:**
- Sequential execution (slow)
- Deep nesting (hard to read)
- Multiple data transformations
- Redundant processing

### 4.2 Proposed Architecture

```
GetDetailedUsageAnalytics()
├── validateAnalyticsRequest()     // Validation only
├── fetchAnalyticsData()           // Parallel data fetching
│   ├── fetchCustomer()            // Goroutine 1
│   ├── fetchSubscriptions()       // Goroutine 2
│   └── fetchAnalyticsData()       // Goroutine 3
└── buildAnalyticsResponse()       // Single-pass processing
    ├── enrichWithMetadata()       // Feature/meter/price data
    ├── calculateCosts()           // Cost calculations
    └── aggregateResults()         // Grouping logic
```

**Benefits:**
- Parallel execution (fast)
- Flat structure (readable)
- Single data structure
- No redundancy

### 4.3 Data Structures

#### New AnalyticsData Struct
```go
type AnalyticsData struct {
    Customer     *customer.Customer
    Subscriptions []*subscription.Subscription
    Analytics    []*events.DetailedUsageAnalytic
    Features     map[string]*feature.Feature
    Meters       map[string]*meter.Meter
    Prices       map[string]*price.Price
    Currency     string
    Params       *events.UsageAnalyticsParams
}
```

#### Parallel Fetching Result
```go
type FetchResult struct {
    Customer     chan *customer.Customer
    Subscriptions chan []*subscription.Subscription
    Analytics    chan []*events.DetailedUsageAnalytic
    Error        chan error
}
```

### 4.4 Implementation Plan

#### Phase 1: Core Refactoring (Week 1)
1. Create new data structures
2. Implement parallel fetching logic
3. Refactor main function structure
4. Unit tests for new functions

#### Phase 2: Optimization (Week 2)
1. Optimize data processing loops
2. Implement single-pass enrichment
3. Performance testing and tuning
4. Integration tests

#### Phase 3: Validation (Week 3)
1. Comprehensive testing
2. Performance benchmarking
3. Code review and cleanup
4. Documentation updates

## 5. Implementation Details

### 5.1 New Function Signatures

```go
// Main function - simplified
func (s *featureUsageTrackingService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

// Helper functions - single responsibility
func (s *featureUsageTrackingService) validateAnalyticsRequest(req *dto.GetUsageAnalyticsRequest) error
func (s *featureUsageTrackingService) fetchAnalyticsData(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*AnalyticsData, error)
func (s *featureUsageTrackingService) buildAnalyticsResponse(ctx context.Context, data *AnalyticsData) (*dto.GetUsageAnalyticsResponse, error)

// Data fetching - parallel execution
func (s *featureUsageTrackingService) fetchCustomer(ctx context.Context, externalCustomerID string) (*customer.Customer, error)
func (s *featureUsageTrackingService) fetchSubscriptions(ctx context.Context, customerID string) ([]*subscription.Subscription, error)
func (s *featureUsageTrackingService) fetchAnalytics(ctx context.Context, params *events.UsageAnalyticsParams) ([]*events.DetailedUsageAnalytic, error)

// Data processing - single pass
func (s *featureUsageTrackingService) enrichWithMetadata(ctx context.Context, data *AnalyticsData) error
func (s *featureUsageTrackingService) calculateCosts(ctx context.Context, data *AnalyticsData) error
func (s *featureUsageTrackingService) aggregateResults(data *AnalyticsData) []*events.DetailedUsageAnalytic
```

### 5.2 Parallel Fetching Implementation

```go
func (s *featureUsageTrackingService) fetchAnalyticsData(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*AnalyticsData, error) {
    // Create channels for parallel execution
    customerChan := make(chan *customer.Customer, 1)
    subscriptionsChan := make(chan []*subscription.Subscription, 1)
    analyticsChan := make(chan []*events.DetailedUsageAnalytic, 1)
    errorChan := make(chan error, 3)

    // Start parallel goroutines
    go func() {
        customer, err := s.fetchCustomer(ctx, req.ExternalCustomerID)
        if err != nil {
            errorChan <- err
            return
        }
        customerChan <- customer
    }()

    go func() {
        // Wait for customer first
        customer := <-customerChan
        subscriptions, err := s.fetchSubscriptions(ctx, customer.ID)
        if err != nil {
            errorChan <- err
            return
        }
        subscriptionsChan <- subscriptions
    }()

    go func() {
        // Create params and fetch analytics
        params := s.createAnalyticsParams(ctx, req)
        analytics, err := s.fetchAnalytics(ctx, params)
        if err != nil {
            errorChan <- err
            return
        }
        analyticsChan <- analytics
    }()

    // Wait for all goroutines to complete
    var customer *customer.Customer
    var subscriptions []*subscription.Subscription
    var analytics []*events.DetailedUsageAnalytic

    for i := 0; i < 3; i++ {
        select {
        case err := <-errorChan:
            return nil, err
        case customer = <-customerChan:
        case subscriptions = <-subscriptionsChan:
        case analytics = <-analyticsChan:
        }
    }

    // Build and return data structure
    return &AnalyticsData{
        Customer:      customer,
        Subscriptions: subscriptions,
        Analytics:     analytics,
        // ... other fields populated in enrichment phase
    }, nil
}
```

## 6. Testing Strategy

### 6.1 Unit Tests
- **Coverage**: 100% of new functions
- **Focus**: Individual function behavior
- **Mocking**: All external dependencies

### 6.2 Integration Tests
- **Coverage**: End-to-end functionality
- **Focus**: API contract compliance
- **Data**: Real database with test data

### 6.3 Performance Tests
- **Load Testing**: 100 concurrent requests
- **Benchmarking**: Before/after performance comparison
- **Monitoring**: Response time, memory usage, CPU usage

### 6.4 Regression Tests
- **Existing Tests**: All current tests must pass
- **API Compatibility**: No breaking changes
- **Data Accuracy**: Identical results to current implementation

## 7. Risk Assessment

### 7.1 Technical Risks

#### High Risk
- **Data Accuracy**: Risk of introducing bugs in business logic
- **Mitigation**: Comprehensive testing, gradual rollout

#### Medium Risk
- **Performance**: Parallel execution might not improve performance as expected
- **Mitigation**: Benchmarking, performance monitoring

#### Low Risk
- **API Changes**: Risk of breaking existing consumers
- **Mitigation**: Maintain exact API contract

### 7.2 Business Risks

#### High Risk
- **Service Disruption**: Refactoring might cause temporary outages
- **Mitigation**: Feature flags, gradual rollout, rollback plan

#### Medium Risk
- **Timeline**: Refactoring might take longer than expected
- **Mitigation**: Phased approach, regular checkpoints

## 8. Success Criteria

### 8.1 Performance Metrics
- [ ] Response time: < 150ms (vs current 200-500ms)
- [ ] Database calls: 3 parallel (vs current 4+ sequential)
- [ ] Memory usage: < 10% increase
- [ ] CPU usage: < 20% increase

### 8.2 Code Quality Metrics
- [ ] Main function: < 60 lines (vs current 125+)
- [ ] Nesting levels: ≤ 2 (vs current 3+)
- [ ] Function complexity: < 10 (vs current 20+)
- [ ] Test coverage: 100% maintained

### 8.3 Business Metrics
- [ ] Zero API breaking changes
- [ ] Zero data accuracy issues
- [ ] Zero performance regressions
- [ ] 100% backward compatibility

## 9. Timeline

### Week 1: Core Refactoring
- Day 1-2: Create data structures and parallel fetching
- Day 3-4: Refactor main function and helpers
- Day 5: Unit tests and initial validation

### Week 2: Optimization
- Day 1-2: Optimize data processing and enrichment
- Day 3-4: Performance testing and tuning
- Day 5: Integration tests

### Week 3: Validation
- Day 1-2: Comprehensive testing and benchmarking
- Day 3-4: Code review and cleanup
- Day 5: Documentation and deployment

## 10. Dependencies

### 10.1 Technical Dependencies
- Go 1.21+ (for improved goroutine performance)
- Existing database schemas (no changes required)
- Current API contracts (maintained)

### 10.2 Team Dependencies
- Backend team: Implementation and testing
- QA team: Performance and integration testing
- DevOps team: Monitoring and deployment

## 11. Rollback Plan

### 11.1 Immediate Rollback
- Revert to previous version if critical issues found
- Feature flag to disable new implementation
- Database rollback if schema changes made

### 11.2 Gradual Rollback
- A/B testing with percentage of traffic
- Monitor performance metrics closely
- Rollback if metrics degrade

## 12. Monitoring and Alerting

### 12.1 Key Metrics
- Response time percentiles (p50, p95, p99)
- Error rate and types
- Database query performance
- Memory and CPU usage

### 12.2 Alerts
- Response time > 200ms
- Error rate > 1%
- Database timeout errors
- Memory usage > 80%

---

**Document Version**: 1.0  
**Last Updated**: 2024-12-19  
**Author**: Development Team  
**Status**: Draft  
**Next Review**: 2024-12-26
