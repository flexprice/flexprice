# Customer Subscription Metadata Flags Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically set system-managed `_fp_*` boolean flags on customer metadata whenever a subscription is created or its type changes, so hierarchy roles are visible directly when listing customers.

**Architecture:** Approach A — incremental flag set. Each subscription service trigger knows which flag to set and calls `CustomerRepo.MergeMetadata` directly (atomic JSONB merge in Postgres). Flags are additive-only (`"true"` once set, never cleared). API layer rejects user attempts to write `_fp_*` keys.

**Tech Stack:** Go 1.23, Ent ORM, PostgreSQL JSONB `||` merge operator, testify suite, in-memory test stores.

---

### Task 1: Define metadata key constants and type→flag helper

**Files:**
- Create: `internal/types/customer_metadata.go`

- [ ] **Step 1: Write the file**

```go
package types

import "strings"

const (
	// System-managed readonly customer metadata keys (prefix _fp_).
	// Set automatically when subscriptions are created or change type.
	// Never set or modified by user-facing APIs.
	MetaKeyHasStandaloneSub         = "_fp_has_standalone_sub"
	MetaKeyHasParentSub             = "_fp_has_parent_sub"
	MetaKeyHasInheritedSub          = "_fp_has_inherited_sub"
	MetaKeyHasGroupedInvoicingSub   = "_fp_has_grouped_invoicing_sub"
	MetaKeyHasDelegatedInvoicingSub = "_fp_has_delegated_invoicing_sub"

	// SystemMetaKeyPrefix is the prefix reserved for all system-managed metadata keys.
	SystemMetaKeyPrefix = "_fp_"
)

// IsSystemMetaKey reports whether key is system-managed and must not be set by users.
func IsSystemMetaKey(key string) bool {
	return strings.HasPrefix(key, SystemMetaKeyPrefix)
}

// SubscriptionTypeToMetaFlag maps a subscription type to its corresponding
// customer metadata flag key. Returns "" for unknown types.
func SubscriptionTypeToMetaFlag(t SubscriptionType) string {
	switch t {
	case SubscriptionTypeStandalone:
		return MetaKeyHasStandaloneSub
	case SubscriptionTypeParent:
		return MetaKeyHasParentSub
	case SubscriptionTypeInherited:
		return MetaKeyHasInheritedSub
	case SubscriptionTypeGroupedInvoicing:
		return MetaKeyHasGroupedInvoicingSub
	case SubscriptionTypeDelegatedInvoicing:
		return MetaKeyHasDelegatedInvoicingSub
	default:
		return ""
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/types/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/types/customer_metadata.go
git commit -m "feat: add customer subscription metadata key constants and type-to-flag helper"
```

---

### Task 2: Add `MergeMetadata` to repository interface and in-memory store

**Files:**
- Modify: `internal/domain/customer/repository.go`
- Modify: `internal/testutil/inmemory_customer_store.go`

- [ ] **Step 1: Add method to the interface**

In `internal/domain/customer/repository.go`, replace the closing `}` of the `Repository` interface with:

```go
// Repository defines the interface for customer data access
type Repository interface {
	Create(ctx context.Context, customer *Customer) error
	Get(ctx context.Context, id string) (*Customer, error)
	List(ctx context.Context, filter *types.CustomerFilter) ([]*Customer, error)
	Count(ctx context.Context, filter *types.CustomerFilter) (int, error)
	ListAll(ctx context.Context, filter *types.CustomerFilter) ([]*Customer, error)
	Update(ctx context.Context, customer *Customer) error
	Delete(ctx context.Context, customer *Customer) error
	GetByLookupKey(ctx context.Context, lookupKey string) (*Customer, error)
	// MergeMetadata merges the given key-value pairs into the customer's existing
	// metadata without overwriting unrelated keys. Safe to call concurrently.
	MergeMetadata(ctx context.Context, customerID string, meta map[string]string) error
}
```

- [ ] **Step 2: Implement in the in-memory store**

Append to the bottom of `internal/testutil/inmemory_customer_store.go`:

```go
// MergeMetadata merges meta into the stored customer's metadata map.
func (s *InMemoryCustomerStore) MergeMetadata(ctx context.Context, customerID string, meta map[string]string) error {
	c, err := s.InMemoryStore.Get(ctx, customerID)
	if err != nil {
		return err
	}
	if c.Metadata == nil {
		c.Metadata = make(map[string]string)
	}
	for k, v := range meta {
		c.Metadata[k] = v
	}
	return s.InMemoryStore.Update(ctx, c.ID, copyCustomer(c))
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/domain/customer/... ./internal/testutil/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/customer/repository.go internal/testutil/inmemory_customer_store.go
git commit -m "feat: add MergeMetadata to customer repository interface and in-memory store"
```

