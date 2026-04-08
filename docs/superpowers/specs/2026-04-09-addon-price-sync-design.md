# Addon Price Sync — Design Spec

**Date:** 2026-04-09  
**Status:** Approved  
**Author:** omkar sonawane

---

## Overview

Extend the existing plan price sync mechanism to support add-on prices. When an add-on's prices are updated (new price added, old price end-dated), active subscriptions that include that add-on must have their subscription line items updated accordingly — new line items created for new prices, and existing line items terminated when prices expire.

Behavior is identical to plan price sync. Only the entity scoping changes: instead of filtering subscriptions by `plan_id`, we join through the `addon_associations` table.

---

## Goals

- New prices apply only to **future invoices**
- Existing invoices retain old pricing (no backfill)
- Full parity with plan price sync: idempotent, batched, cursor-paginated, Redis-locked, Temporal-backed
- Both async (Temporal workflow) and sync (direct HTTP) endpoints exposed

## Non-Goals

- Backfilling historical invoices
- Proration of in-progress billing periods
- Cron-based automatic triggering (event-driven / manual only, same as plan sync)

---

## Architecture

### Layer Map

```
API (addon.go)
  └─ AddonService.SyncAddonPrices
       ├─ addonpricesync.Repository  (SQL: terminate + create deltas)
       └─ PlanService.ReprocessEventsForMissingPairs  (reuse: reprocess usage events)

Temporal
  PriceSyncWorkflow (generalized)
    ├─ EntityType=PLAN  → SyncPlanPrices activity  (existing)
    └─ EntityType=ADDON → SyncAddonPrices activity  (new)
```

---

## Section 1: Domain Layer

**New package:** `internal/domain/addonpricesync/repository.go`

Mirrors `internal/domain/planpricesync/repository.go` with `AddonID` in place of `PlanID`.

```go
type AddonLineItemTerminationDelta struct {
    LineItemID     string
    SubscriptionID string
    PriceID        string
    TargetEndDate  time.Time
}

type AddonLineItemCreationDelta struct {
    SubscriptionID string
    PriceID        string  // addon price ID (entity_type = ADDON)
    CustomerID     string
}

type ListAddonLineItemsToTerminateParams struct {
    AddonID string
    Limit   int
}

type ListAddonLineItemsToCreateParams struct {
    AddonID    string
    Limit      int
    AfterSubID string  // cursor
}

type TerminateExpiredAddonPricesLineItemsParams struct {
    AddonID string
    Limit   int
}

type Repository interface {
    TerminateExpiredAddonPricesLineItems(ctx context.Context, p TerminateExpiredAddonPricesLineItemsParams) (int, error)
    ListAddonLineItemsToTerminate(ctx context.Context, p ListAddonLineItemsToTerminateParams) ([]AddonLineItemTerminationDelta, error)
    ListAddonLineItemsToCreate(ctx context.Context, p ListAddonLineItemsToCreateParams) ([]AddonLineItemCreationDelta, error)
    GetLastSubscriptionIDInBatch(ctx context.Context, p ListAddonLineItemsToCreateParams) (*string, error)
}
```

---

## Section 2: Repository Layer

**New file:** `internal/repository/ent/addon_price_sync.go`

Implements `addonpricesync.Repository`. SQL CTE structure is identical to `plan_price_sync.go` with two substitutions:

| Plan sync | Addon sync |
|-----------|------------|
| `WHERE subscriptions.plan_id = $3` | `JOIN addon_associations aa ON aa.entity_id = s.id WHERE aa.addon_id = $3 AND aa.entity_type = 'SUBSCRIPTION' AND aa.addon_status = 'active'` |
| `WHERE prices.entity_type = 'PLAN'` | `WHERE prices.entity_type = 'ADDON'` |

Everything else is identical:
- `parent_price_id` lineage detection (skip if subscription already has a derived/overridden price)
- Bulk `UPDATE` for termination (sets `end_date` on line items)
- Cursor-based pagination (`AfterSubID`, default batch 1000)
- Creation delta query returns `(subscription_id, price_id, customer_id)` triples

Constructor:
```go
func NewAddonPriceSyncRepository(client postgres.IClient, log *logger.Logger) addonpricesync.Repository
```

---

## Section 3: Service Layer

### `internal/service/addon.go` — new method

```go
SyncAddonPrices(ctx context.Context, addonID string) (*dto.SyncAddonPricesResponse, error)
```

**Flow** (mirrors `SyncPlanPrices` in `plan.go`):

1. Verify addon exists via `addonService.GetAddon`
2. **Termination loop**: call `repo.TerminateExpiredAddonPricesLineItems` in batches until 0 rows terminated
3. **Cursor creation loop** (batches of 1000):
   a. `ListAddonLineItemsToCreate` → missing `(subscriptionID, priceID)` pairs
   b. Bulk-create subscription line items (2000 per transaction)
   c. Fire `ReprocessEventsForPlanWorkflow` (non-blocking) for usage-based prices — convert `AddonLineItemCreationDelta` to `PlanLineItemCreationDelta` (same three fields) and call `planService.ReprocessEventsForMissingPairs`
   d. Advance cursor via `GetLastSubscriptionIDInBatch`
