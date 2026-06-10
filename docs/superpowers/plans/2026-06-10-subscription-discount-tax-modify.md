# Subscription Discount & Tax Modification — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the subscription modify API to add/remove coupons and tax associations post-subscription-creation, with `effective_date` support for both past and future dates.

**Architecture:** Two new `SubscriptionModifyType` values (`coupon`, `tax`) are added to the existing preview/execute modify pipeline. Coupon removal soft-deletes via `CouponAssociation.EndDate`. Tax removal requires adding `start_date`/`end_date` fields to `TaxAssociation` first (schema change), then soft-deletes the same way. Preview and execute share the same validation logic, differing only on whether DB writes happen.

**Tech Stack:** Go 1.23+, Ent ORM, PostgreSQL, `ierr` error package, `types.GetDefaultBaseModel`, `testify/suite` for tests.

**Spec:** `docs/superpowers/specs/2026-06-10-subscription-discount-tax-modify-design.md`

---

## File Map

| File | Action |
|------|--------|
| `ent/schema/taxassociation.go` | Modify — add `start_date`, `end_date` fields; drop unique index |
| `internal/domain/taxassociation/model.go` | Modify — add `StartDate`, `EndDate` fields; update `FromEnt` |
| `internal/repository/ent/taxassociation.go` | Modify — set `start_date` on Create; set `end_date` on Update |
| `internal/api/dto/subscription_modification.go` | Modify — add `SubModifyAction`, `SubModifyCouponParams`, `SubModifyTaxParams`; extend request + validate |
| `internal/service/subscription_modification.go` | Modify — add routing cases for `coupon` and `tax` |
| `internal/service/subscription_modification_coupon.go` | Create — coupon add/remove execute + preview |
| `internal/service/subscription_modification_tax.go` | Create — tax add/remove execute + preview |
| `internal/service/subscription_modification_coupon_test.go` | Create — table-driven coupon tests |
| `internal/service/subscription_modification_tax_test.go` | Create — table-driven tax tests |

---

## Task 1: Add `start_date` and `end_date` to TaxAssociation Ent Schema

**Files:**
- Modify: `ent/schema/taxassociation.go`

`CouponAssociation` already has `start_date`/`end_date`. `TaxAssociation` does not. We add them now and drop the unique constraint that prevents re-adding the same tax rate to the same subscription (time-bounded associations make it obsolete).

- [ ] **Step 1: Edit the schema**

Open `ent/schema/taxassociation.go`. Add two fields to `Fields()` and remove the `Unique_entity_tax_mapping` index from `Indexes()`:

```go
// In Fields(), add after the metadata field:
field.Time("start_date").
    Immutable().
    Default(time.Now).
    Comment("When this tax association becomes effective"),

field.Time("end_date").
    Optional().
    Nillable().
    Comment("When this tax association stops being effective"),
```

Add `"time"` to the import block at the top of the file.

In `Indexes()`, remove this entry entirely:

```go
// DELETE this block:
index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "tax_rate_id").
    StorageKey(Unique_entity_tax_mapping).
    Unique().
    Annotations(entsql.IndexWhere("status = 'published'")),
```

Also remove the now-unused `Unique_entity_tax_mapping` constant from the `const` block at the top of the file, and remove the `entsql` import if it's no longer used.

- [ ] **Step 2: Run code generation**

```bash
make generate-ent
```

Expected: no errors; files under `ent/` regenerate (including `ent/taxassociation/`, `ent/mutation.go`, `ent/client.go`).

- [ ] **Step 3: Generate migration SQL**

```bash
make generate-migration
```

Expected: a new file appears under `migrations/postgres/` containing `ALTER TABLE tax_associations ADD COLUMN start_date ...`, `ADD COLUMN end_date ...`, and `DROP INDEX unique_entity_tax_mapping`.

- [ ] **Step 4: Vet**

```bash
go vet ./ent/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add ent/schema/taxassociation.go ent/ migrations/postgres/
git commit -m "feat: add start_date/end_date to tax_associations schema, drop unique constraint"
```

---

## Task 2: Update TaxAssociation Domain Model and Repository

**Files:**
- Modify: `internal/domain/taxassociation/model.go`
- Modify: `internal/repository/ent/taxassociation.go`

- [ ] **Step 1: Update the domain model**

In `internal/domain/taxassociation/model.go`, add two fields to `TaxAssociation`:

```go
type TaxAssociation struct {
    ID            string                  `json:"id,omitempty"`
    TaxRateID     string                  `json:"tax_rate_id,omitempty"`
    EntityType    types.TaxRateEntityType `json:"entity_type,omitempty"`
    EntityID      string                  `json:"entity_id,omitempty"`
    Priority      int                     `json:"priority,omitempty"`
    AutoApply     bool                    `json:"auto_apply,omitempty"`
    Currency      string                  `json:"currency,omitempty"`
    Metadata      map[string]string       `json:"metadata,omitempty"`
    StartDate     time.Time               `json:"start_date,omitempty"`   // ADD
    EndDate       *time.Time              `json:"end_date,omitempty"`      // ADD
    EnvironmentID string                  `json:"environment_id,omitempty"`
    types.BaseModel
}
```

Add `"time"` to the import block.

Update `FromEnt` to map the new fields:

```go
func FromEnt(e *ent.TaxAssociation) *TaxAssociation {
    return &TaxAssociation{
        ID:            e.ID,
        TaxRateID:     e.TaxRateID,
        EntityType:    types.TaxRateEntityType(e.EntityType),
        EntityID:      e.EntityID,
        Priority:      e.Priority,
        AutoApply:     e.AutoApply,
        Currency:      e.Currency,
        Metadata:      e.Metadata,
        StartDate:     e.StartDate,   // ADD
        EndDate:       e.EndDate,     // ADD
        EnvironmentID: e.EnvironmentID,
        BaseModel: types.BaseModel{
            TenantID:  e.TenantID,
            Status:    types.Status(e.Status),
            CreatedAt: e.CreatedAt,
            UpdatedAt: e.UpdatedAt,
            CreatedBy: e.CreatedBy,
            UpdatedBy: e.UpdatedBy,
        },
    }
}
```

