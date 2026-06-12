# Coupon Code & Unified Scheduling Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an optional human-readable `coupon_code` field to the Coupon entity, and introduce a unified `subscription_coupons` input on subscription create/modify that accepts coupon codes (never internal IDs) with optional start/end date scheduling and optional price-based line-item targeting.

**Architecture:** New `coupon_code` column + unique index on Coupon; new `SubscriptionCouponInput` DTO; resolution (code→ID, price_id→line_item_id) happens in the service layer following the existing `normalizePhaseCoupons` pattern. Old fields stay and get deprecated markers.

**Tech Stack:** Go 1.23+, Ent ORM, Gin, PostgreSQL

---

## Section 1 — `coupon_code` on the Coupon entity

### Ent schema change (`ent/schema/coupon.go`)
Add one optional field:
```go
field.String("coupon_code").
    Optional().
    Nillable().
    Comment("Human-readable coupon code (e.g. SUMMER20). Unique per tenant+environment."),
```

Add a unique index:
```go
index.Fields("tenant_id", "environment_id", "coupon_code").Unique(),
```

Stored **lowercase** (normalised on write). Looked up case-insensitively via `LOWER(coupon_code) = LOWER($1)` or by always lowercasing before storage.

### Domain model (`internal/domain/coupon/model.go`)
Add `CouponCode *string` field.

### Repository (`internal/repository/ent/coupon.go`)
Add `GetByCode(ctx context.Context, code string) (*coupon.Coupon, error)` to the `CouponRepository` interface and implementation. Normalises `code` to lowercase before querying.

### DTO (`internal/api/dto/coupon.go`)
- `CreateCouponRequest`: add optional `CouponCode *string`
- All coupon response types: expose `coupon_code`

---

## Section 2 — `SubscriptionCouponInput` DTO

New struct in `internal/api/dto/subscription.go`:

```go
// SubscriptionCouponInput is the consumer-facing coupon attachment request.
// Uses coupon_code instead of internal IDs; optionally targets a line item via price_id.
type SubscriptionCouponInput struct {
    CouponCode string     `json:"coupon_code" validate:"required"`
    StartDate  *time.Time `json:"start_date,omitempty"`
    EndDate    *time.Time `json:"end_date,omitempty"`
    PriceID    *string    `json:"price_id,omitempty"`
}
```

- `coupon_code` required
- `start_date` optional — defaults to subscription/phase `StartDate` when nil
- `end_date` optional — when nil and coupon cadence is `repeated`, the service computes it via `computeCouponEndDate` (billing-anchor-aligned)
- `price_id` optional — when set, resolved to `subscription_line_item_id` via `priceToLineItemMap`; when absent, coupon applies at subscription level

---

## Section 3 — `CreateSubscriptionRequest` changes

### New field (add alongside existing)
```go
// SubscriptionCoupons is the unified coupon input (preferred).
// Accepts coupon_code; optionally targets a specific line item via price_id.
SubscriptionCoupons []SubscriptionCouponInput `json:"subscription_coupons,omitempty"`
```

### Deprecated markers on old fields
```go
// Deprecated: use SubscriptionCoupons instead.
Coupons []string `json:"coupons,omitempty"`

// Deprecated: use SubscriptionCoupons instead.
LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`
```

Same deprecation applied to `SubscriptionPhaseCreateRequest`.

### Resolution flow (in `normalizePhaseCoupons` or a new `normalizePhaseInputCoupons` function)

For each `SubscriptionCouponInput`:
1. **Code → ID**: call `CouponRepo.GetByCode(ctx, couponCode)` — hard error if not found (coupon doesn't exist or wrong env)
2. **price_id → line_item_id**: look up in `phasePriceToLineItemMap`; if not found, log warning + skip (consistent with existing behaviour for `LineItemCoupons`)
3. **start_date nil** → use phase/subscription `StartDate`
4. **end_date nil** → leave nil; `computeCouponEndDate` in `ApplyCouponsToSubscription` handles it for `repeated` cadence

Both old and new paths funnel into `ApplyCouponsToSubscription`.

---

## Section 4 — Subscription modify API

The `AddCouponToSubscription` modify endpoint is extended to also accept `SubscriptionCouponInput` (alongside the existing `SubscriptionCouponRequest`). A new modify action field or endpoint accepts the new type; resolution follows the same code→ID + price_id→line_item_id logic as subscription creation.

Old modify path stays unchanged for backward compatibility.

---

## Section 5 — Testing

### Uniqueness tests
- Create two coupons with same `coupon_code` in same `tenant_id + environment_id` → conflict/duplicate error
- Same code in different `environment_id` → succeeds (unique index is scoped)
- `coupon_code` stored and matched case-insensitively (`"SUMMER20"` == `"summer20"`)

### Resolution tests (unit, service layer)
- `GetByCode` with existing code → resolves to correct coupon ID
- `GetByCode` with unknown code → returns not-found error
- `SubscriptionCouponInput` with valid `price_id` → association created with correct `subscription_line_item_id`
- `SubscriptionCouponInput` with unknown `price_id` → log warning, association skipped, no error

### End-to-end
- Create subscription with `subscription_coupons` using codes → associations created with correct scheduling
- Old `coupons []string` path still works → backward compatible

---

## What is NOT changing

- `CouponAssociation` schema — no changes
- `ApplyCouponsToSubscription` internal logic — unchanged (receives resolved `SubscriptionCouponRequest`)
- `computeCouponEndDate` — unchanged
- `validateRepeatedCadence` — unchanged (kept as safety net)
- Any existing API consumers using bare coupon IDs — continue to work via deprecated fields
