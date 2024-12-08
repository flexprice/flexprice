package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type EventServiceSuite struct {
	suite.Suite
	ctx        context.Context
	service    *eventService
	store      *testutil.InMemoryEventStore
	broker     *testutil.InMemoryMessageBroker
	msgChannel chan *message.Message
	logger     *logger.Logger
}

func TestEventService(t *testing.T) {
	suite.Run(t, new(EventServiceSuite))
}

func (s *EventServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.store = testutil.NewInMemoryEventStore()
	s.broker = testutil.NewInMemoryMessageBroker()
	s.logger = logger.GetLogger()
	s.service = NewEventService(s.broker, s.store, nil, s.logger).(*eventService)

	// Setup message consumer
	s.msgChannel = s.broker.Subscribe()

	// Start consuming messages
	go func() {
		for msg := range s.msgChannel {
			var event events.Event
			if err := json.Unmarshal(msg.Payload, &event); err != nil {
				continue
			}
			_ = s.store.InsertEvent(s.ctx, &event)
		}
	}()
}

func (s *EventServiceSuite) TearDownTest() {
	s.broker.Close()
}

func (s *EventServiceSuite) TestCreateEvent() {
	testCases := []struct {
		name          string
		input         *dto.IngestEventRequest
		expectedError bool
		verify        func(wg *sync.WaitGroup)
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
			verify: func(wg *sync.WaitGroup) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(100 * time.Millisecond)
					s.True(s.broker.HasMessage("events", "test-1"))
					s.True(s.store.HasEvent("test-1"))
				}()
			},
		},
		{
			name: "missing_required_fields",
			input: &dto.IngestEventRequest{
				EventID: "test-2",
			},
			expectedError: true,
			verify: func(wg *sync.WaitGroup) {
				s.False(s.store.HasEvent("test-2"))
				s.False(s.broker.HasMessage("events", "test-2"))
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			var wg sync.WaitGroup

			err := s.service.CreateEvent(s.ctx, tc.input)
			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)
			}

			tc.verify(&wg)
			wg.Wait()
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
		err := s.store.InsertEvent(s.ctx, event)
		s.NoError(err)
	}

	testCases := []struct {
		name          string
		request       *dto.GetUsageRequest
		expectedValue float64
		expectedError bool
	}{
		{
			name: "count_all_events",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				AggregationType:    string(types.AggregationCount),
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
			},
			expectedValue: float64(3),
			expectedError: false,
		},
		{
			name: "sum_duration_with_status_filter",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				PropertyName:       "duration_ms",
				AggregationType:    string(types.AggregationSum),
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
				Filters: map[string][]string{
					"status": {"success"},
				},
			},
			expectedValue: 300, // 100 + 200 (only success events)
			expectedError: false,
		},
		{
			name: "sum_duration_with_multiple_status_values",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				PropertyName:       "duration_ms",
				AggregationType:    string(types.AggregationSum),
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
				Filters: map[string][]string{
					"status": {"success", "error"},
					"region": {"us-east-1"},
				},
			},
			expectedValue: 250, // 100 + 150 (us-east-1 events only)
			expectedError: false,
		},
		{
			name: "count_with_region_filter",
			request: &dto.GetUsageRequest{
				ExternalCustomerID: "cust-1",
				EventName:          "api_request",
				AggregationType:    string(types.AggregationCount),
				StartTime:          time.Now().Add(-2 * time.Hour),
				EndTime:            time.Now(),
				Filters: map[string][]string{
					"region": {"us-west-2"},
				},
			},
			expectedValue: 1, // Only one event in us-west-2
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
				s.Equal(tc.expectedValue, result.Value)
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
	s.service = NewEventService(s.broker, s.store, mockedMeterRepo, s.logger).(*eventService)

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
		err := s.store.InsertEvent(s.ctx, event)
		s.NoError(err)
	}

	// Test usage calculation with filters
	result, err := s.service.GetUsageByMeter(s.ctx, &dto.GetUsageByMeterRequest{
		MeterID:            testMeter.ID,
		ExternalCustomerID: "cust-1",
		StartTime:          time.Now().Add(-2 * time.Hour),
		EndTime:            time.Now(),
		WindowSize:         types.WindowSizeHour,
	})

	s.NoError(err)
	s.NotNil(result)
	s.Equal(float64(300), result.Value) // Only sum of us-east-1 events (100 + 200)
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
			},
		},
		{
			EventID:            "evt-2",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-3 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(150),
			},
		},
		{
			EventID:            "evt-3",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-2 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(200),
			},
		},
		{
			EventID:            "evt-4",
			ExternalCustomerID: "cust-1",
			EventName:          "api_request",
			Timestamp:          now.Add(-1 * time.Hour),
			Properties: map[string]interface{}{
				"duration_ms": float64(250),
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
		err := s.store.InsertEvent(s.ctx, event)
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
