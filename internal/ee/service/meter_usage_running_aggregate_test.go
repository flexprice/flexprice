package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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
			s.Contains(recorded.meterIDs, m.ID, "the window query supplies event counts for running meters too")

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

// Parent subscriptions save no checkpoint and read none, so both scopes sum their current members.
func (s *MeterUsageServiceSuite) TestRunningSum_ParentSubscriptionSkipsCheckpoints() {
	ctx := s.GetContext()
	fx := s.newParentChildScopeFixture("running")
	m := s.runningSumMeter(ctx, "meter_family", types.ResetUsageNever)
	p := s.createPriceForMeter(ctx, "price_family", m.ID, decimal.NewFromInt(1))
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, &subscription.SubscriptionLineItem{
		ID: "li_family", SubscriptionID: fx.parentSub.ID, CustomerID: fx.parentCust.ID,
		PriceID: p.ID, PriceType: types.PRICE_TYPE_USAGE, MeterID: m.ID, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: s.periodStart, EndDate: s.periodEnd, Quantity: decimal.NewFromInt(1),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	before := s.periodStart.Add(-24 * time.Hour)
	s.insertMeterUsage(ctx, m.ID, fx.parentCust.ExternalID, before, 100)
	s.insertMeterUsage(ctx, m.ID, fx.childCust.ExternalID, before, 200)

	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, fx.parentSub.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.svc.SaveUsageCheckpointsForSubscription(ctx, sub, s.periodStart, nil))
	saved, err := s.GetStores().MeterUsageCheckpointRepo.GetLatestBefore(ctx, meterusagecheckpoint.GetLatestBeforeParams{
		CustomerID: fx.parentCust.ID, MeterID: m.ID, AggregationType: types.AggregationSum, Before: s.periodEnd,
	})
	s.Require().NoError(err)
	s.Nil(saved, "a parent subscription must not save a checkpoint")

	// A checkpoint of the parent's own usage, e.g. from an earlier standalone subscription, must not be read either.
	_, err = s.svc.SaveUsageCheckpoint(ctx, CheckpointedUsageRequest{
		CustomerID: fx.parentCust.ID, ExternalCustomerIDs: []string{fx.parentCust.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: s.periodStart,
	}, nil)
	s.Require().NoError(err)

	for _, tt := range []struct {
		includeChildren bool
		want            int64
	}{{false, 100}, {true, 300}} {
		result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
			SubscriptionID: fx.parentSub.ID, StartTime: s.periodStart, EndTime: s.periodEnd,
			IncludeChildren: tt.includeChildren,
		})
		s.Require().NoError(err)
		got := decimal.Zero
		for _, lu := range result.LineItemUsages {
			if lu.MeterID == m.ID {
				got = lu.Usage
			}
		}
		s.True(decimal.NewFromInt(tt.want).Equal(got), "include_children=%v: want %d, got %s", tt.includeChildren, tt.want, got)
	}
}

// A running total still reports how many events landed in the window.
func (s *MeterUsageServiceSuite) TestRunningSum_KeepsWindowEventCount() {
	ctx := s.GetContext()
	m := s.runningSumMeter(ctx, "meter_counted", types.ResetUsageNever)
	p := s.createPriceForMeter(ctx, "price_counted", m.ID, decimal.NewFromInt(1))
	s.runningSumLineItem(ctx, "li_counted", m.ID, p.ID, s.periodStart, s.periodEnd)
	s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, s.periodStart.Add(-time.Hour), 500)
	s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 100)
	s.insertMeterUsage(ctx, m.ID, s.customer.ExternalID, time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), -30)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID, StartTime: s.periodStart, EndTime: s.periodEnd,
	})
	s.Require().NoError(err)
	var found bool
	for _, lu := range result.LineItemUsages {
		if lu.MeterID == m.ID {
			found = true
			s.True(decimal.NewFromInt(570).Equal(lu.Usage), "usage: got %s", lu.Usage)
			s.EqualValues(2, lu.EventCount)
		}
	}
	s.True(found)
}
