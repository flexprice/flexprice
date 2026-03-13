package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockPubSub is a mock implementation of pubsub.PubSub
type MockPubSub struct {
	mock.Mock
}

func (m *MockPubSub) Publish(ctx context.Context, topic string, msg *message.Message) error {
	args := m.Called(ctx, topic, msg)
	return args.Error(0)
}

func (m *MockPubSub) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	args := m.Called(ctx, topic)
	return args.Get(0).(<-chan *message.Message), args.Error(1)
}

func (m *MockPubSub) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Helper function to create a test service
func createTestRawEventConsumptionService(bulkEnabled bool, batchSize int) (*rawEventsService, *MockPubSub) {
	zapLogger, _ := zap.NewDevelopment()
	testLogger := &logger.Logger{SugaredLogger: zapLogger.Sugar()}

	mockPubSub := new(MockPubSub)
	mockOutputPubSub := new(MockPubSub)

	cfg := &config.Configuration{
		Billing: config.BillingConfig{
			TenantID:      "test-tenant",
			EnvironmentID: "test-env",
		},
		RawEventConsumption: config.RawEventConsumptionConfig{
			Enabled:     true,
			Topic:       "raw_events",
			OutputTopic: "events",
			BulkEvents: config.BulkEventsConfig{
				Enabled:     bulkEnabled,
				BatchSize:   batchSize,
				OutputTopic: "bulk_events",
			},
			RateLimit:     10,
			ConsumerGroup: "test-consumer",
		},
	}

	service := &rawEventsService{
		ServiceParams: ServiceParams{
			Config: cfg,
			Logger: testLogger,
		},
		pubSub:        mockPubSub,
		outputPubSub:  mockOutputPubSub,
		sentryService: &sentry.Service{},
	}

	return service, mockOutputPubSub
}

// Helper to create a valid Bento event JSON
func createValidBentoEvent(id, orgID string) string {
	return `{
		"id": "` + id + `",
		"orgId": "` + orgID + `",
		"methodName": "chat",
		"providerName": "openai",
		"createdAt": "2024-01-01T00:00:00Z",
		"data": {
			"modelName": "gpt-4"
		}
	}`
}

func TestTransformBatchEvents(t *testing.T) {
	service, _ := createTestRawEventConsumptionService(false, 100)

	t.Run("transforms valid events successfully", func(t *testing.T) {
		batch := RawEventBatch{
			Data: []json.RawMessage{
				json.RawMessage(createValidBentoEvent("event-1", "org-1")),
				json.RawMessage(createValidBentoEvent("event-2", "org-2")),
			},
			TenantID:      "tenant-1",
			EnvironmentID: "env-1",
		}

		transformedEvents, skipCount, errorCount := service.transformBatchEvents(batch, "tenant-1", "env-1")

		assert.Equal(t, 2, len(transformedEvents), "should transform 2 events")
		assert.Equal(t, 0, skipCount, "should have 0 skipped events")
		assert.Equal(t, 0, errorCount, "should have 0 errors")
		assert.Equal(t, "event-1", transformedEvents[0].ID)
		assert.Equal(t, "event-2", transformedEvents[1].ID)
	})

	t.Run("skips invalid events", func(t *testing.T) {
		batch := RawEventBatch{
			Data: []json.RawMessage{
				json.RawMessage(createValidBentoEvent("event-1", "org-1")),
				json.RawMessage(`{"id": "event-2"}`), // Missing required fields
			},
			TenantID:      "tenant-1",
			EnvironmentID: "env-1",
		}

		transformedEvents, skipCount, errorCount := service.transformBatchEvents(batch, "tenant-1", "env-1")

		assert.Equal(t, 1, len(transformedEvents), "should transform 1 valid event")
		assert.Equal(t, 1, skipCount, "should skip 1 invalid event")
		assert.Equal(t, 0, errorCount, "should have 0 transformation errors")
	})

	t.Run("handles malformed JSON", func(t *testing.T) {
		batch := RawEventBatch{
			Data: []json.RawMessage{
				json.RawMessage(createValidBentoEvent("event-1", "org-1")),
				json.RawMessage(`{invalid json`),
			},
			TenantID:      "tenant-1",
			EnvironmentID: "env-1",
		}

		transformedEvents, skipCount, errorCount := service.transformBatchEvents(batch, "tenant-1", "env-1")

		assert.Equal(t, 1, len(transformedEvents), "should transform 1 valid event")
		assert.Equal(t, 0, skipCount, "should have 0 skipped events")
		assert.Equal(t, 1, errorCount, "should have 1 transformation error")
	})
}

func TestProcessSingleEventMode(t *testing.T) {
	service, mockOutputPubSub := createTestRawEventConsumptionService(false, 100)

	t.Run("publishes events individually", func(t *testing.T) {
		transformedEvents := []*events.Event{
			{
				ID:                 "event-1",
				TenantID:           "tenant-1",
				EnvironmentID:      "env-1",
				ExternalCustomerID: "org-1",
				EventName:          "openai-chat-gpt-4",
				Timestamp:          time.Now(),
				Properties:         map[string]interface{}{},
			},
			{
				ID:                 "event-2",
				TenantID:           "tenant-1",
				EnvironmentID:      "env-1",
				ExternalCustomerID: "org-2",
				EventName:          "openai-chat-gpt-4",
				Timestamp:          time.Now(),
				Properties:         map[string]interface{}{},
			},
		}

		// Expect 2 individual publish calls
		mockOutputPubSub.On("Publish", mock.Anything, "events", mock.Anything).Return(nil).Times(2)

		err := service.processSingleEventMode(context.Background(), transformedEvents, 0, 0)

		assert.NoError(t, err)
		mockOutputPubSub.AssertExpectations(t)
	})

	t.Run("handles publish failures", func(t *testing.T) {
		transformedEvents := []*events.Event{
			{
				ID:                 "event-1",
				TenantID:           "tenant-1",
				EnvironmentID:      "env-1",
				ExternalCustomerID: "org-1",
				EventName:          "openai-chat-gpt-4",
				Timestamp:          time.Now(),
				Properties:         map[string]interface{}{},
			},
		}

		// Simulate publish failure
		mockOutputPubSub.On("Publish", mock.Anything, "events", mock.Anything).Return(assert.AnError).Once()

		err := service.processSingleEventMode(context.Background(), transformedEvents, 0, 0)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to process")
	})
}

