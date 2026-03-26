# Usage Analytics — Multi-Customer ID Support

**Date:** 2026-03-24
**Status:** Approved

---

## Overview

`GetUsageAnalyticsRequest` currently accepts a single `ExternalCustomerID` (required). This spec adds `ExternalCustomerIDs []string` so callers can request a single merged/aggregated analytics response across multiple customers. The `IncludeChildren` field is simultaneously simplified: children are resolved once upfront and folded into the same flat customer list, eliminating the separate `buildAggregatedAnalyticsWithChildren` code path.

The response is always a **single aggregated total**. No per-customer breakdown is ever returned.

---

## Service Variants Affected

The codebase has three analytics service methods. Each is treated separately:

| Method | Service | Change |
|--------|---------|--------|
| `GetDetailedUsageAnalytics` | `featureUsageTrackingService` (V2 primary) | Full refactor — main target of this spec |
| `GetDetailedUsageAnalyticsV2` | `featureUsageTrackingService` (V2 variant) | `fetchCustomers` updated to respect `ExternalCustomerIDs` |
| `GetDetailedUsageAnalytics` | `eventPostProcessingService` (V1 legacy) | One-line normalisation only, no aggregation support |

---

## Requirements

1. Accept `ExternalCustomerIDs []string` alongside the existing `ExternalCustomerID string`.
2. Union semantics: if both fields are set, merge and deduplicate — no error.
3. At least one of the two fields must be non-empty — validation error otherwise.
4. Response is always a single aggregated total; no per-customer breakdown.
5. `IncludeChildren = true` expands the resolved customer list with each customer's hierarchy children before the fetch loop. Children are resolved exactly once — no recursion, no appending to a slice while iterating over it.
6. Currency mismatch across customers is a hard validation error (consistent with existing `GetDetailedUsageAnalyticsV2` behaviour).
7. If every customer in the resolved list fails to fetch, return an empty `GetUsageAnalyticsResponse` (no panic).
8. V1 legacy path does not support multi-customer aggregation; it silently uses the first resolved ID.
9. All existing callers (invoice service, customer portal, revenue analytics) require no changes.

---

## Files Changed

| File | Change |
|------|--------|
| `internal/api/dto/events.go` | Add `ExternalCustomerIDs`, drop `binding:"required"` from `ExternalCustomerID` |
| `internal/service/feature_usage_tracking.go` | Add `resolveEffectiveExternalIDs`, update `validateAnalyticsRequest`, refactor `GetDetailedUsageAnalytics`, update `fetchCustomers`, delete `buildAggregatedAnalyticsWithChildren` |
| `internal/service/event_post_processing.go` | One-line normalisation at top of V1's `GetDetailedUsageAnalytics` |

---

## DTO Changes (`internal/api/dto/events.go`)

This endpoint is `POST /events/analytics` and binds via `ShouldBindJSON`. Only `json:` tags are needed; `form:` tags are not required.

```go
type GetUsageAnalyticsRequest struct {
    // ExternalCustomerID is the single external customer ID.
    // Optional when ExternalCustomerIDs is provided; required otherwise.
    ExternalCustomerID string `json:"external_customer_id"`

    // ExternalCustomerIDs is a list of external customer IDs whose usage will be
    // merged into a single aggregated response. Unioned with ExternalCustomerID
    // if both are set; duplicates are dropped.
    ExternalCustomerIDs []string `json:"external_customer_ids,omitempty"`

    // ... all other fields unchanged ...
}
```

`binding:"required"` is removed from `ExternalCustomerID`. Validation moves entirely to `validateAnalyticsRequest` in the service layer.

---

## Shared Helper (`feature_usage_tracking.go`)

```go
// resolveEffectiveExternalIDs returns a deduplicated, ordered union of
// req.ExternalCustomerID and req.ExternalCustomerIDs. Empty strings are dropped.
// This is a cheap, allocation-light helper intentionally called in more than one place.
func resolveEffectiveExternalIDs(req *dto.GetUsageAnalyticsRequest) []string
```