---

### Task 3: Implement `MergeMetadata` in the Ent repository

**Files:**
- Modify: `internal/repository/ent/customer.go`

- [ ] **Step 1: Add the method**

Append the following method to `internal/repository/ent/customer.go`, just before the `CustomerQueryOptions` block (before `func (o CustomerQueryOptions)`):

```go
// MergeMetadata atomically merges the provided key-value pairs into the customer's
// JSONB metadata column using the PostgreSQL || operator. Existing keys not present
// in meta are left untouched. Skips archived customers.
func (r *customerRepository) MergeMetadata(ctx context.Context, customerID string, meta map[string]string) error {
	span := StartRepositorySpan(ctx, "customer", "merge_metadata", map[string]interface{}{
		"customer_id": customerID,
	})
	defer FinishSpan(span)

	jsonBytes, err := json.Marshal(meta)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to marshal metadata for merge").
			Mark(ierr.ErrSystem)
	}

	client := r.client.Writer(ctx)
	_, err = client.ExecContext(ctx,
		`UPDATE customers
		 SET metadata   = COALESCE(metadata, '{}'::jsonb) || $1::jsonb,
		     updated_at = NOW(),
		     updated_by = $2
		 WHERE id             = $3
		   AND tenant_id      = $4
		   AND environment_id = $5
		   AND status        != 'archived'`,
		string(jsonBytes),
		types.GetUserID(ctx),
		customerID,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
	)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to merge customer metadata").
			WithReportableDetails(map[string]any{"customer_id": customerID}).
			Mark(ierr.ErrDatabase)
	}

	// Invalidate ID-based cache entry so next Get reflects the merged metadata.
	idKey := cache.GenerateKey(cache.PrefixCustomer, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), customerID)
	r.cache.Delete(ctx, idKey)

	SetSpanSuccess(span)
	r.log.Debugw("merged customer metadata", "customer_id", customerID, "keys", meta)
	return nil
}
```

- [ ] **Step 2: Add `"encoding/json"` to imports if not already present**

Check the import block at the top of `internal/repository/ent/customer.go`. If `"encoding/json"` is missing, add it. The existing import block starts at line 1 — add it alongside the other standard library imports:

```go
import (
    "context"
    "encoding/json"
    "errors"
    "time"
    // ... rest of existing imports
)
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/repository/ent/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/repository/ent/customer.go
git commit -m "feat: implement MergeMetadata in Ent customer repository using JSONB merge"
```

---

### Task 4: Enforce readonly `_fp_*` prefix in `UpdateCustomerRequest`

**Files:**
- Modify: `internal/api/dto/customer.go`

- [ ] **Step 1: Update `Validate()` on `UpdateCustomerRequest`**

The current `Validate()` is at line 165 of `internal/api/dto/customer.go`:

```go
func (r *UpdateCustomerRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return nil
}
```

Replace it with:

```go
func (r *UpdateCustomerRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	for key := range r.Metadata {
		if types.IsSystemMetaKey(key) {
			return ierr.NewError("metadata key is reserved").
				WithHintf("Key %q is managed by the system and cannot be set via the API", key).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}
```

- [ ] **Step 2: Verify the import for `types` is present**

`internal/api/dto/customer.go` already imports `"github.com/flexprice/flexprice/internal/types"` (it uses `types.GetEnvironmentID`). Confirm it's there; add it if missing.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/api/dto/...
```

Expected: no errors.

- [ ] **Step 4: Write a quick unit test**

Add the following test to `internal/api/dto/customer_test.go` (create the file if it doesn't exist):

```go
package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/stretchr/testify/assert"
)

func TestUpdateCustomerRequest_Validate_RejectsSystemMetaKeys(t *testing.T) {
	req := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"_fp_has_parent_sub": "true",
		},
	}
	err := req.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestUpdateCustomerRequest_Validate_AllowsUserMetaKeys(t *testing.T) {
	req := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"hubspot_deal_id": "hs_123",
			"account_tier":    "enterprise",
		},
	}
	err := req.Validate()
	assert.NoError(t, err)
}
```

- [ ] **Step 5: Run the test**

```bash
go test ./internal/api/dto/... -run TestUpdateCustomerRequest_Validate -v
```

Expected: both tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto/customer.go internal/api/dto/customer_test.go
git commit -m "feat: reject _fp_ prefixed keys in UpdateCustomer API validation"
```

