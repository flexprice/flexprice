# Events Pipeline Inherited Subscription Usage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `GetUsageBySubscription` (the normal events pipeline) aggregate usage from all active inherited child customers when called on a parent subscription, matching the behaviour of the feature_usage pipeline.

**Architecture:** Add `externalCustomerIDsForSubscription()` to `subscriptionService` that resolves parent + active/trialing/draft children to external customer IDs. Wire this into `GetUsageBySubscription` so both `GetDistinctEventNames` and each `GetUsageByMeterRequest` fan out across all child customers. Delete the duplicate `getChildExternalCustomerIDsForSubscription()` from `billingService` and replace its 3 call sites.

**Tech Stack:** Go 1.23, Uber FX DI, in-memory test repos (testutil), testify/suite

---

## File Map

| File | Change |
|------|--------|
| `internal/service/subscription.go` | Fix status filter in `getInheritedSubscriptions`; add `externalCustomerIDsForSubscription`; refactor `GetUsageBySubscription` |
| `internal/service/billing.go` | Replace 3 call sites of `getChildExternalCustomerIDsForSubscription`; delete the method |
| `internal/service/subscription_test.go` | Add 3 new test cases |

---

### Task 1: Fix status filter in `getInheritedSubscriptions`

**Files:**
- Modify: `internal/service/subscription.go:7434-7438`

`getInheritedSubscriptions` currently includes `SubscriptionStatusPaused` in its filter. This drives `usageCustomerIDsForSubscription` (feature_usage/meter_usage pipelines). Paused children must be excluded to be consistent with `getChildExternalCustomerIDsForSubscription` in billing.go.

- [ ] **Step 1: Remove Paused from the status filter**

In `internal/service/subscription.go`, locate `getInheritedSubscriptions` (~line 7430). Change:

```go
filter.SubscriptionStatus = []types.SubscriptionStatus{
    types.SubscriptionStatusActive,
    types.SubscriptionStatusTrialing,
    types.SubscriptionStatusDraft,
    types.SubscriptionStatusPaused,
}
```

to:

```go
filter.SubscriptionStatus = []types.SubscriptionStatus{
    types.SubscriptionStatusActive,
    types.SubscriptionStatusTrialing,
    types.SubscriptionStatusDraft,
}
```

- [ ] **Step 2: Verify the project still builds**

```bash
go build ./internal/service/...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription.go
git commit -m "fix(subscription): exclude paused children from usage customer ID resolution"
```

---

### Task 2: Add `externalCustomerIDsForSubscription()` to subscriptionService

**Files:**
- Modify: `internal/service/subscription.go` (insert after line 7427)

- [ ] **Step 1: Write the failing test**

At the bottom of `internal/service/subscription_test.go`, add:

```go
func (s *SubscriptionServiceSuite) TestExternalCustomerIDsForSubscription() {
    ctx := s.GetContext()
    svc := s.service.(*subscriptionService)

    tests := []struct {
        name    string
        setup   func() *subscription.Subscription
        wantIDs []string
    }{
        {
            name: "standalone subscription returns only owner external ID",
            setup: func() *subscription.Subscription {
                return s.testData.subscription // already standalone, ExternalID = "ext_cust_123"
            },
            wantIDs: []string{"ext_cust_123"},
        },
        {
            name: "parent subscription includes active child external IDs",
            setup: func() *subscription.Subscription {
                // promote the existing sub to parent
                parentSub := s.testData.subscription
                parentSub.SubscriptionType = types.SubscriptionTypeParent

                // create a child customer
                childCust := &customer.Customer{
                    ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
                    ExternalID: "ext_child_1",
                    Name:       "Child Customer",
                    BaseModel:  types.GetDefaultBaseModel(ctx),
                }
                s.NoError(s.GetStores().CustomerRepo.Create(ctx, childCust))

                // create an inherited subscription for the child
                childSub := &subscription.Subscription{
                    ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
                    CustomerID:           childCust.ID,
                    PlanID:               parentSub.PlanID,
                    SubscriptionStatus:   types.SubscriptionStatusActive,
                    SubscriptionType:     types.SubscriptionTypeInherited,
                    ParentSubscriptionID: lo.ToPtr(parentSub.ID),
                    Currency:             parentSub.Currency,
                    BaseModel:            types.GetDefaultBaseModel(ctx),
                }
                s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, childSub))
                return parentSub
            },
            wantIDs: []string{"ext_cust_123", "ext_child_1"},
        },
        {
            name: "parent subscription excludes paused child",
            setup: func() *subscription.Subscription {
                parentSub := s.testData.subscription
                parentSub.SubscriptionType = types.SubscriptionTypeParent

                pausedCust := &customer.Customer{
                    ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
                    ExternalID: "ext_paused_child",
                    Name:       "Paused Child Customer",
                    BaseModel:  types.GetDefaultBaseModel(ctx),
                }
                s.NoError(s.GetStores().CustomerRepo.Create(ctx, pausedCust))

                pausedSub := &subscription.Subscription{
                    ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
                    CustomerID:           pausedCust.ID,
                    PlanID:               parentSub.PlanID,
                    SubscriptionStatus:   types.SubscriptionStatusPaused,
                    SubscriptionType:     types.SubscriptionTypeInherited,
                    ParentSubscriptionID: lo.ToPtr(parentSub.ID),
                    Currency:             parentSub.Currency,
                    BaseModel:            types.GetDefaultBaseModel(ctx),
                }
                s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, pausedSub))
                return parentSub
            },
            wantIDs: []string{"ext_cust_123"}, // paused child excluded
        },
    }

    for _, tt := range tests {
        s.Run(tt.name, func() {
            s.ClearStores()
            s.setupTestData()
            sub := tt.setup()
            got, err := svc.externalCustomerIDsForSubscription(ctx, sub)
            s.NoError(err)
            s.ElementsMatch(tt.wantIDs, got)
        })
    }
}
```

