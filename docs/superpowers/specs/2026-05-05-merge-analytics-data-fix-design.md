# Fix: mergeAnalyticsData — Missing PriceResponses Merge & Fragile Base Selection

**Date:** 2026-05-05  
**File scope:** `internal/service/feature_usage_tracking.go`, `internal/service/feature_usage_tracking_test.go`  
**Type:** Bug fix + regression guard  

---

## Context

`GetDetailedUsageAnalyticsV2` aggregates analytics across a parent account and its child accounts (IAM-ORG). For each customer it calls `fetchAnalyticsData`, then merges subsequent customers' data into an aggregated `AnalyticsData` struct via `mergeAnalyticsData`.

Two bugs exist in this path.

---

## Bug 1 — Missing `PriceResponses` merge

### Location
`internal/service/feature_usage_tracking.go` — `mergeAnalyticsData` (~line 3267)

### Root cause
`mergeAnalyticsData` merges every map in `AnalyticsData` (Features, Meters, Prices, Plans, Addons, Groups, Subscriptions) **except `PriceResponses`**.

`PriceResponses` (`map[string]*dto.PriceResponse`) is populated in `fetchSubscriptionPrices` for each customer from their subscription line items. It is the sole source used in `ToGetUsageAnalyticsResponseDTO` for:
- `expand=["price"]` — attaches the full price object to each analytics item
- `PlanID` — derived from `price.EntityID` when `price.EntityType == PLAN`
- `AddOnID` — derived from `price.EntityID` when `price.EntityType == ADDON`
- Subscription-override parent price chain — `price.ParentPriceID` lookup

### Why it only breaks for child-only event types
When both parent and child have usage for the same event type (e.g. VPC), the parent's processing already populates `PriceResponses` with the VPC price. The missing child merge goes unnoticed.

When only the child has usage for an event type (e.g. CPU), the CPU price exists only in the child's `PriceResponses`. Without the merge, `ToGetUsageAnalyticsResponseDTO` finds nothing in the map for that price ID and silently omits price, PlanID, and AddOnID.

**Note:** Cost calculation is unaffected — `calculateCosts` uses `data.Prices` (not `PriceResponses`), and `Prices` was already being merged correctly. Child-only event costs were calculating correctly; only the response DTO was broken.

### Fix
Add a merge loop for `PriceResponses` at the end of `mergeAnalyticsData`, using the same first-wins pattern as all other maps:

```go
// Merge price responses (needed for expand=["price"] when only child has certain event types)
for id, priceResp := range additional.PriceResponses {
    if _, exists := aggregated.PriceResponses[id]; !exists {
        aggregated.PriceResponses[id] = priceResp
    }
}
```

**First-wins behaviour:** If parent and child both carry the same price ID (e.g. shared VPC price), the parent's version is kept — correct, they are the same object. Subscription-scoped override prices have distinct IDs (P2 with ParentPriceID=P1) and merge cleanly.

---

## Bug 2 — Fragile base-selection using loop index

### Location
`internal/service/feature_usage_tracking.go` — `GetDetailedUsageAnalyticsV2` (~line 1381)

### Root cause
```go
if i == 0 {
    aggregatedData = data
} else {
    s.mergeAnalyticsData(aggregatedData, data)
}
```

`i` is the loop index, not the count of successful fetches. If the customer at index 0 (the parent) returns an error and hits `continue`, `aggregatedData` remains `nil`. All subsequent iterations call `mergeAnalyticsData(nil, data)`, which returns early on the nil guard — silently discarding all child data. The endpoint returns an empty response with no error logged at the call site.

### Fix
Replace the index check with a nil check on the accumulator:

```go
if aggregatedData == nil {
    aggregatedData = data
} else {
    s.mergeAnalyticsData(aggregatedData, data)
}
```

The first *successful* customer becomes the base, regardless of its position in the slice.

---

## Merge-Completeness Test

### Location
`internal/service/feature_usage_tracking_test.go` — new test `TestMergeAnalyticsData`

### Purpose
Acts as a structural contract for `mergeAnalyticsData`. If a new map field is added to `AnalyticsData` and forgotten in `mergeAnalyticsData`, the test will fail, forcing a conscious decision.

### Cases

| Case | What it verifies |
|------|-----------------|
| All maps populated, no overlap | Every entry from `additional` appears in `aggregated` after merge |
| Overlapping keys | First-wins: aggregated value is kept, additional value ignored |
| Nil aggregated | No panic, returns cleanly |
| Empty additional | Aggregated is unchanged |

---

## What Is Not Changed

| Field | Reason not merged |
|-------|-------------------|
| `Customer` | Per-customer context, one per iteration |
| `Analytics` | Appended separately at line 1378, reassigned at line 1411 |
| `Currency` | Validated for consistency and set once |
| `Params` | Per-customer (`Params.CustomerID` used only during ClickHouse query, already complete) |

No API changes, no DTO changes, no schema migrations.

---

## Files Changed

| File | Change |
|------|--------|
| `internal/service/feature_usage_tracking.go` | Add `PriceResponses` merge loop in `mergeAnalyticsData`; change `if i == 0` to `if aggregatedData == nil` in `GetDetailedUsageAnalyticsV2` |
| `internal/service/feature_usage_tracking_test.go` | Add `TestMergeAnalyticsData` with 4 table-driven cases |
