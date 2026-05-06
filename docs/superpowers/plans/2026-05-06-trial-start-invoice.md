# Trial-Start Invoice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a `$0` invoice at trial subscription start (billing_reason=`SUBSCRIPTION_TRIAL_START`), auto-paid and fully finalized with invoice number, matching Stripe/Paddle/Orb behavior.

**Architecture:** Add the new billing reason constant, bypass the zero-amount skip in `ComputeInvoice` for this reason, add an early-return guard in `HandlePaymentBehavior` so the invoice is paid but the subscription stays `TRIALING`, then wire up `createTrialStartInvoice` in `subscription_trial.go` and call it from `subscription.go`.

**Tech Stack:** Go 1.23, Ent ORM, Gin, existing service/repository layer.

---

## File Map

| File | Change |
|------|--------|
| `internal/types/invoice.go` | Add `InvoiceBillingReasonSubscriptionTrialStart` constant + Validate() entry |
| `internal/service/invoice.go` | Modify zero-amount skip guard in `ComputeInvoice` (~line 472) |
| `internal/service/subscription_payment_processor.go` | Add early path in `HandlePaymentBehavior` (~line 66) |
| `internal/service/subscription_trial.go` | Add `createTrialStartInvoice` function |
| `internal/service/subscription.go` | Add call to `createTrialStartInvoice` for `TRIALING` subs (~line 440) |
| `internal/service/subscription_trial_test.go` (new) | Integration tests for trial-start invoice |

---

## Task 1: Add `SUBSCRIPTION_TRIAL_START` billing reason type

**Files:**
- Modify: `internal/types/invoice.go:163-210`

- [ ] **Step 1: Add the constant**

In `internal/types/invoice.go`, after the `InvoiceBillingReasonSubscriptionTrialEnd` line (~line 174), add:

```go
// InvoiceBillingReasonSubscriptionTrialStart indicates the $0 invoice created when a trialing subscription starts
InvoiceBillingReasonSubscriptionTrialStart InvoiceBillingReason = "SUBSCRIPTION_TRIAL_START"
```

- [ ] **Step 2: Add to Validate() allowed list**

In the same file, in `func (r InvoiceBillingReason) Validate()`, add `InvoiceBillingReasonSubscriptionTrialStart` to the `allowed` slice. The slice currently ends with `InvoiceBillingReasonManual`:

```go
allowed := []InvoiceBillingReason{
    InvoiceBillingReasonSubscriptionCreate,
    InvoiceBillingReasonSubscriptionCycle,
    InvoiceBillingReasonSubscriptionUpdate,
    InvoiceBillingReasonSubscriptionTrialEnd,
    InvoiceBillingReasonSubscriptionTrialStart,
    InvoiceBillingReasonProration,
    InvoiceBillingReasonManual,
}
```

**Do NOT** add `InvoiceBillingReasonSubscriptionTrialStart` to `TriggersSubscriptionActivationOnFullPayment()` — the subscription must stay `TRIALING` after this invoice is paid.

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/silly-elgamal-f77bf3
go build ./internal/types/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/types/invoice.go
git commit -m "feat(billing): add SUBSCRIPTION_TRIAL_START billing reason"
```

---

## Task 2: Bypass zero-amount skip in `ComputeInvoice` for trial-start

**Files:**
- Modify: `internal/service/invoice.go` (~line 472)

Context: `ComputeInvoice` currently marks every `$0` subscription invoice as `SKIPPED` and returns `skipped=true`. This prevents `ProcessDraftInvoice` (and therefore `FinalizeInvoice`) from running. We need the trial-start invoice to be finalized with an invoice number, so we bypass the skip for this billing reason.

- [ ] **Step 1: Locate the zero-amount guard**

Search for this block in `internal/service/invoice.go`:

```go
if inv.InvoiceType == types.InvoiceTypeSubscription && inv.Subtotal.IsZero() {
    now := time.Now().UTC()
    inv.LastComputedAt = &now
    inv.InvoiceStatus = types.InvoiceStatusSkipped
```

- [ ] **Step 2: Add the `isTrialStart` bypass**

Replace only the `if` condition line — add `&& !isTrialStart` — and add the `isTrialStart` variable declaration immediately before the block. Note: domain `Invoice.BillingReason` is `string`, so a type cast is required for the comparison:

```go
isTrialStart := types.InvoiceBillingReason(inv.BillingReason) == types.InvoiceBillingReasonSubscriptionTrialStart
if inv.InvoiceType == types.InvoiceTypeSubscription && inv.Subtotal.IsZero() && !isTrialStart {
    now := time.Now().UTC()
    inv.LastComputedAt = &now
    inv.InvoiceStatus = types.InvoiceStatusSkipped
    if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
        return err
    }
    skipped = true
    return nil
}
```

Everything inside the block is unchanged. Only the outer `if` condition changes.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/service/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/service/invoice.go
git commit -m "feat(billing): bypass zero-amount skip for SUBSCRIPTION_TRIAL_START invoices"
```

