# Plan Display Order Implementation in Pricing Widget

## Overview
This PRD outlines the implementation of a display order feature for plans in the Pricing Widget, allowing for customizable ordering of plans and their associated entitlements.

## Problem Statement
Currently, the pricing widget displays plans without a defined ordering system, making it difficult for businesses to control the presentation sequence of their pricing plans. This can impact the effectiveness of pricing strategy and user experience.

## Goals
1. Implement a flexible plan ordering system using a `display_order` field
2. Ensure consistent ordering across plans and their entitlements
3. Maintain compatibility with existing plan filtering and search functionality
4. Improve the pricing widget's visual hierarchy

## Technical Requirements

### 1. Database Schema Updates
- Add `display_order` field to the following tables:
  - `plan` table: Integer field to store the display order
  - Default value: NULL (plans without display_order will be shown last)
  - Index the field for efficient sorting

### 2. API Changes

#### List Plans by Filter Endpoint
```json
{
  "sort": [
    {
      "direction": "desc",
      "field": "display_order"
    }
  ],
  "expand": ["entitlements"]
}
```

Key Features:
- Support sorting by `display_order` field
- Apply same sorting logic to expanded entitlements
- Maintain backward compatibility with existing sort parameters

### 3. Frontend Implementation

#### Pricing Widget Updates
- Modify plan fetching logic to include display_order sorting
- Update plan rendering to respect the sorted order
- Handle NULL display_order values appropriately

## Business Rules

### Display Order Logic
1. Plans with display_order values are shown first
2. Plans are sorted in ascending/descending order based on display_order value
3. Plans without display_order (NULL) are displayed last
4. When multiple plans have the same display_order:
   - Secondary sort by plan creation date (newest first)
   - Tertiary sort by plan name (alphabetically)

### Entitlement Ordering
1. When plans are expanded with entitlements:
   - Each Entitlement within the plan will hold its own display_order
   - Entitlements within the plan will be displayed according to the sort defined for them

## User Experience

### Admin Interface
- Allow administrators to set display_order through:
  1. Plan creation form
  2. Plan edit interface
  3. Bulk update functionality

### Display Rules
1. Higher display_order values = higher priority (shown first)
2. Support both ascending and descending sort directions
3. Maintain consistent ordering across all pricing widget instances

## Migration Plan

### Phase 1: Schema Update
1. Add display_order field to plan table
2. Make changes in schema only then make generate-ent followed by make migrate-ent
3. Add appropriate indexes

### Phase 2: API Implementation
1. Update List Plans endpoint to support display_order sorting
2. Implement sorting logic for expanded entitlements
3. Add validation for display_order values

### Phase 3: Frontend Updates
1. Modify pricing widget to use new sorting parameters
2. Update admin interface for display_order management
3. Add bulk update functionality for display_order

## Testing Requirements

### Unit Tests
1. Test sorting logic with:
   - Mixed NULL and non-NULL display_order values
   - Same display_order values
   - Different sort directions
   - Expanded entitlements

### Integration Tests
1. Verify API response format
2. Test sorting persistence
3. Validate entitlement expansion with sorting

### UI Tests
1. Verify correct plan display order
2. Test admin interface functionality
3. Validate bulk update features

## Success Metrics
1. Reduction in support tickets related to plan ordering
2. Increased pricing page conversion rate
3. Decreased bounce rate on pricing pages
4. Admin satisfaction with ordering control

## Future Considerations
1. Support for different ordering schemes per environment
2. A/B testing capabilities for different plan orders
3. Analytics on optimal plan ordering
4. Automated ordering based on conversion rates

## API Documentation Updates

### List Plans by Filter
```json
{
  "sort": [
    {
      "direction": "desc",  // "asc" or "desc"
      "field": "display_order"
    }
  ],
  "expand": ["entitlements"],  // Entitlements will inherit plan sorting
  "filters": [...],  // Existing filter parameters
  "limit": 500,
  "offset": 0
}
```

### Response Format
```json
{
  "items": [
    {
      "id": "plan-id",
      "name": "Plan Name",
      "display_order": 1,
      "entitlements": [
        {
          "id": "entitlement-id",
          "name": "Entitlement Name",
          // Inherits parent plan's display_order
        }
      ]
    }
  ],
  "pagination": {
    "limit": 500,
    "offset": 0,
    "total": 10
  }
}
```

## Rollback Plan
1. Database rollback script to remove display_order field
2. API version control to handle pre-display_order requests
3. Frontend fallback to default sorting

## Security Considerations
1. Validate display_order values on input
2. Ensure proper access controls for order modification
3. Rate limiting for bulk updates

## Monitoring and Alerts
1. Track sorting performance metrics
2. Monitor API response times with sorting
3. Alert on sorting-related errors

## Documentation Requirements
1. Update API documentation
2. Add admin guide for display_order management
3. Update developer documentation
4. Create customer-facing documentation for pricing widget changes
