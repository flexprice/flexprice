# Flexprice • Event Workflow Debugging Tracker

---

## 0. Objectives

| Goal                              | Constraint / KPI                       |
|-----------------------------------|----------------------------------------|
| **Debug unprocessed events**      | Identify exact failure point in event processing workflow |
| **Runtime event analysis**        | Simulate event processing without persisting results |
| **Comprehensive failure tracking** | Track failures at customer, meter, price, and subscription line item levels |
| **Timestamp validation**         | Verify event timestamp falls within subscription line item date ranges |

---

## 1. Overview

The Event Workflow Debugging Tracker provides a comprehensive debugging tool for understanding why events fail to process or produce feature usage records. This feature enables developers and support teams to trace an event through the entire processing pipeline and identify the exact point of failure.

### 1.1 Use Cases

- **Support Debugging**: When a customer reports that an event was not processed, support can use this endpoint to identify the failure point
- **Development Testing**: Developers can test event processing logic without actually processing events
- **Configuration Validation**: Verify that meters, prices, and subscription line items are correctly configured for event processing
- **Troubleshooting**: Identify missing or misconfigured entities in the event processing chain

---

## 2. API Specification

### 2.1 Endpoint

```
GET /events/:id
```

### 2.2 Path Parameters

| Parameter | Type   | Required | Description                    |
|-----------|--------|----------|--------------------------------|
| `id`      | string | Yes      | The event ID to debug          |

### 2.3 Response Structure

#### 2.3.1 Success Response (200 OK)

```json
{
  "event": {
    "id": "event_123",
    "event_name": "api_request",
    "external_customer_id": "customer_456",
    "customer_id": "cust_789",
    "timestamp": "2024-03-20T15:04:05Z",
    "properties": {
      "request_size": 100,
      "response_status": 200
    },
    "source": "api"
  },
  "status": "processed" | "unprocessed" | "failed",
  "processed_events": [
    {
      "subscription_id": "sub_123",
      "sub_line_item_id": "li_456",
      "price_id": "price_789",
      "meter_id": "meter_012",
      "feature_id": "feat_345",
      "qty_total": "100.000000000000000",
      "processed_at": "2024-03-20T15:04:06Z"
    }
  ],
  "debug_tracker": {
    "customer_lookup": {
      "status": "unprocessed" | "found" | "not_found" | "error",
      "customer": {
        "id": "cust_789",
        "external_customer_id": "customer_456"
      },
      "error": {
        "success": false,
        "error": {
          "message": "error message for display",
          "internal_error": "detailed error message"
        }
      }
    },
    "meter_matching": {
      "status": "unprocessed" | "found" | "not_found" | "error",
      "matched_meters": [
        {
          "meter_id": "meter_012",
          "event_name": "api_request",
          "meter": {
            "id": "meter_012",
            "event_name": "api_request",
            "filters": [
              {
                "key": "response_status",
                "values": ["200", "201"]
              }
            ]
          }
        }
      ],
      "error": {
        "success": false,
        "error": {
          "message": "error message for display",
          "internal_error": "detailed error message"
        }
      }
    },
    "price_lookup": {
      "status": "unprocessed" | "found" | "not_found" | "error",
      "matched_prices": [
        {
          "price_id": "price_789",
          "meter_id": "meter_012",
          "status": "published",
          "price": {
            "id": "price_789",
            "meter_id": "meter_012"
          }
        }
      ],
      "error": {
        "success": false,
        "error": {
          "message": "error message for display",
          "internal_error": "detailed error message"
        }
      }
    },
    "subscription_line_item_lookup": {
      "status": "unprocessed" | "found" | "not_found" | "error",
      "matched_line_items": [
        {
          "sub_line_item_id": "li_456",
          "subscription_id": "sub_123",
          "price_id": "price_789",
          "start_date": "2024-03-01T00:00:00Z",
          "end_date": "2024-03-31T23:59:59Z",
          "is_active_for_event": true,
          "timestamp_within_range": true,
          "subscription_line_item": {
            "id": "li_456",
            "subscription_id": "sub_123",
            "price_id": "price_789"
          }
        }
      ],
      "error": {
        "success": false,
        "error": {
          "message": "error message for display",
          "internal_error": "detailed error message"
        }
      }
    },
    "failure_point": {
      "failure_point_type": "customer_lookup" | "meter_lookup" | "price_lookup" | "subscription_line_item_lookup" | null,
      "error": {
        "success": false,
        "error": {
          "message": "error message for display",
          "internal_error": "detailed error message"
        }
      }
    }
  }
}
```

