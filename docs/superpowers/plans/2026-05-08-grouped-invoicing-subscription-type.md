# Grouped Invoicing Subscription Type Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `grouped_invoicing` and `delegated` subscription types, a user-facing `invoicing_behavior` field inside `SubscriptionInheritanceConfig`, membership helpers, and a clubbed invoice billing flow where a parent sub's billing period trigger generates one merged invoice for itself plus all its `grouped_invoicing` children.

**Architecture:** Extend the existing `SubscriptionType` enum with two new values. Hook grouped invoicing into the existing `processSubscriptionPeriod` → `CreateSubscriptionInvoice` pipeline by routing parent subs with grouped children through a new `CreateGroupedSubscriptionInvoice` method that merges line items from all children before computing the invoice. Membership add/remove logic lives in a dedicated `internal/service/subscription_grouped_invoicing.go` file. Post-creation membership changes are exposed via `SubscriptionModificationService` with two new modify types (`grouped_invoicing_add`, `grouped_invoicing_remove`), following the same preview/execute pattern as existing `inheritance` and `quantity_change` types.

**Tech Stack:** Go 1.23, Gin, Uber FX, Ent (PostgreSQL), existing service patterns in `internal/service/`.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/types/subscription.go` | Modify | Add `SubscriptionTypeDelegated`, `SubscriptionTypeGroupedInvoicing` |
| `internal/api/dto/subscription.go` | Modify | Add `InvoicingBehavior` + `SubIDsForGroupedInvoicing` to `SubscriptionInheritanceConfig`; update `Validate()` |
| `internal/api/dto/billing.go` | Modify | Add `PrepareGroupedInvoiceRequestParams` |
| `internal/api/dto/subscription_modification.go` | Modify | Add `SubModifyGroupedInvoicingParams`; add `SubscriptionModifyTypeGroupedInvoicingAdd/Remove`; extend `ExecuteSubscriptionModifyRequest` |
| `internal/service/subscription_grouped_invoicing.go` | **Create** | `addToGroupedInvoicing`, `removeFromGroupedInvoicing`, `getGroupedInvoicingSubscriptions` |
| `internal/service/subscription.go` | Modify | Update `prepareSubscriptionInheritanceForCreate`; update `processSubscriptionPeriod` to skip grouped children and trigger clubbed invoice for parents |
| `internal/service/billing.go` | Modify | Add `PrepareGroupedInvoiceRequest` to interface + implementation |
| `internal/service/invoice.go` | Modify | Skip `grouped_invoicing` in `ComputeInvoice`; add `CreateGroupedSubscriptionInvoice` |
| `internal/service/subscription_modification.go` | Modify | Add `grouped_invoicing_add`/`_remove` cases to Execute + Preview switch |
| `internal/service/subscription_grouped_invoicing_test.go` | **Create** | Unit tests for add/remove helpers |
| `internal/service/subscription_test.go` | Modify | Tests for new create-time behaviors (delegated, grouped_invoicing child create, parent with `sub_ids_for_grouped_invoicing`) |

---

## Task 1: Extend `SubscriptionType` and `SubscriptionChangeType` enums

**Files:**
- Modify: `internal/types/subscription.go:13-50`

- [ ] **Step 1: Add the two new `SubscriptionType` constants and update `SubscriptionTypeValues` and `Validate` hint**

Replace lines 13–50 in `internal/types/subscription.go`:

```go
const (
	// SubscriptionTypeStandalone is a regular subscription with no hierarchy relationship.
	// No invoicing_customer_id or parent_subscription_id may be set.
	SubscriptionTypeStandalone SubscriptionType = "standalone"

	// SubscriptionTypeDelegated has its own line items but the invoice is raised against a
	// different customer (invoicing_customer_id is required; parent_subscription_id must be unset).
	SubscriptionTypeDelegated SubscriptionType = "delegated"

	// SubscriptionTypeParent is the primary subscription that owns line items and aggregates
	// usage from child (inherited) subscriptions, and triggers clubbed invoices for grouped_invoicing children.
	SubscriptionTypeParent SubscriptionType = "parent"

	// SubscriptionTypeInherited is a skeleton subscription created for each child customer
	// in a hierarchy. It carries no line items; events are matched via the parent subscription.
	SubscriptionTypeInherited SubscriptionType = "inherited"

	// SubscriptionTypeGroupedInvoicing has its own line items and entitlements but its invoice
	// is clubbed into the parent's invoice. parent_subscription_id is required; invoicing_customer_id is optional.
	SubscriptionTypeGroupedInvoicing SubscriptionType = "grouped_invoicing"
)

