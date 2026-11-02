package payload

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/stretchr/testify/require"
)

type attemptTestPaymentService struct {
	interfaces.PaymentService
	payment *dto.PaymentResponse
}

func (s *attemptTestPaymentService) GetPayment(_ context.Context, _ string) (*dto.PaymentResponse, error) {
	return s.payment, nil
}

type attemptTestCheckoutService struct {
	interfaces.CheckoutSessionService
	sessions []*dto.CheckoutSessionResponse
}

func (s *attemptTestCheckoutService) List(_ context.Context, _ *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	return &dto.ListCheckoutSessionsResponse{Items: s.sessions}, nil
}

func TestPaymentAttemptFailedPayload(t *testing.T) {
	payment := &dto.PaymentResponse{
		ID:            "pay_01",
		DestinationID: "inv_01",
		Attempts: []*dto.PaymentAttemptResponse{
			{ID: "attempt_01", AttemptNumber: 1},
			{ID: "attempt_02", AttemptNumber: 2, Metadata: types.Metadata{"error_reason": "incorrect_card_details"}},
		},
	}
	session := &dto.CheckoutSessionResponse{ID: "chk_01", CustomerID: "cust_01"}

	tests := []struct {
		name        string
		attemptID   string
		sessions    []*dto.CheckoutSessionResponse
		wantSession bool
		wantErr     bool
	}{
		{name: "includes the checkout session", attemptID: "attempt_02", sessions: []*dto.CheckoutSessionResponse{session}, wantSession: true},
		{name: "omits checkout for a standalone payment", attemptID: "attempt_02"},
		{name: "rejects an unknown attempt", attemptID: "attempt_99", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewPaymentAttemptPayloadBuilder(&Services{
				PaymentService:         &attemptTestPaymentService{payment: payment},
				CheckoutSessionService: &attemptTestCheckoutService{sessions: tt.sessions},
			})
			raw, err := json.Marshal(webhookDto.InternalPaymentEvent{PaymentID: "pay_01", TenantID: "tenant_01", AttemptID: tt.attemptID})
			require.NoError(t, err)

			out, err := builder.BuildPayload(context.Background(), types.WebhookEventPaymentAttemptFailed, raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var got webhookDto.PaymentAttemptWebhookPayload
			require.NoError(t, json.Unmarshal(out, &got))
			require.Equal(t, types.WebhookEventPaymentAttemptFailed, got.EventType)
			require.Equal(t, "inv_01", got.Payment.DestinationID)
			require.Equal(t, 2, got.Attempt.AttemptNumber)
			require.Equal(t, "incorrect_card_details", got.Attempt.Metadata["error_reason"])
			if tt.wantSession {
				require.Equal(t, "cust_01", got.CheckoutSession.CustomerID)
			} else {
				require.Nil(t, got.CheckoutSession)
			}
		})
	}
}
