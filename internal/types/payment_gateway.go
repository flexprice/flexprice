package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PaymentGatewayType represents the type of payment gateway
type PaymentGatewayType string

const (
	PaymentGatewayTypeStripe    PaymentGatewayType = "stripe"
	PaymentGatewayTypeRazorpay  PaymentGatewayType = "razorpay"
	PaymentGatewayTypeNomod     PaymentGatewayType = "nomod"
	PaymentGatewayTypeMoyasar   PaymentGatewayType = "moyasar"
	PaymentGatewayTypePaddle    PaymentGatewayType = "paddle"
	PaymentGatewayTypeWhop      PaymentGatewayType = "whop"
	PaymentGatewayTypeChargebee PaymentGatewayType = "chargebee"
)

// Validate validates the payment gateway type
func (p PaymentGatewayType) Validate() error {
	switch p {
	case PaymentGatewayTypeStripe, PaymentGatewayTypeRazorpay, PaymentGatewayTypeNomod, PaymentGatewayTypeMoyasar, PaymentGatewayTypePaddle, PaymentGatewayTypeWhop, PaymentGatewayTypeChargebee:
		return nil
	default:
		return ierr.NewError("invalid payment gateway type").
			WithHint("Please provide a valid payment gateway type").
			WithReportableDetails(map[string]any{
				"allowed": []PaymentGatewayType{
					PaymentGatewayTypeStripe,
					PaymentGatewayTypeChargebee,
					PaymentGatewayTypeRazorpay,
					PaymentGatewayTypeNomod,
					PaymentGatewayTypeMoyasar,
					PaymentGatewayTypePaddle,
					PaymentGatewayTypeWhop,
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

// PaymentGatewayFromSecretProvider maps a connection's provider onto the gateway
// it configures. ok=false means the provider is not a payment gateway.
func PaymentGatewayFromSecretProvider(p SecretProvider) (PaymentGatewayType, bool) {
	switch p {
	case SecretProviderStripe:
		return PaymentGatewayTypeStripe, true
	case SecretProviderRazorpay:
		return PaymentGatewayTypeRazorpay, true
	case SecretProviderNomod:
		return PaymentGatewayTypeNomod, true
	case SecretProviderMoyasar:
		return PaymentGatewayTypeMoyasar, true
	case SecretProviderPaddle:
		return PaymentGatewayTypePaddle, true
	case SecretProviderWhop:
		return PaymentGatewayTypeWhop, true
	case SecretProviderChargebee:
		return PaymentGatewayTypeChargebee, true
	default:
		return "", false
	}
}
