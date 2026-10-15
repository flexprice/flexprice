package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// A SIGNED_SUM meter's allowance is the entitlement's grant_quota; usage_limit is ignored.
func (s *BillingServiceSuite) TestCalculateMeterUsageCharges_SignedSumAllowanceFromGrantQuota() {
	tests := []struct {
		name  string
		usage float64
		quota *decimal.Decimal
		want  decimal.Decimal
	}{
		{"usage over the ceiling bills the excess", 46, lo.ToPtr(decimal.NewFromInt(10)), decimal.NewFromInt(36)},
		{"usage under the ceiling bills nothing", 4, lo.ToPtr(decimal.NewFromInt(10)), decimal.Zero},
		{"no entitlement bills all usage", 46, nil, decimal.NewFromInt(46)},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()

			m := &meter.Meter{
				ID:          "meter_storage_net",
				Name:        "Storage",
				EventName:   "storage",
				Aggregation: meter.Aggregation{Type: types.AggregationSignedSum, Field: "gb"},
				ResetUsage:  types.ResetUsageNever,
				BaseModel:   types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
			f := &feature.Feature{ID: "feat_storage_net", Name: "Storage", Type: types.FeatureTypeMetered, MeterID: m.ID, BaseModel: types.GetDefaultBaseModel(ctx)}
			s.NoError(s.GetStores().FeatureRepo.Create(ctx, f))
			if tt.quota != nil {
				_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
					ID:                "ent_storage_net",
					EntityType:        types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID:          s.testData.plan.ID,
					FeatureID:         f.ID,
					FeatureType:       types.FeatureTypeMetered,
					IsEnabled:         true,
					UsageLimit:        lo.ToPtr(int64(1000)),
					GrantMeasure:      types.EntitlementGrantMeasureQuantity,
					GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
					GrantQuota:        tt.quota,
					AggregationMode:   types.EntitlementAggregationModeAdditive,
					BaseModel:         types.GetDefaultBaseModel(ctx),
				})
				s.NoError(err)
			}
			p := &price.Price{
				ID:             "price_storage_net",
				Amount:         decimal.NewFromInt(1),
				Currency:       "usd",
				EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
				EntityID:       s.testData.plan.ID,
				BillingModel:   types.BILLING_MODEL_FLAT_FEE,
				Type:           types.PRICE_TYPE_USAGE,
				MeterID:        m.ID,
				BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
				InvoiceCadence: types.InvoiceCadenceArrear,
				BaseModel:      types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

			sub := *s.testData.subscription
			item := &subscription.SubscriptionLineItem{
				ID:             "sub_li_storage_net",
				SubscriptionID: sub.ID,
				CustomerID:     sub.CustomerID,
				EntityID:       s.testData.plan.ID,
				EntityType:     types.SubscriptionLineItemEntityTypePlan,
				PriceID:        p.ID,
				PriceType:      types.PRICE_TYPE_USAGE,
				MeterID:        m.ID,
				Currency:       sub.Currency,
				BillingPeriod:  sub.BillingPeriod,
				InvoiceCadence: types.InvoiceCadenceArrear,
				StartDate:      sub.CurrentPeriodStart,
				BaseModel:      types.GetDefaultBaseModel(ctx),
			}
			sub.LineItems = []*subscription.SubscriptionLineItem{item}

			usage := &dto.GetUsageBySubscriptionResponse{
				StartTime: sub.CurrentPeriodStart,
				EndTime:   sub.CurrentPeriodEnd,
				Currency:  sub.Currency,
				Charges: []*dto.SubscriptionUsageByMetersResponse{
					{SubscriptionLineItemID: item.ID, MeterID: m.ID, Price: p, Quantity: tt.usage, Amount: tt.usage},
				},
			}
			asOf := sub.CurrentPeriodEnd
			_, total, err := s.service.CalculateMeterUsageCharges(ctx, &sub, usage,
				sub.CurrentPeriodStart, sub.CurrentPeriodEnd, types.UsageSourceInvoiceCreation, &asOf)
			s.Require().NoError(err)
			s.True(tt.want.Equal(total), "want %s, got %s", tt.want, total)
		})
	}
}
