# Inherited Subscription Analytics Cost Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix analytics cost calculation returning $0 for child customers by fetching parent subscription line items alongside inherited subscriptions in `fetchAnalyticsData`.

**Architecture:** Add a single private helper `fetchParentSubscriptions` to `featureUsageTrackingService`. It scans a subscription slice, collects unique parent IDs from any `SubscriptionTypeInherited` subscriptions, fetches each parent via `SubRepo.GetWithLineItems`, and returns the parent slice. Called once in `fetchAnalyticsData` immediately after `fetchSubscriptions`; the result is appended to the slice so all existing downstream code (map building, `fetchSubscriptionPrices`, synthetic zero-usage injection) sees the parent line items without modification.

**Tech Stack:** Go 1.23, `github.com/stretchr/testify`, `github.com/flexprice/flexprice/internal/testutil` in-memory stores.

---

## File Structure

| File | Change |
|------|--------|
| `internal/service/feature_usage_tracking.go` | Add `fetchParentSubscriptions` method; call it in `fetchAnalyticsData` after `fetchSubscriptions` (lines ~1999–2003) |
| `internal/service/feature_usage_tracking_analytics_test.go` | Add `TestFetchParentSubscriptions` table-driven tests |

---

## Background: Key Codebase Facts

Before coding, understand these:

**`fetchAnalyticsData`** lives at line ~1991 of `feature_usage_tracking.go`. Its first three steps are:
```go
// 1. Fetch customer
customer, err := s.fetchCustomer(ctx, req.ExternalCustomerID)

// 2. Fetch subscriptions
subscriptions, err := s.fetchSubscriptions(ctx, customer.ID)

// 3. Validate currency consistency
currency, err := s.validateCurrency(subscriptions)
```
The fix inserts a new step between 2 and 3.

**`SubRepo.GetWithLineItems`** is defined on the `subscription.Repository` interface (line 19 of `internal/domain/subscription/repository.go`):
```go
GetWithLineItems(ctx context.Context, id string) (*Subscription, []*SubscriptionLineItem, error)
```
The second return value can be discarded — the line items are already set on `sub.LineItems`.

**Do NOT use `SubRepo.Get`** — it does not eager-load line items.

**`ServiceParams.SubRepo`** is the subscription repository field (line 78 of `internal/service/factory.go`).

**`types.SubscriptionTypeInherited`** is the constant `"inherited"` (in `internal/types/subscription.go`).

**`Subscription.ParentSubscriptionID`** is `*string` — non-nil for inherited subscriptions.

**In-memory store for tests:** `testutil.NewInMemorySubscriptionStore()` without calling `SetLineItemStore` uses the `s.lineItems[id]` path in `GetWithLineItems`. `CreateWithLineItems(ctx, sub, items)` stores line items at `s.lineItems[sub.ID]`. This means: create the parent sub with `CreateWithLineItems`, then `GetWithLineItems` will return those items on `sub.LineItems` — no filter complexity.

**Error wrapping pattern** used in this file:
```go
return nil, ierr.WithError(err).
    WithHint("...").
    Mark(ierr.ErrDatabase)
```

---

## Task 1: Tests for fetchParentSubscriptions

**Files:**
- Modify: `internal/service/feature_usage_tracking_analytics_test.go`

- [ ] **Step 1: Add the failing test block at the end of the file**

Add this entire block after the last closing brace in `feature_usage_tracking_analytics_test.go`:

```go
// --- fetchParentSubscriptions ---

func TestFetchParentSubscriptions(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)

	newSub := func(id string, subType types.SubscriptionType, parentSubID *string) *subscription.Subscription {
		return &subscription.Subscription{
			ID:                   id,
			SubscriptionType:     subType,
			ParentSubscriptionID: parentSubID,
			BaseModel:            types.GetDefaultBaseModel(ctx),
		}
	}

	newLineItem := func(id, subID string) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:             id,
			SubscriptionID: subID,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
	}

	ptr := func(s string) *string { return &s }

	t.Run("no subscriptions — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		parents, err := svc.fetchParentSubscriptions(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("standalone subscription — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_standalone", types.SubscriptionTypeStandalone, nil),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("parent-type subscription — returns empty slice", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_parent", types.SubscriptionTypeParent, nil),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		assert.Empty(t, parents)
	})

	t.Run("one inherited subscription — fetches parent with line items", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		// Create the parent subscription with a line item
		parentSub := newSub("sub_parent_1", types.SubscriptionTypeParent, nil)
		lineItem := newLineItem("li_1", "sub_parent_1")
		require.NoError(t, subStore.CreateWithLineItems(ctx, parentSub, []*subscription.SubscriptionLineItem{lineItem}))

		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_inherited_1", types.SubscriptionTypeInherited, ptr("sub_parent_1")),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		require.Len(t, parents, 1)
		assert.Equal(t, "sub_parent_1", parents[0].ID)
		require.Len(t, parents[0].LineItems, 1)
		assert.Equal(t, "li_1", parents[0].LineItems[0].ID)
	})

	t.Run("two inherited subs pointing to same parent — parent fetched once", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		parentSub := newSub("sub_parent_shared", types.SubscriptionTypeParent, nil)
		lineItem := newLineItem("li_shared", "sub_parent_shared")
		require.NoError(t, subStore.CreateWithLineItems(ctx, parentSub, []*subscription.SubscriptionLineItem{lineItem}))

		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_inh_a", types.SubscriptionTypeInherited, ptr("sub_parent_shared")),
			newSub("sub_inh_b", types.SubscriptionTypeInherited, ptr("sub_parent_shared")),
		}
		parents, err := svc.fetchParentSubscriptions(ctx, subs)
		require.NoError(t, err)
		// Deduplicated: only one parent returned even though two inherited subs reference it
		require.Len(t, parents, 1)
		assert.Equal(t, "sub_parent_shared", parents[0].ID)
	})

	t.Run("parent subscription not found — returns error", func(t *testing.T) {
		subStore := testutil.NewInMemorySubscriptionStore()
		// Do NOT create the parent sub in the store
		svc := &featureUsageTrackingService{
			ServiceParams: ServiceParams{SubRepo: subStore},
		}
		subs := []*subscription.Subscription{
			newSub("sub_inh_missing", types.SubscriptionTypeInherited, ptr("sub_parent_missing")),
		}
		_, err := svc.fetchParentSubscriptions(ctx, subs)
		require.Error(t, err)
	})
}
```

