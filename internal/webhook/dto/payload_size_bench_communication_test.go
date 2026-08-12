package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func BenchmarkCommunicationPayloadSize(b *testing.B) {
	full := buildInvoiceResponse(50)

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
			payload := NewCommunicationWebhookPayload(full, types.WebhookEventInvoiceCommunicationTriggered)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}
