package integrations

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

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

// ToFlexpricePaymentStatus maps a Stripe PaymentIntent status to a FlexPrice PaymentStatus.
func (s StripePaymentStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case StripePaymentStatusSucceeded:
		return types.PaymentStatusSucceeded, nil
	case StripePaymentStatusRequiresPaymentMethod, StripePaymentStatusCanceled:
		return types.PaymentStatusFailed, nil
	default:
		return "", ierr.NewError("unmapped stripe payment status").
			WithReportableDetails(map[string]interface{}{
				"stripe_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}
