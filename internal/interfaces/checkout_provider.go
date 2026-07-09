package interfaces

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutProvider is implemented by each payment gateway that supports hosted checkout.
type CheckoutProvider interface {
	CreatePaymentLink(ctx context.Context, req CheckoutProviderRequest) (*CheckoutProviderResponse, error)

	// CreateAuthorizationLink registers a payment instrument for future
	// off-session charges (a UPI/e-mandate, a saved card, etc.), optionally
	// charging the first invoice as part of the same authorization. Providers
	// that don't support this return an error marked ierr.ErrNotImplemented.
	CreateAuthorizationLink(ctx context.Context, req AuthorizationLinkRequest) (*CheckoutProviderResponse, error)

	// ListSavedPaymentMethods returns the customer's currently usable payment
	// methods/tokens at the gateway, read live — never cached, never persisted
	// locally. Providers that don't support this return ierr.ErrNotImplemented.
	ListSavedPaymentMethods(ctx context.Context, req ListSavedPaymentMethodsRequest) ([]*ProviderPaymentMethod, error)

	// ChargeSavedPaymentMethod charges a specific GatewayMethodID (from
	// ListSavedPaymentMethods) for a given amount. Providers that don't support
	// this return ierr.ErrNotImplemented.
	ChargeSavedPaymentMethod(ctx context.Context, req ChargeSavedPaymentMethodRequest) (*ChargeResult, error)
}

// CheckoutProviderRequest is the unified input for all checkout provider adapters.
type CheckoutProviderRequest struct {
	InvoiceID     string
	CustomerID    string
	Amount        decimal.Decimal
	Currency      string
	PaymentID     string // FlexPrice payment ID — embedded in provider metadata for idempotency
	EnvironmentID string
	SuccessURL    string
	FailureURL    string
	CancelURL     string
	Metadata      map[string]string
}

// CheckoutProviderResponse is the unified output from all checkout provider adapters.
type CheckoutProviderResponse struct {
	ProviderSessionID       string              // stored in EntityIntegrationMapping
	NextAction              types.PaymentAction // type + URL for the customer
	ProviderPaymentIntentID string              // charge/intent ID, stored after payment confirmation
	ExpiresAt               *time.Time          // nil if provider doesn't return expiry
	ProviderMetadata        map[string]string   // debug data only, not for business logic
}

// AuthorizationLinkRequest is the unified input for mandate/authorization
// registration across any provider that supports it.
type AuthorizationLinkRequest struct {
	InvoiceID, CustomerID, PaymentID     string
	Amount                               decimal.Decimal
	Currency                             string
	MaxAmount                            *decimal.Decimal // nil = no ceiling (e.g. plain saved card); set = mandate-style cap (e.g. UPI)
	ExpiresAt                            *time.Time
	PreferredMethod                      types.PaymentMethodType
	EnvironmentID, SuccessURL, CancelURL string
	Metadata                             map[string]string
}

type ListSavedPaymentMethodsRequest struct {
	CustomerID, EnvironmentID string
}

// ProviderPaymentMethod is a normalized view of one confirmed, usable token as it
// exists at the gateway right now. Only active tokens are returned — callers never
// need to filter by status. Never persisted — read fresh on every call.
type ProviderPaymentMethod struct {
	GatewayMethodID  string                  // opaque id at the gateway
	Method           types.PaymentMethodType // e.g. PaymentMethodTypeUPI
	MaxAmount        *decimal.Decimal
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	ProviderMetadata map[string]string
}

type ChargeSavedPaymentMethodRequest struct {
	InvoiceID       string
	CustomerID      string
	PaymentID       string
	GatewayMethodID string
	Amount          decimal.Decimal
	Currency        string
	EnvironmentID   string
	Metadata        map[string]string
}

type ChargeResult struct {
	ProviderPaymentIntentID string
	Status                  types.PaymentStatus // PROCESSING (submitted, awaiting webhook) | SUCCEEDED | FAILED
	ProviderMetadata        map[string]string
}
