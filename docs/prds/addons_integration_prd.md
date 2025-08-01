# Add-ons Integration PRD

## Overview

This PRD outlines the integration of **Add-ons** as a first-class concept into our existing subscription billing and entitlement system, following industry best practices from leading platforms like [Stripe](https://docs.stripe.com/saas), Chargebee, Stigg, Lago, Togai, Orb, and OpenMeter. Add-ons will allow customers to purchase additional features, usage limits, and capabilities beyond their base plan, with full integration into entitlements, billing, and usage tracking.

### Industry Context

Modern SaaS billing platforms handle add-ons through several key patterns:

1. **Stripe's Approach**: Add-ons as separate line items with subscription lifecycle alignment
2. **Chargebee's Model**: Add-ons with independent billing cycles and proration support
3. **Stigg's Entitlement Model**: Add-ons that extend feature limits and capabilities
4. **Lago's Usage-Based Model**: Add-ons that provide additional usage allowances
5. **Togai's Pricing Flexibility**: Add-ons with complex pricing models and dependencies
6. **Orb's Metering Integration**: Add-ons that integrate with usage-based billing
7. **OpenMeter's Analytics**: Add-ons with detailed usage tracking and analytics

## Problem Statement

Currently, our system has the following limitations:

1. **Single Plan Limitation**: Each subscription is tied to a single plan, restricting customers from purchasing additional features or capabilities
2. **Limited Flexibility**: Customers cannot mix and match different products or purchase additional usage beyond their plan limits
3. **No Post-Subscription Enhancements**: Once a subscription is created, customers cannot easily add additional features or capabilities
4. **Entitlement Rigidity**: Entitlements are strictly tied to plans, with no way to extend or modify them after subscription creation

## Functional Requirements

### Core Add-on Capabilities

1. **Add-on Types** (Inspired by Stripe and Chargebee):

   - **Single-instance**: Only one instance allowed per subscription (e.g., "Premium Support")
   - **Multi-instance**: Multiple instances allowed with quantity-based pricing (e.g., "Additional API Calls")
   - **Usage-based**: Add-ons that provide additional usage allowances (e.g., "Extra API Calls")
   - **Feature-based**: Add-ons that unlock specific features (e.g., "Advanced Analytics")

2. **Add-on Lifecycle** (Following Stripe's subscription lifecycle):

   - **Creation Phase**: Add-ons can be attached during subscription creation
   - **Active Phase**: Add-ons can be purchased post-subscription creation
   - **Modification Phase**: Add-ons can be removed or modified during subscription lifecycle
   - **Billing Alignment**: Add-ons inherit subscription billing cycles and periods
   - **Proration Support**: Mid-cycle changes trigger automatic proration calculations
   - **Cancellation Phase**: Add-ons can be cancelled with end-of-period or immediate effect

3. **Entitlement Integration** (Following Stigg's entitlement model):

   - Add-ons contribute to customer entitlements
   - Entitlements from plan + add-ons are merged intelligently
   - Usage limits are aggregated across plan and add-ons
   - Boolean features are enabled if any source (plan or add-on) enables them
   - Feature dependencies are respected (add-ons can depend on base plan features)

4. **Billing Integration** (Following Stripe's billing patterns):

   - Add-ons have their own prices stored in the `prices` table
   - Add-ons contribute to invoice generation as separate line items
   - Add-ons support all existing price types (fixed, usage, tiered)
   - Add-ons respect subscription billing cycles and periods
   - Add-ons support proration for mid-cycle changes
   - Add-ons integrate with usage-based billing (like Lago and Orb)

5. **Lifecycle Management** (Following Industry Standards):

   - **Creation**: Add-ons inherit subscription billing cycles and periods
   - **Modification**: Mid-cycle changes trigger automatic proration (following Chargebee's model)
   - **Cancellation**: Support for immediate or end-of-period cancellation (following Stripe's patterns)
   - **Pause/Resume**: Support for temporary suspension of add-ons
   - **Renewal**: Automatic renewal with subscription unless cancelled
   - **Upgrades/Downgrades**: Seamless transitions with proration handling

## Data Model Design

### New Tables

#### 1. Addons Table

```sql
CREATE TABLE addons (
    id VARCHAR(50) PRIMARY KEY,
    tenant_id VARCHAR(50) NOT NULL,
    environment_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) DEFAULT 'published',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(255),
    updated_by VARCHAR(255),

    name VARCHAR(255) NOT NULL,
    lookup_key VARCHAR(255),
    description TEXT,
    type VARCHAR(20) NOT NULL, -- 'single_instance' or 'multi_instance'
    metadata JSONB,

    UNIQUE(tenant_id, environment_id, lookup_key) WHERE status = 'published' AND lookup_key IS NOT NULL AND lookup_key != ''
);
```

#### 2. Subscription Addons Table

```sql
CREATE TABLE subscription_addons (
    id VARCHAR(50) PRIMARY KEY,
    tenant_id VARCHAR(50) NOT NULL,
    environment_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) DEFAULT 'published',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(255),
    updated_by VARCHAR(255),

    subscription_id VARCHAR(50) NOT NULL,
    addon_id VARCHAR(50) NOT NULL,
    price_id VARCHAR(50) NOT NULL,
    quantity INTEGER DEFAULT 1,
    start_date TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    end_date TIMESTAMP WITH TIME ZONE,

    -- Lifecycle management (following Stripe's patterns)
    status VARCHAR(20) DEFAULT 'active', -- active, cancelled, paused
    cancellation_reason VARCHAR(255),
    cancelled_at TIMESTAMP WITH TIME ZONE,

    -- Proration support (following Chargebee's model)
    proration_behavior VARCHAR(20) DEFAULT 'create_prorations', -- create_prorations, none
    prorated_amount DECIMAL(20,8),

    -- Usage tracking (following Lago and Orb patterns)
    usage_limit DECIMAL(20,8),
    usage_reset_period VARCHAR(20),
    usage_reset_date TIMESTAMP WITH TIME ZONE,

    metadata JSONB,

    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id),
    FOREIGN KEY (addon_id) REFERENCES addons(id),
    FOREIGN KEY (price_id) REFERENCES prices(id)
);
```

### Modified Tables

#### 1. Prices Table

Add nullable `addon_id` field:

```sql
ALTER TABLE prices ADD COLUMN addon_id VARCHAR(50);
ALTER TABLE prices ADD CONSTRAINT fk_prices_addon_id FOREIGN KEY (addon_id) REFERENCES addons(id);
-- Ensure only one of plan_id or addon_id is set
ALTER TABLE prices ADD CONSTRAINT check_price_source CHECK (
    (plan_id IS NOT NULL AND addon_id IS NULL) OR
    (plan_id IS NULL AND addon_id IS NOT NULL)
);
```

#### 2. Entitlements Table

Add nullable `addon_id` field:

```sql
ALTER TABLE entitlements ADD COLUMN addon_id VARCHAR(50);
ALTER TABLE entitlements ADD CONSTRAINT fk_entitlements_addon_id FOREIGN KEY (addon_id) REFERENCES addons(id);
-- Ensure only one of plan_id or addon_id is set
ALTER TABLE entitlements ADD CONSTRAINT check_entitlement_source CHECK (
    (plan_id IS NOT NULL AND addon_id IS NULL) OR
    (plan_id IS NULL AND addon_id IS NOT NULL)
);
```

#### 3. Subscription Line Items Table

Add nullable `addon_id` field:

```sql
ALTER TABLE subscription_line_items ADD COLUMN addon_id VARCHAR(50);
ALTER TABLE subscription_line_items ADD COLUMN addon_display_name VARCHAR(255);
ALTER TABLE subscription_line_items ADD CONSTRAINT fk_subscription_line_items_addon_id FOREIGN KEY (addon_id) REFERENCES addons(id);
```

### Domain Models

#### Addon Domain Model

```go
type Addon struct {
    ID            string            `json:"id"`
    Name          string            `json:"name"`
    LookupKey     string            `json:"lookup_key"`
    Description   string            `json:"description"`
    Type          types.AddonType   `json:"type"` // single_instance | multi_instance
    Metadata      types.Metadata    `json:"metadata"`
    EnvironmentID string            `json:"environment_id"`
    types.BaseModel
}

type AddonType string

const (
    AddonTypeSingleInstance AddonType = "single_instance"
    AddonTypeMultiInstance  AddonType = "multi_instance"
)
```

#### Subscription Addon Domain Model

```go
type SubscriptionAddon struct {
    ID             string          `json:"id"`
    SubscriptionID string          `json:"subscription_id"`
    AddonID        string          `json:"addon_id"`
    PriceID        string          `json:"price_id"`
    Quantity       int             `json:"quantity"`
    StartDate      time.Time       `json:"start_date"`
    EndDate        *time.Time      `json:"end_date,omitempty"`

    // Lifecycle management (following Stripe's patterns)
    Status             types.AddonStatus `json:"status"`
    CancellationReason string            `json:"cancellation_reason,omitempty"`
    CancelledAt        *time.Time        `json:"cancelled_at,omitempty"`

    // Proration support (following Chargebee's model)
    ProrationBehavior string            `json:"proration_behavior"`
    ProratedAmount    decimal.Decimal   `json:"prorated_amount,omitempty"`

    // Usage tracking (following Lago and Orb patterns)
    UsageLimit       *decimal.Decimal `json:"usage_limit,omitempty"`
    UsageResetPeriod string           `json:"usage_reset_period,omitempty"`
    UsageResetDate   *time.Time       `json:"usage_reset_date,omitempty"`

    Metadata         types.Metadata  `json:"metadata"`
    EnvironmentID    string          `json:"environment_id"`
    types.BaseModel
}

type AddonStatus string

const (
    AddonStatusActive    AddonStatus = "active"
    AddonStatusCancelled AddonStatus = "cancelled"
    AddonStatusPaused    AddonStatus = "paused"
)
```

## Business Logic Flow

### 1. Add-on Creation and Management

#### Add-on Creation

```go
func (s *addonService) CreateAddon(ctx context.Context, req *dto.CreateAddonRequest) (*dto.AddonResponse, error) {
    // Validate addon data
    // Create addon
    // Create associated prices (if provided)
    // Create associated entitlements (if provided)
    // Return response
}
```

### 2. Lifecycle Management (Following Industry Standards)

#### Add-on Lifecycle States (Following Stripe's Subscription States)

```go
type AddonLifecycleState string

const (
    AddonStateActive    AddonLifecycleState = "active"
    AddonStatePaused    AddonLifecycleState = "paused"
    AddonStateCancelled AddonLifecycleState = "cancelled"
    AddonStateExpired   AddonLifecycleState = "expired"
)

// Lifecycle transitions following Stripe's patterns
func (s *addonService) HandleLifecycleTransition(ctx context.Context, addonID string, newState AddonLifecycleState, reason string) error {
    // Validate state transition
    // Update addon state
    // Handle billing implications
    // Send webhook events
    // Update entitlements
}
```

#### Lifecycle Events (Following Stripe's Webhook Patterns)

```go
// Addon lifecycle events following Stripe's webhook patterns
type AddonEventType string

const (
    AddonEventCreated     AddonEventType = "addon.created"
    AddonEventUpdated     AddonEventType = "addon.updated"
    AddonEventCancelled   AddonEventType = "addon.cancelled"
    AddonEventPaused      AddonEventType = "addon.paused"
    AddonEventResumed     AddonEventType = "addon.resumed"
    AddonEventRenewed     AddonEventType = "addon.renewed"
    AddonEventProrated    AddonEventType = "addon.prorated"
)
```

#### Add-on Assignment to Subscription (Following Stripe's Patterns)

```go
func (s *subscriptionService) AddAddonToSubscription(ctx context.Context, subscriptionID string, req *dto.AddAddonRequest) error {
    // Validate addon exists and is available
    // Check if addon is already assigned (for single-instance)
    // Validate price compatibility with subscription
    // Validate feature dependencies (following Stigg's model)
    // Create subscription addon record with lifecycle management
    // Create subscription line items for addon prices
    // Calculate proration if mid-cycle (following Chargebee's model)
    // Update entitlements with usage tracking
    // Trigger billing recalculation if needed
    // Send webhook events for addon lifecycle (following Stripe's webhook patterns)
}
```

### 2. Entitlement Resolution Logic

#### Enhanced Entitlement Aggregation

```go
func (s *billingService) GetCustomerEntitlements(ctx context.Context, customerID string, req *dto.GetCustomerEntitlementsRequest) (*dto.CustomerEntitlementsResponse, error) {
    // Get all subscriptions for customer
    // For each subscription:
    //   - Get plan entitlements
    //   - Get addon entitlements
    //   - Merge entitlements intelligently
    // Return aggregated entitlements
}

func mergeEntitlements(planEntitlements []*entitlement.Entitlement, addonEntitlements []*entitlement.Entitlement) []*entitlement.Entitlement {
    // For metered features: sum usage limits
    // For boolean features: enable if any source enables
    // For static features: collect all unique values
    // Handle conflicts and overrides
}
```

#### Entitlement Merging Rules (Following Stigg's and Lago's Models)

1. **Metered Features** (Usage-based, following Lago and Orb patterns):

   - Usage limits are **summed** across plan and add-ons
   - Reset periods must match (enforce same billing period)
   - Soft limits: if any source is soft limit, result is soft limit
   - Usage tracking: Track usage against combined limits with source attribution
   - Overage handling: Apply overage charges based on combined limits

2. **Boolean Features** (Feature-based, following Stigg's model):

   - Feature is **enabled** if any source (plan or add-on) enables it
   - No quantity or limit considerations
   - Feature dependencies: Respect addon dependencies on base plan features
   - Feature conflicts: Handle conflicting feature configurations

3. **Static Features** (Configuration-based):

   - All unique static values are **collected**
   - No deduplication (allow multiple values)
   - Value precedence: Define precedence rules for conflicting values
   - Configuration merging: Merge configurations from multiple sources

4. **Usage Tracking** (Following OpenMeter's analytics model):

   - Track usage by source (plan vs addon)
   - Provide detailed analytics on addon usage
   - Support usage-based addon limits
   - Enable usage-based addon pricing

### 3. Billing Integration

#### Invoice Line Item Strategy (Following Stripe's Billing Patterns)

```go
func (s *billingService) CalculateFixedCharges(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // Get all line items (plan + addon)
    // Calculate charges for each line item
    // Separate line items by source (plan vs addon)
    // Handle proration for addon changes (following Chargebee's model)
    // Apply discounts and credits
    // Return fixed charges with proper attribution
}

func (s *billingService) CalculateUsageCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // Get all entitlements (plan + addon)
    // Calculate usage against combined limits (following Lago and Orb patterns)
    // Apply overage charges based on combined limits
    // Track usage by source for analytics (following OpenMeter's model)
    // Return usage charges with proper attribution
}
```

#### Line Item Attribution (Following Stripe's Invoice Patterns)

- **Plan Line Items**: Include `plan_id` and `plan_display_name`
- **Addon Line Items**: Include `addon_id` and `addon_display_name`
- **Separate Line Items**: Plan and addon charges appear as separate line items for clarity
- **Proration Line Items**: Separate line items for prorated amounts (following Chargebee's model)
- **Usage Line Items**: Separate line items for usage charges with source attribution
- **Discount Line Items**: Separate line items for addon-specific discounts

### 4. Usage Tracking and Enforcement

#### Enhanced Usage Calculation (Following Lago and Orb Patterns)

```go
func (s *billingService) CalculateUsageCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
    // Get all entitlements (plan + addon)
    // Calculate usage against combined entitlements
    // Apply overage charges based on combined limits
    // Track usage by source for analytics (following OpenMeter's model)
    // Handle usage-based addon limits
    // Return usage charges with proper attribution
}

func (s *billingService) TrackAddonUsage(ctx context.Context, subscriptionID string, addonID string, usage decimal.Decimal) error {
    // Track usage against addon limits
    // Update usage reset dates
    // Send webhook events for usage thresholds
    // Trigger billing if usage exceeds limits
}
```

## API Requirements

### 1. Add-on Management APIs (Following Stripe's API Patterns)

#### Create Add-on

```http
POST /api/v1/addons
Content-Type: application/json

{
    "name": "Premium Support",
    "lookup_key": "premium_support",
    "description": "24/7 premium support",
    "type": "single_instance",
    "pricing_model": "fixed", // fixed, usage, tiered
    "prices": [
        {
            "amount": 50.00,
            "currency": "usd",
            "billing_period": "monthly",
            "type": "fixed"
        }
    ],
    "entitlements": [
        {
            "feature_id": "premium_support",
            "feature_type": "boolean",
            "is_enabled": true
        }
    ],
    "dependencies": {
        "required_features": ["basic_support"],
        "required_plans": ["pro", "enterprise"]
    },
    "usage_limits": {
        "max_quantity": 1,
        "usage_reset_period": "monthly"
    }
}
```

#### List Add-ons

```http
GET /api/v1/addons?status=published&type=single_instance
```

#### Update Add-on

```http
PUT /api/v1/addons/{addon_id}
Content-Type: application/json

{
    "name": "Premium Support Plus",
    "description": "Enhanced premium support"
}
```

### 2. Subscription Add-on APIs

#### Add Add-on to Subscription (Following Chargebee's Proration Model)

```http
POST /api/v1/subscriptions/{subscription_id}/addons
Content-Type: application/json

{
    "addon_id": "addon_premium_support",
    "price_id": "price_premium_support_monthly",
    "quantity": 1,
    "proration_behavior": "create_prorations", // create_prorations, none
    "effective_date": "2024-01-15T00:00:00Z", // Optional: specific date for change
    "metadata": {
        "source": "customer_portal",
        "reason": "upgrade_request"
    }
}
```

#### List Subscription Add-ons

```http
GET /api/v1/subscriptions/{subscription_id}/addons
```

#### Remove Add-on from Subscription

```http
DELETE /api/v1/subscriptions/{subscription_id}/addons/{addon_id}
```

#### Update Subscription Add-on (Following Stripe's Update Patterns)

```http
PUT /api/v1/subscriptions/{subscription_id}/addons/{addon_id}
Content-Type: application/json

{
    "quantity": 2,
    "price_id": "price_premium_support_yearly",
    "proration_behavior": "create_prorations",
    "effective_date": "2024-01-15T00:00:00Z",
    "metadata": {
        "source": "customer_portal",
        "reason": "quantity_increase"
    }
}
```

### 3. Enhanced Subscription APIs

#### Create Subscription with Add-ons

```http
POST /api/v1/subscriptions
Content-Type: application/json

{
    "customer_id": "cust_123",
    "plan_id": "plan_pro",
    "currency": "usd",
    "billing_period": "monthly",
    "addons": [
        {
            "addon_id": "addon_premium_support",
            "price_id": "price_premium_support_monthly",
            "quantity": 1
        }
    ]
}
```

#### Enhanced Subscription Response (Following Stripe's Response Patterns)

```json
{
  "id": "sub_123",
  "customer_id": "cust_123",
  "plan_id": "plan_pro",
  "status": "active",
  "line_items": [
    {
      "id": "li_plan_123",
      "plan_id": "plan_pro",
      "plan_display_name": "Pro Plan",
      "price_id": "price_pro_monthly",
      "amount": 29.99,
      "type": "fixed"
    },
    {
      "id": "li_addon_123",
      "addon_id": "addon_premium_support",
      "addon_display_name": "Premium Support",
      "price_id": "price_premium_support_monthly",
      "amount": 50.0,
      "type": "fixed"
    }
  ],
  "addons": [
    {
      "id": "sa_123",
      "addon_id": "addon_premium_support",
      "price_id": "price_premium_support_monthly",
      "quantity": 1,
      "start_date": "2024-01-01T00:00:00Z",
      "status": "active",
      "proration_behavior": "create_prorations",
      "usage_limit": 1000,
      "usage_reset_period": "monthly",
      "usage_reset_date": "2024-02-01T00:00:00Z"
    }
  ],
  "total_addon_amount": 50.0,
  "total_amount": 79.99
}
```

### 4. Enhanced Entitlement APIs (Following Stigg's and Lago's Models)

#### Get Customer Entitlements (Enhanced)

```http
GET /api/v1/customers/{customer_id}/entitlements
```

Response includes addon sources with usage tracking:

```json
{
  "customer_id": "cust_123",
  "features": [
    {
      "feature": {
        "id": "feat_api_calls",
        "name": "API Calls",
        "type": "metered"
      },
      "entitlement": {
        "is_enabled": true,
        "usage_limit": 10000,
        "usage_reset_period": "monthly",
        "current_usage": 7500,
        "usage_percentage": 75.0
      },
      "sources": [
        {
          "subscription_id": "sub_123",
          "plan_id": "plan_pro",
          "plan_name": "Pro Plan",
          "entitlement_id": "ent_plan_123",
          "usage_limit": 5000,
          "current_usage": 4000,
          "source_type": "plan"
        },
        {
          "subscription_id": "sub_123",
          "addon_id": "addon_extra_api",
          "addon_name": "Extra API Calls",
          "entitlement_id": "ent_addon_123",
          "usage_limit": 5000,
          "current_usage": 3500,
          "source_type": "addon"
        }
      ]
    }
  ],
  "usage_summary": {
    "total_usage": 7500,
    "total_limit": 10000,
    "remaining_usage": 2500,
    "overage_amount": 0
  }
}
```

## Edge Cases

### 1. Add-on Conflicts and Validation (Following Industry Standards)

#### Single-Instance Add-on Duplication

```go
func validateAddonAssignment(ctx context.Context, subscriptionID string, addonID string) error {
    // Check if addon is single-instance
    // Check if already assigned to subscription
    // Return error if duplicate assignment attempted
}
```

#### Price Compatibility (Following Stripe's Compatibility Rules)

```go
func validatePriceCompatibility(subscription *subscription.Subscription, addonPrice *price.Price) error {
    // Validate currency matches subscription
    // Validate billing period compatibility
    // Validate billing cadence compatibility
    // Validate pricing model compatibility
    // Return error if incompatible
}
```

#### Lifecycle Compatibility (Following Chargebee's Lifecycle Rules)

```go
func validateLifecycleCompatibility(subscription *subscription.Subscription, addon *Addon) error {
    // Validate subscription state allows addon changes
    // Validate addon availability for subscription
    // Validate billing cycle alignment
    // Return error if incompatible
}
```

### 2. Entitlement Conflicts

#### Conflicting Usage Reset Periods

```go
func validateEntitlementCompatibility(planEntitlements []*entitlement.Entitlement, addonEntitlements []*entitlement.Entitlement) error {
    // Check for same feature with different reset periods
    // Enforce consistent reset periods for metered features
    // Return error if incompatible
}
```

#### Soft vs Hard Limits

```go
func mergeUsageLimits(planLimit *int64, addonLimit *int64, planSoft bool, addonSoft bool) (*int64, bool) {
    // If any source is soft limit, result is soft limit
    // Sum the limits
    // Return combined limit and soft flag
}
```

### 3. Billing Edge Cases (Following Chargebee's and Stripe's Patterns)

#### Proration for Add-on Changes

```go
func calculateAddonProration(subscription *subscription.Subscription, addon *SubscriptionAddon, changeDate time.Time) (decimal.Decimal, error) {
    // Calculate prorated amount for partial periods (following Chargebee's model)
    // Handle mid-cycle addon additions/removals
    // Apply proration behavior (create_prorations vs none)
    // Generate proration line items
    // Return prorated amount
}
```

#### Add-on Cancellation (Following Stripe's Cancellation Patterns)

```go
func cancelSubscriptionAddon(ctx context.Context, subscriptionID string, addonID string, effectiveDate time.Time) error {
    // Set end date for addon
    // Remove from active entitlements
    // Recalculate billing with proration
    // Generate credit/charge for unused period
    // Send webhook events for cancellation
    // Update usage tracking
}
```

#### Usage-Based Addon Limits (Following Lago's Model)

```go
func handleAddonUsageLimit(ctx context.Context, subscriptionID string, addonID string, usage decimal.Decimal) error {
    // Check usage against addon limits
    // Apply overage charges if limits exceeded
    // Send notifications for usage thresholds
    // Update usage reset dates
}
```

### 4. Migration Considerations (Following Industry Best Practices)

#### Existing Subscription Migration

```go
func migrateExistingSubscriptions(ctx context.Context) error {
    // For existing subscriptions:
    //   - Create subscription addon records for plan line items
    //   - Ensure entitlements are properly linked
    //   - Validate data consistency
    //   - Preserve billing history and line items
    //   - Update usage tracking to include addon sources
}
```

#### Data Migration Strategy (Following Stripe's Migration Patterns)

```go
func migrateAddonData(ctx context.Context) error {
    // Phase 1: Schema migration (non-breaking)
    // Phase 2: Data migration (backward compatible)
    // Phase 3: Feature flag rollout
    // Phase 4: Full feature enablement
    // Phase 5: Cleanup and optimization
}
```

## Migration Plan

### Phase 1: Schema Migration

1. Create new tables (`addons`, `subscription_addons`)
2. Add nullable columns to existing tables (`prices.addon_id`, `entitlements.addon_id`, `subscription_line_items.addon_id`)
3. Add constraints and indexes
4. Run data validation scripts

### Phase 2: Core Implementation

1. Implement addon domain models and repositories
2. Implement addon service layer
3. Implement subscription addon service layer
4. Update entitlement aggregation logic
5. Update billing calculation logic

### Phase 3: API Implementation

1. Implement addon management APIs
2. Implement subscription addon APIs
3. Enhance existing subscription APIs
4. Update entitlement APIs

### Phase 4: Testing and Validation

1. Unit tests for all new functionality
2. Integration tests for billing scenarios
3. Performance testing for entitlement aggregation
4. Data migration validation

### Phase 5: Deployment

1. Deploy schema changes
2. Deploy application changes
3. Monitor for issues
4. Rollback plan if needed

## Open Questions (Industry-Informed Considerations)

### 1. Add-on Versioning (Following Stripe's Product Versioning)

- Should add-ons support versioning like plans?
- How to handle add-on updates for existing subscriptions?
- Should versioning follow Stripe's product versioning patterns?

### 2. Add-on Dependencies (Following Togai's Dependency Model)

- Should add-ons support dependencies on other add-ons?
- How to handle circular dependencies?
- Should dependencies follow Togai's complex dependency resolution?

### 3. Add-on Pricing Models (Following Lago's Usage-Based Pricing)

- Should add-ons support tiered pricing?
- How to handle usage-based add-ons with different pricing models?
- Should pricing follow Lago's flexible usage-based pricing?

### 4. Add-on Lifecycle Events (Following Stripe's Webhook Patterns)

- Should add-on changes trigger webhook events?
- How to handle add-on-specific events in the event system?
- Should events follow Stripe's comprehensive webhook patterns?

### 5. Add-on Analytics (Following OpenMeter's Analytics Model)

- How to track add-on usage and performance?
- What metrics are important for add-on optimization?
- Should analytics follow OpenMeter's detailed usage tracking?

### 6. Add-on Marketplace Integration (Following Stripe's App Marketplace)

- Should add-ons support marketplace-style distribution?
- How to handle third-party add-on providers?
- Should marketplace follow Stripe's app marketplace patterns?

## Diagrams

### Entity Relationship Diagram

```
[Plans] 1:N [Entitlements] 1:1 [Features]
[Plans] 1:N [Prices]
[Subscriptions] 1:N [SubscriptionLineItems]
[Subscriptions] 1:N [SubscriptionAddons] N:1 [Addons]
[Addons] 1:N [Entitlements] 1:1 [Features]
[Addons] 1:N [Prices]
[SubscriptionAddons] N:1 [Prices]
```

### Add-on Assignment Flow

```
Customer Request → Validate Add-on → Check Compatibility →
Create SubscriptionAddon → Create Line Items → Update Entitlements →
Recalculate Billing → Return Response
```

### Entitlement Aggregation Flow

```
Get Subscriptions → Get Plan Entitlements → Get Addon Entitlements →
Merge Entitlements → Apply Rules → Return Aggregated Entitlements
```

## Success Metrics (Following Industry Standards)

1. **Feature Adoption**: Percentage of subscriptions with add-ons (target: 30%+)
2. **Revenue Impact**: Additional revenue from add-on sales (target: 15%+ ARR increase)
3. **Customer Satisfaction**: Reduced churn for customers with add-ons (target: 20%+ reduction)
4. **System Performance**: Entitlement aggregation response times (target: <200ms)
5. **Data Integrity**: Zero data inconsistencies in entitlement calculations
6. **Usage Analytics**: Detailed tracking of addon usage patterns (following OpenMeter's model)
7. **Billing Accuracy**: Proration and billing accuracy (target: 99.9%+)
8. **API Performance**: Addon management API response times (target: <100ms)

## Conclusion

This PRD provides a comprehensive framework for integrating add-ons into the existing subscription billing system, following industry best practices from leading platforms like [Stripe](https://docs.stripe.com/saas), Chargebee, Stigg, Lago, Togai, Orb, and OpenMeter. The design maintains backward compatibility while adding significant flexibility for customers to customize their subscriptions.

### Key Industry Alignments

1. **Stripe's Billing Patterns**: Subscription lifecycle management and webhook events
2. **Chargebee's Proration Model**: Flexible proration handling for mid-cycle changes
3. **Stigg's Entitlement Model**: Feature-based entitlement aggregation
4. **Lago's Usage-Based Pricing**: Usage tracking and overage handling
5. **Togai's Dependency Management**: Complex addon dependency resolution
6. **Orb's Metering Integration**: Usage-based billing integration
7. **OpenMeter's Analytics**: Detailed usage tracking and analytics

The implementation prioritizes data consistency, performance, and user experience while providing a solid foundation for future enhancements. The system is designed to scale with business growth and support complex billing scenarios common in modern SaaS applications.
