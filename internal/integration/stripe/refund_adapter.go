package stripe

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	stripeapi "github.com/stripe/stripe-go/v82"
)

// RefundAdapter issues refund-ledger refunds through Stripe.
type RefundAdapter struct {
	Client *Client
	Logger *logger.Logger
}

func (a *RefundAdapter) RefundPayment(ctx context.Context, req interfaces.RefundProviderRequest) (*interfaces.RefundProviderResponse, error) {
	if req.GatewayPaymentID == "" {
		return nil, ierr.NewError("stripe payment intent id is required").
			WithHint("A gateway payment id is required to issue a Stripe refund").
			Mark(ierr.ErrValidation)
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	params := &stripeapi.RefundCreateParams{
		PaymentIntent: stripeapi.String(req.GatewayPaymentID),
		Amount:        stripeapi.Int64(types.ToSmallestUnit(req.Amount, req.Currency)),
	}
	params.SetIdempotencyKey(req.IdempotencyKey)

	refund, err := stripeClient.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	status := normalizeStripeRefundStatus(refund.Status)
	settled := decimal.Zero
	if status == types.RefundStatusSucceeded {
		settled = req.Amount
	}

	return &interfaces.RefundProviderResponse{
		GatewayRefundID: refund.ID,
		Status:          status,
		SettledAmount:   settled,
		Metadata:        map[string]interface{}{"stripe_refund_status": string(refund.Status)},
	}, nil
}

// Card refunds come back "succeeded" on the first call; other methods can sit at "pending".
func normalizeStripeRefundStatus(status stripeapi.RefundStatus) types.RefundStatus {
	switch status {
	case stripeapi.RefundStatusSucceeded:
		return types.RefundStatusSucceeded
	case stripeapi.RefundStatusFailed, stripeapi.RefundStatusCanceled:
		return types.RefundStatusFailed
	default:
		return types.RefundStatusProcessing
	}
}