#### 2.3.2 Error Response (404 Not Found)

```json
{
  "error": "Event not found",
  "hint": "The event with the specified ID does not exist"
}
```

#### 2.3.3 Error Response (500 Internal Server Error)

```json
{
  "error": "Internal server error",
  "hint": "An error occurred while processing the request"
}
```

---

## 3. Implementation Details

### 3.1 Workflow

The endpoint follows this workflow:

1. **Retrieve Event**: Fetch the event by ID from the events table
2. **Check Feature Usage**: Query `feature_usage` table for processed events with matching event ID
3. **Process Event**:
   - **If processed events found**: Return the processed events directly
   - **If no processed events found**: 
     - Call `prepareProcessedEvents()` to simulate event processing at runtime
     - If `prepareProcessedEvents()` returns empty results, initiate the debug tracker
4. **Debug Tracker**: If event processing fails, track each step of the workflow:

#### 3.1.1 Step 1: Customer Lookup

- **Action**: Lookup customer by `external_customer_id` (lookup key)
- **Success Criteria**: Customer found in database
- **Failure Cases**:
  - Customer not found → Failure point: `customer_lookup`, Status: `not_found`, Error: `null`
  - Database/repository error → Failure point: `customer_lookup`, Status: `error`, Error: actual error from repository call

#### 3.1.2 Step 2: Meter Matching

- **Action**: Check meter availability against `event_name` and `properties`
- **Matching Logic**:
  - Match meters where `meter.event_name == event.event_name`
  - Match meters where all meter filters are satisfied by event properties
    - For each filter in `meter.filters`:
      - Check if `event.properties[filter.key]` exists
      - Check if `event.properties[filter.key]` value is in `filter.values`
- **Success Criteria**: At least one meter matches
- **Failure Cases**:
  - No meters found matching event_name → Failure point: `meter_lookup`, Status: `not_found`, Error: `null`
  - No meters found matching properties → Failure point: `meter_lookup`, Status: `not_found`, Error: `null`
  - Database/repository error → Failure point: `meter_lookup`, Status: `error`, Error: actual error from repository call

#### 3.1.3 Step 3: Price Lookup

- **Action**: Check for prices associated with the matched meter IDs
- **Lookup Logic**:
  - Query prices where `price.meter_id IN (matched_meter_ids)`
  - Filter for published prices (`status == 'published'`)
  - Filter for usage-based prices (`is_usage == true`)
- **Success Criteria**: At least one price found for at least one matched meter
- **Failure Cases**:
  - No prices found for any meter → Failure point: `price_lookup`, Status: `not_found`, Error: `null`
  - Database/repository error → Failure point: `price_lookup`, Status: `error`, Error: actual error from repository call

#### 3.1.4 Step 4: Subscription Line Item Lookup

- **Action**: Check for subscription line items associated with the matched price IDs
- **Lookup Logic**:
  - Query subscription line items where `line_item.price_id IN (matched_price_ids)`
  - Filter for active subscriptions (`status IN ('active', 'trialing')`)
  - Filter for usage-based line items (`is_usage == true`)
  - Check if line item is active for event timestamp:
    - `line_item.start_date < event.timestamp < line_item.end_date` (strict inequalities)
    - If `line_item.end_date` is zero/null, treat as open-ended (no upper bound)
- **Success Criteria**: At least one line item found and active for event timestamp
- **Failure Cases**:
  - No line items found for any price → Failure point: `subscription_line_item`, Status: `not_found`, Error: "No subscription line items found for matched prices"
  - Line items found but none are active for event timestamp → Failure point: `subscription_line_item`, Status: `not_found`, Error: "No active subscription line items found for event timestamp"
  - Database error → Status: `error`, Error: actual error from database/repository call

