package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// buildSubscriptionPhaseResponse builds a realistic full subscription phase API
// response. No natural scaling collection exists on a single phase, so this is a single
// fixed fixture.
func buildSubscriptionPhaseResponse() *dto.SubscriptionPhaseResponse {
	return &dto.SubscriptionPhaseResponse{
		SubscriptionPhase: &subscription.SubscriptionPhase{
			ID:             "subph_bench_phase",
			SubscriptionID: "sub_bench_subscription",
			StartDate:      benchTime,
			EndDate:        lo.ToPtr(benchTime.AddDate(0, 1, 0)),
			Metadata:       types.Metadata{"source": "benchmark"},
			EnvironmentID:  "env_bench",
		},
	}
}

func BenchmarkSubscriptionPhasePayloadSize(b *testing.B) {
	full := buildSubscriptionPhaseResponse()

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
			payload := NewSubscriptionPhaseWebhookPayload(NewSubscriptionPhase(full), types.WebhookEventSubscriptionPhaseUpdated)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}
