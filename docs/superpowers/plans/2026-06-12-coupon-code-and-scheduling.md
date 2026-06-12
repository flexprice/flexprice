# Coupon Code & Unified Scheduling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional `coupon_code` field (with partial unique index) to coupons, and introduce a unified `subscription_coupons` input that accepts coupon codes + optional scheduling + optional price-based line-item targeting, deprecating the old bare-ID fields.

**Architecture:** New `coupon_code` ent field + partial unique index → domain model + `GetByCode` repo method → DTO plumbing → `SubscriptionCouponInput` DTO → resolution in `handleSubCoupons` + `normalizePhaseCoupons` + `executeAddCoupon`, all following existing patterns. Old fields stay deprecated but functional.

**Tech Stack:** Go 1.23+, Ent ORM (entgo.io/ent), Gin, PostgreSQL, `github.com/samber/lo`

---

## File Map

| File | Change |
|------|--------|
| `ent/schema/coupon.go` | Add `coupon_code` field + partial unique index |
| `ent/` (generated) | Run `make generate-ent` |
| `migrations/postgres/` | Run `make generate-migration` |
| `internal/domain/coupon/model.go` | Add `CouponCode *string`; update `FromEnt` |
| `internal/domain/coupon/repository.go` | Add `GetByCode` to interface |
| `internal/repository/ent/coupon.go` | Implement `GetByCode`; add code to `Create`/`Update` |
| `internal/api/dto/coupon.go` | Add `CouponCode` to `CreateCouponRequest`, `UpdateCouponRequest`; expose in response |
| `internal/service/coupon.go` | Pass `CouponCode` through `UpdateCoupon` |
| `internal/api/dto/subscription.go` | Add `SubscriptionCouponInput`; add `SubscriptionCoupons` to `CreateSubscriptionRequest` + `SubscriptionPhaseCreateRequest`; deprecate old fields |
| `internal/service/subscription.go` | Update `handleSubCoupons` + `normalizePhaseCoupons` to handle `SubscriptionCouponInput` |
| `internal/api/dto/subscription_modification.go` | Add `CouponCode`, `StartDate`, `EndDate`, `PriceID` to `SubModifyCouponParams`; deprecate `CouponID` |
| `internal/service/subscription_modification_coupon.go` | Resolve code→ID + price→line-item in `executeAddCoupon` |
| `internal/service/coupon_test.go` (new) | Uniqueness + GetByCode tests |
| `internal/service/subscription_test.go` or coupon_association_test.go | Resolution tests |

---

## Task 1: Ent schema — add `coupon_code` field and partial unique index

**Files:**
- Modify: `ent/schema/coupon.go`

- [ ] **Step 1: Read the current schema to confirm exact import block**

```bash
head -15 ent/schema/coupon.go
```

- [ ] **Step 2: Add the field and index**

In `ent/schema/coupon.go`, add the import for `entsql`:

```go
import (
    "entgo.io/ent"
    "entgo.io/ent/dialect/entsql"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
    baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
    "github.com/shopspring/decimal"
)
```

In `Fields()`, add after the existing `metadata` field:

```go
field.String("coupon_code").
    SchemaType(map[string]string{
        "postgres": "varchar(100)",
    }).
    Optional().
    Nillable().
    Comment("Human-readable coupon code (e.g. SUMMER20). Stored lowercase. Unique per tenant+environment when published."),
```

In `Indexes()`, replace:

```go
func (Coupon) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("tenant_id", "environment_id"),
        index.Fields("tenant_id", "environment_id", "coupon_code").
            Unique().
            Annotations(entsql.IndexWhere("coupon_code IS NOT NULL AND coupon_code != '' AND status = 'published'")).
            StorageKey("idx_coupon_tenant_environment_coupon_code_unique"),
    }
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./ent/...
```

Expected: no errors (ent schema is just Go structs; the generated code comes next).

---

## Task 2: Generate ent code and migration

**Files:**
- `ent/` (generated files updated in-place)
- `migrations/postgres/` (new SQL file)

- [ ] **Step 1: Regenerate ent code**

```bash
make generate-ent
```

Expected: `ent/` files regenerated. `ent/coupon.go` will now have `CouponCode *string` field and `SetNillableCouponCode` builder method.

- [ ] **Step 2: Preview the migration SQL**

