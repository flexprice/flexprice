package checkout

import (
	"time"

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
	ID            string `db:"id" json:"id"`
	EnvironmentID string `db:"environment_id" json:"environment_id"`
	CustomerID    string `db:"customer_id" json:"customer_id"`

	// Action is the billing operation this session will perform.
	// Immutable after creation; determines which sub-struct inside
	// Configuration is populated.
	Action types.CheckoutAction `db:"action" json:"action"`

	// CheckoutStatus tracks the session lifecycle. Starts at "initiated"
	// when the session row is inserted; advances to "pending" once the
	// provider call succeeds; settles to completed/failed/expired.
	CheckoutStatus  types.CheckoutStatus           `db:"checkout_status" json:"checkout_status"`
	PaymentProvider *types.CheckoutPaymentProvider `db:"payment_provider" json:"payment_provider,omitempty"`

	// CheckoutInvoiceID and CheckoutPaymentID are set once the apply step
	// creates the corresponding Flexprice entities (completed sessions only).
	CheckoutInvoiceID *string `db:"checkout_invoice_id" json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string `db:"checkout_payment_id" json:"checkout_payment_id,omitempty"`

	// Configuration holds the immutable caller inputs set at creation time.
	// Only the sub-struct matching Action is populated; the others are nil.
	Configuration types.CheckoutConfiguration `db:"configuration" json:"configuration"`

	// Result holds the Flexprice entity IDs created during the apply step.
	// Nil until the session reaches completed status.
	Result *types.CheckoutResult `db:"result" json:"result,omitempty"`

	// ProviderResult holds the external provider response (session URL,
	// payment intent ID, etc.). Set after the provider call in the create
	// step. Source of truth for deriving PaymentActions in API responses.
	ProviderResult *types.CheckoutProviderResult `db:"provider_result" json:"provider_result,omitempty"`

	// IdempotencyKey is caller-supplied. It is unique only while the session
	// is active (initiated|pending). The same key may be reused once the
	// session reaches a terminal state (completed|failed|expired).
	IdempotencyKey *string `db:"idempotency_key" json:"idempotency_key,omitempty"`

	// Redirect URLs sent to the payment provider. The provider redirects the
	// user browser to the appropriate URL after the payment flow completes.
	SuccessURL *string `db:"success_url" json:"success_url,omitempty"`
	FailureURL *string `db:"failure_url" json:"failure_url,omitempty"`
	CancelURL  *string `db:"cancel_url" json:"cancel_url,omitempty"`

	// ExpiresAt is required. A Temporal timer fires at this time for any
	// session still in initiated|pending, marking it expired. The caller
	// must create a new session after expiry (expire-and-restart model).
	ExpiresAt   time.Time  `db:"expires_at" json:"expires_at"`
	CompletedAt *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	CancelledAt *time.Time `db:"cancelled_at" json:"cancelled_at,omitempty"`

	// FailureReason is a human-readable string set on failed sessions.
	FailureReason *string           `db:"failure_reason" json:"failure_reason,omitempty"`
	Metadata      map[string]string `db:"metadata" json:"metadata,omitempty"`

	types.BaseModel
}

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
	if s.PaymentProvider != nil {
		if err := s.PaymentProvider.Validate(); err != nil {
			return err
		}
	}
	if s.ExpiresAt.IsZero() {
		return ierr.NewError("expires_at is required").
			WithHint("expires_at cannot be zero").
			Mark(ierr.ErrValidation)
	}
	return nil
}