### 3.2 Status Initialization

All debug tracker steps are initialized with status `"unprocessed"` to indicate they haven't been checked yet. When a step is executed:
- If the step succeeds → Status changes to `"found"`
- If the step fails with an error → Status changes to `"error"` and error details are populated
- If the step succeeds but returns no results → Status changes to `"not_found"`
- If a previous step fails and returns early → Subsequent steps remain `"unprocessed"`

This allows the response to clearly show which steps were checked and which were skipped due to early failures.

### 3.3 Data Structures

#### 3.2.1 Debug Tracker Response

```go
type EventDebugTrackerResponse struct {
    Event           *events.Event              `json:"event"`
    Status          string                     `json:"status"` // "processed", "unprocessed", "failed"
    ProcessedEvents []*events.FeatureUsage     `json:"processed_events,omitempty"`
    DebugTracker    *DebugTracker              `json:"debug_tracker,omitempty"`
}

type DebugTracker struct {
    CustomerLookup             *CustomerLookupResult             `json:"customer_lookup"`
    MeterMatching              *MeterMatchingResult              `json:"meter_matching"`
    PriceLookup                *PriceLookupResult                `json:"price_lookup"`
    SubscriptionLineItemLookup *SubscriptionLineItemLookupResult `json:"subscription_line_item_lookup"`
    FailurePoint               *FailurePoint                     `json:"failure_point,omitempty"` // null if no failure
}

type FailurePoint struct {
    FailurePointType FailurePointType    `json:"failure_point_type"`
    Error            *ErrorResponse      `json:"error,omitempty"`
}

type FailurePointType string

const (
    FailurePointTypeCustomerLookup             FailurePointType = "customer_lookup"
    FailurePointTypeMeterLookup                FailurePointType = "meter_lookup"
    FailurePointTypePriceLookup                FailurePointType = "price_lookup"
    FailurePointTypeSubscriptionLineItemLookup FailurePointType = "subscription_line_item_lookup"
)

type DebugTrackerStatus string

const (
    DebugTrackerStatusUnprocessed DebugTrackerStatus = "unprocessed"
    DebugTrackerStatusNotFound    DebugTrackerStatus = "not_found"
    DebugTrackerStatusFound       DebugTrackerStatus = "found"
    DebugTrackerStatusError       DebugTrackerStatus = "error"
)

type CustomerLookupResult struct {
    Status   DebugTrackerStatus `json:"status"` // "unprocessed", "found", "not_found", "error"
    Customer *Customer          `json:"customer,omitempty"`
    Error    *ErrorResponse     `json:"error,omitempty"`
}

type MeterMatchingResult struct {
    Status        DebugTrackerStatus `json:"status"` // "unprocessed", "found", "not_found", "error"
    MatchedMeters []MatchedMeter     `json:"matched_meters,omitempty"`
    Error         *ErrorResponse     `json:"error,omitempty"`
}

type MatchedMeter struct {
    MeterID   string       `json:"meter_id"`
    EventName string       `json:"event_name"`
    Meter     *meter.Meter `json:"meter"`
    Filters   []map[string]interface{} `json:"filters"`
}

type PriceLookupResult struct {
    Status        DebugTrackerStatus `json:"status"` // "unprocessed", "found", "not_found", "error"
    MatchedPrices []MatchedPrice     `json:"matched_prices,omitempty"`
    Error         *ErrorResponse      `json:"error,omitempty"`
}

type MatchedPrice struct {
    PriceID string      `json:"price_id"`
    MeterID string      `json:"meter_id"`
    Status  string      `json:"status"`
    Price   *price.Price `json:"price,omitempty"`
}

type SubscriptionLineItemLookupResult struct {
    Status           DebugTrackerStatus           `json:"status"` // "unprocessed", "found", "not_found", "error"
    MatchedLineItems []MatchedSubscriptionLineItem `json:"matched_line_items,omitempty"`
    Error            *ErrorResponse              `json:"error,omitempty"`
}

type MatchedSubscriptionLineItem struct {
    SubLineItemID        string                    `json:"sub_line_item_id"`
    SubscriptionID       string                    `json:"subscription_id"`
    PriceID              string                    `json:"price_id"`
    StartDate            time.Time                 `json:"start_date"`
    EndDate              time.Time                 `json:"end_date"`
    IsActiveForEvent     bool                      `json:"is_active_for_event"`
    TimestampWithinRange bool                      `json:"timestamp_within_range"`
    SubscriptionLineItem *subscription.LineItem    `json:"subscription_line_item,omitempty"`
}

type ErrorResponse struct {
    Success bool        `json:"success"`
    Error   ErrorDetail `json:"error"`
}

type ErrorDetail struct {
    Display       string         `json:"message"`
    InternalError string         `json:"internal_error,omitempty"`
    Details       map[string]any `json:"details,omitempty"`
}
```