```bash
make migrate-ent-dry-run
```

Expected output includes something like:
```sql
ALTER TABLE "coupons" ADD COLUMN "coupon_code" varchar(100) NULL;
CREATE UNIQUE INDEX "idx_coupon_tenant_environment_coupon_code_unique" ON "coupons" ("tenant_id", "environment_id", "coupon_code") WHERE (coupon_code IS NOT NULL AND coupon_code != '' AND status = 'published');
```

- [ ] **Step 3: Generate the migration file**

```bash
make generate-migration
```

Expected: new `.sql` file in `migrations/postgres/`.

- [ ] **Step 4: Apply migration locally**

```bash
make migrate-ent
```

Expected: migration applies without error.

- [ ] **Step 5: Confirm build still passes**

```bash
go build ./...
```

---

## Task 3: Domain model — add `CouponCode` field and update `FromEnt`

**Files:**
- Modify: `internal/domain/coupon/model.go`

- [ ] **Step 1: Add `CouponCode *string` to the struct**

In `internal/domain/coupon/model.go`, add after the `Currency` field:

```go
CouponCode        *string                 `json:"coupon_code,omitempty" db:"coupon_code"`
```

- [ ] **Step 2: Update `FromEnt` to map the new field**

In `FromEnt`, add after `Currency: lo.FromPtrOr(e.Currency, ""),`:

```go
CouponCode:        e.CouponCode,
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/domain/coupon/...
```

---

## Task 4: Repository interface — add `GetByCode`

**Files:**
- Modify: `internal/domain/coupon/repository.go`

- [ ] **Step 1: Add method to interface**

Replace the current `Repository` interface with:

```go
type Repository interface {
    Create(ctx context.Context, coupon *Coupon) error
    Get(ctx context.Context, id string) (*Coupon, error)
    GetByCode(ctx context.Context, code string) (*Coupon, error)
    Update(ctx context.Context, coupon *Coupon) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, filter *types.CouponFilter) ([]*Coupon, error)
    Count(ctx context.Context, filter *types.CouponFilter) (int, error)
    IncrementRedemptions(ctx context.Context, id string) error
}
```

- [ ] **Step 2: Verify the interface compiles (impl will break until Task 5)**

```bash
go build ./internal/domain/coupon/... 2>&1 | grep -v "does not implement"
```

---

## Task 5: Repository implementation — `GetByCode`, `Create`, `Update`

**Files:**
- Modify: `internal/repository/ent/coupon.go`

- [ ] **Step 1: Add `strings` import if not present**

Check imports at the top of the file; add `"strings"` if missing.

- [ ] **Step 2: Add `GetByCode` method**

Add after the `Get` method (around line 152):

```go
func (r *couponRepository) GetByCode(ctx context.Context, code string) (*domainCoupon.Coupon, error) {
    span := StartRepositorySpan(ctx, "coupon", "get_by_code", map[string]interface{}{
        "coupon_code": code,
    })
    defer FinishSpan(span)

    normalised := strings.ToLower(strings.TrimSpace(code))
    if normalised == "" {
        return nil, ierr.NewError("coupon_code is required").
            Mark(ierr.ErrValidation)
    }

    client := r.client.Reader(ctx)
    c, err := client.Coupon.Query().
        Where(
            coupon.CouponCode(normalised),
            coupon.TenantID(types.GetTenantID(ctx)),
            coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
        ).
        Only(ctx)

    if err != nil {
        SetSpanError(span, err)
        if ent.IsNotFound(err) {
            return nil, ierr.WithError(err).
                WithHintf("Coupon with code '%s' was not found", code).
                WithReportableDetails(map[string]any{"coupon_code": code}).
                Mark(ierr.ErrNotFound)
        }
        return nil, ierr.WithError(err).
            WithHint("Failed to get coupon by code").
            Mark(ierr.ErrDatabase)
    }

    SetSpanSuccess(span)
    return domainCoupon.FromEnt(c), nil
}
```

- [ ] **Step 3: Update `Create` to store lowercase `coupon_code`**

In the `createQuery` builder inside `Create`, after `SetNillableDurationInPeriods`:

```go
if c.CouponCode != nil && *c.CouponCode != "" {
    normalised := strings.ToLower(strings.TrimSpace(*c.CouponCode))
    createQuery = createQuery.SetCouponCode(normalised)
}
```

