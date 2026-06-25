# Checkout Payment Provider Design — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire all 4 payment gateways (Stripe, Razorpay, Nomod, Moyasar) into a unified `CheckoutProvider` interface so that `executeCheckoutAction` contacts the gateway, records an `EntityIntegrationMapping` for webhook routing, and `CompleteCheckoutSession` is atomically idempotent — plus a Temporal expiry workflow for expired sessions.

**Architecture:** The `CheckoutSession` record is written to the DB first (status = `initiated`) before any action or provider logic runs — this is unconditional and happens even if fulfillment later fails. Then a thin adapter per provider wraps existing `PaymentService.CreatePaymentLink()` (unchanged) behind a new `CheckoutProvider` interface. `IntegrationFactory.GetCheckoutProvider()` dispatches to the right adapter. After a successful provider call the session is updated to `pending` with the `NextAction` URL. Each webhook handler looks up `EntityIntegrationMapping` first, then routes to `CompleteCheckoutSession`. `MarkCompleted` in the repository does a conditional `UPDATE WHERE status IN (pending, initiated)` to guard against duplicate delivery. If any fulfillment step fails, `CleanupCheckoutSession` deletes the created entities and updates the session to `failed`.

**Tech Stack:** Go 1.23, Ent ORM, PostgreSQL, Temporal (cron task queue), Gin webhooks.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/types/checkout.go` | Modify | Add `PaymentAction` struct; expand `CheckoutPaymentProvider` enum + `SessionExpiry()` |
| `internal/types/checkout_configuration.go` | Modify | Replace nested `CheckoutProviderResult` with flat struct + `PaymentActionForUser()` |
| `internal/types/webhook.go` | Modify | Add `WebhookEventCheckoutSessionExpired` constant |
| `internal/types/schedule.go` | Modify | Add `ScheduleIDCheckoutSessionExpiry` constant + `AllTemporalServerScheduleIDs()` entry |
| `internal/api/dto/checkout_session.go` | Modify | Remove `PaymentAction` struct; simplify `ToCheckoutSessionResponse` |
| `internal/interfaces/checkout_provider.go` | Create | `CheckoutProvider` interface + `CheckoutProviderRequest`/`CheckoutProviderResponse` |
| `internal/interfaces/service.go` | Modify | Add `CheckoutSessionService` interface to `ServiceDependencies` |
| `internal/integration/stripe/checkout_adapter.go` | Create | Stripe adapter |
| `internal/integration/razorpay/checkout_adapter.go` | Create | Razorpay adapter |
| `internal/integration/nomod/checkout_adapter.go` | Create | Nomod adapter |
| `internal/integration/moyasar/checkout_adapter.go` | Create | Moyasar adapter |
| `internal/integration/factory.go` | Modify | Add `GetCheckoutProvider()` |
| `internal/ee/service/checkout_session_actions.go` | Modify | Wire provider call + `EntityIntegrationMapping` + `ExpiresAt` tightening |
| `internal/domain/checkout/repository.go` | Modify | Add `MarkCompleted` to interface |
| `internal/repository/ent/checkout_session.go` | Modify | Implement `MarkCompleted` |
| `internal/ee/service/checkout_session.go` | Modify | Revise `CompleteCheckoutSession` (atomic); revise `CleanupCheckoutSession` (guard + expired) |
| `internal/api/v1/webhook.go` | Modify | Add `checkoutSessionService` to `WebhookHandler`; wire into all `ServiceDependencies{}` literals |
| `internal/integration/stripe/webhook/handler.go` | Modify | `handleCheckoutSessionCompleted` — EntityIntegrationMapping lookup → `CompleteCheckoutSession`, metadata fallback |
| `internal/integration/razorpay/webhook/handler.go` | Modify | Add `payment_link.paid` handler |
| `internal/integration/nomod/webhook/handler.go` | Modify | `handlePaymentLinkPayment` — EntityIntegrationMapping lookup → `CompleteCheckoutSession` |
| `internal/integration/moyasar/webhook/handler.go` | Modify | `handlePaymentPaid` — EntityIntegrationMapping pre-check → `CompleteCheckoutSession` |
| `cmd/server/main.go` | Modify | Pass `checkoutSessionService` to `NewWebhookHandler` |
| `internal/temporal/models/cron.go` | Modify | Add `CheckoutSessionExpiryWorkflowInput` and `CheckoutSessionExpiryWorkflowResult` |
| `internal/temporal/workflows/cron/checkout_session_expiry_workflow.go` | Create | Temporal workflow |
| `internal/temporal/activities/cron/checkout_session_expiry_activities.go` | Create | `ExpireCheckoutSessionsActivity` |
| `internal/temporal/registration.go` | Modify | Register workflow + activity on cron task queue |
| `internal/temporal/service/schedules.go` | Modify | Add schedule config entry |

---

## Task 1: Type Foundation — flat CheckoutProviderResult, PaymentAction, enum expansion, new webhook event

**Files:**
- Modify: `internal/types/checkout.go`
- Modify: `internal/types/checkout_configuration.go`
- Modify: `internal/types/webhook.go`

- [ ] **Step 1: Add `PaymentAction` struct to `internal/types/checkout.go`**

Add after the `PaymentActionType` block (after line 112):

```go
// PaymentAction is the customer-facing next step to complete payment.
// Surfaced in CheckoutSessionResponse; the full CheckoutProviderResult is never exposed.
type PaymentAction struct {
	Type PaymentActionType `json:"type"`
	URL  string            `json:"url"`
}
```

- [ ] **Step 2: Expand `CheckoutPaymentProvider` enum in the same file**

Replace the existing `CheckoutPaymentProvider` const block and its `Validate()` and `SessionExpiry()` methods with:

```go
type CheckoutPaymentProvider string

const (
	CheckoutPaymentProviderStripe   CheckoutPaymentProvider = "stripe"
	CheckoutPaymentProviderRazorpay CheckoutPaymentProvider = "razorpay"
	CheckoutPaymentProviderNomod    CheckoutPaymentProvider = "nomod"
	CheckoutPaymentProviderMoyasar  CheckoutPaymentProvider = "moyasar"
)

func (p CheckoutPaymentProvider) String() string { return string(p) }

