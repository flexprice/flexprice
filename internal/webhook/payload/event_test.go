package payload

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectedEventPayloadBuilder_BuildPayload(t *testing.T) {
	b := NewRejectedEventPayloadBuilder(nil)

	internal := webhookDto.InternalRejectedEvent{
		Reason: types.RejectedEventReasonNoMatchingMeter,
		Event: webhookDto.RejectedEventData{
			ID:                 "evt_1",
			EventName:          "api_kall", // typo'd name
			ExternalCustomerID: "cust_ext_9",
			Source:             "sdk",
			Properties:         map[string]interface{}{"tokens": float64(10)},
		},
	}
	data, err := json.Marshal(internal)
	require.NoError(t, err)

	out, err := b.BuildPayload(context.Background(), types.WebhookEventEventRejected, data)
	require.NoError(t, err)

	var payload webhookDto.RejectedEventWebhookPayload
	require.NoError(t, json.Unmarshal(out, &payload))

	assert.Equal(t, types.WebhookEventEventRejected, payload.EventType)
	assert.Equal(t, types.RejectedEventReasonNoMatchingMeter, payload.Reason)
	assert.Equal(t, "evt_1", payload.Event.ID)
	assert.Equal(t, "api_kall", payload.Event.EventName)
	assert.Equal(t, "cust_ext_9", payload.Event.ExternalCustomerID)
}

func TestRejectedEventPayloadBuilder_BuildPayload_InvalidData(t *testing.T) {
	b := NewRejectedEventPayloadBuilder(nil)

	// malformed JSON
	_, err := b.BuildPayload(context.Background(), types.WebhookEventEventRejected, json.RawMessage(`{`))
	assert.Error(t, err)

	// missing required event id/name
	data, _ := json.Marshal(webhookDto.InternalRejectedEvent{
		Reason: types.RejectedEventReasonNoMatchingMeter,
		Event:  webhookDto.RejectedEventData{},
	})
	_, err = b.BuildPayload(context.Background(), types.WebhookEventEventRejected, data)
	assert.Error(t, err)
}

type attemptTestPaymentService struct {
	interfaces.PaymentService
	payments []*dto.PaymentResponse
}

func (s *attemptTestPaymentService) GetAttempt(_ context.Context, id string) (*dto.PaymentAttemptResponse, error) {
	return &dto.PaymentAttemptResponse{ID: id, AttemptNumber: 2, TenantID: "tenant_01",
		Metadata: types.Metadata{"error_reason": "incorrect_card_details"}}, nil
}

func (s *attemptTestPaymentService) GetStoredPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	if len(s.payments) == 0 {
		return nil, ierr.NewError("payment not found").Mark(ierr.ErrNotFound)
	}
	return s.payments[0], nil
}

type attemptTestCheckoutService struct {
	interfaces.CheckoutSessionService
	sessions []*dto.CheckoutSessionResponse
}

func (s *attemptTestCheckoutService) List(_ context.Context, _ *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	return &dto.ListCheckoutSessionsResponse{Items: s.sessions}, nil
}

