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

// PaymentAction extracts the safe-to-expose action from a provider result.
// All other fields in CheckoutProviderResult are sensitive gateway data.
func (r *CheckoutProviderResult) PaymentAction() *PaymentAction {
	if r == nil {
		return nil
	}
	return r.NextAction
}

// ── payment_provider_config ──────────────────────────────────────────────────

// CheckoutPaymentProviderConfig is the per-checkout-request declaration of
// payment-provider behavior — stored in CheckoutSession.payment_provider_config,
// a dedicated column sibling to Configuration (not nested inside it). See
// docs/superpowers/specs/2026-07-09-razorpay-autocharge-design.md §5.2.
type CheckoutPaymentProviderConfig struct {
	CollectionMethod CollectionMethod               `json:"collection_method,omitempty"`
	Razorpay         *RazorpayPaymentProviderConfig `json:"razorpay,omitempty"`
}

type RazorpayPaymentProviderConfig struct {
	PreferredPaymentMethod PaymentMethodType `json:"preferred_payment_method,omitempty"`
	// MaxAmount optionally tightens the environment's Settings-level ceiling
	// (payment_mandate_limits) for this specific checkout request. Must not
	// exceed the Settings value — validated in the service layer (needs DB
	// access), not here. See design spec §5.3.
	MaxAmount *decimal.Decimal `json:"max_amount,omitempty"`
}

// Validate checks structural validity only: at most one provider sub-object,
// and it must match the top-level payment_provider. The cross-check against
// the environment's Settings-level ceiling happens in the service layer.
func (c *CheckoutPaymentProviderConfig) Validate(provider CheckoutPaymentProvider) error {
	if c == nil {
		return nil
	}
	if c.Razorpay != nil {
		if provider != CheckoutPaymentProviderRazorpay {
			return ierr.NewError("payment_provider_config.razorpay is set but payment_provider is not razorpay").
				WithHint("Only populate the provider sub-object matching the top-level payment_provider field").
				Mark(ierr.ErrValidation)
		}
		if c.Razorpay.PreferredPaymentMethod != "" {
			if err := c.Razorpay.PreferredPaymentMethod.Validate(); err != nil {
				return err
			}
		}
		if c.Razorpay.MaxAmount != nil && c.Razorpay.MaxAmount.IsNegative() {
			return ierr.NewError("payment_provider_config.razorpay.max_amount must not be negative").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}