- [ ] **Step 2: Run the test to confirm it fails (method doesn't exist yet)**

```bash
go test -v -run TestSubscriptionService/TestExternalCustomerIDsForSubscription ./internal/service/...
```

Expected: compile error — `svc.externalCustomerIDsForSubscription undefined`.

- [ ] **Step 3: Add `externalCustomerIDsForSubscription()` to subscriptionService**

In `internal/service/subscription.go`, insert after the closing brace of `usageCustomerIDsForSubscription` (~line 7427):

```go
// externalCustomerIDsForSubscription returns distinct non-empty external customer IDs
// for the subscription owner plus all active/trialing/draft inherited children.
func (s *subscriptionService) externalCustomerIDsForSubscription(ctx context.Context, sub *subscription.Subscription) ([]string, error) {
	internalIDs, err := s.usageCustomerIDsForSubscription(ctx, sub)
	if err != nil {
		return nil, err
	}
	custFilter := types.NewNoLimitCustomerFilter()
	custFilter.CustomerIDs = internalIDs
	customers, err := s.CustomerRepo.List(ctx, custFilter)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(customers))
	for _, c := range customers {
		if c.ExternalID != "" {
			out = append(out, c.ExternalID)
		}
	}
	return lo.Uniq(out), nil
}
```

- [ ] **Step 4: Run the test and confirm it passes**

```bash
go test -v -run TestSubscriptionService/TestExternalCustomerIDsForSubscription ./internal/service/...
```

Expected: `PASS` for all 3 sub-tests.

- [ ] **Step 5: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_test.go
git commit -m "feat(subscription): add externalCustomerIDsForSubscription helper for parent+child external ID resolution"
```

---

### Task 3: Wire `externalCustomerIDsForSubscription` into `GetUsageBySubscription`

**Files:**
- Modify: `internal/service/subscription.go:2186-2344`

- [ ] **Step 1: Write the failing test**

Add to `internal/service/subscription_test.go`:

```go
func (s *SubscriptionServiceSuite) TestGetUsageBySubscription_ParentIncludesChildUsage() {
    ctx := s.GetContext()
    now := s.testData.now

    // Create child customer
    childCust := &customer.Customer{
        ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
        ExternalID: "ext_child_usage",
        Name:       "Child Customer",
        BaseModel:  types.GetDefaultBaseModel(ctx),
    }
    s.NoError(s.GetStores().CustomerRepo.Create(ctx, childCust))

    // Promote existing subscription to parent
    parentSub := s.testData.subscription
    parentSub.SubscriptionType = types.SubscriptionTypeParent
    s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, parentSub))

    // Create inherited subscription for child (no line items needed — line items live on parent)
    childSub := &subscription.Subscription{
        ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
        CustomerID:           childCust.ID,
        PlanID:               parentSub.PlanID,
        SubscriptionStatus:   types.SubscriptionStatusActive,
        SubscriptionType:     types.SubscriptionTypeInherited,
        ParentSubscriptionID: lo.ToPtr(parentSub.ID),
        Currency:             parentSub.Currency,
        BaseModel:            types.GetDefaultBaseModel(ctx),
    }
    s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, childSub))

    // Ingest 500 api_call events for the child customer
    for i := 0; i < 500; i++ {
        event := &events.Event{
            ID:                 s.GetUUID(),
            TenantID:           parentSub.TenantID,
            EventName:          s.testData.meters.apiCalls.EventName,
            ExternalCustomerID: childCust.ExternalID,
            Timestamp:          now.Add(-1 * time.Hour),
            Properties:         map[string]interface{}{},
        }
        s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, event))
    }

    // Parent already has 1500 api_call events from setupTestData.
    // After this test: parent=1500 + child=500 = 2000 total api_calls.
    // Cost at tiered pricing: (1000*0.02) + (1000*0.005) = 20 + 5 = 25
    resp, err := s.service.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
        SubscriptionID: parentSub.ID,
        StartTime:      now.Add(-48 * time.Hour),
        EndTime:        now,
    })
    s.NoError(err)

    // Find api_calls charge
    var apiCharge *dto.SubscriptionUsageByMetersResponse
    for _, c := range resp.Charges {
        if c.MeterDisplayName == "API Calls" {
            apiCharge = c
            break
        }
    }
    s.Require().NotNil(apiCharge, "expected API Calls charge in response")
    s.Equal(float64(2000), apiCharge.Quantity)
    s.Equal(25.0, apiCharge.Amount) // (1000*0.02) + (1000*0.005)
}
```

- [ ] **Step 2: Run the test to confirm it fails (child events not included yet)**

```bash
go test -v -run TestSubscriptionService/TestGetUsageBySubscription_ParentIncludesChildUsage ./internal/service/...
```

Expected: FAIL — `apiCharge.Quantity` is `1500`, not `2000`.

- [ ] **Step 3: Refactor `GetUsageBySubscription` to use multi-customer IDs**

In `internal/service/subscription.go`, inside `GetUsageBySubscription` (~line 2174):

**3a.** Delete the `customer` fetch block (lines ~2186-2190):

```go
// DELETE these lines:
// Get customer
customer, err := s.CustomerRepo.Get(ctx, subscription.CustomerID)
if err != nil {
    return nil, err
}
```

**3b.** In its place, add the external ID resolution right after the subscription fetch:

```go
externalCustomerIDs, err := s.externalCustomerIDsForSubscription(ctx, subscription)
if err != nil {
    return nil, err
}
```

**3c.** Update the `GetDistinctEventNames` call (~line 2262). Change:

```go
distinctEventNames, err := s.EventRepo.GetDistinctEventNames(ctx, []string{customer.ExternalID}, usageStartTime, usageEndTime)
if err != nil {
    s.Logger.ErrorwCtx(ctx, "failed to get distinct event names",
        "error", err,
        "external_customer_id", customer.ExternalID)
    return nil, fmt.Errorf("failed to get distinct event names for customer %s: %w", customer.ExternalID, err)
}
```

to:

```go
distinctEventNames, err := s.EventRepo.GetDistinctEventNames(ctx, externalCustomerIDs, usageStartTime, usageEndTime)
if err != nil {
    s.Logger.ErrorwCtx(ctx, "failed to get distinct event names",
        "error", err,
        "subscription_id", req.SubscriptionID)
    return nil, fmt.Errorf("failed to get distinct event names for subscription %s: %w", req.SubscriptionID, err)
}
```

**3d.** Update the debug log that follows (~line 2276). Change:

```go
s.Logger.DebugwCtx(ctx, "distinct event names optimization",
    "external_customer_id", customer.ExternalID,
    "total_distinct_events", len(distinctEventNames),
    "total_line_items", len(lineItems),
    "distinct_event_names", distinctEventNames)