func TestProcessBulkEventMode(t *testing.T) {
	service, mockOutputPubSub := createTestRawEventConsumptionService(true, 100)

	t.Run("batches events correctly - exactly 100 events", func(t *testing.T) {
		// Create 100 events
		transformedEvents := make([]*events.Event, 100)
		for i := 0; i < 100; i++ {
			transformedEvents[i] = &events.Event{
				ID:                 "event-" + string(rune(i)),
				TenantID:           "tenant-1",
				EnvironmentID:      "env-1",
				ExternalCustomerID: "org-1",
				EventName:          "test-event",
				Timestamp:          time.Now(),
				Properties:         map[string]interface{}{},
			}
		}

		// Expect 1 bulk publish call
		mockOutputPubSub.On("Publish", mock.Anything, "bulk_events", mock.MatchedBy(func(msg *message.Message) bool {
			var batch BulkEventBatch
			json.Unmarshal(msg.Payload, &batch)
			return len(batch.Events) == 100
		})).Return(nil).Once()

		err := service.processBulkEventMode(context.Background(), transformedEvents, "tenant-1", "env-1", 0, 0)

		assert.NoError(t, err)
		mockOutputPubSub.AssertExpectations(t)
	})

	t.Run("batches events correctly - 150 events (2 batches)", func(t *testing.T) {
		// Create 150 events
		transformedEvents := make([]*events.Event, 150)
		for i := 0; i < 150; i++ {
			transformedEvents[i] = &events.Event{
				ID:                 "event-" + string(rune(i)),
				TenantID:           "tenant-1",
				EnvironmentID:      "env-1",
				ExternalCustomerID: "org-1",
				EventName:          "test-event",
				Timestamp:          time.Now(),
				Properties:         map[string]interface{}{},
			}
		}

		// Expect 2 bulk publish calls: first with 100, second with 50
		mockOutputPubSub.On("Publish", mock.Anything, "bulk_events", mock.MatchedBy(func(msg *message.Message) bool {
			var batch BulkEventBatch
			json.Unmarshal(msg.Payload, &batch)
			return len(batch.Events) == 100
		})).Return(nil).Once()

		mockOutputPubSub.On("Publish", mock.Anything, "bulk_events", mock.MatchedBy(func(msg *message.Message) bool {
			var batch BulkEventBatch
			json.Unmarshal(msg.Payload, &batch)
			return len(batch.Events) == 50
		})).Return(nil).Once()

		err := service.processBulkEventMode(context.Background(), transformedEvents, "tenant-1", "env-1", 0, 0)

		assert.NoError(t, err)
		mockOutputPubSub.AssertExpectations(t)
	})

	t.Run("verifies bulk batch structure", func(t *testing.T) {
		transformedEvents := []*events.Event{
			{
				ID:                 "event-1",
				TenantID:           "tenant-1",
				EnvironmentID:      "env-1",
				ExternalCustomerID: "org-1",
				EventName:          "test-event",
				Timestamp:          time.Now(),
				Properties:         map[string]interface{}{},
			},
		}

		mockOutputPubSub.On("Publish", mock.Anything, "bulk_events", mock.MatchedBy(func(msg *message.Message) bool {
			var batch BulkEventBatch
			err := json.Unmarshal(msg.Payload, &batch)
			if err != nil {
				return false
			}
			// Verify structure
			return batch.TenantID == "tenant-1" &&
				batch.EnvironmentID == "env-1" &&
				len(batch.Events) == 1 &&
				batch.Events[0].ID == "event-1"
		})).Return(nil).Once()

		err := service.processBulkEventMode(context.Background(), transformedEvents, "tenant-1", "env-1", 0, 0)

		assert.NoError(t, err)
		mockOutputPubSub.AssertExpectations(t)
	})
}

func TestPublishBulkEventBatch(t *testing.T) {
	service, mockOutputPubSub := createTestRawEventConsumptionService(true, 100)

	t.Run("publishes bulk batch with correct metadata", func(t *testing.T) {
		batch := &BulkEventBatch{
			TenantID:      "tenant-1",
			EnvironmentID: "env-1",
			Events: []*events.Event{
				{ID: "event-1"},
				{ID: "event-2"},
			},
		}

		mockOutputPubSub.On("Publish", mock.Anything, "bulk_events", mock.MatchedBy(func(msg *message.Message) bool {
			return msg.Metadata.Get("tenant_id") == "tenant-1" &&
				msg.Metadata.Get("environment_id") == "env-1" &&
				msg.Metadata.Get("bulk_batch_size") == "2"
		})).Return(nil).Once()

		err := service.publishBulkEventBatch(context.Background(), batch, 0)

		assert.NoError(t, err)
		mockOutputPubSub.AssertExpectations(t)
	})
}
