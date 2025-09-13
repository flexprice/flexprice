package kafka

import (
	"context"
	"encoding/json"

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
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", event.TenantID)

	if err := p.producer.Publish(p.config.Topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish event").
			Mark(ierr.ErrValidation)
	}
	return nil
}