4. Return `SyncAddonPricesResponse` with metrics

### New dependency on `AddonService`

```go
type addonService struct {
    // existing deps ...
    addonPriceSyncRepo addonpricesync.Repository  // new
    planService        PlanService                 // new — for ReprocessEventsForMissingPairs
}
```

`ServiceParams` gains a new field `AddonPriceSyncRepo addonpricesync.Repository`.

### New DTO — `internal/api/dto/`

```go
type SyncAddonPricesResponse struct {
    AddonID             string `json:"addon_id"`
    LineItemsFound      int    `json:"line_items_found"`
    LineItemsCreated    int    `json:"line_items_created"`
    LineItemsTerminated int    `json:"line_items_terminated"`
}
```

### `AddonService` interface update

```go
SyncAddonPrices(ctx context.Context, addonID string) (*dto.SyncAddonPricesResponse, error)
```

---

## Section 4: Temporal Layer

### Generalize `PriceSyncWorkflowInput` — `internal/temporal/models/plan.go`

```go
type PriceSyncWorkflowInput struct {
    EntityType    types.PriceEntityType `json:"entity_type"`   // PRICE_ENTITY_TYPE_PLAN or PRICE_ENTITY_TYPE_ADDON
    EntityID      string                `json:"entity_id"`      // plan ID or addon ID
    PlanID        string                `json:"plan_id"`        // deprecated — read as fallback for backward compat
    TenantID      string                `json:"tenant_id"`
    EnvironmentID string                `json:"environment_id"`
    UserID        string                `json:"user_id"`
}

func (p *PriceSyncWorkflowInput) Validate() error {
    // If EntityID empty and PlanID set, backfill for backward compat
    if p.EntityID == "" && p.PlanID != "" {
        p.EntityID = p.PlanID
        p.EntityType = types.PRICE_ENTITY_TYPE_PLAN
    }
    // validate EntityID != "", EntityType in {PLAN, ADDON}, TenantID, EnvironmentID, UserID
}
```

### Generalize `PriceSyncWorkflow` — `internal/temporal/workflows/price_sync_workflow.go`

```go
func PriceSyncWorkflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
    if err := in.Validate(); err != nil { return nil, err }

    switch in.EntityType {
    case types.PRICE_ENTITY_TYPE_PLAN:
        // existing: execute ActivitySyncPlanPrices
    case types.PRICE_ENTITY_TYPE_ADDON:
        // new: execute ActivitySyncAddonPrices
        // maps result fields onto SyncPlanPricesResponse for uniform return type
    default:
        return nil, ierr.NewError("unsupported entity type").Mark(ierr.ErrValidation)
    }
}
```

The return type stays `*dto.SyncPlanPricesResponse` for both branches (addon branch maps its 3 counter fields onto the same struct — no new workflow return type).

### New `AddonActivities` — `internal/temporal/activities/addon/addon_activities.go`

```go
type AddonActivities struct {
    addonService service.AddonService
}

type SyncAddonPricesInput struct {
    AddonID       string                `json:"addon_id"`
    EntityType    types.PriceEntityType `json:"entity_type"` // always PRICE_ENTITY_TYPE_ADDON
    TenantID      string                `json:"tenant_id"`
    EnvironmentID string                `json:"environment_id"`
    UserID        string                `json:"user_id"`
}

// SyncAddonPrices — registered as "SyncAddonPrices" in Temporal
func (a *AddonActivities) SyncAddonPrices(ctx context.Context, input SyncAddonPricesInput) (*dto.SyncAddonPricesResponse, error) {
    // validate input
    // set ctx tenant/env/user
    // defer: release Redis lock (cache.PrefixAddonPriceSyncLock + input.AddonID)
    // call a.addonService.SyncAddonPrices(ctx, input.AddonID)
}
```

### `buildPriceSyncInput` — `internal/temporal/service/service.go`

Update to handle `EntityType = PRICE_ENTITY_TYPE_ADDON`, populate `EntityID` from addon ID string param.

### `registration.go`

```go
addonService := service.NewAddonService(params)
addonActivities := addonActivities.NewAddonActivities(addonService)
// register addonActivities on the same worker config as planActivities
```

---

## Section 5: API Layer

### Cache constant — `internal/cache/cache.go`

```go
// PrefixAddonPriceSyncLock is the Redis key prefix for addon-level price sync lock (used with addonID).
PrefixAddonPriceSyncLock = "price_sync:addon:"
```

Reuses existing `ExpiryPriceSyncLock` (2h TTL).

