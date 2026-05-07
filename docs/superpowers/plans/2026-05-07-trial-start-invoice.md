# Trial Start Invoice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a $0 preview invoice when a trialing subscription is activated, matching Stripe/Paddle behavior and enabling card capture via downstream checkout flows.

**Architecture:** Add a new `SUBSCRIPTION_TRIAL_START` billing reason that flows through the existing compute → finalize → payment pipeline with two targeted modifications: `ZeroOutAmounts()` on the DTO zeroes all line item amounts before they are written, and an early-return in `attemptPaymentForSubscriptionInvoice` sets payment status by collection method instead of running the full payment processor.

**Tech Stack:** Go 1.23, testify/suite (in-memory stores for integration tests), shopspring/decimal

**Spec:** `docs/superpowers/specs/2026-05-07-trial-start-invoice-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/types/invoice.go` | Modify | Add constant, update all billing reason comments, add to `Validate()` |
| `internal/types/invoice_test.go` | Create | Unit tests for new billing reason |
| `internal/api/dto/invoice.go` | Modify | Add `ZeroOutAmounts()` method |
| `internal/api/dto/invoice_test.go` | Create | Unit test for `ZeroOutAmounts()` |
| `internal/service/invoice.go` | Modify | Three changes in `ComputeInvoice` + early-return in `attemptPaymentForSubscriptionInvoice` |
| `internal/service/subscription.go` | Modify | Add trialing branch in `CreateSubscription` |
| `internal/service/subscription_test.go` | Modify | Integration tests: trial start invoice created, payment status |

---

## Task 1: Add `SUBSCRIPTION_TRIAL_START` billing reason constant

**Files:**
- Modify: `internal/types/invoice.go`
- Create: `internal/types/invoice_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/types/invoice_test.go`:

```go
package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceBillingReason_TrialStart_Validate(t *testing.T) {
	err := InvoiceBillingReasonSubscriptionTrialStart.Validate()
	require.NoError(t, err, "SUBSCRIPTION_TRIAL_START must be a valid billing reason")
}

func TestInvoiceBillingReason_TrialStart_NotFirstOpenInvoiceReason(t *testing.T) {
	// Trial start invoices must NOT trigger subscription activation when paid.
	assert.False(t,
		InvoiceBillingReasonSubscriptionTrialStart.IsFirstSubscriptionOpenInvoiceReason(),
		"SUBSCRIPTION_TRIAL_START must not activate subscription on payment",
	)
}

func TestInvoiceBillingReason_TrialStart_StringValue(t *testing.T) {
	assert.Equal(t, "SUBSCRIPTION_TRIAL_START", string(InvoiceBillingReasonSubscriptionTrialStart))
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v -race ./internal/types/... -run TestInvoiceBillingReason_TrialStart
```

Expected: `FAIL — InvoiceBillingReasonSubscriptionTrialStart undefined`

- [ ] **Step 3: Add the constant to `internal/types/invoice.go`**

Replace the existing `const` block and `Validate()` function (lines ~163–203):

