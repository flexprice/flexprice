# Cancel Inherited Child Subscription — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow operators to remove individual child customers from a parent subscription hierarchy by scheduling the child's `inherited` subscription for cancellation at the parent's current period end.

**Architecture:** Extend the existing `inheritance` modify type with an `action` field (`add`/`remove`), mirroring the `grouped_invoicing` pattern. The remove path schedules the inherited subscription's cancellation at `parent.CurrentPeriodEnd` via `cancel_at`/`cancel_at_period_end`. At period end the existing cron picks up the inherited sub and cancels it (new guard), skipping invoice generation since the parent invoice covers all usage.

**Tech Stack:** Go 1.23, Gin, Ent ORM, PostgreSQL, `testify/suite` for tests, `github.com/samber/lo`, `github.com/shopspring/decimal`.

---

## File Map

| File | Change |
|------|--------|
| `internal/api/dto/subscription_modification.go` | Add `InheritanceAction` type; extend `SubModifyInheritanceRequest` + `Validate()` |
| `internal/service/subscription_modification.go` | Branch execute/preview on action; add `executeRemoveInheritance`, `previewRemoveInheritance`, `resolveCustomersByExternalIDs`, `findInheritedSubForChild` |
| `internal/service/subscription.go` | Add `cancelInheritedSubscriptionAtPeriodEnd`; extend the `SubscriptionTypeInherited` block in `processSubscriptionPeriod` |
| `internal/service/subscription_modification_test.go` | Add tests for remove execute + preview |
| `internal/service/subscription_test.go` | Add test for period-end cron guard on inherited sub |

No DB migrations, no new routes, no ClickHouse changes.

---

## Task 1: Extend the DTO

**Files:**
- Modify: `internal/api/dto/subscription_modification.go`

The current `SubModifyInheritanceRequest` has only one field and always means "add". We add an `action` field that defaults to `"add"` (backward-compatible) and new fields for `"remove"`.

- [ ] **Step 1: Replace `SubModifyInheritanceRequest` and add `InheritanceAction` type**

Open `internal/api/dto/subscription_modification.go`. Replace the existing `SubModifyInheritanceRequest` block (lines 11–24) with:

```go
// InheritanceAction identifies whether children are being added to or removed from inheritance.
type InheritanceAction string

const (
	// InheritanceActionAdd adds inherited child subscriptions to a parent.
	InheritanceActionAdd InheritanceAction = "add"
	// InheritanceActionRemove schedules inherited child subscriptions for cancellation at period end.
	InheritanceActionRemove InheritanceAction = "remove"
)

// SubModifyInheritanceRequest is the payload for adding or removing
// inherited child subscriptions from a parent subscription.
type SubModifyInheritanceRequest struct {
	// Action is "add" or "remove". Defaults to "add" when omitted — fully backward-compatible.
	Action InheritanceAction `json:"action,omitempty"`

	// ExternalCustomerIDsToInheritSubscription is used for action="add".
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`

	// ExternalCustomerIDsToRemove is used for action="remove".
	ExternalCustomerIDsToRemove []string `json:"external_customer_ids_to_remove,omitempty"`
}

