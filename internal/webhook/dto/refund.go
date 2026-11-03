package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalRefundEvent struct {
	RefundID string `json:"refund_id"`
	TenantID string `json:"tenant_id"`
}

type Refund struct {
	ID                  string                  `json:"id"`
	InvoiceID           string                  `json:"invoice_id"`
	PaymentID           *string                 `json:"payment_id,omitempty"`
	CreditNoteID        *string                 `json:"credit_note_id,omitempty"`
	Amount              decimal.Decimal         `json:"amount" swaggertype:"string"`
	SettledAmount       decimal.Decimal         `json:"settled_amount" swaggertype:"string"`
	Currency            string                  `json:"currency"`
	RefundStatus        types.RefundStatus      `json:"refund_status"`
	RefundReason        types.RefundReason      `json:"refund_reason"`
	RefundDestination   types.RefundDestination `json:"refund_destination"`
	RefundDestinationID *string                 `json:"refund_destination_id,omitempty"`
	PaymentGateway      *string                 `json:"payment_gateway,omitempty"`
	GatewayRefundID     *string                 `json:"gateway_refund_id,omitempty"`
	FailureReason       *string                 `json:"failure_reason,omitempty"`
	Attempt             int                     `json:"attempt"`
	SucceededAt         *time.Time              `json:"succeeded_at,omitempty"`
	FailedAt            *time.Time              `json:"failed_at,omitempty"`
	Metadata            types.Metadata          `json:"metadata,omitempty"`
}

func NewRefund(resp *dto.RefundResponse) *Refund {
	if resp == nil || resp.Refund == nil {
		return nil
	}
	return &Refund{
		ID:                  resp.ID,
		InvoiceID:           resp.InvoiceID,
		PaymentID:           resp.PaymentID,
		CreditNoteID:        resp.CreditNoteID,
		Amount:              resp.Amount,
		SettledAmount:       resp.SettledAmount,
		Currency:            resp.Currency,
		RefundStatus:        resp.RefundStatus,
		RefundReason:        resp.RefundReason,
		RefundDestination:   resp.RefundDestination,
		RefundDestinationID: resp.RefundDestinationID,
		PaymentGateway:      resp.PaymentGateway,
		GatewayRefundID:     resp.GatewayRefundID,
		FailureReason:       resp.FailureReason,
		Attempt:             resp.Attempt,
		SucceededAt:         resp.SucceededAt,
		FailedAt:            resp.FailedAt,
		Metadata:            resp.Metadata,
	}
}

type RefundWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Refund    *Refund                `json:"refund"`
}

func NewRefundWebhookPayload(refund *dto.RefundResponse, eventType types.WebhookEventName) *RefundWebhookPayload {
	return &RefundWebhookPayload{EventType: eventType, Refund: NewRefund(refund)}
}