Also update the `ent.IsConstraintError` hint to be more generic:

```go
if ent.IsConstraintError(err) {
    return ierr.WithError(err).
        WithHint("A published coupon with this code already exists in this environment").
        WithReportableDetails(map[string]any{
            "name":        c.Name,
            "coupon_code": c.CouponCode,
        }).
        Mark(ierr.ErrAlreadyExists)
}
```

- [ ] **Step 4: Update `Update` to allow changing `coupon_code`**

In `Update`, find the `updateQuery` builder. Add after existing field updates (look for `.SetUpdatedAt`):

```go
if c.CouponCode != nil {
    if *c.CouponCode == "" {
        updateQuery = updateQuery.ClearCouponCode()
    } else {
        updateQuery = updateQuery.SetCouponCode(strings.ToLower(strings.TrimSpace(*c.CouponCode)))
    }
}
```

- [ ] **Step 5: Verify build**

```bash
go build ./internal/repository/...
```

---

## Task 6: DTO — add `CouponCode` to coupon create/update/response

**Files:**
- Modify: `internal/api/dto/coupon.go`

- [ ] **Step 1: Add `CouponCode` to `CreateCouponRequest`**

```go
type CreateCouponRequest struct {
    Name              string                  `json:"name" validate:"required"`
    CouponCode        *string                 `json:"coupon_code,omitempty"`
    RedeemAfter       *time.Time              `json:"redeem_after,omitempty"`
    RedeemBefore      *time.Time              `json:"redeem_before,omitempty"`
    MaxRedemptions    *int                    `json:"max_redemptions,omitempty"`
    Rules             *map[string]interface{} `json:"rules,omitempty"`
    AmountOff         *decimal.Decimal        `json:"amount_off,omitempty" swaggertype:"string"`
    PercentageOff     *decimal.Decimal        `json:"percentage_off,omitempty" swaggertype:"string"`
    Type              types.CouponType        `json:"type" validate:"required,oneof=fixed percentage"`
    Cadence           types.CouponCadence     `json:"cadence" validate:"required,oneof=once repeated forever"`
    DurationInPeriods *int                    `json:"duration_in_periods,omitempty"`
    Metadata          *map[string]string      `json:"metadata,omitempty"`
    Currency          *string                 `json:"currency,omitempty"`
}
```

- [ ] **Step 2: Add `CouponCode` to `UpdateCouponRequest`**

```go
type UpdateCouponRequest struct {
    Name       *string            `json:"name,omitempty"`
    CouponCode *string            `json:"coupon_code,omitempty"`
    Metadata   *map[string]string `json:"metadata,omitempty"`
}
```

- [ ] **Step 3: Update `ToCoupon` to pass `CouponCode`**

In `ToCoupon`, add to the returned `&coupon.Coupon{}`:

```go
CouponCode:        r.CouponCode,
```

- [ ] **Step 4: Verify build**

```bash
go build ./internal/api/dto/...
```

---

## Task 7: Service — pass `CouponCode` through `UpdateCoupon`

**Files:**
- Modify: `internal/service/coupon.go`

- [ ] **Step 1: Update `UpdateCoupon` to handle the new field**

In `UpdateCoupon` (around line 62), after the `req.Name` block, add:

```go
if req.CouponCode != nil {
    c.CouponCode = req.CouponCode
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/service/...
```

---

## Task 8: DTO — `SubscriptionCouponInput` and deprecated markers

**Files:**
- Modify: `internal/api/dto/subscription.go`

- [ ] **Step 1: Add `SubscriptionCouponInput` struct**

Add after the existing `SubscriptionCouponRequest` struct (around line 268):

