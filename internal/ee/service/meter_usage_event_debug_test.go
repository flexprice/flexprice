package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func (s *MeterUsageServiceSuite) TestDebugEvent_ListsAllRawEventsNewestFirst() {
	ctx := s.GetContext()
	eventID := "2872_pageviews_2026-08-31"
	ts := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	ingestedEarly := time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC)
	ingestedLate := time.Date(2026, 8, 31, 5, 56, 40, 0, time.UTC)

	s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &events.Event{
		ID:                 eventID,
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		EventName:          "pageviews",
		Timestamp:          ts,
		IngestedAt:         ingestedEarly,
		Properties:         map[string]interface{}{"value": float64(0)},
		Source:             "public-api",
	}))
	s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &events.Event{
		ID:                 eventID,
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		EventName:          "pageviews",
		Timestamp:          ts,
		IngestedAt:         ingestedLate,
		Properties:         map[string]interface{}{"value": float64(4975355)},
		Source:             "public-api",
	}))
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 eventID,
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: s.customer.ExternalID,
				EventName:          "pageviews",
				Timestamp:          ts,
				IngestedAt:         ingestedEarly,
				Properties:         map[string]interface{}{"value": float64(0)},
			},
			MeterID:  s.meterAPI.ID,
			QtyTotal: decimal.Zero,
		},
	}))

	resp, err := s.svc.DebugEvent(ctx, s.customer.ExternalID, eventID, windowAround(ts))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Event)
	s.Equal(eventID, resp.Event.ID)
	s.Equal(ingestedLate, resp.Event.IngestedAt)
	s.Equal(float64(4975355), resp.Event.Properties["value"])

	s.Require().Len(resp.Events, 2)
	s.Equal(ingestedLate, resp.Events[0].IngestedAt)
	s.Equal(float64(4975355), resp.Events[0].Properties["value"])
	s.Equal(ingestedEarly, resp.Events[1].IngestedAt)
	s.Equal(float64(0), resp.Events[1].Properties["value"])
}

func (s *MeterUsageServiceSuite) TestDebugEvent_OtherCustomerIsNotFound() {
	ctx := s.GetContext()
	eventID := "evt_scoped_to_customer"
	ts := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

	s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &events.Event{
		ID:                 eventID,
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		EventName:          "pageviews",
		Timestamp:          ts,
		IngestedAt:         ts.Add(time.Hour),
		Properties:         map[string]interface{}{"value": float64(1)},
		Source:             "public-api",
	}))

	_, err := s.svc.DebugEvent(ctx, "some-other-customer", eventID, windowAround(ts))
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *MeterUsageServiceSuite) TestDebugEvent_OutsideWindowIsNotFound() {
	ctx := s.GetContext()
	eventID := "evt_outside_window"
	ts := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

	s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &events.Event{
		ID:                 eventID,
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		EventName:          "pageviews",
		Timestamp:          ts,
		IngestedAt:         ts.Add(time.Hour),
		Properties:         map[string]interface{}{"value": float64(1)},
		Source:             "public-api",
	}))

	_, err := s.svc.DebugEvent(ctx, s.customer.ExternalID, eventID, windowAround(ts.AddDate(0, 0, 30)))
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err))

	resp, err := s.svc.DebugEvent(ctx, s.customer.ExternalID, eventID, windowAround(ts))
	s.Require().NoError(err)
	s.Equal(eventID, resp.Event.ID)
}

func windowAround(ts time.Time) events.TimeRange {
	return events.TimeRange{Start: ts.Add(-time.Hour), End: ts.Add(time.Hour)}
}

// meterSwapFixture: an event that matches two meters after a meter swap. The old meter's line item
// ended before the event; only the new meter's line item is active.
type meterSwapFixture struct {
	event    events.Event
	oldMeter string
	newMeter string
	newLI    string
}

