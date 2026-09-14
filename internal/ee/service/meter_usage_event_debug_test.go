package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
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

	resp, err := s.svc.DebugEvent(ctx, eventID)
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