```go
// SubscriptionCouponInput is the preferred coupon attachment request.
// Identifies a coupon by human-readable code (never internal ID).
// Optionally targets a specific subscription line item via price_id.
type SubscriptionCouponInput struct {
    // Required. The coupon's human-readable code (case-insensitive).
    CouponCode string     `json:"coupon_code" validate:"required"`
    // Optional. When the coupon starts; defaults to subscription/phase StartDate.
    StartDate  *time.Time `json:"start_date,omitempty"`
    // Optional. When the coupon ends; overrides duration_in_periods calculation.
    EndDate    *time.Time `json:"end_date,omitempty"`
    // Optional. Price ID of the line item to target; omit for subscription-level.
    PriceID    *string    `json:"price_id,omitempty"`
}

// Validate validates SubscriptionCouponInput.
func (r *SubscriptionCouponInput) Validate() error {
    if r.CouponCode == "" {
        return ierr.NewError("coupon_code is required").
            WithHint("Provide a valid coupon code").
            Mark(ierr.ErrValidation)
    }
    if r.EndDate != nil && r.StartDate != nil && r.EndDate.Before(*r.StartDate) {
        return ierr.NewError("end_date cannot be before start_date").
            Mark(ierr.ErrValidation)
    }
    return nil
}
```

- [ ] **Step 2: Add `SubscriptionCoupons` to `CreateSubscriptionRequest` and deprecate old fields**

In `CreateSubscriptionRequest` (around lines 421-423), replace:

```go
Coupons []string `json:"coupons,omitempty"`

LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`
```

with:

```go
// SubscriptionCoupons is the preferred way to attach coupons at creation.
// Accepts coupon_code; optionally targets a line item via price_id.
SubscriptionCoupons []SubscriptionCouponInput `json:"subscription_coupons,omitempty"`

// Deprecated: use SubscriptionCoupons instead.
Coupons []string `json:"coupons,omitempty"`

// Deprecated: use SubscriptionCoupons instead.
LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`
```

- [ ] **Step 3: Add `SubscriptionCoupons` to `SubscriptionPhaseCreateRequest` and deprecate old fields**

In `SubscriptionPhaseCreateRequest` (lines 24-28), replace:

```go
// Coupons represents subscription-level coupons to be applied to this phase
Coupons []string `json:"coupons,omitempty"`

// LineItemCoupons represents line item-level coupons (map of line_item_id to coupon IDs)
LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`
```

with:

```go
// SubscriptionCoupons is the preferred way to attach coupons to this phase.
SubscriptionCoupons []SubscriptionCouponInput `json:"subscription_coupons,omitempty"`

// Deprecated: use SubscriptionCoupons instead.
Coupons []string `json:"coupons,omitempty"`

// Deprecated: use SubscriptionCoupons instead.
LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`
```

- [ ] **Step 4: Verify build**

```bash
go build ./internal/api/dto/...
```

---

## Task 9: Subscription service — resolve `SubscriptionCouponInput` in `handleSubCoupons`

**Files:**
- Modify: `internal/service/subscription.go`

Context: `handleSubCoupons` is at line ~4178. It currently reads `req.Coupons` and `req.LineItemCoupons`. We add a new block that processes `req.SubscriptionCoupons` the same way, resolving `coupon_code → coupon_id` via `GetByCode`.

- [ ] **Step 1: Add resolution block in `handleSubCoupons`**

Inside `handleSubCoupons`, after the existing `req.LineItemCoupons` loop (before the `if len(subscriptionCoupons) == 0` check), add:

```go
// Process new SubscriptionCoupons (preferred path): resolve code → ID, price → line item
for _, input := range req.SubscriptionCoupons {
    if err := input.Validate(); err != nil {
        return ierr.WithError(err).
            WithHint("Invalid subscription_coupons entry").
            Mark(ierr.ErrValidation)
    }
    c, err := s.CouponRepo.GetByCode(ctx, input.CouponCode)
    if err != nil {
        return ierr.WithError(err).
            WithHintf("Coupon with code '%s' not found", input.CouponCode).
            Mark(ierr.ErrNotFound)
    }
    startDate := sub.StartDate
    if input.StartDate != nil {
        startDate = *input.StartDate
    }
    couponReq := dto.SubscriptionCouponRequest{
        CouponID:  c.ID,
        StartDate: startDate,
        EndDate:   input.EndDate,
    }
    if input.PriceID != nil {
        if lineItemID, exists := originalPriceToLineItemMap[*input.PriceID]; exists {
            couponReq.LineItemID = lo.ToPtr(lineItemID)
        } else {
            s.Logger.Info(ctx, "subscription_coupons price_id not found in line items, skipping line-item targeting",
                "price_id", *input.PriceID,
                "coupon_code", input.CouponCode,
                "subscription_id", sub.ID)
        }
    }
    subscriptionCoupons = append(subscriptionCoupons, couponReq)
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/service/...
```

