# Subscription Change Proration Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive tests for PR #1733's plan-change proration rework — verifying that upgrade credits are netted against the opening invoice (not issued as wallet credits), that normal cancellation still tops up the wallet, and that the renamed/extended `IsFirstSubscriptionOpenInvoiceReason` and the new `applyFixedChargeAdjustmentToLineItems` helper behave correctly.

**Architecture:** All tests live in `internal/service/subscription_change_test.go` inside the existing `SubscriptionChangeServiceTestSuite`. Integration tests use a backdated subscription (15 days elapsed of a 30-day period) to get deterministic proration amounts (~$300 credit on a $600/month plan). Pure unit tests call the function or method directly without hitting the DB.

**Tech Stack:** Go 1.23+, testify/suite, shopspring/decimal, in-memory test stores from `testutil`.

---

## File Map

| File | Change |
|---|---|
| `internal/service/subscription_change_test.go` | Add `SubScheduleRepo` to `setupServices()`, uncomment suite runner, add 4 helpers, add 6 test families |

---

### Task 1: Wire `SubScheduleRepo` and uncomment suite runner

The service under test calls `SubScheduleRepo` during period-end scheduling. It is currently missing from the test's `ServiceParams`, which would cause a nil-pointer panic for that code path. Also, the suite runner is commented out — no tests in the file run at all today.

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add `SubScheduleRepo` to `setupServices()` and uncomment runner**

In `setupServices()`, the `ServiceParams` literal is missing one field. Add it immediately after `CouponAssociationRepo`:

```go
// internal/service/subscription_change_test.go  (inside setupServices)

serviceParams := ServiceParams{
    Logger:                     s.GetLogger(),
    Config:                     s.GetConfig(),
    DB:                         s.GetDB(),
    TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
    TaxRateRepo:                s.GetStores().TaxRateRepo,
    AuthRepo:                   s.GetStores().AuthRepo,
    UserRepo:                   s.GetStores().UserRepo,
    EventRepo:                  s.GetStores().EventRepo,
    MeterRepo:                  s.GetStores().MeterRepo,
    PriceRepo:                  s.GetStores().PriceRepo,
    CustomerRepo:               s.GetStores().CustomerRepo,
    PlanRepo:                   s.GetStores().PlanRepo,
    SubRepo:                    s.GetStores().SubscriptionRepo,
    SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo, // ADD THIS LINE
    WalletRepo:                 s.GetStores().WalletRepo,
    TenantRepo:                 s.GetStores().TenantRepo,
    InvoiceRepo:                s.GetStores().InvoiceRepo,
    FeatureRepo:                s.GetStores().FeatureRepo,
    EntitlementRepo:            s.GetStores().EntitlementRepo,
    PaymentRepo:                s.GetStores().PaymentRepo,
    SecretRepo:                 s.GetStores().SecretRepo,
    EnvironmentRepo:            s.GetStores().EnvironmentRepo,
    TaskRepo:                   s.GetStores().TaskRepo,
    CreditGrantRepo:            s.GetStores().CreditGrantRepo,
    CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
    CouponRepo:                 s.GetStores().CouponRepo,
    CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
    CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
    AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
    TaxAppliedRepo:             s.GetStores().TaxAppliedRepo,
    CreditNoteRepo:             s.GetStores().CreditNoteRepo,
    CreditNoteLineItemRepo:     s.GetStores().CreditNoteLineItemRepo,
    ConnectionRepo:             s.GetStores().ConnectionRepo,
    SettingsRepo:               s.GetStores().SettingsRepo,
    EventPublisher:             s.GetPublisher(),
    WebhookPublisher:           s.GetWebhookPublisher(),
    ProrationCalculator:        s.GetCalculator(),
}
```

At the very bottom of the file, replace the commented-out runner block:

```go
// BEFORE (commented out):
// func TestSubscriptionChangeServiceTestSuite(t *testing.T) {
//     suite.Run(t, new(SubscriptionChangeServiceTestSuite))
// }

// AFTER (uncommented):
func TestSubscriptionChangeServiceTestSuite(t *testing.T) {
    suite.Run(t, new(SubscriptionChangeServiceTestSuite))
}
```

Add `"github.com/stretchr/testify/suite"` to the import block at the top of the file (it is not currently imported):