func TestPaymentAttemptPayloadBuilder_BuildPayload(t *testing.T) {
	payment := &dto.PaymentResponse{ID: "pay_01", DestinationID: "inv_01", GatewayTrackingID: lo.ToPtr("plink_01")}
	session := &dto.CheckoutSessionResponse{ID: "chk_01", CustomerID: "cust_01", Metadata: types.Metadata{"order": "o_1"},
		Payment: &dto.CheckoutPaymentBlock{ID: "pay_01", Amount: decimal.NewFromInt(500), Currency: "inr"}}

	tests := []struct {
		name     string
		payments []*dto.PaymentResponse
		sessions []*dto.CheckoutSessionResponse
		wantErr  bool
	}{
		{name: "includes the checkout session", payments: []*dto.PaymentResponse{payment}, sessions: []*dto.CheckoutSessionResponse{session}},
		{name: "omits checkout for a standalone payment", payments: []*dto.PaymentResponse{payment}},
		{name: "rejects a missing payment", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPaymentAttemptPayloadBuilder(&Services{
				PaymentService:         &attemptTestPaymentService{payments: tt.payments},
				CheckoutSessionService: &attemptTestCheckoutService{sessions: tt.sessions},
			})
			raw, err := json.Marshal(webhookDto.InternalPaymentEvent{PaymentID: "pay_01", TenantID: "tenant_01", AttemptID: "attempt_02"})
			require.NoError(t, err)

			out, err := builder.BuildPayload(context.Background(), types.WebhookEventPaymentAttemptFailed, raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, json.Unmarshal(out, &got))
			attempt := got["attempt"].(map[string]any)
			assert.Equal(t, "inv_01", got["payment"].(map[string]any)["destination_id"])
			assert.Equal(t, "plink_01", got["payment"].(map[string]any)["gateway_tracking_id"])
			assert.Equal(t, "attempt_02", attempt["id"])
			assert.Equal(t, "incorrect_card_details", attempt["metadata"].(map[string]any)["error_reason"])
			assert.NotContains(t, attempt, "tenant_id")
			if tt.sessions != nil {
				checkout := got["checkout_session"].(map[string]any)
				assert.Equal(t, "cust_01", checkout["customer_id"])
				assert.Equal(t, "500", checkout["amount"])
				assert.Equal(t, "inr", checkout["currency"])
				assert.Equal(t, "o_1", checkout["metadata"].(map[string]any)["order"])
			} else {
				assert.NotContains(t, got, "checkout_session")
			}
		})
	}
}

type paymentFailedTestInvoiceService struct {
	service.InvoiceService
}

func (paymentFailedTestInvoiceService) GetInvoice(_ context.Context, id string) (*dto.InvoiceResponse, error) {
	return &dto.InvoiceResponse{Invoice: invoice.Invoice{ID: id}}, nil
}

func (paymentFailedTestInvoiceService) WithHostedInvoiceURLs(_ context.Context, _ ...*dto.InvoiceResponse) error {
	return nil
}

func (paymentFailedTestInvoiceService) GetInvoicePDFUrl(_ context.Context, _ dto.InvoicePDFRequest) (string, error) {
	return "https://pdf", nil
}

func TestInvoicePaymentFailedPayloadBuilder_BuildPayload(t *testing.T) {
	tests := []struct {
		name    string
		event   webhookDto.InternalInvoicePaymentFailedEvent
		wantErr bool
	}{
		{name: "builds the payload", event: webhookDto.InternalInvoicePaymentFailedEvent{
			InvoiceID: "inv_01", TenantID: "tenant_01", Provider: types.SecretProviderRazorpay,
			Reason: types.InvoicePaymentFailureReasonPaymentMethodUnavailable, IsAutoChargeAttempt: true,
		}},
		{name: "rejects a missing reason", event: webhookDto.InternalInvoicePaymentFailedEvent{
			InvoiceID: "inv_01", TenantID: "tenant_01", Provider: types.SecretProviderRazorpay,
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewInvoicePaymentFailedPayloadBuilder(&Services{InvoiceService: paymentFailedTestInvoiceService{}})
			raw, err := json.Marshal(tt.event)
			require.NoError(t, err)

			out, err := builder.BuildPayload(context.Background(), types.WebhookEventInvoicePaymentFailed, raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var got map[string]any
			require.NoError(t, json.Unmarshal(out, &got))
			assert.Equal(t, string(types.WebhookEventInvoicePaymentFailed), got["event_type"])
			assert.Equal(t, "inv_01", got["invoice"].(map[string]any)["id"])
			assert.Equal(t, "razorpay", got["provider"])
			assert.Equal(t, "payment_method_unavailable", got["reason"])
			assert.Equal(t, true, got["is_auto_charge_attempt"])
			assert.Nil(t, got["payment_id"])
			assert.Nil(t, got["error"])
		})
	}
}