- [ ] **Step 3: Commit**

```bash
git add ent/schema/coupon.go ent/ migrations/ \
        internal/domain/coupon/ \
        internal/repository/ent/coupon.go \
        internal/api/dto/coupon.go internal/api/dto/subscription.go \
        internal/service/coupon.go internal/service/subscription.go
git commit -m "feat: add coupon_code field and SubscriptionCouponInput with code-based resolution"
```

---

## Task 10: Subscription service — resolve `SubscriptionCouponInput` in `normalizePhaseCoupons`

**Files:**
- Modify: `internal/service/subscription.go`

Context: `normalizePhaseCoupons` is at line ~1081. It takes `phaseReq dto.SubscriptionPhaseCreateRequest` which now also has `SubscriptionCoupons []SubscriptionCouponInput`. The function needs the coupon repo for code resolution, so we add `ctx context.Context` to its signature.

- [ ] **Step 1: Update `normalizePhaseCoupons` signature to accept `ctx`**

Change:

```go
func (s *subscriptionService) normalizePhaseCoupons(
    phaseReq dto.SubscriptionPhaseCreateRequest,
    phaseID string,
    phasePriceToLineItemMap map[string]string,
) []dto.SubscriptionCouponRequest {
```

to:

```go
func (s *subscriptionService) normalizePhaseCoupons(
    ctx context.Context,
    phaseReq dto.SubscriptionPhaseCreateRequest,
    phaseID string,
    phasePriceToLineItemMap map[string]string,
) []dto.SubscriptionCouponRequest {
```

- [ ] **Step 2: Add resolution block for `SubscriptionCoupons` in `normalizePhaseCoupons`**

After the existing `phaseReq.LineItemCoupons` loop, add:

```go
// Process new SubscriptionCoupons (preferred path)
for _, input := range phaseReq.SubscriptionCoupons {
    if input.CouponCode == "" {
        continue
    }
    c, err := s.CouponRepo.GetByCode(ctx, input.CouponCode)
    if err != nil {
        s.Logger.Info(ctx, "phase subscription_coupons code not found, skipping",
            "coupon_code", input.CouponCode,
            "phase_id", phaseID)
        continue
    }
    startDate := phaseReq.StartDate
    if input.StartDate != nil {
        startDate = *input.StartDate
    }
    endDate := phaseReq.EndDate
    if input.EndDate != nil {
        endDate = input.EndDate
    }
    couponReq := dto.SubscriptionCouponRequest{
        CouponID:            c.ID,
        SubscriptionPhaseID: lo.ToPtr(phaseID),
        StartDate:           startDate,
        EndDate:             endDate,
    }
    if input.PriceID != nil {
        if lineItemID, exists := phasePriceToLineItemMap[*input.PriceID]; exists {
            couponReq.LineItemID = lo.ToPtr(lineItemID)
        } else {
            s.Logger.Info(ctx, "phase subscription_coupons price_id not found, skipping line-item targeting",
                "price_id", *input.PriceID,
                "coupon_code", input.CouponCode,
                "phase_id", phaseID)
        }
    }
    subscriptionCoupons = append(subscriptionCoupons, couponReq)
}
```

- [ ] **Step 3: Fix all call sites of `normalizePhaseCoupons` to pass `ctx`**

Search for all calls:

```bash
grep -n "normalizePhaseCoupons" internal/service/subscription.go
```

Update each call from `s.normalizePhaseCoupons(phaseReq, ...)` to `s.normalizePhaseCoupons(ctx, phaseReq, ...)`.

- [ ] **Step 4: Verify build**

```bash
go build ./internal/service/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/subscription.go
git commit -m "feat: resolve SubscriptionCouponInput in phase normalization"
```

---

## Task 11: Subscription modify — `coupon_code` + scheduling in `SubModifyCouponParams`

**Files:**
- Modify: `internal/api/dto/subscription_modification.go`
- Modify: `internal/service/subscription_modification_coupon.go`

### Part A — DTO

- [ ] **Step 1: Extend `SubModifyCouponParams`**

Replace:

```go
type SubModifyCouponParams struct {
    Action        SubModifyCouponAction `json:"action" binding:"required"`
    CouponID      *string               `json:"coupon_id,omitempty"`
    AssociationID *string               `json:"association_id,omitempty"`
    EffectiveDate *time.Time            `json:"effective_date,omitempty"`
}
```

