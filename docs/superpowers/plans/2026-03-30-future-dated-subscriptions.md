# Future-Dated Subscriptions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable subscriptions to be created with a future `start_date`, and fix ghost billing caused by plan line items not receiving an `EndDate` on cancellation.

**Architecture:** Three surgical fixes — (1) remove the DTO validation that blocks future `start_date`, (2) add a helper that propagates `EndDate` to plan line items on cancellation, (3) add an `EndDate` pre-filter in billing so expired line items are never invoiced.

**Tech Stack:** Go 1.23, Gin, Ent ORM, PostgreSQL, testify/suite, samber/lo

---

## File Map

| File | Change |
|---|---|
| `internal/api/dto/subscription.go` | Remove 8-line future-date validation block |
| `internal/service/subscription.go` | Add `cancelPlanLineItemsForSubscription()` helper, call it after `cancelAddonsForSubscription` |
| `internal/service/billing.go` | Add `EndDate` pre-filter in `FilterLineItemsToBeInvoiced` and guard in `CalculateFixedCharges` |
| `internal/service/subscription_test.go` | Add: future start_date accepted + cancellation sets EndDate on plan line items |
| `internal/service/billing_test.go` | Add: billing skips line items with EndDate before period start |

**Import note:** `github.com/samber/lo` and `github.com/shopspring/decimal` are already imported in both `subscription_test.go` and `billing_test.go`. Do not add them again.

---

## Task 1: Remove Future-Date Validation

**Files:**
- Modify: `internal/api/dto/subscription.go` (lines 731–738)

### Context

`CreateSubscriptionRequest.Validate()` currently rejects any `start_date` in the future. The `StartDate` field is always non-nil at this point — a default of `time.Now().UTC()` is assigned earlier in `Validate()` if it is nil. Removing the block is safe.

- [ ] **Step 1: Write the failing test**

Add to `internal/service/subscription_test.go` inside `SubscriptionServiceSuite`:

```go
func (s *SubscriptionServiceSuite) TestCreateSubscription_FutureDateAllowed() {
    futureDate := time.Now().UTC().Add(30 * 24 * time.Hour) // 30 days from now
    req := &dto.CreateSubscriptionRequest{
        CustomerID: s.testData.customer.ID,
        PlanID:     s.testData.plan.ID,
        StartDate:  &futureDate,
        Currency:   "USD",
    }
    err := req.Validate()
    s.NoError(err, "future start_date should be allowed")
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/pensive-cohen
go test ./internal/service/... -run TestSubscriptionService/TestCreateSubscription_FutureDateAllowed -v
```

Expected: FAIL — `"start_date cannot be in the future"`

- [ ] **Step 3: Remove the validation block**

In `internal/api/dto/subscription.go`, delete lines 731–738:

```go
// DELETE THESE LINES:
if r.StartDate != nil && r.StartDate.After(time.Now().UTC()) {
	return ierr.NewError("start_date cannot be in the future").
		WithHint("Start date must be in the past or present").
		WithReportableDetails(map[string]interface{}{
			"start_date": *r.StartDate,
		}).
		Mark(ierr.ErrValidation)
}
```

- [ ] **Step 4: Run the test and verify it passes**

```bash
go test ./internal/service/... -run TestSubscriptionService/TestCreateSubscription_FutureDateAllowed -v
```

Expected: PASS

- [ ] **Step 5: Run the full subscription test suite to confirm no regressions**

```bash
go test ./internal/service/... -run TestSubscriptionService -v 2>&1 | tail -20
```

Expected: all existing tests pass

- [ ] **Step 6: Vet and commit**

```bash
go vet ./internal/api/dto/... ./internal/service/...
git add internal/api/dto/subscription.go internal/service/subscription_test.go
git commit -m "feat: allow future start_date on subscription creation"
```

---

## Task 2: Propagate EndDate to Plan Line Items on Cancellation

**Files:**
- Modify: `internal/service/subscription.go`
- Test: `internal/service/subscription_test.go`