```go
import (
    "strings"
    "time"

    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/api/dto"
    "github.com/flexprice/flexprice/internal/domain/customer"
    invoicedomain "github.com/flexprice/flexprice/internal/domain/invoice"
    "github.com/flexprice/flexprice/internal/domain/meter"
    "github.com/flexprice/flexprice/internal/domain/plan"
    "github.com/flexprice/flexprice/internal/domain/price"
    "github.com/flexprice/flexprice/internal/domain/subscription"
    walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
    "github.com/flexprice/flexprice/internal/testutil"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/stretchr/testify/suite"
)
```

- [ ] **Step 2: Run existing tests to confirm they still compile and pass**

```bash
go test -v -race -count=1 ./internal/service -run TestSubscriptionChangeServiceTestSuite -timeout 120s 2>&1 | tail -30
```

Expected: all previously-defined test methods pass (or skip). No compile error. The `_ = ierr.IsValidation` line confirms `ierr` import is still used.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): wire SubScheduleRepo, uncomment suite runner, add imports"
```

---

### Task 2: Add shared test helpers

Four helpers used by multiple test families. They all live as methods on `SubscriptionChangeServiceTestSuite` (except `assertAmountNear` which is also a method). Add them after the existing `createMultiMeterUsagePlan` helper and before the `TestPreviewSubscriptionUpgrade` test.

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the four helper methods**

Paste the following block into the file after `createMultiMeterUsagePlan`:

```go
// backdateSub pins CurrentPeriodStart/CurrentPeriodEnd on the subscription so that
// daysUsed days have already elapsed out of a totalDays-long billing period.
// This gives deterministic proration amounts regardless of when the test runs.
// Returns the refreshed subscription.
func (s *SubscriptionChangeServiceTestSuite) backdateSub(
    sub *subscription.Subscription,
    daysUsed, totalDays int,
) *subscription.Subscription {
    ctx := s.GetContext()
    now := time.Now().UTC()
    sub.CurrentPeriodStart = now.AddDate(0, 0, -daysUsed)
    sub.CurrentPeriodEnd = now.AddDate(0, 0, totalDays-daysUsed)
    require.NoError(s.T(), s.GetStores().SubscriptionRepo.Update(ctx, sub))
    refreshed, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, sub.ID)
    require.NoError(s.T(), err)
    return refreshed
}

// getInvoicesForSub lists all invoices (any status) for the given subscription ID.
// Results are returned in repository order (typically insertion order).
// The opening invoice is always [0] because it is created first.
func (s *SubscriptionChangeServiceTestSuite) getInvoicesForSub(subID string) []*invoicedomain.Invoice {
    ctx := s.GetContext()
    filter := &types.InvoiceFilter{
        QueryFilter:    types.NewDefaultQueryFilter(),
        SubscriptionID: subID,
    }
    invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
    require.NoError(s.T(), err)
    return invoices
}

// getWalletForCustomer returns the first wallet whose CustomerID matches,
// or nil if no wallet exists yet. Tests run with an isolated in-memory store
// so the only wallets present belong to the current test's customer(s).
func (s *SubscriptionChangeServiceTestSuite) getWalletForCustomer(customerID string) *walletdomain.Wallet {
    ctx := s.GetContext()
    wallets, err := s.GetStores().WalletRepo.GetWalletsByFilter(ctx, &types.WalletFilter{
        QueryFilter: types.NewDefaultQueryFilter(),
    })
    require.NoError(s.T(), err)
    for _, w := range wallets {
        if w.CustomerID == customerID {
            return w
        }
    }
    return nil
}