```go
// InvoiceBillingReason indicates why an invoice was generated. It drives diverging
// behavior throughout the compute → finalize → payment pipeline — see each constant
// for which flows it affects.
type InvoiceBillingReason string

const (
	// InvoiceBillingReasonSubscriptionCreate is the initial invoice at subscription
	// activation for non-trial subscriptions.
	// Flow: CreateSubscription → CreateSubscriptionInvoice (InvoiceFlowSubscriptionCreation)
	// Compute: advance charges for current_period_start → current_period_end (ReferencePointPeriodStart)
	// Zero-dollar: marked SKIPPED — no charge, no sync, no invoice number.
	// Side-effect: activates subscription when paid or if no payment is needed.
	InvoiceBillingReasonSubscriptionCreate InvoiceBillingReason = "SUBSCRIPTION_CREATE"

	// InvoiceBillingReasonSubscriptionCycle is generated at the end of each billing period.
	// Flow: ProcessBillingDue → renewal workflow
	// Compute: arrear usage charges for closing period + advance charges for next period (ReferencePointPeriodEnd)
	// Zero-dollar: marked SKIPPED.
	InvoiceBillingReasonSubscriptionCycle InvoiceBillingReason = "SUBSCRIPTION_CYCLE"

	// InvoiceBillingReasonSubscriptionUpdate is generated when a subscription changes
	// mid-period (plan upgrade/downgrade, quantity change) or when
	// OpeningInvoiceAdjustmentAmount is provided at creation.
	// Flow: ChangeSubscription, or CreateSubscription with adjustment amount
	// Compute: proration credits/charges (ReferencePointPeriodStart)
	// Zero-dollar: marked SKIPPED.
	// Side-effect: activates INCOMPLETE subscription when paid.
	InvoiceBillingReasonSubscriptionUpdate InvoiceBillingReason = "SUBSCRIPTION_UPDATE"

	// InvoiceBillingReasonSubscriptionTrialEnd is the first real invoice when a trialing
	// subscription converts to paid at trial end.
	// Flow: ProcessTrialEndDue → processSubscriptionTrialEnd (InvoiceFlowRenewal)
	// Compute: advance charges from trial_end → trial_end + billing_period (ReferencePointPeriodStart).
	//          Billing anchor is reset to trial_end so the first paid period is full-length.
	// Zero-dollar: marked SKIPPED; subscription is activated immediately without payment.
	// Side-effect: activates subscription when paid.
	InvoiceBillingReasonSubscriptionTrialEnd InvoiceBillingReason = "SUBSCRIPTION_TRIAL_END"

	// InvoiceBillingReasonSubscriptionTrialStart is a $0 preview invoice created the moment
	// a trialing subscription is activated. It mirrors the first paid invoice's line items
	// (same advance charges) but all amounts are forced to zero.
	//
	// Purpose:
	//   - Gives customers visibility into what they will be charged when the trial ends.
	//   - For send_invoice: stays PENDING so it syncs to downstream integrations (Stripe,
	//     Paddle) and their checkout / card-capture flow is triggered. Paddle has no other
	//     mechanism to save a payment method outside of a $0 checkout transaction.
	//   - For charge_automatically: marked SUCCEEDED immediately — payment method is on
	//     file and there is nothing to charge.
	//
	// Flow: CreateSubscription (trialing path) → CreateSubscriptionInvoice (InvoiceFlowSubscriptionCreation)
	// Compute: advance charges for current_period_start → current_period_end = trial_start → trial_end
	//          (ReferencePointPeriodStart); all line item amounts zeroed before writing.
	// Zero-dollar: NOT skipped — intentionally $0, must be finalized and visible in listings.
	// Side-effect: none — subscription stays TRIALING regardless of payment status.
	InvoiceBillingReasonSubscriptionTrialStart InvoiceBillingReason = "SUBSCRIPTION_TRIAL_START"

	// InvoiceBillingReasonProration is generated for proration credits or charges when a
	// subscription changes mid-period (cancellation, plan change, quantity adjustment).
	// Flow: ChangeSubscription proration path
	// Zero-dollar: marked SKIPPED.
	InvoiceBillingReasonProration InvoiceBillingReason = "PRORATION"

	// InvoiceBillingReasonManual is for invoices created directly by an administrator
	// outside of any automated subscription lifecycle.
	// Flow: manual invoice creation API
	// Zero-dollar: marked SKIPPED.
	InvoiceBillingReasonManual InvoiceBillingReason = "MANUAL"
)
```

Add `InvoiceBillingReasonSubscriptionTrialStart` to the `Validate()` allowed slice:

```go
func (r InvoiceBillingReason) Validate() error {
	allowed := []InvoiceBillingReason{
		InvoiceBillingReasonSubscriptionCreate,
		InvoiceBillingReasonSubscriptionCycle,
		InvoiceBillingReasonSubscriptionUpdate,
		InvoiceBillingReasonSubscriptionTrialEnd,
		InvoiceBillingReasonSubscriptionTrialStart, // new
		InvoiceBillingReasonProration,
		InvoiceBillingReasonManual,
	}
	// ... rest of function unchanged
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -v -race ./internal/types/... -run TestInvoiceBillingReason_TrialStart
```

Expected: `PASS` (3 tests)

- [ ] **Step 5: Vet**

```bash
go vet ./internal/types/...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/types/invoice.go internal/types/invoice_test.go
git commit -m "feat(billing): add SUBSCRIPTION_TRIAL_START billing reason with full taxonomy comments"
```

---

## Task 2: Add `ZeroOutAmounts()` to `CreateInvoiceRequest`

**Files:**
- Modify: `internal/api/dto/invoice.go`
- Create: `internal/api/dto/invoice_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/dto/invoice_test.go`:

```go
package dto

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCreateInvoiceRequest_ZeroOutAmounts(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(99),
		Total:     decimal.NewFromInt(99),
		AmountDue: decimal.NewFromInt(99),
		LineItems: []CreateInvoiceLineItemRequest{
			{Amount: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(2)},
			{Amount: decimal.NewFromInt(49), Quantity: decimal.NewFromInt(1)},
		},
	}

	req.ZeroOutAmounts()

	assert.True(t, req.Subtotal.IsZero(), "Subtotal must be zero")
	assert.True(t, req.Total.IsZero(), "Total must be zero")
	assert.True(t, req.AmountDue.IsZero(), "AmountDue must be zero")

	for i, li := range req.LineItems {
		assert.True(t, li.Amount.IsZero(), "line item %d Amount must be zero", i)
		// Quantity is deliberately preserved — it shows the pricing skeleton.
		assert.False(t, li.Quantity.IsZero(), "line item %d Quantity must be preserved", i)
	}
}

func TestCreateInvoiceRequest_ZeroOutAmounts_EmptyLineItems(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(50),
		Total:     decimal.NewFromInt(50),
		AmountDue: decimal.NewFromInt(50),
	}
	req.ZeroOutAmounts() // must not panic on nil/empty LineItems
	assert.True(t, req.Subtotal.IsZero())
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -v -race ./internal/api/dto/... -run TestCreateInvoiceRequest_ZeroOutAmounts
```

Expected: `FAIL — ZeroOutAmounts undefined`

- [ ] **Step 3: Add `ZeroOutAmounts()` to `internal/api/dto/invoice.go`**

Add this method directly after `ToComputeRequest()` (after line ~153):

```go
// ZeroOutAmounts forces all monetary amounts on this invoice request to zero while
// preserving line item structure (descriptions, quantities, price metadata).
//
// Used for trial start invoices: the customer sees exactly which charges apply when
// the trial ends, but amounts are always $0 during the trial window. Quantity and
// pricing metadata are kept so the pricing skeleton remains visible (e.g. "1 seat × $99/mo").
func (r *CreateInvoiceRequest) ZeroOutAmounts() {
	r.Subtotal = decimal.Zero
	r.Total = decimal.Zero
	r.AmountDue = decimal.Zero
	for i := range r.LineItems {
		r.LineItems[i].Amount = decimal.Zero
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -v -race ./internal/api/dto/... -run TestCreateInvoiceRequest_ZeroOutAmounts
```

Expected: `PASS` (2 tests)

- [ ] **Step 5: Vet**

```bash
go vet ./internal/api/dto/...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto/invoice.go internal/api/dto/invoice_test.go
git commit -m "feat(billing): add ZeroOutAmounts() to CreateInvoiceRequest for trial start invoices"
```

---

## Task 3: Write failing integration test for trial start invoice

This test verifies the full end-to-end flow: `CreateSubscription` with a trial period must produce a finalized $0 invoice with `billing_reason = SUBSCRIPTION_TRIAL_START`.

