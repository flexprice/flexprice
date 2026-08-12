package webhookDto

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// buildPaymentResponse builds a realistic full payment API response, scaling payment
// attempts -- the one collection on PaymentResponse that grows with retries.
func buildPaymentResponse(nAttempts int) *dto.PaymentResponse {
	attempts := make([]*dto.PaymentAttemptResponse, nAttempts)
	for i := 0; i < nAttempts; i++ {
		attempts[i] = &dto.PaymentAttemptResponse{
			ID:            fmt.Sprintf("pay_attempt_%d", i),
			PaymentID:     "pay_bench_payment",
			AttemptNumber: i + 1,
			Metadata:      types.Metadata{"source": "benchmark"},
			TenantID:      "tenant_bench",
			CreatedAt:     benchTime,
			UpdatedAt:     benchTime,
			CreatedBy:     "bench",
			UpdatedBy:     "bench",
		}
	}

	return &dto.PaymentResponse{
		ID:                     "pay_bench_payment",
		IdempotencyKey:         "idem_bench_payment",
		DestinationType:        types.PaymentDestinationTypeInvoice,
		DestinationID:          "inv_bench_invoice",
		PaymentMethodType:      types.PaymentMethodTypeCard,
		PaymentMethodID:        "pm_bench_card",
		Amount:                 decimal.NewFromInt(5000),
		Currency:               "usd",
		PaymentStatus:          types.PaymentStatusSucceeded,
		TrackAttempts:          true,
		GatewayMetadata:        types.Metadata{"gateway_ref": "bench_ref_123"},
		Metadata:               types.Metadata{"source": "benchmark"},
		SucceededAt:            lo.ToPtr(benchTime),
		Attempts:               attempts,
		InvoiceNumber:          lo.ToPtr("INV-BENCH-0001"),
		TenantID:               "tenant_bench",
		SaveCardAndMakeDefault: false,
		CreatedAt:              benchTime,
		UpdatedAt:              benchTime,
		CreatedBy:              "bench",
		UpdatedBy:              "bench",
		EnvironmentID:          "env_bench",
	}
}

func BenchmarkPaymentPayloadSize(b *testing.B) {
	tiers := []struct {
		name string
		n    int
	}{{"small", 5}, {"medium", 50}, {"huge", 500}}

	for _, tier := range tiers {
		full := buildPaymentResponse(tier.n)

		b.Run(tier.name+"/old", func(b *testing.B) {
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

		b.Run(tier.name+"/new", func(b *testing.B) {
			var size int
			for i := 0; i < b.N; i++ {
				payload := NewPaymentWebhookPayload(full, types.WebhookEventPaymentSuccess)
				data, err := json.Marshal(payload)
				if err != nil {
					b.Fatal(err)
				}
				size = len(data)
			}
			b.ReportMetric(float64(size), "wire_bytes")
		})
	}
}