- [ ] **Step 2: Update the repository Create() to set start_date**

In `internal/repository/ent/taxassociation.go`, in the `Create()` method, add `SetStartDate` to the builder chain. The field is `Immutable` so it can only be set at create time:

```go
_, err := client.TaxAssociation.Create().
    SetID(t.ID).
    SetTaxRateID(t.TaxRateID).
    SetEntityType(string(t.EntityType)).
    SetCurrency(t.Currency).
    SetPriority(t.Priority).
    SetAutoApply(t.AutoApply).
    SetMetadata(t.Metadata).
    SetEnvironmentID(t.EnvironmentID).
    SetEntityID(t.EntityID).
    SetCreatedAt(t.CreatedAt).
    SetUpdatedAt(t.UpdatedAt).
    SetCreatedBy(t.CreatedBy).
    SetTenantID(t.TenantID).
    SetUpdatedBy(t.UpdatedBy).
    SetStartDate(t.StartDate).        // ADD — set from domain model
    Save(ctx)
```

- [ ] **Step 3: Update the repository Update() to support end_date**

In the same file, in the `Update()` method, add `end_date` support after the existing `SetMetadata` call:

```go
_, err := client.TaxAssociation.Update().
    Where(
        entTaxConfig.ID(t.ID),
        entTaxConfig.TenantID(types.GetTenantID(ctx)),
        entTaxConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
    ).
    SetEntityID(t.EntityID).
    SetPriority(t.Priority).
    SetAutoApply(t.AutoApply).
    SetUpdatedAt(time.Now().UTC()).
    SetUpdatedBy(types.GetUserID(ctx)).
    SetMetadata(t.Metadata).
    // ADD: conditionally set end_date when provided
    Save(ctx)
```

Add the conditional before `Save`:

```go
update := client.TaxAssociation.Update().
    Where(
        entTaxConfig.ID(t.ID),
        entTaxConfig.TenantID(types.GetTenantID(ctx)),
        entTaxConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
    ).
    SetEntityID(t.EntityID).
    SetPriority(t.Priority).
    SetAutoApply(t.AutoApply).
    SetUpdatedAt(time.Now().UTC()).
    SetUpdatedBy(types.GetUserID(ctx)).
    SetMetadata(t.Metadata)

if t.EndDate != nil {
    update = update.SetEndDate(*t.EndDate)
}

_, err := update.Save(ctx)
```

- [ ] **Step 4: Vet**

```bash
go vet ./internal/domain/taxassociation/... ./internal/repository/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/taxassociation/model.go internal/repository/ent/taxassociation.go
git commit -m "feat: add StartDate/EndDate to TaxAssociation domain model and repository"
```

---

## Task 3: Add DTO Types and Extend ExecuteSubscriptionModifyRequest

**Files:**
- Modify: `internal/api/dto/subscription_modification.go`

- [ ] **Step 1: Add new type constants**

In `internal/api/dto/subscription_modification.go`, add after the existing `SubscriptionModifyType` constants block:

```go
// New modify type constants
const (
    SubscriptionModifyTypeCoupon SubscriptionModifyType = "coupon"
    SubscriptionModifyTypeTax    SubscriptionModifyType = "tax"
)

// SubModifyAction is the action to perform on a coupon or tax association.
type SubModifyAction string

const (
    SubModifyActionAdd    SubModifyAction = "add"
    SubModifyActionRemove SubModifyAction = "remove"
)
```

- [ ] **Step 2: Add SubModifyCouponParams**

```go
// SubModifyCouponParams is the payload for coupon association changes on a subscription.
type SubModifyCouponParams struct {
    // Action is "add" or "remove".
    Action SubModifyAction `json:"action" binding:"required"`
    // CouponID is required when action is "add".
    CouponID *string `json:"coupon_id,omitempty"`
    // AssociationID is required when action is "remove".
    AssociationID *string `json:"association_id,omitempty"`
    // EffectiveDate is when the change takes effect. For add: when the coupon starts.
    // For remove: when the coupon ends. Defaults to now if not provided. Can be past or future.
    EffectiveDate *time.Time `json:"effective_date,omitempty"`
}

func (r *SubModifyCouponParams) Validate() error {
    switch r.Action {
    case SubModifyActionAdd:
        if r.CouponID == nil || *r.CouponID == "" {
            return ierr.NewError("coupon_id is required for action 'add'").
                WithHint("Provide a valid coupon_id").
                Mark(ierr.ErrValidation)
        }
    case SubModifyActionRemove:
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

- [ ] **Step 3: Add SubModifyTaxParams**

```go
// SubModifyTaxParams is the payload for tax association changes on a subscription.
type SubModifyTaxParams struct {
    // Action is "add" or "remove".
    Action SubModifyAction `json:"action" binding:"required"`
    // TaxRateID is required when action is "add".
    TaxRateID *string `json:"tax_rate_id,omitempty"`
    // AssociationID is required when action is "remove".
    AssociationID *string `json:"association_id,omitempty"`
    // EffectiveDate is when the change takes effect. Defaults to now if not provided. Can be past or future.
    EffectiveDate *time.Time `json:"effective_date,omitempty"`
}