func (s *MeterUsageServiceSuite) setupMeterSwap(eventID string) meterSwapFixture {
	ctx := s.GetContext()
	swapAt := s.periodStart.Add(10 * 24 * time.Hour)
	ts := swapAt.Add(6 * time.Hour)

	oldMeter := s.createMeterWithAggregation(ctx, "meter_stt_old", "stt", types.AggregationSum)
	newMeter := s.createMeterWithAggregation(ctx, "meter_stt_new", "stt", types.AggregationSum)
	oldLI := s.createLineItemForMeter(ctx, "li_stt_old", oldMeter.ID, s.createPriceForMeter(ctx, "pr_stt_old", oldMeter.ID, decimal.NewFromInt(1)).ID)
	oldLI.EndDate = swapAt
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, oldLI))
	newLI := s.createLineItemForMeter(ctx, "li_stt_new", newMeter.ID, s.createPriceForMeter(ctx, "pr_stt_new", newMeter.ID, decimal.NewFromInt(1)).ID)
	newLI.StartDate = swapAt
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, newLI))

	event := events.Event{
		ID:                 eventID,
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		EventName:          "stt",
		Timestamp:          ts,
		IngestedAt:         ts,
		Properties:         map[string]interface{}{"quantity": 1.1},
		Source:             "public-api",
	}
	return meterSwapFixture{event: event, oldMeter: oldMeter.ID, newMeter: newMeter.ID, newLI: newLI.ID}
}

// An event matching two meters has a meter_usage row per meter. After a meter swap only the new
// meter's line item is active, so the debugger must report that row, not the ended meter's.
func (s *MeterUsageServiceSuite) TestDebugEvent_ReportsActiveMeterAfterMeterSwap() {
	ctx := s.GetContext()
	fx := s.setupMeterSwap("evt_meter_swap")
	s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &fx.event))
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{Event: fx.event, MeterID: fx.oldMeter, QtyTotal: decimal.RequireFromString("1.1")},
		{Event: fx.event, MeterID: fx.newMeter, QtyTotal: decimal.NewFromInt(2)},
	}))

	resp, err := s.svc.DebugEvent(ctx, s.customer.ExternalID, fx.event.ID, windowAround(fx.event.Timestamp))
	s.Require().NoError(err)
	s.Equal(types.EventProcessingStatusTypeProcessed, resp.Status)
	s.Require().Len(resp.ProcessedEvents, 1)
	s.Equal(fx.newMeter, resp.ProcessedEvents[0].MeterID)
	s.Equal(fx.newLI, resp.ProcessedEvents[0].SubLineItemID)
	s.Equal("2", resp.ProcessedEvents[0].QtyTotal)
}

// The tracker attributes the event to the meter an active line item bills, even when the ended
// meter's row comes first.
func (s *MeterUsageServiceSuite) TestDebugTracker_AttributesActiveMeterAfterMeterSwap() {
	ctx := s.GetContext()
	fx := s.setupMeterSwap("evt_meter_swap_tracker")
	rows := []*events.MeterUsage{
		{Event: fx.event, MeterID: fx.oldMeter, QtyTotal: decimal.RequireFromString("1.1")},
		{Event: fx.event, MeterID: fx.newMeter, QtyTotal: decimal.NewFromInt(2)},
	}

	tracker := s.svc.(*meterUsageService).runDebugTracker(ctx, &fx.event, rows, windowAround(fx.event.Timestamp))
	s.Require().Nil(tracker.FailurePoint)
	s.Require().NotNil(tracker.AttributedToCustomer)
	s.Require().NotNil(tracker.AttributedToCustomer.MeterUsage)
	s.Equal(fx.newMeter, tracker.AttributedToCustomer.MeterUsage.MeterID)
	s.Equal("2", tracker.AttributedToCustomer.MeterUsage.QtyTotal)
}

// The lookup returns the latest row per meter, so a re-ingested event shows its newest quantity.
func (s *MeterUsageServiceSuite) TestDebugEvent_ShowsLatestVersionPerMeter() {
	ctx := s.GetContext()
	fx := s.setupMeterSwap("evt_meter_swap_versions")
	s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, &fx.event))
	older := fx.event
	older.IngestedAt = fx.event.IngestedAt.Add(-time.Hour)
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{Event: older, MeterID: fx.newMeter, QtyTotal: decimal.NewFromInt(7)},
		{Event: fx.event, MeterID: fx.newMeter, QtyTotal: decimal.NewFromInt(2)},
	}))

	resp, err := s.svc.DebugEvent(ctx, s.customer.ExternalID, fx.event.ID, windowAround(fx.event.Timestamp))
	s.Require().NoError(err)
	s.Require().Len(resp.ProcessedEvents, 1)
	s.Equal("2", resp.ProcessedEvents[0].QtyTotal)
}
