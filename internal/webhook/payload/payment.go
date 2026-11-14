package payload

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

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

	payment, err := b.services.PaymentService.GetStoredPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewPaymentWebhookPayload(payment, eventType)

	return json.Marshal(payload)
}

type PaymentAttemptPayloadBuilder struct {
	services *Services
}

func NewPaymentAttemptPayloadBuilder(services *Services) PayloadBuilder {
	return &PaymentAttemptPayloadBuilder{services: services}
}

func (b *PaymentAttemptPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var event webhookDto.InternalPaymentEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal payment attempt event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if event.PaymentID == "" || event.TenantID == "" || event.AttemptID == "" {
		return nil, ierr.NewError("invalid data for payment attempt event").
			WithHint("Please provide a valid payment ID, tenant ID and attempt ID").
			Mark(ierr.ErrInvalidOperation)
	}

	attempt, err := b.services.PaymentService.GetAttempt(ctx, event.AttemptID)
	if err != nil {
		return nil, err
	}

	payment, err := b.services.PaymentService.GetStoredPayment(ctx, event.PaymentID)
	if err != nil {
		return nil, err
	}

	filter := types.NewDefaultCheckoutSessionFilter()
	filter.CheckoutPaymentIDs = []string{event.PaymentID}
	filter.Status = lo.ToPtr(types.StatusPublished)
	filter.Limit = lo.ToPtr(1)
	sessions, err := b.services.CheckoutSessionService.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	var session *webhookDto.CheckoutSession
	if sessions != nil && len(sessions.Items) > 0 {
		item := sessions.Items[0]
		// List leaves the payment block empty; the session's amount comes from this payment.
		item.Payment = &dto.CheckoutPaymentBlock{ID: payment.ID, Status: payment.PaymentStatus, Amount: payment.Amount, Currency: payment.Currency}
		session = webhookDto.NewCheckoutSession(item)
	}

	return json.Marshal(webhookDto.PaymentAttemptWebhookPayload{
		EventType:       eventType,
		Payment:         webhookDto.NewPayment(payment),
		Attempt:         webhookDto.NewPaymentAttempt(attempt),
		CheckoutSession: session,
	})
}