func (r *SubModifyTaxParams) Validate() error {
    switch r.Action {
    case SubModifyActionAdd:
        if r.TaxRateID == nil || *r.TaxRateID == "" {
            return ierr.NewError("tax_rate_id is required for action 'add'").
                WithHint("Provide a valid tax_rate_id").
                Mark(ierr.ErrValidation)
        }
    case SubModifyActionRemove:
        if r.AssociationID == nil || *r.AssociationID == "" {
            return ierr.NewError("association_id is required for action 'remove'").
                WithHint("Provide the tax association ID to remove").
                Mark(ierr.ErrValidation)
        }
    default:
        return ierr.NewError("unknown tax action: " + string(r.Action)).
            WithHint("Valid values: add, remove").
            Mark(ierr.ErrValidation)
    }
    return nil
}
```

- [ ] **Step 4: Extend ExecuteSubscriptionModifyRequest**

Add two new fields to `ExecuteSubscriptionModifyRequest`:

```go
type ExecuteSubscriptionModifyRequest struct {
    Type                   SubscriptionModifyType           `json:"type" binding:"required"`
    InheritanceParams      *SubModifyInheritanceRequest     `json:"inheritance_params,omitempty"`
    QuantityChangeParams   *SubModifyQuantityChangeRequest  `json:"quantity_change_params,omitempty"`
    GroupedInvoicingParams *SubModifyGroupedInvoicingParams `json:"grouped_invoicing_params,omitempty"`
    TrialEndParams         *SubModifyTrialEndRequest        `json:"trial_end_params,omitempty"`
    CouponParams           *SubModifyCouponParams           `json:"coupon_params,omitempty"`   // ADD
    TaxParams              *SubModifyTaxParams              `json:"tax_params,omitempty"`      // ADD
}
```

- [ ] **Step 5: Add cases to Validate()**

In `ExecuteSubscriptionModifyRequest.Validate()`, add two new cases before the `default`:

```go
case SubscriptionModifyTypeCoupon:
    if r.CouponParams == nil {
        return ierr.NewError("coupon_params is required for type 'coupon'").
            Mark(ierr.ErrValidation)
    }
    return r.CouponParams.Validate()
case SubscriptionModifyTypeTax:
    if r.TaxParams == nil {
        return ierr.NewError("tax_params is required for type 'tax'").
            Mark(ierr.ErrValidation)
    }
    return r.TaxParams.Validate()
```

Also update the `default` case's hint to include the new types:

```go
default:
    return ierr.NewError("unknown modification type: " + string(r.Type)).
        WithHint("Valid values: inheritance, quantity_change, grouped_invoicing, trial_end, coupon, tax").
        Mark(ierr.ErrValidation)
```

- [ ] **Step 6: Vet**

```bash
go vet ./internal/api/dto/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/api/dto/subscription_modification.go
git commit -m "feat: add SubModifyCouponParams, SubModifyTaxParams DTOs and extend modify request"
```

---

## Task 4: Add Routing Cases to subscription_modification.go

**Files:**
- Modify: `internal/service/subscription_modification.go`

These routing stubs reference methods that don't exist yet (they'll be created in Tasks 5 and 6). The project won't compile until Tasks 5 and 6 are complete — that's intentional in this TDD-adjacent approach.

- [ ] **Step 1: Add coupon case to Execute()**

In `Execute()`, add before the `default` case:

```go
case dto.SubscriptionModifyTypeCoupon:
    return s.executeCouponModification(ctx, subscriptionID, req.CouponParams)
```

- [ ] **Step 2: Add tax case to Execute()**

```go
case dto.SubscriptionModifyTypeTax:
    return s.executeTaxModification(ctx, subscriptionID, req.TaxParams)
```

Also update the `default` error hint:
```go
WithHint("Valid values: inheritance, quantity_change, grouped_invoicing, trial_end, coupon, tax").
```

- [ ] **Step 3: Add coupon and tax cases to Preview()**

Same pattern in `Preview()`:

```go
case dto.SubscriptionModifyTypeCoupon:
    return s.previewCouponModification(ctx, subscriptionID, req.CouponParams)
case dto.SubscriptionModifyTypeTax:
    return s.previewTaxModification(ctx, subscriptionID, req.TaxParams)
```

Update `default` hint in Preview() to match.

- [ ] **Step 4: Commit (will be revisited after Tasks 5+6 compile)**

```bash
git add internal/service/subscription_modification.go
git commit -m "feat: add coupon and tax routing stubs to subscription modify service"
```

---

## Task 5: Implement Coupon Modification Service

**Files:**
- Create: `internal/service/subscription_modification_coupon.go`

- [ ] **Step 1: Create the file**

Create `internal/service/subscription_modification_coupon.go` with the following complete content:

```go
package service

import (
    "context"
    "time"

    "github.com/flexprice/flexprice/internal/api/dto"
    coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/types"
)

// ─────────────────────────────────────────────
// Sub-feature: Coupon modification
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeCouponModification(
    ctx context.Context,
    subscriptionID string,
    params *dto.SubModifyCouponParams,
) (*dto.SubscriptionModifyResponse, error) {
    effectiveDate := time.Now().UTC()
    if params.EffectiveDate != nil {
        effectiveDate = params.EffectiveDate.UTC()
    }
    switch params.Action {
    case dto.SubModifyActionAdd:
        return s.executeAddCoupon(ctx, subscriptionID, *params.CouponID, effectiveDate)
    case dto.SubModifyActionRemove:
        return s.executeRemoveCoupon(ctx, subscriptionID, *params.AssociationID, effectiveDate)
    default:
        return nil, ierr.NewError("unknown coupon action: " + string(params.Action)).
            Mark(ierr.ErrValidation)
    }
}

