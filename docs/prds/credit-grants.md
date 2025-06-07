# Credit Grants System Technical Requirements Document

## Overview
This document outlines the design and implementation approach for the Credit Grants system. Credit grants allow the platform to issue credits to subscriptions based on plan configurations or manual overrides. Similar to entitlements, credit grants define benefits that customers receive, but specifically focused on credits that can be used for various purposes within the system.

## Key Concepts

### Credit Grant
A credit grant represents an allocation of credits to a customer, associated with either a plan or a specific subscription:

- For Plan-scoped grants: These are template grants defined at the plan level that are applied when a subscription is created.
- For Subscription-scoped grants: These are specific grants assigned directly to a subscription, which can override or supplement the plan-level grants.

### Credit Grant Properties
The credit grant entity includes:
- Basic identification and reference (ID, name)
- Scope definition (plan or subscription level)
- Credit amount and currency
- Cadence and period information (one-time or recurring)
- Expiration configuration
- Priority for application order

## Schema Design

### Database Schema

```sql
CREATE TABLE credit_grants (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    scope VARCHAR(50) NOT NULL,
    plan_id VARCHAR(50) REFERENCES plans(id),
    subscription_id VARCHAR(50) REFERENCES subscriptions(id),
    credit_amount DECIMAL(19, 4) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    cadence VARCHAR(50) NOT NULL,
    period VARCHAR(50),
    period_count INTEGER,
    expire_in_days INTEGER,
    priority INTEGER,
    
    -- Base columns (handled by BaseMixin)
    tenant_id VARCHAR(50) NOT NULL,
    environment_id VARCHAR(50),
    status VARCHAR(50) NOT NULL DEFAULT 'published',
    metadata JSONB,
    created_by VARCHAR(255) NOT NULL,
    updated_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    CONSTRAINT check_scope_plan CHECK (
        (scope = 'PLAN' AND plan_id IS NOT NULL AND subscription_id IS NULL) OR
        (scope = 'SUBSCRIPTION' AND subscription_id IS NOT NULL AND plan_id IS NOT NULL)
    ),
    CONSTRAINT check_cadence_period CHECK (
        (cadence = 'RECURRING' AND period IS NOT NULL) OR
        (cadence = 'ONETIME')
    )
);

CREATE INDEX idx_credit_grants_tenant_id ON credit_grants(tenant_id);
CREATE INDEX idx_credit_grants_plan_id ON credit_grants(plan_id);
CREATE INDEX idx_credit_grants_subscription_id ON credit_grants(subscription_id);
CREATE INDEX idx_credit_grants_status ON credit_grants(status);
```

### Golang Struct Design

```go
// CreditGrantScope defines the scope of a credit grant
type CreditGrantScope string

const (
    CreditGrantScopePlan         CreditGrantScope = "PLAN"
    CreditGrantScopeSubscription CreditGrantScope = "SUBSCRIPTION"
)

// CreditGrantCadence defines the cadence of a credit grant
type CreditGrantCadence string

const (
    CreditGrantCadenceOneTime  CreditGrantCadence = "ONETIME"
    CreditGrantCadenceRecurring CreditGrantCadence = "RECURRING"
)

// CreditGrant represents a credit allocation for a customer
type CreditGrant struct {
    ID               string             // Unique identifier
    Name             string             // Descriptive name for the grant
    Scope            CreditGrantScope   // PLAN or SUBSCRIPTION
    PlanID           *string            // Reference to the plan (when scope is PLAN)
    SubscriptionID   *string            // Reference to the subscription (when scope is SUBSCRIPTION)
    CreditAmount     float64            // Amount of credits to grant
    Currency         string             // Currency of the grant (USD, etc.)
    Cadence          CreditGrantCadence // ONETIME or RECURRING
    Period           *BillingPeriod     // Required for RECURRING cadence (MONTHLY, etc.)
    PeriodCount      *int               // How many periods to apply the grant for
    ExpireInDays     *int               // Number of days until expiration
    Priority         *int               // Lower priority values are applied first
    
    BaseModel                          // Metadata (timestamps, tenant info, etc)
}
```

## Core Workflows

### 1. Credit Grant Management
- Create, read, update, and delete credit grants at the plan level
- Associate credit grants with plans
- Override credit grants at the subscription level

### 2. Subscription Creation/Update Flow
- When a subscription is created from a plan, apply associated credit grants
- When a subscription is updated, adjust credit grants if necessary
- Support for manually adding/overriding credit grants at subscription level

### 3. Credit Application
- Credits are added to a customer's wallet based on the defined grants
- For one-time grants, credits are applied at subscription creation
- For recurring grants, credits are applied based on the defined period (future enhancement)

## API Design

### Core Endpoints
```
POST /v1/credit-grants           // Create a new credit grant
GET /v1/credit-grants/{id}       // Get details of a specific credit grant
GET /v1/credit-grants            // List credit grants with filtering
PUT /v1/credit-grants/{id}       // Update a credit grant
DELETE /v1/credit-grants/{id}    // Delete a credit grant

// Future endpoints
GET /v1/plans/{planId}/credit-grants             // List all credit grants for a plan
GET /v1/subscriptions/{subscriptionId}/credit-grants  // List all credit grants for a subscription
```

## Implementation Phases

### Phase 1: Basic Credit Grant System
- Implement the credit grant entity and API endpoints
- Integrate with plan management
- Setup basic credit application at subscription creation

### Phase 2: Recurring Credit Grants
- Implement a cron job or temporal activity to handle recurring credit grants
- Add scheduling and application logic for period-based grants

### Phase 3: Subscription-level Overrides
- Implement full support for overriding plan-level grants at subscription level
- Allow for custom grant definition at subscription creation/update

## Considerations

### Performance and Scalability
- Index critical fields for query performance
- Ensure effective caching for frequently accessed credit grant data
- Design the recurring credit application system to scale with growing number of subscriptions

### Data Consistency
- Ensure transactional integrity when applying credits
- Maintain consistency between credit grants and actual wallet balance
- Validate constraints (e.g., check that RECURRING grants have a period defined)

### Business Logic
- Credit grants should follow a consistent application order based on priority
- Expired credits should be properly tracked and not counted in available balance
- Consider currency conversion if credits are granted in different currencies

## Technical Implementation Details

### Entity Integration
- Follow the existing entity development patterns in the codebase
- Implement proper validation at both the model and API levels
- Ensure all queries are properly filtered by tenant and environment

### Wallet Integration
- When credits are granted, they should be added to the customer's wallet
- Credit grants should specify expiration dates and priorities consistent with wallet requirements
- Maintain an audit trail of credit grant applications

## Future Enhancements
- Support for more complex recurring patterns
- Advanced reporting on credit grant usage and effectiveness
- Integration with promotional campaigns and marketing tools
- Support for credit grant templates that can be reused across plans 