**Files:**
- Modify: `internal/service/subscription_test.go`

- [ ] **Step 1: Add the failing integration test to `SubscriptionServiceSuite`**

Add this method to `internal/service/subscription_test.go` (after the last suite method):

```go
func (s *SubscriptionServiceSuite) TestCreateSubscription_TrialStart_Invoice() {
	ctx := s.GetContext()

	// A flat monthly price with a 14-day trial.
	flatPrice := &price.Price{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		PlanID:         s.testData.plan.ID,
		Amount:         decimal.NewFromInt(99),
		Currency:       "usd",
		Type:           types.PRICE_TYPE_FIXED,
		BillingCadence: types.BILLING_CADENCE_RECURRING,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		TrialPeriodDays: 14,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, flatPrice))

	trialStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trialEnd := trialStart.AddDate(0, 0, 14) // Jan 15

	tests := []struct {
		name             string
		collectionMethod types.CollectionMethod
		paymentBehavior  types.PaymentBehavior
		wantPaymentStatus types.PaymentStatus
	}{
		{
			name:              "charge_automatically_marks_trial_invoice_succeeded",
			collectionMethod:  types.CollectionMethodChargeAutomatically,
			paymentBehavior:   types.PaymentBehaviorDefaultActive,
			wantPaymentStatus: types.PaymentStatusSucceeded,
		},
		{
			name:              "send_invoice_keeps_trial_invoice_pending",
			collectionMethod:  types.CollectionMethodSendInvoice,
			paymentBehavior:   types.PaymentBehaviorDefaultActive,
			wantPaymentStatus: types.PaymentStatusPending,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			cm := tt.collectionMethod
			pb := tt.paymentBehavior
			resp, err := s.service.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
				CustomerID:       s.testData.customer.ID,
				PlanID:           s.testData.plan.ID,
				Currency:         "usd",
				StartDate:        trialStart,
				CollectionMethod: &cm,
				PaymentBehavior:  &pb,
			})
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			// Subscription must remain TRIALING — trial start invoice does not activate it.
			s.Equal(types.SubscriptionStatusTrialing, resp.SubscriptionStatus,
				"subscription must stay TRIALING after trial start invoice")

			// Find the trial start invoice.
			invoiceSvc := s.createInvoiceService()
			filter := types.NewInvoiceFilter()
			filter.SubscriptionID = resp.ID
			filter.InvoiceType = types.InvoiceTypeSubscription
			invoices, err := invoiceSvc.ListInvoices(ctx, filter)
			s.Require().NoError(err)

			var trialInv *dto.InvoiceResponse
			for _, inv := range invoices.Items {
				if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialStart) {
					trialInv = inv
					break
				}
			}
			s.Require().NotNil(trialInv, "trial start invoice must exist")

			// Invoice must be FINALIZED, not SKIPPED.
			s.Equal(types.InvoiceStatusFinalized, trialInv.InvoiceStatus,
				"trial start invoice must be FINALIZED, not SKIPPED")

			// All amounts must be zero.
			s.True(trialInv.Subtotal.IsZero(), "subtotal must be zero")
			s.True(trialInv.Total.IsZero(), "total must be zero")

			// Period must be trial window.
			s.Require().NotNil(trialInv.PeriodStart)
			s.Require().NotNil(trialInv.PeriodEnd)
			s.Equal(trialStart.Unix(), trialInv.PeriodStart.Unix(), "period start must be trial start")
			s.Equal(trialEnd.Unix(), trialInv.PeriodEnd.Unix(), "period end must be trial end")

			// Payment status driven by collection method.
			s.Equal(tt.wantPaymentStatus, trialInv.PaymentStatus,
				"payment status must match collection method expectation")
		})
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test -v -race ./internal/service/... -run TestSubscriptionService/TestCreateSubscription_TrialStart_Invoice
```

Expected: `FAIL — trialInv is nil` (no trial start invoice created yet)

---

