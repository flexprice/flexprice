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

// RazorpayPaymentLinkStatus is Razorpay's payment-link (plink_xxx) status enum,
// which is a separate lifecycle from the payment (pay_xxx) enum above. A payment
// link is created, may collect one or more attempts (partially_paid), and lands
// in a terminal state of paid, expired, or cancelled.
type RazorpayPaymentLinkStatus string

const (
	RazorpayPaymentLinkStatusCreated       RazorpayPaymentLinkStatus = "created"
	RazorpayPaymentLinkStatusPartiallyPaid RazorpayPaymentLinkStatus = "partially_paid"
	RazorpayPaymentLinkStatusPaid          RazorpayPaymentLinkStatus = "paid"
	RazorpayPaymentLinkStatusExpired       RazorpayPaymentLinkStatus = "expired"
	RazorpayPaymentLinkStatusCancelled     RazorpayPaymentLinkStatus = "cancelled"
)

// ToFlexpricePaymentStatus maps a Razorpay payment link status to a FlexPrice
// PaymentStatus. Non-terminal states (created / partially_paid) return an empty
// PaymentStatus with a nil error to signal "still pending, no transition".
func (s RazorpayPaymentLinkStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case RazorpayPaymentLinkStatusPaid:
		return types.PaymentStatusSucceeded, nil
	case RazorpayPaymentLinkStatusExpired, RazorpayPaymentLinkStatusCancelled:
		return types.PaymentStatusFailed, nil
	case RazorpayPaymentLinkStatusCreated, RazorpayPaymentLinkStatusPartiallyPaid:
		return "", nil
	default:
		return "", ierr.NewError("unmapped razorpay payment link status").
			WithReportableDetails(map[string]interface{}{
				"razorpay_payment_link_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

// RazorpayInvoiceStatus is Razorpay's invoice (inv_xxx) status enum. Checkout uses an
// invoice as the mandate-registration link, so this is the lifecycle a
// charge_automatically checkout without a saved token sits in.
type RazorpayInvoiceStatus string

const (
	RazorpayInvoiceStatusDraft         RazorpayInvoiceStatus = "draft"
	RazorpayInvoiceStatusIssued        RazorpayInvoiceStatus = "issued"
	RazorpayInvoiceStatusPartiallyPaid RazorpayInvoiceStatus = "partially_paid"
	RazorpayInvoiceStatusPaid          RazorpayInvoiceStatus = "paid"
	RazorpayInvoiceStatusCancelled     RazorpayInvoiceStatus = "cancelled"
	RazorpayInvoiceStatusExpired       RazorpayInvoiceStatus = "expired"
	RazorpayInvoiceStatusDeleted       RazorpayInvoiceStatus = "deleted"
)

// ToFlexpricePaymentStatus maps a Razorpay invoice status to a FlexPrice one.
// Non-terminal states return an empty status and nil error: "still pending".
func (s RazorpayInvoiceStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case RazorpayInvoiceStatusPaid:
		return types.PaymentStatusSucceeded, nil
	case RazorpayInvoiceStatusCancelled, RazorpayInvoiceStatusExpired, RazorpayInvoiceStatusDeleted:
		return types.PaymentStatusFailed, nil
	case RazorpayInvoiceStatusDraft, RazorpayInvoiceStatusIssued, RazorpayInvoiceStatusPartiallyPaid:
		return "", nil
	default:
		return "", ierr.NewError("unmapped razorpay invoice status").
			WithReportableDetails(map[string]interface{}{
				"razorpay_invoice_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}

// RazorpayOrderStatus is Razorpay's order (order_xxx) status enum. An order is the
// idempotency container for a saved-token charge; "attempted" means submitted but not
// captured, so it is not yet an outcome.
type RazorpayOrderStatus string

const (
	RazorpayOrderStatusCreated   RazorpayOrderStatus = "created"
	RazorpayOrderStatusAttempted RazorpayOrderStatus = "attempted"
	RazorpayOrderStatusPaid      RazorpayOrderStatus = "paid"
)

// ToFlexpricePaymentStatus maps a Razorpay order status to a FlexPrice one.
// Non-terminal states return an empty status and nil error: "still pending".
func (s RazorpayOrderStatus) ToFlexpricePaymentStatus() (types.PaymentStatus, error) {
	switch s {
	case RazorpayOrderStatusPaid:
		return types.PaymentStatusSucceeded, nil
	case RazorpayOrderStatusCreated, RazorpayOrderStatusAttempted:
		return "", nil
	default:
		return "", ierr.NewError("unmapped razorpay order status").
			WithReportableDetails(map[string]interface{}{
				"razorpay_order_status": s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
}