func (r *SubModifyInheritanceRequest) Validate() error {
	switch r.Action {
	case InheritanceActionRemove:
		if len(r.ExternalCustomerIDsToRemove) == 0 {
			return ierr.NewError("at least one external customer ID is required for remove").
				WithHint("Provide external_customer_ids_to_remove with at least one non-empty value").
				Mark(ierr.ErrValidation)
		}
	default: // "" or "add"
		if len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
			return ierr.NewError("at least one external customer ID is required").
				WithHint("Provide external_customer_ids_to_inherit_subscription with at least one non-empty value").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}
```

- [ ] **Step 2: Verify the build compiles**

```bash
cd /path/to/repo && go build ./internal/api/dto/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/api/dto/subscription_modification.go
git commit -m "feat(dto): extend SubModifyInheritanceRequest with action=remove"
```

---

## Task 2: executeRemoveInheritance + preview

**Files:**
- Modify: `internal/service/subscription_modification.go`
- Modify: `internal/service/subscription_modification_test.go`

### Step 2a — Write the failing tests

- [ ] **Step 1: Add test helpers to `subscription_modification_test.go`**

Add the following helper after the existing `createActiveSub` helper (around line 141):

```go
// createParentSubWithChild creates a parent customer, a child customer, promotes the
// parent subscription to type=parent, and creates a live inherited subscription for the child.
// Returns (parentCustomer, childCustomer, parentSub, inheritedSub).
func (s *SubscriptionModificationServiceSuite) createParentSubWithChild(parentExtID, childExtID string) (*customer.Customer, *customer.Customer, *subscription.Subscription, *subscription.Subscription) {
	ctx := s.GetContext()

	parent := s.createCustomer(parentExtID)
	child := s.createCustomer(childExtID)
	parentSub := s.createActiveSub(parent.ID)

	_, err := s.service.Execute(ctx, parentSub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		},
	})
	s.Require().NoError(err)

	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = child.ID
	subs, err := s.GetStores().SubscriptionRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	inheritedSub := subs[0]

	updatedParent, err := s.GetStores().SubscriptionRepo.Get(ctx, parentSub.ID)
	s.Require().NoError(err)

	return parent, child, updatedParent, inheritedSub
}
```

- [ ] **Step 2: Add tests for executeRemoveInheritance**

Add after the last existing inheritance test (around line 660):

```go
// TestExecuteRemoveInheritance_Success verifies that a child's inherited subscription
// gets cancel_at set to the parent's CurrentPeriodEnd and cancel_at_period_end=true,
// while the parent stays as type=parent.
func (s *SubscriptionModificationServiceSuite) TestExecuteRemoveInheritance_Success() {
	ctx := s.GetContext()
	_, child, parentSub, inheritedSub := s.createParentSubWithChild("ext-rp-001", "ext-rc-001")

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			Action:                      dto.InheritanceActionRemove,
			ExternalCustomerIDsToRemove: []string{child.ExternalID},
		},
	}

	resp, err := s.service.Execute(ctx, parentSub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// One changed subscription: the inherited child marked for removal
	s.Require().Len(resp.ChangedResources.Subscriptions, 1)
	s.Equal(dto.ChangedSubscriptionActionUpdated, resp.ChangedResources.Subscriptions[0].Action)
	s.Equal(inheritedSub.ID, resp.ChangedResources.Subscriptions[0].ID)

	// Inherited sub should have cancel_at = parent's period end, status still active
	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, inheritedSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, updated.SubscriptionStatus)
	s.True(updated.CancelAtPeriodEnd)
	s.Require().NotNil(updated.CancelAt)
	s.Equal(parentSub.CurrentPeriodEnd.UTC(), updated.CancelAt.UTC())

	// Parent stays type=parent
	refreshedParent, err := s.GetStores().SubscriptionRepo.Get(ctx, parentSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeParent, refreshedParent.SubscriptionType)
}

// TestExecuteRemoveInheritance_NotParent verifies that calling remove on a non-parent
// subscription returns an error.
func (s *SubscriptionModificationServiceSuite) TestExecuteRemoveInheritance_NotParent() {
	ctx := s.GetContext()
	parent := s.createCustomer("ext-rp-002")
	standaloneSubOwner := s.createActiveSub(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			Action:                      dto.InheritanceActionRemove,
			ExternalCustomerIDsToRemove: []string{"some-ext-id"},
		},
	}

	_, err := s.service.Execute(ctx, standaloneSubOwner.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "not a parent subscription")
}