var SubscriptionTypeValues = []SubscriptionType{
	SubscriptionTypeStandalone,
	SubscriptionTypeDelegated,
	SubscriptionTypeParent,
	SubscriptionTypeInherited,
	SubscriptionTypeGroupedInvoicing,
}
```

Update the `Validate()` hint at line ~44:
```go
WithHint("Subscription type must be one of: standalone, delegated, parent, inherited, grouped_invoicing").
```

- [ ] **Step 2: Run `go vet ./internal/types/...` — expected: no errors**

```bash
cd /path/to/repo && go vet ./internal/types/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/types/subscription.go
git commit -m "feat(types): add delegated and grouped_invoicing subscription types"
```

---

## Task 2: Update `SubscriptionInheritanceConfig` DTO

**Files:**
- Modify: `internal/api/dto/subscription.go:274-298`

- [ ] **Step 1: Write the failing test for `InvoicingBehavior` validation**

Add to `internal/api/dto/subscription_test.go` (create it if it does not exist):

```go
package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSubscriptionInheritanceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *dto.SubscriptionInheritanceConfig
		wantErr bool
	}{
		{
			name: "nil config is valid",
			cfg:  nil,
		},
		{
			name: "delegated requires invoicing_customer_external_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionTypeDelegated,
			},
			wantErr: true,
		},
		{
			name: "delegated with parent_subscription_id is invalid",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeDelegated,
				InvoicingCustomerExternalID: strPtr("cust_ext"),
				ParentSubscriptionID:        "sub_123",
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing requires parent_subscription_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionTypeGroupedInvoicing,
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing valid",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior:    types.SubscriptionTypeGroupedInvoicing,
				ParentSubscriptionID: "sub_parent_123",
			},
		},
		{
			name: "inherited requires parent_subscription_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior: types.SubscriptionTypeInherited,
			},
			wantErr: true,
		},
		{
			name: "inherited rejects invoicing_customer_external_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeInherited,
				ParentSubscriptionID:        "sub_parent_123",
				InvoicingCustomerExternalID: strPtr("cust_ext"),
			},
			wantErr: true,
		},
		{
			name: "parent rejects parent_subscription_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior:    types.SubscriptionTypeParent,
				ParentSubscriptionID: "sub_123",
			},
			wantErr: true,
		},
		{
			name: "standalone rejects parent_subscription_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior:    types.SubscriptionTypeStandalone,
				ParentSubscriptionID: "sub_123",
			},
			wantErr: true,
		},
		{
			name: "standalone rejects invoicing_customer_external_id",
			cfg: &dto.SubscriptionInheritanceConfig{
				InvoicingBehavior:           types.SubscriptionTypeStandalone,
				InvoicingCustomerExternalID: strPtr("cust"),
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 2: Run failing test**

```bash
go test ./internal/api/dto/... -run TestSubscriptionInheritanceConfig_Validate -v
```

Expected: FAIL — new fields don't exist yet.

- [ ] **Step 3: Update `SubscriptionInheritanceConfig` struct and `Validate()`**

Replace the struct and its `Validate()` in `internal/api/dto/subscription.go` (lines 274–298):

```go
// SubscriptionInheritanceConfig groups all hierarchy and invoicing-routing fields for
// subscription creation. The InvoicingBehavior field drives validation of all other fields.
// Absent InvoicingBehavior falls back to legacy auto-detection for backward compat.
type SubscriptionInheritanceConfig struct {
	// InvoicingBehavior explicitly declares the subscription's type/role.
	// Defaults to standalone when absent. Accepted values: standalone, delegated, parent,
	// inherited, grouped_invoicing.
	InvoicingBehavior types.SubscriptionType `json:"invoicing_behavior,omitempty"`

	// ExternalCustomerIDsToInheritSubscription: child customer external IDs for which
	// inherited skeleton subscriptions will be created. Only valid for parent behavior.
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`

	// ParentSubscriptionID links this subscription to an existing parent.
	// Required for inherited and grouped_invoicing; rejected for standalone, delegated, parent.
	ParentSubscriptionID string `json:"parent_subscription_id,omitempty"`

	// InvoicingCustomerExternalID sets a different billing recipient (external ID).
	// Required for delegated; rejected for inherited; optional for others.
	InvoicingCustomerExternalID *string `json:"invoicing_customer_external_id,omitempty"`

	// SubIDsForGroupedInvoicing: existing standalone subscription IDs to convert to
	// grouped_invoicing under this parent at creation time. Only valid for parent behavior.
	SubIDsForGroupedInvoicing []string `json:"sub_ids_for_grouped_invoicing,omitempty"`
}

// Validate enforces per-behavior field constraints.
func (c *SubscriptionInheritanceConfig) Validate() error {
	if c == nil {
		return nil
	}

	behavior := c.InvoicingBehavior

	// Legacy auto-detection path: no InvoicingBehavior set — apply original mutual-exclusivity rules.
	if behavior == "" {
		if c.ParentSubscriptionID != "" && len(c.ExternalCustomerIDsToInheritSubscription) > 0 {
			return ierr.NewError("cannot set parent_subscription_id together with external_customer_ids_to_inherit_subscription").
				WithHint("Use either a parent subscription link or child customers to inherit, not both").
				Mark(ierr.ErrValidation)
		}
		if c.InvoicingCustomerExternalID != nil && len(c.ExternalCustomerIDsToInheritSubscription) > 0 {
			return ierr.NewError("cannot set invoicing_customer_external_id together with external_customer_ids_to_inherit_subscription").
				WithHint("Use either invoicing_customer_external_id or external_customer_ids_to_inherit_subscription, not both").
				Mark(ierr.ErrValidation)
		}
		return nil
	}

	switch behavior {
	case types.SubscriptionTypeStandalone:
		if c.ParentSubscriptionID != "" {
			return ierr.NewError("standalone subscription must not have parent_subscription_id").Mark(ierr.ErrValidation)
		}
		if c.InvoicingCustomerExternalID != nil {
			return ierr.NewError("standalone subscription must not have invoicing_customer_external_id").Mark(ierr.ErrValidation)
		}
		if len(c.SubIDsForGroupedInvoicing) > 0 {
			return ierr.NewError("standalone subscription must not have sub_ids_for_grouped_invoicing").Mark(ierr.ErrValidation)
		}

	case types.SubscriptionTypeDelegated:
		if c.InvoicingCustomerExternalID == nil {
			return ierr.NewError("delegated subscription requires invoicing_customer_external_id").
				WithHint("Set invoicing_customer_external_id to the customer that will receive the invoice").
				Mark(ierr.ErrValidation)
		}
		if c.ParentSubscriptionID != "" {
			return ierr.NewError("delegated subscription must not have parent_subscription_id").Mark(ierr.ErrValidation)
		}

	case types.SubscriptionTypeParent:
		if c.ParentSubscriptionID != "" {
			return ierr.NewError("parent subscription must not have parent_subscription_id").Mark(ierr.ErrValidation)
		}

	case types.SubscriptionTypeInherited:
		if c.ParentSubscriptionID == "" {
			return ierr.NewError("inherited subscription requires parent_subscription_id").
				WithHint("Set parent_subscription_id to the parent subscription ID").
				Mark(ierr.ErrValidation)
		}
		if c.InvoicingCustomerExternalID != nil {
			return ierr.NewError("inherited subscription must not have invoicing_customer_external_id").Mark(ierr.ErrValidation)
		}

	case types.SubscriptionTypeGroupedInvoicing:
		if c.ParentSubscriptionID == "" {
			return ierr.NewError("grouped_invoicing subscription requires parent_subscription_id").
				WithHint("Set parent_subscription_id to the parent subscription ID").
				Mark(ierr.ErrValidation)
		}

	default:
		return ierr.NewError("invalid invoicing_behavior").
			WithHint("invoicing_behavior must be one of: standalone, delegated, parent, inherited, grouped_invoicing").
			Mark(ierr.ErrValidation)
	}

	return nil
}
```

- [ ] **Step 4: Run test — expected PASS**

```bash
go test ./internal/api/dto/... -run TestSubscriptionInheritanceConfig_Validate -v
```

- [ ] **Step 5: Run `go vet ./internal/api/dto/...`**

```bash
go vet ./internal/api/dto/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto/subscription.go internal/api/dto/subscription_test.go
git commit -m "feat(dto): add InvoicingBehavior and SubIDsForGroupedInvoicing to SubscriptionInheritanceConfig"
```

---

## Task 3: Add `PrepareGroupedInvoiceRequestParams` to billing DTO

**Files:**
- Modify: `internal/api/dto/billing.go`

- [ ] **Step 1: Add the new params struct after `PrepareSubscriptionInvoiceRequestParams` (after line 117)**

```go
// PrepareGroupedInvoiceRequestParams holds inputs for PrepareGroupedInvoiceRequest.
// It describes a parent subscription plus its grouped_invoicing children for clubbed invoice generation.
type PrepareGroupedInvoiceRequestParams struct {
	ParentSubscription *subscription.Subscription   `validate:"required"`
	ChildSubscriptions []*subscription.Subscription // may be empty; each must have line items loaded
	PeriodStart        time.Time                    `validate:"required"`
	PeriodEnd          time.Time                    `validate:"required"`
	ReferencePoint     types.InvoiceReferencePoint  `validate:"required"`
}

// Validate enforces struct tags.
func (p *PrepareGroupedInvoiceRequestParams) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}
	return p.ReferencePoint.Validate()
}
```

- [ ] **Step 2: Run `go vet ./internal/api/dto/...` — expected: no errors**

```bash
go vet ./internal/api/dto/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/api/dto/billing.go
git commit -m "feat(dto): add PrepareGroupedInvoiceRequestParams for clubbed invoice billing"
```

---

## Task 4: Create `subscription_grouped_invoicing.go` with membership helpers

**Files:**
- Create: `internal/service/subscription_grouped_invoicing.go`
- Create: `internal/service/subscription_grouped_invoicing_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/subscription_grouped_invoicing_test.go`:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type GroupedInvoicingTestSuite struct {
	suite.Suite
	db  *testutil.TestDB
	svc *subscriptionService
}

func (s *GroupedInvoicingTestSuite) SetupTest() {
	s.db = testutil.SetupTestDB(s.T())
	s.svc = NewSubscriptionService(buildServiceParams(s.db)).(*subscriptionService)
}

func (s *GroupedInvoicingTestSuite) TearDownTest() {
	s.db.Cleanup()
}

func (s *GroupedInvoicingTestSuite) makeParentSub(customerID string) *subscription.Subscription {
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         customerID,
		PlanID:             "plan_test",
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: 1,
		BillingAnchor:      anchor,
		StartDate:          anchor,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeParent,
		BaseModel:          types.GetDefaultBaseModel(context.Background()),
	}
	err := s.db.EntClient.Subscription.Create().SetID(sub.ID).Save(context.Background())
	s.Require().NoError(err)
	return sub
}

func (s *GroupedInvoicingTestSuite) makeStandaloneSub(customerID string, anchor time.Time) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         customerID,
		PlanID:             "plan_test",
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: 1,
		BillingAnchor:      anchor,
		StartDate:          anchor,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BaseModel:          types.GetDefaultBaseModel(context.Background()),
	}
	// persist via repo
	err := s.svc.SubRepo.Create(context.Background(), sub)
	s.Require().NoError(err)
	return sub
}

func (s *GroupedInvoicingTestSuite) TestAddToGroupedInvoicing_Success() {
	ctx := testutil.SetupContext()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	parent := s.makeParentSub("cust_parent")
	child := s.makeStandaloneSub("cust_child", anchor)

	err := s.svc.addToGroupedInvoicing(ctx, parent, child.ID)
	s.NoError(err)

	updated, err := s.svc.SubRepo.Get(ctx, child.ID)
	s.NoError(err)
	s.Equal(types.SubscriptionTypeGroupedInvoicing, updated.SubscriptionType)
	s.Equal(lo.ToPtr(parent.ID), updated.ParentSubscriptionID)
}

func (s *GroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsNonStandalone() {
	ctx := testutil.SetupContext()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	parent := s.makeParentSub("cust_parent")
	child := s.makeStandaloneSub("cust_child", anchor)

	// artificially set child type to inherited
	child.SubscriptionType = types.SubscriptionTypeInherited
	err := s.svc.SubRepo.Update(ctx, child)
	s.Require().NoError(err)

	err = s.svc.addToGroupedInvoicing(ctx, parent, child.ID)
	s.Error(err)
}

func (s *GroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsBillingPeriodMismatch() {
	ctx := testutil.SetupContext()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	parent := s.makeParentSub("cust_parent")

	// child has a different billing period
	child := s.makeStandaloneSub("cust_child", anchor)
	child.BillingPeriod = types.BillingPeriodYearly
	err := s.svc.SubRepo.Update(ctx, child)
	s.Require().NoError(err)

	err = s.svc.addToGroupedInvoicing(ctx, parent, child.ID)
	s.Error(err)
}

func (s *GroupedInvoicingTestSuite) TestAddToGroupedInvoicing_RejectsChildStartBeforeParent() {
	ctx := testutil.SetupContext()
	parentAnchor := time.Now().UTC().Truncate(24 * time.Hour)
	parent := s.makeParentSub("cust_parent")
	parent.StartDate = parentAnchor

	childAnchor := parentAnchor.AddDate(0, -1, 0) // one month before parent
	child := s.makeStandaloneSub("cust_child", childAnchor)

	err := s.svc.addToGroupedInvoicing(ctx, parent, child.ID)
	s.Error(err)
}

func (s *GroupedInvoicingTestSuite) TestRemoveFromGroupedInvoicing_Success() {
	ctx := testutil.SetupContext()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	parent := s.makeParentSub("cust_parent")
	child := s.makeStandaloneSub("cust_child", anchor)

	// first add
	err := s.svc.addToGroupedInvoicing(ctx, parent, child.ID)
	s.Require().NoError(err)

	// then remove
	err = s.svc.removeFromGroupedInvoicing(ctx, child.ID)
	s.NoError(err)

	updated, err := s.svc.SubRepo.Get(ctx, child.ID)
	s.NoError(err)
	s.Equal(types.SubscriptionTypeStandalone, updated.SubscriptionType)
	s.Nil(updated.ParentSubscriptionID)
}

func (s *GroupedInvoicingTestSuite) TestRemoveFromGroupedInvoicing_RejectsNonGroupedType() {
	ctx := testutil.SetupContext()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	child := s.makeStandaloneSub("cust_child", anchor)

	err := s.svc.removeFromGroupedInvoicing(ctx, child.ID)
	s.Error(err)
}

func (s *GroupedInvoicingTestSuite) TestGetGroupedInvoicingSubscriptions() {
	ctx := testutil.SetupContext()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	parent := s.makeParentSub("cust_parent")
	child1 := s.makeStandaloneSub("cust_child1", anchor)
	child2 := s.makeStandaloneSub("cust_child2", anchor)

	err := s.svc.addToGroupedInvoicing(ctx, parent, child1.ID)
	s.Require().NoError(err)
	err = s.svc.addToGroupedInvoicing(ctx, parent, child2.ID)
	s.Require().NoError(err)

	children, err := s.svc.getGroupedInvoicingSubscriptions(ctx, parent.ID)
	s.NoError(err)
	s.Len(children, 2)
}

func TestGroupedInvoicingTestSuite(t *testing.T) {
	suite.Run(t, new(GroupedInvoicingTestSuite))
}
```

