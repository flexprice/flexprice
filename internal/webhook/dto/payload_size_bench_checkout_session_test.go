package webhookDto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// buildCheckoutSessionResponse builds a realistic full checkout session API response.
// No natural scaling collection exists on a checkout session, so this is a single fixed
// fixture.
func buildCheckoutSessionResponse() *dto.CheckoutSessionResponse {
	return &dto.CheckoutSessionResponse{
		CheckoutSession: &checkout.CheckoutSession{
			ID:                "checkout_bench_session",
			CustomerID:        "cust_bench_customer",
			Action:            types.CheckoutActionCreateSubscription,
			CheckoutStatus:    types.CheckoutStatusCompleted,
			PaymentProvider:   types.CheckoutPaymentProviderRazorpay,
			CheckoutInvoiceID: lo.ToPtr("inv_bench_invoice"),
			ExpiresAt:         benchTime.Add(24 * time.Hour),
			CompletedAt:       lo.ToPtr(benchTime),
		},
	}
}

func BenchmarkCheckoutSessionPayloadSize(b *testing.B) {
	full := buildCheckoutSessionResponse()

	b.Run("fixed/old", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			data, err := json.Marshal(full)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})

	b.Run("fixed/new", func(b *testing.B) {
		var size int
		for i := 0; i < b.N; i++ {
			payload := NewCheckoutSessionWebhookPayload(full, types.WebhookEventCheckoutSessionCompleted)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}
