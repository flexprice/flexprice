package integrations

import "github.com/flexprice/flexprice/internal/types"

// MapStripePaymentStatus maps a Stripe PaymentIntent status to a FlexPrice PaymentStatus.
// Returns (status, true) when a transition should be applied; ("", false) for in-flight statuses.
func MapStripePaymentStatus(stripeStatus string) (types.PaymentStatus, bool) {
	switch stripeStatus {
	case "succeeded":
		return types.PaymentStatusSucceeded, true
	case "requires_payment_method", "canceled":
		return types.PaymentStatusFailed, true
	default:
		return "", false
	}
}