### 3.4 Service Method

The implementation should leverage the existing `prepareProcessedEvents` method from `featureUsageTrackingService`:

```go
func (s *featureUsageTrackingService) prepareProcessedEvents(
    ctx context.Context, 
    event *events.Event,
) ([]*events.FeatureUsage, error)
```

This method already implements the core processing logic:
- Customer lookup
- Subscription retrieval
- Price and meter matching
- Feature matching
- Event processing against subscriptions

### 3.5 Repository Methods

The following repository methods will be needed:

1. **Event Repository**:
   - `GetEventByID(ctx context.Context, eventID string) (*events.Event, error)`

2. **Feature Usage Repository**:
   - `GetFeatureUsageByEventIDs(ctx context.Context, eventIDs []string) ([]*events.FeatureUsage, error)` (already exists)

3. **Customer Repository**:
   - `GetByLookupKey(ctx context.Context, lookupKey string) (*customer.Customer, error)` (already exists)

4. **Meter Repository**:
   - `List(ctx context.Context, filter *types.MeterFilter) ([]*meter.Meter, error)` (already exists)

5. **Price Repository**:
   - `List(ctx context.Context, filter *types.PriceFilter) ([]*price.Price, error)` (already exists)

6. **Subscription Repository**:
   - `ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.SubscriptionListResponse, error)` (already exists)

---

## 4. Error Handling

### 4.1 Event Not Found

- **HTTP Status**: 404 Not Found
- **Response**: Error message indicating the event ID does not exist

### 4.2 Database Errors

- **HTTP Status**: 500 Internal Server Error
- **Response**: Generic error message (do not expose internal database errors)
- **Logging**: Log full error details server-side for debugging

### 4.3 Processing Errors

- **HTTP Status**: 200 OK (with `status: "failed"` in response)
- **Response**: Include detailed debug tracker information showing where processing failed

### 4.4 Error vs Not Found

- **Error Status**: Used when a repository/service call returns an actual error (e.g., database connection error, query error). The `error` field will contain the actual error details.
- **Not Found Status**: Used when a lookup succeeds but returns no results (e.g., customer exists but not found, meters exist but none match). The `error` field will be `null` or omitted.
- **Unprocessed Status**: Used for steps that were never checked due to early returns from previous steps. The `error` field will be `null` or omitted.
- **Found Status**: Used when a lookup succeeds and returns results. The corresponding data field (e.g., `customer`, `matched_meters`) will be populated.

---

## 5. Performance Considerations

### 5.1 Caching

- Consider caching meter and price lookups for frequently debugged events
- Cache TTL: 5 minutes

### 5.2 Query Optimization

- Use bulk queries where possible (e.g., fetch all meters at once, then filter in memory)
- Use indexes on:
  - `feature_usage.id` (for processed event lookup)
  - `customer.external_customer_id` (for customer lookup)
  - `meter.event_name` (for meter matching)
  - `price.meter_id` (for price lookup)
  - `subscription_line_item.price_id` (for line item lookup)

### 5.3 Timeout

