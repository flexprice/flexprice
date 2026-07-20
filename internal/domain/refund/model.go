package refund

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Refund represents a gateway refund transaction linked to a Payment.
type Refund struct {
	ID                      string                 `json:"id"`
	PaymentID               string                 `json:"payment_id"`
	PaymentGateway          string                 `json:"payment_gateway"`
	GatewayRefundID         *string                `json:"gateway_refund_id,omitempty"`
	GatewayTrackingID       *string                `json:"gateway_tracking_id,omitempty"`
	Amount                  decimal.Decimal        `json:"amount" swaggertype:"string"`
	Currency                string                 `json:"currency"`
	RefundStatus            types.RefundStatus     `json:"refund_status"`
	RefundReason            types.RefundReason     `json:"refund_reason"`
	IdempotencyKey          string                 `json:"idempotency_key"`
	GatewayIdempotencyToken string                 `json:"gateway_idempotency_token"`
	FailureReason           *string                `json:"failure_reason,omitempty"`
	Metadata                types.Metadata         `json:"metadata,omitempty"`
	GatewayMetadata         map[string]interface{} `json:"gateway_metadata,omitempty"`
	InitiatedAt             *time.Time             `json:"initiated_at,omitempty"`
	SucceededAt             *time.Time             `json:"succeeded_at,omitempty"`
	FailedAt                *time.Time             `json:"failed_at,omitempty"`
	CancelledAt             *time.Time             `json:"cancelled_at,omitempty"`
	EnvironmentID           string                 `json:"environment_id"`

	types.BaseModel
}

// TableName returns the table name for the refund.
func (r *Refund) TableName() string {
	return "refunds"
}

// FromEnt converts an Ent Refund to a domain Refund.
func FromEnt(r *ent.Refund) *Refund {
	if r == nil {
		return nil
	}

	return &Refund{
		ID:                      r.ID,
		PaymentID:               r.PaymentID,
		PaymentGateway:          r.PaymentGateway,
		GatewayRefundID:         r.GatewayRefundID,
		GatewayTrackingID:       r.GatewayTrackingID,
		Amount:                  r.Amount,
		Currency:                r.Currency,
		RefundStatus:            types.RefundStatus(r.RefundStatus),
		RefundReason:            types.RefundReason(r.RefundReason),
		IdempotencyKey:          r.IdempotencyKey,
		GatewayIdempotencyToken: r.GatewayIdempotencyToken,
		FailureReason:           r.FailureReason,
		Metadata:                r.Metadata,
		GatewayMetadata:         r.GatewayMetadata,
		InitiatedAt:             r.InitiatedAt,
		SucceededAt:             r.SucceededAt,
		FailedAt:                r.FailedAt,
		CancelledAt:             r.CancelledAt,
		EnvironmentID:           r.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  r.TenantID,
			Status:    types.Status(r.Status),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			CreatedBy: r.CreatedBy,
			UpdatedBy: r.UpdatedBy,
		},
	}
}

// FromEntList converts a slice of Ent Refunds to domain Refunds.
func FromEntList(refunds []*ent.Refund) []*Refund {
	if refunds == nil {
		return nil
	}

	result := make([]*Refund, len(refunds))
	for i, r := range refunds {
		result[i] = FromEnt(r)
	}
	return result
}
