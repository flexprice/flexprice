package payload

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/shopspring/decimal"
)

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

type PaymentPayloadBuilder struct {
	services *Services
}

func NewPaymentPayloadBuilder(services *Services) PayloadBuilder {
	return &PaymentPayloadBuilder{services: services}
}

func (b *PaymentPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalPaymentEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal payment event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	paymentID, tenantID := parsedPayload.PaymentID, parsedPayload.TenantID
	if paymentID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for payment event").
			WithHint("Please provide a valid payment ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	payment, err := b.services.PaymentService.GetPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	payload := NewPaymentWebhookPayload(payment, eventType)

	return json.Marshal(payload)
}
