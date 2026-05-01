# Design: Include Inherited Child Usage in Events Pipeline

**Date:** 2026-05-01  
**Status:** Approved

## Problem

Flexprice has two usage pipelines for invoice/subscription charge calculation:

| Pipeline | Entry point | ClickHouse table |
|----------|-------------|------------------|
| Feature usage | `GetFeatureUsageBySubscription` | `feature_usage` |
| Normal events | `GetUsageBySubscription` | raw `events` |

The feature usage pipeline correctly aggregates usage for **parent + all active inherited children** via `usageCustomerIDsForSubscription()`. The normal events pipeline (`GetUsageBySubscription`) only queries events for the **single subscription owner**, ignoring inherited child customers entirely. This causes under-counting on parent subscriptions during invoice calculation (used for `ReferencePointInternalPreview`).

## Scope

- `GetUsageBySubscription` is only ever called for **parent** (or standalone) subscriptions during invoice calculation. The inherited subscription case is out of scope.
- No changes to the feature_usage or meter_usage pipelines — they already handle child resolution correctly.

## Changes

### 1. Fix status filter inconsistency

`usageCustomerIDsForSubscription()` in `subscription.go` currently includes `SubscriptionStatusPaused` when collecting child subscription IDs. `getChildExternalCustomerIDsForSubscription()` in `billing.go` does not. Both helpers (and the new one) will use **Active, Trialing, Draft** only — paused children's events should not roll up into the parent's invoice.

**File:** `internal/service/subscription.go`  
**Function:** `usageCustomerIDsForSubscription()`  
**Change:** Remove `types.SubscriptionStatusPaused` from the status filter.

### 2. Add `externalCustomerIDsForSubscription()` to subscriptionService

A new private method alongside `usageCustomerIDsForSubscription()`. Delegates internal ID resolution to the existing helper, then does one `CustomerRepo.List` call to convert to external IDs.

```go
func (s *subscriptionService) externalCustomerIDsForSubscription(
    ctx context.Context,
    sub *subscription.Subscription,
) ([]string, error) {
    internalIDs, err := s.usageCustomerIDsForSubscription(ctx, sub)
    if err != nil {
        return nil, err
    }
    custFilter := types.NewNoLimitCustomerFilter()
    custFilter.CustomerIDs = internalIDs
    customers, err := s.CustomerRepo.List(ctx, custFilter)
    if err != nil {
        return nil, err
    }
    out := make([]string, 0, len(customers))
    for _, c := range customers {
        if c.ExternalID != "" {
            out = append(out, c.ExternalID)
        }
    }
    return lo.Uniq(out), nil
}
```

**File:** `internal/service/subscription.go`  
**Location:** After `usageCustomerIDsForSubscription()` (~line 7427)

### 3. Wire into `GetUsageBySubscription`

**File:** `internal/service/subscription.go`  
**Function:** `GetUsageBySubscription()` (~line 2174)

Replace the single-customer usage pattern:

- Remove the `CustomerRepo.Get(ctx, subscription.CustomerID)` call entirely — the single customer lookup was only used for log lines (debug/info) and the single-ID query fan-out. Only error logs are enabled in prod so the debug/info lines carry no value.
- Call `s.externalCustomerIDsForSubscription(ctx, subscription)` right after fetching the subscription.
- Pass the resulting `[]string` to `GetDistinctEventNames` (already accepts `[]string`).
- Set `ExternalCustomerIDs` (not `ExternalCustomerID`) on each `GetUsageByMeterRequest` (field already exists on the struct).
- Any remaining error message that referenced `customer.ExternalID` references `req.SubscriptionID` instead.

### 4. Delete `getChildExternalCustomerIDsForSubscription()` from billingService

**File:** `internal/service/billing.go`  

Replace the one call site:
```go
// Before
extCustomerIDsForUsage, err := s.getChildExternalCustomerIDsForSubscription(ctx, sub)

// After
extCustomerIDsForUsage, err := subscriptionService.externalCustomerIDsForSubscription(ctx, sub)
```

`subscriptionService` is already instantiated in the billing method that calls this. Delete the now-unused `getChildExternalCustomerIDsForSubscription()` method from `billingService`.

## Data flow after change

```
GetUsageBySubscription(parentSubID)
  └─ externalCustomerIDsForSubscription(sub)
       └─ usageCustomerIDsForSubscription(sub)   ← internal IDs: owner + active/trialing/draft children
       └─ CustomerRepo.List(internalIDs)          ← resolve to external IDs
  └─ GetDistinctEventNames(externalCustomerIDs)   ← now fans out across all children
  └─ per meter: GetUsageByMeterRequest{ExternalCustomerIDs: [...]} ← aggregates child usage
```

## What is NOT changing

- `GetFeatureUsageBySubscription` and `GetMeterUsageBySubscription` — already correct.
- `GetUsageByMeterRequest` DTO — `ExternalCustomerIDs []string` field already exists.
- `GetDistinctEventNames` repo method — already accepts `[]string`.
- Invoice generation flow — no changes to when/how `GetUsageBySubscription` is called.

## Testing

- Unit test: `GetUsageBySubscription` for a parent subscription with two active inherited children returns aggregated usage across all three customers.
- Unit test: `GetUsageBySubscription` for a parent subscription with one paused child excludes the paused child's events.
- Unit test: `externalCustomerIDsForSubscription` returns only the owner ID for a standalone subscription.
- Existing feature_usage and meter_usage pipeline tests should be unaffected.