---

## Task 3: Guard `HandlePaymentBehavior` for trial-start invoices

**Files:**
- Modify: `internal/service/subscription_payment_processor.go:51`

Context: `HandlePaymentBehavior` processes payment and updates subscription status. For trial-start invoices, we want to process the `$0` payment (marks invoice as `paid`) but must NOT update subscription status — the subscription must remain `TRIALING`. Without this guard, `allow_incomplete + $0 success` would flip the subscription to `ACTIVE`.

- [ ] **Step 1: Add the early-return guard**

In `internal/service/subscription_payment_processor.go`, at the very top of `HandlePaymentBehavior` (after the opening log line, before the `flowType == InvoiceFlowManual` check, approximately line 66), add:

```go
// Trial-start invoices are always $0. Process payment (marks invoice paid) but do NOT
// update subscription status — subscription must stay TRIALING until trial ends.
if inv.BillingReason == types.InvoiceBillingReasonSubscriptionTrialStart {
    if !inv.AmountDue.IsZero() {
        s.Logger.ErrorwCtx(ctx, "trial-start invoice has non-zero amount_due, skipping payment",
            "subscription_id", sub.ID,
            "invoice_id", inv.ID,
            "amount_due", inv.AmountDue,
        )
        return nil
    }
    s.processPayment(ctx, sub, inv, types.PaymentBehaviorDefaultActive, flowType)
    return nil
}
```

Place this block immediately after the logger call that opens `HandlePaymentBehavior` and before the `if flowType == types.InvoiceFlowManual` block.

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/service/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_payment_processor.go
git commit -m "feat(billing): guard HandlePaymentBehavior for trial-start invoices"
```

---

## Task 4: Add `createTrialStartInvoice` function

**Files:**
- Modify: `internal/service/subscription_trial.go`

Context: This function mirrors `processSubscriptionTrialEnd` in structure. It creates a `SUBSCRIPTION_TRIAL_START` invoice for the trial period. Guards: inherited subscriptions skip (only parent gets the invoice), missing trial bounds skip.

- [ ] **Step 1: Add the function at the end of `subscription_trial.go`**

Append to `internal/service/subscription_trial.go`:

```go
// createTrialStartInvoice creates the $0 trial-start invoice for a newly-created trialing subscription.
// The invoice is finalized with an invoice number and auto-paid immediately.
// Only parent subscriptions get this invoice; inherited subscriptions are skipped.
func (s *subscriptionService) createTrialStartInvoice(
	ctx context.Context,
	sub *subscription.Subscription,
	invoiceService InvoiceService,
) error {
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil
	}
	if sub.TrialStart == nil || sub.TrialEnd == nil {
		return nil
	}

	paymentParams := dto.NewPaymentParametersFromSubscription(
		sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID,
	).NormalizePaymentParameters()

	_, _, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    lo.FromPtr(sub.TrialStart),
		PeriodEnd:      lo.FromPtr(sub.TrialEnd),
		ReferencePoint: types.ReferencePointPeriodStart,
		BillingReason:  types.InvoiceBillingReasonSubscriptionTrialStart,
	}, paymentParams, types.InvoiceFlowSubscriptionCreation, false)
	return err
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/service/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_trial.go
git commit -m "feat(billing): add createTrialStartInvoice function"
```

---

## Task 5: Wire up the call site in subscription creation

**Files:**
- Modify: `internal/service/subscription.go` (~line 416)

Context: The existing guard skips invoice creation for `DRAFT` and `TRIALING` subscriptions:
```go
if sub.SubscriptionStatus != types.SubscriptionStatusDraft && sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
    // ... existing invoice creation + activation logic ...
}
```
We add a separate `if` block immediately after it for the trialing case.

- [ ] **Step 1: Find the existing guard**

Search for this comment in `internal/service/subscription.go`:

```
// Create invoice for non-draft, non-trialing subscriptions (trial conversion invoice is created at trial end).
```

The block follows immediately after.

- [ ] **Step 2: Add the trial-start invoice call**

After the closing `}` of the existing `if sub.SubscriptionStatus != ... TRIALING` block (and before the inherited children loop), add:

```go
// Create $0 trial-start invoice for trialing subscriptions.
if sub.SubscriptionStatus == types.SubscriptionStatusTrialing {
    if err := s.createTrialStartInvoice(ctx, sub, invoiceService); err != nil {
        return err
    }
}
```

The existing block is untouched. This is a separate independent `if`.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/service/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Run existing tests to check nothing broke**

```bash
go test -v -race ./internal/service/... -run TestSubscription -timeout 120s 2>&1 | tail -30
```

Expected: all existing subscription tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/service/subscription.go
git commit -m "feat(billing): create trial-start invoice on TRIALING subscription creation"
```

