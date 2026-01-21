package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PaymentGatewayType represents the type of payment gateway
type PaymentGatewayType string

const (
	PaymentGatewayTypeStripe   PaymentGatewayType = "stripe"
	PaymentGatewayTypeRazorpay PaymentGatewayType = "razorpay"
	PaymentGatewayTypeNomod    PaymentGatewayType = "nomod"
	PaymentGatewayTypeMoyasar  PaymentGatewayType = "moyasar"
)

// Validate validates the payment gateway type
func (p PaymentGatewayType) Validate() error {
	switch p {
	case PaymentGatewayTypeStripe, PaymentGatewayTypeRazorpay, PaymentGatewayTypeNomod, PaymentGatewayTypeMoyasar:
		return nil
	default:
		return ierr.NewError("invalid payment gateway type").
			WithHint("Please provide a valid payment gateway type").
			WithReportableDetails(map[string]any{
				"allowed": []PaymentGatewayType{
					PaymentGatewayTypeStripe,
					PaymentGatewayTypeRazorpay,
					PaymentGatewayTypeNomod,
					PaymentGatewayTypeMoyasar,
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
	WebhookEventTypeProductCreated                       WebhookEventType = "product.created"
	WebhookEventTypeProductUpdated                       WebhookEventType = "product.updated"
	WebhookEventTypeProductDeleted                       WebhookEventType = "product.deleted"
	WebhookEventTypeSubscriptionCreated                  WebhookEventType = "customer.subscription.created"
	WebhookEventTypeSubscriptionUpdated                  WebhookEventType = "customer.subscription.updated"
	WebhookEventTypeSubscriptionDeleted                  WebhookEventType = "customer.subscription.deleted"
	WebhookEventTypePaymentIntentSucceeded               WebhookEventType = "payment_intent.succeeded"
)
