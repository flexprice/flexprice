package payload

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type CheckoutSession struct {
	ID                string                        `json:"id"`
	CustomerID        string                        `json:"customer_id"`
	Action            types.CheckoutAction          `json:"action"`
	CheckoutStatus    types.CheckoutStatus          `json:"checkout_status"`
	PaymentProvider   types.CheckoutPaymentProvider `json:"payment_provider"`
	CheckoutInvoiceID *string                       `json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string                       `json:"checkout_payment_id,omitempty"`
	FailureReason     *string                       `json:"failure_reason,omitempty"`
	ExpiresAt         time.Time                     `json:"expires_at"`
	CompletedAt       *time.Time                    `json:"completed_at,omitempty"`
	CancelledAt       *time.Time                    `json:"cancelled_at,omitempty"`
	PaymentAction     *types.PaymentAction          `json:"payment_action,omitempty"`
}

func NewCheckoutSession(resp *dto.CheckoutSessionResponse) *CheckoutSession {
	if resp == nil || resp.CheckoutSession == nil {
		return nil
	}
	return &CheckoutSession{
		ID:                resp.ID,
		CustomerID:        resp.CustomerID,
		Action:            resp.Action,
		CheckoutStatus:    resp.CheckoutStatus,
		PaymentProvider:   resp.PaymentProvider,
		CheckoutInvoiceID: resp.CheckoutInvoiceID,
		CheckoutPaymentID: resp.CheckoutPaymentID,
		FailureReason:     resp.FailureReason,
		ExpiresAt:         resp.ExpiresAt,
		CompletedAt:       resp.CompletedAt,
		CancelledAt:       resp.CancelledAt,
		PaymentAction:     resp.PaymentAction,
	}
}

// CheckoutSessionWebhookPayload is the outbound payload delivered to subscribers.
type CheckoutSessionWebhookPayload struct {
	EventType       types.WebhookEventName `json:"event_type"`
	CheckoutSession *CheckoutSession       `json:"checkout_session"`
}

func NewCheckoutSessionWebhookPayload(session *dto.CheckoutSessionResponse, eventType types.WebhookEventName) *CheckoutSessionWebhookPayload {
	return &CheckoutSessionWebhookPayload{
		EventType:       eventType,
		CheckoutSession: NewCheckoutSession(session),
	}
}

type CheckoutSessionPayloadBuilder struct {
	services *Services
}

func NewCheckoutSessionPayloadBuilder(services *Services) PayloadBuilder {
	return &CheckoutSessionPayloadBuilder{services: services}
}

func (b *CheckoutSessionPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var ev webhookDto.InternalCheckoutSessionEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal checkout session event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if ev.SessionID == "" || ev.TenantID == "" {
		return nil, ierr.NewError("invalid data for checkout session event").
			WithHint("Please provide a valid session ID and tenant ID").
			Mark(ierr.ErrInvalidOperation)
	}

	session, err := b.services.CheckoutSessionService.Get(ctx, ev.SessionID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(NewCheckoutSessionWebhookPayload(session, eventType))
}