---

## Task 6: Integration tests

**Files:**
- Create: `internal/service/subscription_trial_start_invoice_test.go`

Context: Use the existing `testutil.BaseServiceTestSuite` pattern. The tests must create a real subscription through the service layer (not mock) to exercise the full path: `createTrialStartInvoice` → `ComputeInvoice` → `FinalizeInvoice` → `HandlePaymentBehavior`. Reference `internal/service/subscription_trial_payment_matrix_test.go` for how to set up the suite.

- [ ] **Step 1: Write the failing tests**

Create `internal/service/subscription_trial_start_invoice_test.go`.

Key type notes for this file:
- `InvoiceRepo.List` returns `[]*domain_invoice.Invoice` (domain objects, not DTOs)
- `domain_invoice.Invoice.BillingReason` is `string` — compare using `string(types.InvoiceBillingReasonSubscriptionTrialStart)`
- `domain_invoice.Invoice.InvoiceStatus` is `types.InvoiceStatus`
- `domain_invoice.Invoice.PaymentStatus` is `types.PaymentStatus`
- `types.InvoiceFilter.SubscriptionID` is `string` (not pointer)
- `types.InvoiceLineItemFilter` uses `InvoiceIDs []string` (not a single `InvoiceID`)

```go
package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type SubscriptionTrialStartInvoiceSuite struct {
	testutil.BaseServiceTestSuite
	subSvc SubscriptionService
}

func TestSubscriptionTrialStartInvoice(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialStartInvoiceSuite))
}

func (s *SubscriptionTrialStartInvoiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.subSvc = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

// setupTrialFixtures creates a customer, plan, and recurring fixed price with 14-day trial.
func (s *SubscriptionTrialStartInvoiceSuite) setupTrialFixtures() (custID, planID string) {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		Name:      "Trial Start Test Customer",
		Email:     "trialstart@example.com",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Start Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	pr := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		PlanID:             lo.ToPtr(pl.ID),
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Type:               types.PRICE_TYPE_FIXED,
		TrialPeriodDays:    14,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))

	return cust.ID, pl.ID
}

// findTrialStartInvoice returns the SUBSCRIPTION_TRIAL_START invoice for a subscription, or nil.
func (s *SubscriptionTrialStartInvoiceSuite) findTrialStartInvoice(subID string) *domaininvoice.Invoice {
	ctx := s.GetContext()
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, &types.InvoiceFilter{
		SubscriptionID: subID,
	})
	s.Require().NoError(err)
	for _, inv := range invoices {
		if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialStart) {
			return inv
		}
	}
	return nil
}

// TestTrialStartInvoice_CreatedFinalizedPaid verifies that creating a trialing subscription
// produces a SUBSCRIPTION_TRIAL_START invoice that is FINALIZED, paid, with invoice number,
// and that the subscription stays TRIALING.
func (s *SubscriptionTrialStartInvoiceSuite) TestTrialStartInvoice_CreatedFinalizedPaid() {
	ctx := s.GetContext()
	custID, planID := s.setupTrialFixtures()

	sub, err := s.subSvc.CreateSubscription(ctx, &dto.CreateSubscriptionRequest{
		CustomerID:       custID,
		PlanID:           planID,
		StartDate:        time.Now().UTC(),
		Currency:         "usd",
		CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorAllowIncomplete),
	})
	s.Require().NoError(err)
	s.Require().NotNil(sub)
	s.Equal(types.SubscriptionStatusTrialing, sub.SubscriptionStatus)

	inv := s.findTrialStartInvoice(sub.ID)
	s.Require().NotNil(inv, "SUBSCRIPTION_TRIAL_START invoice must be created")
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus, "must be FINALIZED")
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus, "must be paid")
	s.True(inv.Subtotal.IsZero(), "subtotal must be $0")
	s.True(inv.AmountDue.IsZero(), "amount_due must be $0")
	s.NotNil(inv.InvoiceNumber, "invoice number must be assigned")
	s.NotEmpty(lo.FromPtr(inv.InvoiceNumber), "invoice number must not be empty")

	// Critical: subscription must stay TRIALING — not activated
	updatedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusTrialing, updatedSub.SubscriptionStatus,
		"subscription must stay TRIALING after $0 trial-start invoice is paid")
}

// TestTrialStartInvoice_NotCreatedForNonTrialing verifies a non-trial subscription does not
// produce a SUBSCRIPTION_TRIAL_START invoice.
func (s *SubscriptionTrialStartInvoiceSuite) TestTrialStartInvoice_NotCreatedForNonTrialing() {
	ctx := s.GetContext()
	custID, planID := s.setupTrialFixtures()

	sub, err := s.subSvc.CreateSubscription(ctx, &dto.CreateSubscriptionRequest{
		CustomerID:       custID,
		PlanID:           planID,
		StartDate:        time.Now().UTC(),
		Currency:         "usd",
		CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorDefaultActive),
		TrialPeriodDays:  lo.ToPtr(0), // explicitly no trial
	})
	s.Require().NoError(err)
	s.Require().NotNil(sub)

	inv := s.findTrialStartInvoice(sub.ID)
	s.Nil(inv, "SUBSCRIPTION_TRIAL_START invoice must NOT exist for non-trialing subscription")
}

// TestTrialStartInvoice_HasLineItems verifies the trial-start invoice contains line items
// showing what the customer would pay after trial ends.
func (s *SubscriptionTrialStartInvoiceSuite) TestTrialStartInvoice_HasLineItems() {
	ctx := s.GetContext()
	custID, planID := s.setupTrialFixtures()

	sub, err := s.subSvc.CreateSubscription(ctx, &dto.CreateSubscriptionRequest{
		CustomerID:       custID,
		PlanID:           planID,
		StartDate:        time.Now().UTC(),
		Currency:         "usd",
		CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
		PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorAllowIncomplete),
	})
	s.Require().NoError(err)

	inv := s.findTrialStartInvoice(sub.ID)
	s.Require().NotNil(inv, "SUBSCRIPTION_TRIAL_START invoice must exist")

	lineItems, err := s.GetStores().InvoiceLineItemRepo.List(ctx, &types.InvoiceLineItemFilter{
		InvoiceIDs: []string{inv.ID},
	})
	s.Require().NoError(err)
	s.NotEmpty(lineItems, "trial-start invoice must have line items showing post-trial charges")
}
```