## Task 4: Implement `ComputeInvoice` changes + trialing branch in `CreateSubscription`

These two changes work together — `ComputeInvoice` must be updated before `CreateSubscription` invokes it for a trial start invoice.

**Files:**
- Modify: `internal/service/invoice.go`
- Modify: `internal/service/subscription.go`

- [ ] **Step 1: Add `SUBSCRIPTION_TRIAL_START` to the `refPoint` switch in `ComputeInvoice` (`invoice.go` ~line 403)**

```go
switch types.InvoiceBillingReason(inv.BillingReason) {
case types.InvoiceBillingReasonSubscriptionCreate,
	types.InvoiceBillingReasonSubscriptionTrialEnd,
	types.InvoiceBillingReasonSubscriptionTrialStart, // new
	types.InvoiceBillingReasonSubscriptionUpdate:
	refPoint = types.ReferencePointPeriodStart
case types.InvoiceBillingReasonProration:
	refPoint = types.ReferencePointCancel
}
```

- [ ] **Step 2: Call `ZeroOutAmounts()` after `PrepareSubscriptionInvoiceRequest` (`invoice.go` ~line 425)**

```go
subInvReq, err := billingService.PrepareSubscriptionInvoiceRequest(ctx, params)
if err != nil {
	return false, err
}

// Trial start invoices preview the first period's charges at $0. Amounts are
// computed normally so line item structure is accurate, then forced to zero.
if types.InvoiceBillingReason(inv.BillingReason) == types.InvoiceBillingReasonSubscriptionTrialStart {
	subInvReq.ZeroOutAmounts()
}

computeReq := subInvReq.ToComputeRequest()
```

- [ ] **Step 3: Exempt `SUBSCRIPTION_TRIAL_START` from the SKIPPED check (`invoice.go` ~line 483)**

```go
// Trial start invoices are intentionally $0 — must NOT be marked SKIPPED.
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

- [ ] **Step 4: Add the trialing branch in `CreateSubscription` (`subscription.go` ~line 416)**

The existing block reads:

```go
// Create invoice for non-draft, non-trialing subscriptions (trial conversion invoice is created at trial end).
if sub.SubscriptionStatus != types.SubscriptionStatusDraft && sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
    // ... existing non-trial invoice creation ...
}
```

Extend it to:

```go
// Create invoice for non-draft, non-trialing subscriptions (trial conversion invoice is created at trial end).
if sub.SubscriptionStatus != types.SubscriptionStatusDraft && sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
	// existing non-trial invoice creation — unchanged
	paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID).NormalizePaymentParameters()
	// ... rest of existing block unchanged ...

} else if sub.SubscriptionStatus == types.SubscriptionStatusTrialing {
	// Create a $0 preview invoice at trial start so downstream integrations (Stripe,
	// Paddle) can drive card capture via their $0 checkout flow.
	// syncTrialingStateFromCreateRequest has already aligned:
	//   CurrentPeriodStart = TrialStart
	//   CurrentPeriodEnd   = TrialEnd
	paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID).NormalizePaymentParameters()

	_, _, err = invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    sub.CurrentPeriodStart, // == TrialStart
		PeriodEnd:      sub.CurrentPeriodEnd,   // == TrialEnd
		ReferencePoint: types.ReferencePointPeriodStart,
		BillingReason:  types.InvoiceBillingReasonSubscriptionTrialStart,
	}, paymentParams, types.InvoiceFlowSubscriptionCreation, false)
	if err != nil {
		return err
	}
	// Subscription stays TRIALING — trial start invoice does not gate activation.
}
```

- [ ] **Step 5: Run the integration test**

```bash
go test -v -race ./internal/service/... -run TestSubscriptionService/TestCreateSubscription_TrialStart_Invoice
```

Expected: Most assertions pass. The payment status assertions may still fail until Task 5 is done (trial start invoice may be PENDING for charge_automatically case).

- [ ] **Step 6: Run the full service test suite to catch regressions**

```bash
go test -v -race ./internal/service/... -run TestSubscriptionService -timeout 120s
```

Expected: existing tests still pass.

- [ ] **Step 7: Vet**

```bash
go vet ./internal/service/...
```

Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add internal/service/invoice.go internal/service/subscription.go
git commit -m "feat(billing): create \$0 trial start invoice on trialing subscription activation"
```