// TestExecuteRemoveInheritance_ChildNotFound verifies that removing a child
// that has no inherited sub under this parent returns an error.
func (s *SubscriptionModificationServiceSuite) TestExecuteRemoveInheritance_ChildNotFound() {
	ctx := s.GetContext()
	_, _, parentSub, _ := s.createParentSubWithChild("ext-rp-003", "ext-rc-003")
	unrelated := s.createCustomer("ext-unrelated-003")

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			Action:                      dto.InheritanceActionRemove,
			ExternalCustomerIDsToRemove: []string{unrelated.ExternalID},
		},
	}

	_, err := s.service.Execute(ctx, parentSub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "inherited subscription not found")
}

// TestExecuteRemoveInheritance_AlreadyScheduled verifies that calling remove twice
// on the same child returns an error on the second call.
func (s *SubscriptionModificationServiceSuite) TestExecuteRemoveInheritance_AlreadyScheduled() {
	ctx := s.GetContext()
	_, child, parentSub, _ := s.createParentSubWithChild("ext-rp-004", "ext-rc-004")

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			Action:                      dto.InheritanceActionRemove,
			ExternalCustomerIDsToRemove: []string{child.ExternalID},
		},
	}

	// First call should succeed
	_, err := s.service.Execute(ctx, parentSub.ID, req)
	s.Require().NoError(err)

	// Second call should fail
	_, err = s.service.Execute(ctx, parentSub.ID, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "already scheduled for removal")
}

// TestPreviewRemoveInheritance_Success verifies that preview returns the expected
// changed subscriptions without writing to the database.
func (s *SubscriptionModificationServiceSuite) TestPreviewRemoveInheritance_Success() {
	ctx := s.GetContext()
	_, child, parentSub, inheritedSub := s.createParentSubWithChild("ext-rp-005", "ext-rc-005")

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			Action:                      dto.InheritanceActionRemove,
			ExternalCustomerIDsToRemove: []string{child.ExternalID},
		},
	}

	resp, err := s.service.Preview(ctx, parentSub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Should show one changed subscription with effective date = parent period end
	s.Require().Len(resp.ChangedResources.Subscriptions, 1)
	cs := resp.ChangedResources.Subscriptions[0]
	s.Equal(inheritedSub.ID, cs.ID)
	s.Equal(dto.ChangedSubscriptionActionUpdated, cs.Action)
	s.Require().NotNil(cs.CurrentPeriodEnd)
	s.Equal(parentSub.CurrentPeriodEnd.UTC(), cs.CurrentPeriodEnd.UTC())

	// Preview must NOT have written to the DB
	notChanged, err := s.GetStores().SubscriptionRepo.Get(ctx, inheritedSub.ID)
	s.Require().NoError(err)
	s.Nil(notChanged.CancelAt, "preview must not persist cancel_at")
	s.False(notChanged.CancelAtPeriodEnd, "preview must not persist cancel_at_period_end")
}
```

- [ ] **Step 3: Run the new tests to confirm they fail**

```bash
go test -v -race ./internal/service/... -run "TestSubscriptionModificationServiceSuite/TestExecuteRemoveInheritance|TestSubscriptionModificationServiceSuite/TestPreviewRemoveInheritance" 2>&1 | tail -30
```

Expected: FAIL — `dto.InheritanceActionRemove` undefined and service functions not found.

### Step 2b — Implement

- [ ] **Step 4: Add the remove branch to `executeInheritance` in `subscription_modification.go`**

In `executeInheritance` (starts around line 91), add this block as the very first thing inside the function body, before the existing `sp := s.serviceParams` line:

```go
	if params.Action == dto.InheritanceActionRemove {
		return s.executeRemoveInheritance(ctx, subscriptionID, params)
	}
```

In `previewInheritance` (starts around line 203), add the same branch before `sp := s.serviceParams`:

```go
	if params.Action == dto.InheritanceActionRemove {
		return s.previewRemoveInheritance(ctx, subscriptionID, params)
	}
