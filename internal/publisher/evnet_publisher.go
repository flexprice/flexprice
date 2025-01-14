package publisher

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/dynamodb"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

// EventPublisher handles event publishing across multiple destinations
type EventPublisher interface {
	Publish(ctx context.Context, event *events.Event) error
}

type eventPublisher struct {
	kafkaPublisher  *kafka.EventPublisher
	dynamoPublisher *dynamodb.EventPublisher
	logger          *logger.Logger
	config          *config.EventConfig
	mu              sync.RWMutex
}

// NewEventPublisher creates a new publisher
func NewEventPublisher(
	cfg *config.Configuration,
	logger *logger.Logger,
	kafkaProducer *kafka.Producer,
	dynamoClient *dynamodb.Client,
) (EventPublisher, error) {
	publisher := &eventPublisher{
		logger: logger,
		config: &cfg.Event,
	}

	// Initialize publishers based on configuration
	if cfg.Event.PublishDestination == types.PublishToKafka || cfg.Event.PublishDestination == types.PublishToAll {
		if kafkaProducer == nil {
			return nil, fmt.Errorf("kafka producer is not initialized but it is one of the publish destinations")
		}
		publisher.kafkaPublisher = kafka.NewEventPublisher(kafkaProducer, cfg, logger)
	}

	if cfg.Event.PublishDestination == types.PublishToDynamoDB || cfg.Event.PublishDestination == types.PublishToAll {
		if dynamoClient == nil {
			return nil, fmt.Errorf("dynamodb client is not initialized but it is one of the publish destinations")
		}
		publisher.dynamoPublisher = dynamodb.NewEventPublisher(dynamoClient, cfg, logger)
	}

	if publisher.kafkaPublisher == nil && publisher.dynamoPublisher == nil {
		return nil, fmt.Errorf("no publishers configured for destination: %s", cfg.Event.PublishDestination)
	}

	return publisher, nil
}

func (s *eventPublisher) Publish(ctx context.Context, event *events.Event) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.logger.With(
		zap.String("event_id", event.ID),
		zap.String("event_name", event.EventName),
		zap.String("destination", string(s.config.PublishDestination)),
	).Debug("publishing event")

	switch s.config.PublishDestination {
	case types.PublishToKafka:
		return s.kafkaPublisher.Publish(ctx, event)
	case types.PublishToDynamoDB:
		return s.dynamoPublisher.Publish(ctx, event)
	case types.PublishToAll:
		// Publish to both and fail if either fails
		var kafkaErr, dynamoErr error
		if err := s.kafkaPublisher.Publish(ctx, event); err != nil {
			kafkaErr = fmt.Errorf("failed to publish to kafka: %w", err)
		}

		if err := s.dynamoPublisher.Publish(ctx, event); err != nil {
			dynamoErr = fmt.Errorf("failed to publish to dynamodb: %w", err)
		}

		if kafkaErr != nil && dynamoErr != nil {
			return fmt.Errorf("failed to publish to both kafka and dynamodb: %v, %v", kafkaErr, dynamoErr)
		} else if kafkaErr != nil {
			return kafkaErr
		} else if dynamoErr != nil {
			return dynamoErr
		}

		return nil
	default:
		return fmt.Errorf("unknown publish destination: %s", s.config.PublishDestination)
	}
}

func (s *eventPublisher) publishWebhookEvent(ctx context.Context, event *events.Event) error {
	webhookURL := s.config.WebhookURL
	webhookSecret := s.config.WebhookSecret

	if webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", webhookSecret))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook request failed with status: %s", resp.Status)
	}

	return nil
}
