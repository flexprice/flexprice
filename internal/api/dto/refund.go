package dto

import (
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type RefundResponse struct {
	*refund.Refund
}

type ListRefundsResponse = types.ListResponse[*RefundResponse]

func NewRefundResponse(r *refund.Refund) *RefundResponse {
	if r == nil {
		return nil
	}
	return &RefundResponse{Refund: r}
}

// SettleRefundRequest records a terminal success against a refund row.
type SettleRefundRequest struct {
	RefundID      string          `json:"refund_id" binding:"required"`
	SettledAmount decimal.Decimal `json:"settled_amount" swaggertype:"string"`
	// DestinationID identifies where the money landed: the gateway refund for a
	// GATEWAY row, the wallet transaction for a WALLET one.
	DestinationID   *string                `json:"destination_id,omitempty"`
	GatewayMetadata map[string]interface{} `json:"gateway_metadata,omitempty"`
}

func (r *SettleRefundRequest) Validate() error {
	if r == nil || r.RefundID == "" {
		return ierr.NewError("missing refund ID").
			WithHint("Please provide a refund ID to settle.").
			Mark(ierr.ErrValidation)
	}
	if !r.SettledAmount.IsPositive() {
		return ierr.NewError("settled amount must be positive").
			WithHint("Please provide the amount that actually settled.").
			Mark(ierr.ErrValidation)
	}
	return nil
}