```

- [ ] **Step 5: Add helper functions to `subscription_modification.go`**

Append these three functions at the end of the file (before the final closing of the package, after the `getInheritedSubscriptions` function):

```go
// resolveCustomersByExternalIDs converts external customer IDs to internal IDs.
// Unlike resolveExternalCustomersForInheritance, this does not require StatusPublished
// since we are removing (not adding) children.
func (s *subscriptionModificationService) resolveCustomersByExternalIDs(ctx context.Context, externalIDs []string) ([]string, error) {
	childFilter := types.NewNoLimitCustomerFilter()
	childFilter.ExternalIDs = externalIDs
	customers, err := s.serviceParams.CustomerRepo.ListAll(ctx, childFilter)
	if err != nil {
		return nil, err
	}

	byExternalID := make(map[string]*customer.Customer, len(customers))
	for _, c := range customers {
		byExternalID[c.ExternalID] = c
	}

	result := make([]string, 0, len(externalIDs))
	for _, extID := range externalIDs {
		c, ok := byExternalID[extID]
		if !ok {
			return nil, ierr.NewError("customer not found").
				WithHint("No customer exists for the given external ID").
				WithReportableDetails(map[string]interface{}{"external_id": extID}).
				Mark(ierr.ErrNotFound)
		}
		result = append(result, c.ID)
	}
	return result, nil
}

