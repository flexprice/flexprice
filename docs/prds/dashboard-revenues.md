# Dashboard Revenues API - Product Requirements Document

## Overview

This document defines the requirements for a unified dashboard API endpoint that aggregates three key dashboard sections into a single API call:
1. **Revenue Trend** - Monthly revenue data from paid invoices
2. **Recent Subscriptions** - Count of subscriptions created in the last 7 days, grouped by plan
3. **Invoice Payment Status** - Total counts of invoices by payment status (paid, pending, failed)

The API consolidates these three independent data queries into a single endpoint to reduce network overhead and improve dashboard load performance.

## API Endpoint

**Endpoint:** `POST /v1/dashboard/revenues`

**Authentication:** Required (uses existing authentication middleware)

**Route Group:** `/v1/dashboard` (public routes group)

## Request Structure

### Request Body

```json
{
  "revenue_trend": {
    "window_size": "MONTH",
    "window_count": 3
  },
  "recent_subscriptions": {
    "enabled": true
  },
  "invoice_payment_status": {
    "enabled": true
  }
}
```

### Request Parameters

#### Revenue Trend Parameters

- **window_size** (string, optional, default: "MONTH")
  - Supported values: `MONTH`, `WEEK`, `DAY`, `HOUR`, `3HOUR`, `6HOUR`, `12HOUR`, `30MIN`, `15MIN`, `MINUTE`
  - Defines the time window for aggregating revenue data
  - For Revenue Trend section, typically `MONTH` is used
  - Must be a valid `WindowSize` enum value

- **window_count** (integer, optional, default: 3)
  - Number of time windows to return
  - Must be a positive integer (>= 1)
  - Default value is 3 (e.g., current month, previous month, month before that)
  - Can be set to any number based on frontend requirements

#### Recent Subscriptions Parameters

- **enabled** (boolean, optional, default: true)
  - Whether to include recent subscriptions data in the response
  - If `false`, the section will be omitted from the response

#### Invoice Payment Status Parameters

- **enabled** (boolean, optional, default: true)
  - Whether to include invoice payment status data in the response
  - If `false`, the section will be omitted from the response

### Request Validation

- `window_size` must be a valid `WindowSize` enum value
- `window_count` must be >= 1
- All optional fields can be omitted (will use defaults)

## Response Structure

### Success Response (200 OK)

```json
{
  "revenue_trend": {
    "windows": [
      {
        "window_start": "2026-01-01T00:00:00Z",
        "window_end": "2026-01-31T23:59:59Z",
        "window_label": "Jan 2026",
        "total_revenue": "3166085.00"
      },
      {
        "window_start": "2025-12-01T00:00:00Z",
        "window_end": "2025-12-31T23:59:59Z",
        "window_label": "Dec 2025",
        "total_revenue": "0.00"
      },
      {
        "window_start": "2025-11-01T00:00:00Z",
        "window_end": "2025-11-30T23:59:59Z",
        "window_label": "Nov 2025",
        "total_revenue": "0.00"
      }
    ],
    "window_size": "MONTH",
    "window_count": 3
  },
  "recent_subscriptions": {
    "total_count": 7,
    "by_plan": [
      {
        "plan_id": "plan_123",
        "plan_name": "Sixth Plan",
        "count": 2
      },
      {
        "plan_id": "plan_456",
        "plan_name": "Third Plan",
        "count": 2
      },
      {
        "plan_id": "plan_789",
        "plan_name": "One Plan",
        "count": 1
      },
      {
        "plan_id": "plan_101",
        "plan_name": "Fourth Plan",
        "count": 1
      },
      {
        "plan_id": "plan_202",
        "plan_name": "Second Plan",
        "count": 1
      }
    ],
    "period_start": "2026-01-08T00:00:00Z",
    "period_end": "2026-01-15T23:59:59Z"
  },
  "invoice_payment_status": {
    "paid": 0,
    "pending": 1,
    "failed": 0,
    "period_start": "2026-01-08T00:00:00Z",
    "period_end": "2026-01-15T23:59:59Z"
  }
}
```

### Response Fields