- Set a reasonable timeout for the debug tracker (e.g., 30 seconds)
- If timeout is exceeded, return partial results with a timeout indicator

---

## 6. Security Considerations

### 6.1 Authentication

- Require API key authentication (same as other event endpoints)
- Validate tenant ID from context

### 6.2 Authorization

- Ensure users can only access events for their tenant
- Validate `event.tenant_id` matches `context.tenant_id`

### 6.3 Data Exposure

- Do not expose sensitive customer data beyond what's necessary for debugging
- Sanitize error messages to avoid exposing internal system details

---

## 7. Testing Considerations

### 7.1 Test Cases

1. **Event with processed feature usage**:
   - Event exists in `feature_usage` table
   - Should return processed events directly

2. **Event without processed feature usage (successful processing)**:
   - Event exists but not in `feature_usage`
   - `prepareProcessedEvents()` returns results
   - Should return processed events from runtime processing

3. **Event without processed feature usage (customer not found)**:
   - Customer lookup fails
   - Should return debug tracker with `failure_point: "customer"`

4. **Event without processed feature usage (meter not found)**:
   - Customer found
   - No meters match event_name or properties
   - Should return debug tracker with `failure_point: "meter"`

5. **Event without processed feature usage (price not found)**:
   - Customer found
   - Meters found
   - No prices found for meters
   - Should return debug tracker with `failure_point: "price"`

6. **Event without processed feature usage (line item not found)**:
   - Customer found
   - Meters found
   - Prices found
   - No subscription line items found
   - Should return debug tracker with `failure_point: "subscription_line_item"`

7. **Event without processed feature usage (line item not active)**:
   - Customer found
   - Meters found
   - Prices found
   - Line items found but event timestamp outside date range
   - Should return debug tracker with `failure_point: "subscription_line_item"` and `timestamp_within_range: false`

8. **Event not found**:
   - Event ID does not exist
   - Should return 404

---

## 8. Future Enhancements

### 8.1 Batch Debugging

- Support debugging multiple events at once
- Endpoint: `POST /events/debug` with array of event IDs

### 8.2 Historical Analysis

- Track debug results over time
- Identify patterns in event processing failures

### 8.3 Auto-fix Suggestions

- Provide suggestions for fixing common issues (e.g., "Create customer", "Create meter", "Create price")

### 8.4 Visualization

- Provide a visual flow diagram showing the event processing path
- Highlight failure points in red

---

## 9. Implementation Notes

### 9.1 Method Reuse

The implementation should reuse the existing `prepareProcessedEvents` method from `featureUsageTrackingService`. This ensures consistency between actual event processing and debugging.

### 9.2 Non-Persistent Processing

When calling `prepareProcessedEvents` for debugging purposes, ensure that:
- No feature usage records are persisted to the database
- No side effects occur (e.g., customer auto-creation)
- Results are returned in-memory only

### 9.3 Debug Tracker Execution

The debug tracker should only execute if:
1. No processed events are found in `feature_usage` table
2. `prepareProcessedEvents()` returns empty results

This ensures the debug tracker only runs when necessary and provides additional context beyond what `prepareProcessedEvents` already provides.

---

## 10. API Examples

### 10.1 Successful Event (Already Processed)

**Request:**
```
GET /events/event_123
```

**Response:**
```json
{
  "event": {
    "id": "event_123",
    "event_name": "api_request",
    "external_customer_id": "customer_456",
    "customer_id": "cust_789",
    "timestamp": "2024-03-20T15:04:05Z",
    "properties": {
      "request_size": 100
    }
  },
  "status": "processed",
  "processed_events": [
    {
      "subscription_id": "sub_123",
      "sub_line_item_id": "li_456",
      "price_id": "price_789",
      "meter_id": "meter_012",
      "feature_id": "feat_345",
      "qty_total": "100.000000000000000"
    }
  ]
}
```

### 10.2 Failed Event (Customer Not Found)

**Request:**
```
GET /events/event_456
```