with:

```go
type SubModifyCouponParams struct {
    // Required. "add" to attach; "remove" to detach.
    Action SubModifyCouponAction `json:"action" binding:"required"`
    // CouponCode is the preferred way to identify the coupon for action="add".
    CouponCode *string `json:"coupon_code,omitempty"`
    // Deprecated: use coupon_code instead.
    CouponID *string `json:"coupon_id,omitempty"`
    // Required when action="remove". ID of the CouponAssociation to soft-delete.
    AssociationID *string `json:"association_id,omitempty"`
    // Optional. When to apply the change; defaults to now.
    EffectiveDate *time.Time `json:"effective_date,omitempty"`
    // Optional. When the coupon association starts; defaults to EffectiveDate.
    StartDate *time.Time `json:"start_date,omitempty"`
    // Optional. When the coupon association ends; overrides duration_in_periods.
    EndDate *time.Time `json:"end_date,omitempty"`
    // Optional. Price ID of the line item to target; omit for subscription-level.
    PriceID *string `json:"price_id,omitempty"`
}
```

- [ ] **Step 2: Update `Validate` to accept either `coupon_code` or `coupon_id` for action=add**

```go
func (r *SubModifyCouponParams) Validate() error {
    switch r.Action {
    case SubModifyCouponActionAdd:
        if (r.CouponCode == nil || *r.CouponCode == "") &&
            (r.CouponID == nil || *r.CouponID == "") {
            return ierr.NewError("coupon_code (or deprecated coupon_id) is required for action 'add'").
                WithHint("Provide a valid coupon_code").
                Mark(ierr.ErrValidation)
        }
    case SubModifyCouponActionRemove:
        if r.AssociationID == nil || *r.AssociationID == "" {
            return ierr.NewError("association_id is required for action 'remove'").
                WithHint("Provide the coupon association ID to remove").
                Mark(ierr.ErrValidation)
        }
    default:
        return ierr.NewError("unknown coupon action: " + string(r.Action)).
            WithHint("Valid values: add, remove").
            Mark(ierr.ErrValidation)
    }
    return nil
}
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/api/dto/...
```

### Part B — Service

- [ ] **Step 4: Update `executeCouponModification` to pass new params**

In `executeCouponModification`, replace:

```go
case dto.SubModifyCouponActionAdd:
    return s.executeAddCoupon(ctx, subscriptionID, *params.CouponID, effectiveDate)
```

with:

```go
case dto.SubModifyCouponActionAdd:
    return s.executeAddCoupon(ctx, subscriptionID, params, effectiveDate)
```

- [ ] **Step 5: Rewrite `executeAddCoupon` signature and body**

Replace the `executeAddCoupon` signature from:

```go
func (s *subscriptionModificationService) executeAddCoupon(
    ctx context.Context,
    subscriptionID string,
    couponID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
```

to:

```go
func (s *subscriptionModificationService) executeAddCoupon(
    ctx context.Context,
    subscriptionID string,
    params *dto.SubModifyCouponParams,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
```

At the top of the body, resolve the coupon (replace the existing `sp.CouponRepo.Get(ctx, couponID)` block):

```go
sp := s.serviceParams

subSvc := NewSubscriptionService(sp)
subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
if err != nil {
    return nil, err
}

// Resolve coupon: prefer coupon_code; fall back to deprecated coupon_id
var couponID string
if params.CouponCode != nil && *params.CouponCode != "" {
    c, err := sp.CouponRepo.GetByCode(ctx, *params.CouponCode)
    if err != nil {
        return nil, ierr.NewError("coupon not found").
            WithHintf("No published coupon with code '%s'", *params.CouponCode).
            Mark(ierr.ErrValidation)
    }
    if c.Status != types.StatusPublished {
        return nil, ierr.NewError("coupon not found or inactive").
            Mark(ierr.ErrValidation)
    }
    couponID = c.ID
} else {
    c, err := sp.CouponRepo.Get(ctx, *params.CouponID)
    if err != nil || c.Status != types.StatusPublished {
        return nil, ierr.NewError("coupon not found or inactive").
            WithHint("Provide a valid, active coupon_id or coupon_code").
            Mark(ierr.ErrValidation)
    }
    couponID = c.ID
}

// Resolve price_id → line_item_id
var lineItemID *string
if params.PriceID != nil {
    priceToLI := make(map[string]string)
    for _, li := range subResp.LineItems {
        priceToLI[li.PriceID] = li.ID
    }
    if liID, ok := priceToLI[*params.PriceID]; ok {
        lineItemID = lo.ToPtr(liID)
    } else {
        sp.Logger.Info(ctx, "modify coupon price_id not found in line items, applying at subscription level",
            "price_id", *params.PriceID,
            "subscription_id", subscriptionID)
    }
}

startDate := effectiveDate
if params.StartDate != nil {
    startDate = params.StartDate.UTC()
}
```