#### Revenue Trend Section

- **windows** (array, required)
  - Array of revenue data for each time window
  - Ordered from most recent to oldest (current window first)
  - Each window object contains:
    - **window_start** (string, ISO 8601 timestamp): Start of the time window
    - **window_end** (string, ISO 8601 timestamp): End of the time window
    - **window_label** (string): Human-readable label for the window (e.g., "Jan 2026", "Dec 2025")
    - **total_revenue** (string, decimal): Sum of all paid invoice totals for this window

- **window_size** (string): The window size used for aggregation
- **window_count** (integer): The number of windows returned

#### Recent Subscriptions Section

- **total_count** (integer): Total number of subscriptions created in the last 7 days
- **by_plan** (array): Array of subscription counts grouped by plan
  - Each plan object contains:
    - **plan_id** (string): Internal plan ID
    - **plan_name** (string): Display name of the plan
    - **count** (integer): Number of subscriptions for this plan
- **period_start** (string, ISO 8601 timestamp): Start of the 7-day period (T-7 days from now)
- **period_end** (string, ISO 8601 timestamp): End of the 7-day period (current time)

#### Invoice Payment Status Section

- **paid** (integer): Count of invoices with payment status `SUCCEEDED` or `OVERPAID`
- **pending** (integer): Count of invoices with payment status `PENDING`, `PROCESSING`, or `INITIATED`
- **failed** (integer): Count of invoices with payment status `FAILED`
- **period_start** (string, ISO 8601 timestamp): Start of the 7-day period (T-7 days from now)
- **period_end** (string, ISO 8601 timestamp): End of the 7-day period (current time)

## Business Logic

### Revenue Trend

1. **Window Calculation**
   - Calculate time windows based on `window_size` and `window_count`
   - For `MONTH` window size:
     - Current window: Current month (1st day 00:00:00 to last day 23:59:59)
     - Previous windows: Go back `window_count - 1` months
     - Use calendar months (1st to last day of month)
   - Windows are ordered from most recent to oldest

2. **Revenue Aggregation**
   - For each time window, query invoices where:
     - `payment_status` IN (`SUCCEEDED`, `OVERPAID`)
     - `invoice_status` = `FINALIZED` (only finalized invoices count)
     - Invoice period overlaps with the time window
     - Consider invoices where `period_start` or `period_end` falls within the window, or where the invoice period fully contains the window
   - Sum the `amount_due` (or `amount_paid` for paid invoices) for all matching invoices
   - Return the total as a decimal string

3. **Window Labeling**
   - For `MONTH` windows: Format as "MMM YYYY" (e.g., "Jan 2026", "Dec 2025")
   - For other window sizes: Format appropriately (e.g., "Week of Jan 1", "2026-01-15")

### Recent Subscriptions

1. **Time Period**
   - Calculate period: `T-7 days` to `now` (current time)
   - Use `created_at` field on subscriptions table

2. **Query Logic**
   - Filter subscriptions where:
     - `created_at >= T-7 days`
     - `created_at <= now`
     - Only count subscriptions with status `ACTIVE` or `DRAFT` (exclude cancelled/ended)
   - Group by `plan_id`
   - Count subscriptions per plan

3. **Plan Information**
   - For each unique `plan_id`, fetch plan details to get `plan_name`
   - If plan is deleted or not found, use `plan_id` as the name or mark as "Unknown Plan"

4. **Response Format**
   - Return total count across all plans
   - Return array of plan groups with plan_id, plan_name, and count
   - Order by count descending (most subscriptions first)

### Invoice Payment Status

1. **Time Period**
   - Calculate period: `T-7 days` to `now` (current time)
   - Filter invoices created or finalized within this period

2. **Payment Status Mapping**
   - **Paid**: Count invoices where `payment_status` IN (`SUCCEEDED`, `OVERPAID`)
   - **Pending**: Count invoices where `payment_status` IN (`PENDING`, `PROCESSING`, `INITIATED`)
   - **Failed**: Count invoices where `payment_status` = `FAILED`