---

### Task 5: Wire flag-setting into the subscription service

**Files:**
- Modify: `internal/service/subscription.go` (two locations)
- Modify: `internal/service/subscription_grouped_invoicing.go`

#### 5a — `CreateSubscription`: set flag for the main subscription's customer after the TX

- [ ] **Step 1: Add the metadata call after the transaction block**

In `internal/service/subscription.go`, the transaction block ends around line 459 with `})` and the error check is at line 460. After `if err != nil { return nil, err }` (around line 462), add the following block before the phases handling (before `// Handle phases`):

```go
	// Set system metadata flag on the customer to reflect this subscription's type.
	// Best-effort: log on failure but do not block the subscription creation response.
	if flagKey := types.SubscriptionTypeToMetaFlag(sub.SubscriptionType); flagKey != "" {
		if mergeErr := s.CustomerRepo.MergeMetadata(ctx, sub.CustomerID, map[string]string{flagKey: "true"}); mergeErr != nil {
			s.Logger.WarnwCtx(ctx, "failed to set subscription metadata flag on customer",
				"customer_id", sub.CustomerID,
				"flag", flagKey,
				"error", mergeErr,
			)
		}
	}
```

#### 5b — `createInheritedSubscriptions`: set `_fp_has_inherited_sub` on child customer

- [ ] **Step 2: Add the metadata call at the end of `createInheritedSubscriptions`**

In `internal/service/subscription.go`, `createInheritedSubscriptions` is at line 7457. Currently it returns `nil` after `s.SubRepo.Create()` succeeds. Replace the final `return nil` (after the SubRepo.Create error check block, around line 7497) with:

```go
	// Set inherited-child flag on the child customer. Best-effort.
	if mergeErr := s.CustomerRepo.MergeMetadata(ctx, childCustomerID, map[string]string{types.MetaKeyHasInheritedSub: "true"}); mergeErr != nil {
		s.Logger.WarnwCtx(ctx, "failed to set inherited sub metadata flag on child customer",
			"child_customer_id", childCustomerID,
			"parent_subscription_id", parent.ID,
			"error", mergeErr,
		)
	}
	return nil
```

#### 5c — `addToGroupedInvoicing`: set flags on child and parent customers

- [ ] **Step 3: Add metadata calls in `addToGroupedInvoicing`**

In `internal/service/subscription_grouped_invoicing.go`, `addToGroupedInvoicing` currently ends with `return s.SubRepo.Update(ctx, child)` (line 165). Replace that final line with:

```go
	if err := s.SubRepo.Update(ctx, child); err != nil {
		return err
	}

	// Set grouped-invoicing flag on child customer. Best-effort.
	if mergeErr := s.CustomerRepo.MergeMetadata(ctx, child.CustomerID, map[string]string{types.MetaKeyHasGroupedInvoicingSub: "true"}); mergeErr != nil {
		s.Logger.WarnwCtx(ctx, "failed to set grouped_invoicing metadata flag on child customer",
			"child_customer_id", child.CustomerID,
			"error", mergeErr,
		)
	}

	// Set parent flag on parent customer. Best-effort (idempotent — likely already set).
	if mergeErr := s.CustomerRepo.MergeMetadata(ctx, parentSub.CustomerID, map[string]string{types.MetaKeyHasParentSub: "true"}); mergeErr != nil {
		s.Logger.WarnwCtx(ctx, "failed to set parent sub metadata flag on parent customer",
			"parent_customer_id", parentSub.CustomerID,
			"error", mergeErr,
		)
	}

	return nil
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/service/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_grouped_invoicing.go
git commit -m "feat: set customer subscription metadata flags on subscription creation and type change"
```

---

### Task 6: Service-level tests for metadata flag behaviour

**Files:**
- Modify: `internal/service/subscription_test.go`

- [ ] **Step 1: Write tests for standalone subscription metadata flag**

Find the existing `SubscriptionServiceSuite` in `internal/service/subscription_test.go`. Add the following test methods to it:

```go
func (s *SubscriptionServiceSuite) TestCreateSubscription_SetsStandaloneMetadataFlag() {
	ctx := s.GetContext()

	resp, err := s.service.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(time.Now()),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
	})
	s.NoError(err)
	s.NotNil(resp)

	cust, err := s.GetStores().CustomerRepo.Get(ctx, s.testData.customer.ID)
	s.NoError(err)
	s.Equal("true", cust.Metadata[types.MetaKeyHasStandaloneSub], "standalone flag must be set")
}
```

