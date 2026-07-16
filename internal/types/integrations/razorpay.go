package integrations

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type RazorpayPaymentStatus string

const (
	RazorpayPaymentStatusCreated    RazorpayPaymentStatus = "created"
	RazorpayPaymentStatusAuthorized RazorpayPaymentStatus = "authorized"
	RazorpayPaymentStatusCaptured   RazorpayPaymentStatus = "captured"
	RazorpayPaymentStatusRefunded   RazorpayPaymentStatus = "refunded"
	RazorpayPaymentStatusFailed     RazorpayPaymentStatus = "failed"
)

// ToFlexpricePaymentStatus maps a Razorpay payment status to a FlexPrice PaymentStatus.
func (s RazorpayPaymentStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case RazorpayPaymentStatusCaptured:
		return types.PaymentStatusSucceeded, nil
	case RazorpayPaymentStatusFailed:
		return types.PaymentStatusFailed, nil
	default:
		return "", ierr.NewError("unmapped razorpay payment status").
			WithReportableDetails(map[string]interface{}{
				"razorpay_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}
