package integrations

import "github.com/flexprice/flexprice/internal/types"

type StripePaymentStatus string

const (
	StripePaymentStatusSucceeded             StripePaymentStatus = "succeeded"
	StripePaymentStatusRequiresPaymentMethod StripePaymentStatus = "requires_payment_method"
	StripePaymentStatusRequiresConfirmation  StripePaymentStatus = "requires_confirmation"
	StripePaymentStatusRequiresAction        StripePaymentStatus = "requires_action"
	StripePaymentStatusRequiresCapture       StripePaymentStatus = "requires_capture"
	StripePaymentStatusProcessing            StripePaymentStatus = "processing"
	StripePaymentStatusCanceled              StripePaymentStatus = "canceled"
)

// ToFlexPricePaymentStatus maps a Stripe PaymentIntent status to a FlexPrice PaymentStatus.
// Returns (status, true) when a transition should be applied; ("", false) for in-flight statuses.
func (s StripePaymentStatus) ToFlexPricePaymentStatus() (types.PaymentStatus, bool) {
	switch s {
	case StripePaymentStatusSucceeded:
		return types.PaymentStatusSucceeded, true
	case StripePaymentStatusRequiresPaymentMethod, StripePaymentStatusCanceled:
		return types.PaymentStatusFailed, true
	default:
		return "", false
	}
}