### Two new handlers — `internal/api/v1/addon.go`

**`SyncAddonPrices`** (v1 — Temporal async):
1. Extract `addonID` from `:id` path param
2. Verify addon exists
3. Acquire Redis lock: `cache.PrefixAddonPriceSyncLock + addonID` (SetNX, 2h)
4. Fire `types.TemporalPriceSyncWorkflow` with `PriceSyncWorkflowInput{EntityType: PRICE_ENTITY_TYPE_ADDON, EntityID: addonID}`
5. Return `200` with workflow ID + run ID (lock released by activity defer)

**`SyncAddonPricesV2`** (v2 — direct sync):
1. Extract `addonID`, acquire same Redis lock
2. `defer redisCache.Delete(lockKey)`
3. Call `addonService.SyncAddonPrices(ctx, addonID)`
4. Return `200` with `SyncAddonPricesResponse`

Helper:
```go
func addonPriceSyncLockKey(addonID string) string {
    return cache.PrefixAddonPriceSyncLock + addonID
}
```

### Routes — `internal/api/router.go`

```
POST /addons/:id/sync/subscriptions     → SyncAddonPrices    (v1, Temporal)
POST /addons/:id/sync/subscriptions/v2  → SyncAddonPricesV2  (v2, direct)
```

No new `TemporalWorkflowType` constant — both plan and addon use `TemporalPriceSyncWorkflow`, differentiated by `EntityType` in the input.

---

## Section 6: DI Wiring

**`cmd/server/main.go`** — add to `fx.Provide()`:
```go
entRepo.NewAddonPriceSyncRepository,
```

**`ServiceParams`** — add field:
```go
AddonPriceSyncRepo addonpricesync.Repository
```

---

## Section 7: Tests

**File:** `internal/service/addon_test.go` (or new `addon_price_sync_test.go`)

| Test | Coverage |
|------|----------|
| `TestSyncAddonPrices_BasicCreation` | New addon price → line items created on active subscriptions with that addon |
| `TestSyncAddonPrices_Termination` | Expired addon price `end_date` → line items terminated |
| `TestSyncAddonPrices_IdempotencyNoDuplicates` | Re-running sync → no duplicate line items |
| `TestSyncAddonPrices_PriceLineageSkip` | Subscription already has overridden price (`parent_price_id` set) → skipped |
| `TestSyncAddonPrices_MultipleAddons` | Subscription has 2 addons → syncing addon A doesn't touch addon B line items |
| `TestSyncAddonPrices_InactiveAddonAssociation` | `addon_status != active` → subscription excluded |
| `TestSyncAddonPrices_NoSubscriptions` | No active associations → no-op, returns zeros |

---

## File Inventory

### New files

| File | Purpose |
|------|---------|
| `internal/domain/addonpricesync/repository.go` | Domain interface + delta types |
| `internal/repository/ent/addon_price_sync.go` | SQL CTE implementation |
| `internal/temporal/activities/addon/addon_activities.go` | `AddonActivities` + `SyncAddonPrices` activity |

### Modified files

| File | Change |
|------|--------|
| `internal/service/addon.go` | Add `SyncAddonPrices`, new deps (`addonPriceSyncRepo`, `planService`) |
| `internal/api/v1/addon.go` | Add `SyncAddonPrices`, `SyncAddonPricesV2` handlers + lock key helper |
| `internal/api/router.go` | 2 new routes |
| `internal/cache/cache.go` | Add `PrefixAddonPriceSyncLock` constant |
| `internal/temporal/models/plan.go` | Generalize `PriceSyncWorkflowInput` with `EntityType`/`EntityID` |
| `internal/temporal/workflows/price_sync_workflow.go` | Route on `EntityType` |
| `internal/temporal/service/service.go` | `buildPriceSyncInput` addon support |
| `internal/temporal/registration.go` | Register `AddonActivities` |
| `cmd/server/main.go` | Wire `addonpricesync.Repository` |
| `internal/api/dto/` | Add `SyncAddonPricesResponse` |

---

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Addon association `addon_status != active` | Excluded from `subs` CTE — no line items created |
| Deleted/archived addon | `GetAddon` returns not-found → API returns 404 before any SQL |
| Concurrent sync for same addon | Redis SetNX lock prevents double execution |
| Addon with no subscriptions | Termination + creation loops complete with 0 ops — returns zero metrics |
| Usage-based addon price | `ReprocessEventsForMissingPairs` fired non-blocking for new line items |
| Multiple addons per subscription | SQL filters by `addon_id` — addon A sync never touches addon B line items |
| Price override already applied | `parent_price_id` lineage check in CTE skips already-derived line items |
| Partial billing cycle | No proration — new line items apply from next invoice cycle only |
| Currency / tier mismatch | CTE matches prices by `currency`, `billing_period`, `billing_period_count` — mismatches simply produce no delta row |