```

to:

```go
s.Logger.DebugwCtx(ctx, "distinct event names optimization",
    "subscription_id", req.SubscriptionID,
    "external_customer_ids", externalCustomerIDs,
    "total_distinct_events", len(distinctEventNames),
    "total_line_items", len(lineItems),
    "distinct_event_names", distinctEventNames)
```

**3e.** Inside the meter request building loop (~line 2297-2323), remove the two debug log blocks that reference `customer.ID` and `customer.ExternalID`:

```go
// DELETE this block (no-events skip log):
s.Logger.DebugwCtx(ctx, "skipping meter as there are no events",
    "meter_id", lineItem.MeterID,
    "event_name", meter.EventName,
    "customer_id", customer.ID,
    "external_customer_id", customer.ExternalID,
    "subscription_id", req.SubscriptionID)

// DELETE this block (no-events-for-meter skip log):
s.Logger.DebugwCtx(ctx, "skipping meter with no events",
    "meter_id", lineItem.MeterID,
    "event_name", meter.EventName,
    "customer_id", customer.ID,
    "external_customer_id", customer.ExternalID,
    "subscription_id", req.SubscriptionID)
```

**3f.** In the `GetUsageByMeterRequest` construction (~line 2326), change:

```go
usageRequest := &dto.GetUsageByMeterRequest{
    MeterID:            meterID,
    PriceID:            lineItem.PriceID,
    Meter:              meter.ToMeter(),
    ExternalCustomerID: customer.ExternalID,
    StartTime:          lineItem.GetPeriodStart(usageStartTime),
    EndTime:            lineItem.GetPeriodEnd(usageEndTime),
    Filters:            make(map[string][]string),
}
```

to:

```go
usageRequest := &dto.GetUsageByMeterRequest{
    MeterID:             meterID,
    PriceID:             lineItem.PriceID,
    Meter:               meter.ToMeter(),
    ExternalCustomerIDs: externalCustomerIDs,
    StartTime:           lineItem.GetPeriodStart(usageStartTime),
    EndTime:             lineItem.GetPeriodEnd(usageEndTime),
    Filters:             make(map[string][]string),
}
```

**3g.** Update the performance log (~line 2342). Change:

```go
s.Logger.InfowCtx(ctx, "performance optimization results",
    "subscription_id", req.SubscriptionID,
    "external_customer_id", customer.ExternalID,
    ...
```

to:

```go
s.Logger.InfowCtx(ctx, "performance optimization results",
    "subscription_id", req.SubscriptionID,
    "external_customer_ids", externalCustomerIDs,
    ...
```

- [ ] **Step 4: Build to confirm no compile errors**

```bash
go build ./internal/service/...
```

Expected: clean build.

- [ ] **Step 5: Run the new test and confirm it passes**

```bash
go test -v -run TestSubscriptionService/TestGetUsageBySubscription_ParentIncludesChildUsage ./internal/service/...
```

Expected: PASS.

- [ ] **Step 6: Run the full existing GetUsageBySubscription test suite to catch regressions**

```bash
go test -v -run TestSubscriptionService/TestGetUsageBySubscription ./internal/service/...
```

Expected: all existing sub-tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_test.go
git commit -m "feat(subscription): include inherited child customers in events pipeline usage calculation"
```

---

### Task 4: Replace billing.go call sites and delete `getChildExternalCustomerIDsForSubscription`

**Files:**
- Modify: `internal/service/billing.go:440,1157,3230`
- Delete: `internal/service/billing.go:3098-3134`

There are 3 call sites in billing.go. In all 3, `subscriptionService` is already instantiated in the enclosing function (via `NewSubscriptionService(s.ServiceParams)`). The replacement is a direct method call on that local variable.

- [ ] **Step 1: Replace the 3 call sites**

**Call site 1** (~line 440):

```go
// Before:
extCustomerIDsForUsage, err := s.getChildExternalCustomerIDsForSubscription(ctx, sub)

// After:
extCustomerIDsForUsage, err := subscriptionService.(*subscriptionService).externalCustomerIDsForSubscription(ctx, sub)
```

Wait — `subscriptionService` is typed as `SubscriptionService` (interface). Since `externalCustomerIDsForSubscription` is a private method it is NOT on the interface. You need to call it differently.

The simplest fix: inline the call as a local helper in the billing function, OR expose it via a new unexported-but-accessible approach.

**Correct approach:** Since `externalCustomerIDsForSubscription` is private, and `billingService` has direct access to the same repos (`SubRepo`, `CustomerRepo`), call the shared logic by constructing a temporary `subscriptionService` and using a type assertion:

```go
extCustomerIDsForUsage, err := NewSubscriptionService(s.ServiceParams).(*subscriptionService).externalCustomerIDsForSubscription(ctx, sub)
if err != nil {
    return nil, decimal.Zero, err
}
```

Apply the same pattern to all 3 call sites (~lines 440, 1157, 3230):

```go
// Line ~440
extCustomerIDsForUsage, err := NewSubscriptionService(s.ServiceParams).(*subscriptionService).externalCustomerIDsForSubscription(ctx, sub)
if err != nil {
    return nil, decimal.Zero, err
}

// Line ~1157
extCustomerIDsForUsage, err := NewSubscriptionService(s.ServiceParams).(*subscriptionService).externalCustomerIDsForSubscription(ctx, sub)
if err != nil {
    return nil, decimal.Zero, err
}

// Line ~3230 (error return is `return nil, err` in this function — match the surrounding pattern)
extCustomerIDsForMeter, err := NewSubscriptionService(s.ServiceParams).(*subscriptionService).externalCustomerIDsForSubscription(ctx, sub)
if err != nil {
    return nil, err
}
```

- [ ] **Step 2: Delete `getChildExternalCustomerIDsForSubscription` from billingService**

Remove lines ~3098-3134 from `internal/service/billing.go` (the full function body including its comment).

- [ ] **Step 3: Build to confirm no compile errors**

```bash
go build ./internal/service/...
```

Expected: clean build.

- [ ] **Step 4: Run the billing-related tests**

```bash
go test -v -run TestBilling ./internal/service/...
```

Expected: all billing tests pass (or the same set that passed before this PR — no new failures).

- [ ] **Step 5: Commit**

```bash
git add internal/service/billing.go
git commit -m "refactor(billing): replace getChildExternalCustomerIDsForSubscription with shared externalCustomerIDsForSubscription"
```

---

### Task 5: Final verification

- [ ] **Step 1: Run the full subscription service test suite**

```bash
go test -v -race ./internal/service/... 2>&1 | tail -30
```

Expected: all tests pass, no race conditions.

- [ ] **Step 2: Vet the changed packages**

```bash
go vet ./internal/service/...
```

Expected: no output.

- [ ] **Step 3: Commit (if any last-minute fixes were needed)**

If steps 1-2 required fixes, commit them:
```bash
git add internal/service/
git commit -m "fix(subscription): address vet/race issues in usage pipeline changes"
```
