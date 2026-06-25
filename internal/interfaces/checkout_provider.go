package interfaces

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutProvider is implemented by each payment gateway that supports hosted checkout.
// Adapters wrap the existing provider PaymentService.CreatePaymentLink() without modifying it.
type CheckoutProvider interface {
	CreatePaymentLink(
		ctx context.Context,
		req CheckoutProviderRequest,
		customerSvc CustomerService,
		invoiceSvc InvoiceService,
	) (*CheckoutProviderResponse, error)
}

// CheckoutProviderRequest is the unified input for all checkout provider adapters.
type CheckoutProviderRequest struct {
	InvoiceID     string
	CustomerID    string
	Amount        string // decimal string, e.g. "99.00"
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
