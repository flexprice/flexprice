package dto

import (
	"net/url"
	"time"

	"github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreateCheckoutRequest opens a checkout for a new subscription.
type CreateCheckoutRequest struct {
	// CheckoutAction selects what this checkout creates.
	CheckoutAction types.CheckoutAction `json:"checkout_action" binding:"required"`
	// Mode is required for subscription_creation (payment|setup).
	Mode types.CheckoutMode `json:"mode,omitempty"`

	// SubscriptionCreationParams is the full new-subscription spec; REQUIRED when
	// CheckoutAction == subscription_creation.
	SubscriptionCreationParams *CreateSubscriptionRequest `json:"subscription_creation_params,omitempty"`

	SuccessURL string            `json:"success_url,omitempty"`
	CancelURL  string            `json:"cancel_url,omitempty"`
	SaveCard   bool              `json:"save_card,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Validate enforces the request.
func (r *CreateCheckoutRequest) Validate() error {
	if err := r.CheckoutAction.Validate(); err != nil {
		return err
	}
	if r.SuccessURL == "" || r.CancelURL == "" {
		return ierr.NewError("success_url and cancel_url are required").
			WithHint("Provide both success_url and cancel_url").
			Mark(ierr.ErrValidation)
	}
	if _, err := url.ParseRequestURI(r.SuccessURL); err != nil {
		return ierr.NewError("success_url must be a valid URL").
			WithHint("Provide a valid success_url").
			Mark(ierr.ErrValidation)
	}
	if _, err := url.ParseRequestURI(r.CancelURL); err != nil {
		return ierr.NewError("cancel_url must be a valid URL").
			WithHint("Provide a valid cancel_url").
			Mark(ierr.ErrValidation)
	}
	switch r.CheckoutAction {
	case types.CheckoutActionSubscriptionCreation:
		if r.SubscriptionCreationParams == nil {
			return ierr.NewError("subscription_creation_params is required for subscription_creation checkout").
				WithHint("Provide the `subscription_creation_params` object").Mark(ierr.ErrValidation)
		}
		if r.Mode != types.CheckoutModePayment && r.Mode != types.CheckoutModeSetup {
			return ierr.NewError("mode must be 'payment' or 'setup' for subscription_creation").
				WithHint("Set mode to payment or setup").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// CheckoutResponse is the full checkout entity returned to callers.
type CheckoutResponse struct {
	ID                string                   `json:"id"`
	CustomerID        string                   `json:"customer_id"`
	EntityType        types.CheckoutEntityType `json:"entity_type"`
	EntityID          string                   `json:"entity_id"`
	CheckoutAction    types.CheckoutAction     `json:"checkout_action"`
	Mode              types.CheckoutMode       `json:"mode"`
	Status            types.CheckoutStatus     `json:"status"`
	Amount            *decimal.Decimal         `json:"amount,omitempty"`
	Currency          string                   `json:"currency,omitempty"`
	Provider          types.CheckoutProvider   `json:"provider"`
	ProviderSessionID *string                  `json:"provider_session_id,omitempty"`
	CheckoutURL       string                   `json:"checkout_url,omitempty"`
	SuccessURL        string                   `json:"success_url,omitempty"`
	CancelURL         string                   `json:"cancel_url,omitempty"`
	ExpiresAt         time.Time                `json:"expires_at"`
	CompletedAt       *time.Time               `json:"completed_at,omitempty"`
	CancelledAt       *time.Time               `json:"cancelled_at,omitempty"`
	FailureMessage    *string                  `json:"failure_message,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

// CheckoutResponseFromDomain converts a domain Checkout to the response DTO.
func CheckoutResponseFromDomain(c *checkout.Checkout) *CheckoutResponse {
	if c == nil {
		return nil
	}
	return &CheckoutResponse{
		ID:                c.ID,
		CustomerID:        c.CustomerID,
		EntityType:        c.EntityType,
		EntityID:          c.EntityID,
		CheckoutAction:    c.CheckoutAction,
		Mode:              c.Mode,
		Status:            c.Status,
		Amount:            c.Amount,
		Currency:          c.Currency,
		Provider:          c.Provider,
		ProviderSessionID: c.ProviderSessionID,
		CheckoutURL:       lo.FromPtr(c.CheckoutURL),
		SuccessURL:        lo.FromPtr(c.SuccessURL),
		CancelURL:         lo.FromPtr(c.CancelURL),
		ExpiresAt:         c.ExpiresAt,
		CompletedAt:       c.CompletedAt,
		CancelledAt:       c.CancelledAt,
		FailureMessage:    c.FailureMessage,
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
	}
}
