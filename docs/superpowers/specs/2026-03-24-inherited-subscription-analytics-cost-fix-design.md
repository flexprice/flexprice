# Inherited Subscription Analytics Cost Fix Design

**Date:** 2026-03-24
**Status:** Approved

---

## Overview

When `include_children=true` (or when `ExternalCustomerIDs` contains a child customer directly), the analytics API correctly fetches usage events for child customers from ClickHouse. However, **cost calculation silently produces $0** for those children because the pricing information (subscription line items) lives on the parent subscription, not on the child's INHERITED skeleton subscription.

This spec fixes cost calculation for customers with INHERITED subscriptions by ensuring the parent subscription's line items are available in the pricing lookup map before costs are computed.

---

## Background: Subscription Hierarchy

Three subscription types exist:

| Type | Description |
|------|-------------|
| `standalone` | Regular subscription. Owns its own line items. Cost lookup works correctly today. |
| `parent` | Primary subscription in a hierarchy. Owns all line items for itself and its children. |
| `inherited` | Skeleton subscription created for each child customer. Has **no line items**. Pricing lives on the parent subscription (`ParentSubscriptionID` field). |

Usage events ingested for a child customer are tagged with `sub_line_item_id` values that reference the **parent subscription's** line items. When analytics fetches the child's subscriptions (INHERITED only) and builds `SubscriptionLineItems`, those parent line item IDs are absent — so `calculateCosts` finds nothing and returns $0.

---

## Root Cause

In `fetchAnalyticsData`:

```
subscriptions = fetchSubscriptions(ctx, child.ID)
// → returns [InheritedSub{LineItems: []}]

data.SubscriptionLineItems = {}   // built from subscriptions → empty
data.Subscriptions = [InheritedSub]

analytics = fetchAnalytics(ctx, params)
// → ClickHouse returns events with sub_line_item_id = "parent-line-item-id"

calculateCosts(data)
// → looks up "parent-line-item-id" in SubscriptionLineItems → NOT FOUND → cost = $0

fetchSubscriptionPrices(data)
// → iterates data.Subscriptions to collect PriceIDs from line items → none found → Prices map empty
// → calculateCosts looks up Prices["parent-price-id"] → NOT FOUND → cost = $0 (secondary cause)
```

---

## Fix

After `fetchSubscriptions` returns a list that contains any `SubscriptionTypeInherited` subscription with a non-nil `ParentSubscriptionID`, fetch those parent subscriptions (with line items) and append them to the slice. The existing downstream code in `fetchAnalyticsData` then builds `SubscriptionLineItems` and `SubscriptionsMap` from the full combined slice, and `fetchSubscriptionPrices` iterates the same combined `data.Subscriptions` slice to collect price IDs — so both lookup maps are populated and cost calculation succeeds.

### New helper

```go
// fetchParentSubscriptions fetches the parent subscriptions (with line items) for any
// inherited subscriptions in the provided list, deduplicated by parent subscription ID.
// Returns the parent subscriptions to be appended to the caller's subscription slice.
// Returns an empty slice (no error) when no inherited subscriptions are present.
func (s *featureUsageTrackingService) fetchParentSubscriptions(
    ctx context.Context,
    subscriptions []*subscription.Subscription,
) ([]*subscription.Subscription, error)
```

Called once from `fetchAnalyticsData`, immediately after `fetchSubscriptions`. For INHERITED subs with `ParentSubscriptionID != nil`, deduplicates parent IDs and fetches each using `s.SubRepo.GetWithLineItems(ctx, parentSubID)` (see below). Returns the list of parent subs to append.

### Call site in `fetchAnalyticsData`

```
// 2. Fetch subscriptions
subscriptions, err = fetchSubscriptions(ctx, customer.ID)

// 2a. For inherited subscriptions, also include the parent subscription so its
//     line items are available for cost calculation (SubscriptionLineItems map)
//     and price discovery (fetchSubscriptionPrices iterates data.Subscriptions).
parentSubs, err = fetchParentSubscriptions(ctx, subscriptions)
subscriptions = append(subscriptions, parentSubs...)

// 3. Validate currency consistency (unchanged — reads from combined list)
currency, err = validateCurrency(subscriptions)
```

No other functions change.

---

## How to Fetch a Parent Subscription

Use **`s.SubRepo.GetWithLineItems(ctx, parentSubID)`** — this is the correct method. It is defined on the `subscription.Repository` interface and its implementation fetches the subscription and eagerly loads its line items, setting them on `sub.LineItems` before returning.

> **Do NOT use `s.SubRepo.Get`** — that method performs a plain query with no line item eager-loading; `sub.LineItems` will be nil.

The second return value of `GetWithLineItems` (`[]*SubscriptionLineItem`) can be discarded since the line items are already set on the returned `*Subscription.LineItems`.

---

## What Does Not Change

- `GetDetailedUsageAnalytics` (multi-customer loop) — untouched
- `GetDetailedUsageAnalyticsV2` — untouched; also calls `fetchAnalyticsData` and gains the same fix automatically
- `buildAnalyticsResponse`, `calculateCosts`, `mergeAnalyticsData` — untouched
- V1 legacy path (`event_post_processing.go`) — untouched
- DTO shape — no API changes
- Standalone customers — `fetchParentSubscriptions` returns an empty slice when no INHERITED subs exist; no behaviour change

---

## Side Effect: Synthetic Zero-Usage Injection

`fetchAnalyticsData` contains a loop (after building the data struct) that injects synthetic zero-usage analytics entries for committed line items with no ClickHouse data. After the fix, this loop also iterates the parent subscription's committed line items. This is the **desired behaviour**: the child customer's analytics context should honour the parent's committed minimums for the inherited pricing. No code change is needed here.

---

## Files Changed

| File | Change |
|------|--------|
| `internal/service/feature_usage_tracking.go` | Add `fetchParentSubscriptions` helper; call it in `fetchAnalyticsData` after `fetchSubscriptions` |
| `internal/service/feature_usage_tracking_analytics_test.go` | Tests for the new code path |

---

## Currency Handling

`validateCurrency` is called after the parent subs are appended. Parent and child subscriptions in a hierarchy are created with the same currency (enforced at subscription creation time), so no currency conflict is expected. The existing mismatch error in `validateCurrency` provides a safety net if this invariant is ever violated.

---

## Testing

| Scenario | Expected |
|----------|----------|
| Standalone customer (no INHERITED subs) | `fetchParentSubscriptions` returns empty slice; no behaviour change |
| Child customer with one INHERITED sub | Parent subscription's line items added; cost calculated correctly (non-zero) |
| Two INHERITED subs pointing to the same parent sub | Parent sub fetched once (dedup by parent ID) |
| `fetchParentSubscriptions` fails to fetch parent (DB error) | Error propagated, request fails with a clear error message |
| `include_children=true` with a parent customer: child has INHERITED sub | Child costs calculated correctly; total reflects both parent's and child's usage |
| `include_children=false` (or not set), child customer requested directly via `ExternalCustomerIDs` | Same fix applies — `fetchParentSubscriptions` runs for any customer with INHERITED subs |
| `GetDetailedUsageAnalyticsV2` called with a child customer | Also fixed automatically (V2 also calls `fetchAnalyticsData`) |