- [ ] **Step 2: Run the tests — expect failures (Tasks 1-5 not yet done at this point, or red from missing fixtures)**

```bash
go test -v -race ./internal/service/... -run TestSubscriptionTrialStartInvoice -timeout 120s 2>&1 | tail -40
```

Expected at this step (if running after tasks 1-5 are complete): PASS. If running before tasks 1-5: compilation errors or FAIL.

- [ ] **Step 3: Run full service test suite to check for regressions**

```bash
go test -race ./internal/service/... -timeout 300s 2>&1 | tail -20
```

Expected: all tests pass including existing `TestSubscriptionTrialPaymentMatrix`.

- [ ] **Step 4: Commit**

```bash
git add internal/service/subscription_trial_start_invoice_test.go
git commit -m "test(billing): add integration tests for trial-start invoice creation"
```

---

## Execution Order

Tasks must be run in order (each builds on the previous):

1. Task 1 — types (adds the constant used by all subsequent tasks)
2. Task 2 — ComputeInvoice (allows invoice to be finalized)
3. Task 3 — HandlePaymentBehavior (prevents sub activation)
4. Task 4 — createTrialStartInvoice (the new function)
5. Task 5 — call site (wires it in)
6. Task 6 — tests (validates end-to-end)

## Verification

After all tasks, run:

```bash
go vet ./...
go test -race ./internal/... -timeout 300s 2>&1 | grep -E "FAIL|ok|---"
```

Expected: all packages pass, no failures.