- [ ] **Step 2: Run failing tests**

```bash
go test ./internal/service/... -run TestGroupedInvoicingTestSuite -v
```

Expected: FAIL — methods not defined yet.

- [ ] **Step 3: Create `internal/service/subscription_grouped_invoicing.go`**

```go
package service

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// addToGroupedInvoicing converts a standalone subscription to grouped_invoicing under parentSub.
// Validates all billing constraints before persisting.
func (s *subscriptionService) addToGroupedInvoicing(ctx context.Context, parentSub interface{ GetID() string; GetBillingPeriod() types.BillingPeriod; GetBillingPeriodCount() int; GetBillingAnchor() interface{} }, childSubID string) error {
	// Use concrete type from domain
	return s.addToGroupedInvoicingByID(ctx, parentSub, childSubID)
}
```

Actually, let me write this properly without the interface trick:

```go
package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// addToGroupedInvoicing converts childSubID from standalone to grouped_invoicing under parentSub.
// All nine constraints are validated before any write.
func (s *subscriptionService) addToGroupedInvoicing(ctx context.Context, parentSub *subscription.Subscription, childSubID string) error {
	child, err := s.SubRepo.Get(ctx, childSubID)
	if err != nil {
		return err
	}

	// 1. Child must be standalone
	if child.SubscriptionType != types.SubscriptionTypeStandalone {
		return ierr.NewError("child subscription must be standalone to join grouped invoicing").
			WithHint("Only standalone subscriptions can be converted to grouped_invoicing").
			WithReportableDetails(map[string]any{
				"child_subscription_id": childSubID,
				"subscription_type":     child.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	// 2. Child must be active or trialing
	if child.SubscriptionStatus != types.SubscriptionStatusActive &&
		child.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return ierr.NewError("child subscription must be active or trialing").
			WithReportableDetails(map[string]any{
				"child_subscription_id": childSubID,
				"status":                child.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 3. Child must not already have a parent
	if child.ParentSubscriptionID != nil {
		return ierr.NewError("child subscription already has a parent_subscription_id").
			Mark(ierr.ErrValidation)
	}

	// 4. Parent must be of type parent
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return ierr.NewError("parent subscription must have type parent").
			WithReportableDetails(map[string]any{
				"parent_subscription_id": parentSub.ID,
				"subscription_type":      parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	// 5. Parent must be active or trialing
	if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
		parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return ierr.NewError("parent subscription must be active or trialing").
			WithReportableDetails(map[string]any{
				"parent_subscription_id": parentSub.ID,
				"status":                 parentSub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 6. Billing period must match
	if child.BillingPeriod != parentSub.BillingPeriod {
		return ierr.NewError("child billing_period must match parent").
			WithReportableDetails(map[string]any{
				"parent_billing_period": parentSub.BillingPeriod,
				"child_billing_period":  child.BillingPeriod,
			}).
			Mark(ierr.ErrValidation)
	}

	// 7. Billing period count must match
	if child.BillingPeriodCount != parentSub.BillingPeriodCount {
		return ierr.NewError("child billing_period_count must match parent").
			WithReportableDetails(map[string]any{
				"parent_billing_period_count": parentSub.BillingPeriodCount,
				"child_billing_period_count":  child.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// 8. Billing anchor must match
	if !child.BillingAnchor.Equal(parentSub.BillingAnchor) {
		return ierr.NewError("child billing_anchor must match parent").
			WithReportableDetails(map[string]any{
				"parent_billing_anchor": parentSub.BillingAnchor,
				"child_billing_anchor":  child.BillingAnchor,
			}).
			Mark(ierr.ErrValidation)
	}

	// 9. Child start date must be >= parent start date
	if child.StartDate.Before(parentSub.StartDate) {
		return ierr.NewError("child start_date must be >= parent start_date").
			WithReportableDetails(map[string]any{
				"parent_start_date": parentSub.StartDate,
				"child_start_date":  child.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}

	child.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
	child.ParentSubscriptionID = lo.ToPtr(parentSub.ID)

	return s.SubRepo.Update(ctx, child)
}

// removeFromGroupedInvoicing reverts a grouped_invoicing subscription to standalone.
// Returns error if child is not of grouped_invoicing type.
func (s *subscriptionService) removeFromGroupedInvoicing(ctx context.Context, childSubID string) error {
	child, err := s.SubRepo.Get(ctx, childSubID)
	if err != nil {
		return err
	}
	if child.SubscriptionType != types.SubscriptionTypeGroupedInvoicing {
		return ierr.NewError("subscription is not of type grouped_invoicing").
			WithReportableDetails(map[string]any{
				"subscription_id":   childSubID,
				"subscription_type": child.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}
	child.SubscriptionType = types.SubscriptionTypeStandalone
	child.ParentSubscriptionID = nil
	return s.SubRepo.Update(ctx, child)
}

// getGroupedInvoicingSubscriptions retrieves all grouped_invoicing children for a parent subscription.
func (s *subscriptionService) getGroupedInvoicingSubscriptions(ctx context.Context, parentSubID string) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentSubID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeGroupedInvoicing}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusDraft,
	}
	return s.SubRepo.List(ctx, filter)
}
```