func (s *subscriptionModificationService) executeAddCoupon(
    ctx context.Context,
    subscriptionID string,
    couponID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    // 1. Validate coupon exists and is active
    c, err := sp.CouponRepo.Get(ctx, couponID)
    if err != nil {
        return nil, ierr.NewError("coupon not found or inactive").
            WithHint("Provide a valid, active coupon_id").
            WithReportableDetails(map[string]interface{}{"coupon_id": couponID}).
            Mark(ierr.ErrValidation)
    }
    if c.Status != types.StatusPublished {
        return nil, ierr.NewError("coupon not found or inactive").
            WithHint("The specified coupon is not currently active").
            WithReportableDetails(map[string]interface{}{"coupon_id": couponID, "status": c.Status}).
            Mark(ierr.ErrValidation)
    }

    // 2. Check for overlapping active association for same coupon on this subscription
    filter := &types.CouponAssociationFilter{
        QueryFilter:     types.NewNoLimitQueryFilter(),
        SubscriptionIDs: []string{subscriptionID},
        CouponIDs:       []string{couponID},
        ActiveOnly:      true,
        PeriodStart:     &effectiveDate,
        PeriodEnd:       &effectiveDate,
    }
    existing, err := sp.CouponAssociationRepo.List(ctx, filter)
    if err != nil {
        return nil, err
    }
    if len(existing) > 0 {
        return nil, ierr.NewError("coupon already active on this subscription for the given date range").
            WithHint("Remove the existing coupon association before adding it again, or use a different effective_date").
            WithReportableDetails(map[string]interface{}{
                "coupon_id":       couponID,
                "subscription_id": subscriptionID,
                "effective_date":  effectiveDate,
            }).
            Mark(ierr.ErrValidation)
    }

    // 3. Create association
    assoc := &coupon_association.CouponAssociation{
        ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
        CouponID:       couponID,
        SubscriptionID: subscriptionID,
        StartDate:      effectiveDate,
        EnvironmentID:  types.GetEnvironmentID(ctx),
        BaseModel:      types.GetDefaultBaseModel(ctx),
    }
    if err := sp.CouponAssociationRepo.Create(ctx, assoc); err != nil {
        return nil, err
    }

    s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}

func (s *subscriptionModificationService) executeRemoveCoupon(
    ctx context.Context,
    subscriptionID string,
    associationID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    // 1. Validate association exists and belongs to this subscription
    assoc, err := sp.CouponAssociationRepo.Get(ctx, associationID)
    if err != nil {
        return nil, ierr.NewError("association not found").
            WithHint("Provide a valid association_id belonging to this subscription").
            WithReportableDetails(map[string]interface{}{"association_id": associationID}).
            Mark(ierr.ErrNotFound)
    }
    if assoc.SubscriptionID != subscriptionID {
        return nil, ierr.NewError("association does not belong to this subscription").
            WithReportableDetails(map[string]interface{}{
                "association_id":  associationID,
                "subscription_id": subscriptionID,
            }).
            Mark(ierr.ErrValidation)
    }

    // 2. Check it's still active (EndDate not already in the past)
    now := time.Now().UTC()
    if assoc.EndDate != nil && !assoc.EndDate.After(now) {
        return nil, ierr.NewError("association already inactive").
            WithHint("This coupon association has already ended").
            WithReportableDetails(map[string]interface{}{
                "association_id": associationID,
                "end_date":       assoc.EndDate,
            }).
            Mark(ierr.ErrValidation)
    }

    // 3. Soft-delete: set EndDate to effectiveDate
    assoc.EndDate = &effectiveDate
    if err := sp.CouponAssociationRepo.Update(ctx, assoc); err != nil {
        return nil, err
    }

    s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}

// ─────────────────────────────────────────────
// Preview: Coupon modification
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) previewCouponModification(
    ctx context.Context,
    subscriptionID string,
    params *dto.SubModifyCouponParams,
) (*dto.SubscriptionModifyResponse, error) {
    effectiveDate := time.Now().UTC()
    if params.EffectiveDate != nil {
        effectiveDate = params.EffectiveDate.UTC()
    }
    switch params.Action {
    case dto.SubModifyActionAdd:
        return s.previewAddCoupon(ctx, subscriptionID, *params.CouponID, effectiveDate)
    case dto.SubModifyActionRemove:
        return s.previewRemoveCoupon(ctx, subscriptionID, *params.AssociationID, effectiveDate)
    default:
        return nil, ierr.NewError("unknown coupon action: " + string(params.Action)).
            Mark(ierr.ErrValidation)
    }
}

func (s *subscriptionModificationService) previewAddCoupon(
    ctx context.Context,
    subscriptionID string,
    couponID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    // Same validation as execute, no DB writes
    c, err := sp.CouponRepo.Get(ctx, couponID)
    if err != nil {
        return nil, ierr.NewError("coupon not found or inactive").
            WithHint("Provide a valid, active coupon_id").
            WithReportableDetails(map[string]interface{}{"coupon_id": couponID}).
            Mark(ierr.ErrValidation)
    }
    if c.Status != types.StatusPublished {
        return nil, ierr.NewError("coupon not found or inactive").
            WithReportableDetails(map[string]interface{}{"coupon_id": couponID, "status": c.Status}).
            Mark(ierr.ErrValidation)
    }

    filter := &types.CouponAssociationFilter{
        QueryFilter:     types.NewNoLimitQueryFilter(),
        SubscriptionIDs: []string{subscriptionID},
        CouponIDs:       []string{couponID},
        ActiveOnly:      true,
        PeriodStart:     &effectiveDate,
        PeriodEnd:       &effectiveDate,
    }
    existing, err := sp.CouponAssociationRepo.List(ctx, filter)
    if err != nil {
        return nil, err
    }
    if len(existing) > 0 {
        return nil, ierr.NewError("coupon already active on this subscription for the given date range").
            WithReportableDetails(map[string]interface{}{
                "coupon_id":       couponID,
                "subscription_id": subscriptionID,
            }).
            Mark(ierr.ErrValidation)
    }

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}

func (s *subscriptionModificationService) previewRemoveCoupon(
    ctx context.Context,
    subscriptionID string,
    associationID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    assoc, err := sp.CouponAssociationRepo.Get(ctx, associationID)
    if err != nil {
        return nil, ierr.NewError("association not found").
            WithHint("Provide a valid association_id").
            WithReportableDetails(map[string]interface{}{"association_id": associationID}).
            Mark(ierr.ErrNotFound)
    }
    if assoc.SubscriptionID != subscriptionID {
        return nil, ierr.NewError("association does not belong to this subscription").
            WithReportableDetails(map[string]interface{}{
                "association_id":  associationID,
                "subscription_id": subscriptionID,
            }).
            Mark(ierr.ErrValidation)
    }
    now := time.Now().UTC()
    if assoc.EndDate != nil && !assoc.EndDate.After(now) {
        return nil, ierr.NewError("association already inactive").
            WithReportableDetails(map[string]interface{}{"association_id": associationID}).
            Mark(ierr.ErrValidation)
    }

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}
```

- [ ] **Step 2: Vet**

```bash
go vet ./internal/service/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_modification_coupon.go
git commit -m "feat: implement coupon add/remove modification service"
```

---

## Task 6: Implement Tax Modification Service

**Files:**
- Create: `internal/service/subscription_modification_tax.go`

- [ ] **Step 1: Create the file**

Create `internal/service/subscription_modification_tax.go` with the following complete content:

```go
package service

