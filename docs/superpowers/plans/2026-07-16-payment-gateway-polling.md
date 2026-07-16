# Payment Gateway Polling on GetPayment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `GET /payments/{id}` is called and the payment is in `PENDING` or `PROCESSING` state with a known gateway ID, synchronously fetch the latest status from the gateway (Stripe/Razorpay/Moyasar) and apply any transition before returning.

**Architecture:** All logic lives in `internal/ee/service/payment.go` via a new private method `syncPaymentStatusFromGateway` that is called from `GetPayment` after the DB fetch. Razorpay gets a new public `GetPaymentStatus` method added to its `PaymentService`. Errors from the gateway degrade gracefully — the caller always gets a 200.

**Tech Stack:** Go 1.23, Gin, Ent, `github.com/samber/lo`, `github.com/shopspring/decimal`, `github.com/stretchr/testify`

**Spec:** `docs/superpowers/specs/2026-07-16-payment-gateway-polling-design.md`

---

## File Map

| File | Change |
|---|---|
| `internal/integration/razorpay/payment.go` | Add `GetPaymentStatus(ctx, razorpayPaymentID string) (string, error)` |
| `internal/integration/razorpay/payment_status_test.go` | New — unit tests for `GetPaymentStatus` validation |
| `internal/ee/service/payment.go` | Add `syncPaymentStatusFromGateway`; update `GetPayment` |
| `internal/ee/service/payment_test.go` | Add guard-condition tests for the sync path |

---

## Task 1: Add `GetPaymentStatus` to Razorpay PaymentService

**Files:**
- Modify: `internal/integration/razorpay/payment.go`
- Create: `internal/integration/razorpay/payment_status_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/integration/razorpay/payment_status_test.go`:

```go
package razorpay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPaymentStatus_EmptyID(t *testing.T) {
	svc := &PaymentService{}
	status, err := svc.GetPaymentStatus(context.Background(), "")
	assert.Error(t, err)
	assert.Empty(t, status)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/integration/razorpay/... -run TestGetPaymentStatus -v
```

Expected: compile error — `GetPaymentStatus` undefined.

- [ ] **Step 3: Implement `GetPaymentStatus` in `internal/integration/razorpay/payment.go`**

Add after the `ensureRefunded` method at the end of the file (line ~1003):

```go
// GetPaymentStatus fetches the current status of a Razorpay payment.
// Returns the raw Razorpay status string: "created", "authorized", "captured", "refunded", "failed".
func (s *PaymentService) GetPaymentStatus(ctx context.Context, razorpayPaymentID string) (string, error) {
	if razorpayPaymentID == "" {
		return "", ierr.NewError("razorpay_payment_id is required").Mark(ierr.ErrValidation)
	}
	result, err := s.client.FetchPayment(ctx, razorpayPaymentID)
	if err != nil {
		s.logger.Error(ctx, "failed to fetch Razorpay payment status",
			"razorpay_payment_id", razorpayPaymentID,
			"error", err)
		return "", err
	}
	status, _ := result["status"].(string)
	return status, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/integration/razorpay/... -run TestGetPaymentStatus -v
```

Expected: PASS

- [ ] **Step 5: Verify compilation**

```bash
go build ./internal/integration/razorpay/...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/integration/razorpay/payment.go internal/integration/razorpay/payment_status_test.go
git commit -m "feat(razorpay): add GetPaymentStatus method for gateway polling"
```

---

## Task 2: Add `syncPaymentStatusFromGateway` and update `GetPayment`

**Files:**
- Modify: `internal/ee/service/payment.go`
- Modify: `internal/ee/service/payment_test.go`

- [ ] **Step 1: Write the failing tests for guard conditions**

Append to `internal/ee/service/payment_test.go`, inside the `PaymentServiceSuite`:

```go
func (s *PaymentServiceSuite) TestSyncPaymentStatusFromGateway_GuardConditions() {
	ctx := types.SetEnvironmentID(s.GetContext(), "test-env-id")

	tests := []struct {
		name           string
		status         types.PaymentStatus
		gatewayPayID   *string
		gateway        *string
		expectSameObj  bool
	}{
		{
			name:          "terminal FAILED status — no sync",
			status:        types.PaymentStatusFailed,
			gatewayPayID:  lo.ToPtr("pay_xyz"),
			gateway:       lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
			expectSameObj: true,
		},
		{
			name:          "INITIATED status — no sync",
			status:        types.PaymentStatusInitiated,
			gatewayPayID:  lo.ToPtr("pay_xyz"),
			gateway:       lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
			expectSameObj: true,
		},
		{
			name:          "PENDING with nil gateway_payment_id — no sync",
			status:        types.PaymentStatusPending,
			gatewayPayID:  nil,
			gateway:       lo.ToPtr(string(types.PaymentGatewayTypeStripe)),
			expectSameObj: true,
		},
		{
			name:          "PENDING with nil gateway — no sync",
			status:        types.PaymentStatusPending,
			gatewayPayID:  lo.ToPtr("pay_xyz"),
			gateway:       nil,
			expectSameObj: true,
		},
		{
			name:          "PENDING with unsupported gateway nomod — no sync",
			status:        types.PaymentStatusPending,
			gatewayPayID:  lo.ToPtr("nomod_ref"),
			gateway:       lo.ToPtr(string(types.PaymentGatewayTypeNomod)),
			expectSameObj: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Create payment directly in repo to set arbitrary status
			p := &payment.Payment{
				ID:              "pay_guard_" + tt.name,
				DestinationType: types.PaymentDestinationTypeInvoice,
				DestinationID:   s.testData.invoice.ID,
				PaymentMethodType: types.PaymentMethodTypePaymentLink,
				PaymentStatus:   tt.status,
				Amount:          decimal.NewFromFloat(100),
				Currency:        "usd",
				GatewayPaymentID: tt.gatewayPayID,
				PaymentGateway:  tt.gateway,
				BaseModel:       types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().PaymentRepo.Create(ctx, p))

			svc := s.service.(*paymentService)
			result, err := svc.syncPaymentStatusFromGateway(ctx, p)
			s.NoError(err)
			// Guard returned same object (same ID, status unchanged)
			s.Equal(p.ID, result.ID)
			s.Equal(tt.status, result.PaymentStatus)
		})
	}
}
```