Then replace the existing association creation call further down with:

```go
createReq := dto.CreateCouponAssociationRequest{
    CouponID:               couponID,
    SubscriptionID:         subscriptionID,
    SubscriptionLineItemID: lineItemID,
    StartDate:              startDate,
    EndDate:                params.EndDate,
    Metadata:               map[string]string{},
}
caService := NewCouponAssociationService(sp)
assoc, err := caService.CreateCouponAssociation(ctx, createReq)
if err != nil {
    return nil, err
}
```

> **Note:** The existing body has more validation (duplicate-association check, etc.). Keep all of that logic — only replace the coupon resolution block and the final `CreateCouponAssociation` call to use the new variables. Do not remove the active-association duplicate check.

- [ ] **Step 6: Add `lo` import if missing**

```go
import "github.com/samber/lo"
```

- [ ] **Step 7: Verify build**

```bash
go build ./internal/service/...
```

- [ ] **Step 8: Commit**

```bash
git add internal/api/dto/subscription_modification.go \
        internal/service/subscription_modification_coupon.go
git commit -m "feat: add coupon_code and scheduling to subscription modify coupon params"
```

---

## Task 12: Tests

**Files:**
- Create: `internal/service/coupon_code_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package service_test

import (
    "context"
    "strings"
    "testing"

    "github.com/flexprice/flexprice/internal/testutil"
    "github.com/flexprice/flexprice/internal/api/dto"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
    "github.com/stretchr/testify/suite"
)

type CouponCodeTestSuite struct {
    testutil.BaseIntegrationTestSuite
}

func TestCouponCodeSuite(t *testing.T) {
    suite.Run(t, new(CouponCodeTestSuite))
}

func (s *CouponCodeTestSuite) TestGetByCode_CaseInsensitive() {
    ctx := s.GetContext()
    svc := s.NewCouponService()

    _, err := svc.CreateCoupon(ctx, dto.CreateCouponRequest{
        Name:          "Summer Sale",
        CouponCode:    ptrStr("SUMMER20"),
        Type:          types.CouponTypePercentage,
        Cadence:       types.CouponCadenceOnce,
        PercentageOff: ptrDec("20"),
    })
    s.Require().NoError(err)

    // Lookup with lowercase
    c, err := s.CouponRepo.GetByCode(ctx, "summer20")
    s.Require().NoError(err)
    s.Require().Equal("summer20", *c.CouponCode)

    // Lookup with uppercase
    c, err = s.CouponRepo.GetByCode(ctx, "SUMMER20")
    s.Require().NoError(err)
    s.Require().NotNil(c)
}

func (s *CouponCodeTestSuite) TestGetByCode_NotFound() {
    ctx := s.GetContext()
    _, err := s.CouponRepo.GetByCode(ctx, "NONEXISTENT")
    s.Require().Error(err)
}

func (s *CouponCodeTestSuite) TestCouponCode_UniquenessPublished() {
    ctx := s.GetContext()
    svc := s.NewCouponService()

    createReq := dto.CreateCouponRequest{
        Name:          "Launch Promo",
        CouponCode:    ptrStr("LAUNCH50"),
        Type:          types.CouponTypePercentage,
        Cadence:       types.CouponCadenceOnce,
        PercentageOff: ptrDec("50"),
    }
    _, err := svc.CreateCoupon(ctx, createReq)
    s.Require().NoError(err)

    // Publish first coupon (update status to published so the partial index fires)
    // The test helper or a direct repo call sets status=published
    // NOTE: CreateCoupon sets status=published by default via GetDefaultBaseModel.

    // Creating a second published coupon with the same code must fail
    createReq.Name = "Launch Promo 2"
    _, err = svc.CreateCoupon(ctx, createReq)
    s.Require().Error(err, "expected conflict on duplicate published coupon_code")
}

func (s *CouponCodeTestSuite) TestCouponCode_SameCodeDraftAllowed() {
    ctx := s.GetContext()
    // Two draft coupons with the same code should not conflict.
    // Create both with status=draft via direct repo (bypassing service which sets published).
    code := ptrStr("draftcode")
    err := s.CouponRepo.Create(ctx, makeDraftCoupon(ctx, "Draft1", code))
    s.Require().NoError(err)
    err = s.CouponRepo.Create(ctx, makeDraftCoupon(ctx, "Draft2", code))
    s.Require().NoError(err, "two draft coupons with same code should be allowed")
}

// helpers
func ptrStr(s string) *string { return &s }
func ptrDec(v string) *decimal.Decimal {
    d, _ := decimal.NewFromString(v)
    return &d
}
func makeDraftCoupon(ctx context.Context, name string, code *string) *domainCoupon.Coupon {
    c := &domainCoupon.Coupon{
        ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
        Name:          name,
        CouponCode:    code,
        Type:          types.CouponTypePercentage,
        Cadence:       types.CouponCadenceOnce,
        PercentageOff: ptrDec("10"),
        BaseModel:     types.GetDefaultBaseModel(ctx),
        EnvironmentID: types.GetEnvironmentID(ctx),
    }
    c.Status = types.StatusDraft // override published default
    if c.CouponCode != nil {
        lower := strings.ToLower(*c.CouponCode)
        c.CouponCode = &lower
    }
    return c
}
```