import (
    "context"
    "time"

    "github.com/flexprice/flexprice/internal/api/dto"
    taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/types"
)

// ─────────────────────────────────────────────
// Sub-feature: Tax modification
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeTaxModification(
    ctx context.Context,
    subscriptionID string,
    params *dto.SubModifyTaxParams,
) (*dto.SubscriptionModifyResponse, error) {
    effectiveDate := time.Now().UTC()
    if params.EffectiveDate != nil {
        effectiveDate = params.EffectiveDate.UTC()
    }
    switch params.Action {
    case dto.SubModifyActionAdd:
        return s.executeAddTax(ctx, subscriptionID, *params.TaxRateID, effectiveDate)
    case dto.SubModifyActionRemove:
        return s.executeRemoveTax(ctx, subscriptionID, *params.AssociationID, effectiveDate)
    default:
        return nil, ierr.NewError("unknown tax action: " + string(params.Action)).
            Mark(ierr.ErrValidation)
    }
}

func (s *subscriptionModificationService) executeAddTax(
    ctx context.Context,
    subscriptionID string,
    taxRateID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    // 1. Validate tax rate exists and is active
    taxRate, err := sp.TaxRateRepo.Get(ctx, taxRateID)
    if err != nil {
        return nil, ierr.NewError("tax rate not found or inactive").
            WithHint("Provide a valid, active tax_rate_id").
            WithReportableDetails(map[string]interface{}{"tax_rate_id": taxRateID}).
            Mark(ierr.ErrValidation)
    }
    if taxRate.TaxRateStatus != types.TaxRateStatusActive {
        return nil, ierr.NewError("tax rate not found or inactive").
            WithHint("The specified tax rate is not currently active").
            WithReportableDetails(map[string]interface{}{
                "tax_rate_id": taxRateID,
                "status":      taxRate.TaxRateStatus,
            }).
            Mark(ierr.ErrValidation)
    }

    // 2. Check for existing active association for same tax rate on this subscription.
    // TaxAssociationFilter doesn't have ActiveOnly — filter in-memory.
    filter := &types.TaxAssociationFilter{
        QueryFilter: types.NewNoLimitQueryFilter(),
        EntityType:  types.TaxRateEntityTypeSubscription,
        EntityID:    subscriptionID,
        TaxRateIDs:  []string{taxRateID},
    }
    existing, err := sp.TaxAssociationRepo.List(ctx, filter)
    if err != nil {
        return nil, err
    }
    for _, ta := range existing {
        // Active: started before or at effectiveDate AND not yet ended
        if !ta.StartDate.After(effectiveDate) && (ta.EndDate == nil || ta.EndDate.After(effectiveDate)) {
            return nil, ierr.NewError("tax rate already active on this subscription for the given date range").
                WithHint("Remove the existing tax association before adding it again, or use a different effective_date").
                WithReportableDetails(map[string]interface{}{
                    "tax_rate_id":     taxRateID,
                    "subscription_id": subscriptionID,
                    "effective_date":  effectiveDate,
                }).
                Mark(ierr.ErrValidation)
        }
    }

    // 3. Create association
    assoc := &taxassociation.TaxAssociation{
        ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
        TaxRateID:     taxRateID,
        EntityType:    types.TaxRateEntityTypeSubscription,
        EntityID:      subscriptionID,
        StartDate:     effectiveDate,
        Priority:      100,
        AutoApply:     true,
        EnvironmentID: types.GetEnvironmentID(ctx),
        BaseModel:     types.GetDefaultBaseModel(ctx),
    }
    if err := sp.TaxAssociationRepo.Create(ctx, assoc); err != nil {
        return nil, err
    }

    s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}

func (s *subscriptionModificationService) executeRemoveTax(
    ctx context.Context,
    subscriptionID string,
    associationID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    // 1. Validate association exists and belongs to this subscription
    assoc, err := sp.TaxAssociationRepo.Get(ctx, associationID)
    if err != nil {
        return nil, ierr.NewError("association not found").
            WithHint("Provide a valid association_id belonging to this subscription").
            WithReportableDetails(map[string]interface{}{"association_id": associationID}).
            Mark(ierr.ErrNotFound)
    }
    if assoc.EntityType != types.TaxRateEntityTypeSubscription || assoc.EntityID != subscriptionID {
        return nil, ierr.NewError("association does not belong to this subscription").
            WithReportableDetails(map[string]interface{}{
                "association_id":  associationID,
                "subscription_id": subscriptionID,
            }).
            Mark(ierr.ErrValidation)
    }

    // 2. Check it's still active
    now := time.Now().UTC()
    if assoc.EndDate != nil && !assoc.EndDate.After(now) {
        return nil, ierr.NewError("association already inactive").
            WithHint("This tax association has already ended").
            WithReportableDetails(map[string]interface{}{
                "association_id": associationID,
                "end_date":       assoc.EndDate,
            }).
            Mark(ierr.ErrValidation)
    }

    // 3. Soft-delete: set EndDate to effectiveDate
    assoc.EndDate = &effectiveDate
    if err := sp.TaxAssociationRepo.Update(ctx, assoc); err != nil {
        return nil, err
    }

    s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}

