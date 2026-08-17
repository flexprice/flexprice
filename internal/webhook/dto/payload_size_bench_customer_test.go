package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func BenchmarkCustomerPayloadSize(b *testing.B) {
	tiers := []struct {
		name string
		n    int
	}{{"small", 200}, {"medium", 800}, {"huge", 2000}}

	for _, tier := range tiers {
		full := buildCustomerResponse(tier.n)

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
				payload := NewCustomerWebhookPayload(full, types.WebhookEventCustomerUpdated)
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