### Context

When a subscription is cancelled, `cancelAddonsForSubscription()` (line 4104) terminates addon line items but plan-level line items are never touched. The new helper `cancelPlanLineItemsForSubscription` fixes this by directly calling `s.SubscriptionLineItemRepo.Update()` (NOT `DeleteSubscriptionLineItem` — that function rejects `effectiveFrom < item.StartDate` which would panic for pre-start cancellations).

Call site: line 1780 in `CancelSubscription()`. The new helper goes immediately after `cancelAddonsForSubscription` (after line 1782).

### Helper Spec

```
Signature: cancelPlanLineItemsForSubscription(ctx context.Context, subscriptionID string, effectiveDate time.Time) error

1. List all plan line items for subscriptionID (EntityType = SubscriptionLineItemEntityTypePlan)
2. For each item:
   a. if item.StartDate.After(effectiveDate) → skip (never became active; subscription EndDate already protects billing)
   b. if item.EndDate.IsZero() || item.EndDate.After(effectiveDate):
      - set item.EndDate = effectiveDate
      - call s.SubscriptionLineItemRepo.Update(ctx, item)
3. Return nil on success, wrapped error on failure
```

- [ ] **Step 1: Write the failing test**

Add to `internal/service/subscription_test.go`:

```go
func (s *SubscriptionServiceSuite) TestCancelSubscription_SetsEndDateOnPlanLineItems() {
    // Create a subscription (start_date = now, so it's immediately active)
    now := time.Now().UTC()
    createReq := &dto.CreateSubscriptionRequest{
        CustomerID: s.testData.customer.ID,
        PlanID:     s.testData.plan.ID,
        StartDate:  &now,
        Currency:   "USD",
    }
    sub, err := s.service.CreateSubscription(s.GetContext(), createReq)
    s.Require().NoError(err)

    // Cancel immediately
    cancelReq := &dto.CancelSubscriptionRequest{
        CancellationType: types.CancellationTypeImmediate,
    }
    _, err = s.service.CancelSubscription(s.GetContext(), sub.ID, cancelReq)
    s.Require().NoError(err)

    // Reload line items and verify EndDate is set
    filter := types.NewNoLimitSubscriptionLineItemFilter()
    filter.SubscriptionIDs = []string{sub.ID}
    filter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypePlan)
    lineItems, err := s.GetStores().SubscriptionLineItemRepo.List(s.GetContext(), filter)
    s.Require().NoError(err)
    s.Require().NotEmpty(lineItems, "expected at least one plan line item")

    for _, item := range lineItems {
        s.False(item.EndDate.IsZero(),
            "plan line item %s should have EndDate set after cancellation", item.ID)
        s.True(item.EndDate.Before(now.Add(time.Minute)),
            "plan line item EndDate should be around cancellation time")
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
go test ./internal/service/... -run TestSubscriptionService/TestCancelSubscription_SetsEndDateOnPlanLineItems -v
```

Expected: FAIL — plan line items have zero EndDate

- [ ] **Step 3: Add the helper function**

At the end of `internal/service/subscription.go` (after `cancelAddonsForSubscription`, before the final closing brace of the file), add:

```go
// cancelPlanLineItemsForSubscription sets EndDate on all plan line items for the subscription
// up to effectiveDate. Items that have not yet started (StartDate > effectiveDate) are skipped
// because they never became active; the subscription-level EndDate already protects billing.
// Uses direct repository update (not DeleteSubscriptionLineItem) to avoid the effectiveFrom
// validation in that service function.
func (s *subscriptionService) cancelPlanLineItemsForSubscription(
	ctx context.Context,
	subscriptionID string,
	effectiveDate time.Time,
) error {
	logger := s.Logger.With("subscription_id", subscriptionID, "effective_date", effectiveDate)

	lineItemFilter := types.NewNoLimitSubscriptionLineItemFilter()
	lineItemFilter.SubscriptionIDs = []string{subscriptionID}
	lineItemFilter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypePlan)

	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		logger.Errorw("failed to list plan line items for cancellation", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to list plan line items for cancellation").
			Mark(ierr.ErrDatabase)
	}

	terminated := 0
	for _, item := range lineItems {
		// Skip items that haven't started yet — they never became active
		if item.StartDate.After(effectiveDate) {
			logger.Debugw("skipping plan line item not yet started",
				"line_item_id", item.ID,
				"start_date", item.StartDate)
			continue
		}
		// Skip items already terminated at or before effectiveDate
		if !item.EndDate.IsZero() && !item.EndDate.After(effectiveDate) {
			continue
		}
		item.EndDate = effectiveDate
		if err := s.SubscriptionLineItemRepo.Update(ctx, item); err != nil {
			logger.Errorw("failed to update plan line item end date",
				"line_item_id", item.ID,
				"error", err)
			return ierr.WithError(err).
				WithHintf("Failed to set EndDate on plan line item %s", item.ID).
				Mark(ierr.ErrDatabase)
		}
		terminated++
	}

	logger.Infow("terminated plan line items for subscription",
		"line_items_terminated", terminated)
	return nil
}
```

- [ ] **Step 4: Call the helper from CancelSubscription**

In `internal/service/subscription.go`, after line 1782 (after the `cancelAddonsForSubscription` call block), insert the new call **and** relabel the existing `// Step 7b:` scheduling comment to `// Step 7c:` to avoid duplicate labels:

```go
// Step 7b: Terminate plan line items (set EndDate = effectiveDate)
if err := s.cancelPlanLineItemsForSubscription(ctx, subscription.ID, effectiveDate); err != nil {
    return err
}
```

The surrounding context should look like:

```go
// Step 7a: Cancel all addons on the subscription (mark associations cancelled, terminate addon line items)
if err := s.cancelAddonsForSubscription(ctx, subscription.ID, effectiveDate, req.Reason); err != nil {
    return err
}

// Step 7b: Terminate plan line items (set EndDate = effectiveDate)
if err := s.cancelPlanLineItemsForSubscription(ctx, subscription.ID, effectiveDate); err != nil {
    return err
}

// Step 7c: Handle scheduling for future cancellations (end_of_period and scheduled_date)
```

- [ ] **Step 5: Run the test and verify it passes**

```bash
go test ./internal/service/... -run TestSubscriptionService/TestCancelSubscription_SetsEndDateOnPlanLineItems -v
```

Expected: PASS

- [ ] **Step 6: Run the full subscription test suite**

```bash
go test ./internal/service/... -run TestSubscriptionService -v 2>&1 | tail -30
```

Expected: all existing tests pass

- [ ] **Step 7: Vet and commit**

```bash
go vet ./internal/service/...
git add internal/service/subscription.go internal/service/subscription_test.go
git commit -m "feat: set EndDate on plan line items when subscription is cancelled"
```

---

## Task 3: Billing Skips Line Items with Expired EndDate

**Files:**
- Modify: `internal/service/billing.go`
- Test: `internal/service/billing_test.go`

### Context

Two functions need the guard:

1. **`FilterLineItemsToBeInvoiced`** (~line 2210): Add a pre-filter pass **before** the invoice-deduplication logic. The function has an early-return path when there are no existing invoices (`if len(invoices) == 0 { return lineItems, nil }`) — putting the guard inside the deduplication loop would not catch expired items on that path. The fix is to filter expired items out of `lineItems` before any other processing.

2. **`CalculateFixedCharges`** (~line 162): Add an early-continue guard after the existing `item.StartDate.After(periodEnd)` check (lines 168–176). The guard goes immediately after the closing `continue` of that block.

Guard condition: `if !item.EndDate.IsZero() && !item.EndDate.After(periodStart) { continue }`

Meaning: skip the item if it has an EndDate that is at or before the billing period's start.

- [ ] **Step 1: Write the failing test for FilterLineItemsToBeInvoiced**