// findInheritedSubForChild returns the active or trialing inherited subscription
// for a given child customer under the specified parent subscription.
func (s *subscriptionModificationService) findInheritedSubForChild(ctx context.Context, parentSubID, childCustomerID string) (*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentSubID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	filter.CustomerID = childCustomerID

	subs, err := s.serviceParams.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, ierr.NewError("inherited subscription not found for child customer").
			WithHint("No active inherited subscription exists for this child customer under the given parent").
			WithReportableDetails(map[string]interface{}{
				"parent_subscription_id": parentSubID,
				"child_customer_id":      childCustomerID,
			}).
			Mark(ierr.ErrNotFound)
	}
	return subs[0], nil
}
```

- [ ] **Step 6: Add `executeRemoveInheritance` to `subscription_modification.go`**

Append after the helpers added in Step 5:

```go
func (s *subscriptionModificationService) executeRemoveInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// 1. Fetch and validate parent
	parentSub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil, ierr.NewError("subscription is not a parent subscription").
			WithHint("Only parent subscriptions can have inherited children removed").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":   subscriptionID,
				"subscription_type": parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}
	if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
		parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return nil, ierr.NewError("parent subscription is not active or trialing").
			WithHint("The parent subscription must be active or trialing to remove inherited children").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          parentSub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 2. Resolve external customer IDs → internal IDs (no status check for remove)
	childCustomerIDs, err := s.resolveCustomersByExternalIDs(ctx, params.ExternalCustomerIDsToRemove)
	if err != nil {
		return nil, err
	}

	// 3. Find each child's inherited sub and guard against double-scheduling
	childSubs := make([]*subscription.Subscription, 0, len(childCustomerIDs))
	for _, childCustomerID := range childCustomerIDs {
		childSub, err := s.findInheritedSubForChild(ctx, subscriptionID, childCustomerID)
		if err != nil {
			return nil, err
		}
		if childSub.CancelAt != nil {
			return nil, ierr.NewError("inherited subscription is already scheduled for removal").
				WithHint("The inherited subscription already has a scheduled cancellation").
				WithReportableDetails(map[string]interface{}{
					"child_subscription_id": childSub.ID,
					"cancel_at":             childSub.CancelAt,
				}).
				Mark(ierr.ErrValidation)
		}
		childSubs = append(childSubs, childSub)
	}

	// 4. Effective date = parent's current period end
	effectiveDate := parentSub.CurrentPeriodEnd

	// 5. Transaction: schedule each inherited sub for cancellation at period end
	changedSubs := make([]dto.ChangedSubscription, 0, len(childSubs))
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedSubs = nil
		for _, childSub := range childSubs {
			childSub.CancelAt = &effectiveDate
			childSub.CancelAtPeriodEnd = true
			if err := sp.SubRepo.Update(txCtx, childSub); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to schedule inherited subscription for removal").
					WithReportableDetails(map[string]interface{}{
						"child_subscription_id": childSub.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:               childSub.ID,
				Action:           dto.ChangedSubscriptionActionUpdated,
				Status:           childSub.SubscriptionStatus,
				CurrentPeriodEnd: &effectiveDate,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 6. Return response with parent subscription and changed children
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}
```

- [ ] **Step 7: Add `previewRemoveInheritance` to `subscription_modification.go`**

Append after `executeRemoveInheritance`:

```go
func (s *subscriptionModificationService) previewRemoveInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Validate parent (same as execute, no DB writes)
	parentSub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil, ierr.NewError("subscription is not a parent subscription").
			WithHint("Only parent subscriptions can have inherited children removed").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":   subscriptionID,
				"subscription_type": parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}
	if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
		parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return nil, ierr.NewError("parent subscription is not active or trialing").
			WithHint("The parent subscription must be active or trialing to remove inherited children").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          parentSub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Resolve customers
	childCustomerIDs, err := s.resolveCustomersByExternalIDs(ctx, params.ExternalCustomerIDsToRemove)
	if err != nil {
		return nil, err
	}

	// Validate children and build preview response (no DB mutations)
	effectiveDate := parentSub.CurrentPeriodEnd
	changedSubs := make([]dto.ChangedSubscription, 0, len(childCustomerIDs))
	for _, childCustomerID := range childCustomerIDs {
		childSub, err := s.findInheritedSubForChild(ctx, subscriptionID, childCustomerID)
		if err != nil {
			return nil, err
		}
		if childSub.CancelAt != nil {
			return nil, ierr.NewError("inherited subscription is already scheduled for removal").
				WithHint("The inherited subscription already has a scheduled cancellation").
				WithReportableDetails(map[string]interface{}{
					"child_subscription_id": childSub.ID,
				}).
				Mark(ierr.ErrValidation)
		}
		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:               childSub.ID,
			Action:           dto.ChangedSubscriptionActionUpdated,
			Status:           childSub.SubscriptionStatus,
			CurrentPeriodEnd: &effectiveDate,
		})
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}
```

- [ ] **Step 8: Run all new tests to confirm they pass**

```bash
go test -v -race ./internal/service/... -run "TestSubscriptionModificationServiceSuite/TestExecuteRemoveInheritance|TestSubscriptionModificationServiceSuite/TestPreviewRemoveInheritance" 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 9: Run the full modification test suite to check for regressions**

```bash
go test -v -race ./internal/service/... -run "TestSubscriptionModificationServiceSuite" 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/service/subscription_modification.go internal/service/subscription_modification_test.go
git commit -m "feat(subscription): add executeRemoveInheritance and previewRemoveInheritance"
```

---

## Task 3: Period-End Cron Guard

**Files:**
- Modify: `internal/service/subscription.go`
- Modify: `internal/service/subscription_test.go`

When the period-end cron fires on an `inherited` subscription that has `cancel_at_period_end = true`, it must cancel the sub without generating an invoice and without advancing the period.

### Step 3a — Write the failing test

- [ ] **Step 1: Add a test for the cron guard to `subscription_test.go`**

Find the `TestProcessSubscriptionPeriod` test (around line 4390). Add the following new test after it:

