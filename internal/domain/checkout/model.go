package checkout

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutSession is a single-use session that drives a B2C payment flow.
// It captures the caller's intent (action + configuration) at creation and
// tracks the session through its lifecycle: initiated → pending → completed
// (or failed/expired). Callers poll or receive webhooks to learn the outcome.
//
// Lifecycle:
//
//	created ──► initiated ──► pending ──► completed
//	                │              └──────► failed
//	                └─────────────────────► expired
type CheckoutSession struct {
	ID            string `json:"id"`
	EnvironmentID string `json:"environment_id"`
	CustomerID    string `json:"customer_id"`

	// Action is the billing operation this session will perform.
	// Immutable after creation; determines which sub-struct inside
	// Configuration is populated.
	Action types.CheckoutAction `json:"action"`

	// CheckoutStatus tracks the session lifecycle. Starts at "initiated"
	// when the session row is inserted; advances to "pending" once the
	// provider call succeeds; settles to completed/failed/expired.
	CheckoutStatus types.CheckoutStatus `json:"checkout_status"`
	// PaymentProvider is required and immutable after creation.
	PaymentProvider types.CheckoutPaymentProvider `json:"payment_provider"`

	// CheckoutInvoiceID and CheckoutPaymentID are set once the apply step
	// creates the corresponding Flexprice entities (completed sessions only).
	CheckoutInvoiceID *string `json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string `json:"checkout_payment_id,omitempty"`

	// Configuration holds the immutable caller inputs set at creation time.
	// Only the sub-struct matching Action is populated; the others are nil.
	Configuration types.CheckoutConfiguration `json:"configuration,omitempty"`

	// PaymentProviderConfig holds provider-specific payment configuration
	// (e.g. Razorpay UPI Autopay preferences) supplied at session creation.
	// Nil if not set on the request.
	PaymentProviderConfig *types.CheckoutPaymentProviderConfig `json:"payment_provider_config,omitempty" activity:"redact"`

	// Result holds the Flexprice entity IDs created during the apply step.
	// Nil until the session reaches completed status.
	Result *types.CheckoutResult `json:"result,omitempty" activity:"redact"`

	// ProviderResult holds the external provider response (session URL,
	// payment intent ID, etc.). Set after the provider call in the create
	// step. Source of truth for deriving PaymentActions in API responses.
	ProviderResult *types.CheckoutProviderResult `json:"provider_result,omitempty" activity:"redact"`

	// IdempotencyKey is caller-supplied. It is unique only while the session
	// is active (initiated|pending). The same key may be reused once the
	// session reaches a terminal state (completed|failed|expired).
	IdempotencyKey *string `json:"idempotency_key,omitempty"`

	// Redirect URLs sent to the payment provider. The provider redirects the
	// user browser to the appropriate URL after the payment flow completes.
	SuccessURL *string `json:"success_url,omitempty"`
	FailureURL *string `json:"failure_url,omitempty"`
	CancelURL  *string `json:"cancel_url,omitempty"`

	// ExpiresAt is required. A Temporal timer fires at this time for any
	// session still in initiated|pending, marking it expired. The caller
	// must create a new session after expiry (expire-and-restart model).
	ExpiresAt   time.Time  `json:"expires_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`

	// FailureReason is a human-readable string set on failed sessions.
	FailureReason *string        `json:"failure_reason,omitempty"`
	Metadata      types.Metadata `json:"metadata,omitempty" activity:"drop"`

	types.BaseModel
}

// FromEnt converts a stored checkout session.
func FromEnt(e *ent.CheckoutSession) *CheckoutSession {
	s := &CheckoutSession{
		ID:                    e.ID,
		EnvironmentID:         e.EnvironmentID,
		CustomerID:            e.CustomerID,
		Action:                e.Action,
		CheckoutStatus:        types.CheckoutStatus(e.CheckoutStatus),
		PaymentProvider:       e.PaymentProvider,
		CheckoutInvoiceID:     e.CheckoutInvoiceID,
		CheckoutPaymentID:     e.CheckoutPaymentID,
		Configuration:         e.Configuration,
		PaymentProviderConfig: e.PaymentProviderConfig,
		Result:                e.Result,
		ProviderResult:        e.ProviderResult,
		IdempotencyKey:        e.IdempotencyKey,
		SuccessURL:            e.SuccessURL,
		FailureURL:            e.FailureURL,
		CancelURL:             e.CancelURL,
		CompletedAt:           e.CompletedAt,
		CancelledAt:           e.CancelledAt,
		FailureReason:         e.FailureReason,
		Metadata:              types.Metadata(e.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
	if e.ExpiresAt != nil {
		s.ExpiresAt = *e.ExpiresAt
	}
	return s
}

func (s *CheckoutSession) GetID() string {
	if s == nil {
		return ""
	}
	return s.ID
}

// Validate checks that the session has all required fields and that enum values are valid.
func (s *CheckoutSession) Validate() error {
	if s.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("customer_id cannot be empty").
			Mark(ierr.ErrValidation)
	}
	if err := s.Action.Validate(); err != nil {
		return err
	}
	if err := s.CheckoutStatus.Validate(); err != nil {
		return err
	}
	if s.ExpiresAt.IsZero() {
		return ierr.NewError("expires_at is required").
			WithHint("expires_at cannot be zero").
			Mark(ierr.ErrValidation)
	}
	if err := s.PaymentProvider.Validate(); err != nil {
		return err
	}
	return nil
}
