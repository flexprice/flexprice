package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"go.uber.org/zap"
)

type EventPublisher struct {
	producer *Producer
	logger   *logger.Logger
	config   *config.KafkaConfig
}

func NewEventPublisher(producer *Producer, cfg *config.Configuration, logger *logger.Logger) *EventPublisher {
	return &EventPublisher{
		producer: producer,
		logger:   logger,
		config:   &cfg.Kafka,
	}
}

func (p *EventPublisher) Publish(ctx context.Context, event *events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event").
			Mark(ierr.ErrValidation)
	}

	p.logger.With(
		zap.String("event_id", event.ID),
		zap.String("event_name", event.EventName),
		zap.String("tenant_id", event.TenantID),
	).Debug("publishing event to kafka")

	if event.ID == "" {
		event.ID = watermill.NewUUID()
	}

	msg := message.NewMessage(event.ID, payload)

	// Create a deterministic partition key based on tenant_id and external_customer_id
	// This ensures all events for the same customer go to the same partition
	partitionKey := event.TenantID
	if event.ExternalCustomerID != "" {
		partitionKey = fmt.Sprintf("%s:%s", event.TenantID, event.ExternalCustomerID)
	}
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)
	/*
		TODO: Once we support multiple event import integrations (e.g., S3, Postgres, etc.),
		route those imported events to the lazy topic.
	*/
	if err := p.producer.Publish(p.determineTopic(event), msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish event").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (p *EventPublisher) determineTopic(event *events.Event) string {
	if slices.Contains(p.config.RouteTenantsOnLazyMode, event.TenantID) {
		return p.config.TopicLazy
	}
	return p.config.Topic
}
