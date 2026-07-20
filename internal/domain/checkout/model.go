package checkout

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// JSONB types for complex fields stored as PostgreSQL jsonb columns.

// JSONBCheckoutConfiguration wraps CheckoutConfiguration for JSONB storage.
type JSONBCheckoutConfiguration types.CheckoutConfiguration

func (j *JSONBCheckoutConfiguration) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return ierr.NewError("invalid type for jsonb checkout configuration").
			WithHint("Invalid type for JSONB checkout configuration").
			Mark(ierr.ErrValidation)
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONBCheckoutConfiguration) Value() (driver.Value, error) {
	return json.Marshal(j)
}

func ToJSONBCheckoutConfiguration(c types.CheckoutConfiguration) JSONBCheckoutConfiguration {
	return JSONBCheckoutConfiguration(c)
}

func (j JSONBCheckoutConfiguration) ToCheckoutConfiguration() types.CheckoutConfiguration {
	return types.CheckoutConfiguration(j)
}

// JSONBCheckoutResult wraps CheckoutResult for JSONB storage.
type JSONBCheckoutResult types.CheckoutResult

func (j *JSONBCheckoutResult) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return ierr.NewError("invalid type for jsonb checkout result").
			WithHint("Invalid type for JSONB checkout result").
			Mark(ierr.ErrValidation)
	}
	return json.Unmarshal(bytes, j)
}

func (j *JSONBCheckoutResult) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func ToJSONBCheckoutResult(r *types.CheckoutResult) *JSONBCheckoutResult {
	return (*JSONBCheckoutResult)(r)
}

func (j *JSONBCheckoutResult) ToCheckoutResult() *types.CheckoutResult {
	return (*types.CheckoutResult)(j)
}

// JSONBCheckoutProviderResult wraps CheckoutProviderResult for JSONB storage.
type JSONBCheckoutProviderResult types.CheckoutProviderResult

func (j *JSONBCheckoutProviderResult) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return ierr.NewError("invalid type for jsonb checkout provider result").
			WithHint("Invalid type for JSONB checkout provider result").
			Mark(ierr.ErrValidation)
	}
	return json.Unmarshal(bytes, j)
}

func (j *JSONBCheckoutProviderResult) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func ToJSONBCheckoutProviderResult(r *types.CheckoutProviderResult) *JSONBCheckoutProviderResult {
	return (*JSONBCheckoutProviderResult)(r)
}

func (j *JSONBCheckoutProviderResult) ToProviderResult() *types.CheckoutProviderResult {
	return (*types.CheckoutProviderResult)(j)
}

// JSONBCheckoutPaymentProviderConfig wraps CheckoutPaymentProviderConfig for JSONB storage.
// Pointer-based so an unset config persists as SQL NULL rather than an empty '{}' object.
type JSONBCheckoutPaymentProviderConfig types.CheckoutPaymentProviderConfig

func (j *JSONBCheckoutPaymentProviderConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return ierr.NewError("invalid type for jsonb checkout payment provider config").
			WithHint("Invalid type for JSONB checkout payment provider config").
			Mark(ierr.ErrValidation)
	}
	return json.Unmarshal(bytes, j)
}

func (j *JSONBCheckoutPaymentProviderConfig) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func ToJSONBCheckoutPaymentProviderConfig(c *types.CheckoutPaymentProviderConfig) *JSONBCheckoutPaymentProviderConfig {
	return (*JSONBCheckoutPaymentProviderConfig)(c)
}

func (j *JSONBCheckoutPaymentProviderConfig) ToCheckoutPaymentProviderConfig() *types.CheckoutPaymentProviderConfig {
	return (*types.CheckoutPaymentProviderConfig)(j)
}

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
	CheckoutStatus types.CheckoutStatus `db:"checkout_status" json:"checkout_status"`
	// PaymentProvider is required and immutable after creation.
	PaymentProvider types.CheckoutPaymentProvider `db:"payment_provider" json:"payment_provider"`

	// CheckoutInvoiceID and CheckoutPaymentID are set once the apply step
	// creates the corresponding Flexprice entities (completed sessions only).
	CheckoutInvoiceID *string `db:"checkout_invoice_id" json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string `db:"checkout_payment_id" json:"checkout_payment_id,omitempty"`

	// Configuration holds the immutable caller inputs set at creation time.
	// Only the sub-struct matching Action is populated; the others are nil.
	Configuration JSONBCheckoutConfiguration `db:"configuration,jsonb" json:"configuration,omitempty"`

	// PaymentProviderConfig holds provider-specific payment configuration
	// (e.g. Razorpay UPI Autopay preferences) supplied at session creation.
	// Nil if not set on the request.
	PaymentProviderConfig *JSONBCheckoutPaymentProviderConfig `db:"payment_provider_config,jsonb" json:"payment_provider_config,omitempty"`

	// Result holds the Flexprice entity IDs created during the apply step.
	// Nil until the session reaches completed status.
	Result *JSONBCheckoutResult `db:"result,jsonb" json:"result,omitempty"`

	// ProviderResult holds the external provider response (session URL,
	// payment intent ID, etc.). Set after the provider call in the create
	// step. Source of truth for deriving PaymentActions in API responses.
	ProviderResult *JSONBCheckoutProviderResult `db:"provider_result,jsonb" json:"provider_result,omitempty"`

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
	FailureReason *string        `db:"failure_reason" json:"failure_reason,omitempty"`
	Metadata      types.Metadata `db:"metadata,jsonb" json:"metadata,omitempty"`

	types.BaseModel
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
