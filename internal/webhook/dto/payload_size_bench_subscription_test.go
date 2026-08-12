package webhookDto

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// buildSubscriptionResponse builds a realistic full subscription API response, scaling
// phases, credit grants, and entitlements -- the three collections on SubscriptionResponse
// that grow with data volume. Each entitlement entry nests a full feature object, since
// that's what the raw response actually carries.
func buildSubscriptionResponse(nPhases, nCreditGrants, nEntitlements int) *dto.SubscriptionResponse {
	phases := make([]*dto.SubscriptionPhaseResponse, nPhases)
	for i := 0; i < nPhases; i++ {
		phases[i] = &dto.SubscriptionPhaseResponse{
			SubscriptionPhase: &subscription.SubscriptionPhase{
				ID:             fmt.Sprintf("subph_%d", i),
				SubscriptionID: "sub_bench_subscription",
				StartDate:      benchTime,
				EndDate:        lo.ToPtr(benchTime.AddDate(0, 1, 0)),
			},
		}
	}

	creditGrants := make([]*dto.CreditGrantResponse, nCreditGrants)
	for i := 0; i < nCreditGrants; i++ {
		creditGrants[i] = &dto.CreditGrantResponse{
			CreditGrant: &creditgrant.CreditGrant{
				ID:      fmt.Sprintf("cg_%d", i),
				Name:    fmt.Sprintf("Credit Grant %d", i),
				Scope:   types.CreditGrantScopeSubscription,
				Credits: decimal.NewFromInt(100),
				Cadence: types.CreditGrantCadenceRecurring,
			},
		}
	}

	entitlements := make([]*dto.AggregatedFeature, nEntitlements)
	for i := 0; i < nEntitlements; i++ {
		entitlements[i] = &dto.AggregatedFeature{
			Feature: buildFeatureResponse(),
			Entitlement: &dto.AggregatedEntitlement{
				IsEnabled:        true,
				UsageLimit:       lo.ToPtr(int64(10000)),
				IsSoftLimit:      false,
				UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
				AggregationMode:  types.EntitlementAggregationModeAdditive,
			},
		}
	}

	return &dto.SubscriptionResponse{
		Subscription: &subscription.Subscription{
			ID:                 "sub_bench_subscription",
			CustomerID:         "cust_bench_customer",
			PlanID:             "plan_bench_plan",
			LookupKey:          "bench-plan-monthly",
			SubscriptionStatus: types.SubscriptionStatusActive,
			Currency:           "usd",
			StartDate:          benchTime,
			CurrentPeriodStart: benchTime,
			CurrentPeriodEnd:   benchTime.AddDate(0, 1, 0),
			CancelAtPeriodEnd:  false,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			PauseStatus:        types.PauseStatusNone,
			SubscriptionType:   types.SubscriptionTypeStandalone,
			Metadata:           types.Metadata{"source": "benchmark"},
		},
		Phases:       phases,
		CreditGrants: creditGrants,
		Entitlements: entitlements,
	}
}

func BenchmarkSubscriptionPayloadSize(b *testing.B) {
	tiers := []struct {
		name string
		n    int
	}{{"small", 5}, {"medium", 50}, {"huge", 500}}

	for _, tier := range tiers {
		full := buildSubscriptionResponse(tier.n, tier.n, tier.n)

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
				payload := NewSubscriptionWebhookPayload(full, types.WebhookEventSubscriptionUpdated)
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