// ─────────────────────────────────────────────
// Preview: Tax modification
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) previewTaxModification(
    ctx context.Context,
    subscriptionID string,
    params *dto.SubModifyTaxParams,
) (*dto.SubscriptionModifyResponse, error) {
    effectiveDate := time.Now().UTC()
    if params.EffectiveDate != nil {
        effectiveDate = params.EffectiveDate.UTC()
    }
    switch params.Action {
    case dto.SubModifyActionAdd:
        return s.previewAddTax(ctx, subscriptionID, *params.TaxRateID, effectiveDate)
    case dto.SubModifyActionRemove:
        return s.previewRemoveTax(ctx, subscriptionID, *params.AssociationID, effectiveDate)
    default:
        return nil, ierr.NewError("unknown tax action: " + string(params.Action)).
            Mark(ierr.ErrValidation)
    }
}

func (s *subscriptionModificationService) previewAddTax(
    ctx context.Context,
    subscriptionID string,
    taxRateID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    taxRate, err := sp.TaxRateRepo.Get(ctx, taxRateID)
    if err != nil {
        return nil, ierr.NewError("tax rate not found or inactive").
            WithHint("Provide a valid, active tax_rate_id").
            WithReportableDetails(map[string]interface{}{"tax_rate_id": taxRateID}).
            Mark(ierr.ErrValidation)
    }
    if taxRate.TaxRateStatus != types.TaxRateStatusActive {
        return nil, ierr.NewError("tax rate not found or inactive").
            WithReportableDetails(map[string]interface{}{
                "tax_rate_id": taxRateID,
                "status":      taxRate.TaxRateStatus,
            }).
            Mark(ierr.ErrValidation)
    }

    filter := &types.TaxAssociationFilter{
        QueryFilter: types.NewNoLimitQueryFilter(),
        EntityType:  types.TaxRateEntityTypeSubscription,
        EntityID:    subscriptionID,
        TaxRateIDs:  []string{taxRateID},
    }
    existing, err := sp.TaxAssociationRepo.List(ctx, filter)
    if err != nil {
        return nil, err
    }
    for _, ta := range existing {
        if !ta.StartDate.After(effectiveDate) && (ta.EndDate == nil || ta.EndDate.After(effectiveDate)) {
            return nil, ierr.NewError("tax rate already active on this subscription for the given date range").
                WithReportableDetails(map[string]interface{}{
                    "tax_rate_id":     taxRateID,
                    "subscription_id": subscriptionID,
                }).
                Mark(ierr.ErrValidation)
        }
    }

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}

func (s *subscriptionModificationService) previewRemoveTax(
    ctx context.Context,
    subscriptionID string,
    associationID string,
    effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
    sp := s.serviceParams

    assoc, err := sp.TaxAssociationRepo.Get(ctx, associationID)
    if err != nil {
        return nil, ierr.NewError("association not found").
            WithHint("Provide a valid association_id").
            WithReportableDetails(map[string]interface{}{"association_id": associationID}).
            Mark(ierr.ErrNotFound)
    }
    if assoc.EntityType != types.TaxRateEntityTypeSubscription || assoc.EntityID != subscriptionID {
        return nil, ierr.NewError("association does not belong to this subscription").
            WithReportableDetails(map[string]interface{}{
                "association_id":  associationID,
                "subscription_id": subscriptionID,
            }).
            Mark(ierr.ErrValidation)
    }
    now := time.Now().UTC()
    if assoc.EndDate != nil && !assoc.EndDate.After(now) {
        return nil, ierr.NewError("association already inactive").
            WithReportableDetails(map[string]interface{}{"association_id": associationID}).
            Mark(ierr.ErrValidation)
    }

    subSvc := NewSubscriptionService(sp)
    subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
    if err != nil {
        return nil, err
    }
    return &dto.SubscriptionModifyResponse{
        Subscription:     subResp,
        ChangedResources: dto.ChangedResources{},
    }, nil
}
```

- [ ] **Step 2: Build check**

```bash
go build ./internal/service/...
```

Expected: compiles cleanly; the routing stubs from Task 4 now resolve.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_modification_tax.go
git commit -m "feat: implement tax add/remove modification service"
```

---

## Task 7: Write and Run Coupon Modification Tests

**Files:**
- Create: `internal/service/subscription_modification_coupon_test.go`

Tests live in the same package and add methods to the existing `SubscriptionModificationServiceSuite`.

- [ ] **Step 1: Create the test file**

Create `internal/service/subscription_modification_coupon_test.go`:

```go
package service

import (
    "time"

    "github.com/flexprice/flexprice/internal/api/dto"
    "github.com/flexprice/flexprice/internal/domain/coupon"
    coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
)

// ─────────────────────────────────────────────
// Coupon modification test helpers
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) createCoupon() *coupon.Coupon {
    ctx := s.GetContext()
    pct := decimal.NewFromFloat(10)
    c := &coupon.Coupon{
        ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
        Name:          "Test Coupon",
        Type:          types.CouponTypePercentage,
        PercentageOff: &pct,
        Cadence:       types.CouponCadenceForever,
        BaseModel:     types.GetDefaultBaseModel(ctx),
        EnvironmentID: types.GetEnvironmentID(ctx),
    }
    s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
    return c
}

func (s *SubscriptionModificationServiceSuite) createCouponAssociation(couponID, subID string, startDate time.Time, endDate *time.Time) *coupon_association.CouponAssociation {
    ctx := s.GetContext()
    assoc := &coupon_association.CouponAssociation{
        ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
        CouponID:       couponID,
        SubscriptionID: subID,
        StartDate:      startDate,
        EndDate:        endDate,
        EnvironmentID:  types.GetEnvironmentID(ctx),
        BaseModel:      types.GetDefaultBaseModel(ctx),
    }
    s.Require().NoError(s.GetStores().CouponAssociationRepo.Create(ctx, assoc))
    return assoc
}

// ─────────────────────────────────────────────
// Coupon modification tests
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestCouponModification() {
    ctx := s.GetContext()
    now := s.GetNow()
    past := now.Add(-24 * time.Hour)
    future := now.Add(30 * 24 * time.Hour)

    cust := s.createCustomer("coupon-test-customer")

    tests := []struct {
        name      string
        setup     func() (subID string, req dto.ExecuteSubscriptionModifyRequest)
        wantErr   bool
        errCode   string // ierr marker substring for error cases
        checkResp func(resp *dto.SubscriptionModifyResponse)
    }{
        {
            name: "add coupon with effective_date in past (retroactive)",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionAdd,
                        CouponID:      &c.ID,
                        EffectiveDate: &past,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
                s.Require().Len(resp.Subscription.CouponAssociations, 1)
            },
        },
        {
            name: "add coupon with effective_date in future (scheduled)",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionAdd,
                        CouponID:      &c.ID,
                        EffectiveDate: &future,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
                s.Require().Len(resp.Subscription.CouponAssociations, 1)
            },
        },
        {
            name: "add coupon with nil effective_date defaults to now",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:   dto.SubModifyActionAdd,
                        CouponID: &c.ID,
                        // EffectiveDate omitted
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
                s.Require().Len(resp.Subscription.CouponAssociations, 1)
            },
        },
        {
            name: "add coupon — duplicate active association returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                // Pre-create an active association at 'now'
                s.createCouponAssociation(c.ID, sub.ID, now, nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionAdd,
                        CouponID:      &c.ID,
                        EffectiveDate: &now,
                    },
                }
            },
            wantErr: true,
            errCode: "coupon already active",
        },
        {
            name: "add coupon — coupon not found returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                bogusID := "coupon_notexist"
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:   dto.SubModifyActionAdd,
                        CouponID: &bogusID,
                    },
                }
            },
            wantErr: true,
            errCode: "coupon not found",
        },
        {
            name: "remove coupon with effective_date in past",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                assoc := s.createCouponAssociation(c.ID, sub.ID, past.Add(-time.Hour), nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                        EffectiveDate: &past,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "remove coupon with effective_date in future (scheduled removal)",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                assoc := s.createCouponAssociation(c.ID, sub.ID, now, nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                        EffectiveDate: &future,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "remove coupon with nil effective_date defaults to now",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                assoc := s.createCouponAssociation(c.ID, sub.ID, now, nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                        // EffectiveDate omitted
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "remove coupon — association not found returns 404",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                bogusID := "coupon_assoc_notexist"
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &bogusID,
                    },
                }
            },
            wantErr: true,
            errCode: "association not found",
        },
        {
            name: "remove coupon — association belongs to different subscription returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub1 := s.createActiveSub(cust.ID)
                sub2 := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                // Association is on sub1, but we call with sub2
                assoc := s.createCouponAssociation(c.ID, sub1.ID, now, nil)
                return sub2.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                    },
                }
            },
            wantErr: true,
            errCode: "does not belong",
        },
        {
            name: "remove coupon — already inactive returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                // EndDate is in the past
                assoc := s.createCouponAssociation(c.ID, sub.ID, past.Add(-time.Hour), &past)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                    },
                }
            },
            wantErr: true,
            errCode: "already inactive",
        },
        {
            name: "preview add — no DB write, returns subscription state",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                c := s.createCoupon()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeCoupon,
                    CouponParams: &dto.SubModifyCouponParams{
                        Action:   dto.SubModifyActionAdd,
                        CouponID: &c.ID,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
    }

    for _, tc := range tests {
        s.Run(tc.name, func() {
            subID, req := tc.setup()

            // Determine whether to call Execute or Preview based on test name
            isPreview := len(tc.name) > 7 && tc.name[:7] == "preview"

            var (
                resp *dto.SubscriptionModifyResponse
                err  error
            )
            if isPreview {
                resp, err = s.service.Preview(ctx, subID, req)
            } else {
                resp, err = s.service.Execute(ctx, subID, req)
            }

            if tc.wantErr {
                s.Require().Error(err)
                if tc.errCode != "" {
                    s.Require().Contains(err.Error(), tc.errCode,
                        "expected error to contain %q, got: %v", tc.errCode, err)
                }
                s.Require().Nil(resp)
                return
            }
            s.Require().NoError(err)
            s.Require().NotNil(resp)
            if tc.checkResp != nil {
                tc.checkResp(resp)
            }
        })
    }
}
```

> **Note on `ierr` errors:** The `ierr` package wraps errors with context. `err.Error()` returns the message set by `ierr.NewError(...)`. If `Contains` checks fail, use `s.T().Logf("%+v", err)` to inspect the full error.

- [ ] **Step 2: Run the tests**

```bash
go test -v -race ./internal/service/ -run TestSubscriptionModificationServiceSuite/TestCouponModification
```

Expected: all subtests pass; zero race conditions detected.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_modification_coupon_test.go
git commit -m "test: add coupon modification tests"
```

---

## Task 8: Write and Run Tax Modification Tests

**Files:**
- Create: `internal/service/subscription_modification_tax_test.go`

- [ ] **Step 1: Create the test file**

Create `internal/service/subscription_modification_tax_test.go`:

```go
package service

import (
    "time"

    "github.com/flexprice/flexprice/internal/api/dto"
    taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
    taxrate "github.com/flexprice/flexprice/internal/domain/tax"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
)

// ─────────────────────────────────────────────
// Tax modification test helpers
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) createTaxRate() *taxrate.TaxRate {
    ctx := s.GetContext()
    pct := decimal.NewFromFloat(10)
    tr := &taxrate.TaxRate{
        ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
        Name:            "Test Tax Rate",
        TaxRateStatus:   types.TaxRateStatusActive,
        TaxRateType:     types.TaxRateTypePercentage,
        PercentageValue: &pct,
        BaseModel:       types.GetDefaultBaseModel(ctx),
        EnvironmentID:   types.GetEnvironmentID(ctx),
    }
    s.Require().NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))
    return tr
}

func (s *SubscriptionModificationServiceSuite) createTaxAssociation(taxRateID, subID string, startDate time.Time, endDate *time.Time) *taxassociation.TaxAssociation {
    ctx := s.GetContext()
    assoc := &taxassociation.TaxAssociation{
        ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
        TaxRateID:     taxRateID,
        EntityType:    types.TaxRateEntityTypeSubscription,
        EntityID:      subID,
        StartDate:     startDate,
        EndDate:       endDate,
        Priority:      100,
        AutoApply:     true,
        EnvironmentID: types.GetEnvironmentID(ctx),
        BaseModel:     types.GetDefaultBaseModel(ctx),
    }
    s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(ctx, assoc))
    return assoc
}