```go
// TestProcessSubscriptionPeriod_InheritedWithCancelAtPeriodEnd verifies that an inherited
// subscription with cancel_at_period_end=true is cancelled at period end without having
// its period advanced and without generating an invoice.
func (s *SubscriptionServiceSuite) TestProcessSubscriptionPeriod_InheritedWithCancelAtPeriodEnd() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	// Set up a parent subscription
	parentSub := s.testData.subscription
	periodEnd := now.Add(-time.Minute) // period already ended

	// Create an inherited sub that is scheduled for cancellation at period end
	cancelAt := periodEnd
	inheritedSub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		CustomerID:         types.GenerateUUID(),
		PlanID:             parentSub.PlanID,
		Currency:           parentSub.Currency,
		BillingPeriod:      parentSub.BillingPeriod,
		BillingPeriodCount: parentSub.BillingPeriodCount,
		BillingCycle:       parentSub.BillingCycle,
		BillingAnchor:      parentSub.BillingAnchor,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeInherited,
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   periodEnd,
		StartDate:          now.AddDate(0, -1, 0),
		ParentSubscriptionID: &parentSub.ID,
		CancelAt:           &cancelAt,
		CancelAtPeriodEnd:  true,
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, inheritedSub))

	subService := s.service.(*subscriptionService)
	err := subService.processSubscriptionPeriod(ctx, inheritedSub, now)
	s.Require().NoError(err)

	// The inherited sub must be cancelled
	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, inheritedSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusCancelled, updated.SubscriptionStatus)
	s.Require().NotNil(updated.CancelledAt)
	s.Equal(cancelAt.UTC(), updated.CancelledAt.UTC())
	s.Require().NotNil(updated.EndDate)
	s.Equal(cancelAt.UTC(), updated.EndDate.UTC())

	// Period must NOT have been advanced
	s.Equal(inheritedSub.CurrentPeriodStart.UTC(), updated.CurrentPeriodStart.UTC(), "period start must not change")
	s.Equal(periodEnd.UTC(), updated.CurrentPeriodEnd.UTC(), "period end must not change")
}

// TestProcessSubscriptionPeriod_InheritedWithoutCancelAtPeriodEnd verifies that a plain
// inherited subscription (no cancellation scheduled) still just advances its period.
func (s *SubscriptionServiceSuite) TestProcessSubscriptionPeriod_InheritedWithoutCancelAtPeriodEnd() {
	ctx := s.GetContext()
	now := time.Now().UTC()
	parentSub := s.testData.subscription
	periodEnd := now.Add(-time.Minute)

	inheritedSub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		CustomerID:         types.GenerateUUID(),
		PlanID:             parentSub.PlanID,
		Currency:           parentSub.Currency,
		BillingPeriod:      parentSub.BillingPeriod,
		BillingPeriodCount: parentSub.BillingPeriodCount,
		BillingCycle:       parentSub.BillingCycle,
		BillingAnchor:      parentSub.BillingAnchor,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeInherited,
		CurrentPeriodStart: now.AddDate(0, -1, 0),
		CurrentPeriodEnd:   periodEnd,
		StartDate:          now.AddDate(0, -1, 0),
		ParentSubscriptionID: &parentSub.ID,
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, inheritedSub))

	subService := s.service.(*subscriptionService)
	err := subService.processSubscriptionPeriod(ctx, inheritedSub, now)
	s.Require().NoError(err)

	// Period should have been advanced, status stays active
	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, inheritedSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, updated.SubscriptionStatus)
	s.True(updated.CurrentPeriodStart.After(inheritedSub.CurrentPeriodStart), "period start must advance")
}
```

- [ ] **Step 2: Run the new tests to confirm they fail**

```bash
go test -v -race ./internal/service/... -run "TestSubscriptionServiceSuite/TestProcessSubscriptionPeriod_Inherited" 2>&1 | tail -20
```

Expected: `TestProcessSubscriptionPeriod_InheritedWithCancelAtPeriodEnd` FAILS (inherited sub not being cancelled), `TestProcessSubscriptionPeriod_InheritedWithoutCancelAtPeriodEnd` PASSES (existing behavior already correct).

### Step 3b — Implement

- [ ] **Step 3: Add `cancelInheritedSubscriptionAtPeriodEnd` to `subscription.go`**

