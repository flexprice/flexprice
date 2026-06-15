package checkout

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutSessionRequest is the provider-agnostic request to open a hosted
// checkout session. Objective selects the provider mode (payment vs setup).
type CheckoutSessionRequest struct {
	Objective  types.CheckoutObjective // payment -> charge mode; setup -> card-capture mode
	CheckoutID string                  // flexprice checkout id; goes into provider metadata
	CustomerID string

	// Payment objective only:
	InvoiceID string
	PaymentID string
	Amount    decimal.Decimal
	Currency  string

	SaveCard   bool
	SuccessURL string
	CancelURL  string
	Metadata   map[string]string
}

// CheckoutSessionResponse is the provider-agnostic result.
type CheckoutSessionResponse struct {
	SessionID       string
	URL             string
	PaymentIntentID string
	Status          string
	ExpiresAt       *time.Time
}

// CheckoutProvider creates hosted checkout sessions for a payment provider.
type CheckoutProvider interface {
	CreateCheckoutSession(ctx context.Context, req CheckoutSessionRequest) (*CheckoutSessionResponse, error)
}