Also add this import to the test file if not present: `"github.com/flexprice/flexprice/internal/domain/payment"` and `"github.com/samber/lo"`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ee/service/... -run TestPaymentService/TestSyncPaymentStatusFromGateway -v
```

Expected: compile error — `syncPaymentStatusFromGateway` undefined, `paymentService` type assertion fails.

- [ ] **Step 3: Add `syncPaymentStatusFromGateway` to `internal/ee/service/payment.go`**

Add the following imports to the import block if not already present:
- `"time"`
- `"github.com/samber/lo"`
- `"github.com/flexprice/flexprice/internal/domain/payment"`

Append the method at the end of the file:

```go
// TODO: extract into a GatewayStatusSyncer when supporting more gateways or throttling
func (s *paymentService) syncPaymentStatusFromGateway(ctx context.Context, p *payment.Payment) (*payment.Payment, error) {
	if p.PaymentStatus != types.PaymentStatusPending && p.PaymentStatus != types.PaymentStatusProcessing {
		return p, nil
	}
	if p.GatewayPaymentID == nil || *p.GatewayPaymentID == "" {
		return p, nil
	}
	if p.PaymentGateway == nil {
		return p, nil
	}

	gatewayPaymentID := *p.GatewayPaymentID
	gateway := types.PaymentGatewayType(*p.PaymentGateway)

	var rawStatus string
	switch gateway {
	case types.PaymentGatewayTypeStripe:
		stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
		if err != nil {
			s.Logger.Warn(ctx, "failed to get Stripe integration for payment sync",
				"payment_id", p.ID, "error", err)
			return p, err
		}
		resp, err := stripeIntegration.PaymentSvc.GetPaymentStatusByPaymentIntent(ctx, gatewayPaymentID, "")
		if err != nil {
			s.Logger.Warn(ctx, "failed to fetch payment status from Stripe",
				"payment_id", p.ID, "gateway_payment_id", gatewayPaymentID, "error", err)
			return p, err
		}
		rawStatus = resp.Status

	case types.PaymentGatewayTypeRazorpay:
		razorpayIntegration, err := s.IntegrationFactory.GetRazorpayIntegration(ctx)
		if err != nil {
			s.Logger.Warn(ctx, "failed to get Razorpay integration for payment sync",
				"payment_id", p.ID, "error", err)
			return p, err
		}
		status, err := razorpayIntegration.PaymentSvc.GetPaymentStatus(ctx, gatewayPaymentID)
		if err != nil {
			s.Logger.Warn(ctx, "failed to fetch payment status from Razorpay",
				"payment_id", p.ID, "gateway_payment_id", gatewayPaymentID, "error", err)
			return p, err
		}
		rawStatus = status

	case types.PaymentGatewayTypeMoyasar:
		moyasarIntegration, err := s.IntegrationFactory.GetMoyasarIntegration(ctx)
		if err != nil {
			s.Logger.Warn(ctx, "failed to get Moyasar integration for payment sync",
				"payment_id", p.ID, "error", err)
			return p, err
		}
		resp, err := moyasarIntegration.PaymentSvc.GetPaymentStatus(ctx, gatewayPaymentID)
		if err != nil {
			s.Logger.Warn(ctx, "failed to fetch payment status from Moyasar",
				"payment_id", p.ID, "gateway_payment_id", gatewayPaymentID, "error", err)
			return p, err
		}
		rawStatus = resp.Status

	default:
		return p, nil
	}

	// Map gateway-native status → FlexPrice status
	var newStatus types.PaymentStatus
	switch gateway {
	case types.PaymentGatewayTypeStripe:
		switch rawStatus {
		case "succeeded":
			newStatus = types.PaymentStatusSucceeded
		case "requires_payment_method", "canceled":
			newStatus = types.PaymentStatusFailed
		default:
			return p, nil
		}
	case types.PaymentGatewayTypeRazorpay:
		switch rawStatus {
		case "captured":
			newStatus = types.PaymentStatusSucceeded
		case "failed":
			newStatus = types.PaymentStatusFailed
		default:
			return p, nil
		}
	case types.PaymentGatewayTypeMoyasar:
		switch rawStatus {
		case "paid":
			newStatus = types.PaymentStatusSucceeded
		case "failed":
			newStatus = types.PaymentStatusFailed
		default:
			return p, nil
		}
	}

	if newStatus == p.PaymentStatus {
		return p, nil
	}

	s.Logger.Info(ctx, "gateway status differs from DB, applying transition",
		"payment_id", p.ID,
		"gateway", gateway,
		"db_status", p.PaymentStatus,
		"gateway_raw_status", rawStatus,
		"new_status", newStatus,
	)

	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: lo.ToPtr(string(newStatus)),
	}
	switch newStatus {
	case types.PaymentStatusSucceeded:
		updateReq.SucceededAt = lo.ToPtr(now)
	case types.PaymentStatusFailed:
		updateReq.FailedAt = lo.ToPtr(now)
	}

	if _, err := s.UpdatePayment(ctx, p.ID, updateReq); err != nil {
		s.Logger.Error(ctx, "failed to update payment status from gateway sync",
			"payment_id", p.ID, "new_status", newStatus, "error", err)
		return p, err
	}

	if newStatus == types.PaymentStatusSucceeded && p.DestinationType == types.PaymentDestinationTypeInvoice {
		invoiceSvc := NewInvoiceService(s.ServiceParams)
		if err := invoiceSvc.ReconcilePaymentStatus(ctx, p.DestinationID, types.PaymentStatusSucceeded, &p.Amount); err != nil {
			s.Logger.Error(ctx, "failed to reconcile invoice after gateway sync",
				"payment_id", p.ID, "invoice_id", p.DestinationID, "error", err)
		}
	}

	fresh, err := s.PaymentRepo.Get(ctx, p.ID)
	if err != nil {
		s.Logger.Error(ctx, "failed to re-fetch payment after gateway sync",
			"payment_id", p.ID, "error", err)
		return p, err
	}
	return fresh, nil
}
```

- [ ] **Step 4: Update `GetPayment` in `internal/ee/service/payment.go`**

Replace the existing `GetPayment` method (lines 326–350) with:

```go
// GetPayment gets a payment by ID
func (s *paymentService) GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error) {
	if id == "" {
		return nil, ierr.NewError("payment_id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation)
	}

	p, err := s.PaymentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Best-effort gateway sync for in-flight payments; errors are logged inside and suppressed here
	p, _ = s.syncPaymentStatusFromGateway(ctx, p)

	response := dto.NewPaymentResponse(p)
	if p.DestinationType == types.PaymentDestinationTypeInvoice {
		invoice, err := s.InvoiceRepo.Get(ctx, p.DestinationID)
		if err != nil {
			return nil, err
		}
		if invoice.InvoiceNumber != nil {
			response.InvoiceNumber = invoice.InvoiceNumber
		}
	}
	return response, nil
}
```

- [ ] **Step 5: Run guard tests to verify they pass**

```bash
go test ./internal/ee/service/... -run TestPaymentService/TestSyncPaymentStatusFromGateway -v
```

Expected: all 5 sub-tests PASS.

- [ ] **Step 6: Run the full payment service test suite**

```bash
go test ./internal/ee/service/... -run TestPaymentService -v
```

Expected: all tests PASS (no regressions from `GetPayment` change).

- [ ] **Step 7: Verify full build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Run vet**

```bash
go vet ./internal/ee/service/... ./internal/integration/razorpay/...
```

Expected: no issues.

- [ ] **Step 9: Commit**

```bash
git add internal/ee/service/payment.go internal/ee/service/payment_test.go
git commit -m "feat(payment): sync gateway status on GetPayment for PENDING/PROCESSING payments"
```

---

## Self-Review Checklist

- [x] Razorpay `GetPaymentStatus` method: validates empty ID, calls `client.FetchPayment`, returns raw string — covered in Task 1
- [x] `syncPaymentStatusFromGateway` guard conditions (status, nil gateway ID, nil gateway, unsupported gateway) — tested in Task 2
- [x] Stripe status mapping (`succeeded` → SUCCEEDED, `requires_payment_method`/`canceled` → FAILED)
- [x] Razorpay status mapping (`captured` → SUCCEEDED, `failed` → FAILED)
- [x] Moyasar status mapping (`paid` → SUCCEEDED, `failed` → FAILED)
- [x] Lifecycle transitions: `UpdatePayment` + invoice reconcile for SUCCEEDED+INVOICE — in `syncPaymentStatusFromGateway`
- [x] No-op when gateway status == DB status
- [x] `GetPayment` degrades gracefully: errors from `syncPaymentStatusFromGateway` are suppressed (logged inside)
- [x] Re-fetch from repo after update to return fresh data
- [x] TODO comment in place: `// TODO: extract into a GatewayStatusSyncer when supporting more gateways or throttling`
- [x] `IntegrationFactory` nil guard: if `IntegrationFactory` is nil in tests (not set in `setupService`), guard conditions return early before reaching the factory call — safe
