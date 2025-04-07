package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type EventServiceSuite struct {
	suite.Suite
	ctx       context.Context
	service   EventService
	eventRepo *testutil.InMemoryEventStore
	publisher *testutil.InMemoryPublisherService
	logger    *logger.Logger
}

func TestEventService(t *testing.T) {
	suite.Run(t, new(EventServiceSuite))
}

func (s *EventServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.eventRepo = testutil.NewInMemoryEventStore()
	s.publisher = testutil.NewInMemoryEventPublisher(s.eventRepo).(*testutil.InMemoryPublisherService)
	s.logger = logger.GetLogger()

	s.service = NewEventService(
		s.eventRepo,
		nil, // meter repo not needed for these tests
		s.publisher,
		s.logger,
	)
}

func (s *EventServiceSuite) TearDownTest() {
	s.publisher.Clear()
}

func (s *EventServiceSuite) TestCreateEvent() {
	testCases := []struct {
		name          string
		input         *dto.IngestEventRequest
		expectedError bool
		verify        func(input *dto.IngestEventRequest)
	}{
		{
			name: "successful_event_creation",
			input: &dto.IngestEventRequest{
				EventID:            "test-1",
				ExternalCustomerID: "customer-1",
				EventName:          "api_request",
				Timestamp:          time.Now(),
				Properties: map[string]interface{}{
					"duration_ms": 150,
				},
			},
			expectedError: false,
			verify: func(input *dto.IngestEventRequest) {
				// First verify event was published (this should happen immediately)
				s.True(s.publisher.HasEvent("test-1"), "Event should be published")

				// Give some time for the consumer to process the event
				time.Sleep(100 * time.Millisecond)

				// Then verify it was stored by the consumer
				s.True(s.eventRepo.HasEvent("test-1"), "Event should be stored by consumer")

				// Verify event details through GetEvents
				params := &events.GetEventsParams{
					ExternalCustomerID: input.ExternalCustomerID,
					EventName:          input.EventName,
					StartTime:          input.Timestamp.Add(-time.Hour), // Buffer to ensure we catch the event
					EndTime:            input.Timestamp.Add(time.Hour),
					PageSize:           10,
				}
				storedEvents, _, err := s.eventRepo.GetEvents(s.ctx, params)
				s.NoError(err)
				s.Len(storedEvents, 1)

				event := storedEvents[0]
				s.Equal(input.EventName, event.EventName)
				s.Equal(input.ExternalCustomerID, event.ExternalCustomerID)
			},
		},
		{
			name: "missing_required_fields",
			input: &dto.IngestEventRequest{
				EventID: "test-2",
			},
			expectedError: true,
			verify: func(input *dto.IngestEventRequest) {
				s.False(s.publisher.HasEvent("test-2"), "Event should not be published")
				time.Sleep(100 * time.Millisecond) // Wait for potential consumer processing
				s.False(s.eventRepo.HasEvent("test-2"), "Event should not be stored")
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.service.CreateEvent(s.ctx, tc.input)

			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)
			}

			tc.verify(tc.input)
		})
	}
}

