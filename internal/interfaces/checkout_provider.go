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

	// CreateAuthorizationLink registers a payment instrument for future off-session
	// charges (a UPI/e-mandate, a saved card, etc.), optionally charging the first
	// invoice as part of the same authorization. Unsupported providers return an
	// error marked ierr.ErrNotImplemented.
	CreateAuthorizationLink(ctx context.Context, req AuthorizationLinkRequest) (*CheckoutProviderResponse, error)

	// TryAutoChargingSavedMethod attempts an off-session charge against a previously
	// registered instrument. charged=false means no usable method (caller should
	// fall back to CreateAuthorizationLink). A hard provider error is returned as err.
	TryAutoChargingSavedMethod(ctx context.Context, req AuthorizationLinkRequest) (resp *CheckoutProviderResponse, charged bool, err error)

	// FetchPaymentState asks the provider what happened to a checkout's payment, so a
	// session can be reconciled when the webhook was late, dropped, or errored.
	//
	// The handles are the ones this adapter produced at creation, and only it knows
	// what kind of object each is, so interpreting them is the adapter's job.
	// Providers without a read API return ierr.ErrNotImplemented.
	FetchPaymentState(ctx context.Context, req PaymentStateRequest) (*PaymentState, error)
}

// PaymentStateRequest carries the provider handles recorded on the payment at
// checkout creation. At least one is set, depending on provider and collection method.
type PaymentStateRequest struct {
	// GatewayPaymentID is the provider's payment id when known. Preferred by adapters:
	// it is the object that actually settles.
	GatewayPaymentID string
	// GatewayTrackingID is the pre-payment handle — link, hosted page, invoice or
	// order — that exists only to answer "has anyone paid yet?".
	GatewayTrackingID string
	// InvoiceID is the FlexPrice invoice being settled; some providers key lookups on
	// it (Razorpay orders carry it as the receipt).
	InvoiceID string
}

// PaymentState is what a provider reports about a checkout's payment right now.
type PaymentState struct {
	// Status is empty for a non-terminal state, meaning "still pending, no
	// transition" — not a failure.
	Status types.PaymentStatus
	// GatewayPaymentID is the provider's payment id, empty before anyone has paid.
	GatewayPaymentID string
}

// CheckoutProviderRequest is the unified input for all checkout provider adapters.
type CheckoutProviderRequest struct {
	InvoiceID  string
	CustomerID string
	Amount     decimal.Decimal
	Currency   string
	PaymentID  string // FlexPrice payment ID — embedded in provider metadata for idempotency
	SuccessURL string
	FailureURL string
	CancelURL  string
	Metadata   map[string]string
	// ExpiresAt is when the hosted object should stop accepting payments. Providers
	// that cannot express this ignore it.
	ExpiresAt *time.Time
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
	// CustomerPresent declares an off-session charge as customer-initiated. Providers
	// that cannot express CIT/MIT ignore it.
	CustomerPresent bool
	InvoiceID       string
	CustomerID      string
	PaymentID       string
	Amount          decimal.Decimal
	Currency        string
	MaxAmount       *decimal.Decimal // nil = no ceiling (e.g. plain saved card); set = mandate-style cap (e.g. UPI)
	ExpiresAt       *time.Time
	PreferredMethod types.PaymentMethodType
	SuccessURL      string
	CancelURL       string
	Metadata        map[string]string
}

// ProviderPaymentMethod is a normalized view of one active, usable token at the
// gateway. Never persisted — read fresh on every call.
type ProviderPaymentMethod struct {
	GatewayMethodID  string                  // opaque id at the gateway
	Method           types.PaymentMethodType // e.g. PaymentMethodTypeUPI
	MaxAmount        *decimal.Decimal
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	ProviderMetadata map[string]string

	// Card is set only for card methods; nil for UPI, ACH and the rest.
	Card *ProviderCardDetails
	// IsDefault is the gateway's own default/primary flag, which is what decides
	// the card charged when no method is named. Scoped per provider.
	IsDefault bool
	// Active reports the method is usable now. Expired and unverified methods are
	// reported inactive rather than dropped, so a caller can explain why a saved
	// card stopped working.
	Active bool
	// GatewayAccountID is where this method is vaulted. Internal only — never put
	// it in dto.SavedPaymentMethod. It exists so a split vault (cards spread over
	// several gateway accounts) is diagnosable.
	GatewayAccountID string
}

// ProviderCardDetails is the displayable part of a vaulted card. Field names
// follow Stripe's card object; Chargebee says expiry_month/expiry_year and its
// adapter normalises.
type ProviderCardDetails struct {
	Brand    string
	Last4    string
	ExpMonth int
	ExpYear  int
}