---

## Task 5: Payment status — charge_automatically → SUCCEEDED, send_invoice → PENDING

**Files:**
- Modify: `internal/service/invoice.go` (`attemptPaymentForSubscriptionInvoice`)

- [ ] **Step 1: Add early-return for `SUBSCRIPTION_TRIAL_START` in `attemptPaymentForSubscriptionInvoice` (`invoice.go` ~line 2335)**

Add these lines at the top of `attemptPaymentForSubscriptionInvoice`, **after** the `sub == nil` fetch block (~line 2348) and **before** the Stripe connection check:

```go
// Trial start invoices are always $0 — skip the normal payment pipeline.
// charge_automatically: mark succeeded immediately; payment method is on file
//   but there is nothing to collect.
// send_invoice: leave PENDING so the finalized invoice syncs to downstream
//   integrations (Stripe, Paddle) and triggers their card-capture checkout flow.
// Subscription stays TRIALING in both cases — this invoice does not gate activation.
if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialStart) {
	if sub != nil && types.CollectionMethod(sub.CollectionMethod) == types.CollectionMethodChargeAutomatically {
		zero := decimal.Zero
		return s.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, &zero)
	}
	// send_invoice: invoice stays PENDING — nothing more to do.
	return nil
}
```

- [ ] **Step 2: Run the integration test**

```bash
go test -v -race ./internal/service/... -run TestSubscriptionService/TestCreateSubscription_TrialStart_Invoice
```

Expected: `PASS` (both charge_automatically and send_invoice cases)

- [ ] **Step 3: Run full service suite**

```bash
go test -v -race ./internal/service/... -timeout 180s
```

Expected: all existing tests still pass.

- [ ] **Step 4: Run full test suite**

```bash
go test -race ./... -timeout 300s
```

Expected: all tests pass.

- [ ] **Step 5: Vet and format**

```bash
go vet ./...
gofmt -w internal/types/invoice.go internal/types/invoice_test.go \
          internal/api/dto/invoice.go internal/api/dto/invoice_test.go \
          internal/service/invoice.go internal/service/subscription.go \
          internal/service/subscription_test.go
```

Expected: no vet errors, no formatting changes.

- [ ] **Step 6: Commit**

```bash
git add internal/service/invoice.go
git commit -m "feat(billing): set trial start invoice payment status by collection method"
```

---

## Completion Checklist

- [ ] `SUBSCRIPTION_TRIAL_START` passes `Validate()` and is NOT in `IsFirstSubscriptionOpenInvoiceReason()`
- [ ] `ZeroOutAmounts()` zeroes monetary fields and preserves `Quantity`
- [ ] `ComputeInvoice` routes `SUBSCRIPTION_TRIAL_START` to `ReferencePointPeriodStart`
- [ ] `ComputeInvoice` calls `ZeroOutAmounts()` after `PrepareSubscriptionInvoiceRequest`
- [ ] `ComputeInvoice` does NOT mark `SUBSCRIPTION_TRIAL_START` invoices as SKIPPED
- [ ] `CreateSubscription` creates a trial start invoice when `sub.SubscriptionStatus == TRIALING`
- [ ] Trial start invoice period = `CurrentPeriodStart → CurrentPeriodEnd` (= `TrialStart → TrialEnd`)
- [ ] `charge_automatically` trial start invoice → `PaymentStatus == SUCCEEDED`
- [ ] `send_invoice` trial start invoice → `PaymentStatus == PENDING`
- [ ] Subscription remains `TRIALING` after trial start invoice is created and processed
- [ ] All existing tests pass
