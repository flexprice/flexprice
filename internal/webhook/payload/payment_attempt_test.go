package payload

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

func TestPaymentAttemptFailedPayload(t *testing.T) {
	factory := NewPayloadBuilderFactory(nil)
	builder, err := factory.GetBuilder(types.WebhookEventPaymentAttemptFailed)
	if err != nil {
		t.Fatalf("get builder: %v", err)
	}

	t.Run("includes checkout session", func(t *testing.T) {
		raw, err := json.Marshal(webhookDto.InternalPaymentAttemptEvent{
			PaymentID:         "pay_01",
			TenantID:          "tenant_01",
			AttemptNumber:     2,
			GatewayAttemptID:  "pay_rzp_01",
			ErrorMessage:      "Payment processing failed due to error at bank or wallet gateway",
			CheckoutSessionID: "chs_01",
		})
		if err != nil {
			t.Fatalf("marshal internal event: %v", err)
		}

		out, err := builder.BuildPayload(context.Background(), types.WebhookEventPaymentAttemptFailed, raw)
		if err != nil {
			t.Fatalf("build payload: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}

		if got["event_type"] != string(types.WebhookEventPaymentAttemptFailed) {
			t.Fatalf("event_type = %v", got["event_type"])
		}
		if got["payment_id"] != "pay_01" {
			t.Fatalf("payment_id = %v", got["payment_id"])
		}
		if got["attempt_number"] != float64(2) {
			t.Fatalf("attempt_number = %v", got["attempt_number"])
		}
		if got["gateway_attempt_id"] != "pay_rzp_01" {
			t.Fatalf("gateway_attempt_id = %v", got["gateway_attempt_id"])
		}
		if got["error_message"] != "Payment processing failed due to error at bank or wallet gateway" {
			t.Fatalf("error_message = %v", got["error_message"])
		}
		if got["checkout_session_id"] != "chs_01" {
			t.Fatalf("checkout_session_id = %v", got["checkout_session_id"])
		}
	})

	t.Run("omits empty checkout session", func(t *testing.T) {
		raw, err := json.Marshal(webhookDto.InternalPaymentAttemptEvent{
			PaymentID:        "pay_01",
			TenantID:         "tenant_01",
			AttemptNumber:    1,
			GatewayAttemptID: "pay_rzp_02",
			ErrorMessage:     "card declined",
		})
		if err != nil {
			t.Fatalf("marshal internal event: %v", err)
		}

		out, err := builder.BuildPayload(context.Background(), types.WebhookEventPaymentAttemptFailed, raw)
		if err != nil {
			t.Fatalf("build payload: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if _, ok := got["checkout_session_id"]; ok {
			t.Fatalf("checkout_session_id present: %s", out)
		}
	})
}