func (s *EventServiceSuite) TestGetUsage() {
	// Setup test data with properties for filtering
	testingEvents := []*dto.IngestEventRequest{
		{
			EventID:            "evt-1",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          time.Now().Add(-1 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(100),
				"status":      "success",
				"region":      "us-east-1",
			},
		},
		{
			EventID:            "evt-2",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          time.Now().Add(-30 * time.Minute),
			Properties: map[string]interface{}{
				"duration_ms": float64(150),
				"status":      "error",
				"region":      "us-east-1",
			},
		},
		{
			EventID:            "evt-3",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          time.Now().Add(-15 * time.Minute),
			Properties: map[string]interface{}{
				"duration_ms": float64(200),
				"status":      "success",
				"region":      "us-west-2",
			},
		},
	}

	// Insert test events directly into store
	for _, evt := range testingEvents {
		event := events.NewEvent(
			evt.EventName,
			types.GetTenantID(s.ctx),
			evt.ExternalCustomerID,
			evt.Properties,
			evt.Timestamp,
			evt.EventID,
			evt.CustomerID,
			evt.Source,
		)
		err := s.eventRepo.InsertEvent(s.ctx, event)
		s.NoError(err)
	}

	testCases := []struct {
		name          string
		request       *dto.GetUsageRequest
		expectedValue decimal.Decimal
		expectedError bool
	}{
		{
			name: "count_all_events",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				AggregationType:    types.AggregationCount,
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
			},
			expectedValue: decimal.NewFromFloat(3),
			expectedError: false,
		},
		{
			name: "sum_duration_with_status_filter",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				PropertyName:       "duration_ms",
				AggregationType:    types.AggregationSum,
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
				Filters: map[string][]string{
					"status": {"success"},
				},
			},
			expectedValue: decimal.NewFromFloat(300.0), // 100 + 200 (only success events)
			expectedError: false,
		},
		{
			name: "sum_duration_with_multiple_status_values",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				PropertyName:       "duration_ms",
				AggregationType:    types.AggregationSum,
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
				Filters: map[string][]string{
					"status": {"success", "error"},
					"region": {"us-east-1"},
				},
			},
			expectedValue: decimal.NewFromFloat(250), // 100 + 150 (us-east-1 events only)
			expectedError: false,
		},
		{
			name: "count_with_region_filter",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				AggregationType:    types.AggregationCount,
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
				Filters: map[string][]string{
					"region": {"us-west-2"},
				},
			},
			expectedValue: decimal.NewFromFloat(1), // Only one event in us-west-2
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result, err := s.service.GetUsage(s.ctx, tc.request)
			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)
				s.Equal(tc.expectedValue.InexactFloat64(), result.Value.InexactFloat64())
			}
		})
	}
}

func (s *EventServiceSuite) TestGetUsageByMeter() {
	// Setup test meter with required fields
	testMeter := &meter.Meter{
		ID:        "meter-1",
		Name:      "Test Meter", // Add a name
		EventName: "api_request",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "duration_ms",
		},
		Filters: []meter.Filter{
			{
				Key:    "status",
				Values: []string{"success", "error"},
			},
			{
				Key:    "region",
				Values: []string{"us-east-1"},
			},
		},
		ResetUsage: types.ResetUsageBillingPeriod, // Ensure ResetUsage is valid
		BaseModel: types.BaseModel{
			TenantID: types.GetTenantID(s.ctx),
		},
	}

	// Add the test meter to the mocked meter repository
	mockedMeterRepo := testutil.NewInMemoryMeterStore()
	err := mockedMeterRepo.CreateMeter(s.ctx, testMeter)
	s.NoError(err)

	// Setup the event service with the mocked meter repository
	s.service = NewEventService(
		s.eventRepo,
		mockedMeterRepo,
		s.publisher,
		s.logger,
	)

	// Setup test events
	testingEvents := []*dto.IngestEventRequest{
		{
			EventID:            "evt-1",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          time.Now().Add(-1 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(100),
				"status":      "success",
				"region":      "us-east-1",
			},
		},
		{
			EventID:            "evt-2",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          time.Now().Add(-1 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(200),
				"status":      "error",
				"region":      "us-east-1",
			},
		},
		{
			EventID:            "evt-3",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          time.Now().Add(-1 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(300),
				"status":      "success",
				"region":      "us-west-2", // Should be filtered out
			},
		},
	}

	// Insert test events
	for _, evt := range testingEvents {
		event := events.NewEvent(
			evt.EventName,
			types.GetTenantID(s.ctx),
			evt.ExternalCustomerID,
			evt.Properties,
			evt.Timestamp,
			evt.EventID,
			evt.CustomerID,
			evt.Source,
		)
		err := s.eventRepo.InsertEvent(s.ctx, event)
		s.NoError(err)
	}

	// Test usage calculation with filters
	result, err := s.service.GetUsageByMeter(s.ctx, &dto.GetUsageByMeterRequest{
		MeterID:            testMeter.ID,
		ExternalCustomerID: "cust-1",
		StartTime:          time.Now().Add(-2 * time.Hour),
		EndTime:            time.Now(),
		WindowSize:         types.WindowSizeHour,
		Filters: map[string][]string{
			"region": {"us-east-1"},
		},
	})

	s.NoError(err)
	s.NotNil(result)
	s.Equal(decimal.NewFromFloat(300).InexactFloat64(), result.Value.InexactFloat64()) // Only sum of us-east-1 events (100 + 200)
	s.Equal("api_request", result.EventName)
	s.Equal(types.AggregationSum, result.Type)
}

