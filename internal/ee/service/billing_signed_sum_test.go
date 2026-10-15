package service

import (
	"time"

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

// A price change halfway through the period: each line item bills its own half, at its
// own price, with its share of the 10-unit allowance.
func (s *BillingServiceSuite) TestCalculateMeterUsageCharges_SignedSumPriceChangeBillsEachLineItem() {
	tests := []struct {
		name               string
		reset              types.ResetUsage
		oldUsage, newUsage float64
		want               decimal.Decimal
	}{
		// (50 − 10) × ½ × $1 + (46 − 10) × ½ × $0.9
		{"running total", types.ResetUsageNever, 50, 46, decimal.NewFromFloat(36.2)},
		// (100 − 5) × $1 + max(0, −30 − 5) × $0.9
		{"period net", types.ResetUsageBillingPeriod, 100, -30, decimal.NewFromInt(95)},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()

			m := &meter.Meter{
				ID:          "meter_storage_split",
				Name:        "Storage",
				EventName:   "storage",
				Aggregation: meter.Aggregation{Type: types.AggregationSignedSum, Field: "gb"},
				ResetUsage:  tt.reset,
				BaseModel:   types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
			f := &feature.Feature{ID: "feat_storage_split", Name: "Storage", Type: types.FeatureTypeMetered, MeterID: m.ID, BaseModel: types.GetDefaultBaseModel(ctx)}
			s.NoError(s.GetStores().FeatureRepo.Create(ctx, f))
			_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
				ID:                "ent_storage_split",
				EntityType:        types.ENTITLEMENT_ENTITY_TYPE_PLAN,
				EntityID:          s.testData.plan.ID,
				FeatureID:         f.ID,
				FeatureType:       types.FeatureTypeMetered,
				IsEnabled:         true,
				GrantMeasure:      types.EntitlementGrantMeasureQuantity,
				GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
				GrantQuota:        lo.ToPtr(decimal.NewFromInt(10)),
				AggregationMode:   types.EntitlementAggregationModeAdditive,
				BaseModel:         types.GetDefaultBaseModel(ctx),
			})
			s.NoError(err)

			newPrice := func(id string, amount decimal.Decimal) *price.Price {
				p := &price.Price{
					ID: id, Amount: amount, Currency: "usd",
					EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: s.testData.plan.ID,
					BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE, MeterID: m.ID,
					BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
					BaseModel: types.GetDefaultBaseModel(ctx),
				}
				s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
				return p
			}
			oldPrice := newPrice("price_storage_old", decimal.NewFromInt(1))
			newerPrice := newPrice("price_storage_new", decimal.NewFromFloat(0.9))

			sub := *s.testData.subscription
			change := sub.CurrentPeriodStart.Add(sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart) / 2)
			lineItem := func(id string, p *price.Price, start, end time.Time) *subscription.SubscriptionLineItem {
				return &subscription.SubscriptionLineItem{
					ID: id, SubscriptionID: sub.ID, CustomerID: sub.CustomerID,
					EntityID: s.testData.plan.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
					PriceID: p.ID, PriceType: types.PRICE_TYPE_USAGE, MeterID: m.ID,
					Currency: sub.Currency, BillingPeriod: sub.BillingPeriod, InvoiceCadence: types.InvoiceCadenceArrear,
					StartDate: start, EndDate: end, BaseModel: types.GetDefaultBaseModel(ctx),
				}
			}
			oldItem := lineItem("sub_li_storage_old", oldPrice, sub.CurrentPeriodStart, change)
			newItem := lineItem("sub_li_storage_new", newerPrice, change, time.Time{})
			sub.LineItems = []*subscription.SubscriptionLineItem{oldItem, newItem}

			usage := &dto.GetUsageBySubscriptionResponse{
				StartTime: sub.CurrentPeriodStart,
				EndTime:   sub.CurrentPeriodEnd,
				Currency:  sub.Currency,
				Charges: []*dto.SubscriptionUsageByMetersResponse{
					{SubscriptionLineItemID: oldItem.ID, MeterID: m.ID, Price: oldPrice, Quantity: tt.oldUsage, Amount: tt.oldUsage},
					{SubscriptionLineItemID: newItem.ID, MeterID: m.ID, Price: newerPrice, Quantity: tt.newUsage, Amount: tt.newUsage * 0.9},
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