- [ ] **Step 4: Run tests — expected PASS**

```bash
go test ./internal/service/... -run TestGroupedInvoicingTestSuite -v
```

- [ ] **Step 5: Run `go vet ./internal/service/...`**

```bash
go vet ./internal/service/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/subscription_grouped_invoicing.go internal/service/subscription_grouped_invoicing_test.go
git commit -m "feat(service): add grouped invoicing membership helpers (add/remove/get)"
```

---

## Task 5: Update `prepareSubscriptionInheritanceForCreate`

**Files:**
- Modify: `internal/service/subscription.go:7309-7392`

- [ ] **Step 1: Write failing test for grouped_invoicing child creation via create path**

Add to `internal/service/subscription_test.go`:

```go
func (s *SubscriptionServiceTestSuite) TestCreateSubscription_GroupedInvoicingChild() {
	ctx := s.ctx
	// Create a parent subscription first
	parent := s.createTestParentSubscription("cust_parent")

	// Create a standalone child with matching billing config
	req := dto.CreateSubscriptionRequest{
		CustomerID:         "cust_child",
		PlanID:             parent.PlanID,
		Currency:           parent.Currency,
		BillingPeriod:      parent.BillingPeriod,
		BillingPeriodCount: parent.BillingPeriodCount,
		BillingAnchor:      &parent.BillingAnchor,
		StartDate:          &parent.StartDate,
		Inheritance: &dto.SubscriptionInheritanceConfig{
			InvoicingBehavior:    types.SubscriptionTypeGroupedInvoicing,
			ParentSubscriptionID: parent.ID,
		},
	}
	resp, err := s.subscriptionService.CreateSubscription(ctx, req)
	s.Require().NoError(err)
	s.Equal(string(types.SubscriptionTypeGroupedInvoicing), resp.SubscriptionType)
	s.Equal(parent.ID, lo.FromPtr(resp.ParentSubscriptionID))
}

func (s *SubscriptionServiceTestSuite) TestCreateSubscription_DelegatedType() {
	ctx := s.ctx
	invoicingCustomer := s.createTestCustomer("cust_invoicing")

	req := dto.CreateSubscriptionRequest{
		CustomerID:    "cust_subscriber",
		PlanID:        s.testPlan.ID,
		Currency:      "usd",
		BillingPeriod: types.BillingPeriodMonthly,
		Inheritance: &dto.SubscriptionInheritanceConfig{
			InvoicingBehavior:           types.SubscriptionTypeDelegated,
			InvoicingCustomerExternalID: lo.ToPtr(invoicingCustomer.ExternalID),
		},
	}
	resp, err := s.subscriptionService.CreateSubscription(ctx, req)
	s.Require().NoError(err)
	s.Equal(string(types.SubscriptionTypeDelegated), resp.SubscriptionType)
	s.Equal(invoicingCustomer.ID, lo.FromPtr(resp.InvoicingCustomerID))
}

func (s *SubscriptionServiceTestSuite) TestCreateSubscription_ParentWithSubIDsForGroupedInvoicing() {
	ctx := s.ctx
	// create two existing standalone subs first
	child1 := s.createTestStandaloneSubscription("cust_c1")
	child2 := s.createTestStandaloneSubscription("cust_c2")

	req := dto.CreateSubscriptionRequest{
		CustomerID:    "cust_parent",
		PlanID:        s.testPlan.ID,
		Currency:      "usd",
		BillingPeriod: child1.BillingPeriod,
		BillingAnchor: &child1.BillingAnchor,
		Inheritance: &dto.SubscriptionInheritanceConfig{
			InvoicingBehavior:         types.SubscriptionTypeParent,
			SubIDsForGroupedInvoicing: []string{child1.ID, child2.ID},
		},
	}
	resp, err := s.subscriptionService.CreateSubscription(ctx, req)
	s.Require().NoError(err)
	s.Equal(string(types.SubscriptionTypeParent), resp.SubscriptionType)

	// Verify children were converted
	c1, _ := s.subscriptionService.GetSubscription(ctx, child1.ID)
	s.Equal(string(types.SubscriptionTypeGroupedInvoicing), c1.SubscriptionType)
	s.Equal(resp.ID, lo.FromPtr(c1.ParentSubscriptionID))
}
```

- [ ] **Step 2: Run failing tests**

```bash
go test ./internal/service/... -run "TestCreateSubscription_GroupedInvoicingChild|TestCreateSubscription_DelegatedType|TestCreateSubscription_ParentWithSubIDsForGroupedInvoicing" -v
```

Expected: FAIL.

- [ ] **Step 3: Update `prepareSubscriptionInheritanceForCreate` in `internal/service/subscription.go`**

Replace the function body (lines 7313–7392). The new version switches on `InvoicingBehavior` when set; falls back to legacy detection when absent:

```go
func (s *subscriptionService) prepareSubscriptionInheritanceForCreate(ctx context.Context, req *dto.CreateSubscriptionRequest, sub *subscription.Subscription) (groupedInvoicingSubIDs []string, childCustomerIDs []string, err error) {
	if req.Inheritance == nil {
		sub.SubscriptionType = types.SubscriptionTypeStandalone
		return nil, nil, nil
	}

	inh := req.Inheritance
	if err := inh.Validate(); err != nil {
		return nil, nil, err
	}

	behavior := inh.InvoicingBehavior

	// Legacy auto-detection: InvoicingBehavior not set — preserve original logic
	if behavior == "" {
		if inh.ParentSubscriptionID != "" {
			parentSub, err := s.SubRepo.Get(ctx, inh.ParentSubscriptionID)
			if err != nil {
				return nil, nil, err
			}
			if parentSub.SubscriptionStatus != types.SubscriptionStatusActive {
				return nil, nil, ierr.NewError("parent subscription is not active").
					WithHint("The parent subscription must be active").
					WithReportableDetails(map[string]interface{}{"parent_subscription_id": inh.ParentSubscriptionID}).
					Mark(ierr.ErrValidation)
			}
			sub.InvoicingCustomerID = parentSub.InvoicingCustomerID
			sub.SubscriptionType = types.SubscriptionTypeInherited
			sub.ParentSubscriptionID = lo.ToPtr(inh.ParentSubscriptionID)
		}
		if inh.InvoicingCustomerExternalID != nil {
			invoicingCustomer, err := s.CustomerRepo.GetByLookupKey(ctx, lo.FromPtr(inh.InvoicingCustomerExternalID))
			if err != nil {
				return nil, nil, err
			}
			if invoicingCustomer.Status != types.StatusPublished {
				return nil, nil, ierr.NewError("invoicing customer is not active").Mark(ierr.ErrValidation)
			}
			sub.InvoicingCustomerID = lo.ToPtr(invoicingCustomer.ID)
		}
		if len(inh.ExternalCustomerIDsToInheritSubscription) > 0 {
			resolved, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, inh.ExternalCustomerIDsToInheritSubscription)
			if err != nil {
				return nil, nil, err
			}
			childCustomerIDs = resolved
		}
		if len(childCustomerIDs) > 0 {
			sub.SubscriptionType = types.SubscriptionTypeParent
		} else if sub.SubscriptionType == "" {
			sub.SubscriptionType = types.SubscriptionTypeStandalone
		}
		return nil, childCustomerIDs, s.validateNoInheritedSubForSubscriber(ctx, sub)
	}

	switch behavior {
	case types.SubscriptionTypeStandalone:
		sub.SubscriptionType = types.SubscriptionTypeStandalone

	case types.SubscriptionTypeDelegated:
		invoicingCustomer, err := s.CustomerRepo.GetByLookupKey(ctx, lo.FromPtr(inh.InvoicingCustomerExternalID))
		if err != nil {
			return nil, nil, err
		}
		if invoicingCustomer.Status != types.StatusPublished {
			return nil, nil, ierr.NewError("invoicing customer is not active").Mark(ierr.ErrValidation)
		}
		sub.InvoicingCustomerID = lo.ToPtr(invoicingCustomer.ID)
		sub.SubscriptionType = types.SubscriptionTypeDelegated

	case types.SubscriptionTypeParent:
		sub.SubscriptionType = types.SubscriptionTypeParent
		if inh.InvoicingCustomerExternalID != nil {
			invoicingCustomer, err := s.CustomerRepo.GetByLookupKey(ctx, lo.FromPtr(inh.InvoicingCustomerExternalID))
			if err != nil {
				return nil, nil, err
			}
			if invoicingCustomer.Status != types.StatusPublished {
				return nil, nil, ierr.NewError("invoicing customer is not active").Mark(ierr.ErrValidation)
			}
			sub.InvoicingCustomerID = lo.ToPtr(invoicingCustomer.ID)
		}
		if len(inh.ExternalCustomerIDsToInheritSubscription) > 0 {
			resolved, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, inh.ExternalCustomerIDsToInheritSubscription)
			if err != nil {
				return nil, nil, err
			}
			childCustomerIDs = resolved
		}
		// SubIDsForGroupedInvoicing are processed post-create (parent.ID not known yet)
		groupedInvoicingSubIDs = inh.SubIDsForGroupedInvoicing

	case types.SubscriptionTypeInherited:
		parentSub, err := s.SubRepo.Get(ctx, inh.ParentSubscriptionID)
		if err != nil {
			return nil, nil, err
		}
		if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
			parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
			return nil, nil, ierr.NewError("parent subscription is not active or trialing").Mark(ierr.ErrValidation)
		}
		sub.InvoicingCustomerID = parentSub.InvoicingCustomerID
		sub.SubscriptionType = types.SubscriptionTypeInherited
		sub.ParentSubscriptionID = lo.ToPtr(inh.ParentSubscriptionID)

	case types.SubscriptionTypeGroupedInvoicing:
		parentSub, err := s.SubRepo.Get(ctx, inh.ParentSubscriptionID)
		if err != nil {
			return nil, nil, err
		}
		// Constraints validated inside addToGroupedInvoicing; set type and parent now
		// (actual validation and write happen after sub is persisted in CreateSubscription)
		sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
		sub.ParentSubscriptionID = lo.ToPtr(inh.ParentSubscriptionID)
		// Re-validate constraints (addToGroupedInvoicing will also validate, this is an early check)
		if parentSub.SubscriptionType != types.SubscriptionTypeParent {
			return nil, nil, ierr.NewError("parent subscription must have type parent").Mark(ierr.ErrValidation)
		}
	}

	return groupedInvoicingSubIDs, childCustomerIDs, s.validateNoInheritedSubForSubscriber(ctx, sub)
}

// validateNoInheritedSubForSubscriber rejects standalone/parent/delegated creation when
// the subscriber already has an inherited subscription under another parent.
func (s *subscriptionService) validateNoInheritedSubForSubscriber(ctx context.Context, sub *subscription.Subscription) error {
	skipTypes := map[types.SubscriptionType]bool{
		types.SubscriptionTypeInherited:        true,
		types.SubscriptionTypeGroupedInvoicing: true,
	}
	if skipTypes[sub.SubscriptionType] {
		return nil
	}
	subscriberFilter := types.NewSubscriptionFilter()
	subscriberFilter.CustomerID = sub.CustomerID
	subscriberFilter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	subscriberFilter.Status = lo.ToPtr(types.StatusPublished)
	subscriberFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusDraft,
		types.SubscriptionStatusTrialing,
	}
	subscriberFilter.WithLineItems = false
	subscriberFilter.Limit = lo.ToPtr(1)
	count, err := s.SubRepo.Count(ctx, subscriberFilter)
	if err != nil {
		return err
	}
	if count > 0 {
		return ierr.NewError("customer already has an inherited subscription").
			WithHint("A customer that receives a subscription through hierarchy cannot create a standalone, parent, or delegated subscription.").
			WithReportableDetails(map[string]interface{}{"customer_id": sub.CustomerID}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 4: Update call-site in `CreateSubscription` (line ~272)**

Change the call from:
```go
childCustomerIDs, err := s.prepareSubscriptionInheritanceForCreate(ctx, &req, sub)
```
to:
```go
groupedInvoicingSubIDs, childCustomerIDs, err := s.prepareSubscriptionInheritanceForCreate(ctx, &req, sub)
```

Also after the existing inherited children loop (after line ~455), add:
```go
// Convert standalone subs to grouped_invoicing under the newly created parent
for _, gSubID := range groupedInvoicingSubIDs {
    if err := s.addToGroupedInvoicing(ctx, sub, gSubID); err != nil {
        return err
    }
}
```

- [ ] **Step 5: Run tests — expected PASS**

```bash
go test ./internal/service/... -run "TestCreateSubscription_GroupedInvoicingChild|TestCreateSubscription_DelegatedType|TestCreateSubscription_ParentWithSubIDsForGroupedInvoicing" -v
```

- [ ] **Step 6: Run full service tests to check for regressions**

```bash
go test ./internal/service/... -timeout 300s
```

- [ ] **Step 7: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_test.go
git commit -m "feat(service): update prepareSubscriptionInheritanceForCreate for new subscription types"
```

---

## Task 6: Add `PrepareGroupedInvoiceRequest` to billing service

**Files:**
- Modify: `internal/service/billing.go`

- [ ] **Step 1: Add method to `BillingService` interface (after line 80)**

```go
// PrepareGroupedInvoiceRequest builds a single CreateInvoiceRequest by calling
// PrepareSubscriptionInvoiceRequest for the parent and each grouped_invoicing child,
// then flat-merging all LineItems. The invoice will be raised against the parent's
// GetInvoicingCustomerID().
PrepareGroupedInvoiceRequest(ctx context.Context, params *dto.PrepareGroupedInvoiceRequestParams) (*dto.CreateInvoiceRequest, error)
```

- [ ] **Step 2: Implement the method on `billingService`**

Add after the existing `PrepareSubscriptionInvoiceRequest` implementation:

