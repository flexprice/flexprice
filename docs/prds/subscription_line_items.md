# Subscription Line Items

## Problem Statement

The current subscription system has limitations in its design that need to be addressed:

1. **Single Plan Limitation**: Each subscription is tied to a single `plan_id`, which restricts customers from subscribing to multiple products/plans simultaneously.

2. **Price Selection Ambiguity**: A plan can have multiple prices with the same currency and billing period, making it unclear which specific price the customer has selected.

3. **Plan Version Tracking**: The current system doesn't track plan versions, making it difficult to maintain price consistency when plans are updated.

4. **Limited Product Flexibility**: Customers cannot mix and match different products and their associated prices in a single subscription.

## Current Architecture

### Subscription Model
- Currently, a subscription is tightly coupled with a single plan through `plan_id`
- The subscription stores currency and billing period at the subscription level
- No direct link to specific prices, only indirect through the plan

### Limitations
1. Cannot handle multiple products/plans in one subscription
2. No versioning of plans/prices
3. Ambiguous price selection when multiple prices exist for same currency/billing period
4. Limited flexibility in subscription composition

## Proposed Solution

### 1. Database Schema (ent/schema)

#### SubscriptionLineItem Schema
```go
// SubscriptionLineItem holds the schema definition for the SubscriptionLineItem entity
type SubscriptionLineItem struct {
    ent.Schema
}

// Fields of the SubscriptionLineItem
func (SubscriptionLineItem) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            Unique().
            Immutable(),
        field.String("subscription_id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            NotEmpty().
            Immutable(),
        field.String("customer_id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            NotEmpty().
            Immutable(),
        field.String("plan_id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            Optional().
            Nillable(),
        field.String("plan_display_name").
            Optional().
            Nillable(),
        field.String("price_id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            NotEmpty(),
        field.String("price_type").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            Optional().
            Nillable(),
        field.String("meter_id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            Optional().
            Nillable(),
        field.String("meter_display_name").
            Optional().
            Nillable(),
        field.String("display_name").
            Optional().
            Nillable(),
        field.Other("quantity", decimal.Decimal{}).
            SchemaType(map[string]string{
                "postgres": "numeric(20,8)",
            }).
            Default(decimal.Zero),
        field.String("currency").
            SchemaType(map[string]string{
                "postgres": "varchar(10)",
            }).
            NotEmpty(),
        field.String("billing_period").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            NotEmpty(),
        field.Time("start_date").
            Optional().
            Nillable(),
        field.Time("end_date").
            Optional().
            Nillable(),
        field.JSON("metadata", map[string]string{}).
            Optional().
            SchemaType(map[string]string{
                "postgres": "jsonb",
            }),
    }
}

// Edges of the SubscriptionLineItem
func (SubscriptionLineItem) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("subscription", Subscription.Type).
            Ref("line_items").
            Field("subscription_id").
            Unique().
            Required().
            Immutable(),
    }
}

// Indexes of the SubscriptionLineItem
func (SubscriptionLineItem) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("tenant_id", "subscription_id", "status"),
        index.Fields("tenant_id", "customer_id", "status"),
        index.Fields("tenant_id", "plan_id", "status"),
        index.Fields("tenant_id", "price_id", "status"),
        index.Fields("tenant_id", "meter_id", "status"),
        index.Fields("start_date", "end_date"),
    }
}
```

### 2. Repository Layer Changes

#### SubscriptionLineItem Repository
```go
type SubscriptionLineItemRepository interface {
    // Core operations
    Create(ctx context.Context, item *SubscriptionLineItem) error
    Get(ctx context.Context, id string) (*SubscriptionLineItem, error)
    Update(ctx context.Context, item *SubscriptionLineItem) error
    Delete(ctx context.Context, id string) error
    
    // Bulk operations
    CreateBulk(ctx context.Context, items []*SubscriptionLineItem) error
    
    // Query operations
    ListBySubscription(ctx context.Context, subscriptionID string) ([]*SubscriptionLineItem, error)
    ListByCustomer(ctx context.Context, customerID string) ([]*SubscriptionLineItem, error)
    
    // Future extensibility
    GetByPriceID(ctx context.Context, priceID string) ([]*SubscriptionLineItem, error)
    GetByPlanID(ctx context.Context, planID string) ([]*SubscriptionLineItem, error)
}
```