// assertAmountNear fails the test if |actual - expected| >= tol.
// Use for proration amounts where wall-clock timing introduces sub-cent variance.
func (s *SubscriptionChangeServiceTestSuite) assertAmountNear(expected, actual decimal.Decimal, tol float64, msg string) {
    s.T().Helper()
    diff := actual.Sub(expected).Abs()
    tolDec := decimal.NewFromFloat(tol)
    assert.True(s.T(), diff.LessThan(tolDec),
        "%s: expected %s ≈ %s (tol=%s), got diff=%s", msg, expected, actual, tolDec, diff)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/service/... 2>&1
```

Expected: no output (no compile errors).

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add backdateSub, getInvoicesForSub, getWalletForCustomer, assertAmountNear helpers"
```

---

### Task 3: Family 5 — `TestIsFirstSubscriptionOpenInvoiceReason`

Pure unit test — no DB access. Verifies the renamed method, particularly that `SUBSCRIPTION_UPDATE` now returns `true` (the key new behaviour in this PR).

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the test method**

Add after `TestGenerateWarningsHelper`:

```go
// TestIsFirstSubscriptionOpenInvoiceReason verifies that paying a SUBSCRIPTION_UPDATE
// invoice triggers subscription activation logic (new in PR #1733) along with the
// original SUBSCRIPTION_CREATE and SUBSCRIPTION_TRIAL_END reasons.
func (s *SubscriptionChangeServiceTestSuite) TestIsFirstSubscriptionOpenInvoiceReason() {
    cases := []struct {
        reason   types.InvoiceBillingReason
        wantTrue bool
    }{
        {types.InvoiceBillingReasonSubscriptionCreate, true},
        {types.InvoiceBillingReasonSubscriptionTrialEnd, true},
        {types.InvoiceBillingReasonSubscriptionUpdate, true}, // added in PR #1733
        {types.InvoiceBillingReasonSubscriptionCycle, false},
        {types.InvoiceBillingReasonProration, false},
        {types.InvoiceBillingReasonManual, false},
    }
    for _, tc := range cases {
        tc := tc
        s.Run(string(tc.reason), func() {
            got := tc.reason.IsFirstSubscriptionOpenInvoiceReason()
            assert.Equal(s.T(), tc.wantTrue, got,
                "IsFirstSubscriptionOpenInvoiceReason() for reason %q", tc.reason)
        })
    }
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 ./internal/service -run "TestSubscriptionChangeServiceTestSuite/TestIsFirstSubscriptionOpenInvoiceReason" -timeout 60s
```

Expected output contains six `--- PASS` lines (one per sub-test).

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add TestIsFirstSubscriptionOpenInvoiceReason unit tests"
```

---

### Task 4: Family 6 — `TestApplyFixedChargeAdjustmentToLineItems`

Pure unit test. Calls the package-level function `applyFixedChargeAdjustmentToLineItems` (defined in `billing.go`, same `service` package) directly — no service instantiation needed.

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the test method**

```go
// TestApplyFixedChargeAdjustmentToLineItems verifies the billing helper that
// distributes a proration credit across invoice line items in order.
func (s *SubscriptionChangeServiceTestSuite) TestApplyFixedChargeAdjustmentToLineItems() {
    mkItem := func(amount float64) dto.CreateInvoiceLineItemRequest {
        return dto.CreateInvoiceLineItemRequest{Amount: decimal.NewFromFloat(amount)}
    }

    cases := []struct {
        name     string
        credit   decimal.Decimal
        items    []dto.CreateInvoiceLineItemRequest
        wantAmts []float64 // expected Amount per output item
    }{
        {
            name:     "credit_smaller_than_single_item",
            credit:   decimal.NewFromFloat(300),
            items:    []dto.CreateInvoiceLineItemRequest{mkItem(2000)},
            wantAmts: []float64{1700},
        },
        {
            name:     "credit_spans_two_items_exhausts_first",
            credit:   decimal.NewFromFloat(300),
            items:    []dto.CreateInvoiceLineItemRequest{mkItem(200), mkItem(200)},
            wantAmts: []float64{0, 100},
        },
        {
            name:     "credit_equals_total",
            credit:   decimal.NewFromFloat(400),
            items:    []dto.CreateInvoiceLineItemRequest{mkItem(200), mkItem(200)},
            wantAmts: []float64{0, 0},
        },
        {
            name:     "credit_exceeds_total_capped_at_zero",
            credit:   decimal.NewFromFloat(500),
            items:    []dto.CreateInvoiceLineItemRequest{mkItem(200), mkItem(200)},
            wantAmts: []float64{0, 0},
        },
        {
            name:     "zero_credit_leaves_items_unchanged",
            credit:   decimal.Zero,
            items:    []dto.CreateInvoiceLineItemRequest{mkItem(200)},
            wantAmts: []float64{200},
        },
        {
            name:     "empty_items_returns_empty",
            credit:   decimal.NewFromFloat(300),
            items:    []dto.CreateInvoiceLineItemRequest{},
            wantAmts: []float64{},
        },
    }

    for _, tc := range cases {
        tc := tc
        s.Run(tc.name, func() {
            result := applyFixedChargeAdjustmentToLineItems(tc.items, tc.credit)
            require.Len(s.T(), result, len(tc.wantAmts),
                "result length mismatch for case %q", tc.name)
            for i, wantF := range tc.wantAmts {
                want := decimal.NewFromFloat(wantF)
                assert.True(s.T(), result[i].Amount.Equal(want),
                    "item[%d] amount: want %s got %s", i, want, result[i].Amount)
                assert.False(s.T(), result[i].Amount.IsNegative(),
                    "item[%d] must not be negative", i)
            }
        })
    }
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -count=1 ./internal/service -run "TestSubscriptionChangeServiceTestSuite/TestApplyFixedChargeAdjustmentToLineItems" -timeout 60s
```

Expected: six `--- PASS` sub-tests.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add TestApplyFixedChargeAdjustmentToLineItems unit tests"
```

---

### Task 5: Family 1 — `TestUpgradeWithCreateProrations`

Integration test. $600/month → $2000/month, immediate, `create_prorations`. Subscription backdated 15 days into a 30-day period → expected credit ≈ $300, adjusted opening invoice ≈ $1700.

The test calls `PreviewSubscriptionChange` once, stores the response, runs two preview subtests, then calls `ExecuteSubscriptionChange` once, stores the response, and runs five execute subtests.

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the test method**

```go
// TestUpgradeWithCreateProrations verifies the full plan-change proration flow
// introduced in PR #1733: credit from the cancelled subscription is netted
// against the new subscription's opening invoice instead of being issued as
// a wallet credit.
//
// Setup: $600/month plan → $2000/month plan, 15 days elapsed of 30-day period.
// Expected credit ≈ $300 ($600 * 15/30).
// Expected opening invoice ≈ $1700 ($2000 - $300).
func (s *SubscriptionChangeServiceTestSuite) TestUpgradeWithCreateProrations() {
    ctx := s.GetContext()

    cust := s.createTestCustomer()
    plan600 := s.createTestPlan("Starter-600", decimal.NewFromFloat(600))
    plan2000 := s.createTestPlan("Pro-2000", decimal.NewFromFloat(2000))
    sub := s.createTestSubscription(plan600.ID, cust.ID)
    sub = s.backdateSub(sub, 15, 30) // 15 days elapsed of 30-day period

    expectedCredit := decimal.NewFromFloat(300)
    expectedNetInvoice := decimal.NewFromFloat(1700)
    const tol = 1.0 // $1 tolerance for wall-clock sub-second variance

    immediateAt := types.ScheduleTypeImmediate
    req := dto.SubscriptionChangeRequest{
        TargetPlanID:       plan2000.ID,
        ProrationBehavior:  types.ProrationBehaviorCreateProrations,
        BillingCadence:     types.BILLING_CADENCE_RECURRING,
        BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
        BillingPeriodCount: 1,
        BillingCycle:       types.BillingCycleAnniversary,
        ChangeAt:           &immediateAt,
    }

    // ── Preview ──────────────────────────────────────────────────────────────
    preview, err := s.subscriptionChangeService.PreviewSubscriptionChange(ctx, sub.ID, req)
    require.NoError(s.T(), err, "PreviewSubscriptionChange must not error")
    require.NotNil(s.T(), preview)

    s.Run("preview/shows_credit_in_proration_details", func() {
        require.NotNil(s.T(), preview.ProrationDetails,
            "proration details must be present when create_prorations is set")
        s.assertAmountNear(expectedCredit, preview.ProrationDetails.CreditAmount, tol,
            "ProrationDetails.CreditAmount")
    })

    s.Run("preview/next_invoice_total_is_netted", func() {
        require.NotNil(s.T(), preview.NextInvoicePreview,
            "next invoice preview must be present")
        s.assertAmountNear(expectedNetInvoice, preview.NextInvoicePreview.Total, tol,
            "NextInvoicePreview.Total")
        // Line items must sum to Subtotal and none may be negative.
        lineSum := decimal.Zero
        for i, li := range preview.NextInvoicePreview.LineItems {
            assert.False(s.T(), li.Amount.IsNegative(),
                "line item[%d] amount must not be negative, got %s", i, li.Amount)
            lineSum = lineSum.Add(li.Amount)
        }
        assert.True(s.T(), lineSum.Equal(preview.NextInvoicePreview.Subtotal),
            "line items sum %s must equal Subtotal %s", lineSum, preview.NextInvoicePreview.Subtotal)
    })

    // ── Execute ───────────────────────────────────────────────────────────────
    execResp, execErr := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, sub.ID, req)
    require.NoError(s.T(), execErr, "ExecuteSubscriptionChange must not error")
    require.NotNil(s.T(), execResp)

    s.Run("execute/old_sub_cancelled", func() {
        oldSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
        require.NoError(s.T(), err)
        assert.Equal(s.T(), types.SubscriptionStatusCancelled, oldSub.SubscriptionStatus,
            "old subscription must be cancelled")
        assert.NotNil(s.T(), oldSub.CancelledAt,
            "old subscription CancelledAt must be set")
    })

    s.Run("execute/new_sub_active_on_target_plan", func() {
        newSub, err := s.GetStores().SubscriptionRepo.Get(ctx, execResp.NewSubscription.ID)
        require.NoError(s.T(), err)
        assert.Equal(s.T(), types.SubscriptionStatusActive, newSub.SubscriptionStatus,
            "new subscription must be active")
        assert.Equal(s.T(), plan2000.ID, newSub.PlanID,
            "new subscription must be on the target plan")
    })

    s.Run("execute/opening_invoice_billing_reason", func() {
        invoices := s.getInvoicesForSub(execResp.NewSubscription.ID)
        require.NotEmpty(s.T(), invoices,
            "new subscription must have at least one invoice")
        opening := invoices[0]
        assert.Equal(s.T(), string(types.InvoiceBillingReasonSubscriptionUpdate), opening.BillingReason,
            "opening invoice billing reason must be SUBSCRIPTION_UPDATE")
    })

    s.Run("execute/opening_invoice_amount_netted", func() {
        invoices := s.getInvoicesForSub(execResp.NewSubscription.ID)
        require.NotEmpty(s.T(), invoices)
        opening := invoices[0]
        s.assertAmountNear(expectedNetInvoice, opening.AmountDue, tol,
            "opening invoice AmountDue")
    })

    s.Run("execute/no_wallet_credit_created", func() {
        w := s.getWalletForCustomer(cust.ID)
        if w != nil {
            assert.True(s.T(), w.Balance.IsZero(),
                "wallet balance must be zero — proration credit is netted on invoice, not issued to wallet; got balance=%s",
                w.Balance)
        }
        // nil wallet is also correct (no wallet created at all).
    })
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -race -count=1 ./internal/service -run "TestSubscriptionChangeServiceTestSuite/TestUpgradeWithCreateProrations" -timeout 120s
```

Expected: seven `--- PASS` entries (two preview + five execute subtests).

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add TestUpgradeWithCreateProrations integration test"
```

---

### Task 6: Family 2 — `TestCancelWithCreateProrations`

Integration test. Normal cancellation (no plan change) with `create_prorations`. The wallet top-up path is active here — `SkipProrationWalletCredit` is `false` (default).

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the test method**

```go
// TestCancelWithCreateProrations verifies that a normal subscription cancellation
// (not a plan change) with create_prorations issues a wallet credit for the
// unused period. This exercises the path where SkipProrationWalletCredit is false
// (the default), confirming PR #1733 did not break standard cancellation.
//
// Setup: $600/month plan, 15 days elapsed of 30-day period.
// Expected wallet credit ≈ $300 ($600 * 15/30).
func (s *SubscriptionChangeServiceTestSuite) TestCancelWithCreateProrations() {
    ctx := s.GetContext()

    cust := s.createTestCustomer()
    plan600 := s.createTestPlan("Starter-600-cancel", decimal.NewFromFloat(600))
    sub := s.createTestSubscription(plan600.ID, cust.ID)
    sub = s.backdateSub(sub, 15, 30)

    expectedCredit := decimal.NewFromFloat(300)
    const tol = 1.0

    cancelResp, err := s.subscriptionService.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
        CancellationType:  types.CancellationTypeImmediate,
        ProrationBehavior: types.ProrationBehaviorCreateProrations,
    })
    require.NoError(s.T(), err, "CancelSubscription must not error")
    require.NotNil(s.T(), cancelResp)

    s.Run("cancel/response_total_credit_amount", func() {
        s.assertAmountNear(expectedCredit, cancelResp.TotalCreditAmount, tol,
            "CancelSubscriptionResponse.TotalCreditAmount")
    })

    s.Run("cancel/wallet_exists_for_customer", func() {
        w := s.getWalletForCustomer(cust.ID)
        require.NotNil(s.T(), w,
            "a wallet must be created for the customer when proration credit is issued")
    })

    s.Run("cancel/wallet_balance_matches_credit", func() {
        w := s.getWalletForCustomer(cust.ID)
        require.NotNil(s.T(), w)
        s.assertAmountNear(expectedCredit, w.Balance, tol,
            "wallet Balance after cancellation proration")
    })

    s.Run("cancel/subscription_is_cancelled", func() {
        updatedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
        require.NoError(s.T(), err)
        assert.Equal(s.T(), types.SubscriptionStatusCancelled, updatedSub.SubscriptionStatus,
            "subscription must be cancelled after CancelSubscription")
    })
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -race -count=1 ./internal/service -run "TestSubscriptionChangeServiceTestSuite/TestCancelWithCreateProrations" -timeout 120s
```

Expected: four `--- PASS` sub-tests.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add TestCancelWithCreateProrations integration test"
```

---

### Task 7: Family 3 — `TestUpgradeNoneProration`

Integration test. $600 → $2000 with `proration_behavior = none`. Confirms that no credit is applied, the opening invoice is the full $2000, and the wallet is empty.

No backdate needed because with `none` proration no credit is calculated regardless of elapsed time.

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the test method**

```go
// TestUpgradeNoneProration verifies that when proration_behavior is "none"
// the opening invoice for the new subscription is the full plan price with
// no adjustment, and no wallet credit is issued.
func (s *SubscriptionChangeServiceTestSuite) TestUpgradeNoneProration() {
    ctx := s.GetContext()

    cust := s.createTestCustomer()
    plan600 := s.createTestPlan("Starter-600-nopro", decimal.NewFromFloat(600))
    plan2000 := s.createTestPlan("Pro-2000-nopro", decimal.NewFromFloat(2000))
    sub := s.createTestSubscription(plan600.ID, cust.ID)
    // No backdate — timing is irrelevant for none proration.

    req := dto.SubscriptionChangeRequest{
        TargetPlanID:       plan2000.ID,
        ProrationBehavior:  types.ProrationBehaviorNone,
        BillingCadence:     types.BILLING_CADENCE_RECURRING,
        BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
        BillingPeriodCount: 1,
        BillingCycle:       types.BillingCycleAnniversary,
    }

    execResp, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, sub.ID, req)
    require.NoError(s.T(), err, "ExecuteSubscriptionChange must not error")
    require.NotNil(s.T(), execResp)

    s.Run("execute/new_sub_active_on_target_plan", func() {
        newSub, err := s.GetStores().SubscriptionRepo.Get(ctx, execResp.NewSubscription.ID)
        require.NoError(s.T(), err)
        assert.Equal(s.T(), types.SubscriptionStatusActive, newSub.SubscriptionStatus)
        assert.Equal(s.T(), plan2000.ID, newSub.PlanID)
    })

    s.Run("execute/opening_invoice_is_full_price", func() {
        invoices := s.getInvoicesForSub(execResp.NewSubscription.ID)
        require.NotEmpty(s.T(), invoices, "new subscription must have an invoice")
        opening := invoices[0]
        expected := decimal.NewFromFloat(2000)
        assert.True(s.T(), opening.AmountDue.Equal(expected),
            "opening invoice must be full $2000 with none proration; got %s", opening.AmountDue)
    })

    s.Run("execute/no_proration_applied_in_response", func() {
        assert.Nil(s.T(), execResp.ProrationApplied,
            "ProrationApplied must be nil when proration_behavior=none")
    })

    s.Run("execute/no_wallet_credit", func() {
        w := s.getWalletForCustomer(cust.ID)
        if w != nil {
            assert.True(s.T(), w.Balance.IsZero(),
                "wallet balance must be zero for none proration; got %s", w.Balance)
        }
    })
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -race -count=1 ./internal/service -run "TestSubscriptionChangeServiceTestSuite/TestUpgradeNoneProration" -timeout 120s
```

Expected: four `--- PASS` sub-tests.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add TestUpgradeNoneProration integration test"
```

---

### Task 8: Family 4 — `TestUpgradeScheduledPeriodEnd`

Integration test. Verifies the period-end scheduling path: change is recorded in `SubScheduleRepo`, old subscription stays active, no new subscription is created immediately.

**Files:**
- Modify: `internal/service/subscription_change_test.go`

- [ ] **Step 1: Add the test method**

```go
// TestUpgradeScheduledPeriodEnd verifies that when change_at=period_end the
// plan change is scheduled (not immediately executed): the old subscription
// stays active and no new subscription is created until the schedule fires.
func (s *SubscriptionChangeServiceTestSuite) TestUpgradeScheduledPeriodEnd() {
    ctx := s.GetContext()

    cust := s.createTestCustomer()
    plan600 := s.createTestPlan("Starter-600-sched", decimal.NewFromFloat(600))
    plan2000 := s.createTestPlan("Pro-2000-sched", decimal.NewFromFloat(2000))
    sub := s.createTestSubscription(plan600.ID, cust.ID)

    periodEnd := types.ScheduleTypePeriodEnd
    req := dto.SubscriptionChangeRequest{
        TargetPlanID:       plan2000.ID,
        ProrationBehavior:  types.ProrationBehaviorCreateProrations,
        BillingCadence:     types.BILLING_CADENCE_RECURRING,
        BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
        BillingPeriodCount: 1,
        BillingCycle:       types.BillingCycleAnniversary,
        ChangeAt:           &periodEnd,
    }

    schedResp, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, sub.ID, req)
    require.NoError(s.T(), err, "ExecuteSubscriptionChange (period_end) must not error")
    require.NotNil(s.T(), schedResp)

    s.Run("scheduled/response_is_scheduled", func() {
        assert.True(s.T(), schedResp.IsScheduled,
            "response.IsScheduled must be true for period_end change")
        require.NotNil(s.T(), schedResp.ScheduleID,
            "response.ScheduleID must be set")
        assert.NotEmpty(s.T(), *schedResp.ScheduleID,
            "ScheduleID must be a non-empty string")
    })

    s.Run("scheduled/old_sub_still_active", func() {
        currentSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
        require.NoError(s.T(), err)
        assert.Equal(s.T(), types.SubscriptionStatusActive, currentSub.SubscriptionStatus,
            "original subscription must stay active after period_end scheduling")
    })

    s.Run("scheduled/new_sub_id_is_empty", func() {
        // When change is scheduled, NewSubscription.ID is the zero value (not yet created).
        assert.Empty(s.T(), execResp.NewSubscription.ID,
            "NewSubscription.ID must be empty — new sub is not created until schedule fires")
    })
}
```

Wait — `execResp` is not defined inside the `s.Run` closure. Fix: use `schedResp`:

```go
    s.Run("scheduled/new_sub_id_is_empty", func() {
        // When change is scheduled, NewSubscription.ID is the zero value (not yet created).
        assert.Empty(s.T(), schedResp.NewSubscription.ID,
            "NewSubscription.ID must be empty — new sub is not created until schedule fires")
    })