Find the `CascadeCancelToInheritedSubscriptions` function (around line 3364). Add the new function right after it:

```go
// cancelInheritedSubscriptionAtPeriodEnd cancels an inherited subscription that was
// scheduled for removal at period end. It does not generate an invoice — the parent
// subscription's invoice already covers this child's full-period usage.
func (s *subscriptionService) cancelInheritedSubscriptionAtPeriodEnd(ctx context.Context, sub *subscription.Subscription) error {
	cancelledAt := *sub.CancelAt
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	sub.CancelledAt = &cancelledAt
	sub.EndDate = &cancelledAt
	s.Logger.Info(ctx, "cancelling inherited subscription at period end",
		"subscription_id", sub.ID,
		"cancelled_at", cancelledAt)
	return s.SubRepo.Update(ctx, sub)
}
```

- [ ] **Step 4: Extend the `SubscriptionTypeInherited` block in `processSubscriptionPeriod`**

Find the block at line ~3007:

```go
	// For inherited subscriptions, skip invoice creation and only advance the billing period.
	// Invoices are created on the parent subscription; the child just needs its period kept current.
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		newPeriod := periods[len(periods)-1]
		sub.CurrentPeriodStart = newPeriod.start
		sub.CurrentPeriodEnd = newPeriod.end
		s.Logger.Info(ctx, "advancing period for inherited subscription (no invoice created)",
			"subscription_id", sub.ID,
			"new_period_start", sub.CurrentPeriodStart,
			"new_period_end", sub.CurrentPeriodEnd,
			"periods_skipped", len(periods)-1)
		return s.SubRepo.Update(ctx, sub)
	}
```

Replace it with:

```go
	// For inherited subscriptions, skip invoice creation.
	// Invoices are created on the parent subscription; the child just needs its period kept current.
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		// If scheduled for period-end removal, cancel without advancing period.
		if sub.CancelAtPeriodEnd && sub.CancelAt != nil {
			return s.cancelInheritedSubscriptionAtPeriodEnd(ctx, sub)
		}
		// Otherwise, just advance the period; no invoice created.
		newPeriod := periods[len(periods)-1]
		sub.CurrentPeriodStart = newPeriod.start
		sub.CurrentPeriodEnd = newPeriod.end
		s.Logger.Info(ctx, "advancing period for inherited subscription (no invoice created)",
			"subscription_id", sub.ID,
			"new_period_start", sub.CurrentPeriodStart,
			"new_period_end", sub.CurrentPeriodEnd,
			"periods_skipped", len(periods)-1)
		return s.SubRepo.Update(ctx, sub)
	}
```

- [ ] **Step 5: Run the new tests to confirm they pass**

```bash
go test -v -race ./internal/service/... -run "TestSubscriptionServiceSuite/TestProcessSubscriptionPeriod_Inherited" 2>&1 | tail -20
```

Expected: both PASS.

- [ ] **Step 6: Run the broader subscription service test suite to check for regressions**

```bash
go test -race ./internal/service/... -run "TestSubscriptionServiceSuite" -timeout 300s 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_test.go
git commit -m "feat(subscription): cancel inherited sub at period end without invoice"
```

---

## Task 4: Final Smoke Check

- [ ] **Step 1: Run full service test suite**

```bash
go test -race ./internal/service/... -timeout 300s 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 2: Vet the whole codebase**

```bash
go vet ./...
```

Expected: no errors.

- [ ] **Step 3: Final commit summarising the feature**

```bash
git add -A
git commit -m "feat(subscription): remove inherited child subscription via modify/execute

Adds action=remove to the inheritance modify type. Callers post to
POST /subscriptions/:parentId/modify/execute with
{ type: inheritance, inheritance_params: { action: remove,
  external_customer_ids_to_remove: [...] } }
The inherited sub is scheduled for cancellation at parent.CurrentPeriodEnd
and cancelled by the period-end cron without generating a separate invoice."
```
