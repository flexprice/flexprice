# Subscription Discount & Tax Modification — Design Spec

**Date:** 2026-06-10  
**Branch:** feat/coupons  
**Status:** Approved for implementation

---

## Overview

Add the ability to add or remove coupons and tax associations on an existing subscription post-creation, via the existing subscription modify API. Supports time-bounded discounts (e.g. 10% off year 1, 34% off year 2) via `effective_date`. Both preview (dry-run) and execute endpoints are provided.

---

## Requirements

- Add a coupon to an existing subscription, with an optional `effective_date` (past or future)
- Remove a coupon from an existing subscription (soft-delete the `CouponAssociation` by setting `EndDate`)
- Add a tax rate association to an existing subscription
- Remove a tax rate association from an existing subscription (soft-delete)
- `effective_date` defaults to `time.Now()` if not provided
- `effective_date` can be in the past (retroactive) or future (scheduled)
- Changes only affect future invoices — current open/draft invoice is untouched
- Preview returns the updated subscription state only (no invoice projection)
- One association per request (coupon OR tax, never both)
- Modifications route through the existing `/modify/preview` and `/modify/execute` endpoints

---

## API Layer

No new endpoints. Existing routes:

```
POST /subscriptions/{id}/modify/preview
POST /subscriptions/{id}/modify/execute
```

Two new `SubscriptionModifyType` values added to `internal/api/dto/subscription_modification.go`:

```go
SubscriptionModifyTypeCoupon SubscriptionModifyType = "coupon"
SubscriptionModifyTypeTax    SubscriptionModifyType = "tax"
```

`ExecuteSubscriptionModifyRequest` gains two new optional params fields:

```go
CouponParams *SubModifyCouponParams `json:"coupon_params,omitempty"`
TaxParams    *SubModifyTaxParams    `json:"tax_params,omitempty"`
```

Exactly one params field must be set and must match the declared `type`. Enforced in the existing handler switch.

---

## DTO Layer

New types in `internal/api/dto/subscription_modification.go`:

```go
type SubModifyAction string

const (
    SubModifyActionAdd    SubModifyAction = "add"
    SubModifyActionRemove SubModifyAction = "remove"
)

type SubModifyCouponParams struct {
    Action        SubModifyAction `json:"action"`          // required
    CouponID      *string         `json:"coupon_id"`       // required for add
    AssociationID *string         `json:"association_id"`  // required for remove
    EffectiveDate *time.Time      `json:"effective_date"`  // defaults to now
}

type SubModifyTaxParams struct {
    Action        SubModifyAction `json:"action"`          // required
    TaxRateID     *string         `json:"tax_rate_id"`     // required for add
    AssociationID *string         `json:"association_id"`  // required for remove
    EffectiveDate *time.Time      `json:"effective_date"`  // defaults to now
}
```

**Validation rules** (enforced via `Validate()` on each struct):

- `action` must be `"add"` or `"remove"`
- `add`: `coupon_id` / `tax_rate_id` required; `association_id` must be nil
- `remove`: `association_id` required; `coupon_id` / `tax_rate_id` must be nil
- `effective_date` is always optional

---

## Service Layer

`SubscriptionModificationService` in `internal/service/subscription_modification.go` routes to two new handlers:

```
handleCouponModification(ctx, sub, params, dryRun bool)
handleTaxModification(ctx, sub, params, dryRun bool)
```

`effective_date` is resolved at the top of each handler:

```go
effectiveDate := time.Now()
if params.EffectiveDate != nil {
    effectiveDate = *params.EffectiveDate
}
```

### Coupon — add (execute)

1. Validate coupon exists and is active
2. Check no existing active `CouponAssociation` for same `coupon_id` on this subscription with an overlapping date range
3. Create `CouponAssociation{CouponID, SubscriptionID, StartDate=effectiveDate}`
4. Return updated subscription with new association appended

### Coupon — remove (execute)

1. Validate `CouponAssociation` exists and `SubscriptionID` matches
2. Validate association is currently active (no past `EndDate`)
3. Set `EndDate = effectiveDate` on the association record
4. Return updated subscription

### Tax — add (execute)

1. Validate tax rate exists and is active
2. Check no duplicate active `TaxAssociation` for same `tax_rate_id` on this subscription
3. Create `TaxAssociation{TaxRateID, EntityType=SUBSCRIPTION, EntityID=subscriptionID, StartDate=effectiveDate}`
4. Return updated subscription

### Tax — remove (execute)

1. Validate `TaxAssociation` exists and `EntityID` matches subscription
2. Validate association is currently active
3. Set `EndDate = effectiveDate` on the association record
4. Return updated subscription

### Preview (dryRun=true)

Same validation steps as execute, but no DB writes. The simulated state is constructed in memory:

