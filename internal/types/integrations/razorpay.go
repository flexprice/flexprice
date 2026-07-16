package integrations

import "github.com/flexprice/flexprice/internal/types"

type RazorpayPaymentStatus string

const (
	RazorpayPaymentStatusCreated    RazorpayPaymentStatus = "created"
	RazorpayPaymentStatusAuthorized RazorpayPaymentStatus = "authorized"
	RazorpayPaymentStatusCaptured   RazorpayPaymentStatus = "captured"
	RazorpayPaymentStatusRefunded   RazorpayPaymentStatus = "refunded"
	RazorpayPaymentStatusFailed     RazorpayPaymentStatus = "failed"
)

// ToFlexPricePaymentStatus maps a Razorpay payment status to a FlexPrice PaymentStatus.
// Returns (status, true) when a transition should be applied; ("", false) for in-flight statuses.
func (s RazorpayPaymentStatus) ToFlexPricePaymentStatus() (types.PaymentStatus, bool) {
	switch s {
	case RazorpayPaymentStatusCaptured:
		return types.PaymentStatusSucceeded, true
	case RazorpayPaymentStatusFailed:
		return types.PaymentStatusFailed, true
	default:
		return "", false
	}
}
