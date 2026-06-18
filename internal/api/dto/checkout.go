package dto

import (
	"net/url"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateCheckoutRequest opens a checkout for a new subscription.
type CreateCheckoutRequest struct {
	// CheckoutType selects what this checkout creates.
	CheckoutType types.CheckoutType `json:"checkout_type" binding:"required"`
	// Objective is required for subscription_creation (payment|setup).
	Objective types.CheckoutObjective `json:"objective,omitempty"`

	// Subscription is the full new-subscription spec; REQUIRED when
	// CheckoutType == subscription_creation (carries trial, grants, coupons,
	// commitments, overrides, etc. — checkout only overrides the collection
	// method / payment behavior / status).
	Subscription *CreateSubscriptionRequest `json:"subscription,omitempty"`

	SuccessURL string            `json:"success_url,omitempty"`
	CancelURL  string            `json:"cancel_url,omitempty"`
	SaveCard   bool              `json:"save_card,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Validate enforces the request.
func (r *CreateCheckoutRequest) Validate() error {
	if err := r.CheckoutType.Validate(); err != nil {
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
	switch r.CheckoutType {
	case types.CheckoutTypeSubscriptionCreation:
		if r.Subscription == nil {
			return ierr.NewError("subscription is required for subscription_creation checkout").
				WithHint("Provide the `subscription` object").Mark(ierr.ErrValidation)
		}
		if r.Objective != types.CheckoutObjectivePayment && r.Objective != types.CheckoutObjectiveSetup {
			return ierr.NewError("objective must be 'payment' or 'setup' for subscription_creation").
				WithHint("Set objective to payment or setup").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

type CheckoutResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	CheckoutURL string `json:"checkout_url"`
}