Called by both `validateAnalyticsRequest` (to check at-least-one) and `GetDetailedUsageAnalytics` (to drive the fetch loop). Two calls to the same cheap helper is intentional and cleaner than threading the resolved list through validation.

---

## Validation

### `validateAnalyticsRequest` (used by `GetDetailedUsageAnalytics` V2 primary)

Replace the existing `ExternalCustomerID == ""` check:

```go
func (s *featureUsageTrackingService) validateAnalyticsRequest(req *dto.GetUsageAnalyticsRequest) error {
    if len(resolveEffectiveExternalIDs(req)) == 0 {
        return ierr.NewError("external_customer_id or external_customer_ids is required").
            WithHint("Provide at least one external customer ID").
            Mark(ierr.ErrValidation)
    }
    if req.WindowSize != "" {
        return req.WindowSize.Validate()
    }
    return nil
}
```

### `validateAnalyticsRequestV2` (used by `GetDetailedUsageAnalyticsV2`)

**No change.** This function intentionally does not require any customer ID because `GetDetailedUsageAnalyticsV2` supports "no ID = all customers". It only validates `WindowSize`.

---

## `GetDetailedUsageAnalytics` Refactor (V2 primary, `feature_usage_tracking.go`)

### New flow

```
1. validateAnalyticsRequest(req)
   → error if no IDs provided

2. effectiveIDs = resolveEffectiveExternalIDs(req)
   → deduplicated union of both fields

3. if req.IncludeChildren:
       childExternalIDs = []
       for each id in effectiveIDs:          // safe: iterating original slice, not appending to it
           customer = fetchCustomer(id)      // error → return error
           children = fetchChildCustomers(customer.ID)  // error → log + continue
           for each child: append child.ExternalID to childExternalIDs
       effectiveIDs = dedup(effectiveIDs + childExternalIDs)
   // Children resolved exactly once, before the fetch loop starts

4. var aggregatedData *AnalyticsData
   var allAnalytics []*events.DetailedUsageAnalytic   // accumulated separately; mergeAnalyticsData does NOT touch Analytics

   for each id in effectiveIDs:
       customerReq = shallow copy of req
       customerReq.ExternalCustomerID  = id
       customerReq.ExternalCustomerIDs = nil
       customerReq.IncludeChildren     = false
       data, err = fetchAnalyticsData(ctx, &customerReq)
       if err:
           log warning, continue   // non-fatal per customer
           continue
       allAnalytics = append(allAnalytics, data.Analytics...)   // accumulate every customer's analytics items
       if aggregatedData == nil:
           aggregatedData = data   // first success: use as base for subscriptions, maps, currency
       else:
           if data.Currency != "" && aggregatedData.Currency != "" &&
              data.Currency != aggregatedData.Currency:
               return hard currency mismatch error
           mergeAnalyticsData(aggregatedData, data)  // merges subscriptions, line items,
                                                      // features, meters, prices, plans, addons
                                                      // (does NOT merge Analytics slice — handled above)

5. if aggregatedData == nil:
       return &dto.GetUsageAnalyticsResponse{}, nil   // all customers failed — empty response, no panic

6. aggregatedData.Analytics = allAnalytics   // assign accumulated analytics before building response

7. return buildAnalyticsResponse(ctx, aggregatedData, req)
```

### Deleted function

`buildAggregatedAnalyticsWithChildren` is removed. The loop above replaces it entirely, handling both multi-ID and `IncludeChildren` through one code path.

### Why `mergeAnalyticsData` is called — and what it does not do

`buildAnalyticsResponse` → `calculateCosts` resolves costs via `aggregatedData.SubscriptionLineItems`. Without merging each additional customer's subscription and line item maps into the aggregated base, line items for non-first customers would not be found and their costs would be silently zero. `mergeAnalyticsData` (already used by `GetDetailedUsageAnalyticsV2`) handles this merge for subscriptions, line items, features, meters, prices, plans, addons, and groups.

