package interfaces

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// RefundProvider is the unified gateway refund abstraction. Adapters normalise
// onto this shape and add no refund logic of their own.
type RefundProvider interface {
	RefundPayment(ctx context.Context, req RefundProviderRequest) (*RefundProviderResponse, error)
}

type RefundProviderRequest struct {
	// GatewayPaymentID is the provider's identifier for the money being returned:
	// a payment for Razorpay, a transaction for Chargebee.
	GatewayPaymentID string
	Amount           decimal.Decimal
	Currency         string
	// IdempotencyKey must be distinct per refund row: one payment can be
	// refunded more than once.
	IdempotencyKey string
}

type RefundProviderResponse struct {
	GatewayRefundID string
	// Status is SUCCEEDED for gateways that settle inline, PROCESSING when the
	// outcome arrives later by webhook, FAILED when the gateway rejected it.
	Status types.RefundStatus
	// SettledAmount is non-zero only when Status is SUCCEEDED.
	SettledAmount decimal.Decimal
	Metadata      map[string]interface{}
}