Add to `internal/service/billing_test.go` inside `BillingServiceSuite`:

```go
func (s *BillingServiceSuite) TestFilterLineItemsToBeInvoiced_SkipsExpiredLineItems() {
    now := time.Now().UTC()
    periodStart := now
    periodEnd := now.Add(30 * 24 * time.Hour)

    // Line item that ended BEFORE the period — should be excluded
    expiredEndDate := now.Add(-1 * time.Hour)
    expiredItem := &subscription.SubscriptionLineItem{
        ID:        "li_expired",
        StartDate: now.Add(-60 * 24 * time.Hour),
        EndDate:   expiredEndDate,
        PriceType: types.PRICE_TYPE_FIXED,
        Status:    types.StatusPublished,
    }

    // Line item with no EndDate — should be included
    activeItem := &subscription.SubscriptionLineItem{
        ID:        "li_active",
        StartDate: now,
        PriceType: types.PRICE_TYPE_FIXED,
        Status:    types.StatusPublished,
    }

    sub := s.testData.subscription

    result, err := s.service.FilterLineItemsToBeInvoiced(
        s.GetContext(), sub, periodStart, periodEnd,
        []*subscription.SubscriptionLineItem{expiredItem, activeItem},
    )
    s.Require().NoError(err)
    s.Len(result, 1, "only the active line item should be returned")
    s.Equal("li_active", result[0].ID)
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
go test ./internal/service/... -run TestBillingService/TestFilterLineItemsToBeInvoiced_SkipsExpiredLineItems -v
```

Expected: FAIL — both items are returned (because no existing invoices → early-return path returns all items without filtering)

- [ ] **Step 3: Add the EndDate pre-filter to FilterLineItemsToBeInvoiced**

In `internal/service/billing.go`, inside `FilterLineItemsToBeInvoiced`, add a pre-filter pass after the `len(lineItems) == 0` early return (after line 2220) and before the `sub.EndDate` validation:

Current code at line 2218:
```go
// If no line items to process, return empty slice immediately
if len(lineItems) == 0 {
    return []*subscription.SubscriptionLineItem{}, nil
}

// Validate period against subscription end date
if sub.EndDate != nil && !periodStart.Before(*sub.EndDate) {
```

Change to:
```go
// If no line items to process, return empty slice immediately
if len(lineItems) == 0 {
    return []*subscription.SubscriptionLineItem{}, nil
}

// Pre-filter: exclude line items whose EndDate is before the billing period started.
// This must happen before the early-return path (no existing invoices) to ensure
// expired items are never included regardless of invoice history.
activeLineItems := make([]*subscription.SubscriptionLineItem, 0, len(lineItems))
for _, item := range lineItems {
    if !item.EndDate.IsZero() && !item.EndDate.After(periodStart) {
        continue
    }
    activeLineItems = append(activeLineItems, item)
}
lineItems = activeLineItems

// Validate period against subscription end date
if sub.EndDate != nil && !periodStart.Before(*sub.EndDate) {
```

- [ ] **Step 4: Run the FilterLineItemsToBeInvoiced test and verify it passes**

```bash
go test ./internal/service/... -run TestBillingService/TestFilterLineItemsToBeInvoiced_SkipsExpiredLineItems -v
```

Expected: PASS

- [ ] **Step 5: Write the failing test for CalculateFixedCharges**

Add to `internal/service/billing_test.go`:

```go
func (s *BillingServiceSuite) TestCalculateFixedCharges_SkipsExpiredLineItems() {
    now := time.Now().UTC()
    periodStart := now
    periodEnd := now.Add(30 * 24 * time.Hour)

    // Build a subscription whose LineItems include one expired item
    sub := *s.testData.subscription // shallow copy; replacing LineItems slice is safe
    expiredEndDate := now.Add(-1 * time.Hour)
    expiredItem := &subscription.SubscriptionLineItem{
        ID:             "li_fixed_expired",
        SubscriptionID: sub.ID,
        PriceID:        s.testData.prices.fixed.ID,
        PriceType:      types.PRICE_TYPE_FIXED,
        StartDate:      now.Add(-60 * 24 * time.Hour),
        EndDate:        expiredEndDate,
        Status:         types.StatusPublished,
        Quantity:       decimal.NewFromInt(1),
        BillingPeriod:  sub.BillingPeriod,
        InvoiceCadence: types.InvoiceCadenceAdvance,
    }
    sub.LineItems = []*subscription.SubscriptionLineItem{expiredItem}

    lineItems, total, err := s.service.CalculateFixedCharges(s.GetContext(), &sub, periodStart, periodEnd)
    s.Require().NoError(err)
    s.Empty(lineItems, "expired line item should not produce any charges")
    s.True(total.IsZero(), "total should be zero for expired line items")
}
```

- [ ] **Step 6: Run the test and verify it fails**

```bash
go test ./internal/service/... -run TestBillingService/TestCalculateFixedCharges_SkipsExpiredLineItems -v
```

Expected: FAIL — the expired item generates charges

- [ ] **Step 7: Add the EndDate guard to CalculateFixedCharges**

In `internal/service/billing.go`, inside `CalculateFixedCharges`, add the guard immediately after the closing `continue` of the `item.StartDate.After(periodEnd)` block (after line 176). The current code around lines 168–176:

```go
// skip if the line item start date is after the period end
if item.StartDate.After(periodEnd) {
    s.Logger.Debugw("skipping fixed charge line item because it starts after the period end",
        "subscription_id", sub.ID,
        "line_item_id", item.ID,
        "price_id", item.PriceID,
        "start_date", item.StartDate,
        "period_end", periodEnd)
    continue
}
```

Add immediately after this block:

```go
// Skip line items that ended before the billing period started
if !item.EndDate.IsZero() && !item.EndDate.After(periodStart) {
    s.Logger.Debugw("skipping fixed charge line item because it ended before the period start",
        "subscription_id", sub.ID,
        "line_item_id", item.ID,
        "price_id", item.PriceID,
        "end_date", item.EndDate,
        "period_start", periodStart)
    continue
}
```

- [ ] **Step 8: Run both billing tests and verify they pass**

```bash
go test ./internal/service/... -run "TestBillingService/TestFilterLineItemsToBeInvoiced_SkipsExpiredLineItems|TestBillingService/TestCalculateFixedCharges_SkipsExpiredLineItems" -v
```

Expected: both PASS

- [ ] **Step 9: Run the full billing test suite**

```bash
go test ./internal/service/... -run TestBillingService -v 2>&1 | tail -30
```

Expected: all existing tests pass. The pre-filter in `FilterLineItemsToBeInvoiced` is safe for existing tests because all line items in the billing test fixture (`setupTestData`) are created with zero `EndDate` — the pre-filter skips items only when `!item.EndDate.IsZero()`, so zero-EndDate items are always passed through unchanged.

- [ ] **Step 10: Run all service tests**

```bash
go test ./internal/service/... 2>&1 | grep -E "FAIL|ok"
```

Expected: no FAILs

- [ ] **Step 11: Vet and commit**

```bash
go vet ./internal/service/...
git add internal/service/billing.go internal/service/billing_test.go
git commit -m "feat: skip expired line items in billing to prevent ghost charges"
```

---

## Final Verification

- [ ] **Run all service and DTO tests**

```bash
go test ./internal/service/... ./internal/api/dto/... 2>&1 | grep -E "FAIL|ok"
```

Expected: all packages pass, no FAILs

- [ ] **Vet all changed packages**

```bash
go vet ./internal/api/dto/... ./internal/service/...
```

Expected: no issues

- [ ] **Confirm three commits exist**

```bash
git log --oneline -5
```

Expected commits:
1. `feat: allow future start_date on subscription creation`
2. `feat: set EndDate on plan line items when subscription is cancelled`
3. `feat: skip expired line items in billing to prevent ghost charges`
