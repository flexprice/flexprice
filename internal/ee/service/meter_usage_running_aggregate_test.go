package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type recordingWindowUsageStore struct {
	events.MeterUsageRepository
	meterIDs []string
}

func (r *recordingWindowUsageStore) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	r.meterIDs = append(r.meterIDs, params.MeterIDs...)
	return r.MeterUsageRepository.GetUsageMultiMeter(ctx, params)
}

func (s *MeterUsageServiceSuite) runningSumMeter(ctx context.Context, id string, reset types.ResetUsage) *meter.Meter {
	m := &meter.Meter{
		ID:          id,
		Name:        id,
		EventName:   "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "gb"},
		ResetUsage:  reset,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	return m
}

func (s *MeterUsageServiceSuite) runningSumLineItem(ctx context.Context, id, meterID, priceID string, start, end time.Time) {
	li := s.createLineItemForMeter(ctx, id, meterID, priceID)
	li.StartDate, li.EndDate = start, end
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))
}

// A price change splits the period into two line items, and each reads its own window:
// the net inside it, or for a running total, the total held at its end.
func (s *MeterUsageServiceSuite) TestRunningSum_LineItemsReadTheirOwnWindow() {
	tests := []struct {
		name    string
		reset   types.ResetUsage
		wantOld decimal.Decimal
		wantNew decimal.Decimal
	}{
		{"billing_period_reads_each_window_net", types.ResetUsageBillingPeriod, decimal.NewFromInt(100), decimal.NewFromInt(-30)},
		{"never_reads_the_total_at_each_end", types.ResetUsageNever, decimal.NewFromInt(600), decimal.NewFromInt(570)},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			m := s.runningSumMeter(ctx, "meter_storage", tt.reset)
			p := s.createPriceForMeter(ctx, "price_storage", m.ID, decimal.NewFromInt(1))
			change := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
			s.runningSumLineItem(ctx, "li_old", m.ID, p.ID, s.periodStart, change)
			s.runningSumLineItem(ctx, "li_new", m.ID, p.ID, change, s.periodEnd)

			s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC), 500)
			s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 100)
			s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), -30)

			recorded := &recordingWindowUsageStore{MeterUsageRepository: s.GetStores().MeterUsageRepo}
			s.svc.(*meterUsageService).repo = recorded
			result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
				SubscriptionID: s.sub.ID,
				StartTime:      s.periodStart,
				EndTime:        s.periodEnd,
			})
			s.Require().NoError(err)
			if tt.reset == types.ResetUsageNever {
				s.NotContains(recorded.meterIDs, m.ID, "checkpointed meters must not also query window usage")
			} else {
				s.Contains(recorded.meterIDs, m.ID)
			}

			usageByItem := map[string]decimal.Decimal{}
			for _, lu := range result.LineItemUsages {
				if lu.MeterID == m.ID {
					usageByItem[lu.LineItem.ID] = lu.Usage
				}
			}
			s.Require().Len(usageByItem, 2)
			s.True(tt.wantOld.Equal(usageByItem["li_old"]), "old: want %s, got %s", tt.wantOld, usageByItem["li_old"])
			s.True(tt.wantNew.Equal(usageByItem["li_new"]), "new: want %s, got %s", tt.wantNew, usageByItem["li_new"])
		})
	}
}

func (s *MeterUsageServiceSuite) TestSaveUsageCheckpointsForSubscription() {
	ctx := s.GetContext()
	running := s.runningSumMeter(ctx, "meter_running", types.ResetUsageNever)
	periodic := s.runningSumMeter(ctx, "meter_periodic", types.ResetUsageBillingPeriod)
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
			AggregationType: types.AggregationSum,
			Before:          s.periodEnd.Add(time.Second),
		}
	}
	saved, err := store.GetLatestBefore(ctx, afterPeriod(running.ID))
	s.Require().NoError(err)
	s.Require().NotNil(saved)
	s.Equal(types.AggregationSum, saved.AggregationType)
	s.True(decimal.NewFromInt(40).Equal(saved.Total), "got %s", saved.Total)

	none, err := store.GetLatestBefore(ctx, afterPeriod(periodic.ID))
	s.Require().NoError(err)
	s.Nil(none, "a meter that resets each period keeps no running aggregate")
}

func TestKeepsRunningAggregate(t *testing.T) {
	m := func(aggType types.AggregationType, reset types.ResetUsage) *meter.Meter {
		return &meter.Meter{Aggregation: meter.Aggregation{Type: aggType}, ResetUsage: reset}
	}
	require.True(t, keepsRunningAggregate(m(types.AggregationSum, types.ResetUsageNever)))
	require.False(t, keepsRunningAggregate(m(types.AggregationSum, types.ResetUsageBillingPeriod)))
	require.False(t, keepsRunningAggregate(m(types.AggregationCount, types.ResetUsageNever)))
	require.False(t, keepsRunningAggregate(nil))
}

func (s *MeterUsageServiceSuite) TestRunningSumQuietPeriodWithZeroPrice() {
	ctx := s.GetContext()
	m := s.runningSumMeter(ctx, "meter_quiet", types.ResetUsageNever)
	price := s.createPriceForMeter(ctx, "price_quiet", m.ID, decimal.Zero)
	s.runningSumLineItem(ctx, "li_quiet", m.ID, price.ID, s.periodStart, s.periodEnd)
	s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, s.periodStart.Add(-time.Hour), 80)
	_, err := s.svc.SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
		CustomerID: s.customer.ID, ExternalCustomerIDs: []string{s.customer.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: s.periodStart,
	}, nil)
	s.Require().NoError(err)
	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID, StartTime: s.periodStart, EndTime: s.periodEnd,
	})
	s.Require().NoError(err)
	var found bool
	for _, usage := range result.LineItemUsages {
		if usage.MeterID == m.ID {
			found = true
			s.True(usage.Usage.Equal(decimal.NewFromInt(80)), "quiet period must retain the opening balance, got %s", usage.Usage)
		}
	}
	s.True(found, "zero-priced running meter must be returned without current-period events")
}