func (s *EventServiceSuite) TestGetEvents() {
	now := time.Now()
	// Setup test data
	testEvents := []*dto.IngestEventRequest{
		{
			EventID:            "evt-1",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-4 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(100),
				"model":       "gpt-3.5",
				"type":        "completion",
				"region":      "us-east-1",
			},
		},
		{
			EventID:            "evt-2",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-3 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(150),
				"model":       "gpt-4",
				"type":        "completion",
				"region":      "us-east-1",
			},
		},
		{
			EventID:            "evt-3",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-2 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(200),
				"model":       "gpt-4o",
				"type":        "embedding",
				"region":      "us-west-2",
			},
		},
		{
			EventID:            "evt-4",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-1 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(250),
				"model":       "gpt-4o",
				"type":        "completion",
				"region":      "eu-west-1",
			},
		},
	}

	// Insert test events
	for _, evt := range testEvents {
		event := events.NewEvent(
			evt.EventName,
			types.GetTenantID(s.ctx),
			evt.ExternalCustomerID,
			evt.Properties,
			evt.Timestamp,
			evt.EventID,
			evt.CustomerID,
			evt.Source,
		)
		err := s.eventRepo.InsertEvent(s.ctx, event)
		s.NoError(err)
	}

	s.Run("get_events_with_pagination", func() {
		// First page
		result, err := s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PageSize:           2,
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 2)
		s.True(result.HasMore)
		s.Equal("evt-4", result.Events[0].ID) // Most recent first
		s.Equal("evt-3", result.Events[1].ID)
		s.Equal(uint64(4), result.TotalCount) // Total count should be 4
		s.Equal(2, result.Offset)             // Offset should be incremented by PageSize

		// Second page using last key
		result, err = s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PageSize:           2,
			IterLastKey:        result.IterLastKey,
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 2)
		s.False(result.HasMore)
		s.Equal("evt-2", result.Events[0].ID)
		s.Equal("evt-1", result.Events[1].ID)
		// For cursor-based pagination, we should still have the total count
		s.Equal(uint64(4), result.TotalCount)
	})

	s.Run("get_events_with_property_filters", func() {
		// Test single property filter
		result, err := s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PropertyFilters: map[string][]string{
				"model": {"gpt-4o"},
			},
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 2)
		s.Equal("evt-4", result.Events[0].ID)
		s.Equal("evt-3", result.Events[1].ID)

		// Test multiple property filters
		result, err = s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PropertyFilters: map[string][]string{
				"model": {"gpt-4o"},
				"type":  {"completion"},
			},
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 1)
		s.Equal("evt-4", result.Events[0].ID)

		// Test multiple values for a property
		result, err = s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PropertyFilters: map[string][]string{
				"model": {"gpt-4", "gpt-3.5"},
			},
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 2)
		s.Equal("evt-2", result.Events[0].ID)
		s.Equal("evt-1", result.Events[1].ID)

		// Test combination of standard filters and property filters
		result, err = s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PropertyFilters: map[string][]string{
				"region": {"us-east-1"},
			},
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 2)
		s.Equal("evt-2", result.Events[0].ID)
		s.Equal("evt-1", result.Events[1].ID)

		// Test with no matching properties
		result, err = s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PropertyFilters: map[string][]string{
				"non_existent_property": {"some_value"},
			},
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 0)
	})

	s.Run("get_events_with_iter_first_key", func() {
		// Get initial state
		initialResult, err := s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PageSize:           2,
		})
		s.NoError(err)
		s.NotNil(initialResult)

		// Add new event with a definitely later timestamp
		newEvent := &dto.IngestEventRequest{
			EventID:            "evt-5",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now,
			Properties: map[string]interface{}{
				"duration_ms": float64(300),
				"model":       "gpt-4o",
				"type":        "completion",
			},
		}

		err = s.service.CreateEvent(s.ctx, newEvent)
		s.NoError(err)

		// Wait a bit for the event to be processed through the message channel
		time.Sleep(100 * time.Millisecond)

		// Query new events using iter_first_key
		result, err := s.service.GetEvents(s.ctx, &dto.GetEventsRequest{
			ExternalCustomerID: "cust-1",
			PageSize:           2,
			IterFirstKey:       initialResult.IterFirstKey,
		})
		s.NoError(err)
		s.NotNil(result)
		s.Len(result.Events, 1)
		s.Equal("evt-5", result.Events[0].ID) // Only the new event
	})
}
