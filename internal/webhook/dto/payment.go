package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalPaymentEvent struct {
	PaymentID string `json:"payment_id"`
	TenantID  string `json:"tenant_id"`
}

type Payment struct {
	ID                string                       `json:"id"`
	DestinationType   types.PaymentDestinationType `json:"destination_type"`
	DestinationID     string                       `json:"destination_id"`
	PaymentMethodType types.PaymentMethodType      `json:"payment_method_type"`
	Amount            decimal.Decimal              `json:"amount" swaggertype:"string"`
	Currency          string                       `json:"currency"`
	PaymentStatus     types.PaymentStatus          `json:"payment_status"`
	PaymentGateway    *string                      `json:"payment_gateway,omitempty"`
	SucceededAt       *time.Time                   `json:"succeeded_at,omitempty"`
	FailedAt          *time.Time                   `json:"failed_at,omitempty"`
	RefundedAt        *time.Time                   `json:"refunded_at,omitempty"`
	VoidedAt          *time.Time                   `json:"voided_at,omitempty"`
	ErrorMessage      *string                      `json:"error_message,omitempty"`
	InvoiceNumber     *string                      `json:"invoice_number,omitempty"`
}

func NewPayment(resp *dto.PaymentResponse) *Payment {
	if resp == nil {
		return nil
	}
	return &Payment{
		ID:                resp.ID,
		DestinationType:   resp.DestinationType,
		DestinationID:     resp.DestinationID,
		PaymentMethodType: resp.PaymentMethodType,
		Amount:            resp.Amount,
		Currency:          resp.Currency,
		PaymentStatus:     resp.PaymentStatus,
		PaymentGateway:    resp.PaymentGateway,
		SucceededAt:       resp.SucceededAt,
		FailedAt:          resp.FailedAt,
		RefundedAt:        resp.RefundedAt,
		VoidedAt:          resp.VoidedAt,
		ErrorMessage:      resp.ErrorMessage,
		InvoiceNumber:     resp.InvoiceNumber,
	}
}

type PaymentWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Payment   *Payment               `json:"payment"`
}

func NewPaymentWebhookPayload(payment *dto.PaymentResponse, eventType types.WebhookEventName) *PaymentWebhookPayload {
	return &PaymentWebhookPayload{EventType: eventType, Payment: NewPayment(payment)}
}
