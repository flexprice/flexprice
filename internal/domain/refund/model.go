package refund

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Refund represents one settlement attempt of refunded value.
type Refund struct {
	ID                      string                  `json:"id"`
	PaymentID               *string                 `json:"payment_id,omitempty"`
	InvoiceID               string                  `json:"invoice_id"`
	CreditNoteID            *string                 `json:"credit_note_id,omitempty"`
	PaymentGateway          *string                 `json:"payment_gateway,omitempty"`
	GatewayRefundID         *string                 `json:"gateway_refund_id,omitempty"`
	GatewayTrackingID       *string                 `json:"gateway_tracking_id,omitempty"`
	Amount                  decimal.Decimal         `json:"amount" swaggertype:"string"`
	SettledAmount           decimal.Decimal         `json:"settled_amount" swaggertype:"string"`
	Currency                string                  `json:"currency"`
	RefundStatus            types.RefundStatus      `json:"refund_status"`
	RefundReason            types.RefundReason      `json:"refund_reason"`
	RefundDestination       types.RefundDestination `json:"refund_destination"`
	RefundDestinationID     *string                 `json:"refund_destination_id,omitempty"`
	Attempt                 int                     `json:"attempt"`
	IdempotencyKey          string                  `json:"idempotency_key"`
	GatewayIdempotencyToken *string                 `json:"gateway_idempotency_token,omitempty"`
	FailureReason           *string                 `json:"failure_reason,omitempty"`
	Metadata                types.Metadata          `json:"metadata,omitempty" activity:"drop"`
	GatewayMetadata         map[string]interface{}  `json:"gateway_metadata,omitempty" activity:"redact"`
	InitiatedAt             *time.Time              `json:"initiated_at,omitempty"`
	SucceededAt             *time.Time              `json:"succeeded_at,omitempty"`
	FailedAt                *time.Time              `json:"failed_at,omitempty"`
	CancelledAt             *time.Time              `json:"cancelled_at,omitempty"`
	EnvironmentID           string                  `json:"environment_id"`

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
		InvoiceID:               r.InvoiceID,
		CreditNoteID:            r.CreditNoteID,
		PaymentGateway:          r.PaymentGateway,
		GatewayRefundID:         r.GatewayRefundID,
		GatewayTrackingID:       r.GatewayTrackingID,
		Amount:                  r.Amount,
		SettledAmount:           r.SettledAmount,
		Currency:                r.Currency,
		RefundStatus:            types.RefundStatus(r.RefundStatus),
		RefundReason:            types.RefundReason(r.RefundReason),
		RefundDestination:       types.RefundDestination(r.RefundDestination),
		RefundDestinationID:     r.RefundDestinationID,
		Attempt:                 r.Attempt,
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

func (r *Refund) IsSettled() bool {
	if r == nil {
		return false
	}
	return r.RefundStatus.IsSettled()
}

func (r *Refund) NeedsGatewayDispatch() bool {
	if r == nil {
		return false
	}
	return r.RefundDestination == types.RefundDestinationGateway &&
		r.RefundStatus == types.RefundStatusPending
}

func (r *Refund) SettledOrZero() decimal.Decimal {
	if r == nil || !r.IsSettled() {
		return decimal.Zero
	}
	return r.SettledAmount
}