func (p CheckoutPaymentProvider) Validate() error {
	allowed := []CheckoutPaymentProvider{
		CheckoutPaymentProviderStripe,
		CheckoutPaymentProviderRazorpay,
		CheckoutPaymentProviderNomod,
		CheckoutPaymentProviderMoyasar,
	}
	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid checkout payment provider").
			WithHint("Allowed values: stripe, razorpay, nomod, moyasar").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SessionExpiry returns the default lifetime for a checkout session with this provider.
func (p CheckoutPaymentProvider) SessionExpiry() time.Duration {
	switch p {
	case CheckoutPaymentProviderStripe:
		return 24 * time.Hour
	case CheckoutPaymentProviderNomod:
		return 30 * time.Minute
	default:
		return 15 * time.Minute
	}
}
```

- [ ] **Step 3: Replace `CheckoutProviderResult` in `internal/types/checkout_configuration.go`**

Remove the existing `CheckoutProviderResult` and `ProviderSubscriptionResult` structs (the bottom section of the file after the JSONB result structs comment at line 86), and replace with:

```go
// CheckoutProviderResult is the flat, action-agnostic provider response stored in
// checkout_sessions.provider_result. It is never serialized to API callers directly —
// use PaymentActionForUser() to extract the safe-to-expose action.
type CheckoutProviderResult struct {
	// NextAction is what the customer must do to complete payment.
	NextAction *PaymentAction `json:"next_action,omitempty"`

	// ProviderSessionID is stored in EntityIntegrationMapping at link creation.
	//   Stripe:   Checkout Session ID  (cs_xxx)
	//   Razorpay: Payment Link ID      (plink_xxx)
	//   Nomod:    Payment Link ID      (NOTE: webhook uses Charge ID; look up by PaymentLinkID field)
	//   Moyasar:  Payment ID
	ProviderSessionID string `json:"provider_session_id,omitempty"`

	// ProviderPaymentIntentID is the provider-side charge/intent ID.
	// Stripe returns this at link creation (pi_xxx); others populate it from the webhook payload.
	ProviderPaymentIntentID string `json:"provider_payment_intent_id,omitempty"`

	// ExpiresAt is the provider URL expiry. When set and earlier than the session expiry,
	// executeCheckoutAction tightens the session expiry to match.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// ProviderMetadata holds provider-specific data not needed for business logic.
	ProviderMetadata map[string]string `json:"provider_metadata,omitempty"`
}

// PaymentActionForUser extracts the safe-to-expose action from a provider result.
// All other fields in CheckoutProviderResult are sensitive gateway data.
func (r *CheckoutProviderResult) PaymentActionForUser() *PaymentAction {
	if r == nil {
		return nil
	}
	return r.NextAction
}
```

Make sure to add `"time"` to the import block if it is not already there.

- [ ] **Step 4: Add `WebhookEventCheckoutSessionExpired` to `internal/types/webhook.go`**

Find the existing checkout event names block and add:

```go
// checkout event names
const (
	WebhookEventCheckoutSessionInitiated WebhookEventName = "checkout.session.initiated"
	WebhookEventCheckoutSessionCompleted WebhookEventName = "checkout.session.completed"
	WebhookEventCheckoutSessionFailed    WebhookEventName = "checkout.session.failed"
	WebhookEventCheckoutSessionExpired   WebhookEventName = "checkout.session.expired"
)
```

(These constants already exist for the first three — just add `CheckoutSessionExpired` alongside them. If they're in a combined const block, add only the missing constant.)

- [ ] **Step 5: Verify it compiles**

```bash
go build ./internal/types/...
```

Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add internal/types/checkout.go internal/types/checkout_configuration.go internal/types/webhook.go
git commit -m "feat(checkout): flatten CheckoutProviderResult, add PaymentAction type and enum expansion"
```

---

## Task 2: DTO — remove duplicate PaymentAction, simplify ToCheckoutSessionResponse

**Files:**
- Modify: `internal/api/dto/checkout_session.go`

- [ ] **Step 1: Remove the `PaymentAction` struct from the dto file**

In `internal/api/dto/checkout_session.go`, delete these lines (around line 93–97):

```go
// PaymentAction is derived from ProviderResult at response-build time; never stored.
type PaymentAction struct {
	Type types.PaymentActionType `json:"type"`
	URL  string                  `json:"url"`
}
```

- [ ] **Step 2: Update `CheckoutSessionResponse` to use `types.PaymentAction`**

Change the `PaymentAction` field type (line ~100):

```go
// CheckoutSessionResponse is the API response for a single checkout session.
type CheckoutSessionResponse struct {
	*domainCheckout.CheckoutSession
	PaymentAction *types.PaymentAction `json:"payment_action,omitempty"`
}
```

- [ ] **Step 3: Replace `ToCheckoutSessionResponse`**

Replace the existing function body (lines 109–121) with:

```go
// ToCheckoutSessionResponse maps a domain session to its API response.
// PaymentAction is derived from ProviderResult; the raw ProviderResult is zeroed
// before serialization because it contains sensitive gateway tokens.
func ToCheckoutSessionResponse(s *domainCheckout.CheckoutSession) *CheckoutSessionResponse {
	resp := &CheckoutSessionResponse{CheckoutSession: s}
	if s.ProviderResult != nil {
		resp.PaymentAction = (*types.CheckoutProviderResult)(s.ProviderResult).PaymentActionForUser()
		// Zero out provider_result — not safe to expose to API callers.
		resp.CheckoutSession.ProviderResult = nil
	}
	return resp
}
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./internal/api/dto/...
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/api/dto/checkout_session.go
git commit -m "feat(checkout): simplify ToCheckoutSessionResponse using PaymentActionForUser"
```

---

## Task 3: CheckoutProvider Interface

**Files:**
- Create: `internal/interfaces/checkout_provider.go`

- [ ] **Step 1: Create the interface file**

```go
package interfaces

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutProvider is implemented by each payment gateway that supports hosted checkout.
// Adapters wrap the existing provider PaymentService.CreatePaymentLink() without modifying it.
type CheckoutProvider interface {
	CreatePaymentLink(
		ctx context.Context,
		req CheckoutProviderRequest,
		customerSvc CustomerService,
		invoiceSvc InvoiceService,
	) (*CheckoutProviderResponse, error)
}

// CheckoutProviderRequest is the unified input for all checkout provider adapters.
type CheckoutProviderRequest struct {
	InvoiceID     string
	CustomerID    string
	Amount        string // decimal string, e.g. "99.00"
	Currency      string
	PaymentID     string // FlexPrice payment ID — embedded in provider metadata for idempotency
	EnvironmentID string
	SuccessURL    string
	FailureURL    string
	CancelURL     string
	Metadata      map[string]string
}

// CheckoutProviderResponse is the unified output from all checkout provider adapters.
type CheckoutProviderResponse struct {
	ProviderSessionID       string             // stored in EntityIntegrationMapping
	NextAction              types.PaymentAction // type + URL for the customer
	ProviderPaymentIntentID string             // charge/intent ID, stored after payment confirmation
	ExpiresAt               *time.Time         // nil if provider doesn't return expiry
	ProviderMetadata        map[string]string  // debug data only, not for business logic
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/interfaces/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/interfaces/checkout_provider.go
git commit -m "feat(checkout): add CheckoutProvider interface"
```

---

## Task 4: Stripe Checkout Adapter

**Files:**
- Create: `internal/integration/stripe/checkout_adapter.go`

The existing `stripe.PaymentService.CreatePaymentLink` signature is:
```go
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req *dto.CreateStripePaymentLinkRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*dto.StripePaymentLinkResponse, error)
```
`dto.StripePaymentLinkResponse` has fields: `ID` (cs_xxx), `PaymentURL`, `PaymentIntentID`.

`dto.CreateStripePaymentLinkRequest` fields: `InvoiceID`, `CustomerID`, `Amount` (decimal), `Currency`, `SuccessURL`, `CancelURL`, `Metadata`, `EnvironmentID`, `PaymentID`.

- [ ] **Step 1: Create the adapter**

```go
package stripe

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutAdapter wraps stripe.PaymentService behind the CheckoutProvider interface.
type CheckoutAdapter struct {
	Svc *PaymentService
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
	customerSvc interfaces.CustomerService,
	invoiceSvc interfaces.InvoiceService,
) (*interfaces.CheckoutProviderResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.CreatePaymentLink(ctx, &dto.CreateStripePaymentLinkRequest{
		InvoiceID:     req.InvoiceID,
		CustomerID:    req.CustomerID,
		Amount:        amount,
		Currency:      req.Currency,
		SuccessURL:    req.SuccessURL,
		CancelURL:     req.CancelURL,
		Metadata:      req.Metadata,
		EnvironmentID: req.EnvironmentID,
		PaymentID:     req.PaymentID,
	}, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID:       r.ID,
		NextAction:              types.PaymentAction{Type: types.PaymentActionTypeCheckoutURL, URL: r.PaymentURL},
		ProviderPaymentIntentID: r.PaymentIntentID,
	}, nil
}
```

- [ ] **Step 2: Verify**

```bash
go build ./internal/integration/stripe/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/stripe/checkout_adapter.go
git commit -m "feat(checkout): add Stripe checkout adapter"
```

---

## Task 5: Razorpay Checkout Adapter

**Files:**
- Create: `internal/integration/razorpay/checkout_adapter.go`

Razorpay's `PaymentService.CreatePaymentLink` signature:
```go
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req *CreatePaymentLinkRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*RazorpayPaymentLinkResponse, error)
```
`CreatePaymentLinkRequest` fields: `InvoiceID`, `CustomerID`, `Amount` (decimal), `Currency`, `SuccessURL`, `CancelURL`, `Metadata`, `PaymentID`, `EnvironmentID`.
`RazorpayPaymentLinkResponse` fields: `ID` (plink_xxx), `PaymentURL`, `IsRazorpayInvoiceLink`.

- [ ] **Step 1: Create the adapter**

```go
package razorpay

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutAdapter wraps razorpay.PaymentService behind the CheckoutProvider interface.
type CheckoutAdapter struct {
	Svc *PaymentService
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
	customerSvc interfaces.CustomerService,
	invoiceSvc interfaces.InvoiceService,
) (*interfaces.CheckoutProviderResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.CreatePaymentLink(ctx, &CreatePaymentLinkRequest{
		InvoiceID:     req.InvoiceID,
		CustomerID:    req.CustomerID,
		Amount:        amount,
		Currency:      req.Currency,
		SuccessURL:    req.SuccessURL,
		CancelURL:     req.CancelURL,
		Metadata:      req.Metadata,
		PaymentID:     req.PaymentID,
		EnvironmentID: req.EnvironmentID,
	}, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: r.ID,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: r.PaymentURL},
	}, nil
}
```

- [ ] **Step 2: Verify**

```bash
go build ./internal/integration/razorpay/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/razorpay/checkout_adapter.go
git commit -m "feat(checkout): add Razorpay checkout adapter"
```

---

## Task 6: Nomod Checkout Adapter

**Files:**
- Create: `internal/integration/nomod/checkout_adapter.go`

Nomod's `PaymentService.CreatePaymentLink` signature:
```go
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req CreatePaymentLinkReq, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*CreatePaymentLinkResp, error)
```
`CreatePaymentLinkReq` fields: `InvoiceID`, `CustomerID`, `Amount` (decimal), `Currency`, `SuccessURL`, `FailureURL`, `Metadata`, `PaymentID`, `EnvironmentID`.
`CreatePaymentLinkResp` fields: `ID` (payment link ID), `PaymentURL`, `ReferenceID`.

- [ ] **Step 1: Create the adapter**

```go
package nomod

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutAdapter wraps nomod.PaymentService behind the CheckoutProvider interface.
type CheckoutAdapter struct {
	Svc *PaymentService
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
	customerSvc interfaces.CustomerService,
	invoiceSvc interfaces.InvoiceService,
) (*interfaces.CheckoutProviderResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.CreatePaymentLink(ctx, CreatePaymentLinkReq{
		InvoiceID:     req.InvoiceID,
		CustomerID:    req.CustomerID,
		Amount:        amount,
		Currency:      req.Currency,
		SuccessURL:    req.SuccessURL,
		FailureURL:    req.FailureURL,
		Metadata:      req.Metadata,
		PaymentID:     req.PaymentID,
		EnvironmentID: req.EnvironmentID,
	}, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}
	providerMetadata := map[string]string{}
	if r.ReferenceID != "" {
		providerMetadata["reference_id"] = r.ReferenceID
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: r.ID,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: r.PaymentURL},
		ProviderMetadata:  providerMetadata,
	}, nil
}
```

- [ ] **Step 2: Verify**

```bash
go build ./internal/integration/nomod/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/nomod/checkout_adapter.go
git commit -m "feat(checkout): add Nomod checkout adapter"
```

---

## Task 7: Moyasar Checkout Adapter

**Files:**
- Create: `internal/integration/moyasar/checkout_adapter.go`

Moyasar's `PaymentService.CreatePaymentLink` signature:
```go
func (s *PaymentService) CreatePaymentLink(ctx context.Context, req CreatePaymentLinkRequest, customerService interfaces.CustomerService, invoiceService interfaces.InvoiceService) (*CreatePaymentLinkResponse, error)
```
`CreatePaymentLinkRequest` fields: `InvoiceID`, `CustomerID`, `Amount` (decimal), `Currency`, `SuccessURL`, `CancelURL`, `Metadata`, `PaymentID`, `EnvironmentID`.
`CreatePaymentLinkResponse` fields: `ID` (Moyasar payment ID), `PaymentURL`, `PaymentID`.

- [ ] **Step 1: Create the adapter**

```go
package moyasar

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutAdapter wraps moyasar.PaymentService behind the CheckoutProvider interface.
type CheckoutAdapter struct {
	Svc *PaymentService
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
	customerSvc interfaces.CustomerService,
	invoiceSvc interfaces.InvoiceService,
) (*interfaces.CheckoutProviderResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.CreatePaymentLink(ctx, CreatePaymentLinkRequest{
		InvoiceID:     req.InvoiceID,
		CustomerID:    req.CustomerID,
		Amount:        amount,
		Currency:      req.Currency,
		SuccessURL:    req.SuccessURL,
		CancelURL:     req.CancelURL,
		Metadata:      req.Metadata,
		PaymentID:     req.PaymentID,
		EnvironmentID: req.EnvironmentID,
	}, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}
	// For Moyasar, the payment ID is also the charge ID used in webhooks.
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID:       r.ID,
		NextAction:              types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: r.PaymentURL},
		ProviderPaymentIntentID: r.ID,
	}, nil
}
```

- [ ] **Step 2: Verify**

```bash
go build ./internal/integration/moyasar/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/moyasar/checkout_adapter.go
git commit -m "feat(checkout): add Moyasar checkout adapter"
```

---

## Task 8: Factory Method — GetCheckoutProvider

**Files:**
- Modify: `internal/integration/factory.go`

The factory file already has `GetStripeIntegration`, `GetRazorpayIntegration`, `GetNomodIntegration`, `GetMoyasarIntegration` methods. Each returns a struct with a `PaymentSvc *PaymentService` field.

- [ ] **Step 1: Add the import for the interfaces package**

In `internal/integration/factory.go`, ensure `"github.com/flexprice/flexprice/internal/interfaces"` is in the import block, and the four provider adapter sub-packages are imported:

```go
import (
    // ... existing imports ...
    "github.com/flexprice/flexprice/internal/interfaces"
    stripeint "github.com/flexprice/flexprice/internal/integration/stripe"
    razorpayint "github.com/flexprice/flexprice/internal/integration/razorpay"
    nomodint "github.com/flexprice/flexprice/internal/integration/nomod"
    moyasarint "github.com/flexprice/flexprice/internal/integration/moyasar"
    ierr "github.com/flexprice/flexprice/internal/errors"
)
```

Note: `stripe`, `razorpay`, `nomod`, `moyasar` are likely already imported. Use the same import alias already in use; only add what's missing.

- [ ] **Step 2: Add `GetCheckoutProvider` method**

Add at the end of `factory.go` before the final closing brace:

```go
// GetCheckoutProvider returns the CheckoutProvider adapter for the given payment provider.
// Returns ErrValidation for providers that do not support hosted checkout (Paddle, Whop).
func (f *Factory) GetCheckoutProvider(ctx context.Context, provider types.CheckoutPaymentProvider) (interfaces.CheckoutProvider, error) {
	switch provider {
	case types.CheckoutPaymentProviderStripe:
		i, err := f.GetStripeIntegration(ctx)
		if err != nil {
			return nil, err
		}
		return &stripe.CheckoutAdapter{Svc: i.PaymentSvc}, nil
	case types.CheckoutPaymentProviderRazorpay:
		i, err := f.GetRazorpayIntegration(ctx)
		if err != nil {
			return nil, err
		}
		return &razorpay.CheckoutAdapter{Svc: i.PaymentSvc}, nil
	case types.CheckoutPaymentProviderNomod:
		i, err := f.GetNomodIntegration(ctx)
		if err != nil {
			return nil, err
		}
		return &nomod.CheckoutAdapter{Svc: i.PaymentSvc}, nil
	case types.CheckoutPaymentProviderMoyasar:
		i, err := f.GetMoyasarIntegration(ctx)
		if err != nil {
			return nil, err
		}
		return &moyasar.CheckoutAdapter{Svc: i.PaymentSvc}, nil
	default:
		return nil, ierr.NewError("payment provider not supported for checkout").
			WithHintf("%s does not support hosted checkout sessions", provider).
			Mark(ierr.ErrValidation)
	}
}
```

Important: use the import aliases that are already in the file for each provider package. Check the existing import block and match. The adapter struct `CheckoutAdapter` is in the provider's own package, so if stripe is imported as `"github.com/flexprice/flexprice/internal/integration/stripe"` you'd write `stripe.CheckoutAdapter{}`. Look at how `GetStripeIntegration` returns `*StripeIntegration` — the PaymentSvc field name may differ. Check the struct definition for each integration.

To find the `PaymentSvc` field name, run:
```bash
grep -n "PaymentSvc\|PaymentService\b" internal/integration/factory.go | head -20
```

Use whatever field name the factory already uses.

- [ ] **Step 3: Verify**

```bash
go build ./internal/integration/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/factory.go
git commit -m "feat(checkout): add GetCheckoutProvider factory method"
```

---

## Task 9: Wire Provider Call in executeCheckoutAction

**Files:**
- Modify: `internal/ee/service/checkout_session_actions.go`
- Modify: `internal/ee/service/checkout_session.go` (fix `completeSubscriptionCheckout` provider result field)

`executeCheckoutAction` currently ends with:
```go
session.CheckoutStatus = types.CheckoutStatusPending
// ...
return s.CheckoutSessionRepo.Update(ctx, session)
```

The spec adds provider call + EntityIntegrationMapping + ExpiresAt tightening between `createCheckoutPayment` and `CheckoutSessionRepo.Update`.

- [ ] **Step 1: Add necessary imports to `checkout_session_actions.go`**

Ensure these packages are imported:
```go
import (
    "context"
    "time"

    "github.com/flexprice/flexprice/internal/api/dto"
    domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
    domainMapping "github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/interfaces"
    "github.com/flexprice/flexprice/internal/types"
)
```

- [ ] **Step 2: Replace `executeCheckoutAction` with the provider-wired version**

Replace the entire function body in `checkout_session_actions.go`:

```go
func (s *checkoutSessionService) executeCheckoutAction(ctx context.Context, session *domainCheckout.CheckoutSession) error {
	s.Logger.Debug(ctx, "executeCheckoutAction: starting", "session_id", session.ID, "action", session.Action)
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		s.Logger.Debug(ctx, "executeCheckoutAction: creating draft subscription", "session_id", session.ID)
		subResp, invResp, err := s.createDraftSubscription(ctx, session)
		if err != nil {
			s.Logger.Error(ctx, "executeCheckoutAction: createDraftSubscription failed", "session_id", session.ID, "error", err)
			return err
		}
		s.Logger.Debug(ctx, "executeCheckoutAction: draft subscription created", "sub_id", subResp.ID, "inv_id", invResp.ID)

		result := types.CheckoutResult{
			CreateSubscriptionResult: &types.CreateSubscriptionResult{
				SubscriptionID: subResp.ID,
				InvoiceID:      invResp.ID,
			},
		}
		session.Result = (*domainCheckout.JSONBCheckoutResult)(&result)

		payResp, err := s.createCheckoutPayment(ctx, &invResp.Invoice, session.PaymentProvider)
		if err != nil {
			return err
		}
		result.CreateSubscriptionResult.PaymentID = payResp.ID
		session.CheckoutInvoiceID = &invResp.ID
		session.CheckoutPaymentID = &payResp.ID

		// Contact the payment gateway and get the hosted checkout URL.
		providerResult, err := s.callCheckoutProvider(ctx, session, &invResp.Invoice, payResp)
		if err != nil {
			return err
		}
		session.ProviderResult = (*domainCheckout.JSONBCheckoutProviderResult)(providerResult)
		session.CheckoutStatus = types.CheckoutStatusPending

	default:
		return ierr.NewError("unsupported checkout action").
			WithHint("No fulfillment handler for this action type").
			WithReportableDetails(map[string]any{"action": session.Action}).
			Mark(ierr.ErrValidation)
	}

	return s.CheckoutSessionRepo.Update(ctx, session)
}

// callCheckoutProvider contacts the payment gateway, tightens ExpiresAt if the provider URL
// expires sooner, and records an EntityIntegrationMapping (ProviderSessionID → FlexPrice PaymentID).
func (s *checkoutSessionService) callCheckoutProvider(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
	inv interface{ GetID() string },
	payResp *dto.PaymentResponse,
) (*types.CheckoutProviderResult, error) {
	provider, err := s.IntegrationFactory.GetCheckoutProvider(ctx, session.PaymentProvider)
	if err != nil {
		return nil, err
	}

	customerSvc := NewCustomerService(s.ServiceParams)
	invoiceSvc := NewInvoiceService(s.ServiceParams)

	req := interfaces.CheckoutProviderRequest{
		InvoiceID:     *session.CheckoutInvoiceID,
		CustomerID:    session.CustomerID,
		Amount:        payResp.Amount.String(),
		Currency:      payResp.Currency,
		PaymentID:     payResp.ID,
		EnvironmentID: session.EnvironmentID,
		Metadata:      map[string]string(session.Metadata),
	}
	if session.SuccessURL != nil {
		req.SuccessURL = *session.SuccessURL
	}
	if session.FailureURL != nil {
		req.FailureURL = *session.FailureURL
	}
	if session.CancelURL != nil {
		req.CancelURL = *session.CancelURL
	}

	resp, err := provider.CreatePaymentLink(ctx, req, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}

	// Tighten session expiry if the provider URL expires sooner.
	if resp.ExpiresAt != nil && resp.ExpiresAt.Before(session.ExpiresAt) {
		session.ExpiresAt = *resp.ExpiresAt
	}

	// Record ProviderSessionID → FlexPrice PaymentID so incoming webhooks can route back.
	mapping := &domainMapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         payResp.ID,
		EntityType:       types.IntegrationEntityTypePayment,
		ProviderType:     session.PaymentProvider.String(),
		ProviderEntityID: resp.ProviderSessionID,
		EnvironmentID:    session.EnvironmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err := s.EntityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	return &types.CheckoutProviderResult{
		NextAction:              &resp.NextAction,
		ProviderSessionID:       resp.ProviderSessionID,
		ProviderPaymentIntentID: resp.ProviderPaymentIntentID,
		ExpiresAt:               resp.ExpiresAt,
		ProviderMetadata:        resp.ProviderMetadata,
	}, nil
}
```

Note: `inv interface{ GetID() string }` may need to be replaced with the actual invoice type. Check `invResp.Invoice` type — it's `invoice.Invoice`. Change the signature to `inv *invoice.Invoice` if needed and import `"github.com/flexprice/flexprice/internal/domain/invoice"`. The `inv` parameter is not actually used in the body (amount comes from `payResp`); remove it from the signature and the call site if that's the case.

- [ ] **Step 3: Fix `completeSubscriptionCheckout` to use flat ProviderResult**

In `internal/ee/service/checkout_session.go`, find `completeSubscriptionCheckout` (currently at line 67). The block that reads `providerResult.CreateSubscriptionResult.PaymentIntentID` needs to be updated:

Replace:
```go
if providerResult != nil && providerResult.CreateSubscriptionResult != nil {
    if id := providerResult.CreateSubscriptionResult.PaymentIntentID; id != "" {
        updateReq.GatewayPaymentID = &id
    }
}
```

With:
```go
if providerResult != nil && providerResult.ProviderPaymentIntentID != "" {
    id := providerResult.ProviderPaymentIntentID
    updateReq.GatewayPaymentID = &id
}
```

- [ ] **Step 4: Verify**

```bash
go build ./internal/ee/service/...
```

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/ee/service/checkout_session_actions.go internal/ee/service/checkout_session.go
git commit -m "feat(checkout): wire provider call and EntityIntegrationMapping in executeCheckoutAction"
```

---

## Task 10: MarkCompleted Repository Method

**Files:**
- Modify: `internal/domain/checkout/repository.go`
- Modify: `internal/repository/ent/checkout_session.go`

- [ ] **Step 1: Add `MarkCompleted` to the Repository interface**

In `internal/domain/checkout/repository.go`, add after the `Delete` method:

```go
// MarkCompleted atomically transitions the session from pending/initiated to completed.
// Returns (true, nil) if this call claimed the transition.
// Returns (false, nil) if the session was already in a terminal state — idempotent no-op.
// Never returns an error for the already-terminal case.
MarkCompleted(ctx context.Context, sessionID string, completedAt time.Time, providerResult *types.CheckoutProviderResult) (bool, error)
```

Add `"time"` to the import if not present.

- [ ] **Step 2: Implement `MarkCompleted` in the Ent repository**

In `internal/repository/ent/checkout_session.go`, add at the end of the file (before the query-options section):

```go
func (r *checkoutSessionRepository) MarkCompleted(ctx context.Context, sessionID string, completedAt time.Time, providerResult *types.CheckoutProviderResult) (bool, error) {
	r.log.Debug(ctx, "marking checkout session completed", "id", sessionID)

	span := StartRepositorySpan(ctx, "checkout_session", "mark_completed", map[string]interface{}{"id": sessionID})
	defer FinishSpan(span)

	n, err := r.client.Writer(ctx).CheckoutSession.Update().
		Where(
			entCheckout.ID(sessionID),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
			entCheckout.CheckoutStatusIn(types.CheckoutStatusPending, types.CheckoutStatusInitiated),
		).
		SetCheckoutStatus(types.CheckoutStatusCompleted).
		SetCompletedAt(completedAt).
		SetProviderResult(providerResult).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return false, ierr.WithError(err).WithHint("failed to mark checkout session completed").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return n > 0, nil
}
```

- [ ] **Step 3: Verify**

```bash
go build ./internal/domain/checkout/... ./internal/repository/ent/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/checkout/repository.go internal/repository/ent/checkout_session.go
git commit -m "feat(checkout): add MarkCompleted atomic conditional UPDATE to checkout session repository"
```

---

## Task 11: Revise CompleteCheckoutSession — Atomic Idempotency

**Files:**
- Modify: `internal/ee/service/checkout_session.go`

The current `CompleteCheckoutSession` does a GET then status check then `completeCheckoutAction` then `Update`. This races on duplicate webhooks. Replace with the `MarkCompleted`-based flow.

- [ ] **Step 1: Replace `CompleteCheckoutSession` with the atomic version**

In `internal/ee/service/checkout_session.go`, replace the entire `CompleteCheckoutSession` method:

```go
func (s *checkoutSessionService) CompleteCheckoutSession(ctx context.Context, sessionID string, providerResult *types.CheckoutProviderResult) error {
	if sessionID == "" {
		return ierr.NewError("session ID is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Fetch session for completeCheckoutAction context (subscription/invoice/payment IDs).
	session, err := s.CheckoutSessionRepo.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// Fast-path guard: already in a terminal state — nothing to do.
	switch session.CheckoutStatus {
	case types.CheckoutStatusCompleted, types.CheckoutStatusFailed, types.CheckoutStatusExpired:
		return ierr.NewError("checkout session already in terminal state").
			WithHintf("session %s is %s", sessionID, session.CheckoutStatus).
			Mark(ierr.ErrConflict)
	}

	// Run sub-steps idempotently before claiming the session.
	// Safe to run in parallel with a duplicate webhook — each step is conditional.
	if err := s.completeCheckoutAction(ctx, session, providerResult); err != nil {
		return err
	}

	// Atomic claim: only one concurrent caller gets n > 0.
	now := time.Now().UTC()
	claimed, err := s.CheckoutSessionRepo.MarkCompleted(ctx, sessionID, now, providerResult)
	if err != nil {
		return err
	}
	if !claimed {
		// Another process completed it simultaneously — idempotent no-op.
		return ierr.NewError("checkout session already completed by concurrent request").
			WithHintf("session %s was claimed by another process", sessionID).
			Mark(ierr.ErrConflict)
	}

	session.CheckoutStatus = types.CheckoutStatusCompleted
	session.CompletedAt = &now
	s.publishCheckoutEvent(ctx, dto.ToCheckoutSessionResponse(session), types.WebhookEventCheckoutSessionCompleted)
	return nil
}
```

- [ ] **Step 2: Make `completeSubscriptionCheckout` idempotent**

The existing `completeSubscriptionCheckout` directly sets subscription status and calls FinalizeInvoice without guards. Replace with idempotent versions.

In `checkout_session_actions.go`, replace `completeSubscriptionCheckout`:

```go
func (s *checkoutSessionService) completeSubscriptionCheckout(ctx context.Context, session *domainCheckout.CheckoutSession, providerResult *types.CheckoutProviderResult) error {
	if session.Result == nil || session.Result.CreateSubscriptionResult == nil {
		return ierr.NewError("session has no fulfillment result").
			WithHint("checkout session must have been fulfilled before it can be completed").
			Mark(ierr.ErrValidation)
	}
	res := session.Result.CreateSubscriptionResult

	// 1. Activate subscription: only update if still in draft.
	sub, err := s.SubRepo.Get(ctx, res.SubscriptionID)
	if err != nil {
		return err
	}
	if sub.SubscriptionStatus == types.SubscriptionStatusDraft {
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		if err := s.SubRepo.Update(ctx, sub); err != nil {
			return err
		}
	}

	// 2. Finalize the draft invoice (idempotent: check status first).
	invSvc := NewInvoiceService(s.ServiceParams)
	invResp, err := invSvc.GetInvoice(ctx, res.InvoiceID)
	if err != nil {
		return err
	}
	if invResp.InvoiceStatus != types.InvoiceStatusFinalized {
		if err := invSvc.FinalizeInvoice(ctx, res.InvoiceID); err != nil {
			return err
		}
	}

	// 3. Mark the checkout payment as SUCCEEDED, storing the gateway payment ID.
	statusStr := string(types.PaymentStatusSucceeded)
	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &statusStr,
		SucceededAt:   &now,
	}
	if providerResult != nil && providerResult.ProviderPaymentIntentID != "" {
		id := providerResult.ProviderPaymentIntentID
		updateReq.GatewayPaymentID = &id
	}
	paySvc := NewPaymentService(s.ServiceParams)
	if _, err := paySvc.UpdatePayment(ctx, res.PaymentID, updateReq); err != nil {
		return err
	}

	// 4. Reconcile invoice payment status (marks invoice as paid — already idempotent).
	return invSvc.ReconcilePaymentStatus(ctx, res.InvoiceID, types.PaymentStatusSucceeded, nil)
}
```

- [ ] **Step 3: Verify**

```bash
go build ./internal/ee/service/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/ee/service/checkout_session.go internal/ee/service/checkout_session_actions.go
git commit -m "feat(checkout): make CompleteCheckoutSession atomically idempotent via MarkCompleted"
```

---

## Task 12: Revise CleanupCheckoutSession — Idempotency

**Files:**
- Modify: `internal/ee/service/checkout_session.go`

- [ ] **Step 1: Replace `CleanupCheckoutSession` with the idempotent version**

Replace the existing `CleanupCheckoutSession` method:

```go
func (s *checkoutSessionService) CleanupCheckoutSession(ctx context.Context, session *domainCheckout.CheckoutSession, reason error) error {
	// Guard: already in a terminal state — idempotent no-op.
	switch session.CheckoutStatus {
	case types.CheckoutStatusCompleted, types.CheckoutStatusFailed, types.CheckoutStatusExpired:
		return nil
	}

	// Archive all entities created during fulfillment.
	// Treat IsNotFound as success: partial failure in a prior cleanup run is safe to ignore.
	if session.Result != nil && session.Result.CreateSubscriptionResult != nil {
		res := session.Result.CreateSubscriptionResult
		if res.PaymentID != "" {
			if err := s.PaymentRepo.Delete(ctx, res.PaymentID); err != nil && !ierr.IsNotFound(err) {
				s.Logger.Error(ctx, "failed to archive checkout payment", "payment_id", res.PaymentID, "error", err)
			}
		}
		if res.InvoiceID != "" {
			if err := s.InvoiceRepo.Delete(ctx, res.InvoiceID); err != nil && !ierr.IsNotFound(err) {
				s.Logger.Error(ctx, "failed to archive checkout invoice", "invoice_id", res.InvoiceID, "error", err)
			}
		}
		if res.SubscriptionID != "" {
			if err := s.SubRepo.Delete(ctx, res.SubscriptionID); err != nil && !ierr.IsNotFound(err) {
				s.Logger.Error(ctx, "failed to archive checkout subscription", "subscription_id", res.SubscriptionID, "error", err)
			}
		}
	}

	// Terminal status depends on whether this is a natural expiry or an error.
	if reason != nil {
		session.CheckoutStatus = types.CheckoutStatusFailed
		msg := reason.Error()
		session.FailureReason = &msg
	} else {
		session.CheckoutStatus = types.CheckoutStatusExpired
	}

	if err := s.CheckoutSessionRepo.Update(ctx, session); err != nil {
		return err
	}

	// Publish the appropriate lifecycle webhook.
	resp := dto.ToCheckoutSessionResponse(session)
	if reason != nil {
		s.publishCheckoutEvent(ctx, resp, types.WebhookEventCheckoutSessionFailed)
	} else {
		s.publishCheckoutEvent(ctx, resp, types.WebhookEventCheckoutSessionExpired)
	}
	return nil
}
```

- [ ] **Step 2: Verify**

```bash
go build ./internal/ee/service/...
```

Expected: no output.

- [ ] **Step 3: Run existing checkout tests**

```bash
go test -v -race ./internal/ee/service -run TestCheckoutSession -count=1
```

Expected: all existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/ee/service/checkout_session.go
git commit -m "feat(checkout): make CleanupCheckoutSession idempotent with expired/failed status distinction"
```

---

## Task 13: Add CheckoutSessionService to ServiceDependencies and WebhookHandler

**Files:**
- Modify: `internal/interfaces/service.go`
- Modify: `internal/api/v1/webhook.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add `CheckoutSessionService` interface to `internal/interfaces/service.go`**

Add this interface before or after the `CreditAdjustmentService` interface, and add the field to `ServiceDependencies`:

```go
// CheckoutSessionService defines the minimal interface needed by webhook handlers.
type CheckoutSessionService interface {
	CompleteCheckoutSession(ctx context.Context, sessionID string, providerResult *types.CheckoutProviderResult) error
}
```

Then add to `ServiceDependencies` struct:
```go
type ServiceDependencies struct {
	CustomerService                 CustomerService
	PaymentService                  PaymentService
	InvoiceService                  InvoiceService
	PlanService                     PlanService
	SubscriptionService             SubscriptionService
	EntityIntegrationMappingService EntityIntegrationMappingService
	PriceUnitService                PriceUnitService
	CreditAdjustmentService         CreditAdjustmentService
	CheckoutSessionService          CheckoutSessionService  // ADD THIS
	DB                              postgres.IClient
}
```

Required imports in `service.go`: `"context"`, `"github.com/flexprice/flexprice/internal/types"` — check if already present.

- [ ] **Step 2: Add `checkoutSessionService` to `WebhookHandler` in `internal/api/v1/webhook.go`**

In the `WebhookHandler` struct, add the field:
```go
checkoutSessionService interfaces.CheckoutSessionService
```

In `NewWebhookHandler` constructor, add parameter and assignment:
```go
func NewWebhookHandler(
    cfg *config.Configuration,
    svixClient *svix.Client,
    logger *logger.Logger,
    integrationFactory *integration.Factory,
    customerService interfaces.CustomerService,
    paymentService interfaces.PaymentService,
    invoiceService interfaces.InvoiceService,
    planService interfaces.PlanService,
    subscriptionService interfaces.SubscriptionService,
    entityIntegrationMappingService interfaces.EntityIntegrationMappingService,
    checkoutSessionService interfaces.CheckoutSessionService,  // ADD
    db postgres.IClient,
    webhookService *flexwebhook.WebhookService,
) *WebhookHandler {
    return &WebhookHandler{
        // ...existing fields...
        checkoutSessionService: checkoutSessionService,  // ADD
    }
}
```

- [ ] **Step 3: Add `CheckoutSessionService` to every `ServiceDependencies{}` literal in `webhook.go`**

There are several places where `ServiceDependencies{}` is built. Find them with:
```bash
grep -n "ServiceDependencies{" internal/api/v1/webhook.go
```

For each occurrence (there are approximately 8), add:
```go
CheckoutSessionService: h.checkoutSessionService,
```

- [ ] **Step 4: Update `cmd/server/main.go` to pass `checkoutSessionService` to `NewWebhookHandler`**

Find the `NewWebhookHandler` call (line ~408) and add `checkoutSessionService` as a parameter in the right position (after `entityIntegrationMappingService`, before `db`):

```go
Webhook: v1.NewWebhookHandler(
    cfg, svixClient, logger, integrationFactory,
    customerService, paymentService, invoiceService,
    planService, subscriptionService, entityIntegrationMappingService,
    checkoutSessionService,  // ADD
    db, webhookService,
),
```

- [ ] **Step 5: Verify the whole binary compiles**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/interfaces/service.go internal/api/v1/webhook.go cmd/server/main.go
git commit -m "feat(checkout): add CheckoutSessionService to ServiceDependencies and WebhookHandler"
```

---

## Task 14: Stripe Webhook — EntityIntegrationMapping Routing

**Files:**
- Modify: `internal/integration/stripe/webhook/handler.go`

The existing `handleCheckoutSessionCompleted` (line 795) uses metadata lookup. Replace with EntityIntegrationMapping primary + metadata fallback.

- [ ] **Step 1: Replace `handleCheckoutSessionCompleted`**

Replace the function (lines 795–849) with:

```go
func (h *Handler) handleCheckoutSessionCompleted(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	var checkoutSession stripeapi.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
		h.logger.Error(ctx, "failed to parse checkout session from webhook, skipping event", "error", err, "event_id", event.ID)
		return nil
	}

	h.logger.Info(ctx, "received checkout.session.completed webhook",
		"checkout_session_id", checkoutSession.ID,
		"event_id", event.ID)

	// Primary path: EntityIntegrationMapping lookup (cs_xxx → FlexPrice payment ID).
	mappings, err := services.EntityIntegrationMappingService.ListEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{checkoutSession.ID},
			ProviderTypes:     []string{"stripe"},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err == nil && len(mappings.EntityIntegrationMappings) > 0 {
		mapping := mappings.EntityIntegrationMappings[0]
		sessions, sessErr := services.CheckoutSessionService.(interface {
			// We call CompleteCheckoutSession directly via the interface.
		})
		_ = sessions
		var providerPaymentIntentID string
		if checkoutSession.PaymentIntent != nil {
			providerPaymentIntentID = checkoutSession.PaymentIntent.ID
		}
		providerResult := &types.CheckoutProviderResult{
			ProviderPaymentIntentID: providerPaymentIntentID,
		}
		// Find the checkout session by payment ID.
		sessionFilter := &types.CheckoutSessionFilter{
			QueryFilter:        types.NewDefaultQueryFilter(),
			CheckoutPaymentIDs: []string{mapping.EntityID},
			CheckoutStatuses:   []types.CheckoutStatus{types.CheckoutStatusPending, types.CheckoutStatusInitiated},
		}
		checkoutSessions, csErr := services.EntityIntegrationMappingService.ListEntityIntegrationMappings(ctx, nil)
		_ = checkoutSessions
		_ = csErr
		// NOTE: use CheckoutSessionService directly — it holds the repo.
		// The handler needs access to CheckoutSession repo via the service.
		// Call CompleteCheckoutSession on the service.
		_ = sessionFilter
		if completeErr := services.CheckoutSessionService.CompleteCheckoutSession(ctx, mapping.EntityID, providerResult); completeErr != nil {
			if ierr.IsConflict(completeErr) {
				h.logger.Info(ctx, "checkout session already completed, skipping", "event_id", event.ID)
				return nil
			}
			h.logger.Error(ctx, "failed to complete checkout session", "error", completeErr, "event_id", event.ID)
			return completeErr
		}
		return nil
	}
```

**STOP** — the above approach has a flaw. `mapping.EntityID` is the FlexPrice payment ID, not the checkout session ID. We need to look up the checkout session by payment ID. `CheckoutSessionService` in `ServiceDependencies` only exposes `CompleteCheckoutSession(sessionID, ...)`, so we need the session ID.

The cleanest path: `CheckoutSessionService` needs a `GetByPaymentID` method, OR we make `EntityIntegrationMapping` store the session ID instead. Per the spec, the mapping stores `ProviderSessionID → FlexPrice PaymentID`. So we have the payment ID from the mapping, and need to find the checkout session.

Actually, re-reading the spec (Section 12 decision tree): the flow is:
1. `EntityIntegrationMappingRepo.List({ProviderEntityIDs:[id], ProviderType, EntityType:payment})`
2. Get FlexPrice payment via `mapping.EntityID`
3. `CheckoutSessionRepo.List({CheckoutPaymentID: payment.ID, Statuses: [pending, initiated]})`
4. → `CompleteCheckoutSession(ctx, session.ID, providerResult)`

So `CheckoutSessionService` needs to expose `List` (or a dedicated method). Add `List` to the `CheckoutSessionService` interface in `internal/interfaces/service.go`:

```go
type CheckoutSessionService interface {
    CompleteCheckoutSession(ctx context.Context, sessionID string, providerResult *types.CheckoutProviderResult) error
    List(ctx context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error)
}
```

Then update `internal/ee/service/checkout_session.go` to confirm `checkoutSessionService` implements this (it already has `List`).

After this fix, re-write `handleCheckoutSessionCompleted` properly:

```go
func (h *Handler) handleCheckoutSessionCompleted(ctx context.Context, event *stripeapi.Event, environmentID string, services *ServiceDependencies) error {
	var checkoutSession stripeapi.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
		h.logger.Error(ctx, "failed to parse checkout session from webhook, skipping event", "error", err, "event_id", event.ID)
		return nil
	}

	h.logger.Info(ctx, "received checkout.session.completed webhook",
		"checkout_session_id", checkoutSession.ID,
		"event_id", event.ID)

	var providerPaymentIntentID string
	if checkoutSession.PaymentIntent != nil {
		providerPaymentIntentID = checkoutSession.PaymentIntent.ID
	}

	// Primary path: EntityIntegrationMapping lookup (cs_xxx → FlexPrice payment ID).
	mappings, err := services.EntityIntegrationMappingService.ListEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{checkoutSession.ID},
			ProviderTypes:     []string{"stripe"},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err == nil && len(mappings.EntityIntegrationMappings) > 0 {
		paymentID := mappings.EntityIntegrationMappings[0].EntityID
		if routed := h.routeToCheckoutSession(ctx, paymentID, &types.CheckoutProviderResult{
			ProviderPaymentIntentID: providerPaymentIntentID,
		}, services); routed {
			return nil
		}
	}

	// Fallback: legacy path for sessions created before EntityIntegrationMapping was in place.
	flexpricePaymentID := checkoutSession.Metadata["flexprice_payment_id"]
	if flexpricePaymentID == "" {
		h.logger.Info(ctx, "no lookup succeeded for checkout session webhook, skipping", "event_id", event.ID)
		return nil
	}

	payment, err := services.PaymentService.GetPayment(ctx, flexpricePaymentID)
	if err != nil {
		h.logger.Error(ctx, "failed to get payment from database, skipping event", "error", err, "event_id", event.ID)
		return nil
	}
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Info(ctx, "payment already succeeded, skipping event", "event_id", event.ID)
		return nil
	}

	var paymentIntent *stripeapi.PaymentIntent
	if checkoutSession.PaymentIntent != nil {
		paymentIntent, err = h.paymentSvc.GetPaymentIntent(ctx, checkoutSession.PaymentIntent.ID, environmentID)
		if err != nil {
			h.logger.Error(ctx, "failed to fetch payment intent, continuing without it", "error", err, "event_id", event.ID)
			paymentIntent = nil
		}
	}
	return h.paymentSvc.HandleFlexPriceCheckoutPayment(ctx, paymentIntent, payment, services.CustomerService, services.InvoiceService, services.PaymentService)
}

// routeToCheckoutSession finds the active checkout session for a payment and completes it.
// Returns true if a checkout session was found and CompleteCheckoutSession was called (regardless of outcome).
func (h *Handler) routeToCheckoutSession(ctx context.Context, paymentID string, providerResult *types.CheckoutProviderResult, services *ServiceDependencies) bool {
	sessions, err := services.CheckoutSessionService.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:        types.NewDefaultQueryFilter(),
		CheckoutPaymentIDs: []string{paymentID},
		CheckoutStatuses:   []types.CheckoutStatus{types.CheckoutStatusPending, types.CheckoutStatusInitiated},
	})
	if err != nil || sessions == nil || len(sessions.Items) == 0 {
		return false
	}
	sessionID := sessions.Items[0].ID
	if err := services.CheckoutSessionService.CompleteCheckoutSession(ctx, sessionID, providerResult); err != nil {
		if ierr.IsConflict(err) {
			h.logger.Info(ctx, "checkout session already completed by concurrent request, skipping", "session_id", sessionID)
			return true
		}
		h.logger.Error(ctx, "failed to complete checkout session", "error", err, "session_id", sessionID)
		return true // still routed; don't fall through to legacy path
	}
	h.logger.Info(ctx, "checkout session completed via EntityIntegrationMapping routing", "session_id", sessionID)
	return true
}
```

Important: update `internal/interfaces/service.go` to add `List` to `CheckoutSessionService` before implementing this step.

- [ ] **Step 2: Add `List` to `CheckoutSessionService` interface in `internal/interfaces/service.go`**

```go
type CheckoutSessionService interface {
	CompleteCheckoutSession(ctx context.Context, sessionID string, providerResult *types.CheckoutProviderResult) error
	List(ctx context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error)
}
```

Add `"github.com/flexprice/flexprice/internal/api/dto"` to imports if needed.

- [ ] **Step 3: Verify the stripe handler compiles**

```bash
go build ./internal/integration/stripe/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/stripe/webhook/handler.go internal/interfaces/service.go
git commit -m "feat(checkout): Stripe webhook routes to CompleteCheckoutSession via EntityIntegrationMapping"
```

---

## Task 15: Razorpay Webhook — payment_link.paid Handler

**Files:**
- Modify: `internal/integration/razorpay/webhook/handler.go`

The Razorpay webhook currently handles `payment.captured` and `payment.failed`. Add a new `payment_link.paid` case.

- [ ] **Step 1: Add `payment_link.paid` to the event switch**

In `HandleWebhookEvent` (line ~62), find the switch statement and add a new case:

```go
case "payment_link.paid":
    return h.handlePaymentLinkPaid(ctx, event, environmentID, services)
```

- [ ] **Step 2: Add `handlePaymentLinkPaid` method**

Add at the end of the handler file:

```go
// handlePaymentLinkPaid processes Razorpay payment_link.paid webhook events for
// FlexPrice-initiated checkout sessions.
func (h *Handler) handlePaymentLinkPaid(ctx context.Context, event *RazorpayWebhookEvent, environmentID string, services *ServiceDependencies) error {
	paymentLinkID := event.Payload.PaymentLink.Entity.ID
	if paymentLinkID == "" {
		h.logger.Info(ctx, "payment_link.paid webhook missing payment_link ID, skipping", "event_id", event.EventID)
		return nil
	}

	var providerPaymentIntentID string
	if event.Payload.Payment != nil {
		providerPaymentIntentID = event.Payload.Payment.Entity.ID
	}

	h.logger.Info(ctx, "received payment_link.paid webhook",
		"payment_link_id", paymentLinkID,
		"payment_id", providerPaymentIntentID,
		"event_id", event.EventID)

	mappings, err := services.EntityIntegrationMappingService.ListEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{paymentLinkID},
			ProviderTypes:     []string{"razorpay"},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err != nil || mappings == nil || len(mappings.EntityIntegrationMappings) == 0 {
		h.logger.Info(ctx, "no EntityIntegrationMapping for Razorpay payment link, skipping",
			"payment_link_id", paymentLinkID)
		return nil
	}

	paymentID := mappings.EntityIntegrationMappings[0].EntityID
	sessions, err := services.CheckoutSessionService.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:        types.NewDefaultQueryFilter(),
		CheckoutPaymentIDs: []string{paymentID},
		CheckoutStatuses:   []types.CheckoutStatus{types.CheckoutStatusPending, types.CheckoutStatusInitiated},
	})
	if err != nil || sessions == nil || len(sessions.Items) == 0 {
		h.logger.Info(ctx, "no active checkout session for Razorpay payment, skipping",
			"payment_id", paymentID)
		return nil
	}

	sessionID := sessions.Items[0].ID
	providerResult := &types.CheckoutProviderResult{
		ProviderPaymentIntentID: providerPaymentIntentID,
	}
	if err := services.CheckoutSessionService.CompleteCheckoutSession(ctx, sessionID, providerResult); err != nil {
		if ierr.IsConflict(err) {
			h.logger.Info(ctx, "checkout session already completed, skipping", "session_id", sessionID)
			return nil
		}
		h.logger.Error(ctx, "failed to complete checkout session", "error", err, "session_id", sessionID)
		return err
	}
	h.logger.Info(ctx, "checkout session completed via Razorpay payment_link.paid", "session_id", sessionID)
	return nil
}
```

Note: check the exact struct shape of `RazorpayWebhookEvent.Payload` for `PaymentLink.Entity.ID` and `Payment.Entity.ID`. Run:
```bash
grep -n "type RazorpayWebhookEvent\|Payload\|PaymentLink\|Entity" internal/integration/razorpay/webhook/handler.go | head -20
```
And adjust field access to match the actual struct.

- [ ] **Step 3: Verify**

```bash
go build ./internal/integration/razorpay/...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/razorpay/webhook/handler.go
git commit -m "feat(checkout): add Razorpay payment_link.paid webhook handler routing to CompleteCheckoutSession"
```

---

## Task 16: Nomod Webhook — PaymentLinkID-Based Routing

**Files:**
- Modify: `internal/integration/nomod/webhook/handler.go`

The existing `handlePaymentLinkPayment` uses `GetPaymentByGatewayTrackingID`. Replace with `EntityIntegrationMapping` lookup.

- [ ] **Step 1: Replace `handlePaymentLinkPayment`**

Replace the function at line ~237:

```go
// handlePaymentLinkPayment processes FlexPrice-initiated payment link payments via checkout sessions.
// NOTE: Nomod's webhook fires with a Charge ID as payload.ID, and a separate PaymentLinkID field.
// We stored the PaymentLinkID in EntityIntegrationMapping at session creation, so we MUST look up
// by PaymentLinkID here. Using payload.ID (the Charge ID) would always miss.
func (h *Handler) handlePaymentLinkPayment(ctx context.Context, charge *nomod.ChargeResponse, nomodPaymentLinkID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "processing Nomod payment link payment",
		"charge_id", charge.ID,
		"nomod_payment_link_id", nomodPaymentLinkID)

	// Primary path: EntityIntegrationMapping lookup.
	mappings, err := services.EntityIntegrationMappingService.ListEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{nomodPaymentLinkID},
			ProviderTypes:     []string{"nomod"},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err != nil || mappings == nil || len(mappings.EntityIntegrationMappings) == 0 {
		h.logger.Info(ctx, "no EntityIntegrationMapping for Nomod payment link, falling through to legacy path",
			"nomod_payment_link_id", nomodPaymentLinkID)
		// Legacy fallback: look up by gateway_tracking_id (pre-EntityIntegrationMapping sessions).
		return h.handlePaymentLinkPaymentLegacy(ctx, charge, nomodPaymentLinkID, services)
	}

	paymentID := mappings.EntityIntegrationMappings[0].EntityID
	sessions, sessErr := services.CheckoutSessionService.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:        types.NewDefaultQueryFilter(),
		CheckoutPaymentIDs: []string{paymentID},
		CheckoutStatuses:   []types.CheckoutStatus{types.CheckoutStatusPending, types.CheckoutStatusInitiated},
	})
	if sessErr != nil || sessions == nil || len(sessions.Items) == 0 {
		h.logger.Info(ctx, "no active checkout session for Nomod payment, skipping",
			"payment_id", paymentID)
		return nil
	}

	sessionID := sessions.Items[0].ID
	// Store the Charge ID as gateway_payment_id via ProviderPaymentIntentID.
	providerResult := &types.CheckoutProviderResult{
		ProviderPaymentIntentID: charge.ID,
	}
	if err := services.CheckoutSessionService.CompleteCheckoutSession(ctx, sessionID, providerResult); err != nil {
		if ierr.IsConflict(err) {
			h.logger.Info(ctx, "checkout session already completed, skipping", "session_id", sessionID)
			return nil
		}
		h.logger.Error(ctx, "failed to complete checkout session", "error", err, "session_id", sessionID)
		return err
	}
	h.logger.Info(ctx, "checkout session completed via Nomod payment link", "session_id", sessionID)
	return nil
}

// handlePaymentLinkPaymentLegacy is the pre-EntityIntegrationMapping path.
// Remove after all active sessions have been migrated.
func (h *Handler) handlePaymentLinkPaymentLegacy(ctx context.Context, charge *nomod.ChargeResponse, nomodPaymentLinkID string, services *ServiceDependencies) error {
```

Move the existing body of `handlePaymentLinkPayment` (lines 243 onwards) into `handlePaymentLinkPaymentLegacy`.

- [ ] **Step 2: Verify**

```bash
go build ./internal/integration/nomod/...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add internal/integration/nomod/webhook/handler.go
git commit -m "feat(checkout): Nomod webhook routes to CompleteCheckoutSession via PaymentLinkID EntityIntegrationMapping"
```

---

## Task 17: Moyasar Webhook — EntityIntegrationMapping Routing

**Files:**
- Modify: `internal/integration/moyasar/webhook/handler.go`

The existing `handlePaymentPaid` dispatches on `flexprice_payment_id` in metadata. Insert an EntityIntegrationMapping check before that.

- [ ] **Step 1: Update `handlePaymentPaid` to check EntityIntegrationMapping first**

In `handlePaymentPaid` (line ~135), insert the EntityIntegrationMapping block as the first priority:

```go
func (h *Handler) handlePaymentPaid(ctx context.Context, payment *moyasar.MoyasarPayment, environmentID string, services *ServiceDependencies) error {
	h.logger.Info(ctx, "received payment_paid webhook",
		"moyasar_payment_id", payment.ID,
		"status", payment.Status,
	)

	// Primary path: EntityIntegrationMapping lookup (Moyasar payment ID → FlexPrice payment ID).
	// Moyasar uses the same ID at creation and in webhooks.
	mappings, err := services.EntityIntegrationMappingService.ListEntityIntegrationMappings(
		ctx,
		&types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{payment.ID},
			ProviderTypes:     []string{"moyasar"},
			EntityType:        types.IntegrationEntityTypePayment,
		},
	)
	if err == nil && mappings != nil && len(mappings.EntityIntegrationMappings) > 0 {
		paymentID := mappings.EntityIntegrationMappings[0].EntityID
		sessions, sessErr := services.CheckoutSessionService.List(ctx, &types.CheckoutSessionFilter{
			QueryFilter:        types.NewDefaultQueryFilter(),
			CheckoutPaymentIDs: []string{paymentID},
			CheckoutStatuses:   []types.CheckoutStatus{types.CheckoutStatusPending, types.CheckoutStatusInitiated},
		})
		if sessErr == nil && sessions != nil && len(sessions.Items) > 0 {
			sessionID := sessions.Items[0].ID
			providerResult := &types.CheckoutProviderResult{
				ProviderPaymentIntentID: payment.ID,
			}
			if completeErr := services.CheckoutSessionService.CompleteCheckoutSession(ctx, sessionID, providerResult); completeErr != nil {
				if ierr.IsConflict(completeErr) {
					h.logger.Info(ctx, "checkout session already completed, skipping", "session_id", sessionID)
					return nil
				}
				h.logger.Error(ctx, "failed to complete checkout session", "error", completeErr, "session_id", sessionID)
				return completeErr
			}
			h.logger.Info(ctx, "checkout session completed via Moyasar EntityIntegrationMapping", "session_id", sessionID)
			return nil
		}
	}

	// Legacy path: flexprice_payment_id in metadata → lifecycle-managed payment.
	if payment.Metadata != nil {
		if flexpricePaymentID := payment.Metadata["flexprice_payment_id"]; flexpricePaymentID != "" {
			return h.handlePaymentLifecycle(ctx, payment, flexpricePaymentID, services)
		}
	}

	// Moyasar invoice-link flow.
	if payment.InvoiceID != "" {
		return h.handleInvoicePayment(ctx, payment, services)
	}

	h.logger.Info(ctx, "webhook payment has no known anchor, skipping", "moyasar_payment_id", payment.ID)
	return nil
}
```

- [ ] **Step 2: Verify**

```bash
go build ./internal/integration/moyasar/...
```

Expected: no output.

- [ ] **Step 3: Run all tests**

```bash
go test -v -race ./internal/integration/... -count=1 2>&1 | tail -30
```

Expected: PASS on all tests in the integration packages (or SKIP for tests requiring live credentials).

- [ ] **Step 4: Commit**

```bash
git add internal/integration/moyasar/webhook/handler.go
git commit -m "feat(checkout): Moyasar webhook routes to CompleteCheckoutSession via EntityIntegrationMapping"
```

---

## Task 18: Temporal Expiry Workflow

**Files:**
- Modify: `internal/temporal/models/cron.go`
- Modify: `internal/types/schedule.go`
- Create: `internal/temporal/workflows/cron/checkout_session_expiry_workflow.go`
- Create: `internal/temporal/activities/cron/checkout_session_expiry_activities.go`
- Modify: `internal/temporal/registration.go`
- Modify: `internal/temporal/service/schedules.go`

- [ ] **Step 1: Add models to `internal/temporal/models/cron.go`**

Add at the end of the file (after the last model):

```go
// CheckoutSessionExpiryWorkflowInput is the input for CheckoutSessionExpiryWorkflow.
type CheckoutSessionExpiryWorkflowInput struct{}

// CheckoutSessionExpiryWorkflowResult captures outcome metrics.
type CheckoutSessionExpiryWorkflowResult struct {
	Total     int
	Succeeded int
	Failed    int
}
```

- [ ] **Step 2: Add `ScheduleIDCheckoutSessionExpiry` to `internal/types/schedule.go`**

Add the constant:
```go
ScheduleIDCheckoutSessionExpiry ScheduleID = "checkout-session-expiry"
```

Add it to `AllTemporalServerScheduleIDs()`:
```go
func AllTemporalServerScheduleIDs() []ScheduleID {
	return []ScheduleID{
		// ... existing entries ...
		ScheduleIDCheckoutSessionExpiry,
	}
}
```

- [ ] **Step 3: Create the workflow file**

Create `internal/temporal/workflows/cron/checkout_session_expiry_workflow.go`:

```go
package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityExpireCheckoutSessions = "ExpireCheckoutSessionsActivity"
)

// CheckoutSessionExpiryWorkflow expires checkout sessions that have passed their expiry date.
// It is triggered by a Temporal Schedule every 5 minutes.
func CheckoutSessionExpiryWorkflow(ctx workflow.Context, _ cronModels.CheckoutSessionExpiryWorkflowInput) (*cronModels.CheckoutSessionExpiryWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting CheckoutSessionExpiryWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.CheckoutSessionExpiryWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityExpireCheckoutSessions).Get(ctx, &result); err != nil {
		log.Error("CheckoutSessionExpiryWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("CheckoutSessionExpiryWorkflow completed",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return &result, nil
}
```

- [ ] **Step 4: Create the activity file**

Create `internal/temporal/activities/cron/checkout_session_expiry_activities.go`:

```go
package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

const checkoutExpiryBatchSize = 100

// CheckoutSessionExpiryActivities wraps checkout session expiry logic.
type CheckoutSessionExpiryActivities struct {
	checkoutSvc        service.CheckoutSessionService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
	logger             *logger.Logger
}

func NewCheckoutSessionExpiryActivities(
	checkoutSvc service.CheckoutSessionService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	log *logger.Logger,
) *CheckoutSessionExpiryActivities {
	return &CheckoutSessionExpiryActivities{
		checkoutSvc:        checkoutSvc,
		tenantService:      tenantService,
		environmentService: environmentService,
		logger:             log,
	}
}

// ExpireCheckoutSessionsActivity finds and expires checkout sessions that have passed their
// expiry date across all tenants and environments.
func (a *CheckoutSessionExpiryActivities) ExpireCheckoutSessionsActivity(ctx context.Context) (*cronModels.CheckoutSessionExpiryWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting checkout session expiry activity")
	a.logger.Info(ctx, "starting checkout session expiry cron", "time", time.Now().UTC().Format(time.RFC3339))

	tenants, err := a.tenantService.GetAllTenants(ctx)
	if err != nil {
		return nil, err
	}

	result := &cronModels.CheckoutSessionExpiryWorkflowResult{}
	now := time.Now().UTC()

	for _, tenant := range tenants {
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenant.ID)
		envFilter := types.GetDefaultFilter()
		envFilter.Limit = 1000
		environments, err := a.environmentService.GetEnvironments(tenantCtx, envFilter)
		if err != nil {
			a.logger.Error(ctx, "failed to get environments", "tenant_id", tenant.ID, "error", err)
			return nil, err
		}

		for _, environment := range environments.Environments {
			envCtx := context.WithValue(tenantCtx, types.CtxEnvironmentID, environment.ID)

			filter := &types.CheckoutSessionFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  checkoutExpiryBatchSize,
					Offset: 0,
				},
				CheckoutStatuses: []types.CheckoutStatus{
					types.CheckoutStatusInitiated,
					types.CheckoutStatusPending,
				},
				ExpiresAtLT: &now,
			}

			sessions, err := a.checkoutSvc.List(envCtx, filter)
			if err != nil {
				a.logger.Error(ctx, "failed to list expired checkout sessions",
					"tenant_id", tenant.ID, "env_id", environment.ID, "error", err)
				continue
			}

			for i, sess := range sessions.Items {
				if i%10 == 0 {
					activity.RecordHeartbeat(ctx, "tenant="+tenant.ID+" env="+environment.ID)
				}
				result.Total++

				// Fetch the full domain session for CleanupCheckoutSession.
				fullSession, err := a.checkoutSvc.Get(envCtx, sess.ID)
				if err != nil {
					a.logger.Error(ctx, "failed to get checkout session for expiry", "session_id", sess.ID, "error", err)
					result.Failed++
					continue
				}

				if err := a.checkoutSvc.CleanupCheckoutSession(envCtx, fullSession.CheckoutSession, nil); err != nil {
					a.logger.Error(ctx, "failed to expire checkout session", "session_id", sess.ID, "error", err)
					result.Failed++
					continue
				}
				a.logger.Info(ctx, "expired checkout session", "session_id", sess.ID)
				result.Succeeded++
			}
		}
	}

	a.logger.Info(ctx, "completed checkout session expiry cron",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}
```

Note: `a.checkoutSvc.Get` returns `*dto.CheckoutSessionResponse` which embeds `*domainCheckout.CheckoutSession`. Adjust if the `Get` return type differs — look at `service.CheckoutSessionService.Get`. `CleanupCheckoutSession` takes `*domainCheckout.CheckoutSession`. You may need to import `domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"` and call `fullSession.CheckoutSession` to get the domain type.

- [ ] **Step 5: Add `checkoutSessionExpiry` bundle field to `registration.go`**

In `internal/temporal/registration.go`, in the `cronActivityBundle` struct, add:
```go
checkoutSessionExpiry *cronActivities.CheckoutSessionExpiryActivities
```

In the function that constructs the bundle (find where `walletCreditExpiry` is initialized), add:
```go
checkoutSessionExpiry: cronActivities.NewCheckoutSessionExpiryActivities(
    service.NewCheckoutSessionService(params),
    tenantService,
    environmentService,
    params.Logger,
),
```

In the `case types.TemporalTaskQueueCron:` block, add to `workflowsList`:
```go
cronWorkflows.CheckoutSessionExpiryWorkflow,
```

Add to `activitiesList`:
```go
cron.checkoutSessionExpiry.ExpireCheckoutSessionsActivity,
```

- [ ] **Step 6: Add the schedule to `internal/temporal/service/schedules.go`**

In `AllTemporalScheduleConfigs()`, add:
```go
{
    ID:        types.ScheduleIDCheckoutSessionExpiry,
    Interval:  5 * time.Minute,
    Workflow:  cronWorkflows.CheckoutSessionExpiryWorkflow,
    Input:     models.CheckoutSessionExpiryWorkflowInput{},
    TaskQueue: types.TemporalTaskQueueCron,
},
```

- [ ] **Step 7: Verify the full binary compiles**

```bash
go build ./...
```

Expected: no output.

- [ ] **Step 8: Run all tests**

```bash
go test -race ./... -count=1 2>&1 | grep -E "FAIL|ok" | tail -30
```

Expected: no FAIL lines.

- [ ] **Step 9: Commit**

```bash
git add internal/temporal/models/cron.go \
        internal/types/schedule.go \
        internal/temporal/workflows/cron/checkout_session_expiry_workflow.go \
        internal/temporal/activities/cron/checkout_session_expiry_activities.go \
        internal/temporal/registration.go \
        internal/temporal/service/schedules.go
git commit -m "feat(checkout): add Temporal checkout session expiry workflow (every 5 min)"
```

---

## Self-Review

**Spec coverage check:**

| Spec Section | Task(s) |
|---|---|
| §4.1 Move PaymentAction to types | Task 1 |
| §4.2 Flatten CheckoutProviderResult | Task 1 |
| §4.3 PaymentActionForUser() | Task 1 |
| §4.4 Simplify ToCheckoutSessionResponse | Task 2 |
| §4.5 WebhookEventCheckoutSessionExpired | Task 1 |
| §5 Session creation idempotency (already done) | already implemented |
| §6 CheckoutProvider interface | Task 3 |
| §7 Per-provider adapters | Tasks 4–7 |
| §8 Factory method | Task 8 |
| §9 Enum expansion | Task 1 |
| §10 executeCheckoutAction wiring | Task 9 |
| §11.1 MarkCompleted repo | Task 10 |
| §11.2–11.4 Revised CompleteCheckoutSession | Task 11 |
| §12 Webhook routing (all 4 providers) | Tasks 14–17 |
| §12.5 CheckoutSessionService to ServiceDependencies | Task 13 |
| §13 CleanupCheckoutSession idempotency | Task 12 |
| §14 Temporal expiry workflow | Task 18 |

**Type consistency check:**

- `types.PaymentAction` defined in Task 1, used in Tasks 3, 4, 5, 6, 7 ✓
- `interfaces.CheckoutProvider` defined in Task 3, used in Task 8 ✓
- `interfaces.CheckoutProviderRequest/Response` defined in Task 3, used in Tasks 4–7 ✓
- `CheckoutAdapter.Svc` — must match actual field name in each integration struct (verify in Task 8) ✓
- `MarkCompleted(ctx, sessionID, time.Time, *types.CheckoutProviderResult) (bool, error)` defined in Task 10, used in Task 11 ✓
- `services.CheckoutSessionService.CompleteCheckoutSession` defined in Task 13, used in Tasks 14–17 ✓
- `services.CheckoutSessionService.List` added to interface in Task 14 step 2, used in Tasks 14–17 ✓
- `CheckoutSessionExpiryWorkflow` constant defined in Task 18, referenced in Tasks 18 registration steps ✓