```

The full correct method:

```go
func (s *SubscriptionChangeServiceTestSuite) TestUpgradeScheduledPeriodEnd() {
    ctx := s.GetContext()

    cust := s.createTestCustomer()
    plan600 := s.createTestPlan("Starter-600-sched", decimal.NewFromFloat(600))
    plan2000 := s.createTestPlan("Pro-2000-sched", decimal.NewFromFloat(2000))
    sub := s.createTestSubscription(plan600.ID, cust.ID)

    _ = cust // used in subtests implicitly via closure

    periodEnd := types.ScheduleTypePeriodEnd
    req := dto.SubscriptionChangeRequest{
        TargetPlanID:       plan2000.ID,
        ProrationBehavior:  types.ProrationBehaviorCreateProrations,
        BillingCadence:     types.BILLING_CADENCE_RECURRING,
        BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
        BillingPeriodCount: 1,
        BillingCycle:       types.BillingCycleAnniversary,
        ChangeAt:           &periodEnd,
    }

    schedResp, err := s.subscriptionChangeService.ExecuteSubscriptionChange(ctx, sub.ID, req)
    require.NoError(s.T(), err, "ExecuteSubscriptionChange (period_end) must not error")
    require.NotNil(s.T(), schedResp)

    s.Run("scheduled/response_is_scheduled", func() {
        assert.True(s.T(), schedResp.IsScheduled,
            "response.IsScheduled must be true for period_end change")
        require.NotNil(s.T(), schedResp.ScheduleID,
            "response.ScheduleID must be set")
        assert.NotEmpty(s.T(), *schedResp.ScheduleID,
            "ScheduleID must be a non-empty string")
    })

    s.Run("scheduled/old_sub_still_active", func() {
        currentSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
        require.NoError(s.T(), err)
        assert.Equal(s.T(), types.SubscriptionStatusActive, currentSub.SubscriptionStatus,
            "original subscription must stay active after period_end scheduling")
    })

    s.Run("scheduled/new_sub_id_is_empty", func() {
        // When change is scheduled, NewSubscription is zero-valued (new sub not yet created).
        assert.Empty(s.T(), schedResp.NewSubscription.ID,
            "NewSubscription.ID must be empty — new sub is not created until the schedule fires")
    })
}
```

- [ ] **Step 2: Run the test**

```bash
go test -v -race -count=1 ./internal/service -run "TestSubscriptionChangeServiceTestSuite/TestUpgradeScheduledPeriodEnd" -timeout 120s
```

Expected: three `--- PASS` sub-tests.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): add TestUpgradeScheduledPeriodEnd integration test"
```

