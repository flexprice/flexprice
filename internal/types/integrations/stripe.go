package integrations

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// StripePaymentStatus is a Stripe PaymentIntent status.
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

// ToFlexpricePaymentStatus maps a PaymentIntent status. In-flight states return empty+nil
// (pending); requires_payment_method means declined; anything unrecognised is an error.
func (s StripePaymentStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case StripePaymentStatusSucceeded:
		return types.PaymentStatusSucceeded, nil
	case StripePaymentStatusRequiresPaymentMethod, StripePaymentStatusCanceled:
		return types.PaymentStatusFailed, nil
	case StripePaymentStatusRequiresConfirmation, StripePaymentStatusRequiresAction,
		StripePaymentStatusRequiresCapture, StripePaymentStatusProcessing:
		return "", nil
	default:
		return "", ierr.NewError("unmapped stripe payment status").
			WithReportableDetails(map[string]interface{}{
				"stripe_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

// StripeCheckoutSessionStatus is a Stripe Checkout Session status: whether the customer
// is done with the hosted page.
type StripeCheckoutSessionStatus string

const (
	StripeCheckoutSessionStatusOpen     StripeCheckoutSessionStatus = "open"
	StripeCheckoutSessionStatusComplete StripeCheckoutSessionStatus = "complete"
	StripeCheckoutSessionStatusExpired  StripeCheckoutSessionStatus = "expired"
)

// StripeCheckoutPaymentStatus is a Checkout Session's payment_status: whether the money
// behind the page has arrived.
type StripeCheckoutPaymentStatus string

const (
	StripeCheckoutPaymentStatusPaid              StripeCheckoutPaymentStatus = "paid"
	StripeCheckoutPaymentStatusUnpaid            StripeCheckoutPaymentStatus = "unpaid"
	StripeCheckoutPaymentStatusNoPaymentRequired StripeCheckoutPaymentStatus = "no_payment_required"
)

// ToFlexpricePaymentStatus needs both status and payment_status to answer "is this paid?":
// complete+unpaid is a delayed method (bank debit) still pending, and anything
// unrecognised is an error rather than a silent pending.
func (s StripeCheckoutSessionStatus) ToFlexpricePaymentStatus(paymentStatus StripeCheckoutPaymentStatus) (types.PaymentStatus, error) {
	switch s {
	case StripeCheckoutSessionStatusExpired:
		return types.PaymentStatusFailed, nil
	case StripeCheckoutSessionStatusOpen:
		return "", nil
	case StripeCheckoutSessionStatusComplete:
		switch paymentStatus {
		case StripeCheckoutPaymentStatusPaid, StripeCheckoutPaymentStatusNoPaymentRequired:
			return types.PaymentStatusSucceeded, nil
		case StripeCheckoutPaymentStatusUnpaid:
			return "", nil
		default:
			return "", ierr.NewError("unmapped stripe checkout payment status").
				WithReportableDetails(map[string]interface{}{
					"stripe_session_status": s,
					"stripe_payment_status": paymentStatus,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	default:
		return "", ierr.NewError("unmapped stripe checkout session status").
			WithReportableDetails(map[string]interface{}{
				"stripe_session_status": s,
				"stripe_payment_status": paymentStatus,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}
