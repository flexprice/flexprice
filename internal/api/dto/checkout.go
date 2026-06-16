package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateCheckoutRequest is the discriminated-union request that opens a checkout.
// It either creates a brand-new subscription (subscription_creation) or performs an
// in-place plan upgrade of an existing subscription (subscription_change).
type CreateCheckoutRequest struct {
	// CheckoutType selects what this checkout creates.
	CheckoutType types.CheckoutType `json:"checkout_type" binding:"required"`
	// Objective is required for subscription_creation (payment|setup); for
	// subscription_change it is implicitly payment and may be omitted.
	Objective types.CheckoutObjective `json:"objective,omitempty"`

	// Subscription is the full new-subscription spec; REQUIRED when
	// CheckoutType == subscription_creation (carries trial, grants, coupons,
	// commitments, overrides, etc. — checkout only overrides the collection
	// method / payment behavior / status).
	Subscription *CreateSubscriptionRequest `json:"subscription,omitempty"`

	// SubscriptionChange is REQUIRED when CheckoutType == subscription_change.
	SubscriptionChange *SubscriptionChangeCheckoutPayload `json:"subscription_change,omitempty"`

	SuccessURL string            `json:"success_url,omitempty"`
	CancelURL  string            `json:"cancel_url,omitempty"`
	SaveCard   bool              `json:"save_card,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// SubscriptionChangeCheckoutPayload describes an in-place plan UPGRADE: the new
// plan's subscription is created incomplete (opening invoice raised, proration
// credit netted) and the source subscription stays active until the invoice is paid.
type SubscriptionChangeCheckoutPayload struct {
	SourceSubscriptionID string                  `json:"source_subscription_id" binding:"required"`
	TargetPlanID         string                  `json:"target_plan_id" binding:"required"`
	ProrationBehavior    types.ProrationBehavior `json:"proration_behavior,omitempty"`
}

// Validate enforces the discriminated union.
func (r *CreateCheckoutRequest) Validate() error {
	if err := r.CheckoutType.Validate(); err != nil {
		return err
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
	case types.CheckoutTypeSubscriptionChange:
		if r.SubscriptionChange == nil {
			return ierr.NewError("subscription_change is required for subscription_change checkout").
				WithHint("Provide the `subscription_change` object").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

type CheckoutResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	CheckoutURL string `json:"checkout_url"`
}
