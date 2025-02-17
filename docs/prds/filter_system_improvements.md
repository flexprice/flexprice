# Filter System Improvements

## Current Implementation

### 1. Base Query Filter Structure
```go
// QueryFilter represents a generic query filter with optional fields
type QueryFilter struct {
    Limit  *int    `json:"limit,omitempty" form:"limit"`
    Offset *int    `json:"offset,omitempty" form:"offset"`
    Status *string `json:"status,omitempty" form:"status"`
    Sort   *string `json:"sort,omitempty" form:"sort"`
    Order  *string `json:"order,omitempty" form:"order"`
    Expand *Expand  `json:"expand,omitempty" form:"expand"`
}
```

### 2. Time Range Filter
```go
// TimeRangeFilter provides time-based filtering capabilities
type TimeRangeFilter struct {
    StartTime *time.Time `json:"start_time,omitempty" form:"start_time"`
    EndTime   *time.Time `json:"end_time,omitempty" form:"end_time"`
}
```

### 3. Entity-Specific Filters
Entity-specific filters embed the base filters and add their own fields:

```go
// InvoiceFilter example
type InvoiceFilter struct {
    *QueryFilter
    *TimeRangeFilter
    CustomerID     string                 `json:"customer_id,omitempty" form:"customer_id"`
    SubscriptionID string                 `json:"subscription_id,omitempty" form:"subscription_id"`
    InvoiceType    InvoiceType            `json:"invoice_type,omitempty" form:"invoice_type"`
    InvoiceStatus  []InvoiceStatus        `json:"invoice_status,omitempty" form:"invoice_status"`
    PaymentStatus  []PaymentStatus `json:"payment_status,omitempty" form:"payment_status"`
}

// SubscriptionFilter example
type SubscriptionFilter struct {
    *QueryFilter
    *TimeRangeFilter
    CustomerID          string                  `json:"customer_id,omitempty" form:"customer_id"`
    PlanID             string                   `json:"plan_id,omitempty" form:"plan_id"`
    SubscriptionStatus []SubscriptionStatus    `json:"subscription_status,omitempty" form:"subscription_status"`
}
```

### 4. Query Options Interface
```go
// BaseQueryOptions defines common operations for query builders
type BaseQueryOptions[T any] interface {
    ApplyTenantFilter(ctx context.Context, query T) T
    ApplyStatusFilter(query T, status string) T
    ApplySortFilter(query T, field string, order string) T
    ApplyPaginationFilter(query T, limit int, offset int) T
    GetFieldName(field string) string
}
```

### 5. Standardized Response Format
```go
// PaginationResponse represents standardized pagination metadata
type PaginationResponse struct {
    Total  int `json:"total"`
    Limit  int `json:"limit"`
    Offset int `json:"offset"`
}

// ListResponse represents a paginated response with items
type ListResponse[T any] struct {
    Items      []T                `json:"items"`
    Pagination PaginationResponse `json:"pagination"`
}

// Entity-specific responses use type aliases
type ListInvoicesResponse = ListResponse[*InvoiceResponse]
type ListSubscriptionsResponse = ListResponse[*SubscriptionResponse]
```

## Implementation Details

### 1. Repository Layer
- Each repository implements its own query options type (e.g., `InvoiceQueryOptions`, `SubscriptionQueryOptions`)
- Query options handle:
  - Tenant isolation
  - Status filtering
  - Sorting
  - Pagination
  - Field name mapping

### 2. Service Layer
- Services use the Count method to get total items for pagination
- Handle filter validation and defaults
- Transform domain models to DTOs
- Apply business logic filters

### 3. Default Values
```go
func NewDefaultQueryFilter() *QueryFilter {
    return &QueryFilter{
        Limit:  lo.ToPtr(50),
        Offset: lo.ToPtr(0),
        Status: lo.ToPtr(StatusPublished),
        Sort:   lo.ToPtr("created_at"),
        Order:  lo.ToPtr("desc"),
    }
}

func NewNoLimitQueryFilter() *QueryFilter {
    return &QueryFilter{
        Limit:  nil, // No limit
        Offset: lo.ToPtr(0),
        Status: lo.ToPtr(StatusPublished),
        Sort:   lo.ToPtr("created_at"),
        Order:  lo.ToPtr("desc"),
    }
}
```

## Key Features

1. **Consistent Filter Structure**
   - All entities use the same base filter types
   - Common fields are handled uniformly
   - Entity-specific fields are properly typed

2. **Type Safety**
   - Use of Go generics for type-safe responses
   - Strongly typed entity-specific fields
   - Pointer fields for optional values

3. **Query Building**
   - Standardized query building process
   - Consistent tenant isolation
   - Uniform pagination handling

4. **Response Format**
   - Generic list response type
   - Consistent pagination metadata
   - Type-safe item arrays

5. **Default Values**
   - Factory functions for common filter configurations
   - Consistent default sorting and pagination
   - Support for unlimited queries

## Benefits

1. **Consistency**
   - Uniform filter structure across all entities
   - Consistent response format
   - Standard pagination handling

2. **Maintainability**
   - Reduced code duplication
   - Clear separation of concerns
   - Easy to add new entities

3. **Type Safety**
   - Compile-time type checking
   - Clear interface contracts
   - Type-safe generic responses

4. **Performance**
   - Efficient query building
   - Proper pagination support
   - Optimized database queries

## Future Enhancements

1. **Advanced Filtering**
   - Support for complex conditions (AND/OR)
   - Dynamic field filtering
   - Custom operators (gt, lt, contains, etc.)

2. **Query Optimization**
   - Query caching
   - Result set caching
   - Query plan optimization

3. **Additional Features**
   - Field selection/projection
   - Cursor-based pagination
   - Bulk operations support