**Response:**
```json
{
  "event": {
    "id": "event_456",
    "event_name": "api_request",
    "external_customer_id": "customer_999",
    "timestamp": "2024-03-20T15:04:05Z",
    "properties": {}
  },
  "status": "failed",
  "debug_tracker": {
    "customer_lookup": {
      "status": "not_found"
    },
    "meter_matching": {
      "status": "unprocessed"
    },
    "price_lookup": {
      "status": "unprocessed"
    },
    "subscription_line_item_lookup": {
      "status": "unprocessed"
    },
    "failure_point": {
      "failure_point_type": "customer_lookup"
    }
  }
}
```

### 10.3 Failed Event (Meter Not Found)

**Request:**
```
GET /events/event_789
```

**Response:**
```json
{
  "event": {
    "id": "event_789",
    "event_name": "unknown_event",
    "external_customer_id": "customer_456",
    "customer_id": "cust_789",
    "timestamp": "2024-03-20T15:04:05Z",
    "properties": {}
  },
  "status": "failed",
  "debug_tracker": {
    "customer_lookup": {
      "status": "found",
      "customer": {
        "id": "cust_789",
        "external_customer_id": "customer_456"
      }
    },
    "meter_matching": {
      "status": "not_found"
    },
    "price_lookup": {
      "status": "unprocessed"
    },
    "subscription_line_item_lookup": {
      "status": "unprocessed"
    },
    "failure_point": {
      "failure_point_type": "meter_lookup"
    }
  }
}
```

### 10.4 Failed Event (Database Error)

**Request:**
```
GET /events/event_error
```

**Response:**
```json
{
  "event": {
    "id": "event_error",
    "event_name": "api_request",
    "external_customer_id": "customer_456",
    "timestamp": "2024-03-20T15:04:05Z",
    "properties": {}
  },
  "status": "failed",
  "debug_tracker": {
    "customer_lookup": {
      "status": "found",
      "customer": {
        "id": "cust_789",
        "external_customer_id": "customer_456"
      }
    },
    "meter_matching": {
      "status": "error",
      "error": {
        "success": false,
        "error": {
          "message": "database connection error",
          "internal_error": "database connection error"
        }
      }
    },
    "price_lookup": {
      "status": "unprocessed"
    },
    "subscription_line_item_lookup": {
      "status": "unprocessed"
    },
    "failure_point": {
      "failure_point_type": "meter_lookup",
      "error": {
        "success": false,
        "error": {
          "message": "database connection error",
          "internal_error": "database connection error"
        }
      }
    }
  }
}
```

---

## 11. Acceptance Criteria

- [ ] API endpoint `GET /events/:id` is implemented
- [ ] Endpoint returns processed events if found in `feature_usage` table
- [ ] Endpoint calls `prepareProcessedEvents()` if no processed events found
- [ ] Debug tracker executes if `prepareProcessedEvents()` returns empty
- [ ] Debug tracker tracks customer lookup with appropriate status (`unprocessed`, `found`, `not_found`, `error`)
- [ ] Debug tracker tracks meter matching with matched meters and filters
- [ ] Debug tracker tracks price lookup with matched prices
- [ ] Debug tracker tracks subscription line item lookup with date range validation (strict: `start_date < timestamp < end_date`)
- [ ] Response includes `failure_point` object with `failure_point_type` and `error` fields
- [ ] Errors from repository/service calls are properly captured and returned in `error` field
- [ ] Steps that weren't checked show `unprocessed` status
- [ ] Steps that were checked but found nothing show `not_found` status
- [ ] Steps that encountered errors show `error` status with actual error details
- [ ] All error cases return appropriate HTTP status codes
- [ ] Tenant isolation is enforced
- [ ] API key authentication is required
- [ ] Performance is acceptable (< 2 seconds for typical queries)
- [ ] Comprehensive test coverage for all failure scenarios

---

## 12. References

- `internal/ee/service/feature_usage_tracking.go` - `prepareProcessedEvents` method
- `internal/domain/events/model.go` - Event and FeatureUsage models
- `internal/api/v1/events.go` - Existing event handlers
- `internal/repository/clickhouse/feature_usage.go` - Feature usage repository methods
