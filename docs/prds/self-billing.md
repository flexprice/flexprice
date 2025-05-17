# Self-Billing Implementation for Flexprice

## Overview
This document outlines the simplified approach to implement self-billing within Flexprice, allowing us to use our own system to bill tenants using Flexprice, with a focus on rapid implementation.

## Current Architecture
Flexprice's normal flow:
1. Customer creation via `CustomerService.CreateCustomer`
2. Event ingestion via `eventService.CreateEvent`
3. Subscription creation via `SubscriptionService.CreateSubscription`
4. Usage summarization via `BillingService.GetCustomerUsageSummary`

Each request is authenticated with JWT tokens containing:
- `tenant_id` - Organization using Flexprice
- `user_id` - User making the request

## Challenges
1. **Cross-Tenant Authentication**: How to make API calls across tenant boundaries
2. **Event Data Management**: How to use tenant events as usage data for billing without duplication
3. **Separation of Concerns**: Maintaining clear boundaries between tenant data

## Simplified Implementation Approach

### 1. Customer-Tenant Mapping
Instead of modifying the Tenant schema, use natural mapping:

```
Billing Tenant
├── Customers
│   ├── Customer.ExternalID = Tenant1.ID
│   ├── Customer.ExternalID = Tenant2.ID
│   └── ...
└── Usage Events (from Tenant activities)
```

**Pros:**
- No schema changes required
- Direct 1:1 mapping using existing fields
- Faster implementation

**Cons:**
- Less explicit than a dedicated field
- Potential conflicts if external IDs ever match tenant IDs

### 2. Specialized Billing API

Create a specialized API endpoint for billing operations that switches context:

```go
// In tenant.go
// @Router /tenant/v1/billing [get]
func (h *TenantHandler) GetBillingInfo(c *gin.Context) {
    // Override tenant_id in context with billing tenant ID
    billingCtx := context.WithValue(c.Request.Context(), types.CtxTenantID, BillingTenantID)
    c.Request = c.Request.WithContext(billingCtx)
    
    // Proceed with standard operations but in billing tenant context
    // ...
}
```

**Pros:**
- Isolated change that doesn't require middleware modifications
- Reuses existing billing functionality
- Minimal changes to authentication flow

**Cons:**
- Context switching can be confusing and error-prone
- May not handle all cross-tenant scenarios

### 3. Event Replication

Add bulk event ingestion capability to replicate events for billing:

```go
// In EventService
func (s *eventService) CreateBulkEvents(ctx context.Context, events []*events.Event) error {
    // Implementation
}

// In Kafka consumer
func consumeMessages(consumer kafka.MessageConsumer, eventRepo events.Repository, topic string, log *logger.Logger) {
    // For each event:
    // 1. Process original event for tenant
    // 2. Create a billing copy with transformed properties for the billing tenant
    
    billingEvent := createBillingEvent(originalEvent, billingTenantID)
    events := []*events.Event{originalEvent, billingEvent}
    eventService.CreateBulkEvents(ctx, events)
}
```

**Pros:**
- Captures all events for billing purposes
- Relatively straightforward implementation
- No complex aggregation jobs needed

**Cons:**
- Doubles event storage requirements
- Requires corrections in two places if events need fixing
- Potential synchronization issues

## Implementation Details

### 1. Create Billing Tenant

Create a special billing tenant in the system:

```go
billingTenant := &tenant.Tenant{
    ID:     "billing-tenant-id",  // Use a fixed ID for easier reference
    Name:   "Flexprice Billing",
    Status: types.StatusPublished,
    // Other required fields...
}
```

### 2. Customer Creation from Tenants

When a new tenant is created, automatically create a corresponding customer:

```go
func createCustomerFromTenant(ctx context.Context, tenant *tenant.Tenant) error {
    billingCtx := context.WithValue(ctx, types.CtxTenantID, BillingTenantID)
    
    // Create customer with tenant ID as external ID
    req := dto.CreateCustomerRequest{
        Name:       tenant.Name,
        Email:      tenant.BillingDetails.Email,
        ExternalID: tenant.ID,  // Using tenant ID as external ID
        // Map other fields as needed
    }
    
    _, err := customerService.CreateCustomer(billingCtx, req)
    return err
}
```

### 3. Billing API Endpoint

Add an API to get billing information for the current tenant:

```go
// @Router /tenant/v1/billing/usage [get]
func (h *TenantHandler) GetTenantUsage(c *gin.Context) {
    currentTenantID := types.GetTenantID(c.Request.Context())
    
    // Create billing context
    billingCtx := context.WithValue(c.Request.Context(), types.CtxTenantID, BillingTenantID)
    
    // Find customer by external ID (tenant ID)
    customer, err := h.customerService.GetCustomerByLookupKey(billingCtx, currentTenantID)
    if err != nil {
        c.Error(err)
        return
    }
    
    // Get usage for this customer
    usage, err := h.billingService.GetCustomerUsageSummary(billingCtx, customer.ID, &dto.GetCustomerUsageSummaryRequest{})
    if err != nil {
        c.Error(err)
        return
    }
    
    c.JSON(http.StatusOK, usage)
}
```

### 4. Event Replication Implementation

Add a bulk insert method to the event repository:

```go
func (r *EventRepository) BulkInsertEvents(ctx context.Context, events []*events.Event) error {
    // Implementation to insert multiple events
}
```

Modify the Kafka consumer to create billing events:

```go
func consumeMessages(consumer kafka.MessageConsumer, eventRepo events.Repository, topic string, log *logger.Logger) {
    // Process message...
    
    // Create original event
    originalEvent := events.NewEvent(...)
    
    // Create billing copy with modified properties
    billingEvent := events.NewEvent(
        "tenant_event", // Standardized event name for billing
        BillingTenantID,
        originalEvent.TenantID, // Use original tenant ID as external customer ID
        map[string]interface{}{
            "original_event_name": originalEvent.EventName,
            "original_timestamp": originalEvent.Timestamp,
            "tenant_id": originalEvent.TenantID,
        },
        originalEvent.Timestamp,
        "",  // Generate new ID
        "",  // Customer ID will be looked up by external ID
        "system",
    )
    
    // Insert both events
    eventRepo.BulkInsertEvents(ctx, []*events.Event{originalEvent, billingEvent})
}
```

## Tradeoffs and Considerations

### Performance Impact
- Doubling events increases storage requirements
- Additional queries for customer lookups
- Context switching adds minimal overhead

### Maintainability
- Simpler implementation but less clean architecture
- More technical debt than a proper cross-tenant solution
- Corrections need to be applied in multiple places

### Security
- Context switching requires careful implementation
- Need to ensure billing tenant data is properly isolated
- Access control should be strict for billing endpoints

### Migration Path
This simpler implementation can serve immediate needs while a more robust solution is developed:

1. Use this approach to get self-billing working quickly
2. Monitor performance and identify bottlenecks
3. Incrementally improve with more robust architecture as needed:
   - Introduce proper tenant-customer mapping
   - Implement cross-tenant middleware
   - Consider event aggregation instead of duplication

## Next Steps

1. Create the billing tenant
2. Implement event bulk insertion
3. Modify Kafka consumer for event replication
4. Create tenant-to-customer mapping on tenant creation
5. Implement billing API endpoints
6. Set up plans and subscriptions for tenant billing
7. Test the flow end-to-end