#### Subscription Repository Updates
```go
type Repository interface {
    // Existing methods...
    
    // New methods for line items
    CreateWithLineItems(ctx context.Context, subscription *Subscription, items []*SubscriptionLineItem) error
    GetWithLineItems(ctx context.Context, id string) (*Subscription, []*SubscriptionLineItem, error)
}
```

### 3. API Structure

#### Initial Phase (Creation Only)
```go
type CreateSubscriptionRequest struct {
    CustomerID        string                       `json:"customer_id" validate:"required"`
    Currency         string                       `json:"currency" validate:"required"`
    BillingPeriod    string                       `json:"billing_period" validate:"required"`
    StartDate        time.Time                    `json:"start_date,omitempty"`
    BillingAnchor    time.Time                    `json:"billing_anchor,omitempty"`
    
    // For backward compatibility
    PlanID           string                       `json:"plan_id,omitempty"`
    
    // New fields
    LineItems        []SubscriptionLineItemRequest `json:"line_items,omitempty"`
    Metadata         map[string]string            `json:"metadata,omitempty"`
}

type SubscriptionLineItemRequest struct {
    PriceID          string                       `json:"price_id" validate:"required"`
    Quantity         decimal.Decimal              `json:"quantity" validate:"required"`
    DisplayName      string                       `json:"display_name,omitempty"`
    Metadata         map[string]string            `json:"metadata,omitempty"`
}
```

### 4. Future Extensibility Points

#### 1. Line Item Customization
- Support for custom display names at line item level
- Ability to override plan/price/meter display names
- Custom metadata per line item
- Custom billing periods per line item (future)

#### 2. Proration Support (Future Phase)
```go
type UpdateLineItemRequest struct {
    PriceID          string          `json:"price_id,omitempty"`
    Quantity         decimal.Decimal  `json:"quantity,omitempty"`
    ProrationBehavior string         `json:"proration_behavior,omitempty"` // immediate, next_period, none
    ProrationDate    time.Time       `json:"proration_date,omitempty"`
}
```

#### 3. Billing Period Flexibility
- Support for different billing periods per line item
- Alignment options for billing cycles
- Trial period management per line item

## Implementation Phases

### Phase 1: Core Infrastructure
1. Create `SubscriptionLineItem` entity and schema
2. Implement repository layer
3. Update subscription creation flow
4. Implement backward compatibility layer
5. Basic validation and error handling

### Phase 2: API Enhancement
1. Enhanced subscription retrieval with line items
2. Usage tracking per line item
3. Improved error handling and validation
4. Documentation updates

### Phase 3: Advanced Features (Future)
1. Line item management endpoints
2. Proration handling
3. Different billing cycles per line item
4. Bulk operations support

## Required Changes

### Database Changes
1. New `subscription_line_items` table
2. Foreign key relationships
3. Indexes for efficient querying
4. Migration scripts for existing data

### Service Layer Changes
1. Update `SubscriptionService`:
   - `CreateSubscription`
   - `GetSubscription`
   - `GetUsageBySubscription`
2. Update `InvoiceService`:
   - `CreateSubscriptionInvoice`
   - Usage calculation logic

### Migration Strategy

1. **Database Migration**
   - Create new tables without breaking existing ones
   - Backfill data for existing subscriptions

2. **Code Migration**
   - Implement new features behind feature flags
   - Gradual rollout to ensure stability

3. **Client Migration**
   - Provide migration guide
   - Support both old and new APIs during transition

## Monitoring and Metrics

1. Track usage of old vs new API endpoints
2. Monitor performance metrics
3. Track error rates during migration
4. Monitor subscription creation success rates

## Future Considerations

1. Support for different billing cycles per line item
2. Advanced proration rules
3. Bulk operations for line items
4. Enhanced reporting capabilities
5. Support for trial periods per line item
6. Custom display name overrides
7. Line item-specific metadata handling
8. Usage aggregation improvements 