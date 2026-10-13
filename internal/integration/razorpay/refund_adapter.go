package razorpay

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type RefundAdapter struct {
	Client RazorpayClient
	Logger *logger.Logger
}

func (a *RefundAdapter) RefundPayment(ctx context.Context, req interfaces.RefundProviderRequest) (*interfaces.RefundProviderResponse, error) {
	if req.GatewayPaymentID == "" {
		return nil, ierr.NewError("razorpay payment id is required").
			WithHint("A gateway payment id is required to issue a Razorpay refund").
			Mark(ierr.ErrValidation)
	}

	result, err := a.Client.RefundPayment(ctx, req.GatewayPaymentID, toPaise(req.Amount), req.IdempotencyKey)
	if err != nil {
		return nil, err
	}

	refundID, _ := result["id"].(string)
	status := normalizeRazorpayRefundStatus(result["status"])

	settled := decimal.Zero
	if status == types.RefundStatusSucceeded {
		settled = req.Amount
	}

	return &interfaces.RefundProviderResponse{
		GatewayRefundID: refundID,
		Status:          status,
		SettledAmount:   settled,
		Metadata:        result,
	}, nil
}

// Razorpay holds a normal refund at "pending" until the bank confirms it;
// instant refunds come back "processed" on the first call.
func normalizeRazorpayRefundStatus(raw interface{}) types.RefundStatus {
	status, _ := raw.(string)
	switch status {
	case "processed":
		return types.RefundStatusSucceeded
	case "failed":
		return types.RefundStatusFailed
	default:
		return types.RefundStatusProcessing
	}
}
