package payments

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InitiatePaymentParams holds all inputs required to create a payment record
// in INITIATED state before making a gateway call.
type InitiatePaymentParams struct {
	// DestinationType is INVOICE (autopay) or AUTH (save payment method).
	DestinationType types.PaymentDestinationType
	// DestinationID is the invoice_id for INVOICE, customer_id for AUTH.
	DestinationID string
	// PaymentMethodType is the instrument type (CARD, ACH, etc.).
	PaymentMethodType types.PaymentMethodType
	// Gateway is the provider identifier ("moyasar", "stripe", "razorpay", ...).
	Gateway string
	Amount  decimal.Decimal
	// Currency is the ISO 4217 code (USD, SAR, INR, ...).
	Currency string
}

// RecordPaymentSuccessParams holds inputs for marking a payment SUCCEEDED.
type RecordPaymentSuccessParams struct {
	FlexpricePaymentID string
	GatewayPaymentID   string
	SucceededAt        time.Time
}

// RecordPaymentFailureParams holds inputs for marking a payment FAILED.
type RecordPaymentFailureParams struct {
	FlexpricePaymentID string
	GatewayPaymentID   string
	ErrorMessage       string
	FailedAt           time.Time
}

// RecordPaymentVoidedParams holds inputs for marking a payment VOIDED.
type RecordPaymentVoidedParams struct {
	FlexpricePaymentID string
	GatewayPaymentID   string
	VoidedAt           time.Time
}

// RecordPaymentRefundedParams holds inputs for marking a payment REFUNDED.
type RecordPaymentRefundedParams struct {
	FlexpricePaymentID string
	GatewayPaymentID   string
	RefundedAt         time.Time
}
