package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
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

type CheckoutProviderResult struct {
	CreateSubscriptionResult *ProviderSubscriptionResult `json:"create_subscription_result,omitempty"`
}

type ProviderSubscriptionResult struct {
	SessionID       string `json:"session_id"`
	SessionURL      string `json:"session_url"`
	PaymentIntentID string `json:"payment_intent_id"`
}
