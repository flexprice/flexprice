# Simplified Entitlements System Design

## Overview
This document outlines the design for a simplified entitlements system. Initially, we will assign entitlements directly based on the subscription plan. When a customer subscribes to a plan, they immediately receive the associated entitlements. For metered features, the total allowed usage for the current period is dynamically computed based on the subscription's current period and the entitlement's UsageResetPeriod. This version does not include a grant system or rollover functionality, which will be addressed in future iterations.

## Key Concepts

### Entitlement
An entitlement represents a customer's benefit rights associated with their subscription plan. It links a plan with a specific feature and provides the minimum set of information needed to determine access and usage:

- For Boolean features: a simple enable/disable flag.
- For Metered features: a dynamically resolved usage allowance for the current billing period.
- For Config features: a static configuration value.

### Future Enhancements
- Introduce entitlement grants to manage fine-grained usage allocation.
- Implement rollover functionality to carry over unused usage to the next period.
- Support add-on entitlements that can modify or extend the base benefits.

## Schema Design

### Entitlement Struct
```go
// FeatureType indicates the type of feature: boolean, metered, or config
// defined in internal/types/feature.go
type FeatureType string

const (
    FeatureTypeBoolean FeatureType = "boolean"
    FeatureTypeMetered FeatureType = "metered"
    FeatureTypeConfig  FeatureType = "config"
)

// BillingPeriod defines the reset period for metered usages
// defined in internal/types/price.go
type BillingPeriod string

const (
    BillingPeriodMonthly BillingPeriod = "monthly"
    BillingPeriodYearly  BillingPeriod = "yearly"
    BillingPeriodOneTime BillingPeriod = "one_time"
)

// Entitlement represents the benefits a customer gets from a subscription plan
// This minimal structure is used initially without employing a separate grant system.
type Entitlement struct {
    ID               string         // Unique identifier
    PlanID           string         // Reference to the plan
    FeatureID        string         // Reference to the feature
    FeatureType      FeatureType    // Type of feature: boolean, metered, or config
    
    // For Boolean features:
    IsEnabled        bool
    
    // For Metered features:
    UsageLimit       *int64         // Allowed usage per period (nil if unlimited)
    UsageResetPeriod BillingPeriod  // Billing period: monthly, yearly, or one_time
    IsSoftLimit      bool           // If true, usage can exceed the limit
    
    // For Config features:
    StaticValue      string         // Predefined static value
    
    BaseModel                     // Metadata (timestamps, tenant info, etc)
}
```

#### Notes:
- The dynamic resolution of allowed usage for metered features is computed at runtime by comparing the consumed usage with the `UsageLimit` based on the subscription's current period and the defined `UsageResetPeriod`.
- This design covers our immediate needs of providing plan benefits at onboarding, leaving advanced usage tracking (e.g., grants and rollover) for future enhancements.

## Core Workflows

### 1. Entitlement Assignment on Subscription
- When a customer subscribes to a plan, the system assigns the associated entitlements.
- For metered features, the system dynamically computes the total allowed usage for the current billing period using the `UsageLimit` and `UsageResetPeriod`.

### 2. Usage Tracking (for Metered Features)
- Usage is tracked dynamically based on the subscription's active billing period.
- A usage service calculates remaining usage by comparing the consumed amount against the `UsageLimit` for that period.

### 3. Access Control
- Boolean Features: Access is determined by the value of `IsEnabled`.
- Metered Features: Access is controlled by dynamically computing the remaining quota for the current period.
- Config Features: A static configuration (`StaticValue`) is returned to the client.

## API Design

### Core Endpoints
```
POST /v1/entitlements        // Create or update entitlements during subscription onboarding
GET /v1/entitlements/{id}    // Get details of a specific entitlement
GET /v1/subscriptions/{subscriptionId}/entitlements  // List all entitlements for a subscription
```

### Usage Service Integration
```go
// define new service in internal/services/usage.go
type UsageService interface {
    CheckAccess(ctx context.Context, customerID, featureID string) (bool, error)
    TrackUsage(ctx context.Context, customerID, featureID string, quantity int64) error
    GetRemainingQuota(ctx context.Context, customerID, featureID string) (int64, error)
}
```

## Implementation Phases

### Phase 1: Basic Entitlement Integration
- Implement the minimal entitlement structure as defined above.
- Dynamically compute usage allowance for metered features based on the current billing period.
- Support simple boolean and config features without a grants system.

### Future Phases:
- **Phase 2:** Introduce entitlement grants to manage add-ons and more granular usage allocation.
- **Phase 3:** Implement rollover functionality to carry over unused usage to subsequent billing periods.
- **Phase 4:** Enhance usage analytics and reporting for entitlements and grants.

## Considerations

### Performance and Scalability
- Cache frequently accessed entitlement data to reduce computation overhead.
- Ensure dynamic calculations for metered features are optimized for performance.
- Design the system to scale horizontally as the number of subscriptions grows.

### Data Consistency
- Use transactional processes when computing dynamic usage totals to ensure accuracy.
- Consider eventual consistency for non-critical usage tracking features.

## Future Work
- Add a detailed grants system to track individual usage allocations and support add-ons.
- Implement rollover functionality where unused usage is carried over to the next billing period.
- Introduce dynamic adjustments for entitlements based on customer behavior and add-ons.
- Expand API endpoints to manage and query detailed entitlement grant information. 