**Important:** `mergeAnalyticsData` does **not** touch `aggregated.Analytics`. The analytics items from each customer are accumulated separately into `allAnalytics` and assigned to `aggregatedData.Analytics` in step 6, mirroring the pattern in `GetDetailedUsageAnalyticsV2`.

---

## `fetchCustomers` Update (used by `GetDetailedUsageAnalyticsV2`)

```go
func (s *featureUsageTrackingService) fetchCustomers(ctx context.Context, req *dto.GetUsageAnalyticsRequest) ([]*customer.Customer, error) {
    effectiveIDs := resolveEffectiveExternalIDs(req)
    if len(effectiveIDs) > 0 {
        // Fetch only the specified customers
        customers := make([]*customer.Customer, 0, len(effectiveIDs))
        for _, id := range effectiveIDs {
            cust, err := s.fetchCustomer(ctx, id)
            if err != nil {
                return nil, err
            }
            customers = append(customers, cust)
        }
        return customers, nil
    }
    // No IDs specified — fetch all customers (existing behaviour)
    customers, err := s.CustomerRepo.List(ctx, types.NewNoLimitCustomerFilter())
    if err != nil {
        return nil, ierr.WithError(err).
            WithHint("Failed to fetch customers").
            Mark(ierr.ErrDatabase)
    }
    return customers, nil
}
```

`ExternalCustomerIDs` alone (with `ExternalCustomerID` empty) correctly triggers the specific-customer path because `resolveEffectiveExternalIDs` unions both fields.

---

## V1 Path (`event_post_processing.go`)

One-line normalisation at the very top of V1's `GetDetailedUsageAnalytics`, before any existing logic:

```go
if req.ExternalCustomerID == "" && len(req.ExternalCustomerIDs) > 0 {
    req.ExternalCustomerID = req.ExternalCustomerIDs[0]
}
```

V1 is single-customer only. Sending multiple IDs to V1 silently uses the first one — acceptable since V1 is a legacy path being phased out and does not support hierarchy or multi-customer aggregation.

---

## What Does Not Change

- `fetchAnalyticsData` — always operates on a single customer, untouched.
- `buildAnalyticsResponse`, `calculateCosts`, `enrichWithMetadata` — untouched.
- `createAnalyticsParams` — reads `req.ExternalCustomerID` which is always a single ID when `fetchAnalyticsData` is called; untouched.
- `mergeAnalyticsData` — already exists and is reused as-is.
- `validateAnalyticsRequestV2` — untouched.
- HTTP handler (`internal/api/v1/events.go`) — untouched.
- Invoice service callers (`GenerateInvoiceLineItemUsage`, `GenerateInvoiceLineItemUsageV2`) — untouched.
- Customer portal and revenue analytics callers — untouched.

---

## Testing

| Scenario | Expected |
|----------|----------|
| Both `ExternalCustomerID` and `ExternalCustomerIDs` empty | Validation error |
| Only `ExternalCustomerID` set | Proceeds normally, single customer |
| Only `ExternalCustomerIDs` set (one entry) | Proceeds normally, single customer |
| Both fields set with overlapping IDs | Deduplication: each unique ID fetched once |
| Two distinct customers with known usage | Response total = sum of both customers' usage |
| `IncludeChildren = true`, parent with one child | Child usage included in total; `fetchChildCustomers` called once per parent |
| `IncludeChildren = true`, child already in `ExternalCustomerIDs` | Deduplication: child fetched once, not twice |
| All customers in resolved list fail to fetch | Empty `GetUsageAnalyticsResponse` returned, no panic |
| Currency mismatch between customers | Hard validation error |
| V1 path: `ExternalCustomerID` empty, `ExternalCustomerIDs = ["x"]` | Proceeds with `"x"` as the single customer |
| Existing invoice service caller (sets `ExternalCustomerID` only) | No behaviour change |
| `fetchCustomers` called with only `ExternalCustomerIDs` set | Returns those specific customers, not all customers |