You also need to add these imports to the test file (the file already imports `testing`, `dto`, `ierr`, `assert`, `require` — add the missing ones):

```go
import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

Replace the existing import block at the top of `feature_usage_tracking_analytics_test.go` with the above.

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice
go test ./internal/service/ -run TestFetchParentSubscriptions -v 2>&1 | head -30
```

Expected: compile error or `undefined: fetchParentSubscriptions` — the method doesn't exist yet.

---

## Task 2: Implement fetchParentSubscriptions

**Files:**
- Modify: `internal/service/feature_usage_tracking.go`

- [ ] **Step 1: Add the helper method**

Add `fetchParentSubscriptions` as a new method on `featureUsageTrackingService`. Place it immediately after the `fetchChildCustomers` method (which ends around line 1988).

```go
// fetchParentSubscriptions fetches the parent subscriptions (with line items) for any
// inherited subscriptions in the provided list, deduplicated by parent subscription ID.
// Returns an empty slice (no error) when no inherited subscriptions are present.
// The caller should append the result to their subscription slice before building
// SubscriptionLineItems and SubscriptionsMap so that parent pricing is available
// for cost calculation and price discovery.
func (s *featureUsageTrackingService) fetchParentSubscriptions(
	ctx context.Context,
	subscriptions []*subscription.Subscription,
) ([]*subscription.Subscription, error) {
	seen := make(map[string]bool)
	var parents []*subscription.Subscription
	for _, sub := range subscriptions {
		if sub.SubscriptionType != types.SubscriptionTypeInherited {
			continue
		}
		if sub.ParentSubscriptionID == nil {
			continue
		}
		parentID := *sub.ParentSubscriptionID
		if seen[parentID] {
			continue
		}
		seen[parentID] = true
		parentSub, _, err := s.SubRepo.GetWithLineItems(ctx, parentID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch parent subscription for cost calculation").
				Mark(ierr.ErrDatabase)
		}
		parents = append(parents, parentSub)
	}
	return parents, nil
}
```

- [ ] **Step 2: Wire it into fetchAnalyticsData**

Find this block in `fetchAnalyticsData` (around line 1999):

```go
	// 2. Fetch subscriptions
	subscriptions, err := s.fetchSubscriptions(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	// 3. Validate currency consistency
	currency, err := s.validateCurrency(subscriptions)
```

Replace with:

```go
	// 2. Fetch subscriptions
	subscriptions, err := s.fetchSubscriptions(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	// 2a. For inherited subscriptions, also include the parent subscription so its
	//     line items are available for cost calculation (SubscriptionLineItems map)
	//     and price discovery (fetchSubscriptionPrices iterates data.Subscriptions).
	parentSubs, err := s.fetchParentSubscriptions(ctx, subscriptions)
	if err != nil {
		return nil, err
	}
	subscriptions = append(subscriptions, parentSubs...)

	// 3. Validate currency consistency
	currency, err := s.validateCurrency(subscriptions)
```

- [ ] **Step 3: Run the tests to confirm they pass**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice
go test ./internal/service/ -run TestFetchParentSubscriptions -v
```

Expected: all 6 subtests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/service/feature_usage_tracking.go internal/service/feature_usage_tracking_analytics_test.go
git commit -m "fix: include parent subscription line items in analytics for inherited subscriptions"
```

---

## Task 3: Final Verification

**Files:** No changes.

- [ ] **Step 1: Run full service test suite**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice
go test ./internal/service/... -timeout 120s 2>&1 | tail -20
```

Expected: all tests pass (or only the pre-existing flaky test `TestCreditSequentialVsProportionalApplication` may fail intermittently — that is a known pre-existing issue unrelated to this change).

- [ ] **Step 2: Vet**

```bash
go vet ./internal/service/...
```

Expected: no output (no errors).

- [ ] **Step 3: Confirm the analytics test file has no unused imports**

```bash
go build ./internal/service/...
```

Expected: no output (no errors).