// ─────────────────────────────────────────────
// Tax modification tests
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestTaxModification() {
    ctx := s.GetContext()
    now := s.GetNow()
    past := now.Add(-24 * time.Hour)
    future := now.Add(30 * 24 * time.Hour)

    cust := s.createCustomer("tax-test-customer")

    tests := []struct {
        name      string
        setup     func() (subID string, req dto.ExecuteSubscriptionModifyRequest)
        wantErr   bool
        errCode   string
        checkResp func(resp *dto.SubscriptionModifyResponse)
    }{
        {
            name: "add tax with effective_date in past (retroactive)",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionAdd,
                        TaxRateID:     &tr.ID,
                        EffectiveDate: &past,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "add tax with effective_date in future (scheduled)",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionAdd,
                        TaxRateID:     &tr.ID,
                        EffectiveDate: &future,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "add tax with nil effective_date defaults to now",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:    dto.SubModifyActionAdd,
                        TaxRateID: &tr.ID,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "add tax — duplicate active association returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                s.createTaxAssociation(tr.ID, sub.ID, now, nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionAdd,
                        TaxRateID:     &tr.ID,
                        EffectiveDate: &now,
                    },
                }
            },
            wantErr: true,
            errCode: "already active",
        },
        {
            name: "add tax — tax rate not found returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                bogusID := "taxrate_notexist"
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:    dto.SubModifyActionAdd,
                        TaxRateID: &bogusID,
                    },
                }
            },
            wantErr: true,
            errCode: "tax rate not found",
        },
        {
            name: "remove tax with effective_date in past",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                assoc := s.createTaxAssociation(tr.ID, sub.ID, past.Add(-time.Hour), nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                        EffectiveDate: &past,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "remove tax with effective_date in future (scheduled removal)",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                assoc := s.createTaxAssociation(tr.ID, sub.ID, now, nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                        EffectiveDate: &future,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "remove tax with nil effective_date defaults to now",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                assoc := s.createTaxAssociation(tr.ID, sub.ID, now, nil)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
        {
            name: "remove tax — association not found returns 404",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                bogusID := "ta_notexist"
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &bogusID,
                    },
                }
            },
            wantErr: true,
            errCode: "association not found",
        },
        {
            name: "remove tax — association belongs to different subscription returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub1 := s.createActiveSub(cust.ID)
                sub2 := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                assoc := s.createTaxAssociation(tr.ID, sub1.ID, now, nil)
                return sub2.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                    },
                }
            },
            wantErr: true,
            errCode: "does not belong",
        },
        {
            name: "remove tax — already inactive returns 422",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                assoc := s.createTaxAssociation(tr.ID, sub.ID, past.Add(-time.Hour), &past)
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:        dto.SubModifyActionRemove,
                        AssociationID: &assoc.ID,
                    },
                }
            },
            wantErr: true,
            errCode: "already inactive",
        },
        {
            name: "preview add tax — no DB write, returns subscription state",
            setup: func() (string, dto.ExecuteSubscriptionModifyRequest) {
                sub := s.createActiveSub(cust.ID)
                tr := s.createTaxRate()
                return sub.ID, dto.ExecuteSubscriptionModifyRequest{
                    Type: dto.SubscriptionModifyTypeTax,
                    TaxParams: &dto.SubModifyTaxParams{
                        Action:    dto.SubModifyActionAdd,
                        TaxRateID: &tr.ID,
                    },
                }
            },
            checkResp: func(resp *dto.SubscriptionModifyResponse) {
                s.Require().NotNil(resp.Subscription)
            },
        },
    }

    for _, tc := range tests {
        s.Run(tc.name, func() {
            subID, req := tc.setup()
            isPreview := len(tc.name) > 7 && tc.name[:7] == "preview"

            var (
                resp *dto.SubscriptionModifyResponse
                err  error
            )
            if isPreview {
                resp, err = s.service.Preview(ctx, subID, req)
            } else {
                resp, err = s.service.Execute(ctx, subID, req)
            }

            if tc.wantErr {
                s.Require().Error(err)
                if tc.errCode != "" {
                    s.Require().Contains(err.Error(), tc.errCode,
                        "expected error to contain %q, got: %v", tc.errCode, err)
                }
                s.Require().Nil(resp)
                return
            }
            s.Require().NoError(err)
            s.Require().NotNil(resp)
            if tc.checkResp != nil {
                tc.checkResp(resp)
            }
        })
    }
}
```

- [ ] **Step 2: Run the tests**

```bash
go test -v -race ./internal/service/ -run TestSubscriptionModificationServiceSuite/TestTaxModification
```

Expected: all subtests pass.

- [ ] **Step 3: Run full suite to check for regressions**

```bash
go test -race ./internal/service/ -run TestSubscriptionModificationServiceSuite
```

Expected: all existing tests still pass.

- [ ] **Step 4: Commit**

```bash
git add internal/service/subscription_modification_tax_test.go
git commit -m "test: add tax modification tests"
```

---

## Task 9: Final Vet and Run All Tests

- [ ] **Step 1: Full vet**

```bash
go vet ./...
```

Expected: no errors.

- [ ] **Step 2: Run all service tests**

```bash
go test -race ./internal/service/... -timeout 120s
```

Expected: PASS for all tests.

- [ ] **Step 3: Generate Swagger docs** (if API layer already has swagger annotations)

```bash
make swagger
```

Expected: regenerated docs include updated hints for the modify endpoint. No new endpoints to annotate — the existing `/modify/preview` and `/modify/execute` swagger comments may need the new type values added to their `@Param` descriptions. Update if present.

- [ ] **Step 4: Final commit**

```bash
git add .
git commit -m "feat: subscription discount and tax modification (add/remove post-creation)"
```
