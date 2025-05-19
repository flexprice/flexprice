# Subscription Price Overrides - Technical Requirements Document

## 1. Overview

### 1.1 Purpose
This document outlines the technical requirements for implementing price overrides in the subscription creation flow. This feature will allow customers to customize pricing on a per-subscription basis, enabling flexible pricing strategies while maintaining the core plan structure.

### 1.2 Problem Statement
Currently, all subscriptions created from a plan inherit the exact same pricing structure. Businesses need the flexibility to offer custom pricing for specific customers without creating entirely new plans for each pricing variation.

### 1.3 Proposed Solution
Implement a price override mechanism that allows creation of subscription-specific prices that override the default plan prices. These overridden prices will be stored as separate entities with links to their parent prices, ensuring proper tracking and audit capabilities.

## 2. Data Model Changes

### 2.1 Price Schema Updates

Update the `Price` entity in both Ent schema and domain model:

#### 2.1.1 New Columns
| Column | Type | Description |
|--------|------|-------------|
| `scope` | `enum('PLAN','SUBSCRIPTION')` | Indicates if price is a plan default or subscription override |
| `parent_price_id` | `varchar(50)` FK → `prices.id` | References the original price (only set when `scope='SUBSCRIPTION'`) |
| `subscription_id` | `varchar(50)` FK nullable | References the subscription (only set when `scope='SUBSCRIPTION'`) |

#### 2.1.2 Schema Implementation Notes
- All existing prices will be marked with `scope='PLAN'`
- `parent_price_id` and `subscription_id` will be `NULL` for plan prices
- For subscription-scoped prices, all fields except the overridden values will be copied from the parent price
- All prices, regardless of scope, will be immutable after creation

### 2.2 Migration Strategy
1. Add the new columns to the Prices table
2. Update existing prices to have `scope='PLAN'`
3. Add appropriate indexes for efficient lookups

## 3. API Changes

### 3.1 Subscription Creation API

Add support for line item overrides in the subscription creation endpoint:

```json
{
  "customer_id": "cust_123",
  "plan_id": "plan_456",
  // existing fields...

  "override_line_items": [
    {
      "price_id": "price_base_usd",
      "quantity": 10,
      "amount": "8.00"
    }
  ]
}
```

### 3.2 Override Line Items Schema
- `price_id` (required): References the plan price to override
- `quantity` (optional): Quantity for this line item
- `amount` (optional): New price amount that overrides the original price

## 4. Repository Layer Changes

### 4.1 Price Repository Updates

1. Update `List` and `ListAll` methods to filter by `scope` and handle subscription-specific queries
2. Add a new method to create subscription-scoped price overrides
3. Ensure appropriate caching strategies for both plan and subscription prices

### 4.2 Query Changes

When fetching prices:
- Default behavior should be to fetch only `PLAN` scoped prices
- Add support to fetch prices by subscription ID for subscription-scoped prices
- Update price filters to include scope filtering

## 5. Service Layer Changes

### 5.1 Subscription Service Changes

Update the `CreateSubscription` method to:
1. Process the standard plan prices
2. Process any override line items:
   - For each override, create a clone of the original price with modified values
   - Set the appropriate scope and reference fields
   - Include these subscription-scoped prices in the subscription line items

### 5.2 Price Service Changes

1. Update methods to ensure they maintain the scope separation
2. Add support for creating derived subscription-scoped prices
3. Ensure price calculations properly consider overridden values

## 6. Business Logic Implementation

### 6.1 Price Override Flow

1. Customer selects a plan
2. Optional: Customer provides overrides for specific prices
3. System creates subscription with standard plan prices
4. For any overridden prices:
   - System creates subscription-scoped price clones
   - These clones reference both the parent price and the subscription
   - Subscription line items reference these new prices instead of the originals

### 6.2 Comparison Logic

When determining if a price has been overridden:
- Check if there's a subscription-scoped price with matching `parent_price_id`
- Compare relevant fields (amount, etc.) to identify specific differences

## 7. Edge Cases and Considerations

### 7.1 Partial Updates and Validation

- Validate that overridden prices only modify allowed fields
- Ensure currency matches between original and overridden prices
- Verify the original price exists and is valid for overriding

### 7.2 Reporting and Analytics

- Consider impact on reporting when prices are overridden
- Ensure data aggregations account for both plan and subscription-scoped prices
- Provide traceability for auditing price changes

### 7.3 Invoice and Billing Considerations

- Ensure invoicing logic properly handles subscription-specific prices
- Update billing calculation to use overridden prices when available
- Verify that preview invoices reflect the correct overridden values

### 7.4 Subscription Changes

- Define behavior when a subscription with custom pricing is modified
- Determine policy for price overrides during plan changes or upgrades
- Consider version control for price changes over time

## 8. Migration and Backward Compatibility

### 8.1 Existing Data

- Ensure existing subscriptions continue to function with plan-scoped prices
- Validate that existing price queries aren't affected by the scope changes
- Add appropriate database constraints to maintain data integrity

### 8.2 API Compatibility

- Maintain backward compatibility for existing API consumers
- Document the new override capabilities clearly
- Consider versioning strategy for the enhanced endpoints

## 9. Testing Strategy

### 9.1 Test Cases

1. Create subscription with no overrides (baseline)
2. Create subscription with price amount overrides
3. Attempt invalid overrides (mismatched currencies, etc.)
4. Verify invoice generation with overridden prices
5. Test subscription modification with overridden prices
6. Ensure billing calculations correctly use overridden values

### 9.2 Performance Testing

- Measure impact of price scope filtering on query performance
- Verify caching strategies work effectively with the new model
- Test with large numbers of subscription-specific prices

## 10. Implementation Phases

### 10.1 Phase 1 - Core Schema Changes
- Add new columns to the price table
- Update repositories to handle the scope filtering
- Migrate existing data

### 10.2 Phase 2 - Override Functionality
- Implement subscription creation with override_line_items
- Add price cloning logic
- Update affected services

### 10.3 Phase 3 - Testing and Refinement
- Comprehensive testing of all scenarios
- Performance tuning
- Documentation updates

## 11. Dependencies and Impacts

### 11.1 Services Impacted
- Subscription service
- Price service
- Billing service
- Invoice service

### 11.2 API Endpoints Affected
- `POST /subscriptions` (create subscription)
- `GET /prices` (when filtering by plan)

## 12. Summary

The subscription price override feature provides much-needed flexibility for custom pricing while maintaining the structure and organization of the plan-based subscription model. By carefully separating plan-scoped and subscription-scoped prices, we can implement this feature with minimal disruption to existing functionality while enabling powerful new business capabilities. 