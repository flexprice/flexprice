package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
)

type CheckoutConfiguration struct {
	CreateSubscriptionParams *CreateSubscriptionParams `json:"create_subscription_params,omitempty"`
}

// Validate validates that the configuration holds all required fields
// for the given checkout action. Called at request entry so errors surface before
// any DB operations begin.
func (c *CheckoutConfiguration) Validate(action CheckoutAction) error {
	switch action {
	case CheckoutActionCreateSubscription:
		if c.CreateSubscriptionParams == nil {
			return ierr.NewError("create_subscription_params is required for create_subscription action").
				WithHint("Provide create_subscription_params in configuration").
				Mark(ierr.ErrValidation)
		}
		return c.CreateSubscriptionParams.Validate()
	}
	return nil
}

type CreateSubscriptionParams struct {
	PlanID        string            `json:"plan_id"`
	Currency      string            `json:"currency"`
	LookupKey     string            `json:"lookup_key,omitempty"`
	StartDate     *time.Time        `json:"start_date,omitempty"`
	EndDate       *time.Time        `json:"end_date,omitempty"`
	BillingPeriod BillingPeriod     `json:"billing_period"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// Validate checks that all required fields for subscription creation are present
// and internally consistent, mirroring CreateSubscriptionRequest validation rules.
func (p *CreateSubscriptionParams) Validate() error {
	if p.PlanID == "" {
		return ierr.NewError("plan_id is required").
			WithHint("Provide a valid plan_id in create_subscription_params").
			Mark(ierr.ErrValidation)
	}

	if err := ValidateCurrencyCode(p.Currency); err != nil {
		return err
	}

	if p.BillingPeriod == "" {
		return ierr.NewError("billing_period is required").
			WithHint("Provide a valid billing_period in create_subscription_params (e.g. MONTHLY, ANNUAL)").
			Mark(ierr.ErrValidation)
	}
	if err := p.BillingPeriod.Validate(); err != nil {
		return err
	}

	if p.StartDate != nil && p.EndDate != nil && p.EndDate.Before(*p.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("Ensure the subscription end date is on or after the start date").
			WithReportableDetails(map[string]any{
				"start_date": p.StartDate,
				"end_date":   p.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ── JSONB result structs ──────────────────────────────────────────────────────

type CheckoutResult struct {
	CreateSubscriptionResult *CreateSubscriptionResult `json:"create_subscription_result,omitempty"`
}

type CreateSubscriptionResult struct {
	SubscriptionID string `json:"subscription_id"`
	InvoiceID      string `json:"invoice_id"`
	PaymentID      string `json:"payment_id"`
}

// ── JSONB provider_result structs ────────────────────────────────────────────

// CheckoutProviderResult is the flat, action-agnostic provider response stored in
// checkout_sessions.provider_result. It is never serialized to API callers directly —
// use PaymentAction() to extract the safe-to-expose action.
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

func (r *CheckoutProviderResult) PaymentAction() *PaymentAction {
	if r == nil {
		return nil
	}
	return r.NextAction
}

// CheckoutPaymentProviderConfig is the per-checkout payment behavior config,
// stored in CheckoutSession.payment_provider_config.
type CheckoutPaymentProviderConfig struct {
	CollectionMethod CollectionMethod  `json:"collection_method,omitempty"`
	PreferredMethod  PaymentMethodType `json:"preferred_method,omitempty"`
	MaxMandateLimit  *decimal.Decimal  `json:"max_mandate_limit,omitempty" swaggertype:"string"`
}

func (c *CheckoutPaymentProviderConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.PreferredMethod != "" {
		if err := c.PreferredMethod.Validate(); err != nil {
			return err
		}
	}
	if c.MaxMandateLimit != nil && c.MaxMandateLimit.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("mandate_limit must be greater than zero").Mark(ierr.ErrValidation)
	}
	return nil
}