3. **Invoice Status Filter**
   - Only count invoices with `invoice_status` = `FINALIZED`
   - Exclude `DRAFT` and `VOIDED` invoices

4. **Query Logic**
   - Query invoices where:
     - `created_at >= T-7 days` OR `finalized_at >= T-7 days` (if finalized_at exists)
     - `invoice_status` = `FINALIZED`
   - Group by `payment_status` and count

## Error Handling

### Validation Errors (400 Bad Request)

- Invalid `window_size` value
- Invalid `window_count` (must be >= 1)
- Malformed request body

### Server Errors (500 Internal Server Error)

- Database query failures
- Service layer errors
- Unexpected errors during aggregation

### Partial Data Handling

- If one section fails, the API should still return data for other sections
- Failed sections should include an error message in the response
- Example response with partial failure:

```json
{
  "revenue_trend": {
    "windows": [...],
    "window_size": "MONTH",
    "window_count": 3
  },
  "recent_subscriptions": {
    "error": "Failed to fetch subscriptions: database connection timeout"
  },
  "invoice_payment_status": {
    "paid": 0,
    "pending": 1,
    "failed": 0
  }
}
```

## Edge Cases

### Revenue Trend

1. **No Invoices in Window**
   - Return `total_revenue: "0.00"` for that window
   - Still include the window in the response with zero value

2. **Partial Month Windows**
   - For current month, include all days up to current date
   - For future months, return empty or zero

3. **Multiple Window Sizes**
   - Support all window sizes defined in `WindowSize` enum
   - Ensure proper time boundary calculations for each window type

### Recent Subscriptions

1. **No Subscriptions in Last 7 Days**
   - Return `total_count: 0` and empty `by_plan` array

2. **Deleted Plans**
   - If a plan is deleted but subscriptions reference it, use plan_id or "Unknown Plan" as name
   - Still include the count in the response

3. **Subscriptions Without Plans**
   - If `plan_id` is null/empty, group under "No Plan" or exclude from grouping

### Invoice Payment Status

1. **No Invoices in Period**
   - Return all counts as 0

2. **Invoices with Multiple Payment Statuses**
   - Use the current `payment_status` field value
   - Don't count the same invoice multiple times

3. **Time Zone Considerations**
   - Use UTC for all time calculations
   - Ensure consistent time boundaries across all queries

## Implementation Notes

### Database Queries

1. **Revenue Trend Query**
   - Use aggregation query to sum invoice amounts grouped by time window
   - Filter by payment_status and invoice_status
   - Consider using database date functions for window calculations

2. **Recent Subscriptions Query**
   - Use GROUP BY on plan_id with COUNT
   - Join with plans table to get plan names
   - Filter by created_at date range

3. **Invoice Payment Status Query**
   - Use conditional aggregation (COUNT with CASE/IF) to count by payment_status
   - Single query can return all three counts

### Performance Considerations

- All three queries should execute sequentially (no goroutines as per requirements)
- Consider database indexes on:
  - `invoices.payment_status`
  - `invoices.invoice_status`
  - `invoices.created_at` / `invoices.finalized_at`
  - `subscriptions.created_at`
  - `subscriptions.plan_id`

### Caching Strategy (Future Consideration)

- Consider caching dashboard data for short periods (e.g., 1-5 minutes)
- Cache invalidation on invoice/subscription updates
- Not required for initial implementation

## Testing Requirements

### Unit Tests

- Test window calculation logic for different window sizes
- Test revenue aggregation with various invoice scenarios
- Test subscription grouping and counting
- Test payment status counting logic

### Integration Tests

- Test full API endpoint with all three sections
- Test with various window_count values
- Test with empty data sets
- Test error handling and partial failures

### Edge Case Tests

- Test with no invoices/subscriptions
- Test with deleted plans
- Test with different time zones
- Test with boundary dates (month boundaries, etc.)

## Future Enhancements (Out of Scope)

- Support for custom date ranges (beyond T-7 days)
- Support for filtering by customer, subscription, etc.
- Real-time updates via WebSocket
- Caching layer for improved performance
- Parallel execution with goroutines (explicitly excluded per requirements)
