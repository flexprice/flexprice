package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// CheckoutEntityType is the kind of subject a checkout activates.
type CheckoutEntityType string

const (
	CheckoutEntityTypeSubscription CheckoutEntityType = "subscription"
)

func (t CheckoutEntityType) Validate() error {
	switch t {
	case CheckoutEntityTypeSubscription:
		return nil
	default:
		return ierr.NewError("invalid checkout entity type").
			WithHint("Checkout entity type must be 'subscription'").
			WithReportableDetails(map[string]any{"entity_type": t}).
			Mark(ierr.ErrValidation)
	}
}

// CheckoutAction is the operation a checkout defers.
type CheckoutAction string

const (
	CheckoutActionSubscriptionCreation CheckoutAction = "subscription_creation"
)

func (a CheckoutAction) Validate() error {
	switch a {
	case CheckoutActionSubscriptionCreation:
		return nil
	default:
		return ierr.NewError("invalid checkout action").
			WithHint("Checkout action must be 'subscription_creation'").
			WithReportableDetails(map[string]any{"checkout_action": a}).
			Mark(ierr.ErrValidation)
	}
}

// CheckoutProvider identifies which payment provider handles a checkout session.
type CheckoutProvider string

const (
	CheckoutProviderFlexprice CheckoutProvider = "flexprice"
	CheckoutProviderStripe    CheckoutProvider = "stripe"
)

func (p CheckoutProvider) Validate() error {
	switch p {
	case CheckoutProviderFlexprice, CheckoutProviderStripe:
		return nil
	default:
		return ierr.NewError("invalid checkout provider").
			WithHint("Checkout provider must be 'flexprice' or 'stripe'").
			WithReportableDetails(map[string]any{"provider": p}).
			Mark(ierr.ErrValidation)
	}
}

// CheckoutObjective drives parking state, provider mode and completion trigger.
type CheckoutObjective string

const (
	CheckoutObjectivePayment CheckoutObjective = "payment"
	CheckoutObjectiveSetup   CheckoutObjective = "setup"
)

func (o CheckoutObjective) Validate() error {
	switch o {
	case CheckoutObjectivePayment, CheckoutObjectiveSetup:
		return nil
	default:
		return ierr.NewError("invalid checkout objective").
			WithHint("Checkout objective must be 'payment' or 'setup'").
			WithReportableDetails(map[string]any{"objective": o}).
			Mark(ierr.ErrValidation)
	}
}

// CheckoutStatus is the workflow state of the checkout (distinct from the base mixin lifecycle status).
type CheckoutStatus string

const (
	CheckoutStatusPending   CheckoutStatus = "pending"
	CheckoutStatusCompleted CheckoutStatus = "completed"
	CheckoutStatusExpired   CheckoutStatus = "expired"
	CheckoutStatusCancelled CheckoutStatus = "cancelled"
	CheckoutStatusFailed    CheckoutStatus = "failed"
)

func (s CheckoutStatus) Validate() error {
	switch s {
	case CheckoutStatusPending, CheckoutStatusCompleted, CheckoutStatusExpired,
		CheckoutStatusCancelled, CheckoutStatusFailed:
		return nil
	default:
		return ierr.NewError("invalid checkout status").
			WithHint("Invalid checkout status").
			WithReportableDetails(map[string]any{"status": s}).
			Mark(ierr.ErrValidation)
	}
}
