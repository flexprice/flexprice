package chargebee

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type RefundAdapter struct {
	Client ChargebeeClient
	Logger *logger.Logger
}

func (a *RefundAdapter) RefundPayment(ctx context.Context, req interfaces.RefundProviderRequest) (*interfaces.RefundProviderResponse, error) {
	if req.GatewayPaymentID == "" {
		return nil, ierr.NewError("chargebee transaction id is required").
			WithHint("A gateway transaction id is required to issue a Chargebee refund").
			Mark(ierr.ErrValidation)
	}

	refund, err := a.Client.RefundTransaction(
		ctx,
		req.GatewayPaymentID,
		amountToMinorUnits(req.Amount, req.Currency),
		idempotencyScoped(req.IdempotencyKey, "refund"),
	)
	if err != nil {
		return nil, err
	}

	var status types.RefundStatus
	switch classifyTransaction(refund.Status) {
	case transactionSettled:
		status = types.RefundStatusSucceeded
	case transactionFailed:
		status = types.RefundStatusFailed
	default:
		status = types.RefundStatusProcessing
	}

	settled := decimal.Zero
	if status == types.RefundStatusSucceeded {
		settled = decimal.NewFromInt(refund.Amount).Shift(-types.GetCurrencyPrecision(req.Currency))
	}

	return &interfaces.RefundProviderResponse{
		GatewayRefundID: refund.Id,
		Status:          status,
		SettledAmount:   settled,
		Metadata: map[string]interface{}{
			"chargebee_transaction_id": refund.Id,
			"status":                   string(refund.Status),
			"amount_minor":             refund.Amount,
		},
	}, nil
}