```go
func (s *billingService) PrepareGroupedInvoiceRequest(ctx context.Context, params *dto.PrepareGroupedInvoiceRequestParams) (*dto.CreateInvoiceRequest, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	// Build invoice request for parent
	baseReq, err := s.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   params.ParentSubscription,
		PeriodStart:    params.PeriodStart,
		PeriodEnd:      params.PeriodEnd,
		ReferencePoint: params.ReferencePoint,
	})
	if err != nil {
		return nil, err
	}

	// Merge each child's line items into the base request
	for _, child := range params.ChildSubscriptions {
		childReq, err := s.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
			Subscription:   child,
			PeriodStart:    params.PeriodStart,
			PeriodEnd:      params.PeriodEnd,
			ReferencePoint: params.ReferencePoint,
		})
		if err != nil {
			return nil, err
		}
		baseReq.LineItems = append(baseReq.LineItems, childReq.LineItems...)
	}

	// Recalculate subtotal and amount_due from merged line items
	var subtotal decimal.Decimal
	for _, li := range baseReq.LineItems {
		subtotal = subtotal.Add(li.Amount)
	}
	baseReq.Subtotal = subtotal
	baseReq.Total = subtotal
	baseReq.AmountDue = subtotal

	return baseReq, nil
}
```

**Note:** The `decimal` import must already be in the file. Check the existing imports — if not present, add `"github.com/shopspring/decimal"`.

- [ ] **Step 3: Run `go vet ./internal/service/...`**

```bash
go vet ./internal/service/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/service/billing.go
git commit -m "feat(billing): add PrepareGroupedInvoiceRequest for clubbed invoice line item merging"
```

---

## Task 7: Update invoice service — skip `grouped_invoicing`, add `CreateGroupedSubscriptionInvoice`

**Files:**
- Modify: `internal/service/invoice.go:380`

- [ ] **Step 1: Extend the skip check in `ComputeInvoice` (line 380)**

Change:
```go
if sub.SubscriptionType == types.SubscriptionTypeInherited {
    return true, nil
}
```
to:
```go
if sub.SubscriptionType == types.SubscriptionTypeInherited ||
    sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
    return true, nil
}
```

- [ ] **Step 2: Add `CreateGroupedSubscriptionInvoice` to the `InvoiceService` interface**

Find the `InvoiceService` interface definition in `internal/service/invoice.go` and add:

```go
// CreateGroupedSubscriptionInvoice generates a single clubbed invoice for a parent
// subscription plus all its grouped_invoicing children for the given period.
// It advances each child's current_period_start/end after successful invoice creation.
CreateGroupedSubscriptionInvoice(
    ctx context.Context,
    parentSub *subscription.Subscription,
    childSubs []*subscription.Subscription,
    periodStart, periodEnd time.Time,
    paymentParams *dto.PaymentParameters,
) (*dto.InvoiceResponse, error)
```

- [ ] **Step 3: Implement `CreateGroupedSubscriptionInvoice`**

Add the method to `invoiceService` after `CreateSubscriptionInvoice` (after line ~1870):

```go
func (s *invoiceService) CreateGroupedSubscriptionInvoice(
	ctx context.Context,
	parentSub *subscription.Subscription,
	childSubs []*subscription.Subscription,
	periodStart, periodEnd time.Time,
	paymentParams *dto.PaymentParameters,
) (*dto.InvoiceResponse, error) {
	billingService := NewBillingService(s.ServiceParams)

	// Load line items for parent and each child
	parentWithItems, _, err := s.SubRepo.GetWithLineItems(ctx, parentSub.ID)
	if err != nil {
		return nil, err
	}
	childWithItems := make([]*subscription.Subscription, 0, len(childSubs))
	for _, ch := range childSubs {
		c, _, err := s.SubRepo.GetWithLineItems(ctx, ch.ID)
		if err != nil {
			return nil, err
		}
		childWithItems = append(childWithItems, c)
	}

	// Build merged invoice request
	mergedReq, err := billingService.PrepareGroupedInvoiceRequest(ctx, &dto.PrepareGroupedInvoiceRequestParams{
		ParentSubscription: parentWithItems,
		ChildSubscriptions: childWithItems,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		ReferencePoint:     types.ReferencePointPeriodEnd,
	})
	if err != nil {
		return nil, err
	}

	// Skip if no line items (nothing to invoice)
	if len(mergedReq.LineItems) == 0 {
		return nil, nil
	}

	// Create draft against parent's invoicing customer
	draftReq := dto.CreateDraftInvoiceRequest{
		CustomerID:     parentSub.GetInvoicingCustomerID(),
		SubscriptionID: &parentSub.ID,
		Currency:       parentSub.Currency,
		BillingPeriod:  lo.ToPtr(string(parentSub.BillingPeriod)),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
	}
	draft, err := s.CreateEmptyDraftInvoice(ctx, draftReq)
	if err != nil {
		return nil, err
	}

	// Manually populate the draft with merged line items and compute totals
	skipped, err := s.computeInvoiceWithRequest(ctx, draft.ID, mergedReq)
	if err != nil {
		return nil, err
	}
	if skipped {
		return nil, nil
	}

	if err := s.ProcessDraftInvoice(ctx, draft.ID, paymentParams, parentWithItems, types.InvoiceFlowRenewal); err != nil {
		return nil, err
	}

	inv, err := s.InvoiceRepo.Get(ctx, draft.ID)
	if err != nil {
		return nil, err
	}
	return dto.NewInvoiceResponse(inv), nil
}
```

**Note:** `computeInvoiceWithRequest` is a new internal helper that applies a pre-built `CreateInvoiceRequest` to a draft invoice instead of recomputing it from the subscription's line items. If `ComputeInvoice` is currently monolithic, extract a thin wrapper or add a flag. Check the existing `ComputeInvoice` signature and adapt accordingly — the key change is bypassing the re-fetch of line items from the subscription.

- [ ] **Step 4: Run `go vet ./internal/service/...`**

```bash
go vet ./internal/service/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/invoice.go
git commit -m "feat(invoice): skip grouped_invoicing in ComputeInvoice; add CreateGroupedSubscriptionInvoice"
```

---

## Task 8: Wire grouped invoice into `processSubscriptionPeriod`

**Files:**
- Modify: `internal/service/subscription.go:2704-3038`

- [ ] **Step 1: Write failing integration test**

Add to `internal/service/subscription_test.go`:

```go
func (s *SubscriptionServiceTestSuite) TestUpdateBillingPeriods_GroupedInvoicingChildSkipped() {
	ctx := s.ctx
	// Create parent with one grouped_invoicing child
	parent := s.createTestParentSubscription("cust_parent")
	child := s.createTestStandaloneSubscription("cust_child")
	err := s.subscriptionService.(*subscriptionService).addToGroupedInvoicing(ctx, parent, child.ID)
	s.Require().NoError(err)

	// Expire child's period
	child.CurrentPeriodEnd = time.Now().UTC().Add(-1 * time.Hour)
	err = s.subscriptionService.(*subscriptionService).SubRepo.Update(ctx, child)
	s.Require().NoError(err)

	resp, err := s.subscriptionService.UpdateBillingPeriods(ctx)
	s.Require().NoError(err)

	// Child should appear in response but no invoice should be created for it
	var childItem *dto.SubscriptionUpdatePeriodResponseItem
	for _, item := range resp.Items {
		if item.SubscriptionID == child.ID {
			childItem = item
		}
	}
	s.Require().NotNil(childItem, "child should be picked up by cron")
	s.True(childItem.Success, "child period processing should succeed (skipped)")

	// No invoice should exist for the child
	invoices, err := s.invoiceService.ListInvoices(ctx, &dto.ListInvoicesRequest{SubscriptionID: lo.ToPtr(child.ID)})
	s.Require().NoError(err)
	s.Empty(invoices.Items)
}

func (s *SubscriptionServiceTestSuite) TestUpdateBillingPeriods_ParentGeneratesGroupedInvoice() {
	ctx := s.ctx
	parent := s.createTestParentSubscription("cust_parent")
	child := s.createTestStandaloneSubscription("cust_child")
	err := s.subscriptionService.(*subscriptionService).addToGroupedInvoicing(ctx, parent, child.ID)
	s.Require().NoError(err)

	// Expire parent's period
	parent.CurrentPeriodEnd = time.Now().UTC().Add(-1 * time.Hour)
	err = s.subscriptionService.(*subscriptionService).SubRepo.Update(ctx, parent)
	s.Require().NoError(err)

	_, err = s.subscriptionService.UpdateBillingPeriods(ctx)
	s.Require().NoError(err)

	// One invoice should exist for the parent's invoicing customer
	invoices, err := s.invoiceService.ListInvoices(ctx, &dto.ListInvoicesRequest{SubscriptionID: lo.ToPtr(parent.ID)})
	s.Require().NoError(err)
	s.NotEmpty(invoices.Items)
}
```

