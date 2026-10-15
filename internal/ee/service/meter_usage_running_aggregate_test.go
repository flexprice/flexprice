package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *MeterUsageServiceSuite) signedSumMeter(ctx context.Context, id string, reset types.ResetUsage) *meter.Meter {
	m := &meter.Meter{
		ID:          id,
		Name:        id,
		EventName:   "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSignedSum, Field: "gb"},
		ResetUsage:  reset,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	return m
}

func (s *MeterUsageServiceSuite) signedSumLineItem(ctx context.Context, id, meterID, priceID string, start, end time.Time) {
	li := s.createLineItemForMeter(ctx, id, meterID, priceID)
	li.StartDate, li.EndDate = start, end
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))
}

// A price change splits the period into two line items. The line item active at
// period end bills the whole period once; the one it replaced bills nothing.
func (s *MeterUsageServiceSuite) TestSignedSum_PeriodEndLineItemBillsThePeriod() {
	tests := []struct {
		name    string
		reset   types.ResetUsage
		wantNew decimal.Decimal
	}{
		{"billing_period_bills_the_period_net", types.ResetUsageBillingPeriod, decimal.NewFromInt(70)},
		{"never_bills_the_total_held", types.ResetUsageNever, decimal.NewFromInt(570)},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			m := s.signedSumMeter(ctx, "meter_storage", tt.reset)
			p := s.createPriceForMeter(ctx, "price_storage", m.ID, decimal.NewFromInt(1))
			change := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
			s.signedSumLineItem(ctx, "li_old", m.ID, p.ID, s.periodStart, change)
			s.signedSumLineItem(ctx, "li_new", m.ID, p.ID, change, s.periodEnd)

			s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC), 500)
			s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 100)
			s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), -30)

			result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
				SubscriptionID: s.sub.ID,
				StartTime:      s.periodStart,
				EndTime:        s.periodEnd,
			})
			s.Require().NoError(err)

			usageByItem := map[string]decimal.Decimal{}
			for _, lu := range result.LineItemUsages {
				if lu.MeterID == m.ID {
					usageByItem[lu.LineItem.ID] = lu.Usage
				}
			}
			s.Require().Len(usageByItem, 2)
			s.True(usageByItem["li_old"].IsZero(), "replaced line item bills nothing, got %s", usageByItem["li_old"])
			s.True(tt.wantNew.Equal(usageByItem["li_new"]), "want %s, got %s", tt.wantNew, usageByItem["li_new"])
		})
	}
}

func (s *MeterUsageServiceSuite) TestSaveUsageCheckpointsForSubscription() {
	ctx := s.GetContext()
	running := s.signedSumMeter(ctx, "meter_running", types.ResetUsageNever)
	periodic := s.signedSumMeter(ctx, "meter_periodic", types.ResetUsageBillingPeriod)
	for _, m := range []*meter.Meter{running, periodic} {
		p := s.createPriceForMeter(ctx, "price_"+m.ID, m.ID, decimal.NewFromInt(1))
		s.createLineItemForMeter(ctx, "li_"+m.ID, m.ID, p.ID)
	}
	s.insertMeterUsage(ctx, running.ID, s.customer.ExternalID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 40)
	s.insertMeterUsage(ctx, periodic.ID, s.customer.ExternalID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 7)

	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, s.sub.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.svc.SaveUsageCheckpointsForSubscription(ctx, sub, s.periodEnd, nil))

	store := s.GetStores().MeterUsageCheckpointRepo
	afterPeriod := func(meterID string) meterusagecheckpoint.GetLatestBeforeParams {
		return meterusagecheckpoint.GetLatestBeforeParams{
			CustomerID:      s.customer.ID,
			MeterID:         meterID,
			AggregationType: types.AggregationSignedSum,
			Before:          s.periodEnd.Add(time.Second),
		}
	}
	saved, err := store.GetLatestBefore(ctx, afterPeriod(running.ID))
	s.Require().NoError(err)
	s.Require().NotNil(saved)
	s.Equal(types.AggregationSignedSum, saved.AggregationType)
	s.True(decimal.NewFromInt(40).Equal(saved.Total), "got %s", saved.Total)

	none, err := store.GetLatestBefore(ctx, afterPeriod(periodic.ID))
	s.Require().NoError(err)
	s.Nil(none, "a meter that resets each period keeps no running aggregate")
}

func TestKeepsRunningAggregate(t *testing.T) {
	m := func(aggType types.AggregationType, reset types.ResetUsage) *meter.Meter {
		return &meter.Meter{Aggregation: meter.Aggregation{Type: aggType}, ResetUsage: reset}
	}
	require.True(t, keepsRunningAggregate(m(types.AggregationSignedSum, types.ResetUsageNever)))
	require.False(t, keepsRunningAggregate(m(types.AggregationSignedSum, types.ResetUsageBillingPeriod)))
	require.False(t, keepsRunningAggregate(m(types.AggregationSum, types.ResetUsageNever)), "not opted in yet")
	require.False(t, keepsRunningAggregate(nil))
}

func TestSignedSumBillable(t *testing.T) {
	quota := decimal.NewFromInt(10)
	withQuota := &dto.AggregatedEntitlement{IsEnabled: true, GrantConfig: dto.GrantConfig{GrantQuota: &quota}}
	tests := []struct {
		name  string
		usage decimal.Decimal
		ent   *dto.AggregatedEntitlement
		want  decimal.Decimal
	}{
		{"no entitlement bills all", decimal.NewFromInt(46), nil, decimal.NewFromInt(46)},
		{"ceiling is taken off", decimal.NewFromInt(46), withQuota, decimal.NewFromInt(36)},
		{"under the ceiling bills 0", decimal.NewFromInt(4), withQuota, decimal.Zero},
		{"unlimited bills 0", decimal.NewFromInt(46), &dto.AggregatedEntitlement{IsEnabled: true, GrantConfig: dto.GrantConfig{GrantUnlimited: true}}, decimal.Zero},
		{"usage_limit alone is ignored", decimal.NewFromInt(46), &dto.AggregatedEntitlement{IsEnabled: true, UsageLimit: lo.ToPtr(int64(10))}, decimal.NewFromInt(46)},
		{"negative usage bills 0", decimal.NewFromInt(-5), nil, decimal.Zero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signedSumBillable(tt.usage, tt.ent)
			if !tt.want.Equal(got) {
				t.Fatalf("want %s, got %s", tt.want, got)
			}
		})
	}
}
