package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PaymentGatewayType represents the type of payment gateway
type PaymentGatewayType string

const (
	PaymentGatewayTypeStripe   PaymentGatewayType = "stripe"
	PaymentGatewayTypeRazorpay PaymentGatewayType = "razorpay"
	PaymentGatewayTypeFinix    PaymentGatewayType = "finix"
)

// Validate validates the payment gateway type
func (p PaymentGatewayType) Validate() error {
	switch p {
	case PaymentGatewayTypeStripe, PaymentGatewayTypeRazorpay, PaymentGatewayTypeFinix:
		return nil
	default:
		return ierr.NewError("invalid payment gateway type").
			WithHint("Please provide a valid payment gateway type").
			WithReportableDetails(map[string]any{
				"allowed": []PaymentGatewayType{
					PaymentGatewayTypeStripe,
					PaymentGatewayTypeRazorpay,
					PaymentGatewayTypeFinix,
				},
			}).
			Mark(ierr.ErrValidation)
	}
}

// String returns the string representation of the payment gateway type
func (p PaymentGatewayType) String() string {
	return string(p)
}

// WebhookEventType represents the type of webhook event
type WebhookEventType string

const (
	// Stripe webhook events
	WebhookEventTypeCheckoutSessionCompleted             WebhookEventType = "checkout.session.completed"
	WebhookEventTypeCheckoutSessionAsyncPaymentSucceeded WebhookEventType = "checkout.session.async_payment_succeeded"
	WebhookEventTypeCheckoutSessionAsyncPaymentFailed    WebhookEventType = "checkout.session.async_payment_failed"
	WebhookEventTypeCheckoutSessionExpired               WebhookEventType = "checkout.session.expired"
	WebhookEventTypeCustomerCreated                      WebhookEventType = "customer.created"
	WebhookEventTypePaymentIntentPaymentFailed           WebhookEventType = "payment_intent.payment_failed"
	WebhookEventTypeInvoicePaymentPaid                   WebhookEventType = "invoice_payment.paid"
	WebhookEventTypeSetupIntentSucceeded                 WebhookEventType = "setup_intent.succeeded"
)

// Validate validates the webhook event type
func (w WebhookEventType) Validate() error {
	switch w {
	case WebhookEventTypeCheckoutSessionCompleted,
		WebhookEventTypeCheckoutSessionAsyncPaymentSucceeded,
		WebhookEventTypeCheckoutSessionAsyncPaymentFailed,
		WebhookEventTypeCheckoutSessionExpired,
		WebhookEventTypeCustomerCreated,
		WebhookEventTypePaymentIntentPaymentFailed,
		WebhookEventTypeInvoicePaymentPaid,
		WebhookEventTypeSetupIntentSucceeded:
		return nil
	default:
		return ierr.NewError("invalid webhook event type").
			WithHint("Please provide a valid webhook event type").
			WithReportableDetails(map[string]any{
				"allowed": []WebhookEventType{
					WebhookEventTypeCheckoutSessionCompleted,
					WebhookEventTypeCheckoutSessionAsyncPaymentSucceeded,
					WebhookEventTypeCheckoutSessionAsyncPaymentFailed,
					WebhookEventTypeCheckoutSessionExpired,
					WebhookEventTypeCustomerCreated,
					WebhookEventTypePaymentIntentPaymentFailed,
					WebhookEventTypeInvoicePaymentPaid,
					WebhookEventTypeSetupIntentSucceeded,
				},
			}).
			Mark(ierr.ErrValidation)
	}
}

// String returns the string representation of the webhook event type
func (w WebhookEventType) String() string {
	return string(w)
}

// GetGatewayFromEventType returns the payment gateway type from a webhook event type
func (w WebhookEventType) GetGatewayFromEventType() PaymentGatewayType {
	switch w {
	case WebhookEventTypeCheckoutSessionCompleted,
		WebhookEventTypeCheckoutSessionAsyncPaymentSucceeded,
		WebhookEventTypeCheckoutSessionAsyncPaymentFailed,
		WebhookEventTypeCheckoutSessionExpired,
		WebhookEventTypeCustomerCreated,
		WebhookEventTypePaymentIntentPaymentFailed,
		WebhookEventTypeInvoicePaymentPaid,
		WebhookEventTypeSetupIntentSucceeded:
		return PaymentGatewayTypeStripe
	default:
		return PaymentGatewayTypeStripe // Default to Stripe for unknown events
	}
}