- [ ] **Step 2: Run tests to confirm they fail (before implementation is complete)**

```bash
go test -v -race ./internal/service/... -run TestCouponCodeSuite 2>&1 | tail -20
```

Expected: compilation or runtime failures since `GetByCode` isn't wired yet.

- [ ] **Step 3: Run tests after all tasks are complete**

```bash
go test -v -race ./internal/service/... -run TestCouponCodeSuite
```

Expected: all pass.

- [ ] **Step 4: Commit tests**

```bash
git add internal/service/coupon_code_test.go
git commit -m "test: add coupon_code uniqueness and GetByCode tests"
```

---

## Task 13: Swagger

**Files:**
- Generated: `docs/swagger/`

- [ ] **Step 1: Regenerate swagger docs**

```bash
make swagger
```

- [ ] **Step 2: Verify swagger contains new fields**

```bash
grep -A3 "coupon_code" docs/swagger/swagger.json | head -20
grep "subscription_coupons" docs/swagger/swagger.json | head -5
```

Expected: both fields appear in the output.

- [ ] **Step 3: Final build + vet**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 4: Commit**

```bash
git add docs/swagger/
git commit -m "docs: regenerate swagger with coupon_code and subscription_coupons"
```

---

## Self-Review

**Spec coverage:**
- ✅ `coupon_code` field on Coupon (Task 1-6)
- ✅ Partial unique index matching customer pattern (Task 1)
- ✅ Stored lowercase (Task 5)
- ✅ `GetByCode` method (Task 4-5)
- ✅ `SubscriptionCouponInput` DTO (Task 8)
- ✅ `subscription_coupons` on `CreateSubscriptionRequest` + `SubscriptionPhaseCreateRequest` (Task 8)
- ✅ Old fields deprecated (Task 8)
- ✅ Resolution in subscription creation `handleSubCoupons` (Task 9)
- ✅ Resolution in `normalizePhaseCoupons` (Task 10)
- ✅ Modify endpoint extended with `coupon_code` + scheduling + `price_id` (Task 11)
- ✅ Uniqueness tests: published conflict, draft ok, case-insensitive lookup (Task 12)
- ✅ Swagger regenerated (Task 13)

**Type consistency:**
- `SubscriptionCouponInput.CouponCode string` used in Tasks 8, 9, 10, 11 ✅
- `CouponRepository.GetByCode(ctx, code string) (*Coupon, error)` defined in Task 4, implemented in Task 5, called in Tasks 9, 10, 11 ✅
- `SubModifyCouponParams.CouponCode *string` defined in Task 11 Part A, used in Task 11 Part B ✅
