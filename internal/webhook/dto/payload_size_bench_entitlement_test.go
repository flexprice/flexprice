package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// buildEntitlementResponse builds a realistic full entitlement API response, including
// the nested feature object the minimal webhook payload drops. No natural scaling
// collection exists on a single entitlement, so this is a single fixed fixture.
func buildEntitlementResponse() *dto.EntitlementResponse {
	return &dto.EntitlementResponse{
		Entitlement: &entitlement.Entitlement{
			ID:               "ent_bench_entitlement",
			EntityType:       types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:         "sub_bench_subscription",
			FeatureID:        "feat_bench_feature",
			FeatureType:      types.FeatureTypeMetered,
			IsEnabled:        true,
			UsageLimit:       lo.ToPtr(int64(10000)),
			UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			IsSoftLimit:      false,
		},
		Feature: buildFeatureResponse(),
	}
}

func BenchmarkEntitlementPayloadSize(b *testing.B) {
	full := buildEntitlementResponse()

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
			payload := NewEntitlementWebhookPayload(full, types.WebhookEventEntitlementUpdated)
			data, err := json.Marshal(payload)
			if err != nil {
				b.Fatal(err)
			}
			size = len(data)
		}
		b.ReportMetric(float64(size), "wire_bytes")
	})
}