- [ ] **Step 2: Write test for parent + inherited flags when creating a parent subscription with child customers**

```go
func (s *SubscriptionServiceSuite) TestCreateSubscription_SetsParentAndInheritedFlags() {
	ctx := s.GetContext()

	// Create a child customer (child lookup uses external ID)
	childExtID := "ext_meta_inherited_child"
	child := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: childExtID,
		Name:       "Child Customer",
		Email:      "child-meta@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, child))

	resp, err := s.service.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(time.Now()),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
		Inheritance: &dto.SubscriptionInheritanceConfig{
			ExternalCustomerIDsToInheritSubscription: []string{childExtID},
		},
	})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(types.SubscriptionTypeParent, resp.SubscriptionType)

	// Parent customer must have _fp_has_parent_sub
	parentCust, err := s.GetStores().CustomerRepo.Get(ctx, s.testData.customer.ID)
	s.NoError(err)
	s.Equal("true", parentCust.Metadata[types.MetaKeyHasParentSub], "parent flag must be set on parent customer")

	// Child customer must have _fp_has_inherited_sub
	childUpdated, err := s.GetStores().CustomerRepo.Get(ctx, child.ID)
	s.NoError(err)
	s.Equal("true", childUpdated.Metadata[types.MetaKeyHasInheritedSub], "inherited flag must be set on child customer")
}
```

- [ ] **Step 3: Write test for grouped invoicing flag**

```go
func (s *SubscriptionServiceSuite) TestCreateSubscription_SetsGroupedInvoicingFlag() {
	ctx := s.GetContext()

	// Create a child customer and a standalone subscription for them
	child := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_meta_gi_child",
		Name:       "GI Child Customer",
		Email:      "gi-child-meta@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, child))

	childSubResp, err := s.service.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         child.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(time.Now()),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
	})
	s.NoError(err)

	// Create a parent subscription that pulls the child sub into grouped invoicing
	parentResp, err := s.service.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(time.Now()),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
		Inheritance: &dto.SubscriptionInheritanceConfig{
			SubscriptionsIDsForGroupedInvoicing: []string{childSubResp.ID},
		},
	})
	s.NoError(err)
	s.NotNil(parentResp)
	s.Equal(types.SubscriptionTypeParent, parentResp.SubscriptionType)

	// Child customer must have _fp_has_grouped_invoicing_sub
	childUpdated, err := s.GetStores().CustomerRepo.Get(ctx, child.ID)
	s.NoError(err)
	s.Equal("true", childUpdated.Metadata[types.MetaKeyHasGroupedInvoicingSub], "grouped_invoicing flag must be set on child customer")

	// Parent customer must have _fp_has_parent_sub
	parentUpdated, err := s.GetStores().CustomerRepo.Get(ctx, s.testData.customer.ID)
	s.NoError(err)
	s.Equal("true", parentUpdated.Metadata[types.MetaKeyHasParentSub], "parent flag must be set on parent customer")
}
```

- [ ] **Step 4: Run only the new tests**

```bash
go test -v -race ./internal/service/... -run "TestSubscriptionService/TestCreateSubscription_SetsStandaloneMetadataFlag|TestSubscriptionService/TestCreateSubscription_SetsParentAndInheritedFlags|TestSubscriptionService/TestCreateSubscription_SetsGroupedInvoicingFlag"
```

Expected: all 3 PASS.

- [ ] **Step 5: Run the full subscription test suite to check for regressions**

```bash
go test -v -race ./internal/service/... -run "SubscriptionService" -timeout 300s
```

Expected: all existing tests continue to PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/subscription_test.go
git commit -m "test: verify customer subscription metadata flags are set correctly"
```

---

### Task 7: Smoke-test the readonly enforcement end-to-end

This task verifies the API validation runs correctly via a unit test rather than spinning up the full server.

**Files:**
- No new files — validation is already tested in Task 4.

- [ ] **Step 1: Run all dto tests**

```bash
go test -v ./internal/api/dto/... -timeout 60s
```

Expected: all PASS.

- [ ] **Step 2: Run all service tests**

```bash
go test -race ./internal/service/... -timeout 300s
```

Expected: all PASS.

- [ ] **Step 3: Vet the whole codebase**

```bash
go vet ./...
```

Expected: no errors.

- [ ] **Step 4: Final commit if any loose files**

```bash
git status
```

If clean, nothing to do. Otherwise stage and commit any remaining changes.