- [ ] **Step 2: Run failing tests**

```bash
go test ./internal/service/... -run "TestUpdateBillingPeriods_GroupedInvoicing" -v
```

Expected: FAIL.

- [ ] **Step 3: Update `processSubscriptionPeriod` to skip `grouped_invoicing` children**

In `processSubscriptionPeriod` (line ~2704), after the existing draft/paused skip blocks and before the invoice creation loop, add:

```go
// Skip invoice creation (and period advance) for grouped_invoicing children.
// The parent's processSubscriptionPeriod handles their invoices and advances their periods.
if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
    s.Logger.InfowCtx(ctx, "skipping period processing for grouped_invoicing child subscription",
        "subscription_id", sub.ID,
        "parent_subscription_id", sub.ParentSubscriptionID)
    return nil
}
```

- [ ] **Step 4: Add grouped invoice path inside `processSubscriptionPeriod` — before advancing parent's period**

Inside the `s.DB.WithTx` block, right after the existing per-period invoice creation loop and before `sub.CurrentPeriodStart = newPeriod.start`, add:

```go
// For parent subs: collect grouped_invoicing children and generate a single clubbed invoice
if sub.SubscriptionType == types.SubscriptionTypeParent {
    groupedChildren, err := s.getGroupedInvoicingSubscriptions(ctx, sub.ID)
    if err != nil {
        return err
    }
    if len(groupedChildren) > 0 {
        invoiceService := NewInvoiceService(s.ServiceParams)
        paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID).NormalizePaymentParameters()
        _, err := invoiceService.CreateGroupedSubscriptionInvoice(
            ctx,
            sub,
            groupedChildren,
            period.start, // use the last completed period
            period.end,
            paymentParams,
        )
        if err != nil {
            return err
        }
        // Advance each child's billing period to match parent's new period
        newPeriod := periods[len(periods)-1]
        for _, child := range groupedChildren {
            child.CurrentPeriodStart = newPeriod.start
            child.CurrentPeriodEnd = newPeriod.end
            if err := s.SubRepo.Update(ctx, child); err != nil {
                return err
            }
        }
    }
}
```

**Note:** The above snippet runs for each completed period `i` in the loop. Verify the exact placement against the `for i := 0; i < len(periods)-1; i++` loop in `processSubscriptionPeriod` (around line 2911) and insert inside that loop, after `CreateSubscriptionInvoice` is called for the parent, conditioned on `sub.SubscriptionType == types.SubscriptionTypeParent`.

- [ ] **Step 5: Run tests — expected PASS**

```bash
go test ./internal/service/... -run "TestUpdateBillingPeriods_GroupedInvoicing" -v
```

- [ ] **Step 6: Run full test suite**

```bash
go test ./internal/service/... -timeout 300s
```

- [ ] **Step 7: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_test.go
git commit -m "feat(billing): wire grouped invoicing into UpdateBillingPeriods"
```

---

## Task 9: Add grouped invoicing membership to `SubscriptionModificationService`

**Files:**
- Modify: `internal/api/dto/subscription_modification.go`
- Modify: `internal/service/subscription_modification.go`

The existing `SubscriptionModificationService` already has `Execute`/`Preview` methods that switch on `req.Type`. This task adds two new type values and wires them in.

- [ ] **Step 1: Add new constants and DTO to `internal/api/dto/subscription_modification.go`**

After the existing `SubscriptionModifyTypeQuantityChange` constant, add:

```go
SubscriptionModifyTypeGroupedInvoicingAdd    SubscriptionModifyType = "grouped_invoicing_add"
SubscriptionModifyTypeGroupedInvoicingRemove SubscriptionModifyType = "grouped_invoicing_remove"
```

Add the params struct before `ExecuteSubscriptionModifyRequest`:

```go
// SubModifyGroupedInvoicingParams is the payload for grouped invoicing membership changes.
type SubModifyGroupedInvoicingParams struct {
	// ParentSubscriptionID is required for grouped_invoicing_add.
	ParentSubscriptionID string   `json:"parent_subscription_id,omitempty"`
	ChildSubscriptionIDs []string `json:"child_subscription_ids" validate:"required,min=1"`
}