---

### Task 9: Full suite verification and vet

- [ ] **Step 1: Run the entire suite**

```bash
go test -v -race -count=1 ./internal/service -run TestSubscriptionChangeServiceTestSuite -timeout 300s 2>&1 | grep -E "^(=== RUN|--- PASS|--- FAIL|FAIL|ok)"
```

Expected: every test method and sub-test reports `--- PASS`. No `--- FAIL` lines.

- [ ] **Step 2: Run go vet**

```bash
go vet ./internal/service/...
```

Expected: no output (no issues).

- [ ] **Step 3: Final commit**

```bash
git add internal/service/subscription_change_test.go
git commit -m "test(sub-change): final cleanup after full suite run"
```

Only needed if Step 1 required any fixes. If everything passed cleanly after Task 8, skip this commit.

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task that covers it |
|---|---|
| Suite runner uncommented | Task 1 |
| SubScheduleRepo wired | Task 1 |
| `backdateSub` helper | Task 2 |
| `getInvoicesForSub` helper | Task 2 |
| `getWalletForCustomer` helper | Task 2 |
| `assertAmountNear` helper | Task 2 |
| `IsFirstSubscriptionOpenInvoiceReason` unit test (6 cases) | Task 3 |
| `applyFixedChargeAdjustmentToLineItems` unit test (6 cases) | Task 4 |
| Upgrade preview shows credit in proration details | Task 5 |
| Upgrade preview next invoice total is netted | Task 5 |
| Upgrade execute — old sub cancelled | Task 5 |
| Upgrade execute — new sub active on target plan | Task 5 |
| Upgrade execute — opening invoice billing reason = SUBSCRIPTION_UPDATE | Task 5 |
| Upgrade execute — opening invoice amount netted | Task 5 |
| Upgrade execute — no wallet credit created | Task 5 |
| Cancel — response TotalCreditAmount ≈ $300 | Task 6 |
| Cancel — wallet exists for customer | Task 6 |
| Cancel — wallet balance ≈ $300 | Task 6 |
| Cancel — sub is cancelled | Task 6 |
| None proration — new sub active | Task 7 |
| None proration — opening invoice full price | Task 7 |
| None proration — no ProrationApplied | Task 7 |
| None proration — no wallet credit | Task 7 |
| Period-end — response.IsScheduled true | Task 8 |
| Period-end — old sub still active | Task 8 |
| Period-end — no new sub created | Task 8 |

All spec requirements covered. ✅

**Type consistency:**
- `types.InvoiceBillingReasonSubscriptionUpdate` — defined in `internal/types/invoice.go`, used in Tasks 3 and 5. ✅
- `applyFixedChargeAdjustmentToLineItems` — package-level function in `internal/service/billing.go`, called from same package in Task 4. ✅
- `invoice.AmountDue` (`decimal.Decimal`) — confirmed from `internal/domain/invoice/model.go` line 42. ✅
- `invoice.BillingReason` (`string`) — confirmed from `internal/domain/invoice/model.go` line 99. ✅
- `wallet.Balance` (`decimal.Decimal`) — confirmed from `line_item_proration_test.go` usage. ✅
- `InvoiceFilter.SubscriptionID` (`string`, singular) — confirmed from `internal/types/invoice.go` line 347. ✅
- `WalletFilter.QueryFilter` — confirmed from `internal/types/wallet.go` line 334. ✅
- `SubScheduleRepo` field on `ServiceParams` — confirmed from `internal/service/factory.go` line 82. ✅
- `s.GetStores().SubscriptionScheduleRepo` — confirmed from `internal/service/subscription_test.go` line 260. ✅
