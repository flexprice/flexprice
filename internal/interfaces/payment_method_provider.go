package interfaces

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// PaymentMethodProvider is implemented by each gateway that can vault a payment
// method and let the customer manage it afterwards.
type PaymentMethodProvider interface {
	// ListSavedMethods returns the customer's usable methods at this gateway.
	ListSavedMethods(ctx context.Context, flexCustomerID string) ([]ProviderPaymentMethod, error)

	// DeleteSavedMethod removes one vaulted method.
	DeleteSavedMethod(ctx context.Context, flexCustomerID, gatewayMethodID string) error

	// SetDefaultSavedMethod picks which method is charged when no method is named.
	SetDefaultSavedMethod(ctx context.Context, flexCustomerID, gatewayMethodID string) error

	// CreateSetupLink returns a hosted page on which the customer adds a method.
	CreateSetupLink(ctx context.Context, req SetupLinkRequest) (*SetupLinkResponse, error)
}

// SetupLinkRequest asks a provider for a hosted add-a-payment-method page.
type SetupLinkRequest struct {
	CustomerID string // FlexPrice customer id; the adapter resolves the gateway one
	ReturnURL  string
	MaxAmount  *decimal.Decimal // per-debit ceiling; nil uses the provider default
	SetDefault bool             // make the saved method the default, where the provider supports it
}

// SetupLinkResponse is the hosted page the customer is sent to.
type SetupLinkResponse struct {
	URL               string
	ProviderSessionID string
	ExpiresAt         *time.Time
	Status            string // provider's link status, e.g. Razorpay's issued, paid, expired or cancelled
	CreatedAt         time.Time
}