- For `add`: append a synthetic association object to the subscription's in-memory association list
- For `remove`: set `EndDate` on the matched association object in-memory

Response uses the existing `SubscriptionModifyResponse` — `Subscription` carries the full updated subscription state; `ChangedResources` lists the added or modified association.

---

## Error Handling

All validation errors return `400 Bad Request`. Business logic errors return `422 Unprocessable Entity`.

| Scenario | Code | Message |
|----------|------|---------|
| `action` missing or invalid | 400 | `invalid action` |
| `add` missing `coupon_id` / `tax_rate_id` | 400 | `coupon_id required for add` |
| `remove` missing `association_id` | 400 | `association_id required for remove` |
| Coupon not found or inactive | 422 | `coupon not found or inactive` |
| Tax rate not found or inactive | 422 | `tax rate not found or inactive` |
| Association not found | 422 | `association not found` |
| Association belongs to different subscription | 422 | `association does not belong to this subscription` |
| Association already inactive | 422 | `association already inactive` |
| Duplicate active association (overlapping date range) | 422 | `coupon already active on this subscription for the given date range` |

Execute is atomic — if any validation fails, nothing is written.

---

## Testing

Table-driven tests alongside implementation:

**`internal/service/subscription_modification_coupon_test.go`**

| Test case |
|-----------|
| Add coupon — `effective_date` in past (retroactive) |
| Add coupon — `effective_date` in future (scheduled) |
| Add coupon — `effective_date` nil, defaults to now |
| Add coupon — duplicate active association → 422 |
| Add coupon — coupon not found → 422 |
| Add coupon — coupon inactive → 422 |
| Remove coupon — `effective_date` in past |
| Remove coupon — `effective_date` in future (scheduled removal) |
| Remove coupon — `effective_date` nil, defaults to now |
| Remove coupon — association not found → 422 |
| Remove coupon — association belongs to different subscription → 422 |
| Remove coupon — association already inactive → 422 |
| Preview add — no DB write, returns updated subscription state |
| Preview remove — no DB write, returns updated subscription state |

**`internal/service/subscription_modification_tax_test.go`** — mirrors coupon test cases with `tax_rate_id` / `TaxAssociation`.

---

## Time-Bounded Discount Pattern

To configure year 1 = 10% off, year 2 = 34% off:

```
# Call 1 — add year-1 coupon
POST /subscriptions/{id}/modify/execute
{
  "type": "coupon",
  "coupon_params": {
    "action": "add",
    "coupon_id": "<10pct-coupon-id>",
    "effective_date": "2026-01-01T00:00:00Z"
  }
}

# Call 2 — schedule removal of year-1 coupon
POST /subscriptions/{id}/modify/execute
{
  "type": "coupon",
  "coupon_params": {
    "action": "remove",
    "association_id": "<assoc-id-from-call-1>",
    "effective_date": "2026-12-31T23:59:59Z"
  }
}

# Call 3 — add year-2 coupon
POST /subscriptions/{id}/modify/execute
{
  "type": "coupon",
  "coupon_params": {
    "action": "add",
    "coupon_id": "<34pct-coupon-id>",
    "effective_date": "2027-01-01T00:00:00Z"
  }
}
```

The `CouponAssociation.StartDate` / `EndDate` fields already model this — the billing engine respects these dates when applying discounts to invoices.

---

## Schema Change Required

`CouponAssociation` already has `StartDate` and `EndDate` fields. `TaxAssociation` does not — it currently only has `EntityType`, `EntityID`, `Priority`, `AutoApply`, `Currency`. To support `effective_date` for both past and future on tax operations, `StartDate` and `EndDate` must be added to `TaxAssociation`.

Required:
1. Add `start_date` (non-nullable, default now) and `end_date` (nullable) fields to `ent/schema/taxassociation.go`
2. Run `make generate-ent` and `make generate-migration` to produce the migration SQL

---

## Files to Change

| File | Change |
|------|--------|
| `ent/schema/taxassociation.go` | Add `start_date` and `end_date` fields |
| `internal/domain/taxassociation/model.go` | Add `StartDate time.Time` and `EndDate *time.Time` fields |
| `internal/api/dto/subscription_modification.go` | Add `SubModifyAction`, `SubModifyCouponParams`, `SubModifyTaxParams`; extend `ExecuteSubscriptionModifyRequest` |
| `internal/service/subscription_modification.go` | Add routing cases for `coupon` and `tax` types |
| `internal/service/subscription_modification_coupon.go` | New file — coupon add/remove logic |
| `internal/service/subscription_modification_tax.go` | New file — tax add/remove logic |
| `internal/service/subscription_modification_coupon_test.go` | New file — coupon modification tests |
| `internal/service/subscription_modification_tax_test.go` | New file — tax modification tests |
| `internal/interfaces/service.go` | No change needed — existing `SubscriptionModificationService` interface covers this |
