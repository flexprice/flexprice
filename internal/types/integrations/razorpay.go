package integrations

import "github.com/flexprice/flexprice/internal/types"

// MapRazorpayPaymentStatus maps a Razorpay payment status to a FlexPrice PaymentStatus.
// Returns (status, true) when a transition should be applied; ("", false) for in-flight statuses.
func MapRazorpayPaymentStatus(razorpayStatus string) (types.PaymentStatus, bool) {
	switch razorpayStatus {
	case "captured":
		return types.PaymentStatusSucceeded, true
	case "failed":
		return types.PaymentStatusFailed, true
	default:
		return "", false
	}
}