func (r *SubModifyGroupedInvoicingParams) Validate(modifyType SubscriptionModifyType) error {
	if len(r.ChildSubscriptionIDs) == 0 {
		return ierr.NewError("child_subscription_ids must not be empty").
			WithHint("Provide child_subscription_ids with at least one entry").
			Mark(ierr.ErrValidation)
	}
	if modifyType == SubscriptionModifyTypeGroupedInvoicingAdd && r.ParentSubscriptionID == "" {
		return ierr.NewError("parent_subscription_id is required for grouped_invoicing_add").
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

Add `GroupedInvoicingParams` field to `ExecuteSubscriptionModifyRequest`:

```go
type ExecuteSubscriptionModifyRequest struct {
	Type                 SubscriptionModifyType          `json:"type" binding:"required"`
	InheritanceParams    *SubModifyInheritanceRequest    `json:"inheritance_params,omitempty"`
	QuantityChangeParams *SubModifyQuantityChangeRequest `json:"quantity_change_params,omitempty"`
	GroupedInvoicingParams *SubModifyGroupedInvoicingParams `json:"grouped_invoicing_params,omitempty"`
}
```

Update `Validate()` — add two new cases to the switch:

```go
case SubscriptionModifyTypeGroupedInvoicingAdd, SubscriptionModifyTypeGroupedInvoicingRemove:
	if r.GroupedInvoicingParams == nil {
		return ierr.NewError("grouped_invoicing_params is required for type '" + string(r.Type) + "'").
			Mark(ierr.ErrValidation)
	}
	return r.GroupedInvoicingParams.Validate(r.Type)
```

Also update the `default` hint to include the new values:
```go
WithHint("Valid values: inheritance, quantity_change, grouped_invoicing_add, grouped_invoicing_remove").
```

- [ ] **Step 2: Write tests for the new validation**

In `internal/api/dto/subscription_modification_test.go` (create if it doesn't exist), add table-driven tests:

```go
package dto_test

import (
	"testing"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/stretchr/testify/require"
)

func TestExecuteSubscriptionModifyRequest_Validate_GroupedInvoicing(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.ExecuteSubscriptionModifyRequest
		wantErr bool
	}{
		{
			name: "grouped_invoicing_add with valid params",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
					ParentSubscriptionID: "parent_123",
					ChildSubscriptionIDs: []string{"child_1", "child_2"},
				},
			},
		},
		{
			name: "grouped_invoicing_add missing parent_subscription_id",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
					ChildSubscriptionIDs: []string{"child_1"},
				},
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing_add missing params",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
			},
			wantErr: true,
		},
		{
			name: "grouped_invoicing_remove with valid params — parent not required",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingRemove,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
					ChildSubscriptionIDs: []string{"child_1"},
				},
			},
		},
		{
			name: "grouped_invoicing_remove missing child_subscription_ids",
			req: dto.ExecuteSubscriptionModifyRequest{
				Type: dto.SubscriptionModifyTypeGroupedInvoicingRemove,
				GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
```

Run: `go test ./internal/api/dto/... -run TestExecuteSubscriptionModifyRequest_Validate_GroupedInvoicing -v`
Expected: PASS

- [ ] **Step 3: Wire into `SubscriptionModificationService` in `internal/service/subscription_modification.go`**

In `Execute()`, add to the switch:

```go
case dto.SubscriptionModifyTypeGroupedInvoicingAdd, dto.SubscriptionModifyTypeGroupedInvoicingRemove:
	return s.executeGroupedInvoicingMembership(ctx, req.Type, req.GroupedInvoicingParams)
```

In `Preview()`, add to the switch:

```go
case dto.SubscriptionModifyTypeGroupedInvoicingAdd, dto.SubscriptionModifyTypeGroupedInvoicingRemove:
	return s.previewGroupedInvoicingMembership(ctx, req.Type, req.GroupedInvoicingParams)
```

Also update the `default` hint in both switches to include the new values.

- [ ] **Step 4: Implement `executeGroupedInvoicingMembership` and `previewGroupedInvoicingMembership`**

Add a new file `internal/service/subscription_modification_grouped.go` (keeps the main file tidy):

```go
package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) previewGroupedInvoicingMembership(
	ctx context.Context,
	modifyType dto.SubscriptionModifyType,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams).(*subscriptionService)

	var parentSub *subscription.Subscription
	if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
		var err error
		parentSub, err = s.serviceParams.SubRepo.Get(ctx, params.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
	}

	changed := make([]dto.ChangedSubscription, 0, len(params.ChildSubscriptionIDs))
	for _, childID := range params.ChildSubscriptionIDs {
		var validateErr error
		if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
			validateErr = subSvc.validateAddToGroupedInvoicingDryRun(ctx, parentSub, childID)
		} else {
			validateErr = subSvc.validateRemoveFromGroupedInvoicingDryRun(ctx, childID)
		}
		action := dto.ChangedSubscriptionActionUpdated
		status := types.SubscriptionStatusActive
		if validateErr != nil {
			return nil, validateErr
		}
		changed = append(changed, dto.ChangedSubscription{
			ID:     childID,
			Action: action,
			Status: status,
		})
	}

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changed,
		},
	}, nil
}

func (s *subscriptionModificationService) executeGroupedInvoicingMembership(
	ctx context.Context,
	modifyType dto.SubscriptionModifyType,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams).(*subscriptionService)

	var parentSub *subscription.Subscription
	if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
		var err error
		parentSub, err = s.serviceParams.SubRepo.Get(ctx, params.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
	}

	changed := make([]dto.ChangedSubscription, 0, len(params.ChildSubscriptionIDs))
	err := s.serviceParams.DB.WithTx(ctx, func(txCtx context.Context) error {
		for _, childID := range params.ChildSubscriptionIDs {
			var opErr error
			if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
				opErr = subSvc.addToGroupedInvoicing(txCtx, parentSub, childID)
			} else {
				opErr = subSvc.removeFromGroupedInvoicing(txCtx, childID)
			}
			if opErr != nil {
				return opErr
			}
			changed = append(changed, dto.ChangedSubscription{
				ID:     childID,
				Action: dto.ChangedSubscriptionActionUpdated,
				Status: types.SubscriptionStatusActive,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changed,
		},
	}, nil
}
```

**Note on status field:** The `ChangedSubscription.Status` field is set to `Active` as a placeholder since the grouped invoicing membership change does not alter the subscription status. The actual status is unaffected by add/remove operations.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/api/dto/... -run TestExecuteSubscriptionModifyRequest_Validate_GroupedInvoicing -v
go test ./internal/service/... -run "TestSubscriptionGroupedInvoicingTestSuite" -v
go vet ./internal/api/dto/... ./internal/service/...
```

Expected: all pass, no vet errors.

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto/subscription_modification.go internal/service/subscription_modification.go internal/service/subscription_modification_grouped.go
git commit -m "feat(modification-service): add grouped_invoicing_add/remove to SubscriptionModificationService"
```

---

## Task 10: Ent schema migration

**Files:**
- Modify: `ent/schema/subscription.go` (comment only — no column change)
- Generate migration SQL

- [ ] **Step 1: Update `subscription_type` field comment in `ent/schema/subscription.go`**

Find the `subscription_type` field definition and update its comment to include `delegated` and `grouped_invoicing`:

```go
field.String("subscription_type").
    Default(string(types.SubscriptionTypeStandalone)).
    Comment("standalone | delegated | parent | inherited | grouped_invoicing").
    Optional().
    Nillable(),
```

- [ ] **Step 2: Run `make generate-ent`**

```bash
make generate-ent
```

Expected: no schema changes (column is varchar, no check constraint update needed unless the schema defines one — verify in generated SQL).

- [ ] **Step 3: Run `make migrate-ent-dry-run` to confirm no destructive changes**

```bash
make migrate-ent-dry-run
```

- [ ] **Step 4: Commit**

```bash
git add ent/schema/subscription.go ent/
git commit -m "chore(ent): update subscription_type field comment to include delegated and grouped_invoicing"
```

---

## Task 11: Final regression and `go vet`

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -timeout 300s
```

- [ ] **Step 2: Run `go vet`**

```bash
go vet ./...
```

- [ ] **Step 3: Run `gofmt`**

```bash
gofmt -w internal/types/subscription.go \
    internal/api/dto/subscription.go \
    internal/api/dto/billing.go \
    internal/api/dto/subscription_modification.go \
    internal/service/subscription_grouped_invoicing.go \
    internal/service/subscription.go \
    internal/service/billing.go \
    internal/service/invoice.go \
    internal/service/subscription_modification.go \
    internal/service/subscription_modification_grouped.go
```

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "chore: gofmt and final cleanup for grouped invoicing feature"
```

---

## Self-Review Checklist

### Spec coverage

| Spec requirement | Task |
|---|---|
| `SubscriptionTypeDelegated` + `SubscriptionTypeGroupedInvoicing` enum values | Task 1 |
| `SubscriptionChangeTypeAddToGroupedInvoicing` + `Remove` | Task 1 |
| `InvoicingBehavior` + `SubIDsForGroupedInvoicing` on `SubscriptionInheritanceConfig` | Task 2 |
| Per-behavior `Validate()` rules | Task 2 |
| `PrepareGroupedInvoiceRequestParams` DTO | Task 3 |
| `addToGroupedInvoicing` / `removeFromGroupedInvoicing` / `getGroupedInvoicingSubscriptions` | Task 4 |
| All 9 add-constraints validated | Task 4 |
| `prepareSubscriptionInheritanceForCreate` extended for all 5 behaviors | Task 5 |
| `SubIDsForGroupedInvoicing` processed post-parent-create | Task 5 |
| `PrepareGroupedInvoiceRequest` (flat merge line items) | Task 6 |
| `ComputeInvoice` skips `grouped_invoicing` | Task 7 |
| `CreateGroupedSubscriptionInvoice` | Task 7 |
| `grouped_invoicing` child skipped in `UpdateBillingPeriods` | Task 8 |
| Parent triggers clubbed invoice + advances children periods | Task 8 |
| Modification service preview + execute for membership (grouped_invoicing_add/remove) | Task 9 |
| Dry-run validation helper | Task 4 |
| Ent schema comment updated, migration generated | Task 10 |
| Backward compat: existing subs unaffected | Tasks 2, 5 (legacy path) |

All spec requirements are covered.

### Potential implementation notes

1. **`computeInvoiceWithRequest` in Task 7**: `ComputeInvoice` currently re-fetches line items from the subscription. `CreateGroupedSubscriptionInvoice` has already computed them via `PrepareGroupedInvoiceRequest`. You may need to either (a) add an override parameter to `ComputeInvoice` that accepts a pre-built request, or (b) write `computeInvoiceWithRequest` as a new private method that applies a `CreateInvoiceRequest` directly to the draft without re-fetching. Inspect `ComputeInvoice` carefully before implementing.

2. **Period variable in Task 8**: The grouped invoice loop references `period.start`/`period.end` — make sure you capture the current period variable from the outer loop correctly (Go loop variable capture).

3. **`buildServiceParams` in Task 4 tests**: Use whatever test helper creates `ServiceParams` in `subscription_test.go` — typically `setupServices()` in the test suite.
